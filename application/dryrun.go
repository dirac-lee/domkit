package application

import "github.com/dirac-lee/domkit/domain"

// DryRunResult 命令试跑结果：
// Passed=true 表示通过全部业务校验；false 时 BrokenRules 携带违反明细。
// 试跑不产生任何持久化与事件副作用。
type DryRunResult struct {
	Passed      bool
	BrokenRules []domain.BrokenRule
}

// DryRunPass 构造校验通过的试跑结果。
func DryRunPass() DryRunResult {
	return DryRunResult{Passed: true}
}

// DryRunReject 构造校验未通过的试跑结果。
func DryRunReject(rules []domain.BrokenRule) DryRunResult {
	cp := make([]domain.BrokenRule, len(rules))
	copy(cp, rules)
	return DryRunResult{Passed: false, BrokenRules: cp}
}

// RuleCodes 返回全部违反项的错误码，便于上层做分支判断/埋点。
func (r DryRunResult) RuleCodes() []string {
	codes := make([]string, 0, len(r.BrokenRules))
	for _, br := range r.BrokenRules {
		codes = append(codes, br.Code)
	}
	return codes
}
