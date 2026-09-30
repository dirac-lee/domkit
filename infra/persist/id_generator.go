package persist

import (
	"context"
	"fmt"
	"sync"
	"uuid"

	"github.com/dirac-lee/domkit/domain"
)

// ============ UUID 字符串主键生成器 ============

// UUIDGenerator 为字符串系主键（type OrderID string 等）发放 UUIDv7，
// 应用侧生成、无状态、不依赖数据库；UUIDv7 时间有序，对数据库聚簇索引友好。
type UUIDGenerator[ID ~string] struct {
	bizKey string
}

// NewUUIDGenerator 创建指定渠道的 UUID 主键生成器。
func NewUUIDGenerator[ID ~string](bizKey string) *UUIDGenerator[ID] {
	return &UUIDGenerator[ID]{bizKey: bizKey}
}

func (g *UUIDGenerator[ID]) BizKey() string { return g.bizKey }

func (g *UUIDGenerator[ID]) NextID(_ context.Context) (ID, error) {
	return ID(uuid.NewV7().String()), nil
}

func (g *UUIDGenerator[ID]) NextIDs(ctx context.Context, n int) ([]ID, error) {
	if n <= 0 {
		return nil, fmt.Errorf("persist: NextIDs requires positive n, got %d", n)
	}
	out := make([]ID, n)
	for i := range out {
		id, err := g.NextID(ctx)
		if err != nil {
			return nil, err
		}
		out[i] = id
	}
	return out, nil
}

// 编译期断言：UUIDGenerator 满足领域端口。
var (
	_ domain.IDGenerator[string] = (*UUIDGenerator[string])(nil)
	_ domain.IDGenerator[int64]  = (*SegmentIDGenerator[int64])(nil)
)

// ============ 号段模式主键生成器 ============

// SegmentIDGenerator 号段模式生成器（对标 Java AbstractSegmentIdGenerator）：
// 内存持有当前号段，耗尽时同步向 SegmentAllocator 申请下一段；
// 段内取号只走内存（无锁竞争之外的开销），申请新段由分配器保证并发安全。
// convert 决定 int64 原始号到主键类型 ID 的转换（纯数字 / 带前缀字符串 / 自定义强类型）。
type SegmentIDGenerator[ID comparable] struct {
	mu        sync.Mutex
	bizKey    string
	allocator domain.SegmentAllocator
	convert   func(int64) ID
	segment   domain.Segment
}

// NewSegmentIDGenerator 创建号段生成器。
func NewSegmentIDGenerator[ID comparable](bizKey string, allocator domain.SegmentAllocator, convert func(int64) ID) *SegmentIDGenerator[ID] {
	return &SegmentIDGenerator[ID]{
		bizKey:    bizKey,
		allocator: allocator,
		convert:   convert,
		// 初始号段置为耗尽态，确保首次 NextID 触发分配器申请真实号段。
		segment: domain.Segment{Current: 1, Max: 0},
	}
}

// NewInt64SegmentGenerator 产出纯 int64 主键的号段生成器（对标 LongSegmentIdGenerator）。
func NewInt64SegmentGenerator(bizKey string, allocator domain.SegmentAllocator) *SegmentIDGenerator[int64] {
	return NewSegmentIDGenerator[int64](bizKey, allocator, func(raw int64) int64 { return raw })
}

// NewFormattedStringSegmentGenerator 产出带格式字符串主键的号段生成器
// （对标 StringSegmentIdGenerator），format 为 fmt.Sprintf 模板，如 "ORD-%08d"。
func NewFormattedStringSegmentGenerator(bizKey, format string, allocator domain.SegmentAllocator) *SegmentIDGenerator[string] {
	return NewSegmentIDGenerator[string](bizKey, allocator, func(raw int64) string { return fmt.Sprintf(format, raw) })
}

func (g *SegmentIDGenerator[ID]) BizKey() string { return g.bizKey }

// NextID 生成下一个主键；当前号段耗尽时向分配器申请下一段。
func (g *SegmentIDGenerator[ID]) NextID(ctx context.Context) (ID, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if !g.segment.HasNext() {
		seg, err := g.allocator.AllocateNext(ctx, g.bizKey)
		if err != nil {
			var zero ID
			return zero, fmt.Errorf("persist: allocate segment for %q failed: %w", g.bizKey, err)
		}
		g.segment = seg
	}
	raw, next, err := g.segment.Take()
	if err != nil {
		var zero ID
		return zero, err
	}
	g.segment = next
	return g.convert(raw), nil
}

// NextIDs 批量生成 n 个主键；段边界处自动跨段，号段模式下段内连续。
func (g *SegmentIDGenerator[ID]) NextIDs(ctx context.Context, n int) ([]ID, error) {
	if n <= 0 {
		return nil, fmt.Errorf("persist: NextIDs requires positive n, got %d", n)
	}
	out := make([]ID, n)
	for i := range out {
		id, err := g.NextID(ctx)
		if err != nil {
			return nil, err
		}
		out[i] = id
	}
	return out, nil
}

// ============ 内存号段分配器（参考实现/测试） ============

// MemorySegmentAllocator 线程安全的内存号段分配器，模拟 id_segment 表语义：
// 每个 bizKey 维护已发放水位，AllocateNext 原子推进 step 个号并返回新区间。
// 已发放区间不回收（重启内存实现会丢失水位，生产环境请用数据库实现）。
type MemorySegmentAllocator struct {
	mu          sync.Mutex
	defaultStep int
	steps       map[string]int
	watermark   map[string]int64
}

