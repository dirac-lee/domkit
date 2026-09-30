package domain

import "github.com/dirac-lee/domkit/domain"

// OrderStatus 订单生命周期状态（富枚举值对象）。
// 以字符串编码作为对外/持久化值（Value），另携带中文展示名（Name），
// 用类型安全的枚举取代领域内散落的 "created"/"paid"/"cancelled" 裸字符串。
type OrderStatus struct {
	value string // 业务编码（对外值）
	name  string // 中文展示名
}

// Value 实现 domain.EnumValue[string]：返回持久化/传输用编码。
func (s OrderStatus) Value() string { return s.value }

// Name 实现 domain.EnumValue[string]：返回中文展示名。
func (s OrderStatus) Name() string { return s.name }

// String 实现 fmt.Stringer：%s 渲染时输出编码，
// 使规则文案里仍能显示 created/paid 等编码而非结构体。
func (s OrderStatus) String() string { return s.value }

// 订单全部生命周期状态（值均为不可变值对象，可安全并发使用）。
var (
	StatusCreated   = OrderStatus{value: "created", name: "已创建"}
	StatusPaid      = OrderStatus{value: "paid", name: "已支付"}
	StatusCancelled = OrderStatus{value: "cancelled", name: "已取消"}
)

// 编译期断言：OrderStatus 必须满足框架的枚举值规约。
var _ domain.EnumValue[string] = StatusCreated
