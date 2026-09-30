package eventbus

import (
	"time"

	"github.com/dirac-lee/domkit/domain"
)

// MemoryEventBus 同步内存事件总线：Publish 在调用方 goroutine 内
// 按 priority 顺序依次投递，任一处理器失败即中断并返回错误。
// 单体服务或需要「提交后立即同步处理」语义的场景使用。
type MemoryEventBus struct {
	reg     *registry
	metrics Metrics
}

// NewMemoryEventBus 创建同步内存事件总线。
func NewMemoryEventBus() *MemoryEventBus {
	return &MemoryEventBus{reg: newRegistry(), metrics: NopMetrics{}}
}

// SetMetrics 注入监控实现（非线程安全语义：在装配期、发布前调用一次）。
func (m *MemoryEventBus) SetMetrics(metrics Metrics) {
	if metrics != nil {
		m.metrics = metrics
	}
}

func (m *MemoryEventBus) Subscribe(eventName, subscriberName string, handler EventHandler, priority int) {
	m.reg.Subscribe(eventName, subscriberName, handler, priority)
}

func (m *MemoryEventBus) Publish(evt domain.DomainEvent) error {
	items := m.reg.snapshot(evt.EventName())
	if len(items) == 0 {
		return nil
	}
	for _, item := range items {
		start := time.Now()
		err := item.handler.Handle(evt)
		m.metrics.Consumed(evt.EventName(), item.name, err == nil, 1)
		if err != nil {
			m.metrics.Published(evt.EventName(), false, time.Since(start).Milliseconds())
			return err
		}
	}
	m.metrics.Published(evt.EventName(), true, 0)
	return nil
}
