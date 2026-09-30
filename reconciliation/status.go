// Package reconciliation 提供异构读副本与写模型之间的对账能力：
// 一致性状态判定（纯函数）、检测与补救、去重，以及面向单一聚合的对账管理器。
//
// 与 readmodel 的分工：readmodel 定义「副本/投影」原语；本包消费这些原语，
// 解决「副本与写模型是否一致、不一致如何自愈」的问题。
package reconciliation

// Status 异构读副本与写模型的一致性状态（与具体存储无关）。
// 枚举值从 1 开始，使零值非法，避免未初始化时被误判为 CONSISTENT。
type Status uint8

const (
	// StatusConsistent V' >= V，副本已最新。
	StatusConsistent Status = iota + 1
	// StatusStale V' < V，副本落后，需从写模型补同步。
	StatusStale
	// StatusOrphan 写模型已无此聚合（V<0）但副本仍有数据，需清理。
	StatusOrphan
	// StatusUntracked 副本未追踪版本（V'<0），无法对账。
	StatusUntracked
)

// String 返回状态的英文编码，供日志与接口输出。
func (s Status) String() string {
	switch s {
	case StatusConsistent:
		return "CONSISTENT"
	case StatusStale:
		return "STALE"
	case StatusOrphan:
		return "ORPHAN"
	case StatusUntracked:
		return "UNTRACKED"
	default:
		return "UNKNOWN"
	}
}

// Reconciliation 对账结果（不可变值对象）。
type Reconciliation struct {
	Status       Status // 判定状态
	ReadVersion  int64  // 异构读副本版本 V'
	WriteVersion int64  // 写模型当前版本 V
}

// Judge 一致性纯函数判定，优先级固定：
// 先看副本是否追踪（V'<0 → UNTRACKED），再看写模型是否还存在（V<0 → ORPHAN），
// 最后比较版本（V'>=V → CONSISTENT，否则 STALE）。
func Judge(readVersion, writeVersion int64) Reconciliation {
	switch {
	case readVersion < 0:
		return Reconciliation{Status: StatusUntracked, ReadVersion: readVersion, WriteVersion: writeVersion}
	case writeVersion < 0:
		return Reconciliation{Status: StatusOrphan, ReadVersion: readVersion, WriteVersion: writeVersion}
	case readVersion >= writeVersion:
		return Reconciliation{Status: StatusConsistent, ReadVersion: readVersion, WriteVersion: writeVersion}
	default:
		return Reconciliation{Status: StatusStale, ReadVersion: readVersion, WriteVersion: writeVersion}
	}
}

// IsStale / IsConsistent / IsOrphan / IsUntracked 语义化状态判断，供调用方按状态分支。
func (r Reconciliation) IsStale() bool      { return r.Status == StatusStale }
func (r Reconciliation) IsConsistent() bool { return r.Status == StatusConsistent }
func (r Reconciliation) IsOrphan() bool     { return r.Status == StatusOrphan }
func (r Reconciliation) IsUntracked() bool  { return r.Status == StatusUntracked }
