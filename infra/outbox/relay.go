package outbox

import (
	"context"
	"errors"
	"time"

	"github.com/dirac-lee/domkit/domain"
)

// RelayConfig 补偿轮询器配置（对标 Java OutboxRelayConfig）。
type RelayConfig struct {
	// Interval 轮询间隔，Start 后台协程使用。
	Interval time.Duration
	// BatchSize 单轮最大认领条数，<=0 使用默认 100。
	BatchSize int
	// Grace 新建消息宽限期：早于 now-grace 的 pending 消息才会被认领，
	// 给事务后的即时发布路径留出窗口，避免重复投递。
	Grace time.Duration
	// Lease 认领租约：processing 超过 lease 未完结视为持有者崩溃，
	// 允许其他实例重新认领（消息可能因此被再投递一次，消费者需幂等）。
	Lease time.Duration
	// MaxRetry 最大发布次数（含首次）；<=0 表示无限重试。
	MaxRetry int
}

// Relay Outbox 补偿轮询器：定时原子认领 pending/租约过期消息并发布。
// 多实例部署安全：认领靠 store 的 claim token 行级翻转，
// 每条消息同一时刻只被一个实例处理；状态推进全部带 token 守卫。
type Relay struct {
	store      OutboxStore
	publisher  EventPublisher
	serializer DomainEventSerializer
	cfg        RelayConfig
	// newToken 每轮认领的令牌生成器，默认 domain.NewEventID（测试可替换）。
	newToken func() string
}

// NewRelay 创建补偿轮询器。
func NewRelay(store OutboxStore, pub EventPublisher, ser DomainEventSerializer, cfg RelayConfig) *Relay {
	if cfg.BatchSize <= 0 {
		cfg.BatchSize = 100
	}
	return &Relay{
		store:      store,
		publisher:  pub,
		serializer: ser,
		cfg:        cfg,
		newToken:   domain.NewEventID,
	}
}

// Start 启动后台补偿协程，直到 ctx 取消。
func (r *Relay) Start(ctx context.Context) {
	ticker := time.NewTicker(r.cfg.Interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			_ = r.ScanPending(ctx)
		}
	}
}

// ScanPending 执行一轮「认领 → 发布 → 完结」，导出给手动触发/测试场景。
func (r *Relay) ScanPending(ctx context.Context) error {
	token := r.newToken()
	list, err := r.store.ClaimPending(ctx, r.cfg.BatchSize, r.cfg.Grace, r.cfg.Lease, token)
	if err != nil {
		return err
	}
	for _, msg := range list {
		r.processOne(ctx, msg, token)
	}
	return nil
}

// processOne 处理单条已认领消息；任何状态推进若返回 ErrClaimLost
// （租约过期后被别的实例重认领）都静默让位，绝不覆盖新持有者的结论。
func (r *Relay) processOne(ctx context.Context, msg *OutboxMessage, token string) {
	evt, err := r.serializer.Deserialize(msg.Payload, msg.EventName)
	if err != nil {
		// 消息体无法解析（未注册类型/数据损坏），重试无意义，直接死信。
		_ = r.store.MoveToDeadLetter(ctx, msg.ID, token, "deserialize: "+err.Error())
		return
	}
	if err := r.publisher.Publish(ctx, evt); err != nil {
		// msg.RetryCount 为此前失败次数；本次为第 RetryCount+1 次发布尝试。
		nextAttempts := msg.RetryCount + 1
		if r.cfg.MaxRetry > 0 && nextAttempts >= r.cfg.MaxRetry {
			// 达到上限直接死信（不先 Release，避免 Release 清空 token 后死信守卫失败）。
			_ = r.store.MoveToDeadLetter(ctx, msg.ID, token, err.Error())
			return
		}
		if _, relErr := r.store.Release(ctx, msg.ID, token, err.Error()); errors.Is(relErr, ErrClaimLost) {
			// 租约过期后被别的实例重认领，静默让位。
			return
		}
		return
	}
	if err := r.store.MarkSent(ctx, msg.ID, token); err != nil && !errors.Is(err, ErrClaimLost) {
		// 标记失败但事件已发布：消息保持 processing，下轮租约过期后会被重投一次，
		// 依赖消费端幂等去重（at-least-once 语义）。
		return
	}
}
