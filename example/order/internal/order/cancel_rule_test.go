package order

import (
	"context"
	"strings"
	"testing"
)

// TestCancelPaidOrderReportsCancelRule 回归测试：取消「已支付」订单时，
// 必须返回「取消」专属规则文案，而不能复用支付模板出现“可以支付”。
// 与组合根同包，直接走 NewApplication 的端到端链路。
func TestCancelPaidOrderReportsCancelRule(t *testing.T) {
	ctx := context.Background()
	app := newTestApplication(t)

	// 1. 建单并支付，使订单进入 paid、版本为 2。
	o, err := app.Commands.Create(ctx, "NO-CANCEL-1", 200)
	if err != nil {
		t.Fatalf("创建订单失败: %v", err)
	}
	if _, err := app.Commands.Pay(ctx, o.ID); err != nil {
		t.Fatalf("支付失败: %v", err)
	}

	// 2. 取消已支付订单：应被规则拦截，文案动词必须是“取消”。
	if _, err := app.Commands.Cancel(ctx, o.ID); err == nil {
		t.Fatal("取消已支付订单应返回规则错误")
	} else {
		msg := err.Error()
		if !strings.Contains(msg, "仅 created 状态可以取消") {
			t.Fatalf("错误文案应提示“可以取消”，实际：%s", msg)
		}
		if strings.Contains(msg, "可以支付") {
			t.Fatalf("取消场景不应出现支付文案，实际：%s", msg)
		}
	}

	// 3. 规则拦截不落库：订单仍为 paid、版本仍停留在 2。
	got, err := app.Commands.Get(ctx, o.ID)
	if err != nil {
		t.Fatalf("重新查询订单失败: %v", err)
	}
	if got.Status.Value() != "paid" || got.CurrentVersion() != 2 {
		t.Fatalf("拦截后订单不应被改动，实际 status=%s version=%d", got.Status, got.CurrentVersion())
	}
}
