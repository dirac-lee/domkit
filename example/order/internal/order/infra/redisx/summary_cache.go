package redisx

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	orderdomain "github.com/dirac-lee/domkit/example/order/internal/order/domain"
	"github.com/redis/go-redis/v9"
)

// SummaryCache 订单概要缓存：cache-aside 模式——读时未命中回源并回填，写库成功后删缓存。
type SummaryCache struct {
	client *Client
	ttl    time.Duration
}

// NewSummaryCache 创建概要缓存。
func NewSummaryCache(c *Client, ttl time.Duration) *SummaryCache {
	return &SummaryCache{client: c, ttl: ttl}
}

// Get cache-aside 读取：先 Redis，未命中走 loader 查库并回填 TTL。
// loader 从持久层加载概要，未命中返回 (零值, false, nil)。
func (c *SummaryCache) Get(ctx context.Context, id orderdomain.OrderID,
	loader func(context.Context, orderdomain.OrderID) (orderdomain.OrderSummary, bool, error),
) (orderdomain.OrderSummary, bool, error) {
	summary, ok, err := c.readCache(ctx, id)
	if err == nil && ok {
		return summary, true, nil
	}
	// err != nil（Redis 故障/脏数据）时同样降级直查库，不阻断读流程。
	return c.loadAndFill(ctx, id, loader)
}

// readCache 读取并反序列化缓存；redis.Nil 视为正常未命中。
func (c *SummaryCache) readCache(ctx context.Context, id orderdomain.OrderID) (orderdomain.OrderSummary, bool, error) {
	payload, err := c.client.rdb.Get(ctx, summaryKey(id)).Bytes()
	if errors.Is(err, redis.Nil) {
		return orderdomain.OrderSummary{}, false, nil
	}
	if err != nil {
		return orderdomain.OrderSummary{}, false, err
	}
	var summary orderdomain.OrderSummary
	if err := json.Unmarshal(payload, &summary); err != nil {
		return orderdomain.OrderSummary{}, false, err
	}
	return summary, true, nil
}

// loadAndFill 回源查库，命中则异步式回填（此处同步回填，失败仅影响性能不影响正确性）。
func (c *SummaryCache) loadAndFill(ctx context.Context, id orderdomain.OrderID,
	loader func(context.Context, orderdomain.OrderID) (orderdomain.OrderSummary, bool, error),
) (orderdomain.OrderSummary, bool, error) {
	summary, ok, err := loader(ctx, id)
	if err != nil || !ok {
		return summary, ok, err
	}
	_ = c.writeCache(ctx, summary) // 回填失败忽略：下次读仍会回源
	return summary, true, nil
}

// writeCache 序列化并带 TTL 写入缓存。
func (c *SummaryCache) writeCache(ctx context.Context, summary orderdomain.OrderSummary) error {
	payload, err := json.Marshal(summary)
	if err != nil {
		return err
	}
	return c.client.rdb.Set(ctx, summaryKey(summary.ID), payload, c.ttl).Err()
}

// Invalidate 删除缓存，写库成功后由事件订阅者调用（先更新库、后删缓存）。
func (c *SummaryCache) Invalidate(ctx context.Context, id orderdomain.OrderID) error {
	if err := c.client.rdb.Del(ctx, summaryKey(id)).Err(); err != nil {
		return fmt.Errorf("redis: invalidate summary %q failed: %w", id, err)
	}
	return nil
}

// summaryKey 概要缓存键：summary:{id}。
func summaryKey(id orderdomain.OrderID) string {
	return fmt.Sprintf("summary:%s", string(id))
}
