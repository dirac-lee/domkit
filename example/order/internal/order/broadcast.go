package order

import (
	"context"
	"time"

	"github.com/dirac-lee/domkit/broadcast"
	"github.com/dirac-lee/domkit/domain"
	"github.com/dirac-lee/domkit/example/order/internal/order/app"
	orderdomain "github.com/dirac-lee/domkit/example/order/internal/order/domain"
)

// OrderPaidNotice 订单已支付对外消息体，承载对接方（履约 / 财务）关心的字段。
type OrderPaidNotice struct {
	OrderNo string `json:"orderNo"`
	Amount  int64  `json:"amount"`
	PaidAt  string `json:"paidAt"` // RFC3339 支付时刻
}

// newOrderPaidSubscriber 构造订阅 order.paid 的对外广播订阅者。
// 领域事件本身不带业务字段，BuildPayload 内从写模型加载订单再组装消息体——
// 这是真实订阅者查询数据的常见做法，避免把对外字段塞进领域事件。
func newOrderPaidSubscriber(
	messengers broadcast.Messenger,
	repo domain.Repository[orderdomain.OrderID, orderdomain.Order],
) *broadcast.Subscriber[OrderPaidNotice] {
	return &broadcast.Subscriber[OrderPaidNotice]{
		Messenger:     messengers,
		Serializer:    broadcast.NewJSONSerializer(),
		AggregateType: "Order",
		Topic:         "order-paid",
		SenderCode:    "order-service",
		BuildPayload: func(evt domain.DomainEvent) (OrderPaidNotice, error) {
			return buildOrderPaidNotice(evt, repo)
		},
	}
}

// buildOrderPaidNotice 按事件聚合 ID 加载订单并组装支付通知；
// 订单缺失复用应用层的 ErrOrderNotFound，避免重复定义同义错误。
func buildOrderPaidNotice(
	evt domain.DomainEvent,
	repo domain.Repository[orderdomain.OrderID, orderdomain.Order],
) (OrderPaidNotice, error) {
	id := orderdomain.OrderID(evt.AggregateKey())
	o, err := repo.GetByID(context.Background(), id)
	if err != nil {
		return OrderPaidNotice{}, err
	}
	if o == nil {
		return OrderPaidNotice{}, app.ErrOrderNotFound
	}
	// 支付时刻以「支付事件发生时间」为准（事件即支付事实），不读聚合 UpdatedAt：
	// UpdatedAt 表示聚合审计时间，而支付事件时间才是对外广播需要表达的业务事实时间。
	return OrderPaidNotice{
		OrderNo: o.OrderNo,
		Amount:  o.Amount,
		PaidAt:  evt.OccurredAt().Format(time.RFC3339),
	}, nil
}
