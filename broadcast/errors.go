package broadcast

import (
	"errors"
	"fmt"
)

// SendError 对外广播「发送阶段」失败（底层介质抖动、网络异常等），具有可重试语义。
// 上层可据此类型决策重试、降级或熔断。
type SendError struct {
	Topic string // 发送目标 topic
	Cause error
}

func (e *SendError) Error() string {
	return fmt.Sprintf("broadcast: 发送失败 topic=%s: %v", e.Topic, e.Cause)
}

func (e *SendError) Unwrap() error { return e.Cause }

// EnvelopeError 对外广播「信封处理阶段」失败（构消息/序列化/配置缺失），
// 多为编程或配置错误，具有不可重试语义，不应对其自动重试。
type EnvelopeError struct {
	Stage string // 处理阶段：build-payload / serialize / config
	Cause error
}

func (e *EnvelopeError) Error() string {
	return fmt.Sprintf("broadcast: 信封处理失败 stage=%s: %v", e.Stage, e.Cause)
}

func (e *EnvelopeError) Unwrap() error { return e.Cause }

// IsRetryable 判断广播失败是否值得重试：仅发送阶段错误（*SendError）可重试。
func IsRetryable(err error) bool {
	var sendErr *SendError
	return errors.As(err, &sendErr)
}

// wrapSend 包装发送阶段异常；若已是 *SendError 则原样返回，避免重复嵌套。
func wrapSend(topic string, cause error) error {
	if cause == nil {
		return nil
	}
	var sendErr *SendError
	if errors.As(cause, &sendErr) {
		return cause
	}
	return &SendError{Topic: topic, Cause: cause}
}

// wrapEnvelope 包装信封处理阶段异常；若已是 *EnvelopeError 则原样返回。
func wrapEnvelope(stage string, cause error) error {
	if cause == nil {
		return nil
	}
	var envelopeErr *EnvelopeError
	if errors.As(cause, &envelopeErr) {
		return cause
	}
	return &EnvelopeError{Stage: stage, Cause: cause}
}
