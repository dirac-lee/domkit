package outbox

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/dirac-lee/domkit/domain"
)

type sampleEvent struct {
	domain.BaseDomainEvent[string]
	Kind string
}

func (*sampleEvent) EventName() string { return "sample.event" }

var errPersist = errors.New("persist failed")

func newTestSerializer() *JsonEventSerializer {
	ser := NewJsonEventSerializer()
	ser.Register("sample.event", func() domain.DomainEvent { return &sampleEvent{} })
	return ser
}

func newSampleEvent(aggID string, version uint64) *sampleEvent {
	return &sampleEvent{
		BaseDomainEvent: domain.NewBaseDomainEvent(aggID, version),
		Kind:            "hello",
	}
}

// ---------- JsonEventSerializer ----------

func TestJsonEventSerializerRoundTrip(t *testing.T) {
	ser := newTestSerializer()
	original := newSampleEvent("agg-1", 3)

	payload, err := ser.Serialize(original)
	if err != nil {
		t.Fatalf("serialize failed: %v", err)
	}
	back, err := ser.Deserialize(payload, "sample.event")
	if err != nil {
		t.Fatalf("deserialize failed: %v", err)
	}
	got, ok := back.(*sampleEvent)
	if !ok {
		t.Fatalf("expected *sampleEvent, got %T", back)
	}
	if got.Kind != "hello" || got.AggregateID() != "agg-1" || got.AggregateVersion() != 3 {
		t.Fatalf("round-trip content mismatch: %+v", got)
	}
}

func TestJsonEventSerializerUnknownEventName(t *testing.T) {
	ser := newTestSerializer()
	if _, err := ser.Deserialize([]byte(`{}`), "ghost.event"); err == nil {
		t.Fatal("expected error for unregistered event name")
	}
}

// ---------- UoW 专用最小假存储（只记录 SaveMessage） ----------

type recordingStore struct {
	saved  []*OutboxMessage
	lastTx any
}

func (s *recordingStore) SaveMessage(_ context.Context, tx any, msg *OutboxMessage) error {
	s.lastTx = tx
	s.saved = append(s.saved, msg)
	return nil
}
func (s *recordingStore) ClaimPending(context.Context, int, time.Duration, time.Duration, string) ([]*OutboxMessage, error) {
	return nil, nil
}
func (s *recordingStore) MarkSent(context.Context, string, string) error { return nil }
func (s *recordingStore) Release(context.Context, string, string, string) (int, error) {
	return 0, nil
}
func (s *recordingStore) MoveToDeadLetter(context.Context, string, string, string) error {
	return nil
}

// ---------- OutboxUnitOfWork ----------

func TestOutboxUnitOfWorkPersistsBeforeSavingMessageInSameTx(t *testing.T) {
	store := &recordingStore{}
	ser := newTestSerializer()
	uow := NewOutboxUnitOfWork("tx-1", store, ser)

	ar := &domain.AggregateRoot[string]{ID: "agg-1"}
	ar.CollectEvent(newSampleEvent("agg-1", 3))

	persisted := false
	uow.RegisterChange(domain.ChangeNew, ar, func(_ context.Context) error {
		// 聚合落库时 outbox 表中还不应存在消息。
		if len(store.saved) != 0 {
			return errors.New("outbox message saved before aggregate persisted")
		}
		persisted = true
		return nil
	})

	if err := uow.Commit(context.Background()); err != nil {
		t.Fatalf("commit failed: %v", err)
	}
	if !persisted {
		t.Fatal("persist action not executed")
	}
	if len(store.saved) != 1 {
		t.Fatalf("expected 1 outbox message, got %d", len(store.saved))
	}
	msg := store.saved[0]
	if store.lastTx != "tx-1" {
		t.Fatalf("message must be saved in uow tx, got %v", store.lastTx)
	}
	if msg.EventName != "sample.event" || msg.AggregateID != "agg-1" || msg.Version != 3 ||
		msg.Status != StatusPending {
		t.Fatalf("unexpected message: %+v", msg)
	}
	back, err := ser.Deserialize(msg.Payload, msg.EventName)
	if err != nil {
		t.Fatalf("saved payload is not deserializable: %v", err)
	}
	if back.(*sampleEvent).Kind != "hello" {
		t.Fatalf("payload content mismatch: %+v", back)
	}
	// 事件已被 PullEvents 取走，重复提交不应再产生消息。
	uom2 := NewOutboxUnitOfWork("tx-2", store, ser)
	uom2.RegisterChange(domain.ChangeNew, ar, func(_ context.Context) error { return nil })
	if err := uom2.Commit(context.Background()); err != nil {
		t.Fatalf("second commit failed: %v", err)
	}
	if len(store.saved) != 1 {
		t.Fatalf("pulled events must not be redelivered, got %d messages", len(store.saved))
	}
}

