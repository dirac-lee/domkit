package domain

import "github.com/dirac-lee/domkit/readmodel"

// OrderSummary 订单概要读模型：面向列表 / 分页查询，仅保留展示所需字段。
// 它是读模型（Projection），与写模型 Order 在类型层面严格分离。
type OrderSummary struct {
	ID      OrderID // 订单主键
	OrderNo string  // 订单号
	Status  string  // 生命周期状态编码（created/paid/cancelled）
	Amount  int64   // 订单金额
	Version uint64  // 已物化版本（来自写模型基线版本）
}

// ProjectionKind 实现 readmodel.Projection，返回投影逻辑名。
func (OrderSummary) ProjectionKind() string { return "OrderSummary" }

// ProjectOrderSummary 订单聚合 → 概要投影 的纯映射器。
// 只做字段取值，无存储访问、无副作用，可独立单测。
var ProjectOrderSummary readmodel.Projector[*Order, OrderSummary] = func(o *Order) OrderSummary {
	return OrderSummary{
		ID:      o.ID,
		OrderNo: o.OrderNo,
		Status:  o.Status.Value(), // 枚举降为字符串编码输出
		Amount:  o.Amount,
		Version: o.CurrentVersion(),
	}
}
