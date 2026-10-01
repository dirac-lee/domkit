//go:build integration

package integration

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"testing"
	"time"

	ordermysql "github.com/dirac-lee/domkit/example/order/internal/order/infra/mysql"
	"github.com/dirac-lee/domkit/infra/outbox"
	"gorm.io/gorm"
)

func TestMySQLOutboxStoreClaimPending(t *testing.T) {
	env := setupMySQLOutboxStore(t)
	prefix := env.newOutboxPrefix(t)
	now := time.Now()

	seedOutboxMessages(t, env.db,
		outboxPO(prefix+"old-pending", outbox.StatusPending, "", now.Add(-2*time.Minute), nil, 0),
		outboxPO(prefix+"fresh-pending", outbox.StatusPending, "", now, nil, 0),
		outboxPO(prefix+"expired-processing", outbox.StatusProcessing, "old-owner", now.Add(-3*time.Minute), ptrTime(now.Add(-2*time.Minute)), 1),
		outboxPO(prefix+"active-processing", outbox.StatusProcessing, "active-owner", now.Add(-3*time.Minute), ptrTime(now.Add(-10*time.Second)), 0),
	)

	got, err := env.store.ClaimPending(context.Background(), 10, time.Minute, time.Minute, "relay-1")
	if err != nil {
		t.Fatalf("ClaimPending() unexpected error: %v", err)
	}
	gotIDs := outboxMessageIDs(got)
	wantIDs := []string{prefix + "expired-processing", prefix + "old-pending"}
	if !slices.Equal(gotIDs, wantIDs) {
		t.Fatalf("claimed ids = %v, want %v", gotIDs, wantIDs)
	}
	assertOutboxRow(t, env.db, prefix+"old-pending", outbox.StatusProcessing, "relay-1", 0, "")
	assertOutboxRow(t, env.db, prefix+"expired-processing", outbox.StatusProcessing, "relay-1", 1, "")
	assertOutboxRow(t, env.db, prefix+"fresh-pending", outbox.StatusPending, "", 0, "")
	assertOutboxRow(t, env.db, prefix+"active-processing", outbox.StatusProcessing, "active-owner", 0, "")
}

func TestMySQLOutboxStoreStateTransitions(t *testing.T) {
	env := setupMySQLOutboxStore(t)
	prefix := env.newOutboxPrefix(t)
	now := time.Now().Add(-2 * time.Minute)

	seedOutboxMessages(t, env.db,
		outboxPO(prefix+"sent", outbox.StatusProcessing, "relay-1", now, ptrTime(now), 0),
		outboxPO(prefix+"release", outbox.StatusProcessing, "relay-1", now, ptrTime(now), 2),
		outboxPO(prefix+"dead", outbox.StatusProcessing, "relay-1", now, ptrTime(now), 0),
	)

	if err := env.store.MarkSent(context.Background(), prefix+"sent", "relay-1"); err != nil {
		t.Fatalf("MarkSent() unexpected error: %v", err)
	}
	attempts, err := env.store.Release(context.Background(), prefix+"release", "relay-1", "broker down")
	if err != nil {
		t.Fatalf("Release() unexpected error: %v", err)
	}
	if attempts != 3 {
		t.Fatalf("Release() attempts = %d, want 3", attempts)
	}
	if err := env.store.MoveToDeadLetter(context.Background(), prefix+"dead", "relay-1", "bad payload"); err != nil {
		t.Fatalf("MoveToDeadLetter() unexpected error: %v", err)
	}

	assertOutboxRow(t, env.db, prefix+"sent", outbox.StatusSent, "relay-1", 0, "")
	assertOutboxSentAt(t, env.db, prefix+"sent")
	assertOutboxRow(t, env.db, prefix+"release", outbox.StatusPending, "", 3, "broker down")
	assertOutboxRow(t, env.db, prefix+"dead", outbox.StatusDeadLetter, "", 0, "bad payload")
}

