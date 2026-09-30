package app

import (
	"context"

	"github.com/dirac-lee/domkit/example/order/internal/order/port"
)

// PaymentService 支付应用用例：编排「向支付渠道发起预授权」。
// 只依赖端口 port.PaymentAuthorizer，不感知具体渠道/协议——渠道细节被关在 ACL 层。
type PaymentService struct {
	authorizer port.PaymentAuthorizer
}

// NewPaymentService 注入支付端口；组合根传入 ACL 适配器实例。
func NewPaymentService(authorizer port.PaymentAuthorizer) *PaymentService {
	return &PaymentService{authorizer: authorizer}
}

// Authorize 发起预授权，直接透传端口结果；此处可扩展编排（如写本地预授权记录）。
func (s *PaymentService) Authorize(ctx context.Context, cmd port.PreAuthCommand) (port.PreAuthResult, error) {
	return s.authorizer.Authorize(ctx, cmd)
}
