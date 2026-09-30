package persist

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/dirac-lee/domkit/domain"
)

func TestSegmentInt64GeneratorContinuousAcrossSegments(t *testing.T) {
	alloc := NewMemorySegmentAllocator(3) // 每段 3 个号：[1,3] [4,6] ...
	gen := NewInt64SegmentGenerator("order", alloc)
	ctx := context.Background()

	var got []int64
	for i := 0; i < 7; i++ {
		id, err := gen.NextID(ctx)
		if err != nil {
			t.Fatalf("next id failed: %v", err)
		}
		got = append(got, id)
	}
	want := []int64{1, 2, 3, 4, 5, 6, 7}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("expected continuous ids %v, got %v", want, got)
		}
	}
	batch, err := gen.NextIDs(ctx, 3)
	if err != nil || len(batch) != 3 || batch[0] != 8 || batch[2] != 10 {
		t.Fatalf("batch ids mismatch: %v err=%v", batch, err)
	}
}

func TestFormattedStringSegmentGenerator(t *testing.T) {
	alloc := NewMemorySegmentAllocator(100)
	gen := NewFormattedStringSegmentGenerator("order-no", "ORD-%08d", alloc)
	id, err := gen.NextID(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if id != "ORD-00000001" {
		t.Fatalf("formatted id mismatch: %s", id)
	}
}

func TestSegmentGeneratorBizKeyIsolation(t *testing.T) {
	alloc := NewMemorySegmentAllocator(1000)
	orderGen := NewInt64SegmentGenerator("order", alloc)
	userGen := NewInt64SegmentGenerator("user", alloc)

	o1, _ := orderGen.NextID(context.Background())
	u1, _ := userGen.NextID(context.Background())
	o2, _ := orderGen.NextID(context.Background())
	if o1 != 1 || u1 != 1 || o2 != 2 {
		t.Fatalf("bizKey spaces must be isolated: order=%d,%d user=%d", o1, o2, u1)
	}
}

// 并发取号：50 个 goroutine 各取 100 个，结果必须全局唯一且连续无空洞。
func TestSegmentGeneratorConcurrentUniqueness(t *testing.T) {
	alloc := NewMemorySegmentAllocator(250)
	gen := NewInt64SegmentGenerator("seq", alloc)
	ctx := context.Background()

	const workers = 50
	const per = 100
	var wg sync.WaitGroup
	results := make([][]int64, workers)
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			ids := make([]int64, per)
			for i := range ids {
				id, err := gen.NextID(ctx)
				if err != nil {
					t.Errorf("next id failed: %v", err)
					return
				}
				ids[i] = id
			}
			results[idx] = ids
		}(w)
	}
	wg.Wait()

	seen := make(map[int64]struct{}, workers*per)
	for _, ids := range results {
		for _, id := range ids {
			if _, dup := seen[id]; dup {
				t.Fatalf("duplicate id allocated: %d", id)
			}
			seen[id] = struct{}{}
		}
	}
	if len(seen) != workers*per {
		t.Fatalf("expected %d unique ids, got %d", workers*per, len(seen))
	}
}

func TestUUIDGenerator(t *testing.T) {
	type docID string
	gen := NewUUIDGenerator[docID]("doc")
	if gen.BizKey() != "doc" {
		t.Fatalf("biz key mismatch: %s", gen.BizKey())
	}
	id1, err := gen.NextID(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	id2, _ := gen.NextID(context.Background())
	if id1 == id2 || !strings.Contains(string(id1), "-") {
		t.Fatalf("uuid ids should be unique strings: %q %q", id1, id2)
	}
	batch, err := gen.NextIDs(context.Background(), 3)
	if err != nil || len(batch) != 3 {
		t.Fatalf("batch uuid mismatch: %v err=%v", batch, err)
	}
}

func TestMemorySegmentAllocatorSeedAndCustomStep(t *testing.T) {
	alloc := NewMemorySegmentAllocator(10)
	alloc.Seed("legacy", 999) // 模拟已有存量数据，下一段从 1000 开始
	gen := NewInt64SegmentGenerator("legacy", alloc)

	id, err := gen.NextID(context.Background())
	if err != nil || id != 1000 {
		t.Fatalf("expected first id 1000 after seed, got %d err=%v", id, err)
	}
}

func TestIdGeneratorRegistryTypedAccess(t *testing.T) {
	registry := NewIdGeneratorRegistry()
	registry.Register(NewInt64SegmentGenerator("counter", NewMemorySegmentAllocator(100)))
	registry.Register(NewFormattedStringSegmentGenerator("order-no", "ORD-%08d", NewMemorySegmentAllocator(100)))
	ctx := context.Background()

	n, err := registry.NextID[int64](ctx, "counter")
	if err != nil || n != 1 {
		t.Fatalf("typed int64 next id mismatch: %d err=%v", n, err)
	}
	s, err := registry.NextID[string](ctx, "order-no")
	if err != nil || s != "ORD-00000001" {
		t.Fatalf("typed string next id mismatch: %q err=%v", s, err)
	}

	// 未注册渠道。
	if _, err := registry.NextID[int64](ctx, "ghost"); err == nil {
		t.Fatal("expected error for unregistered bizKey")
	}
	// 主键类型不匹配。
	if _, err := registry.NextID[string](ctx, "counter"); err == nil {
		t.Fatal("expected error when key type mismatches")
	}
}

// 编译期断言：MemorySegmentAllocator/StoreSegmentAllocator 满足领域端口。
var (
	_ domain.SegmentAllocator = (*MemorySegmentAllocator)(nil)
	_ domain.SegmentAllocator = (*StoreSegmentAllocator)(nil)
)

func TestStoreSegmentAllocatorResolvesStep(t *testing.T) {
	store := &fakeSegmentStore{}
	alloc := NewStoreSegmentAllocator(store, 10)
	alloc.SetStep("big", 100)

	seg1, err := alloc.AllocateNext(context.Background(), "normal")
	if err != nil {
		t.Fatal(err)
	}
	seg2, err := alloc.AllocateNext(context.Background(), "big")
	if err != nil {
		t.Fatal(err)
	}
	if seg1.Max != 10 || seg2.Max != 100 {
		t.Fatalf("step resolution mismatch: normal=%v big=%v", seg1, seg2)
	}
	if got := store.lastBizKey; got != "big" || store.lastStep != 100 {
		t.Fatalf("store invocation mismatch: biz=%s step=%d", got, store.lastStep)
	}
}

type fakeSegmentStore struct {
	lastBizKey string
	lastStep   int
}

func (f *fakeSegmentStore) AllocateSegment(_ context.Context, bizKey string, step int) (int64, int64, error) {
	f.lastBizKey = bizKey
	f.lastStep = step
	low := int64(1)
	return low, low + int64(step) - 1, nil
}

func TestSegmentValueObject(t *testing.T) {
	seg := domain.Segment{Current: 5, Max: 7}
	if seg.Remaining() != 3 {
		t.Fatalf("remaining mismatch: %d", seg.Remaining())
	}
	v, next, err := seg.Take()
	if err != nil || v != 5 || next.Current != 6 {
		t.Fatalf("take mismatch: v=%d next=%v err=%v", v, next, err)
	}
	exhausted := domain.Segment{Current: 8, Max: 7}
	if _, _, err := exhausted.Take(); err != domain.ErrSegmentExhausted {
		t.Fatalf("expected ErrSegmentExhausted, got %v", err)
	}
}
