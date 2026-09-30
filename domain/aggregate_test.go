package domain

import (
	"errors"
	"strings"
	"testing"
	"time"
)

// ---- 规则注册表与聚合收集器 ----

var (
	codeSimple = NewMessageCode("SIMPLE", "简单规则描述")
	codeParam  = NewMessageCode("PARAM", "参数规则：%s / %d")
)

func testRegistry() *BrokenRuleRegistry {
	return NewBrokenRuleRegistry(codeSimple, codeParam)
}

type passRule struct{}

func (passRule) IsSatisfied() bool       { return true }
func (passRule) BrokenRule() *BrokenRule { return nil }

type failRule struct {
	code string
	msg  string
}

func (failRule) IsSatisfied() bool { return false }
func (r failRule) BrokenRule() *BrokenRule {
	return &BrokenRule{Code: r.code, Message: r.msg}
}

func TestRegistryLookupAndDescription(t *testing.T) {
	r := testRegistry()
	if r.Size() != 2 {
		t.Fatalf("expected 2 codes, got %d", r.Size())
	}
	if got := r.Description("SIMPLE"); got != "简单规则描述" {
		t.Fatalf("description mismatch: %q", got)
	}
	if _, ok := r.Lookup("MISSING"); ok {
		t.Fatal("unregistered code must not be found")
	}
	if got := r.Description("MISSING"); got != "" {
		t.Fatalf("missing code description should be empty, got %q", got)
	}
}

func TestAggregateCollectsBrokenRulesFromRegistry(t *testing.T) {
	var ar AggregateRoot[string]
	ar.SetRuleRegistry(testRegistry())

	ar.AddBrokenRule(codeSimple)
	if !ar.HasBrokenRules() || len(ar.BrokenRules()) != 1 {
		t.Fatal("expected one broken rule")
	}
	got := ar.BrokenRules()[0]
	if got.Code != "SIMPLE" || got.Message != "简单规则描述" {
		t.Fatalf("rule should resolve description from registry, got %+v", got)
	}
}

func TestAggregateParamBrokenRuleFormatting(t *testing.T) {
	var ar AggregateRoot[string]
	ar.SetRuleRegistry(testRegistry())
	ar.AddBrokenRuleWithParams(codeParam, "hello", 42)

	br := ar.BrokenRules()[0]
	if br.Message != "参数规则：hello / 42" {
		t.Fatalf("param formatting mismatch: %q", br.Message)
	}
	if len(br.Params) != 2 {
		t.Fatalf("params should be retained, got %v", br.Params)
	}

	err := ar.FirstRuleError()
	var dre *DomainRuleError
	if !errors.As(err, &dre) || !dre.Single {
		t.Fatalf("FirstRuleError should produce single DomainRuleError, got %#v", err)
	}
	if !strings.Contains(err.Error(), "[PARAM]") {
		t.Fatalf("error string should contain code and message: %v", err)
	}
}

func TestAggregateCheckRuleCollectsAndClears(t *testing.T) {
	var ar AggregateRoot[string]
	ar.SetRuleRegistry(testRegistry())

	if !ar.CheckRule(passRule{}) {
		t.Fatal("pass rule should return true")
	}
	if ar.HasBrokenRules() {
		t.Fatal("pass rule must not be collected")
	}
	if ar.CheckRule(nil) != true {
		t.Fatal("nil rule should be treated as satisfied")
	}

	if ar.CheckRule(failRule{code: "X", msg: "失败"}) {
		t.Fatal("fail rule should return false")
	}
	if ar.CheckRule(failRule{code: "Y", msg: "又失败"}) {
		t.Fatal("second failed rule should also return false")
	}
	if len(ar.BrokenRules()) != 2 {
		t.Fatalf("expected 2 collected rules")
	}
	err := ar.AggregateRuleError()
	var dre *DomainRuleError
	if !errors.As(err, &dre) || dre.Single {
		t.Fatalf("AggregateRuleError should be non-single, got %#v", err)
	}

	ar.ClearBrokenRules()
	if ar.HasBrokenRules() {
		t.Fatal("rules should be cleared")
	}
	if ar.AggregateRuleError() != nil || ar.FirstRuleError() != nil {
		t.Fatal("no error expected after clear")
	}
}

func TestAggregateFallsBackToCodeDescriptionWithoutRegistry(t *testing.T) {
	var ar AggregateRoot[string] // 不注入注册表
	ar.AddBrokenRule(NewMessageCode("RAW", "码自带描述"))
	if got := ar.BrokenRules()[0].Message; got != "码自带描述" {
		t.Fatalf("expected fallback to code description, got %q", got)
	}
}

// ---- 幂等版本 ----

func TestAggregateRootMarkTimestamps(t *testing.T) {
	var ar AggregateRoot[string]
	ar.MarkCreated()
	if ar.CreatedAt.IsZero() || ar.UpdatedAt.IsZero() {
		t.Fatal("MarkCreated should initialize both audit timestamps")
	}
	if !ar.CreatedAt.Equal(ar.UpdatedAt) {
		t.Fatal("MarkCreated should use the same timestamp for creation and update")
	}

	createdAt := time.Now().Add(-time.Second)
	ar.CreatedAt = createdAt
	ar.UpdatedAt = createdAt
	ar.MarkModified()
	if !ar.CreatedAt.Equal(createdAt) {
		t.Fatal("MarkModified must not change CreatedAt")
	}
	if !ar.UpdatedAt.After(createdAt) {
		t.Fatal("MarkModified should refresh UpdatedAt")
	}
}

