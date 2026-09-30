package application

import (
	"context"
	"errors"
	"testing"

	"github.com/dirac-lee/domkit/domain"
)

// 本地规则夹具：执行器测试专用。
type execFailRule struct {
	code string
}

func (execFailRule) IsSatisfied() bool { return false }
func (r execFailRule) BrokenRule() *domain.BrokenRule {
	return &domain.BrokenRule{Code: r.code, Message: "规则不通过"}
}

func newExecExecutor() (*CommandExecutor, *fakeRepo, *[]string) {
	repo := &fakeRepo{}
	var dispatched []string
	exec := NewCommandExecutor(func(_ context.Context, evt domain.DomainEvent) error {
		dispatched = append(dispatched, evt.EventName())
		return nil
	})
	return exec, repo, &dispatched
}

func TestExecuteNewAggregateInsertsAndDispatches(t *testing.T) {
	exec, repo, dispatched := newExecExecutor()
	agg := newAggregate("a1")

	got, err := exec.Execute(context.Background(), agg, repo, func(a *fakeAggregate) error {
		a.NextVersion()
		a.CollectEvent(newEvent("a1"))
		return nil
	})
	if err != nil {
		t.Fatalf("execute failed: %v", err)
	}
	if got != agg {
		t.Fatal("executor should return the same aggregate instance")
	}
	if len(repo.inserts) != 1 || len(repo.updates) != 0 {
		t.Fatalf("new aggregate must be inserted, not updated: %+v", repo)
	}
	if len(*dispatched) != 1 || (*dispatched)[0] != "fake.event" {
		t.Fatalf("event should be dispatched after persist, got %v", *dispatched)
	}
	if agg.CurrentVersion() != 1 {
		t.Fatalf("version baseline should advance to 1, got %d", agg.CurrentVersion())
	}
	// 事件在提交时已被取走。
	if len(agg.PullEvents()) != 0 {
		t.Fatal("events must be drained after commit")
	}
}

func TestExecuteExistingAggregateUpdates(t *testing.T) {
	exec, repo, _ := newExecExecutor()
	agg := newAggregate("a1")
	agg.MarkPersisted(3) // 仓储装载：基线 3
	agg.NextVersion()    // 待提交 4

	if _, err := exec.Execute(context.Background(), agg, repo, func(*fakeAggregate) error { return nil }); err != nil {
		t.Fatalf("execute failed: %v", err)
	}
	if len(repo.updates) != 1 || len(repo.inserts) != 0 {
		t.Fatalf("existing aggregate must be updated: inserts=%d updates=%d", len(repo.inserts), len(repo.updates))
	}
	if agg.CurrentVersion() != 4 {
		t.Fatalf("baseline should advance to 4, got %d", agg.CurrentVersion())
	}
}

func TestExecuteLogicRuleErrorHasNoSideEffects(t *testing.T) {
	exec, repo, dispatched := newExecExecutor()
	agg := newAggregate("a1")
	agg.CollectEvent(newEvent("a1")) // 逻辑失败前暂存了事件

	ruleErr := domain.NewDomainRuleError([]domain.BrokenRule{{Code: "LOGIC_X", Message: "逻辑内规则失败"}})
	_, err := exec.Execute(context.Background(), agg, repo, func(*fakeAggregate) error {
		return ruleErr
	})
	if !domain.IsDomainRuleError(err) {
		t.Fatalf("expected domain rule error, got %v", err)
	}
	if len(repo.inserts)+len(repo.updates) != 0 {
		t.Fatal("rule error must not trigger persistence")
	}
	if len(*dispatched) != 0 {
		t.Fatal("rule error must not dispatch events")
	}
	if len(agg.PullEvents()) != 0 {
		t.Fatal("staged events must be drained after rule failure")
	}
}

func TestExecuteRuleOptionFailureHasNoSideEffects(t *testing.T) {
	exec, repo, dispatched := newExecExecutor()
	agg := newAggregate("a1")
	agg.NextVersion()
	agg.CollectEvent(newEvent("a1"))

	_, err := exec.Execute(context.Background(), agg, repo,
		func(*fakeAggregate) error { return nil },
		execFailRule{code: "OPT_X"},
	)
	if !domain.IsDomainRuleError(err) {
		t.Fatalf("expected aggregate rule error, got %v", err)
	}
	if len(repo.inserts)+len(repo.updates) != 0 {
		t.Fatal("failed rule must not trigger persistence")
	}
	if len(*dispatched) != 0 {
		t.Fatal("failed rule must not dispatch events")
	}
	if rules := agg.BrokenRules(); len(rules) != 1 || rules[0].Code != "OPT_X" {
		t.Fatalf("rule should be collected on aggregate: %+v", rules)
	}
}

