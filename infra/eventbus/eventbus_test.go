package eventbus

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/dirac-lee/domkit/domain"
)

type testEvent struct {
	domain.BaseDomainEvent[string]
	Tag string
}

func (*testEvent) EventName() string { return "test.event" }

type otherEvent struct {
	domain.BaseDomainEvent[string]
}

func (*otherEvent) EventName() string { return "other.event" }

func newTestEvent() *testEvent {
	return &testEvent{BaseDomainEvent: domain.NewBaseDomainEvent("agg-1", 1), Tag: "x"}
}

// countingMetrics 线程安全的测试 Metrics。
type countingMetrics struct {
	mu           sync.Mutex
	publishedOK  int
	publishedBad int
	consumed     []consumeRecord
	deadLetters  []deadRecord
}

type consumeRecord struct {
	subscriber string
	success    bool
	attempts   int
}

type deadRecord struct {
	subscriber string
	reason     string
}

func (m *countingMetrics) Published(_ string, success bool, _ int64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if success {
		m.publishedOK++
	} else {
		m.publishedBad++
	}
}

func (m *countingMetrics) Consumed(_, subscriber string, success bool, attempts int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.consumed = append(m.consumed, consumeRecord{subscriber, success, attempts})
}

func (m *countingMetrics) DeadLettered(_, subscriber, reason string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.deadLetters = append(m.deadLetters, deadRecord{subscriber, reason})
}

func (m *countingMetrics) snapshot() (ok, bad int, consumed []consumeRecord, dead []deadRecord) {
	m.mu.Lock()
	defer m.mu.Unlock()
	consumed = append([]consumeRecord(nil), m.consumed...)
	dead = append([]deadRecord(nil), m.deadLetters...)
	return m.publishedOK, m.publishedBad, consumed, dead
}

// ---------- 同步总线 ----------

func TestMemoryEventBusPriorityOrder(t *testing.T) {
	bus := NewMemoryEventBus()
	var order []string
	bus.Subscribe("test.event", "low", FuncHandler(func(domain.DomainEvent) error {
		order = append(order, "low")
		return nil
	}), 1)
	bus.Subscribe("test.event", "high", FuncHandler(func(domain.DomainEvent) error {
		order = append(order, "high")
		return nil
	}), 10)
	bus.Subscribe("test.event", "mid", FuncHandler(func(domain.DomainEvent) error {
		order = append(order, "mid")
		return nil
	}), 5)

	if err := bus.Publish(newTestEvent()); err != nil {
		t.Fatalf("publish failed: %v", err)
	}
	want := []string{"high", "mid", "low"}
	for i := range want {
		if order[i] != want[i] {
			t.Fatalf("priority order mismatch: got %v want %v", order, want)
		}
	}
}

func TestMemoryEventBusFailureShortCircuits(t *testing.T) {
	bus := NewMemoryEventBus()
	metrics := &countingMetrics{}
	bus.SetMetrics(metrics)
	boom := errors.New("boom")
	reached := false
	bus.Subscribe("test.event", "first", FuncHandler(func(domain.DomainEvent) error { return boom }), 10)
	bus.Subscribe("test.event", "second", FuncHandler(func(domain.DomainEvent) error {
		reached = true
		return nil
	}), 1)

	if err := bus.Publish(newTestEvent()); !errors.Is(err, boom) {
		t.Fatalf("expected handler error, got %v", err)
	}
	if reached {
		t.Fatal("subscribers after a failing handler must not run")
	}
	ok, bad, consumed, _ := metrics.snapshot()
	if ok != 0 || bad != 1 || len(consumed) != 1 || consumed[0].success {
		t.Fatalf("metrics mismatch: ok=%d bad=%d consumed=%v", ok, bad, consumed)
	}
}

func TestMemoryEventBusEventNameIsolation(t *testing.T) {
	bus := NewMemoryEventBus()
	called := 0
	bus.Subscribe("test.event", "h", FuncHandler(func(domain.DomainEvent) error {
		called++
		return nil
	}), 0)
	if err := bus.Publish(&otherEvent{BaseDomainEvent: domain.NewBaseDomainEvent("a", 1)}); err != nil {
		t.Fatal(err)
	}
	if called != 0 {
		t.Fatalf("handler subscribed to other event must not run, calls=%d", called)
	}
}

