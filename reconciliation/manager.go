package reconciliation

import (
	"context"
	"errors"

	"github.com/dirac-lee/domkit/readmodel"
)

// ErrReplicaNotFound 管理器中未注册指定副本。
var ErrReplicaNotFound = errors.New("reconciliation: replica not registered")

// Manager 单一聚合（或共享同一 ID 类型的一组聚合）的对账管理器。
//
// Go 版不像 Java 那样以 Class<T> + 强制转换做异构注册表，而是以 ID 类型参数
// 绑定一个聚合：内部所有副本/版本读取器 ID 类型一致，因此全程类型安全、零强制转换。
// 它同时承担「注册表」（持有全部副本）与「管理器」（编排检测补救）两项内聚职责。
type Manager[ID comparable] struct {
	write      VersionReader[ID]                       // 写模型版本读取器
	replicaIDs []string                                // 副本 ID 的注册顺序，保证遍历确定
	replicas   map[string]readmodel.ReadModelReplica[ID] // 副本逻辑ID -> 副本
	dedup      Dedup[ID]                               // 去重策略
}

// NewManager 以写模型版本读取器构造管理器，默认不去重。
func NewManager[ID comparable](write VersionReader[ID]) *Manager[ID] {
	return &Manager[ID]{
		write:    write,
		replicas: map[string]readmodel.ReadModelReplica[ID]{},
		dedup:    NoOpDedup[ID]{},
	}
}

// SetDedup 注入去重策略；传 nil 时保持默认不去重。返回自身便于链式装配。
func (m *Manager[ID]) SetDedup(d Dedup[ID]) *Manager[ID] {
	if d != nil {
		m.dedup = d
	}
	return m
}

// RegisterReplica 登记一个副本，键取自副本自身 ReplicaID；重复登记同一 ID 视为覆盖更新。
// 返回自身便于链式装配。
func (m *Manager[ID]) RegisterReplica(replica readmodel.ReadModelReplica[ID]) *Manager[ID] {
	id := replica.ReplicaID()
	if _, exists := m.replicas[id]; !exists {
		m.replicaIDs = append(m.replicaIDs, id)
	}
	m.replicas[id] = replica
	return m
}

// Reconcile 对该聚合全部已注册副本执行「检测 + 补救」，返回每个副本的对账结果。
// 任一副本失败立即停止并返回已完成的部分结果与错误（fail-fast）。
func (m *Manager[ID]) Reconcile(ctx context.Context, id ID) (map[string]Reconciliation, error) {
	results := map[string]Reconciliation{}
	for _, replicaID := range m.replicaIDs {
		if err := m.reconcileOne(ctx, replicaID, id, results); err != nil {
			return results, err
		}
	}
	return results, nil
}

// ReconcileReplica 仅对单个指定副本对账；副本未注册返回 ErrReplicaNotFound。
func (m *Manager[ID]) ReconcileReplica(ctx context.Context, replicaID string, id ID) (Reconciliation, error) {
	replica, ok := m.replicas[replicaID]
	if !ok {
		return Reconciliation{}, ErrReplicaNotFound
	}
	return ReconcileAndResync(ctx, replica, m.write, id)
}

// ReconcileBatch 批量对账：由调用方自备候选 ID 集合（如新增副本回填、运维修复存量漂移）。
// 框架不决定「何时对账、对账哪些」；结果按聚合 ID 分组。
func (m *Manager[ID]) ReconcileBatch(ctx context.Context, ids []ID) (map[ID]map[string]Reconciliation, error) {
	results := make(map[ID]map[string]Reconciliation, len(ids))
	for _, id := range ids {
		perReplica, err := m.Reconcile(ctx, id)
		if err != nil {
			return results, err
		}
		results[id] = perReplica
	}
	return results, nil
}

// reconcileOne 处理单个 (副本, 聚合ID)：去重判断 → 检测补救 → 标记已处理。
// 抽成共享助手，供全副本/批量两条路径复用，避免重复编排逻辑。
func (m *Manager[ID]) reconcileOne(
	ctx context.Context,
	replicaID string,
	id ID,
	out map[string]Reconciliation,
) error {
	if m.dedup.ShouldSkip(replicaID, id) {
		return nil
	}
	result, err := ReconcileAndResync(ctx, m.replicas[replicaID], m.write, id)
	if err != nil {
		return err
	}
	out[replicaID] = result
	m.dedup.Mark(replicaID, id)
	return nil
}
