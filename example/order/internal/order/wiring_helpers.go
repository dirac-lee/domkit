package order

import (
	"context"

	"github.com/dirac-lee/domkit/domain"
	orderdomain "github.com/dirac-lee/domkit/example/order/internal/order/domain"
	"github.com/dirac-lee/domkit/infra/eventbus"
	"github.com/dirac-lee/domkit/infra/outbox"
	"github.com/dirac-lee/domkit/infra/persist"
)

// eventBusPublisher 把进程内内存事件总线适配为 outbox.EventPublisher。
type eventBusPublisher struct {
	bus *eventbus.MemoryEventBus
}

// Publish 实现 outbox.EventPublisher。
func (p eventBusPublisher) Publish(_ context.Context, evt domain.DomainEvent) error {
	return p.bus.Publish(evt)
}

// newOrderEventSerializer 构造事件 JSON 序列化器并注册三类订单事件，
// 使 outbox 在提交后能把 payload 还原为具体事件，而不是裸 map。
func newOrderEventSerializer() *outbox.JsonEventSerializer {
	ser := outbox.NewJsonEventSerializer()
	ser.Register("order.created", func() domain.DomainEvent { return &orderdomain.OrderCreatedEvent{} })
	ser.Register("order.paid", func() domain.DomainEvent { return &orderdomain.OrderPaidEvent{} })
	ser.Register("order.cancelled", func() domain.DomainEvent { return &orderdomain.OrderCancelledEvent{} })
	return ser
}

// syncSummaryAndInvalidate 先投影读侧，成功后失效详情缓存（cache-aside：先更新读表后删缓存）。
func syncSummaryAndInvalidate(ctx context.Context, id orderdomain.OrderID,
	summary OrderSummaryStore, cache SummaryCache) error {
	if err := summary.Sync(ctx, id); err != nil {
		return err
	}
	return cache.Invalidate(ctx, id)
}

// newOrderIDGenerator 订单主键生成器：UUID 字符串，不依赖外部资源，真实/测试统一使用。
func newOrderIDGenerator() domain.IDGenerator[orderdomain.OrderID] {
	return persist.NewUUIDGenerator[orderdomain.OrderID]("order")
}