func TestOutboxUnitOfWorkPersistFailureLeavesNoMessage(t *testing.T) {
	store := &recordingStore{}
	uow := NewOutboxUnitOfWork("tx", store, newTestSerializer())
	uow.RegisterChange(domain.ChangeNew, &domain.AggregateRoot[string]{}, func(_ context.Context) error {
		return errPersist
	})

	if err := uow.Commit(context.Background()); !errors.Is(err, errPersist) {
		t.Fatalf("expected wrapped persist error, got %v", err)
	}
	if len(store.saved) != 0 {
		t.Fatal("no outbox message may be written when persist fails")
	}
}

func TestOutboxUnitOfWorkDoubleCommitRejected(t *testing.T) {
	uow := NewOutboxUnitOfWork("tx", &recordingStore{}, newTestSerializer())
	uow.RegisterChange(domain.ChangeNew, &domain.AggregateRoot[string]{}, func(_ context.Context) error { return nil })

	if err := uow.Commit(context.Background()); err != nil {
		t.Fatalf("first commit failed: %v", err)
	}
	if err := uow.Commit(context.Background()); !errors.Is(err, ErrOutboxUoWCommitted) {
		t.Fatalf("expected ErrOutboxUoWCommitted, got %v", err)
	}
}

// ---------- 测试工具：基于内存存储装配 Relay 场景 ----------

var fixedNow = time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)

func newSeedStore(t *testing.T, msgs ...*OutboxMessage) *MemoryOutboxStore {
	t.Helper()
	store := NewMemoryOutboxStore()
	now := fixedNow
	store.SetClock(func() time.Time { return now })
	for _, m := range msgs {
		if err := store.SaveMessage(context.Background(), "tx", m); err != nil {
			t.Fatalf("seed message failed: %v", err)
		}
	}
	return store
}

// markerPublisher 对 AggregateKey == "will-fail" 的事件发布失败。
type markerPublisher struct{}

func (markerPublisher) Publish(_ context.Context, evt domain.DomainEvent) error {
	if evt.AggregateKey() == "will-fail" {
		return errors.New("publish failed")
	}
	return nil
}

func testRelay(store OutboxStore, maxRetry int) *Relay {
	r := NewRelay(store, markerPublisher{}, newTestSerializer(), RelayConfig{
		Interval:  time.Second,
		BatchSize: 100,
		Grace:     time.Second,
		Lease:     30 * time.Second,
		MaxRetry:  maxRetry,
	})
	r.newToken = func() string { return "token" }
	return r
}

func seedMsg(id, aggKey string, retry int, status OutboxStatus, createdAt time.Time) *OutboxMessage {
	payload, _ := newTestSerializer().Serialize(newSampleEvent(aggKey, 1))
	return &OutboxMessage{
		ID: id, EventName: "sample.event", Payload: payload,
		RetryCount: retry, Status: status, CreatedAt: createdAt,
	}
}

// ---------- Relay：认领 → 发布 → 完结 ----------

func TestRelaySuccessRetryPoisonAndDeadLetter(t *testing.T) {
	old := seedMsg("m-ok", "agg-ok", 0, StatusPending, fixedNow.Add(-time.Minute))
	ghost := &OutboxMessage{
		ID: "m-ghost", EventName: "ghost.event", Payload: []byte(`{}`),
		Status: StatusPending, CreatedAt: fixedNow.Add(-time.Minute),
	}
	retryMsg := seedMsg("m-retry", "will-fail", 0, StatusPending, fixedNow.Add(-time.Minute))
	deadMsg := seedMsg("m-dead", "will-fail", 2, StatusPending, fixedNow.Add(-time.Minute))
	store := newSeedStore(t, old, ghost, retryMsg, deadMsg)

	relay := testRelay(store, 3) // 最大发布 3 次
	if err := relay.ScanPending(context.Background()); err != nil {
		t.Fatalf("scan failed: %v", err)
	}
	if got := store.Get("m-ok"); got.Status != StatusSent {
		t.Fatalf("m-ok expected sent, got %s", got.Status)
	}
	if got := store.Get("m-ghost"); got.Status != StatusDeadLetter {
		t.Fatalf("poison message expected deadletter, got %s", got.Status)
	}
	if got := store.Get("m-retry"); got.Status != StatusPending || got.RetryCount != 1 {
		t.Fatalf("m-retry expected released pending with retry=1, got %s retry=%d", got.Status, got.RetryCount)
	}
	if got := store.Get("m-dead"); got.Status != StatusDeadLetter {
		t.Fatalf("m-dead expected deadletter, got %s", got.Status)
	}
}

