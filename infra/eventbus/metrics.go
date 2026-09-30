package eventbus

// Metrics 事件总线监控 SPI（对标 Java IEventMetrics），不绑定具体监控实现。
// 生产环境可桥接 Prometheus/Micrometer；测试可用内存计数实现断言投递结果。
type Metrics interface {
	// Published 记录事件是否被总线接收（本地总线即入队结果）。
	Published(eventName string, success bool, latencyMS int64)
	// Consumed 记录一次订阅者投递的最终结果，attempts 为总尝试次数（含首次）。
	Consumed(eventName, subscriber string, success bool, attempts int)
	// DeadLettered 记录重试耗尽后进入死信的投递。
	DeadLettered(eventName, subscriber, reason string)
}

// NopMetrics 空监控实现（默认）。
type NopMetrics struct{}

func (NopMetrics) Published(string, bool, int64)       {}
func (NopMetrics) Consumed(string, string, bool, int)  {}
func (NopMetrics) DeadLettered(string, string, string) {}
