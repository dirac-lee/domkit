package readmodel

import (
	"context"
	"errors"
	"testing"

	"github.com/dirac-lee/domkit/domain"
)

// errTestLoad 模拟写模型加载时的真实失败。
var errTestLoad = errors.New("load failed")

// ---- 测试夹具 ----

// testAgg 测试聚合，内嵌 AggregateRoot 自动满足 AggregateWithVersion。
type testAgg struct {
	domain.AggregateRoot[string]
	Name string
}

// testView 测试投影。
type testView struct{ id string }

func (testView) ProjectionKind() string { return "testView" }

// projectAgg 聚合 → 投影 的纯映射器。
func projectAgg(a *testAgg) testView { return testView{id: a.ID} }

// fakeLoader 可控加载器：命中 data 返回聚合，未命中返回 ErrAggregateNotFound，
// boom=true 时返回真实错误。
type fakeLoader struct {
	data map[string]*testAgg
	boom bool
}

func (l *fakeLoader) load(_ context.Context, id string) (*testAgg, error) {
	if l.boom {
		return nil, errTestLoad
	}
	if a, ok := l.data[id]; ok {
		return a, nil
	}
	return nil, ErrAggregateNotFound
}

// newTestReplica 用给定加载器装配测试副本。
func newTestReplica(loader *fakeLoader) *MemoryReplica[string, testView] {
	return NewMemoryReplica("test", loader.load, projectAgg)
}

// ---- 用例 ----

// 物化后应能读到投影与版本。
func TestMemoryReplicaSyncMaterializes(t *testing.T) {
	loader := &fakeLoader{data: map[string]*testAgg{
		"a1": {AggregateRoot: domain.AggregateRoot[string]{ID: "a1", Version: 2}},
	}}
	replica := newTestReplica(loader)

	if err := replica.Sync(context.Background(), "a1"); err != nil {
		t.Fatalf("Sync 返回错误: %v", err)
	}
	ver, err := replica.ReadVersion(context.Background(), "a1")
	if err != nil || ver != 2 {
		t.Fatalf("ReadVersion = %d, err=%v；期望 2,nil", ver, err)
	}
	if _, ok := replica.GetByID(context.Background(), "a1"); !ok {
		t.Fatal("GetByID 未命中已物化条目")
	}
}

// 写模型未找到时应清理残留，版本回落为 0。
func TestMemoryReplicaSyncPurgesOrphan(t *testing.T) {
	loader := &fakeLoader{data: map[string]*testAgg{}}
	replica := newTestReplica(loader)
	// 预置一条残留。
	replica.upsert("a1", testView{id: "a1"}, 5)

	if err := replica.Sync(context.Background(), "a1"); err != nil {
		t.Fatalf("Sync 返回错误: %v", err)
	}
	ver, _ := replica.ReadVersion(context.Background(), "a1")
	if ver != 0 {
		t.Fatalf("清理后 ReadVersion = %d，期望 0", ver)
	}
}

// 加载真实失败应原样传播，且不改动副本。
func TestMemoryReplicaSyncPropagatesLoadError(t *testing.T) {
	loader := &fakeLoader{boom: true}
	replica := newTestReplica(loader)

	if !errors.Is(replica.Sync(context.Background(), "a1"), errTestLoad) {
		t.Fatal("期望传播 errTestLoad")
	}
}

// 分页：3 条数据、页大小 2，首页 2 条、次页 1 条，总条数恒为 3。
func TestMemoryReplicaPage(t *testing.T) {
	loader := &fakeLoader{data: map[string]*testAgg{
		"a1": {AggregateRoot: domain.AggregateRoot[string]{ID: "a1"}},
		"a2": {AggregateRoot: domain.AggregateRoot[string]{ID: "a2"}},
		"a3": {AggregateRoot: domain.AggregateRoot[string]{ID: "a3"}},
	}}
	replica := newTestReplica(loader)
	for id := range loader.data {
		if err := replica.Sync(context.Background(), id); err != nil {
			t.Fatalf("Sync %s 失败: %v", id, err)
		}
	}

	first := replica.Page(context.Background(), mustPageRequest(t, 1, 2))
	if len(first.Data()) != 2 || first.TotalCount() != 3 {
		t.Fatalf("首页 data=%d total=%d，期望 2,3", len(first.Data()), first.TotalCount())
	}
	second := replica.Page(context.Background(), mustPageRequest(t, 2, 2))
	if len(second.Data()) != 1 {
		t.Fatalf("次页 data=%d，期望 1", len(second.Data()))
	}
}

// NewPageRequest 边界校验。
func TestNewPageRequestInvalid(t *testing.T) {
	if _, err := NewPageRequest(0, 10); err == nil {
		t.Fatal("页码 0 应报错")
	}
	if _, err := NewPageRequest(1, 201); err == nil {
		t.Fatal("页大小 201 应报错")
	}
}

// mustPageRequest 构造分页请求，失败即终止测试。
func mustPageRequest(t *testing.T, number, size int) PageRequest {
	t.Helper()
	req, err := NewPageRequest(number, size)
	if err != nil {
		t.Fatalf("构造分页请求失败: %v", err)
	}
	return req
}
