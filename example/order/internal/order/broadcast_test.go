package order

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/dirac-lee/domkit/broadcast"
)

// TestPaymentBroadcastsEnvelope 端到端验证：支付订单后对外广播一条「订单已支付」信封。
// 与组合根同包，直接调用 NewApplication，覆盖装配 → 支付 → 事件分发 → 广播的完整链路。
func TestPaymentBroadcastsEnvelope(t *testing.T) {
	ctx := context.Background()
	app := newTestApplication(t)

	// 1. 创建并支付订单。
	o, err := app.Commands.Create(ctx, "NO-BC-1", 8800)
	if err != nil {
		t.Fatalf("创建订单失败: %v", err)
	}
	if _, err := app.Commands.Pay(ctx, o.ID); err != nil {
		t.Fatalf("支付失败: %v", err)
	}

	// 2. 创建事件不广播，支付后应恰有 1 条（若创建也广播会得到 2 条）。
	records, err := app.Broadcasts.ByAggregate(ctx, string(o.ID))
	if err != nil {
		t.Fatalf("查询广播记录失败: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("支付后应恰有 1 条广播，实际 %d", len(records))
	}

	// 3. 核对发送目标与信封内容。
	rec := records[0]
	if rec.Topic != "order-paid" || rec.SenderCode != "order-service" {
		t.Fatalf("目标 topic / 发送方错误: %+v", rec)
	}

	var env broadcast.Envelope[OrderPaidNotice]
	if err := json.Unmarshal([]byte(rec.Envelope), &env); err != nil {
		t.Fatalf("信封 JSON 非法: %v", err)
	}
	// 支付后聚合版本为 2，成因操作 PAY。
	if env.AggregateID != string(o.ID) || env.Version != 2 || env.CauseOperation != "PAY" {
		t.Fatalf("信封元数据错误: %+v", env)
	}
	if env.AggregateType != "Order" || env.SchemaVersion != 1 {
		t.Fatalf("聚合类型 / 协议版本错误: %+v", env)
	}
	if env.MessageID == "" || env.SourceEventID == "" {
		t.Fatal("消息标识 / 来源事件标识不应为空")
	}
	// 消息体来自支付后重新加载的写模型。
	if env.Payload.OrderNo != "NO-BC-1" || env.Payload.Amount != 8800 {
		t.Fatalf("消息体业务字段错误: %+v", env.Payload)
	}
	// paidAt 必须是合法且非零的 RFC3339 时间，防止回退成零值时间。
	paidAt, err := time.Parse(time.RFC3339, env.Payload.PaidAt)
	if err != nil {
		t.Fatalf("paidAt 非合法 RFC3339: %v", err)
	}
	if paidAt.IsZero() {
		t.Fatal("paidAt 不应为零值时间")
	}
}
