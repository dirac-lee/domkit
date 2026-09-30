package domain

import "sync"

// BrokenRuleRegistry 业务规则消息注册表。
// 集中维护「错误码 -> 消息模板」，业务代码只声明/引用 MessageCode 常量，
// 聚合收集规则违反时按 Code 解析最新模板，避免消息散落在各校验分支。
// 并发安全：通常在包初始化期注册、运行期只读，内部仍加锁以支持热注册。
type BrokenRuleRegistry struct {
	mu    sync.RWMutex
	codes map[string]MessageCode
}

// NewBrokenRuleRegistry 创建注册表并注册初始消息码。
func NewBrokenRuleRegistry(codes ...MessageCode) *BrokenRuleRegistry {
	r := &BrokenRuleRegistry{codes: make(map[string]MessageCode, len(codes))}
	r.Register(codes...)
	return r
}

// Register 注册一组消息码，重复 Code 覆盖旧值。
func (r *BrokenRuleRegistry) Register(codes ...MessageCode) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, c := range codes {
		r.codes[c.Code] = c
	}
}

// Lookup 按错误码查找消息码。
func (r *BrokenRuleRegistry) Lookup(code string) (MessageCode, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	c, ok := r.codes[code]
	return c, ok
}

// Description 按错误码返回描述模板，未注册时返回空串。
func (r *BrokenRuleRegistry) Description(code string) string {
	if c, ok := r.Lookup(code); ok {
		return c.Description
	}
	return ""
}

// Size 返回已注册消息码数量。
func (r *BrokenRuleRegistry) Size() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.codes)
}