func TestExecutePersistFailureSurfacesError(t *testing.T) {
	repo := &fakeRepo{failOn: "insert"}
	exec := NewCommandExecutor(nil)
	_, err := exec.Execute(context.Background(), newAggregate("a1"), repo,
		func(a *fakeAggregate) error { return nil })
	if !errors.Is(err, errBoom) {
		t.Fatalf("expected persist error surfaced, got %v", err)
	}
}

// ---- Dry-run ----

func TestTryExecutePassDrainsEventsWithoutPersistence(t *testing.T) {
	exec, repo, _ := newExecExecutor()
	agg := newAggregate("a1")

	res, err := exec.TryExecute(context.Background(), agg, repo, func(a *fakeAggregate) error {
		a.NextVersion()
		a.CollectEvent(newEvent("a1"))
		return nil
	})
	if err != nil || !res.Passed {
		t.Fatalf("expected pass result, got %+v err=%v", res, err)
	}
	if len(repo.inserts)+len(repo.updates) != 0 {
		t.Fatal("dry-run must not persist")
	}
	if len(agg.PullEvents()) != 0 {
		t.Fatal("dry-run must drain staged events")
	}
	// 试跑不推进基线（版本瞬态保留在一次性实例上，实例不应再用于真实执行）。
	if agg.CurrentVersion() != 0 {
		t.Fatalf("dry-run must not advance committed baseline, got %d", agg.CurrentVersion())
	}
}

func TestTryExecuteRejectByLogicRuleError(t *testing.T) {
	exec, repo, _ := newExecExecutor()
	agg := newAggregate("a1")

	res, err := exec.TryExecute(context.Background(), agg, repo, func(*fakeAggregate) error {
		return domain.NewDomainRuleError([]domain.BrokenRule{{Code: "LOGIC_X", Message: "失败"}})
	})
	if err != nil {
		t.Fatalf("rule error should become reject result, got %v", err)
	}
	if res.Passed || len(res.BrokenRules) != 1 || res.RuleCodes()[0] != "LOGIC_X" {
		t.Fatalf("unexpected reject result: %+v", res)
	}
}

func TestTryExecuteRejectByCollectedRules(t *testing.T) {
	exec, repo, _ := newExecExecutor()
	agg := newAggregate("a1")

	res, err := exec.TryExecute(context.Background(), agg, repo,
		func(*fakeAggregate) error { return nil },
		execFailRule{code: "R1"},
	)
	if err != nil || res.Passed {
		t.Fatalf("expected reject, got %+v err=%v", res, err)
	}
	if codes := res.RuleCodes(); len(codes) != 1 || codes[0] != "R1" {
		t.Fatalf("unexpected codes: %v", codes)
	}
}

func TestTryExecuteNonRuleErrorPropagates(t *testing.T) {
	exec, repo, _ := newExecExecutor()
	res, err := exec.TryExecute(context.Background(), newAggregate("a1"), repo,
		func(*fakeAggregate) error { return errBoom })
	if !errors.Is(err, errBoom) {
		t.Fatalf("non-rule error must propagate, got %v", err)
	}
	if res.Passed || len(res.BrokenRules) != 0 {
		t.Fatalf("error path should return zero result, got %+v", res)
	}
}

// ---- 异构主键：同一个非泛型 CommandExecutor 实例同时服务 int64 主键聚合 ----

type int64Aggregate struct {
	domain.AggregateRoot[int64]
	Note string
}

type int64Repo struct{ inserted bool }

func (r *int64Repo) Insert(_ context.Context, _ *int64Aggregate) error {
	r.inserted = true
	return nil
}
func (r *int64Repo) Update(context.Context, *int64Aggregate) error { return nil }
func (r *int64Repo) Delete(context.Context, *int64Aggregate) error { return nil }
func (r *int64Repo) GetByID(_ context.Context, id int64) (*int64Aggregate, error) {
	return &int64Aggregate{AggregateRoot: domain.AggregateRoot[int64]{ID: id}}, nil
}
func (r *int64Repo) CurrentVersion(context.Context, int64) (uint64, bool, error) {
	return 0, true, nil
}

var _ domain.Repository[int64, int64Aggregate] = (*int64Repo)(nil)

func TestExecuteServesInt64IDAggregate(t *testing.T) {
	exec, _, _ := newExecExecutor() // 与 string 主键用例共享同一个执行器实例
	repo := &int64Repo{}
	agg := &int64Aggregate{AggregateRoot: domain.AggregateRoot[int64]{ID: 42}, Note: "seq"}

	got, err := exec.Execute(context.Background(), agg, repo, func(a *int64Aggregate) error {
		a.NextVersion()
		return nil
	})
	if err != nil {
		t.Fatalf("execute int64 aggregate failed: %v", err)
	}
	if !repo.inserted {
		t.Fatal("int64 aggregate should be inserted")
	}
	if got.ID != 42 || got.CurrentVersion() != 1 {
		t.Fatalf("int64 aggregate state mismatch: id=%d baseline=%d", got.ID, got.CurrentVersion())
	}
}
