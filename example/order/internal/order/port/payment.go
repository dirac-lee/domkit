package port

import (
	"context"

	"github.com/dirac-lee/domkit/example/order/internal/order/domain"
)

// PreAuthCommand 发起支付预授权的入参命令（端口契约 DTO）。
// 用参数对象聚合三个字段，避免应用用例出现过长参数列表。
type PreAuthCommand struct {
	OrderID domain.OrderID // 目标订单主键（仅携带，ACL 不解读）
	OrderNo string         // 商户单号来源
	Amount  int64          // 预授权金额
}

// PreAuthResult 预授权领域结果；Reused=true 表示命中对方已有记录而短路。
// json tag 固定对外字段名，保证入站响应契约不随内部结构调整而变化。
type PreAuthResult struct {
	PreAuthID string `json:"preAuthId"` // 支付侧预授权流水号
	Reused    bool   `json:"reused"`    // 是否复用了已存在的预授权
}

// PaymentAuthorizer 支付预授权端口：应用层只依赖此接口，完全不感知具体支付渠道/协议。
// 真正实现由 ACL 层（PreAuthClient）提供，实现依赖倒置——应用核心不向外看。
type PaymentAuthorizer interface {
	Authorize(ctx context.Context, cmd PreAuthCommand) (PreAuthResult, error)
}