// NewMemorySegmentAllocator 创建内存号段分配器，defaultStep 为未单独配置渠道的步长。
func NewMemorySegmentAllocator(defaultStep int) *MemorySegmentAllocator {
	if defaultStep <= 0 {
		defaultStep = 1000
	}
	return &MemorySegmentAllocator{
		defaultStep: defaultStep,
		steps:       make(map[string]int),
		watermark:   make(map[string]int64),
	}
}

// SetStep 为指定渠道配置号段步长。
func (a *MemorySegmentAllocator) SetStep(bizKey string, step int) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.steps[bizKey] = step
}

// Seed 设置渠道初始水位（模拟建表初值 current_max_id = startID-1），必须在首次取号前调用。
func (a *MemorySegmentAllocator) Seed(bizKey string, currentMax int64) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.watermark[bizKey] = currentMax
}

// AllocateNext 原子分配 [watermark+1, watermark+step]。
func (a *MemorySegmentAllocator) AllocateNext(_ context.Context, bizKey string) (domain.Segment, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	step := a.steps[bizKey]
	if step <= 0 {
		step = a.defaultStep
	}
	low := a.watermark[bizKey] + 1
	high := low + int64(step) - 1
	a.watermark[bizKey] = high
	return domain.Segment{Current: low, Max: high}, nil
}

// ============ 数据库号段分配接缝（P5 提供 SQL/GORM 实现） ============

// SegmentStore 号段表原子推进端口，由数据库基础设施实现。
// 实现必须在 REQUIRES_NEW 独立短事务内完成行锁与更新：
//
//	SELECT current_max_id FROM id_segment WHERE biz_key = ? FOR UPDATE;
//	UPDATE id_segment SET current_max_id = ? WHERE biz_key = ?;
//
// 返回新区间 [low, high]；行锁保证多实例不会拿到重叠区间。
type SegmentStore interface {
	AllocateSegment(ctx context.Context, bizKey string, step int) (low, high int64, err error)
}

// StoreSegmentAllocator 把 SegmentStore 适配为 domain.SegmentAllocator，
// 按 bizKey 解析步长（默认 1000）。
type StoreSegmentAllocator struct {
	store       SegmentStore
	defaultStep int
	steps       map[string]int
}

// NewStoreSegmentAllocator 创建数据库号段分配器。
func NewStoreSegmentAllocator(store SegmentStore, defaultStep int) *StoreSegmentAllocator {
	if defaultStep <= 0 {
		defaultStep = 1000
	}
	return &StoreSegmentAllocator{store: store, defaultStep: defaultStep, steps: make(map[string]int)}
}

// SetStep 为指定渠道配置号段步长。
func (a *StoreSegmentAllocator) SetStep(bizKey string, step int) {
	a.steps[bizKey] = step
}

// AllocateNext 在独立短事务内原子推进号段表水位。
func (a *StoreSegmentAllocator) AllocateNext(ctx context.Context, bizKey string) (domain.Segment, error) {
	step := a.steps[bizKey]
	if step <= 0 {
		step = a.defaultStep
	}
	low, high, err := a.store.AllocateSegment(ctx, bizKey, step)
	if err != nil {
		return domain.Segment{}, err
	}
	return domain.Segment{Current: low, Max: high}, nil
}

// ============ 多渠道生成器注册中心 ============

// keyedGenerator 注册中心对生成器的最小依赖（主键类型擦除后只剩渠道标识）。
type keyedGenerator interface {
	BizKey() string
}

// IdGeneratorRegistry 按 bizKey 隔离不同主键空间的生成器（对标 Java IdGeneratorRegistry）。
// Go 泛型接口不变式（IDGenerator[int64] 不是 IDGenerator[any]）决定了异构主键
// 只能以 any 擦除存储，取用/取号通过 Go 1.27 泛型方法恢复具体类型。
type IdGeneratorRegistry struct {
	mu         sync.RWMutex
	generators map[string]any
}

// NewIdGeneratorRegistry 创建空注册中心。
func NewIdGeneratorRegistry() *IdGeneratorRegistry {
	return &IdGeneratorRegistry{generators: make(map[string]any)}
}

// Register 注册一个生成器；重复注册同一 bizKey 覆盖旧实例。
func (r *IdGeneratorRegistry) Register(gen keyedGenerator) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.generators[gen.BizKey()] = gen
}

// Get 按主键类型取用生成器；未注册或主键类型不匹配时返回错误。
func (r *IdGeneratorRegistry) Get[ID comparable](bizKey string) (domain.IDGenerator[ID], error) {
	r.mu.RLock()
	gen, ok := r.generators[bizKey]
	r.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("persist: id generator not registered for bizKey %q", bizKey)
	}
	typed, ok := gen.(domain.IDGenerator[ID])
	if !ok {
		return nil, fmt.Errorf("persist: id generator for %q has mismatched key type %T", bizKey, gen)
	}
	return typed, nil
}

// NextID 便捷取号：按主键类型与 bizKey 直接生成下一个主键。
func (r *IdGeneratorRegistry) NextID[ID comparable](ctx context.Context, bizKey string) (ID, error) {
	gen, err := r.Get[ID](bizKey)
	if err != nil {
		var zero ID
		return zero, err
	}
	return gen.NextID(ctx)
}
