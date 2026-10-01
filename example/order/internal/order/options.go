package order

import (
	"fmt"
	"time"

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
