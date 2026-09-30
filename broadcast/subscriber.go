package broadcast

import (
	"context"
	"errors"

	"github.com/dirac-lee/domkit/domain"
)

// BuildPayloadFunc 由领域事件构建对接方约定消息体的函数，由订阅方提供。
type BuildPayloadFunc[P any] func(evt domain.DomainEvent) (P, error)

// Subscriber 对外广播订阅者（参数对象）：收到领域事件后依次执行
// 「构消息体 → 组信封 → 序列化 → 发送」。
//
// 其 Handle 方法签名与 infra/eventbus.EventHandler 完全一致，借助 Go 结构化
// 类型可直接注册到事件总线，而本包无需 import eventbus，保持 broadcast 只依赖 domain。
type Subscriber[P any] struct {
	Messenger     Messenger           // 对外发送通道（必填）
	Serializer    Serializer          // 信封序列化器（必填）
	AggregateType string              // 聚合根类型名，写入信封元数据（必填）
	Topic         string              // 对外广播 topic（必填）
	SenderCode    string              // 发送方标识，用于追踪（必填）
	BuildPayload  BuildPayloadFunc[P] // 由事件构建消息体（必填）
}

// Handle 事件总线回调：前置依赖缺失立即报不可重试的配置错误；
// 构消息/序列化失败为不可重试，发送失败为可重试；各阶段提前返回、不嵌套。
func (s *Subscriber[P]) Handle(evt domain.DomainEvent) error {
	// 事件到达时再次校验，保证即便使用方未在装配期调用 Validate 也不会带病执行。
	if err := s.Validate(); err != nil {
		return err
	}

	// 1. 构建对接方约定的消息体。
	payload, err := s.BuildPayload(evt)
	if err != nil {
		return wrapEnvelope("build-payload", err)
	}
	// 2. 用事件元数据组装信封。
	envelope := NewEnvelope(s.AggregateType, evt, payload)
	// 3. 序列化为字符串。
	serialized, err := s.Serializer.Serialize(envelope)
	if err != nil {
		return wrapEnvelope("serialize", err)
	}
	// 4. 发往对外 topic（事件回调不携带请求 ctx，使用后台上下文）。
	if err := s.Messenger.Send(context.Background(), s.Topic, s.SenderCode, serialized); err != nil {
		return wrapSend(s.Topic, err)
	}
	return nil
}

// Validate 校验必填协作对象与字符串，缺失统一翻译为不可重试的 EnvelopeError。
// 建议在组合根装配订阅者后、注册到事件总线前显式调用一次，使配置错误在
// 启动期即暴露（fail-fast），而不是等到第一条事件到达才被动发现。
func (s *Subscriber[P]) Validate() error {
	if s.Messenger == nil {
		return wrapEnvelope("config", errors.New("messenger 未配置"))
	}
	if s.Serializer == nil {
		return wrapEnvelope("config", errors.New("serializer 未配置"))
	}
	if s.BuildPayload == nil {
		return wrapEnvelope("config", errors.New("buildPayload 未配置"))
	}
	if s.AggregateType == "" || s.Topic == "" || s.SenderCode == "" {
		return wrapEnvelope("config", errors.New("aggregateType/topic/senderCode 不能为空"))
	}
	return nil
}
