package domain

import (
	"context"
	"errors"
)

// IDGenerator 唯一主键生成器端口（对标 Java IIdGenerator）。
// 一个实例对应一个 bizKey（一个独立 ID 空间/一种聚合），由基础设施提供实现：
// UUID 字符串、数据库号段、雪花算法等。领域层只依赖该端口，不绑定具体发放策略。
type IDGenerator[ID comparable] interface {
	// BizKey 业务键（渠道/聚合类型），用于在注册中心隔离不同 ID 空间。
	BizKey() string
	// NextID 生成下一个唯一主键。
	NextID(ctx context.Context) (ID, error)
	// NextIDs 批量生成 n 个主键（号段模式下为连续值）。
	NextIDs(ctx context.Context, n int) ([]ID, error)
}

// Segment 一段连续可用的主键区间 [Current, Max]，左闭右闭（对标 Java IdSegment）。
// Current 为下一个将分配的值，Max 为本段上限。
type Segment struct {
	Current int64
	Max     int64
}

// HasNext 是否还有剩余可分配主键。
func (s Segment) HasNext() bool { return s.Current <= s.Max }

// Take 取出当前值并把区间游标前进一步，返回新的不可变区间。
func (s Segment) Take() (int64, Segment, error) {
	if !s.HasNext() {
		return 0, Segment{}, ErrSegmentExhausted
	}
	v := s.Current
	return v, Segment{Current: s.Current + 1, Max: s.Max}, nil
}

// Remaining 本段剩余可分配数量。
func (s Segment) Remaining() int64 {
	if s.Max < s.Current {
		return 0
	}
	return s.Max - s.Current + 1
}

// ErrSegmentExhausted 号段已耗尽，需先向分配器申请新段。
var ErrSegmentExhausted = errors.New("id segment exhausted, allocate next segment first")

// SegmentAllocator 号段分配端口：向底层存储申请下一段连续主键（对标 Java IIdSegmentAllocator）。
// 实现必须保证多实例并发安全（数据库行锁 SELECT ... FOR UPDATE 或等价机制），
// 段一旦发放即视为消耗，重启不回收，以保证主键不重复。
type SegmentAllocator interface {
	// AllocateNext 为 bizKey 原子分配下一段，返回的 Segment.Current 为该段第一个可用值。
	AllocateNext(ctx context.Context, bizKey string) (Segment, error)
}
