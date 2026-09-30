package broadcast

import (
	"context"
	"errors"
	"time"

	"github.com/dirac-lee/domkit/domain"
)

// testPayload 测试用对外消息体。
type testPayload struct {
	Greeting string `json:"greeting"`
}

// testEvent 测试用领域事件，直接填充导出的元数据字段。
type testEvent struct {
	domain.BaseDomainEvent[string]
}

func (*testEvent) EventName() string { return "test.event" }

// newTestEvent 构造归属 aggID、版本 ver、成因操作 opCode 的测试事件。
func newTestEvent(aggID string, ver uint64, opCode string) *testEvent {
	return &testEvent{BaseDomainEvent: domain.BaseDomainEvent[string]{
		EventIDVal:          "evt-1",
		AggregateIDVal:      aggID,
		AggregateVersionVal: ver,
		OccurredAtVal:       time.Date(2026, 9, 30, 10, 0, 0, 0, time.UTC),
		OperationCodeVal:    opCode,
	}}
}

// sentMessage 记录一次发送的入参。
type sentMessage struct {
	topic      string
	senderCode string
	payload    string
}

// recordingMessenger 可录制、可注入发送错误的测试 Messenger。
type recordingMessenger struct {
	sent    []sentMessage
	sendErr error
}

func (m *recordingMessenger) Send(_ context.Context, topic, senderCode, payload string) error {
	m.sent = append(m.sent, sentMessage{topic: topic, senderCode: senderCode, payload: payload})
	return m.sendErr
}

// failingSerializer 始终返回固定错误的测试 Serializer，用于制造序列化失败。
type failingSerializer struct{ err error }

func (s failingSerializer) Serialize(any) (string, error) { return "", s.err }

// asSendError 提取 *SendError，提取失败时让测试失败。
func asSendError(t TLike, err error) *SendError {
	t.Helper()
	var se *SendError
	if !errors.As(err, &se) {
		t.Fatalf("期望 *SendError，实际 %T: %v", err, err)
	}
	return se
}

// asEnvelopeError 提取 *EnvelopeError，提取失败时让测试失败。
func asEnvelopeError(t TLike, err error) *EnvelopeError {
	t.Helper()
	var ee *EnvelopeError
	if !errors.As(err, &ee) {
		t.Fatalf("期望 *EnvelopeError，实际 %T: %v", err, err)
	}
	return ee
}

// TLike 收敛 *testing.T 用到的方法，便于 helper 复用。
type TLike interface {
	Helper()
	Fatalf(format string, args ...any)
}
