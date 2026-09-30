package reconciliation

import (
	"context"
	"errors"
	"testing"
)

// onceDedup 处理一次后即跳过的去重实现：同键第二次起 ShouldSkip 返回 true。
type onceDedup struct {
	seen map[string]struct{}
}

func newOnceDedup() *onceDedup {
	return &onceDedup{seen: map[string]struct{}{}}
}

// dedupKey 拼接副本与聚合 ID，作为去重窗口内的唯一键。
func (d *onceDedup) dedupKey(replicaID string, id string) string {
	return replicaID + "|" + id
}

func (d *onceDedup) ShouldSkip(replicaID string, id string) bool {
	_, ok := d.seen[d.dedupKey(replicaID, id)]
	return ok
}

func (d *onceDedup) Mark(replicaID string, id string) {
	d.seen[d.dedupKey(replicaID, id)] = struct{}{}
}

// 注册两个副本后对账：应返回每副本一个结果，且均按「副本落后」触发重建。
func TestManagerReconcileAllReplicas(t *testing.T) {
	reader := &fakeVersionReader{version: 1, exists: true}
	manager := NewManager(reader).
		RegisterReplica(&fakeReplica[string]{id: "es", readV: 0}).
		RegisterReplica(&fakeReplica[string]{id: "redis", readV: 0})

	results, err := manager.Reconcile(context.Background(), "a1")
	if err != nil {
		t.Fatalf("Reconcile 失败: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("结果数 = %d，期望 2", len(results))
	}
	if results["es"].Status != StatusStale || results["redis"].Status != StatusStale {
		t.Fatalf("两副本均应 STALE，得到 %+v", results)
	}
}

// 单副本对账；未注册副本返回 ErrReplicaNotFound。
func TestManagerReconcileReplica(t *testing.T) {
	reader := &fakeVersionReader{version: 1, exists: true}
	manager := NewManager(reader).
		RegisterReplica(&fakeReplica[string]{id: "es", readV: 1})

	r, err := manager.ReconcileReplica(context.Background(), "es", "a1")
	if err != nil || !r.IsConsistent() {
		t.Fatalf("期望 CONSISTENT,nil，得到 %+v,%v", r, err)
	}
	if _, err := manager.ReconcileReplica(context.Background(), "missing", "a1"); !errors.Is(err, ErrReplicaNotFound) {
		t.Fatalf("未注册副本错误 = %v，期望 ErrReplicaNotFound", err)
	}
}

// 批量对账：结果按聚合 ID 分组。
func TestManagerReconcileBatch(t *testing.T) {
	reader := &fakeVersionReader{version: 1, exists: true}
	manager := NewManager(reader).
		RegisterReplica(&fakeReplica[string]{id: "es", readV: 1})

	results, err := manager.ReconcileBatch(context.Background(), []string{"a1", "a2"})
	if err != nil {
		t.Fatalf("ReconcileBatch 失败: %v", err)
	}
	if len(results) != 2 || len(results["a1"]) != 1 || len(results["a2"]) != 1 {
		t.Fatalf("批量结果应含两个聚合、各一副本，得到 %+v", results)
	}
}

// 去重：同一聚合第二次对账时，已标记副本应被全部跳过、结果为空。
func TestManagerDedupSkipsSecondRun(t *testing.T) {
	reader := &fakeVersionReader{version: 1, exists: true}
	manager := NewManager(reader).
		SetDedup(newOnceDedup()).
		RegisterReplica(&fakeReplica[string]{id: "es", readV: 1})

	if _, err := manager.Reconcile(context.Background(), "a1"); err != nil {
		t.Fatalf("首次对账失败: %v", err)
	}
	second, err := manager.Reconcile(context.Background(), "a1")
	if err != nil {
		t.Fatalf("二次对账失败: %v", err)
	}
	if len(second) != 0 {
		t.Fatalf("去重窗口内二次对账结果应为空，得到 %+v", second)
	}
}
