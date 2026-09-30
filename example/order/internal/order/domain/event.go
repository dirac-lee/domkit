package domain

import "github.com/dirac-lee/domkit/domain"

// OrderCreatedEvent 订单创建事件
type OrderCreatedEvent struct {
	domain.BaseDomainEvent[OrderID]
}

func NewOrderCreatedEvent(aggID OrderID, ver uint64) *OrderCreatedEvent {
	return &OrderCreatedEvent{
		BaseDomainEvent: domain.NewBaseDomainEvent(aggID, ver),
	}
}
func (o *OrderCreatedEvent) EventName() string { return "order.created" }

// OrderPaidEvent 订单支付事件
type OrderPaidEvent struct {
	domain.BaseDomainEvent[OrderID]
}

func NewOrderPaidEvent(aggID OrderID, ver uint64) *OrderPaidEvent {
	return &OrderPaidEvent{
		BaseDomainEvent: domain.NewBaseDomainEvent(aggID, ver),
	}
}
func (o *OrderPaidEvent) EventName() string { return "order.paid" }

// OrderCancelledEvent 订单取消事件
type OrderCancelledEvent struct {
	domain.BaseDomainEvent[OrderID]
}

func NewOrderCancelledEvent(aggID OrderID, ver uint64) *OrderCancelledEvent {
	return &OrderCancelledEvent{
		BaseDomainEvent: domain.NewBaseDomainEvent(aggID, ver),
	}
}
func (o *OrderCancelledEvent) EventName() string { return "order.cancelled" }
