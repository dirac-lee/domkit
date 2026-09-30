// Package redisx 是 order 示例的 Redis 适配层，承担两项职责：
// 支付幂等锁与订单概要读模型缓存。第三方依赖（go-redis）仅出现在本包。
package redisx

import (
	"context"
	"fmt"

	"github.com/redis/go-redis/v9"
)

// Client 包装 go-redis 客户端，对上层屏蔽具体 SDK。
type Client struct {
	rdb *redis.Client
}

// NewClient 创建 Redis 客户端并立即 Ping 探活：
// 启动阶段就暴露地址/口令错误，而不是等到首个请求才失败。
func NewClient(addr, password string) (*Client, error) {
	rdb := redis.NewClient(&redis.Options{
		Addr:     addr,
		Password: password, // 无口令时传空串
		DB:       0,
	})
	if err := rdb.Ping(context.Background()).Err(); err != nil {
		return nil, fmt.Errorf("redis: ping %q failed: %w", addr, err)
	}
	return &Client{rdb: rdb}, nil
}

// Close 关闭连接、释放资源。
func (c *Client) Close() error {
	return c.rdb.Close()
}