func TestMySQLOutboxStoreClaimLost(t *testing.T) {
	env := setupMySQLOutboxStore(t)
	prefix := env.newOutboxPrefix(t)
	now := time.Now().Add(-2 * time.Minute)

	seedOutboxMessages(t, env.db,
		outboxPO(prefix+"owned", outbox.StatusProcessing, "relay-1", now, ptrTime(now), 0),
	)

	if err := env.store.MarkSent(context.Background(), prefix+"owned", "other"); !errors.Is(err, outbox.ErrClaimLost) {
		t.Fatalf("MarkSent() error = %v, want ErrClaimLost", err)
	}
	if _, err := env.store.Release(context.Background(), prefix+"owned", "other", "failed"); !errors.Is(err, outbox.ErrClaimLost) {
		t.Fatalf("Release() error = %v, want ErrClaimLost", err)
	}
	if err := env.store.MoveToDeadLetter(context.Background(), prefix+"owned", "other", "bad"); !errors.Is(err, outbox.ErrClaimLost) {
		t.Fatalf("MoveToDeadLetter() error = %v, want ErrClaimLost", err)
	}
	assertOutboxRow(t, env.db, prefix+"owned", outbox.StatusProcessing, "relay-1", 0, "")
}

type mysqlOutboxEnv struct {
	db    *gorm.DB
	store *ordermysql.OutboxStore
}

func setupMySQLOutboxStore(t *testing.T) mysqlOutboxEnv {
	t.Helper()

	dsn := envOr(envMySQLDSN, defaultMySQLDSN)
	db := probeMySQL(t, dsn)
	if err := ordermysql.Migrate(db); err != nil {
		t.Fatalf("迁移 MySQL 表失败: %v", err)
	}
	return mysqlOutboxEnv{db: db, store: ordermysql.NewOutboxStore(db)}
}

func (e mysqlOutboxEnv) newOutboxPrefix(t *testing.T) string {
	t.Helper()

	prefix := fmt.Sprintf("it-outbox-%d-", time.Now().UnixNano())
	t.Cleanup(func() { e.cleanupOutboxPrefix(t, prefix) })
	return prefix
}

func (e mysqlOutboxEnv) cleanupOutboxPrefix(t *testing.T, prefix string) {
	t.Helper()

	if err := e.db.Where("id LIKE ?", prefix+"%").Delete(&ordermysql.OutboxMessagePO{}).Error; err != nil {
		t.Errorf("清理 outbox 测试数据失败: %v", err)
	}
}

func seedOutboxMessages(t *testing.T, db *gorm.DB, rows ...ordermysql.OutboxMessagePO) {
	t.Helper()

	if err := db.Create(&rows).Error; err != nil {
		t.Fatalf("写入 outbox 测试数据失败: %v", err)
	}
}

func outboxPO(id string, status outbox.OutboxStatus, token string,
	createdAt time.Time, claimedAt *time.Time, retryCount int) ordermysql.OutboxMessagePO {
	return ordermysql.OutboxMessagePO{
		ID:          id,
		EventName:   "order.test",
		AggregateID: id,
		Version:     1,
		Payload:     []byte(`{}`),
		Status:      string(status),
		RetryCount:  retryCount,
		CreatedAt:   createdAt,
		ClaimToken:  token,
		ClaimedAt:   claimedAt,
	}
}

func outboxMessageIDs(messages []*outbox.OutboxMessage) []string {
	ids := make([]string, 0, len(messages))
	for _, msg := range messages {
		ids = append(ids, msg.ID)
	}
	return ids
}

func ptrTime(t time.Time) *time.Time {
	return &t
}

func assertOutboxRow(t *testing.T, db *gorm.DB, id string, wantStatus outbox.OutboxStatus,
	wantToken string, wantRetry int, wantError string) {
	t.Helper()

	var po ordermysql.OutboxMessagePO
	if err := db.Where("id = ?", id).Take(&po).Error; err != nil {
		t.Fatalf("查询 outbox %q 失败: %v", id, err)
	}
	if po.Status != string(wantStatus) || po.ClaimToken != wantToken ||
		po.RetryCount != wantRetry || po.LastError != wantError {
		t.Fatalf("outbox %q = status:%s token:%s retry:%d error:%q, want status:%s token:%s retry:%d error:%q",
			id, po.Status, po.ClaimToken, po.RetryCount, po.LastError,
			wantStatus, wantToken, wantRetry, wantError)
	}
}

func assertOutboxSentAt(t *testing.T, db *gorm.DB, id string) {
	t.Helper()

	var po ordermysql.OutboxMessagePO
	if err := db.Where("id = ?", id).Take(&po).Error; err != nil {
		t.Fatalf("查询 outbox %q 失败: %v", id, err)
	}
	if po.SentAt == nil || po.SentAt.IsZero() {
		t.Fatalf("outbox %q sent_at should be set", id)
	}
}
