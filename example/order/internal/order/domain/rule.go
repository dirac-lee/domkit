package domain

import (
	"fmt"

	"github.com/dirac-lee/domkit/domain"
)

// 订单聚合的规则消息码（错误码 + 消息模板）。
var (
	// RuleAmountInvalid 金额非法，%d 为实际金额。
	RuleAmountInvalid = domain.NewMessageCode(
		"ORDER_AMOUNT_INVALID",
		"订单金额必须大于 0，当前金额：%d",
	)
	// RuleOrderCannotPay 状态不允许支付，参数依次为当前状态、要求状态。
	RuleOrderCannotPay = domain.NewMessageCode(
		"ORDER_STATUS_CANNOT_PAY",
		"订单当前状态为 %s，仅 %s 状态可以支付",
	)
	// RuleOrderCannotCancel 状态不允许取消，参数依次为当前状态、要求状态。
	// 独立成模板，避免取消场景复用支付模板，错误文案出现"可以支付"。
	RuleOrderCannotCancel = domain.NewMessageCode(
		"ORDER_STATUS_CANNOT_CANCEL",
		"订单当前状态为 %s，仅 %s 状态可以取消",
	)
)

// OrderRuleRegistry 订单聚合规则注册表，工厂方法中注入聚合。
var OrderRuleRegistry = domain.NewBrokenRuleRegistry(
	RuleAmountInvalid,
	RuleOrderCannotPay,
	RuleOrderCannotCancel,
)

// AmountPositiveRule 金额必须大于 0（BusinessRule 对象式校验，
// 违反内容同样取自消息码模板，保证错误码与文案集中维护）。
type AmountPositiveRule struct {
	Amount int64
}

func (a *AmountPositiveRule) IsSatisfied() bool {
	return a.Amount > 0
}

func (a *AmountPositiveRule) BrokenRule() *domain.BrokenRule {
	return &domain.BrokenRule{
		Code:    RuleAmountInvalid.Code,
		Message: fmt.Sprintf(RuleAmountInvalid.Description, a.Amount),
		Params:  []any{a.Amount},
	}
}

// OrderCanPayRule 订单可支付校验：仅 created 状态可支付。
type OrderCanPayRule struct {
	Order *Order
}

func (r *OrderCanPayRule) IsSatisfied() bool {
	return r.Order.Status == StatusCreated
}

func (r *OrderCanPayRule) BrokenRule() *domain.BrokenRule {
	// 取枚举编码填入文案，保证对外仍是 created/paid 等可读字符串。
	current, required := r.Order.Status.Value(), StatusCreated.Value()
	return &domain.BrokenRule{
		Code:    RuleOrderCannotPay.Code,
		Message: fmt.Sprintf(RuleOrderCannotPay.Description, current, required),
		Params:  []any{current, required},
	}
}
