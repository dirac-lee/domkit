package order

import (
	"fmt"
	"time"

	"github.com/dirac-lee/domkit/infra/outbox"
	"github.com/dirac-lee/domkit/readmodel"
)

// Options 订单域运行配置：由 config 从配置源绑定，取代装配里的硬编码。
type Options struct {
	// HTTPAddr HTTP 监听地址，缺失即视为配置不完整（fail-fast）。
	HTTPAddr string `config:"http-addr,required"`
	// DefaultPageSize 列表查询缺省每页条数，配置缺失时取默认值 20。
	DefaultPageSize int `config:"default-page-size,default=20"`

	// MySQLDSN 数据库连接串，真实运行必填。
	MySQLDSN string `config:"mysql.dsn,required"`
	// RedisAddr Redis 地址（host:port），真实运行必填。
	RedisAddr string `config:"redis.addr,required"`
	// RedisPassword Redis 口令，无口令留空。
	RedisPassword string `config:"redis.password,default="`
	// SummaryCacheTTLSec 详情缓存 TTL（秒）。
	SummaryCacheTTLSec int `config:"cache.summary-ttl,default=30"`
	// PayIdempotencyTTLSec 支付幂等锁 TTL（秒）。
	PayIdempotencyTTLSec int `config:"idempotency.pay-ttl,default=300"`
	// RelayIntervalSec Outbox 补偿轮询间隔（秒），0 使用框架默认值。
	RelayIntervalSec int `config:"outbox.relay-interval-sec,default=1"`
	// RelayBatchSize Outbox 单轮认领数量，0 使用框架默认值。
	RelayBatchSize int `config:"outbox.relay-batch-size,default=100"`
	// RelayGraceSec Outbox 新消息宽限期（秒），应大于提交后即时发布超时。
	RelayGraceSec int `config:"outbox.relay-grace-sec,default=5"`
	// RelayLeaseSec Outbox 认领租约（秒），0 使用框架默认值。
	RelayLeaseSec int `config:"outbox.relay-lease-sec,default=30"`
	// RelayMaxRetry Outbox 最大发布次数，<=0 表示无限重试。
	RelayMaxRetry int `config:"outbox.relay-max-retry,default=10"`
}

// Validate 校验启动期即可确定的配置，避免错误配置拖到请求处理阶段才暴露。
func (o *Options) Validate() error {
	if _, err := readmodel.NewPageRequest(1, o.DefaultPageSize); err != nil {
		return fmt.Errorf("order: invalid default page size %d: %w", o.DefaultPageSize, err)
	}
	if o.SummaryCacheTTLSec <= 0 {
		return fmt.Errorf("order: cache.summary-ttl must be positive, got %d", o.SummaryCacheTTLSec)
	}
	if o.PayIdempotencyTTLSec <= 0 {
		return fmt.Errorf("order: idempotency.pay-ttl must be positive, got %d", o.PayIdempotencyTTLSec)
	}
	if err := o.RelayConfig().WithDefaults().Validate(); err != nil {
		return fmt.Errorf("order: invalid outbox relay config: %w", err)
	}
	return nil
}

// SummaryCacheTTL 返回详情缓存 TTL。
func (o *Options) SummaryCacheTTL() time.Duration {
	return time.Duration(o.SummaryCacheTTLSec) * time.Second
}

// PayIdempotencyTTL 返回支付幂等锁 TTL。
func (o *Options) PayIdempotencyTTL() time.Duration {
	return time.Duration(o.PayIdempotencyTTLSec) * time.Second
}

// RelayConfig 返回 Outbox Relay 配置；0 值字段交由框架默认值兜底。
func (o *Options) RelayConfig() outbox.RelayConfig {
	return outbox.RelayConfig{
		Interval:  time.Duration(o.RelayIntervalSec) * time.Second,
		BatchSize: o.RelayBatchSize,
		Grace:     time.Duration(o.RelayGraceSec) * time.Second,
		Lease:     time.Duration(o.RelayLeaseSec) * time.Second,
		MaxRetry:  o.RelayMaxRetry,
	}
}
