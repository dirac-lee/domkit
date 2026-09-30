package broadcast

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/dirac-lee/domkit/domain"
)

// errBoom 测试用通用底层错误。
var errBoom = errors.New("boom")

// newValidSubscriber 构造依赖齐全的测试订阅者；具体用例可按需替换协作对象。
func newValidSubscriber(messenger Messenger, serializer Serializer) *Subscriber[testPayload] {
	return &Subscriber[testPayload]{
		Messenger:     messenger,
		Serializer:    serializer,
		AggregateType: "Order",
		Topic:         "order-broadcast",
		SenderCode:    "order-service",
		BuildPayload: func(domain.DomainEvent) (testPayload, error) {
			return testPayload{Greeting: "hi"}, nil
		},
	}
}

// TestSubscriber_HandleSuccess 成功路径应发送一条信封，元数据与消息体正确。
func TestSubscriber_HandleSuccess(t *testing.T) {
	messenger := &recordingMessenger{}
	sub := newValidSubscriber(messenger, NewJSONSerializer())

	if err := sub.Handle(newTestEvent("agg-7", 2, "PAY")); err != nil {
		t.Fatalf("处理失败: %v", err)
	}
	if len(messenger.sent) != 1 {
		t.Fatalf("应发送 1 条，实际 %d", len(messenger.sent))
	}
	sent := messenger.sent[0]
	if sent.topic != "order-broadcast" || sent.senderCode != "order-service" {
		t.Fatalf("发送目标/发送方错误: %+v", sent)
	}

	// 解析已发送内容，核对信封关键字段。
	var env Envelope[testPayload]
	if err := json.Unmarshal([]byte(sent.payload), &env); err != nil {
		t.Fatalf("信封 JSON 非法: %v", err)
	}
	if env.AggregateID != "agg-7" || env.Version != 2 || env.CauseOperation != "PAY" {
		t.Fatalf("信封元数据错误: %+v", env)
	}
	if env.Payload.Greeting != "hi" {
		t.Fatalf("消息体错误: %+v", env.Payload)
	}
}

// TestSubscriber_BuildPayloadError 构消息失败为不可重试信封错误，且不触达发送。
func TestSubscriber_BuildPayloadError(t *testing.T) {
	messenger := &recordingMessenger{}
	sub := newValidSubscriber(messenger, NewJSONSerializer())
	sub.BuildPayload = func(domain.DomainEvent) (testPayload, error) { return testPayload{}, errBoom }

	err := asEnvelopeError(t, sub.Handle(newTestEvent("agg-7", 2, "PAY")))
	if err.Stage != "build-payload" || IsRetryable(err) {
		t.Fatalf("应为 build-payload 不可重试错误，实际 stage=%s", err.Stage)
	}
	if len(messenger.sent) != 0 {
		t.Fatal("构消息失败不应发送")
	}
}

// TestSubscriber_SerializeError 序列化失败为不可重试信封错误。
func TestSubscriber_SerializeError(t *testing.T) {
	sub := newValidSubscriber(&recordingMessenger{}, failingSerializer{err: errBoom})
	err := asEnvelopeError(t, sub.Handle(newTestEvent("agg-7", 2, "PAY")))
	if err.Stage != "serialize" || IsRetryable(err) {
		t.Fatalf("应为 serialize 不可重试错误，实际 stage=%s", err.Stage)
	}
}

// TestSubscriber_SendError 发送失败为可重试错误，并保留目标 topic。
func TestSubscriber_SendError(t *testing.T) {
	messenger := &recordingMessenger{sendErr: errBoom}
	sub := newValidSubscriber(messenger, NewJSONSerializer())

	err := asSendError(t, sub.Handle(newTestEvent("agg-7", 2, "PAY")))
	if err.Topic != "order-broadcast" || !IsRetryable(err) {
		t.Fatalf("应为可重试发送错误，实际 %+v", err)
	}
}

// TestSubscriber_SendErrorNotDoubleWrapped 底层已是 *SendError 时原样上抛，不重复嵌套。
func TestSubscriber_SendErrorNotDoubleWrapped(t *testing.T) {
	orig := &SendError{Topic: "orig", Cause: errBoom}
	messenger := &recordingMessenger{sendErr: orig}
	sub := newValidSubscriber(messenger, NewJSONSerializer())

	got := asSendError(t, sub.Handle(newTestEvent("agg-7", 2, "PAY")))
	if got != orig || got.Topic != "orig" {
		t.Fatalf("应原样返回底层 SendError，实际 %+v", got)
	}
}

// TestSubscriber_InvalidConfig 配置缺失统一为 config 阶段的不可重试信封错误。
func TestSubscriber_InvalidConfig(t *testing.T) {
	evt := newTestEvent("agg-7", 2, "PAY")
	cases := []struct {
		name   string
		build  func() *Subscriber[testPayload]
	}{
		{"零值", func() *Subscriber[testPayload] { return &Subscriber[testPayload]{} }},
		{"缺 messenger", func() *Subscriber[testPayload] {
			s := newValidSubscriber(&recordingMessenger{}, NewJSONSerializer())
			s.Messenger = nil
			return s
		}},
		{"缺 serializer", func() *Subscriber[testPayload] {
			s := newValidSubscriber(&recordingMessenger{}, NewJSONSerializer())
			s.Serializer = nil
			return s
		}},
		{"缺 buildPayload", func() *Subscriber[testPayload] {
			s := newValidSubscriber(&recordingMessenger{}, NewJSONSerializer())
			s.BuildPayload = nil
			return s
		}},
		{"空 topic", func() *Subscriber[testPayload] {
			s := newValidSubscriber(&recordingMessenger{}, NewJSONSerializer())
			s.Topic = ""
			return s
		}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := asEnvelopeError(t, tc.build().Handle(evt))
			if err.Stage != "config" || IsRetryable(err) {
				t.Fatalf("应为 config 不可重试错误，实际 stage=%s", err.Stage)
			}
		})
	}
}
