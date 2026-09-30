package domain

import "github.com/dirac-lee/domkit/domain"

// 订单域自定义业务操作：在内置 NEW / DELETE 之外扩展的操作。
var (
	// OperationPay 支付订单。
	OperationPay = domain.NewEntityOperation("PAY", "支付")
	// OperationCancel 取消订单。
	OperationCancel = domain.NewEntityOperation("CANCEL", "取消")
)

// OrderOperationRegistry 订单操作注册表：集中声明订单「允许触发」的全部操作。
// 注册表构造完成后只读，进程内复用同一实例即可。
var OrderOperationRegistry = func() *domain.OperationRegistry {
	r := domain.NewOperationRegistry() // 已内置 NEW / DELETE
	r.Register(OperationPay, OperationCancel)
	return r
}()
