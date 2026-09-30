package domain

import "time"

// Entity 子实体（聚合内部实体）基类：承载身份标识、软删标记与审计字段。
// 业务子实体通过嵌入获得这些能力，例如：
//
//	type ItemID string
//	type OrderItem struct { domain.Entity[ItemID]; ProductID string }
//
// 与 AggregateRoot 的分工：本类型不含版本号、规则校验与领域事件——
// 那些是「聚合级」能力，只有聚合根才需要。
type Entity[ID comparable] struct {
	ID        ID
	Deleted   bool // 软删除标记
	CreatedAt time.Time // 创建时间
	UpdatedAt time.Time // 最近修改时间
	CreatedBy string // 创建人
	UpdatedBy string // 最近修改人
}

// MarkCreated 子实体在构造末尾调用一次：把创建时间与修改时间同时置为当前。
func (e *Entity[ID]) MarkCreated() {
	now := time.Now()
	e.CreatedAt = now
	e.UpdatedAt = now
}

// MarkModified 子实体发生修改时调用：只刷新修改时间。
func (e *Entity[ID]) MarkModified() {
	e.UpdatedAt = time.Now()
}

// SameIdentityAs 基于身份标识判断两个子实体是否为同一实体：
// 双方 ID 均为非零值且相等。零值 ID 的实体视为尚未获得身份，不参与等同判定。
func (e *Entity[ID]) SameIdentityAs(other *Entity[ID]) bool {
	var zero ID
	// 提前返回：任一 ID 为零值都不视为同一已持久化实体。
	if e.ID == zero || other.ID == zero {
		return false
	}
	return e.ID == other.ID
}