func TestAggregateRootBeginChangeMarksAuditTimeAndVersion(t *testing.T) {
	var created AggregateRoot[string]
	if got := created.BeginCreate(); got != 1 {
		t.Fatalf("BeginCreate version = %d, want 1", got)
	}
	if created.CreatedAt.IsZero() || created.UpdatedAt.IsZero() {
		t.Fatal("BeginCreate should initialize audit timestamps")
	}
	if created.PendingVersion() != 1 {
		t.Fatalf("BeginCreate pending version = %d, want 1", created.PendingVersion())
	}

	modified := AggregateRoot[string]{Version: 3, CreatedAt: time.Now().Add(-time.Hour)}
	createdAt := modified.CreatedAt
	if got := modified.BeginModify(); got != 4 {
		t.Fatalf("BeginModify version = %d, want 4", got)
	}
	if !modified.CreatedAt.Equal(createdAt) {
		t.Fatal("BeginModify must not change CreatedAt")
	}
	if !modified.UpdatedAt.After(createdAt) {
		t.Fatal("BeginModify should refresh UpdatedAt")
	}
}

func TestNextVersionIsIdempotentWithinOperation(t *testing.T) {
	var ar AggregateRoot[string] // 基线 0
	if ar.CurrentVersion() != 0 {
		t.Fatal("new aggregate baseline should be 0")
	}
	first := ar.NextVersion()
	if first != 1 {
		t.Fatalf("first NextVersion should be 1, got %d", first)
	}
	for range 3 {
		if ar.NextVersion() != 1 {
			t.Fatal("repeated NextVersion within one operation must return 1")
		}
	}
	// 提交前基线不能被提前推进。
	if ar.CurrentVersion() != 0 || ar.PendingVersion() != 1 {
		t.Fatalf("baseline must stay 0 before commit, got baseline=%d pending=%d",
			ar.CurrentVersion(), ar.PendingVersion())
	}

	ar.CommitVersion()
	if ar.CurrentVersion() != 1 {
		t.Fatalf("baseline should advance to 1 after commit, got %d", ar.CurrentVersion())
	}

	// 下一次业务操作：新版本 2。
	if ar.NextVersion() != 2 {
		t.Fatalf("next operation version should be 2, got %d", ar.NextVersion())
	}
	ar.CommitVersion()
	if ar.CurrentVersion() != 2 {
		t.Fatalf("baseline should be 2, got %d", ar.CurrentVersion())
	}
}

func TestCommitVersionWithoutGeneratedVersionIsNoop(t *testing.T) {
	ar := AggregateRoot[string]{Version: 5}
	ar.CommitVersion()
	if ar.CurrentVersion() != 5 {
		t.Fatal("CommitVersion must be no-op when NextVersion was never called")
	}
}

func TestCommitVersionDoesNotTouchAuditTime(t *testing.T) {
	ar := AggregateRoot[string]{UpdatedAt: time.Now()}
	updatedAt := ar.UpdatedAt
	ar.NextVersion()
	ar.CommitVersion()
	if !ar.UpdatedAt.Equal(updatedAt) {
		t.Fatal("CommitVersion should not update audit timestamps")
	}
}

func TestIsNewSemantics(t *testing.T) {
	var ar AggregateRoot[string]
	if !ar.IsNew() {
		t.Fatal("version 0 should be treated as new")
	}
	ar.NextVersion()
	ar.CommitVersion()
	if ar.IsNew() {
		t.Fatal("committed aggregate should not be new")
	}
}

// ---- 乐观锁版本检查 ----

func TestCheckVersion(t *testing.T) {
	if err := CheckVersion("a1", 3, 3, true); err != nil {
		t.Fatalf("matching version should pass, got %v", err)
	}

	err := CheckVersion("a1", 3, 4, true)
	if !IsConcurrencyConflict(err) {
		t.Fatalf("version mismatch should be conflict, got %v", err)
	}
	var ce *ConcurrencyConflictError[string]
	if !errors.As(err, &ce) || ce.AggregateMissing || ce.Expected != 3 || ce.Actual != 4 {
		t.Fatalf("conflict detail mismatch: %+v", ce)
	}

	errMissing := CheckVersion("a1", 3, 0, false)
	var ce2 *ConcurrencyConflictError[string]
	if !errors.As(errMissing, &ce2) || !ce2.AggregateMissing {
		t.Fatalf("missing aggregate should be AggregateMissing conflict, got %v", errMissing)
	}

	// 哨兵可被 wrap 后识别；字符串拼接不算 wrap。
	if IsConcurrencyConflict(errors.New("repo: " + errMissing.Error())) {
		t.Fatal("plain non-wrap string error should not match sentinel")
	}
	if !IsConcurrencyConflict(errors.Join(errMissing)) {
		t.Fatal("wrapped conflict should be detectable")
	}
}

func TestDomainRuleErrorIsDetectableWhenWrapped(t *testing.T) {
	var ar AggregateRoot[string]
	ar.AddBrokenRule(codeSimple)
	err := ar.FirstRuleError()
	// 字符串拼接不是真正的 wrap，识别应失败。
	if IsDomainRuleError(errors.New("wrap: " + err.Error())) {
		t.Fatal("plain string-wrapped error must not be detected as domain rule error")
	}
	// errors.Join 是真正的 wrap，errors.As 应能识别。
	if !IsDomainRuleError(errors.Join(err)) {
		t.Fatal("errors.Join-wrapped DomainRuleError should be detectable")
	}
}
