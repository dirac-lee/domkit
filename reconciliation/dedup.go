package reconciliation

// Dedup 对账去重：避免同一 (副本, 聚合ID) 在短时间窗口内被重复补救。
// Manager 的 ID 类型已确定，故此处用副本逻辑 ID（string）+ 聚合 ID 定位。
type Dedup[ID comparable] interface {
	// ShouldSkip 返回 true 表示该 (副本, 聚合ID) 窗口内已处理、本次应跳过。
	ShouldSkip(replicaID string, id ID) bool
	// Mark 标记该 (副本, 聚合ID) 已处理。
	Mark(replicaID string, id ID)
}

// NoOpDedup 默认不去重：每次都执行检测与补救。
type NoOpDedup[ID comparable] struct{}

// ShouldSkip 恒为 false。
func (NoOpDedup[ID]) ShouldSkip(string, ID) bool { return false }

// Mark 空操作。
func (NoOpDedup[ID]) Mark(string, ID) {}
