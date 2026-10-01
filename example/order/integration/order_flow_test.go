//go:build integration

package integration

import (
	"context"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/dirac-lee/domkit/example/order/internal/order/infra/mysql"
)

// TestOrderLifecycleOutboxAndBroadcast 验证建单→支付的完整写链路：
// 写模型版本推进、读侧投影同步、outbox 即时发布为 sent、对外广播恰好落库 1 条。
func TestOrderLifecycleOutboxAndBroadcast(t *testing.T) {
	env := setup(t)
	id := createOrder(t, env)

	// 写模型：版本 1、状态 created。
	assertOrderRow(t, env, id, "created", 1)
	// outbox：order.created 在提交后已即时发布为 sent。
	assertOutboxStatus(t, env, id, "order.created", "sent")

	// 支付后：写模型推进到版本 2，读侧投影同步到版本 2。
	payStatus, _ := doJSON(t, http.MethodPost, env.server.URL+"/orders/"+id+"/pay", nil)
	if payStatus != http.StatusOK {
		t.Fatalf("支付应返回 200，实际 %d", payStatus)
	}
	assertOrderRow(t, env, id, "paid", 2)
	assertSummaryVersion(t, env, id, 2)

	// outbox：order.paid 同样即时发布；广播恰好 1 条（建单不广播）。
	assertOutboxStatus(t, env, id, "order.paid", "sent")
	assertBroadcastCount(t, env, id, 1)

	// 对外广播查询接口应能读到这 1 条留痕。
	queryStatus, body := doJSON(t, http.MethodGet, env.server.URL+"/orders/"+id+"/broadcasts", nil)
	if queryStatus != http.StatusOK {
		t.Fatalf("查询广播应返回 200，实际 %d", queryStatus)
	}
	if dataLen(body) != 1 {
		t.Fatalf("广播接口应返回 1 条，实际 %v", body["data"])
	}
}

// TestSummaryCacheFillAndInvalidate 验证概要缓存的 cache-aside 语义：
// 首次查询回源并回填 Redis；支付写后投影订阅主动失效缓存。
func TestSummaryCacheFillAndInvalidate(t *testing.T) {
	env := setup(t)
	id := createOrder(t, env)
	key := "summary:" + id

	// 首次详情查询：缓存未命中 → 读投影表 → 成功后回填 Redis。
	status, _ := doJSON(t, http.MethodGet, env.server.URL+"/orders/"+id, nil)
	if status != http.StatusOK {
		t.Fatalf("详情查询应返回 200，实际 %d", status)
	}
	if env.redis.Exists(context.Background(), key).Val() != 1 {
		t.Fatal("首次查询后概要应已回填 Redis")
	}

	// 支付触发投影 Sync 后 Invalidate，缓存键应被删除，避免脏读。
	payStatus, _ := doJSON(t, http.MethodPost, env.server.URL+"/orders/"+id+"/pay", nil)
	if payStatus != http.StatusOK {
		t.Fatalf("支付应返回 200，实际 %d", payStatus)
	}
	if env.redis.Exists(context.Background(), key).Val() != 0 {
		t.Fatal("支付写后应失效概要缓存")
	}
}

// TestDuplicatePayBlocked 验证支付幂等守卫：支付成功后的 TTL 窗口内，
// 重复支付在进入用例前即被拦截为 409。
func TestDuplicatePayBlocked(t *testing.T) {
	env := setup(t)
	id := createOrder(t, env)

	first, _ := doJSON(t, http.MethodPost, env.server.URL+"/orders/"+id+"/pay", nil)
	if first != http.StatusOK {
		t.Fatalf("首次支付应返回 200，实际 %d", first)
	}
	second, _ := doJSON(t, http.MethodPost, env.server.URL+"/orders/"+id+"/pay", nil)
	if second != http.StatusConflict {
		t.Fatalf("窗口内重复支付应返回 409，实际 %d", second)
	}
}

// createOrder 通过 HTTP 建单并返回新订单 ID（订单号用纳秒时间戳保证唯一）。
func createOrder(t *testing.T, env *testEnv) string {
	t.Helper()

	orderNo := fmt.Sprintf("IT-%d", time.Now().UnixNano())
	status, body := doJSON(t, http.MethodPost, env.server.URL+"/orders",
		map[string]any{"orderNo": orderNo, "amount": 100})
	if status != http.StatusCreated {
		t.Fatalf("建单应返回 201，实际 %d，body=%v", status, body)
	}

	data, ok := body["data"].(map[string]any)
	if !ok {
		t.Fatalf("响应缺少 data 对象: %v", body)
	}
	id, ok := data["id"].(string)
	if !ok || id == "" {
		t.Fatalf("响应 data 缺少有效 id: %v", body)
	}
	t.Cleanup(func() { env.cleanupOrderData(t, id) })
	return id
}

// assertOrderRow 断言写模型 orders 中该订单的状态与版本。
func assertOrderRow(t *testing.T, env *testEnv, id, status string, version uint64) {
	t.Helper()

	var po mysql.OrderPO
	if err := env.gorm.Where("id = ?", id).First(&po).Error; err != nil {
		t.Fatalf("查询 orders 失败: %v", err)
	}
	if po.Status != status || po.Version != version {
		t.Fatalf("写模型期望 status=%s version=%d，实际 status=%s version=%d",
			status, version, po.Status, po.Version)
	}
}

// assertSummaryVersion 断言读侧投影 order_summaries 已物化到指定版本。
func assertSummaryVersion(t *testing.T, env *testEnv, id string, version uint64) {
	t.Helper()

	var po mysql.OrderSummaryPO
	if err := env.gorm.Where("id = ?", id).First(&po).Error; err != nil {
		t.Fatalf("查询 order_summaries 失败: %v", err)
	}
	if po.Version != version {
		t.Fatalf("投影版本期望 %d，实际 %d", version, po.Version)
	}
}

// assertOutboxStatus 断言指定聚合事件的 outbox 记录已流转到目标状态。
func assertOutboxStatus(t *testing.T, env *testEnv, id, eventName, wantStatus string) {
	t.Helper()

	var po mysql.OutboxMessagePO
	err := env.gorm.Where("aggregate_id = ? AND event_name = ?", id, eventName).First(&po).Error
	if err != nil {
		t.Fatalf("查询 outbox[%s] 失败: %v", eventName, err)
	}
	if po.Status != wantStatus {
		t.Fatalf("outbox[%s] 状态期望 %s，实际 %s", eventName, wantStatus, po.Status)
	}
}

// assertBroadcastCount 断言该订单的对外广播留痕条数恰好为 want。
func assertBroadcastCount(t *testing.T, env *testEnv, id string, want int64) {
	t.Helper()

	var got int64
	err := env.gorm.Model(&mysql.BroadcastRecordPO{}).
		Where("aggregate_id = ? AND topic = ? AND sender_code = ?", id, "order-paid", "order-service").
		Count(&got).Error
	if err != nil {
		t.Fatalf("统计 broadcast_records 失败: %v", err)
	}
	if got != want {
		t.Fatalf("广播条数期望 %d，实际 %d", want, got)
	}
}

// dataLen 读取统一响应 data（本接口为数组）的长度；缺失或非数组返回 -1。
func dataLen(body map[string]any) int {
	list, ok := body["data"].([]any)
	if !ok {
		return -1
	}
	return len(list)
}
