package reconciliation

import (
	"context"
	"errors"
	"testing"
)

// ---- 测试夹具 ----

// fakeVersionReader 可控写模型版本读取器。
type fakeVersionReader struct {
	version uint64
	exists  bool
	err     error
}

func (f *fakeVersionReader) CurrentVersion(context.Context, string) (uint64, bool, error) {
	return f.version, f.exists, f.err
}

// fakeReplica 可控读副本，记录被重建/清理的次数与目标 ID。
type fakeReplica[ID comparable] struct {
	id        string
	readV     int64
	fail      error
	rebuilt   int
	purged    int
	targetID  ID
}

func (f *fakeReplica[ID]) ReplicaID() string { return f.id }

func (f *fakeReplica[ID]) ReadVersion(context.Context, ID) (int64, error) {
	return f.readV, f.fail
}

func (f *fakeReplica[ID]) Rebuild(_ context.Context, id ID) error {
	f.rebuilt++
	f.targetID = id
	return f.fail
}

func (f *fakeReplica[ID]) PurgeOrphan(_ context.Context, id ID) error {
	f.purged++
	f.targetID = id
	return f.fail
}

// ---- Judge 纯函数：覆盖四种状态（表驱动） ----

func TestJudge(t *testing.T) {
	cases := []struct {
		name           string
		readV, writeV  int64
		want           Status
	}{
		{"副本未追踪", -1, 5, StatusUntracked},
		{"写模型缺失-孤儿", 0, -1, StatusOrphan},
		{"版本相等-一致", 3, 3, StatusConsistent},
		{"副本超前-一致", 4, 3, StatusConsistent},
		{"副本落后-陈旧", 2, 3, StatusStale},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := Judge(c.readV, c.writeV); got.Status != c.want {
				t.Fatalf("Judge(%d,%d) = %s，期望 %s", c.readV, c.writeV, got.Status, c.want)
			}
		})
	}
}

// ---- Reconcile 仅检测 ----

// 一致：不触发任何补救。
func TestReconcileConsistent(t *testing.T) {
	reader := &fakeVersionReader{version: 2, exists: true}
	replica := &fakeReplica[string]{id: "r1", readV: 2}

	r, err := Reconcile(context.Background(), replica, reader, "a1")
	if err != nil || !r.IsConsistent() {
		t.Fatalf("Reconcile = %+v,%v，期望 CONSISTENT,nil", r, err)
	}
	if replica.rebuilt != 0 || replica.purged != 0 {
		t.Fatal("仅检测不应触发重建/清理")
	}
}

// 写模型不存在：应判 ORPHAN，且仅检测不触发清理。
func TestReconcileOrphanDetection(t *testing.T) {
	reader := &fakeVersionReader{exists: false}
	replica := &fakeReplica[string]{id: "r1", readV: 0}

	r, err := Reconcile(context.Background(), replica, reader, "ghost")
	if err != nil || !r.IsOrphan() {
		t.Fatalf("Reconcile = %+v,%v，期望 ORPHAN,nil", r, err)
	}
	if replica.purged != 0 {
		t.Fatal("仅检测不应触发清理")
	}
}

// ---- ReconcileAndResync 检测 + 补救 ----

// STALE → 调用 Rebuild。
func TestReconcileAndResyncStale(t *testing.T) {
	reader := &fakeVersionReader{version: 2, exists: true}
	replica := &fakeReplica[string]{id: "r1", readV: 1}

	r, err := ReconcileAndResync(context.Background(), replica, reader, "a1")
	if err != nil || !r.IsStale() {
		t.Fatalf("期望 STALE,nil，得到 %+v,%v", r, err)
	}
	if replica.rebuilt != 1 || replica.targetID != "a1" {
		t.Fatal("STALE 应重建目标聚合")
	}
}

// ORPHAN → 调用 PurgeOrphan。
func TestReconcileAndResyncOrphan(t *testing.T) {
	reader := &fakeVersionReader{exists: false}
	replica := &fakeReplica[string]{id: "r1", readV: 0}

	r, err := ReconcileAndResync(context.Background(), replica, reader, "ghost")
	if err != nil || !r.IsOrphan() {
		t.Fatalf("期望 ORPHAN,nil，得到 %+v,%v", r, err)
	}
	if replica.purged != 1 {
		t.Fatal("ORPHAN 应清理残留")
	}
}

// UNTRACKED：既不重建也不清理。
func TestReconcileAndResyncUntracked(t *testing.T) {
	reader := &fakeVersionReader{version: 2, exists: true}
	replica := &fakeReplica[string]{id: "r1", readV: -1}

	r, _ := ReconcileAndResync(context.Background(), replica, reader, "a1")
	if !r.IsUntracked() || replica.rebuilt != 0 || replica.purged != 0 {
		t.Fatal("UNTRACKED 不应触发任何补救")
	}
}

// 重建失败应原样传播错误。
func TestReconcileAndResyncRebuildError(t *testing.T) {
	errRebuild := errors.New("rebuild failed")
	reader := &fakeVersionReader{version: 2, exists: true}
	replica := &fakeReplica[string]{id: "r1", readV: 1, fail: errRebuild}

	if _, err := ReconcileAndResync(context.Background(), replica, reader, "a1"); !errors.Is(err, errRebuild) {
		t.Fatalf("期望传播 errRebuild，得到 %v", err)
	}
}