func TestSubscribeToTypedHandler(t *testing.T) {
	bus := NewMemoryEventBus()
	var gotTag string
	SubscribeTo[*testEvent](bus, "test.event", "typed", 5, func(e *testEvent) error {
		gotTag = e.Tag
		return nil
	})
	if err := bus.Publish(newTestEvent()); err != nil {
		t.Fatal(err)
	}
	if gotTag != "x" {
		t.Fatalf("typed handler did not receive event: %q", gotTag)
	}
}

// ---------- 异步总线 ----------

func waitCh(t *testing.T, ch <-chan struct{}, timeout time.Duration, msg string) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(timeout):
		t.Fatal("timed out waiting: " + msg)
	}
}

func TestAsyncEventBusFanoutAndMetrics(t *testing.T) {
	metrics := &countingMetrics{}
	bus := NewAsyncEventBus(AsyncConfig{Workers: 2, QueueSize: 16, Metrics: metrics})
	defer bus.Shutdown(context.Background())

	a := make(chan struct{}, 1)
	b := make(chan struct{}, 1)
	bus.Subscribe("test.event", "sub-a", FuncHandler(func(domain.DomainEvent) error {
		a <- struct{}{}
		return nil
	}), 10)
	bus.Subscribe("test.event", "sub-b", FuncHandler(func(domain.DomainEvent) error {
		b <- struct{}{}
		return nil
	}), 1)

	if err := bus.Publish(newTestEvent()); err != nil {
		t.Fatal(err)
	}
	waitCh(t, a, time.Second, "subscriber a")
	waitCh(t, b, time.Second, "subscriber b")

	ok, _, consumed, dead := metrics.snapshot()
	if ok != 1 || len(consumed) != 2 || len(dead) != 0 {
		t.Fatalf("async fanout metrics mismatch: ok=%d consumed=%v dead=%v", ok, consumed, dead)
	}
	for _, c := range consumed {
		if !c.success || c.attempts != 1 {
			t.Fatalf("expected first-attempt success, got %+v", c)
		}
	}
}

func TestAsyncEventBusRetryThenSuccess(t *testing.T) {
	metrics := &countingMetrics{}
	bus := NewAsyncEventBus(AsyncConfig{MaxRetry: 3, RetryDelay: time.Millisecond, Metrics: metrics})
	defer bus.Shutdown(context.Background())

	done := make(chan struct{}, 1)
	attempts := 0
	mu := sync.Mutex{}
	bus.Subscribe("test.event", "flaky", FuncHandler(func(domain.DomainEvent) error {
		mu.Lock()
		attempts++
		n := attempts
		mu.Unlock()
		if n < 3 {
			return errors.New("transient")
		}
		done <- struct{}{}
		return nil
	}), 0)

	if err := bus.Publish(newTestEvent()); err != nil {
		t.Fatal(err)
	}
	waitCh(t, done, time.Second, "retry success")
	if err := bus.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}

	_, _, consumed, dead := metrics.snapshot()
	if len(consumed) != 1 || !consumed[0].success || consumed[0].attempts != 3 {
		t.Fatalf("expected success at attempt 3, got consumed=%v", consumed)
	}
	if len(dead) != 0 {
		t.Fatalf("no dead letter expected, got %v", dead)
	}
}

