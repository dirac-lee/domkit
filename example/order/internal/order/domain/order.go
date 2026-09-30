package domain

import (
	"github.com/dirac-lee/domkit/domain"
)

// OrderID 订单主键：字符串系强类型 ID，编译期防止与其他聚合主键串用。
type OrderID string

// Order 订单聚合根
type Order struct {
	domain.AggregateRoot[OrderID]
	OrderNo string
	Amount  int64
	Status  OrderStatus // 生命周期状态：富枚举，类型安全，不再是裸字符串
}

// CreateOrder 订单工厂方法：主键由调用方通过 IDGenerator 发放后显式传入
// （领域不绑定 UUID/号段等具体策略）；规则违反由聚合内聚收集，校验失败返回聚合规则错误。
func CreateOrder(id OrderID, orderNo string, amount int64) (*Order, error) {
	order := &Order{
		ID:      id,
		OrderNo: orderNo,
		Amount:  amount,
		Status:  StatusCreated,
	}
	order.SetRuleRegistry(OrderRuleRegistry)
	order.SetOperationRegistry(OrderOperationRegistry)

	// 聚合内聚校验：规则不通过时违反项已收集进聚合。
	if !order.CheckRule(&AmountPositiveRule{Amount: amount}) {
		return nil, order.AggregateRuleError()
	}

	// 记录成因操作 NEW，随后 CollectEvent 会把它回填到创建事件。
	if err := order.RecordOperation(domain.OperationNew); err != nil {
		return nil, err
	}
	ver := order.BeginCreate()
	order.CollectEvent(NewOrderCreatedEvent(order.ID, ver))
	return order, nil
}

// Pay 领域行为：支付订单。
func (o *Order) Pay() error {
	// 方式一：BusinessRule 对象 + 聚合收集器，非 created 状态规则不通过。
	if !o.CheckRule(&OrderCanPayRule{Order: o}) {
		return o.AggregateRuleError()
	}

	// 规则通过后记录成因操作 PAY，CollectEvent 时回填到支付事件。
	if err := o.RecordOperation(OperationPay); err != nil {
		return err
	}
	o.Status = StatusPaid
	ver := o.BeginModify()
	o.CollectEvent(NewOrderPaidEvent(o.ID, ver))
	return nil
}

// Cancel 领域行为：取消订单，演示参数化规则的内联收集方式（AddBrokenRuleWithParams）。
func (o *Order) Cancel() error {
	if o.Status == StatusPaid {
		o.AddBrokenRuleWithParams(RuleOrderCannotCancel, o.Status, StatusCreated)
		return o.FirstRuleError()
	}
	// 记录成因操作 CANCEL，CollectEvent 时回填到取消事件。
	if err := o.RecordOperation(OperationCancel); err != nil {
		return err
	}
	o.Status = StatusCancelled
	ver := o.BeginModify()
	o.CollectEvent(NewOrderCancelledEvent(o.ID, ver))
	return nil
}
