package broadcast

import "context"

// Messenger 对外广播消息发送端口，与框架内部事件 MQ 链路完全解耦。
// 实现负责把已序列化信封发送到对接方约定的对外 topic；底层介质
// （RocketMQ / Kafka 等）由具体模块各自实现。topic 为对接方约定字符串，
// 不复用内部事件链路的主题解析。
type Messenger interface {
	// Send 发送一条已序列化信封：
	// topic 为目标主题，senderCode 为发送方标识（用于日志与追踪）。
	Send(ctx context.Context, topic, senderCode, serializedEnvelope string) error
}
