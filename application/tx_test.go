package application

import (
	"context"
	"errors"
	"testing"

	"github.com/dirac-lee/domkit/domain"
)

// countingTxManager 记录事务边界与传播行为的测试替身。
type countingTxManager struct {
	begins    int
	commits   int
	rollbacks int
	lastTx    any
	lastProp  Propagation
	fail      bool // 为 true 时模拟提交阶段失败
}

func (m *countingTxManager) DoInTx(ctx context.Context, p Propagation, fn func(context.Context, any) error) error {
	m.begins++
	m.lastProp = p
	tx := "tx-handle"
	m.lastTx = tx
	if err := fn(ctx, tx); err != nil {
		m.rollbacks++
		return err
	}
	if m.fail {
		m.rollbacks++
		return errors.New("commit failed")
	}
	m.commits++
	return nil
}

func TestInTxReturnsValueAndCommits(t *testing.T) {
	tm := &countingTxManager{}
	v, err := InTx(context.Background(), tm, PropagationRequired,
		func(_ context.Context, tx any) (string, error) {
			if tx != "tx-handle" {
				return "", errors.New("tx handle not propagated")
			}
			return "ok", nil
		})
	if err != nil || v != "ok" {
		t.Fatalf("unexpected: v=%q err=%v", v, err)
	}
	if tm.begins != 1 || tm.commits != 1 || tm.rollbacks != 0 || tm.lastProp != PropagationRequired {
		t.Fatalf("tx boundary mismatch: %+v", tm)
	}
}

func TestInTxRollsBackOnCallbackError(t *testing.T) {
	tm := &countingTxManager{}
	_, err := InTx[int](context.Background(), tm, PropagationRequiresNew,
		func(context.Context, any) (int, error) { return 0, errBoom })
	if !errors.Is(err, errBoom) {
		t.Fatalf("expected errBoom, got %v", err)
	}
	if tm.commits != 0 || tm.rollbacks != 1 || tm.lastProp != PropagationRequiresNew {
		t.Fatalf("expected rollback with REQUIRES_NEW: %+v", tm)
	}
}

func TestNoopTxManagerPassesNilTx(t *testing.T) {
	called := false
	err := NoopTxManager{}.DoInTx(context.Background(), PropagationRequired,
		func(_ context.Context, tx any) error {
			called = true
			if tx != nil {
				t.Fatalf("noop tx must pass nil handle, got %v", tx)
			}
			return nil
		})
	if err != nil || !called {
		t.Fatalf("noop manager should run callback directly: called=%v err=%v", called, err)
	}
}

func TestTxCommandExecutorRunsUoWInsideTransaction(t *testing.T) {
	tm := &countingTxManager{}
	repo := &fakeRepo{}
	var seenTx any
	exec := NewTxCommandExecutor(tm, PropagationRequired,
		func(tx any) domain.UnitOfWork {
			seenTx = tx
			return NewSimpleUnitOfWork(nil)
		})

	agg := newAggregate("a1")
	_, err := exec.Execute(context.Background(), agg, repo,
		func(a *fakeAggregate) error { a.NextVersion(); return nil })
	if err != nil {
		t.Fatalf("execute failed: %v", err)
	}
	if tm.begins != 1 || tm.commits != 1 || tm.rollbacks != 0 {
		t.Fatalf("uow must commit inside tx: %+v", tm)
	}
	if seenTx != "tx-handle" {
		t.Fatalf("uow factory should receive tx handle, got %v", seenTx)
	}
	if len(repo.inserts) != 1 || agg.CurrentVersion() != 1 {
		t.Fatalf("aggregate should persist with new baseline, inserts=%d version=%d",
			len(repo.inserts), agg.CurrentVersion())
	}
}

func TestTxCommandExecutorRollsBackOnPersistFailure(t *testing.T) {
	tm := &countingTxManager{}
	repo := &fakeRepo{failOn: "insert"}
	exec := NewTxCommandExecutor(tm, PropagationRequired,
		func(any) domain.UnitOfWork { return NewSimpleUnitOfWork(nil) })

	_, err := exec.Execute(context.Background(), newAggregate("a1"), repo,
		func(*fakeAggregate) error { return nil })
	if !errors.Is(err, errBoom) {
		t.Fatalf("persist error should surface, got %v", err)
	}
	if tm.commits != 0 || tm.rollbacks != 1 {
		t.Fatalf("failed persist must roll back tx: %+v", tm)
	}
}