func TestRelayRespectsGraceWindow(t *testing.T) {
	// 刚创建（宽限期内）的消息本轮不认领。
	fresh := seedMsg("m-fresh", "agg-fresh", 0, StatusPending, fixedNow.Add(-time.Millisecond))
	store := newSeedStore(t, fresh)
	relay := testRelay(store, 3)

	if err := relay.ScanPending(context.Background()); err != nil {
		t.Fatalf("scan failed: %v", err)
	}
	if got := store.Get("m-fresh"); got.Status != StatusPending {
		t.Fatalf("fresh message must stay pending during grace, got %s", got.Status)
	}
}

func TestRelayZeroMaxRetryRetriesForever(t *testing.T) {
	msg := seedMsg("m-forever", "will-fail", 99, StatusPending, fixedNow.Add(-time.Minute))
	store := newSeedStore(t, msg)
	relay := testRelay(store, 0) // 0 = 无限重试

	if err := relay.ScanPending(context.Background()); err != nil {
		t.Fatalf("scan failed: %v", err)
	}
	got := store.Get("m-forever")
	if got.Status != StatusPending || got.RetryCount != 100 {
		t.Fatalf("infinite retry -> released pending retry=100, got %s retry=%d", got.Status, got.RetryCount)
	}
}

// ---------- 多实例抢占安全 ----------

// 并发认领：两个 Relay（不同 token）对同一存储同时 ClaimPending，
// 每行只能被一个 token 抢中。
func TestClaimPendingIsExclusiveAcrossInstances(t *testing.T) {
	msgs := []*OutboxMessage{
		seedMsg("m1", "a", 0, StatusPending, fixedNow.Add(-time.Minute)),
		seedMsg("m2", "a", 0, StatusPending, fixedNow.Add(-time.Minute)),
		seedMsg("m3", "a", 0, StatusPending, fixedNow.Add(-time.Minute)),
		seedMsg("m4", "a", 0, StatusPending, fixedNow.Add(-time.Minute)),
	}
	store := newSeedStore(t, msgs...)

	// 每轮最多抢 2 条：先到锁的实例取走最早的 2 条，后到者取走剩余 2 条，
	// 既无重叠也无遗漏（batchSize=100 时由单个实例一把抢完同样是合法语义）。
	done := make(chan int, 2)
	claim := func(token string) {
		got, _ := store.ClaimPending(context.Background(), 2, time.Second, 30*time.Second, token)
		done <- len(got)
	}
	go claim("instance-A")
	go claim("instance-B")

	a := <-done
	b := <-done
	if a != 2 || b != 2 {
		t.Fatalf("expected 2 + 2 exclusive claims, got %d + %d", a, b)
	}
	// 认领后全部为 processing 且 token 非空。
	for _, id := range []string{"m1", "m2", "m3", "m4"} {
		if got := store.Get(id); got.Status != StatusProcessing || got.ClaimToken == "" {
			t.Fatalf("%s not properly claimed: %+v", id, got)
		}
	}
}

// 租约过期后可被其他实例重认领；旧持有者的状态推进必须被 token 守卫拒绝。
func TestStaleClaimCanBeReclaimedAndGuarded(t *testing.T) {
	store := NewMemoryOutboxStore()
	created := fixedNow.Add(-time.Hour)
	store.SetClock(func() time.Time { return fixedNow })
	msg := seedMsg("m-stale", "a", 1, StatusProcessing, created)
	msg.ClaimToken = "old-owner"
	msg.ClaimedAt = fixedNow.Add(-time.Minute) // 租约 30s，已过期
	if err := store.SaveMessage(context.Background(), "tx", msg); err != nil {
		t.Fatal(err)
	}

	// 旧持有者在过期后仍尝试完结 → 被守卫拒绝（此刻尚未重认领，但 token 不符即拒绝）。
	// 先模拟新实例重认领。
	got, err := store.ClaimPending(context.Background(), 100, time.Second, 30*time.Second, "new-owner")
	if err != nil || len(got) != 1 || got[0].ClaimToken != "new-owner" {
		t.Fatalf("stale claim should be reclaimable, got=%d err=%v", len(got), err)
	}
	if err := store.MarkSent(context.Background(), "m-stale", "old-owner"); !errors.Is(err, ErrClaimLost) {
		t.Fatalf("old owner MarkSent must fail with ErrClaimLost, got %v", err)
	}
	if _, err := store.Release(context.Background(), "m-stale", "old-owner", "x"); !errors.Is(err, ErrClaimLost) {
		t.Fatalf("old owner Release must fail with ErrClaimLost, got %v", err)
	}
	// 新持有者正常完结。
	if err := store.MarkSent(context.Background(), "m-stale", "new-owner"); err != nil {
		t.Fatalf("new owner MarkSent failed: %v", err)
	}
	if row := store.Get("m-stale"); row.Status != StatusSent {
		t.Fatalf("expected sent by new owner, got %s", row.Status)
	}
}
