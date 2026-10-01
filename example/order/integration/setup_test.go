//go:build integration

// Package integration 是 order 示例的真实依赖集成测试。
//
// 仅在显式带上 -tags=integration 时编译运行（见 Makefile 的 test-integration），
// 连接本地 docker-compose 拉起的 MySQL 与 Redis，端到端验证：
// 缓存命中与写后失效、支付幂等拦截、outbox 状态流转、对外广播落库。
//
// 若探测到 MySQL/Redis 不可达，用例整体 t.Skip 而非失败——
// 以便在缺少容器运行时的环境下不产生误导性的红灯。
package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"

	"github.com/dirac-lee/domkit/config"
	"github.com/dirac-lee/domkit/example/order/internal/httpapi"
	"github.com/dirac-lee/domkit/example/order/internal/order"
	ordermysql "github.com/dirac-lee/domkit/example/order/internal/order/infra/mysql"
)

// 默认连接串：与 docker-compose 暴露的端口/账号一致，可用环境变量覆盖。
const (
	defaultMySQLDSN  = "order:order@tcp(127.0.0.1:3306)/order?charset=utf8mb4&parseTime=True&loc=Local"
	defaultRedisAddr = "127.0.0.1:6379"

	envMySQLDSN  = "ORDER_TEST_MYSQL_DSN"
	envRedisAddr = "ORDER_TEST_REDIS_ADDR"
	probeTimeout = 3 * time.Second
)

// testEnv 承载一次集成测试所需的全部句柄：
// app 走真实装配、gorm/redis 为独立连接（仅用于断言旁路）、server 为 httptest 路由。
type testEnv struct {
	app    *order.Application
	gorm   *gorm.DB
	redis  *redis.Client
	server *httptest.Server
}

// setup 探活真实依赖后装配应用并起 HTTP 路由；依赖不可达则跳过。
func setup(t *testing.T) *testEnv {
	t.Helper()

	dsn := envOr(envMySQLDSN, defaultMySQLDSN)
	redisAddr := envOr(envRedisAddr, defaultRedisAddr)

	gdb := probeMySQL(t, dsn)
	rdb := probeRedis(t, redisAddr)

	app := newRealApplication(t, dsn, redisAddr)
	t.Cleanup(func() {
		if err := app.Close(); err != nil {
			t.Errorf("关闭应用失败: %v", err)
		}
	})
	server := httptest.NewServer(httpapi.NewRouter(app, log.Default()))
	t.Cleanup(server.Close)

	return &testEnv{app: app, gorm: gdb, redis: rdb, server: server}
}

func (e *testEnv) cleanupOrderData(t *testing.T, id string) {
	t.Helper()

	// 清理顺序从派生数据到主表，避免后续增加外键时产生约束问题。
	operations := []struct {
		name string
		run  func() error
	}{
		{"Redis summary", func() error {
			return e.redis.Del(context.Background(), "summary:"+id).Err()
		}},
		{"broadcast records", func() error {
			return e.gorm.Where("aggregate_id = ?", id).Delete(&ordermysql.BroadcastRecordPO{}).Error
		}},
		{"outbox messages", func() error {
			return e.gorm.Where("aggregate_id = ?", id).Delete(&ordermysql.OutboxMessagePO{}).Error
		}},
		{"order summaries", func() error {
			return e.gorm.Where("id = ?", id).Delete(&ordermysql.OrderSummaryPO{}).Error
		}},
		{"orders", func() error {
			return e.gorm.Where("id = ?", id).Delete(&ordermysql.OrderPO{}).Error
		}},
	}
	for _, op := range operations {
		if err := op.run(); err != nil {
			t.Errorf("清理 %s 失败: %v", op.name, err)
		}
	}
}

// probeMySQL 打开独立连接并 Ping；不可达时 Skip。连接交由测试结束时关闭。
func probeMySQL(t *testing.T, dsn string) *gorm.DB {
	t.Helper()

	gdb, err := gorm.Open(mysql.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Skipf("MySQL 不可达，跳过集成测试: %v", err)
	}
	sqlDB, err := gdb.DB()
	if err != nil {
		t.Skipf("获取 MySQL 底层连接失败: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), probeTimeout)
	defer cancel()
	if err := sqlDB.PingContext(ctx); err != nil {
		t.Skipf("MySQL Ping 失败，跳过集成测试: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	return gdb
}

// probeRedis 创建独立客户端并 Ping；不可达时 Skip。
func probeRedis(t *testing.T, addr string) *redis.Client {
	t.Helper()

	rdb := redis.NewClient(&redis.Options{Addr: addr})
	ctx, cancel := context.WithTimeout(context.Background(), probeTimeout)
	defer cancel()
	if err := rdb.Ping(ctx).Err(); err != nil {
		t.Skipf("Redis 不可达，跳过集成测试: %v", err)
	}
	t.Cleanup(func() { _ = rdb.Close() })
	return rdb
}

// newRealApplication 用真实 MySQL/Redis 配置装配应用；装配失败直接终止。
func newRealApplication(t *testing.T, dsn, redisAddr string) *order.Application {
	t.Helper()

	src := config.NewMapSource().
		Set("order.http-addr", ":0").
		Set("order.mysql.dsn", dsn).
		Set("order.redis.addr", redisAddr)
	app, err := order.NewApplication(config.NewContext(src))
	if err != nil {
		t.Fatalf("装配真实应用失败: %v", err)
	}
	return app
}

// doJSON 发起一次 HTTP 调用并解析统一响应体；body 为 nil 表示无请求体。
// 返回状态码与解包后的 JSON（响应体为空时为 nil）。
func doJSON(t *testing.T, method, url string, body any) (int, map[string]any) {
	t.Helper()

	req, err := http.NewRequest(method, url, encodeBody(t, body))
	if err != nil {
		t.Fatalf("构造请求失败: %v", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("发起请求失败: %v", err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("读取响应失败: %v", err)
	}
	var out map[string]any
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &out)
	}
	return resp.StatusCode, out
}

// encodeBody 把可选请求体序列化为 JSON；为空则返回 nil Reader。
func encodeBody(t *testing.T, body any) io.Reader {
	t.Helper()
	if body == nil {
		return nil
	}
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("序列化请求体失败: %v", err)
	}
	return bytes.NewReader(raw)
}

// envOr 读取环境变量，缺失时回退默认值。
func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
