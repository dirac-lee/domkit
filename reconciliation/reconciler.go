package reconciliation

import (
	"context"

	"github.com/dirac-lee/domkit/readmodel"
)

// VersionReader 对账对写模型的最小要求：只读当前版本，不关心聚合值类型。
// domain.Repository[ID, 任意T] 都自动满足本接口——对账无需装载整个聚合。
type VersionReader[ID comparable] interface {
	CurrentVersion(ctx context.Context, id ID) (version uint64, exists bool, err error)
}

// writeVersionOf 把仓储的 (version, exists) 翻译为对账用的有符号版本号：
// 聚合不存在时 exists=false → 返回 -1，作为 ORPHAN 判定信号（uint64 无法表达 -1）。
func writeVersionOf[ID comparable](ctx context.Context, reader VersionReader[ID], id ID) (int64, error) {
	version, exists, err := reader.CurrentVersion(ctx, id)
	if err != nil {
		return 0, err
	}
	if !exists {
		return -1, nil
	}
	return int64(version), nil
}

// Reconcile 仅检测：比较副本版本 V' 与写模型版本 V，不做任何补救动作。
func Reconcile[ID comparable](
	ctx context.Context,
	replica readmodel.ReadModelReplica[ID],
	write VersionReader[ID],
	id ID,
) (Reconciliation, error) {
	readVersion, err := replica.ReadVersion(ctx, id)
	if err != nil {
		return Reconciliation{}, err
	}
	writeVersion, err := writeVersionOf(ctx, write, id)
	if err != nil {
		return Reconciliation{}, err
	}
	return Judge(readVersion, writeVersion), nil
}

// ReconcileAndResync 检测 + 立即补救（纯同步原语，内部不 sleep）：
//   - STALE  → replica.Rebuild：从写模型当前快照重建副本条目；
//   - ORPHAN → replica.PurgeOrphan：删除写模型已不存在的残留条目；
//   - CONSISTENT / UNTRACKED：不动作。
//
// 返回的是「补救前」的检测结果；补救动作失败时同时返回该结果与错误。
func ReconcileAndResync[ID comparable](
	ctx context.Context,
	replica readmodel.ReadModelReplica[ID],
	write VersionReader[ID],
	id ID,
) (Reconciliation, error) {
	result, err := Reconcile(ctx, replica, write, id)
	if err != nil {
		return Reconciliation{}, err
	}

	switch result.Status {
	case StatusStale:
		err = replica.Rebuild(ctx, id)
	case StatusOrphan:
		err = replica.PurgeOrphan(ctx, id)
	}
	return result, err
}