func TestAsyncEventBusDeadLetterAfterRetries(t *testing.T) {
	metrics := &countingMetrics{}
	type dl struct{ event, sub string }
	deadCh := make(chan dl, 1)
	bus := NewAsyncEventBus(AsyncConfig{
		MaxRetry:   1,
		RetryDelay: time.Millisecond,
		Metrics:    metrics,
		OnDeadLetter: func(eventName, subscriber string, _ error) {
			deadCh <- dl{eventName, subscriber}
		},
	})
	defer bus.Shutdown(context.Background())

	bus.Subscribe("test.event", "always-fail", FuncHandler(func(domain.DomainEvent) error {
		return errors.New("permanent")
	}), 0)

	if err := bus.Publish(newTestEvent()); err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-deadCh:
		if got.event != "test.event" || got.sub != "always-fail" {
			t.Fatalf("dead letter callback mismatch: %+v", got)
		}
	case <-time.After(time.Second):
		t.Fatal("dead letter callback not invoked")
	}
	if err := bus.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	_, _, consumed, dead := metrics.snapshot()
	if len(consumed) != 1 || consumed[0].success || consumed[0].attempts != 2 {
		t.Fatalf("expected failed delivery with 2 attempts, got %v", consumed)
	}
	if len(dead) != 1 || dead[0].reason == "" {
		t.Fatalf("expected 1 dead letter with reason, got %v", dead)
	}
}

// 有界队列满时 Publish 阻塞形成背压，队列有空位后恢复。
func TestAsyncEventBusBackpressure(t *testing.T) {
	gate := make(chan struct{})
	bus := NewAsyncEventBus(AsyncConfig{Workers: 1, QueueSize: 1})
	defer bus.Shutdown(context.Background())

	// 三个订阅者：优先级最高的被 gate 卡住（worker 被占住），队列容量 1，
	// 第三个任务入队时队列必满 → Publish 阻塞背压。
	bus.Subscribe("test.event", "slow", FuncHandler(func(domain.DomainEvent) error {
		<-gate
		return nil
	}), 10)
	bus.Subscribe("test.event", "fast-1", FuncHandler(func(domain.DomainEvent) error { return nil }), 5)
	bus.Subscribe("test.event", "fast-2", FuncHandler(func(domain.DomainEvent) error { return nil }), 1)

	published := make(chan error, 1)
	go func() { published <- bus.Publish(newTestEvent()) }()

	select {
	case err := <-published:
		t.Fatalf("Publish should block under backpressure, returned early with %v", err)
	case <-time.After(30 * time.Millisecond):
	}
	close(gate)
	select {
	case err := <-published:
		if err != nil {
			t.Fatalf("publish after backpressure release failed: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Publish did not return after queue drained")
	}
}

// Shutdown 等待队列排空；关闭后 Publish 返回 ErrBusClosed。
func TestAsyncEventBusShutdownDrainsThenRejects(t *testing.T) {
	delivered := make(chan struct{}, 8)
	bus := NewAsyncEventBus(AsyncConfig{Workers: 2, QueueSize: 8})
	bus.Subscribe("test.event", "h", FuncHandler(func(domain.DomainEvent) error {
		delivered <- struct{}{}
		return nil
	}), 0)

	for i := 0; i < 5; i++ {
		if err := bus.Publish(newTestEvent()); err != nil {
			t.Fatal(err)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := bus.Shutdown(ctx); err != nil {
		t.Fatalf("graceful shutdown should drain queue, got %v", err)
	}
	if len(delivered) != 5 {
		t.Fatalf("expected 5 deliveries before shutdown, got %d", len(delivered))
	}
	if err := bus.Publish(newTestEvent()); !errors.Is(err, ErrBusClosed) {
		t.Fatalf("expected ErrBusClosed after shutdown, got %v", err)
	}
	// 重复 Shutdown 幂等。
	if err := bus.Shutdown(context.Background()); err != nil {
		t.Fatalf("double shutdown must be idempotent, got %v", err)
	}
}

// Shutdown 超时放弃排空（worker 被永久卡住时不阻塞进程退出）。
func TestAsyncEventBusShutdownTimeout(t *testing.T) {
	gate := make(chan struct{})
	bus := NewAsyncEventBus(AsyncConfig{Workers: 1, QueueSize: 4})
	bus.Subscribe("test.event", "stuck", FuncHandler(func(domain.DomainEvent) error {
		<-gate
		return nil
	}), 0)

	if err := bus.Publish(newTestEvent()); err != nil {
		t.Fatal(err)
	}
	// 让 worker 取走任务并卡住。
	time.Sleep(20 * time.Millisecond)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if err := bus.Shutdown(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected deadline exceeded, got %v", err)
	}
	close(gate)
}
