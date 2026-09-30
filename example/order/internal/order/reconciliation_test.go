package order

import (
	"context"
	"testing"
)

// TestReconciliationSelfHealsStaleReplica 端到端对账自愈（真实内存仓储 + 真实副本 + 真实管理器）。
// 与组合根同包，直接调用 NewApplication，验证从装配到自愈的完整链路。
func TestReconciliationSelfHealsStaleReplica(t *testing.T) {
	ctx := context.Background()
	app := newTestApplication(t)

	// 1. 创建订单：事件投影已让副本物化，readV=1。
	o, err := app.Commands.Create(ctx, "NO-1", 100)
	if err != nil {
		t.Fatalf("创建订单失败: %v", err)
	}
	id := o.ID

	// 2. 模拟读侧条目丢失（如投影写失败/读存储损坏）：删除副本条目。
	if err := app.ReadModel.PurgeOrphan(ctx, id); err != nil {
		t.Fatalf("清理副本条目失败: %v", err)
	}
	if _, ok, qerr := app.ReadModel.GetByID(ctx, id); qerr != nil {
		t.Fatalf("查询副本失败: %v", qerr)
	} else if ok {
		t.Fatal("模拟丢失后副本应查不到该条目")
	}

	// 3. 对账：应检测为 STALE（readV=0 < writeV=1），并立即触发重建。
	results, err := app.Reconciler.Reconcile(ctx, id)
	if err != nil {
		t.Fatalf("对账失败: %v", err)
	}
	r := results["memory:order-summary"]
	if !r.IsStale() {
		t.Fatalf("期望检测为 STALE，得到 %s", r.Status)
	}

	// 4. 自愈后副本条目恢复、版本回到写模型版本 1。
	if _, ok, qerr := app.ReadModel.GetByID(ctx, id); qerr != nil {
		t.Fatalf("查询副本失败: %v", qerr)
	} else if !ok {
		t.Fatal("自愈后副本条目应已重建恢复")
	}
	readV, _ := app.ReadModel.ReadVersion(ctx, id)
	if readV != 1 {
		t.Fatalf("自愈后副本版本 = %d，期望 1", readV)
	}
}
