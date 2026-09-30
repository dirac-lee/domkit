package persist

import (
	"context"
	"errors"
	"testing"

	"github.com/dirac-lee/domkit/domain"
)

type item struct {
	domain.AggregateRoot[string]
	Name string
}

func newTestRepo() *MemoryRepository[string, item] {
	return NewMemoryRepository(
		func(i *item) string { return i.ID },
		VersionAccess[item]{
			Current: func(i *item) uint64 { return i.CurrentVersion() },
			Next:    func(i *item) uint64 { return i.PendingVersion() },
			Reload:  func(i *item, v uint64) { i.MarkPersisted(v) },
		},
	)
}

func TestMemoryRepositoryInsertGetAndDuplicate(t *testing.T) {
	repo := newTestRepo()
	ctx := context.Background()
	a := &item{Name: "a"}
	a.ID = "i1"
	a.NextVersion() // 新建操作：待提交版本 1（事件携带的版本）

	if err := repo.Insert(ctx, a); err != nil {
		t.Fatalf("insert failed: %v", err)
	}
	if repo.Len() != 1 {
		t.Fatalf("expected len 1, got %d", repo.Len())
	}
	// 提交成功后推进内存对象基线（模拟 UoW.CommitVersion）。
	a.CommitVersion()

	got, err := repo.GetByID(ctx, "i1")
	if err != nil || got == nil || got.Name != "a" {
		t.Fatalf("get failed: %v %+v", err, got)
	}
	if got.Version != 1 {
		t.Fatalf("reloaded aggregate should carry committed row version 1, got %d", got.Version)
	}
	// 装载副本的瞬态已重建：下一次操作版本应为 2。
	if got.NextVersion() != 2 {
		t.Fatalf("next version after reload should be 2, got %d", got.NextVersion())
	}

	// GetByID 返回防御性副本，修改不应影响仓储内部。
	got.Name = "mutated"
	again, _ := repo.GetByID(ctx, "i1")
	if again.Name != "a" {
		t.Fatal("GetByID must return a defensive copy")
	}

	// 同 ID 重复插入 → 冲突。
	if err := repo.Insert(ctx, a); !domain.IsConcurrencyConflict(err) {
		t.Fatalf("duplicate insert should conflict, got %v", err)
	}

	missing, err := repo.GetByID(ctx, "ghost")
	if err != nil || missing != nil {
		t.Fatalf("missing id should return (nil, nil), got %v %+v", err, missing)
	}
	v, exists, err := repo.CurrentVersion(ctx, "ghost")
	if err != nil || exists || v != 0 {
		t.Fatalf("CurrentVersion missing: v=%d exists=%v err=%v", v, exists, err)
	}
}

func TestMemoryRepositoryUpdateCAS(t *testing.T) {
	repo := newTestRepo()
	ctx := context.Background()
	a := &item{Name: "v1"}
	a.ID = "i1"
	a.NextVersion() // v1
	if err := repo.Insert(ctx, a); err != nil {
		t.Fatal(err)
	}
	a.CommitVersion() // 基线 -> 1

	// 第二次业务操作：修改 + 新版本 2。
	a.Name = "v2"
	a.NextVersion()
	if err := repo.Update(ctx, a); err != nil {
		t.Fatalf("cas update with matching baseline should succeed: %v", err)
	}
	a.CommitVersion()
	stored, _ := repo.GetByID(ctx, "i1")
	if stored.Name != "v2" || stored.Version != 2 {
		t.Fatalf("updated content/version mismatch: %+v", stored)
	}

	// 过期副本（持有 v1 基线，未参与 v2 更新）更新 → 冲突，actual=2。
	oldView := &item{Name: "stale"}
	oldView.ID = "i1"
	oldView.MarkPersisted(1) // 持有的过期基线
	err := repo.Update(ctx, oldView)
	var ce *domain.ConcurrencyConflictError[string]
	if !errors.As(err, &ce) {
		t.Fatalf("expected ConcurrencyConflictError, got %v", err)
	}
	if ce.Expected != 1 || ce.Actual != 2 || ce.AggregateMissing {
		t.Fatalf("conflict detail mismatch: %+v", ce)
	}
}

func TestMemoryRepositoryDeleteAndOrphan(t *testing.T) {
	repo := newTestRepo()
	ctx := context.Background()
	a := &item{Name: "a"}
	a.ID = "i1"
	a.NextVersion()
	if err := repo.Insert(ctx, a); err != nil {
		t.Fatal(err)
	}
	a.CommitVersion() // 基线 1 == 行版本 1

	if err := repo.Delete(ctx, a); err != nil {
		t.Fatalf("delete with current version failed: %v", err)
	}
	if repo.Len() != 0 {
		t.Fatal("aggregate should be removed")
	}

	// 再次删除 → ORPHAN 冲突。
	err := repo.Delete(ctx, a)
	var ce *domain.ConcurrencyConflictError[string]
	if !errors.As(err, &ce) || !ce.AggregateMissing {
		t.Fatalf("expected AggregateMissing conflict, got %v", err)
	}
}

// ---- int64 主键聚合（数据库自增/号段风格），与 string 系仓储同构 ----

type counter struct {
	domain.AggregateRoot[int64]
	Value int
}

func newInt64Repo() *MemoryRepository[int64, counter] {
	return NewMemoryRepository(
		func(c *counter) int64 { return c.ID },
		VersionAccess[counter]{
			Current: func(c *counter) uint64 { return c.CurrentVersion() },
			Next:    func(c *counter) uint64 { return c.PendingVersion() },
			Reload:  func(c *counter, v uint64) { c.MarkPersisted(v) },
		},
	)
}

var _ domain.Repository[int64, counter] = (*MemoryRepository[int64, counter])(nil)

func TestMemoryRepositoryInt64IDLifecycle(t *testing.T) {
	repo := newInt64Repo()
	ctx := context.Background()

	// 自增主键由仓储外部（分配器）在 Insert 前写入。
	c := &counter{Value: 1}
	c.ID = 1
	c.NextVersion()
	if err := repo.Insert(ctx, c); err != nil {
		t.Fatalf("insert int64 aggregate failed: %v", err)
	}
	c.CommitVersion()

	got, err := repo.GetByID(ctx, 1)
	if err != nil || got == nil || got.ID != 1 || got.Version != 1 {
		t.Fatalf("get int64 aggregate mismatch: %+v err=%v", got, err)
	}

	// 把行版本推进到 2，再用持有 v1 基线的过期副本制造冲突；错误详情携带 int64 主键。
	c.NextVersion()
	if err := repo.Update(ctx, c); err != nil {
		t.Fatalf("update to v2 failed: %v", err)
	}
	c.CommitVersion()

	stale := &counter{Value: 9}
	stale.ID = 1
	stale.MarkPersisted(1)
	err = repo.Update(ctx, stale)
	var ce *domain.ConcurrencyConflictError[int64]
	if !errors.As(err, &ce) {
		t.Fatalf("expected ConcurrencyConflictError[int64], got %v", err)
	}
	if ce.AggregateID != 1 || ce.Actual != 2 || ce.Expected != 1 {
		t.Fatalf("int64 conflict detail mismatch: %+v", ce)
	}
}
