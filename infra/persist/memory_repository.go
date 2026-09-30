package persist

import (
	"context"
	"sync"

	"github.com/dirac-lee/domkit/domain"
)

// VersionAccess 告诉 MemoryRepository 如何读取与重建聚合的版本瞬态。
// 由于 Go 泛型无法直接访问 T 内嵌的 AggregateRoot 字段/方法，由调用方显式提供：
//   - Current：聚合当前持有的基线版本（装载版本，作为 CAS 期望版本）；
//   - Next：   本次操作的待提交版本（未生成新版本时返回基线即可）；
//   - Reload： 装载行数据后重建版本瞬态（通常对接 AggregateRoot.MarkPersisted）。
type VersionAccess[T any] struct {
	Current func(*T) uint64
	Next    func(*T) uint64
	Reload  func(agg *T, committedVersion uint64)
}

type storedEntry[T any] struct {
	agg     T
	version uint64 // 已提交行版本，独立于内存对象跟踪，作为 CAS 比较基准
}

// MemoryRepository 线程安全的内存聚合仓储，作为 Repository 的参考实现，
// 便于测试与无数据库环境运行；Insert 写入待提交版本，Update/Delete 严格执行
// 「行版本 == 聚合基线版本」的乐观锁检查（CAS）。
//
// ID 为聚合主键类型（string 系强类型 ID、int64、uint64 等任意可比较类型）。
type MemoryRepository[ID comparable, T any] struct {
	mu   sync.Mutex
	data map[ID]storedEntry[T]
	idOf func(*T) ID
	va   VersionAccess[T]
}

// NewMemoryRepository 创建内存仓储。类型实参通常可由 idOf/va 推断。
func NewMemoryRepository[ID comparable, T any](idOf func(*T) ID, va VersionAccess[T]) *MemoryRepository[ID, T] {
	return &MemoryRepository[ID, T]{
		data: make(map[ID]storedEntry[T]),
		idOf: idOf,
		va:   va,
	}
}

// aggregateVersioned 版本读写方法集：嵌入 domain.AggregateRoot 的聚合，
// 其指针 *T 经内嵌方法提升自动满足，业务方无需手写。
type aggregateVersioned interface {
	CurrentVersion() uint64 // 读取聚合基线版本（CAS 期望版本）
	PendingVersion() uint64 // 读取本次操作的待提交版本
	MarkPersisted(uint64)   // 以行版本重建版本瞬态
}

// NewAggregateRepository 为「嵌入 domain.AggregateRoot 的聚合」提供零样板内存仓储：
// 版本读写接缝由编译器按 *T 的方法集自动接线，调用方只需提供主键提取函数，
// 免去手写 VersionAccess 的三连转发闭包。
//
// 若聚合未嵌入 AggregateRoot，或版本存放在自定义字段需要特殊读写，
// 请改用 NewMemoryRepository 并显式传入 VersionAccess。
func NewAggregateRepository[ID comparable, T any, P interface {
	*T
	aggregateVersioned
}](idOf func(P) ID) *MemoryRepository[ID, T] {
	// P 即 *T：把 func(P) ID 适配为仓储内部统一使用的 func(*T) ID。
	idOfBase := func(agg *T) ID { return idOf(P(agg)) }

	// 默认版本接缝：三个字段全部转发到 AggregateRoot 的同名方法。
	return NewMemoryRepository(idOfBase, VersionAccess[T]{
		Current: func(agg *T) uint64 { return P(agg).CurrentVersion() },
		Next:    func(agg *T) uint64 { return P(agg).PendingVersion() },
		Reload:  func(agg *T, version uint64) { P(agg).MarkPersisted(version) },
	})
}

// Insert 写入新建聚合，行版本取待提交版本；同 ID 已存在时返回冲突错误。
func (r *MemoryRepository[ID, T]) Insert(_ context.Context, agg *T) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	id := r.idOf(agg)
	if existing, exists := r.data[id]; exists {
		return domain.NewConcurrencyConflictError(id, r.va.Current(agg), existing.version, false)
	}
	r.data[id] = storedEntry[T]{agg: *agg, version: r.va.Next(agg)}
	return nil
}

// Update CAS 更新：仅当行版本等于聚合基线版本时写入，并把行版本推进到待提交版本；
// 聚合已被删除或版本过期时返回 ErrConcurrencyConflict。
func (r *MemoryRepository[ID, T]) Update(_ context.Context, agg *T) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	id := r.idOf(agg)
	entry, exists := r.data[id]
	expected := r.va.Current(agg)
	if err := domain.CheckVersion(id, expected, entry.version, exists); err != nil {
		return err
	}
	r.data[id] = storedEntry[T]{agg: *agg, version: r.va.Next(agg)}
	return nil
}

// Delete CAS 删除：版本一致才删除；聚合不存在或版本过期返回冲突错误。
func (r *MemoryRepository[ID, T]) Delete(_ context.Context, agg *T) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	id := r.idOf(agg)
	entry, exists := r.data[id]
	if err := domain.CheckVersion(id, r.va.Current(agg), entry.version, exists); err != nil {
		return err
	}
	delete(r.data, id)
	return nil
}

// GetByID 按主键返回聚合副本并以行版本重建版本瞬态，未命中返回 (nil, nil)。
func (r *MemoryRepository[ID, T]) GetByID(_ context.Context, id ID) (*T, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	entry, ok := r.data[id]
	if !ok {
		return nil, nil
	}
	cp := entry.agg
	r.va.Reload(&cp, entry.version)
	return &cp, nil
}

// CurrentVersion 返回已提交行版本；聚合不存在时 exists=false。
func (r *MemoryRepository[ID, T]) CurrentVersion(_ context.Context, id ID) (uint64, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	entry, ok := r.data[id]
	if !ok {
		return 0, false, nil
	}
	return entry.version, true, nil
}

// Len 返回当前存储的聚合数量（主要用于测试/运维观察）。
func (r *MemoryRepository[ID, T]) Len() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.data)
}
