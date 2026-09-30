package broadcast

import (
	"encoding/json"
	"testing"
)

// TestNewEnvelope_Metadata 信封元数据应全部取自触发事件，消息标识新生成。
func TestNewEnvelope_Metadata(t *testing.T) {
	evt := newTestEvent("agg-7", 3, "PAY")
	env := NewEnvelope("Order", evt, testPayload{Greeting: "hi"})

	// 来自事件的元数据。
	if env.AggregateID != "agg-7" || env.Version != 3 || env.CauseOperation != "PAY" {
		t.Fatalf("信封元数据回填错误: %+v", env)
	}
	if env.SourceEventID != "evt-1" || env.AggregateType != "Order" {
		t.Fatalf("来源事件/聚合类型错误: %+v", env)
	}
	if env.SchemaVersion != 1 {
		t.Fatalf("协议版本应为 1，实际 %d", env.SchemaVersion)
	}
	// 消息标识为新生成的 UUID，不应复用事件标识。
	if env.MessageID == "" || env.MessageID == evt.EventID() {
		t.Fatalf("消息标识应新生成，实际 %q", env.MessageID)
	}
	if env.Payload.Greeting != "hi" {
		t.Fatalf("消息体错误: %+v", env.Payload)
	}
}

// TestJSONSerializer_RoundTrip 信封经 JSON 序列化后字段名与结构应可完整还原。
func TestJSONSerializer_RoundTrip(t *testing.T) {
	env := NewEnvelope("Order", newTestEvent("agg-7", 3, "PAY"), testPayload{Greeting: "hi"})

	serialized, err := NewJSONSerializer().Serialize(env)
	if err != nil {
		t.Fatalf("序列化失败: %v", err)
	}

	// 反序列化回同一信封类型，逐字段核对。
	var got Envelope[testPayload]
	if err := json.Unmarshal([]byte(serialized), &got); err != nil {
		t.Fatalf("反序列化失败: %v", err)
	}
	if got.AggregateID != env.AggregateID || got.Version != env.Version ||
		got.CauseOperation != env.CauseOperation || got.Payload.Greeting != "hi" {
		t.Fatalf("往返后字段不一致: %+v", got)
	}
}

// TestIsRetryable 仅发送阶段错误可重试，信封错误与 nil 不可重试。
func TestIsRetryable(t *testing.T) {
	if !IsRetryable(&SendError{Topic: "t"}) {
		t.Fatal("SendError 应可重试")
	}
	if IsRetryable(&EnvelopeError{Stage: "serialize"}) {
		t.Fatal("EnvelopeError 不应可重试")
	}
	if IsRetryable(nil) {
		t.Fatal("nil 不应可重试")
	}
}
