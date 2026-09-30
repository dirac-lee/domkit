package domain

import (
	"errors"
	"fmt"
	"strings"
)

// DomainRuleError 领域业务规则校验失败错误，可携带一条或多条违反项。
type DomainRuleError struct {
	// Single 为 true 时表示仅取首条违反（对应单条异常），false 为聚合异常。
	Single      bool
	BrokenRules []BrokenRule
}

// NewDomainRuleError 创建包含全部违反项的聚合规则错误。
func NewDomainRuleError(rules []BrokenRule) *DomainRuleError {
	return &DomainRuleError{BrokenRules: rules}
}

// NewSingleDomainRuleError 创建仅含单条违反的规则错误。
func NewSingleDomainRuleError(rule BrokenRule) *DomainRuleError {
	return &DomainRuleError{Single: true, BrokenRules: []BrokenRule{rule}}
}

func (e *DomainRuleError) Error() string {
	var sb strings.Builder
	sb.WriteString("domain rule broken: ")
	for i, r := range e.BrokenRules {
		if i > 0 {
			sb.WriteString("; ")
		}
		sb.WriteString(fmt.Sprintf("[%s] %s", r.Code, r.Message))
	}
	return sb.String()
}

// IsDomainRuleError 判断错误链中是否包含领域规则错误。
func IsDomainRuleError(err error) bool {
	var de *DomainRuleError
	return errors.As(err, &de)
}

// ErrConcurrencyConflict 乐观锁版本冲突哨兵错误，
// 仓储 Update/Delete 发现持有的聚合版本已过期时返回（可用 errors.Is 判断）。
var ErrConcurrencyConflict = errors.New("aggregate version conflict")

// ConcurrencyConflictError 并发冲突详情，ID 为聚合主键类型。
// 擦除场景用 errors.Is(err, ErrConcurrencyConflict) 判断；
// 需要详情时按具体主键实例化类型断言，如 *ConcurrencyConflictError[OrderID]。
type ConcurrencyConflictError[ID comparable] struct {
	// AggregateID 冲突的聚合主键。
	AggregateID ID
	// Expected 调用方持有的版本（更新条件）。
	Expected uint64
	// Actual 存储中当前的版本；聚合不存在时为 0。
	Actual uint64
	// AggregateMissing 为 true 表示聚合在存储中已不存在（ORPHAN 场景）。
	AggregateMissing bool
}

// NewConcurrencyConflictError 创建并发冲突错误。
func NewConcurrencyConflictError[ID comparable](id ID, expected, actual uint64, missing bool) *ConcurrencyConflictError[ID] {
	return &ConcurrencyConflictError[ID]{
		AggregateID:      id,
		Expected:         expected,
		Actual:           actual,
		AggregateMissing: missing,
	}
}

func (e *ConcurrencyConflictError[ID]) Error() string {
	if e.AggregateMissing {
		return fmt.Sprintf("aggregate %v no longer exists (expected version %d)", e.AggregateID, e.Expected)
	}
	return fmt.Sprintf("aggregate %v version conflict: expected %d, actual %d",
		e.AggregateID, e.Expected, e.Actual)
}

// Unwrap 支持 errors.Is(err, ErrConcurrencyConflict)。
func (e *ConcurrencyConflictError[ID]) Unwrap() error { return ErrConcurrencyConflict }

// IsConcurrencyConflict 判断错误链中是否包含乐观锁冲突。
func IsConcurrencyConflict(err error) bool {
	return errors.Is(err, ErrConcurrencyConflict)
}
