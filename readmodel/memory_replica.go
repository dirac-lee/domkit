package readmodel

import (
	"context"
	"errors"
	"sync"
)

// ErrAggregateNotFound 写模型中不存在该聚合。
// load 适配器需把各自仓储的「未找到」错误翻译为本哨兵，副本据此清理残留条目。
var ErrAggregateNotFound = errors.New("readmodel: aggregate not found in write model")

// ReadModelReplica 读模型副本的自我维护契约（对账能力的载体）。
//
// 一份物理副本（一个内存表 / ES 索引 / Redis 键空间）自行声明身份、读取副本版本、
// 从写模型重建自身、清理残留条目。不变式：每个副本都可对账，不存在「可选对账」的副本。
type ReadModelReplica[ID comparable] interface {
	// ReplicaID 副本标识（如 memory:order-summary），在本聚合内全局唯一。
	ReplicaID() string
	// ReadVersion 读取副本中聚合 id 已物化的版本 V'。
	// 副本缺失返回 0（对账判 STALE、需重建）。
	ReadVersion(ctx context.Context, id ID) (int64, error)
	// Rebuild 以聚合 id 为粒度，从写模型当前快照重建本副本。
	Rebuild(ctx context.Context, id ID) error
	// PurgeOrphan 写模型已无此聚合时，删除本副本中的残留条目。
	PurgeOrphan(ctx context.Context, id ID) error
}

// storedEntry 副本中的单条物化记录：投影视图 + 已物化版本。
type storedEntry[P Projection] struct {
	view    P
	version int64
}

// MemoryReplica 内存读模型副本：线程安全，承载单一全量投影 P。
// 写（Sync/Rebuild/PurgeOrphan）与读（GetByID/List/Page）收敛于同一对象。
type MemoryReplica[ID comparable, P Projection] struct {
	replicaID string
	mu        sync.RWMutex
	entries   map[ID]storedEntry[P]
	// syncFn 在构造时由 load + project 组装，闭包内捕获具体聚合类型，
	// 因此结构体本身无需再携带聚合类型参数。
	syncFn func(ctx context.Context, id ID) error
}

// NewMemoryReplica 构造内存副本。
//
//	load    从写模型加载聚合，未找到时返回 ErrAggregateNotFound；
//	project 聚合 → 投影 的纯映射器。
//
// 三个类型参数 ID/AGG/P 均可由实参推断，调用点无需显式给出。
func NewMemoryReplica[ID comparable, AGG AggregateWithVersion, P Projection](
	replicaID string,
	load func(ctx context.Context, id ID) (AGG, error),
	project Projector[AGG, P],
) *MemoryReplica[ID, P] {
	r := &MemoryReplica[ID, P]{
		replicaID: replicaID,
		entries:   make(map[ID]storedEntry[P]),
	}
	// 实时投影 / 重建共用同一物化动作。
	r.syncFn = func(ctx context.Context, id ID) error {
		agg, err := load(ctx, id)
		if err != nil {
			return r.handleLoadError(ctx, id, err)
		}
		r.upsert(id, project(agg), int64(agg.CurrentVersion()))
		return nil
	}
	return r
}

// handleLoadError 区分「写模型已删除」与「真实失败」：
// 前者清理残留条目并正常返回，后者向上传播错误。
func (r *MemoryReplica[ID, P]) handleLoadError(ctx context.Context, id ID, err error) error {
	if errors.Is(err, ErrAggregateNotFound) {
		return r.PurgeOrphan(ctx, id)
	}
	return err
}

// upsert 加锁写入/更新一条物化记录。
func (r *MemoryReplica[ID, P]) upsert(id ID, view P, version int64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.entries[id] = storedEntry[P]{view: view, version: version}
}

// Sync 实时投影入口（通常由领域事件订阅者调用），语义等同 Rebuild。
func (r *MemoryReplica[ID, P]) Sync(ctx context.Context, id ID) error {
	return r.syncFn(ctx, id)
}

// ReplicaID 实现 ReadModelReplica，返回副本标识。
func (r *MemoryReplica[ID, P]) ReplicaID() string { return r.replicaID }

// ReadVersion 实现 ReadModelReplica，返回副本已物化版本；缺失返回 0。
func (r *MemoryReplica[ID, P]) ReadVersion(_ context.Context, id ID) (int64, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if e, ok := r.entries[id]; ok {
		return e.version, nil
	}
	return 0, nil
}

// Rebuild 实现 ReadModelReplica，从写模型重建该聚合的副本条目。
func (r *MemoryReplica[ID, P]) Rebuild(ctx context.Context, id ID) error {
	return r.syncFn(ctx, id)
}

// PurgeOrphan 实现 ReadModelReplica，删除副本残留条目。
func (r *MemoryReplica[ID, P]) PurgeOrphan(_ context.Context, id ID) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.entries, id)
	return nil
}

// GetByID 按键取投影；未命中 ok 为 false。
func (r *MemoryReplica[ID, P]) GetByID(_ context.Context, id ID) (P, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	e, ok := r.entries[id]
	return e.view, ok
}

// List 返回全部投影（内存 map 遍历，不保证顺序）。
func (r *MemoryReplica[ID, P]) List(_ context.Context) []P {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]P, 0, len(r.entries))
	for _, e := range r.entries {
		out = append(out, e.view)
	}
	return out
}

// Page 对全量投影做内存分页。越界页码返回空数据，但仍携带总条数。
func (r *MemoryReplica[ID, P]) Page(ctx context.Context, req PageRequest) PageResult[P] {
	all := r.List(ctx)
	total := len(all)
	start := req.Offset()
	if start >= total {
		return NewPageResult([]P{}, int64(total), req)
	}
	end := start + req.PageSize()
	if end > total {
		end = total
	}
	return NewPageResult(all[start:end], int64(total), req)
}

// 编译期断言：*MemoryReplica 必须满足 ReadModelReplica 契约。
var _ ReadModelReplica[string] = (*MemoryReplica[string, projectionProbe])(nil)

// projectionProbe 仅供上述编译期断言使用。
type projectionProbe struct{}

func (projectionProbe) ProjectionKind() string { return "probe" }
