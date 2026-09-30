package acl

import (
	"errors"
	"fmt"

	"github.com/dirac-lee/domkit/domain"
)

// ConversionError 防腐层本地数据转换错误（请求转换 / 响应转换 / 查重键提取 / 已存在记录转换）。
// 属于本地问题，重试无意义，应修复入参或映射；故 IsRetryableErr 对其返回 false。
// 通过 Unwrap 保留原始错误，便于排障与 errors.As 定位根因。
type ConversionError struct {
	Stage string // 出错阶段描述（如 请求转换）
	Err   error  // 原始错误
}

func (e *ConversionError) Error() string {
	return fmt.Sprintf("acl %s失败：%v", e.Stage, e.Err)
}

func (e *ConversionError) Unwrap() error { return e.Err }

// CommunicationError 防腐层外部通信错误（网络、超时、远程业务错误、非预期状态码）。
// 通常可由上层决策重试、降级或熔断；故 IsRetryableErr 对其返回 true。
type CommunicationError struct {
	Stage string
	Err   error
}

func (e *CommunicationError) Error() string {
	return fmt.Sprintf("acl %s失败：%v", e.Stage, e.Err)
}

func (e *CommunicationError) Unwrap() error { return e.Err }

// IsRetryableErr 判断外部调用异常是否值得重试：
//   - nil：无异常，不可重试；
//   - ConversionError：本地转换问题，不可重试；
//   - CommunicationError：通信问题，可重试；
//   - 领域规则错误：业务语义失败，重试无意义，不可重试；
//   - 其余未知异常：默认可重试。
func IsRetryableErr(err error) bool {
	// 提前返回，按错误类型逐条判定，不做多层嵌套。
	if err == nil {
		return false
	}
	var convErr *ConversionError
	if errors.As(err, &convErr) {
		return false
	}
	var commErr *CommunicationError
	if errors.As(err, &commErr) {
		return true
	}
	if domain.IsDomainRuleError(err) {
		return false
	}
	return true
}
