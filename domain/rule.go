package domain

// BrokenRule 单条业务规则违反（值对象）。
type BrokenRule struct {
	// Code 规则错误码，对应 MessageCode.Code。
	Code string
	// Message 渲染后的人类可读描述。
	Message string
	// Params 渲染参数（可选），供日志/国际化等场景还原模板变量。
	Params []any
}

// BusinessRule 业务规则接口：由领域层的规则对象实现，
// 可交给聚合根 CheckRule 或独立 RuleValidator 执行。
type BusinessRule interface {
	// IsSatisfied 返回 true 表示规则通过。
	IsSatisfied() bool
	// BrokenRule 返回规则违反详情。
	BrokenRule() *BrokenRule
}

// RuleValidator 独立规则校验器，适用于聚合构造前等聚合收集器不可用的场景。
type RuleValidator struct {
	rules []BusinessRule
}

// NewRuleValidator 创建规则校验器。
func NewRuleValidator(rules ...BusinessRule) *RuleValidator {
	return &RuleValidator{rules: rules}
}

// Validate 依次校验规则：
// failFast=true 时遇到第一个失败立即返回；false 时收集全部违反后统一返回。
func (v *RuleValidator) Validate(failFast bool) error {
	var brokenList []BrokenRule
	for _, r := range v.rules {
		if !r.IsSatisfied() {
			brokenList = append(brokenList, *r.BrokenRule())
			if failFast {
				return NewDomainRuleError(brokenList)
			}
		}
	}
	if len(brokenList) > 0 {
		return NewDomainRuleError(brokenList)
	}
	return nil
}
