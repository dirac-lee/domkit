package eventbus

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"time"

	"github.com/dirac-lee/domkit/domain"
)

// ErrBusClosed 总线关闭后再发布事件。
var ErrBusClosed = errors.New("eventbus: bus already closed")

// AsyncConfig 异步事件总线配置（对标 Java LocalEventManagerConfig）。
type AsyncConfig struct {
	// Workers 投递 worker 数量，默认 1。
	Workers int
	// QueueSize 待投递任务的有界队列容量，默认 1024；
	// 队列满时 Publish 阻塞等待（背压），对标 ThreadPoolExecutor 的 CallerRunsPolicy。
	QueueSize int
	// MaxRetry 处理器失败后的最大重试次数（不含首次），0 表示不重试。
	MaxRetry int
	// RetryDelay 重试间隔。
	RetryDelay time.Duration
	// Metrics 监控钩子，nil 时使用 NopMetrics。
	Metrics Metrics
	// OnDeadLetter 重试耗尽后的回调（告警/补偿入口），可为 nil。
	OnDeadLetter func(eventName, subscriber string, err error)
}

// delivery 一次「事件 × 订阅者」的投递任务。
type delivery struct {
	evt     domain.DomainEvent
	item    subItem
	attempt int // 已尝试次数（含正在执行的一次），从 1 开始
}

// AsyncEventBus 异步内存事件总线（对标 Java ThreadPoolEventManager）：
// Publish 仅把每个订阅者的投递任务放入有界队列后立即返回；worker 池并发消费，
// 失败按固定间隔延时重试，耗尽后记录死信监控与回调，不影响其他订阅者。
type AsyncEventBus struct {
	reg     *registry
	cfg     AsyncConfig
	metrics Metrics

	queue  chan *delivery
	stop   chan struct{}
	wg     sync.WaitGroup
	closed atomic.Bool
}

// NewAsyncEventBus 创建异步总线并启动 worker；Shutdown 前必须调用以释放资源。
func NewAsyncEventBus(cfg AsyncConfig) *AsyncEventBus {
	if cfg.Workers <= 0 {
		cfg.Workers = 1
	}
	if cfg.QueueSize <= 0 {
		cfg.QueueSize = 1024
	}
	if cfg.Metrics == nil {
		cfg.Metrics = NopMetrics{}
	}
	bus := &AsyncEventBus{
		reg:     newRegistry(),
		cfg:     cfg,
		metrics: cfg.Metrics,
		queue:   make(chan *delivery, cfg.QueueSize),
		stop:    make(chan struct{}),
	}
	bus.wg.Add(cfg.Workers)
	for i := 0; i < cfg.Workers; i++ {
		go bus.worker()
	}
	return bus
}

func (b *AsyncEventBus) Subscribe(eventName, subscriberName string, handler EventHandler, priority int) {
	b.reg.Subscribe(eventName, subscriberName, handler, priority)
}

// Publish 把事件广播给全部订阅者：每个订阅者一个独立投递任务。
// 队列满时阻塞形成背压；总线已关闭返回 ErrBusClosed。
func (b *AsyncEventBus) Publish(evt domain.DomainEvent) error {
	items := b.reg.snapshot(evt.EventName())
	if len(items) == 0 {
		return nil
	}
	if b.closed.Load() {
		return ErrBusClosed
	}
	for _, item := range items {
		task := &delivery{evt: evt, item: item, attempt: 1}
		// 有界队列满即阻塞形成背压；总线关闭时立即解除并报错，避免生产者永久挂起。
		select {
		case b.queue <- task:
		case <-b.stop:
			return ErrBusClosed
		}
	}
	b.metrics.Published(evt.EventName(), true, 0)
	return nil
}

// QueueLen 返回当前待投递任务数（运维/测试观察用）。
func (b *AsyncEventBus) QueueLen() int { return len(b.queue) }

func (b *AsyncEventBus) worker() {
	defer b.wg.Done()
	for {
		select {
		case <-b.stop:
			return
		case task := <-b.queue:
			b.runWithRetry(task)
		}
	}
}

// runWithRetry 同步执行一次投递及全部重试（延时期间 worker 被占用，
// 本地总线场景可接受；关闭总线会中断等待，任务不再重试）。
func (b *AsyncEventBus) runWithRetry(task *delivery) {
	err := task.item.handler.Handle(task.evt)
	if err == nil {
		b.metrics.Consumed(task.evt.EventName(), task.item.name, true, task.attempt)
		return
	}
	for task.attempt <= b.cfg.MaxRetry {
		if !b.waitRetry() {
			// 总线关闭：放弃投递，不记死信（进程退出语义，由 outbox 等机制兜底）。
			return
		}
		task.attempt++
		err = task.item.handler.Handle(task.evt)
		if err == nil {
			b.metrics.Consumed(task.evt.EventName(), task.item.name, true, task.attempt)
			return
		}
	}
	b.metrics.Consumed(task.evt.EventName(), task.item.name, false, task.attempt)
	b.metrics.DeadLettered(task.evt.EventName(), task.item.name, err.Error())
	if b.cfg.OnDeadLetter != nil {
		b.cfg.OnDeadLetter(task.evt.EventName(), task.item.name, err)
	}
}

// waitRetry 等待重试间隔；总线关闭时立即返回 false。
func (b *AsyncEventBus) waitRetry() bool {
	if b.cfg.RetryDelay <= 0 {
		return !b.closed.Load()
	}
	timer := time.NewTimer(b.cfg.RetryDelay)
	defer timer.Stop()
	select {
	case <-timer.C:
		return true
	case <-b.stop:
		return false
	}
}

// Shutdown 停止接收新任务（此后 Publish 返回 ErrBusClosed）：
//  1. 在 ctx 时限内等待队列排空（已入队任务会被 worker 执行完）；
//  2. 关闭 stop 让 worker 退出 select 循环；
//  3. 在剩余 ctx 时限内等待 worker 结束在途 handler。
//
// 注意：Go 无法强制终止 goroutine。若某处理器永不返回，其 worker
// 会残留——超时路径直接返回 ctx.Err() 放弃等待，保证进程能退出；
// 正常路径下 worker 自然结束后返回 nil。
func (b *AsyncEventBus) Shutdown(ctx context.Context) error {
	if !b.closed.CompareAndSwap(false, true) {
		return nil
	}
	// 阶段 1：排空已入队任务。
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()
	for b.QueueLen() > 0 {
		select {
		case <-ctx.Done():
			close(b.stop)
			return ctx.Err()
		case <-ticker.C:
		}
	}
	close(b.stop)
	// 阶段 2：等待 worker 结束在途 handler（卡住则按 ctx 超时放弃）。
	done := make(chan struct{})
	go func() {
		b.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
