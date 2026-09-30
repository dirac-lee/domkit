package eventbus

import "github.com/dirac-lee/domkit/domain"

// EventHandler 事件处理器。
type EventHandler interface {
	Handle(evt domain.DomainEvent) error
}

// EventBus 本地事件总线 SPI：
// Subscribe 按「事件名 + 订阅者名」登记处理器，priority 越大越先执行；
// Publish 把事件广播（broadcast）给该事件的全部订阅者。
//
// 广播语义说明：对应 Java 蓝本的 fan-out 投递；蓝本中订阅者依赖图
// （predecessor/successor 边与环检测）在 Go 版收敛为线性 priority 排序，
// 优先级即传播顺序，避免引入运行期依赖图机制。
type EventBus interface {
	Subscribe(eventName, subscriberName string, handler EventHandler, priority int)
	Publish(evt domain.DomainEvent) error
}

// SubscribeTo 类型安全的订阅便捷函数：只接收 E 类型事件，
// 非 E 类型（总线按事件名过滤，正常不会混入）被静默跳过。
//
//	eventbus.SubscribeTo(bus, "inventory", 10, func(e *OrderPaidEvent) error { ... })
func SubscribeTo[E domain.DomainEvent](bus EventBus, eventName, subscriberName string, priority int, handle func(E) error) {
	bus.Subscribe(eventName, subscriberName, FuncHandler(func(evt domain.DomainEvent) error {
		typed, ok := evt.(E)
		if !ok {
			return nil
		}
		return handle(typed)
	}), priority)
}
