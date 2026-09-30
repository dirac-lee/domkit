package redisx

import (
	"context"
	"fmt"
	"time"

	orderdomain "github.com/dirac-lee/domkit/example/order/internal/order/domain"
)

// PayIdempotencyGuard 支付幂等守卫：在 TTL 时间窗内用 SET NX EX 拦截重复支付。
type PayIdempotencyGuard struct {
	client *Client
	ttl    time.Duration
}

// NewPayIdempotencyGuard 创建支付守卫。
func NewPayIdempotencyGuard(c *Client, ttl time.Duration) *PayIdempotencyGuard {
	return &PayIdempotencyGuard{client: c, ttl: ttl}
}

// TryAcquire 尝试占用订单支付名额：key 不存在才设置成功并返回 true；
// key 已存在（窗口内重复支付）返回 false。TTL 到期自动释放，天然避免死锁。
func (g *PayIdempotencyGuard) TryAcquire(ctx context.Context, id orderdomain.OrderID) (bool, error) {
	ok, err := g.client.rdb.SetNX(ctx, payLockKey(id), "1", g.ttl).Result()
	if err != nil {
		return false, fmt.Errorf("redis: acquire pay lock %q failed: %w", id, err)
	}
	return ok, nil
}

// Release 在支付业务失败时主动删锁，让用户可立即重试而不必等 TTL。
func (g *PayIdempotencyGuard) Release(ctx context.Context, id orderdomain.OrderID) error {
	if err := g.client.rdb.Del(ctx, payLockKey(id)).Err(); err != nil {
		return fmt.Errorf("redis: release pay lock %q failed: %w", id, err)
	}
	return nil
}

// payLockKey 支付锁键：order:{id}:pay。
func payLockKey(id orderdomain.OrderID) string {
	return fmt.Sprintf("order:%s:pay", string(id))
}
