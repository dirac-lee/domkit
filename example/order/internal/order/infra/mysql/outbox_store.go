package mysql

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/dirac-lee/domkit/infra/outbox"
	"gorm.io/gorm"
)

// OutboxStore 发件箱存储：实现 outbox.OutboxStore，只负责落库与状态推进（纯 CRU）。
// 「提交后即时发布」与具体存储解耦，由组合根的后端无关装饰器包裹本结构实现。
type OutboxStore struct {
	db *gorm.DB
}

// NewOutboxStore 创建发件箱存储。
func NewOutboxStore(db *gorm.DB) *OutboxStore {
	return &OutboxStore{db: db}
}

// 编译期断言：OutboxStore 必须满足发件箱存储契约。
var _ outbox.OutboxStore = (*OutboxStore)(nil)

// SaveMessage 在业务事务 tx 内写入一条待发消息，并登记提交后即时发布。
func (s *OutboxStore) SaveMessage(ctx context.Context, tx any, msg *outbox.OutboxMessage) error {
	db, ok := tx.(*gorm.DB)
	if !ok || db == nil {
		db = s.db // 兜底：未拿到 GORM 事务句柄时退回基础连接
	}
	if err := db.WithContext(ctx).Create(toOutboxPO(msg)).Error; err != nil {
		return fmt.Errorf("mysql: save outbox message %q failed: %w", msg.ID, err)
	}
	return nil
}

// ClaimPending 认领一批待发消息（Relay 兜底路径，独立短事务、不含发送动作）。
//
// 说明：这里采用「选出候选 → 按 token 批量翻转 → 按 token 重查」实现，
// 在单实例 Relay 下严格正确；多实例并发抢同一批时，最终以 claim_token 归属为准，
// 消费端按 at-least-once 幂等兜底。生产多实例可改为逐条 UPDATE ... LIMIT 行级认领。
func (s *OutboxStore) ClaimPending(ctx context.Context, batchSize int,
	grace, lease time.Duration, token string) ([]*outbox.OutboxMessage, error) {
	candidates, err := s.selectCandidates(ctx, batchSize, grace, lease)
	if err != nil || len(candidates) == 0 {
		return nil, err
	}

	ids := make([]string, 0, len(candidates))
	for i := range candidates {
		ids = append(ids, candidates[i].ID)
	}
	if err := s.flashClaim(ctx, ids, token, time.Now()); err != nil {
		return nil, err
	}
	return s.claimedByToken(ctx, token)
}

// MarkSent 在本 token 持有下置为 sent；丢失认领返回 ErrClaimLost。
func (s *OutboxStore) MarkSent(ctx context.Context, id, token string) error {
	res := s.db.WithContext(ctx).Model(&OutboxMessagePO{}).
		Where("id = ? AND claim_token = ?", id, token).
		Updates(map[string]any{"status": outbox.StatusSent, "sent_at": time.Now()})
	if res.Error != nil {
		return fmt.Errorf("mysql: mark sent %q failed: %w", id, res.Error)
	}
	if res.RowsAffected == 0 {
		return outbox.ErrClaimLost
	}
	return nil
}

// Release 在本 token 持有下释放回 pending，RetryCount+1 并记录原因。
func (s *OutboxStore) Release(ctx context.Context, id, token, reason string) (int, error) {
	attempts, ok, err := s.retryCount(ctx, id, token)
	if err != nil {
		return 0, err
	}
	if !ok {
		return 0, outbox.ErrClaimLost
	}
	attempts++
	res := s.db.WithContext(ctx).Model(&OutboxMessagePO{}).
		Where("id = ? AND claim_token = ?", id, token).
		Updates(map[string]any{
			"status":      outbox.StatusPending,
			"claim_token": "",
			"retry_count": attempts,
			"last_error":  reason,
		})
	if res.Error != nil {
		return 0, fmt.Errorf("mysql: release %q failed: %w", id, res.Error)
	}
	if res.RowsAffected == 0 {
		return 0, outbox.ErrClaimLost
	}
	return attempts, nil
}

// MoveToDeadLetter 在本 token 持有下转入死信，清空认领令牌。
func (s *OutboxStore) MoveToDeadLetter(ctx context.Context, id, token, reason string) error {
	res := s.db.WithContext(ctx).Model(&OutboxMessagePO{}).
		Where("id = ? AND claim_token = ?", id, token).
		Updates(map[string]any{
			"status":      outbox.StatusDeadLetter,
			"last_error":  reason,
			"claim_token": "",
		})
	if res.Error != nil {
		return fmt.Errorf("mysql: dead-letter %q failed: %w", id, res.Error)
	}
	if res.RowsAffected == 0 {
		return outbox.ErrClaimLost
	}
	return nil
}

// ---- 认领辅助 ----

// selectCandidates 选出候选：pending 已过宽限期，或 processing 租约已过期。
func (s *OutboxStore) selectCandidates(ctx context.Context, batchSize int,
	grace, lease time.Duration) ([]OutboxMessagePO, error) {
	now := time.Now()
	var candidates []OutboxMessagePO
	err := s.db.WithContext(ctx).
		Where("status = ? AND created_at < ?", outbox.StatusPending, now.Add(-grace)).
		Or("status = ? AND claimed_at < ?", outbox.StatusProcessing, now.Add(-lease)).
		Order("created_at").Limit(batchSize).Find(&candidates).Error
	if err != nil {
		return nil, fmt.Errorf("mysql: select claim candidates failed: %w", err)
	}
	return candidates, nil
}

// flashClaim 把候选行批量翻转为 processing 并打上本 token。
func (s *OutboxStore) flashClaim(ctx context.Context, ids []string, token string, now time.Time) error {
	err := s.db.WithContext(ctx).Model(&OutboxMessagePO{}).
		Where("id IN ? AND status IN ?", ids,
			[]string{string(outbox.StatusPending), string(outbox.StatusProcessing)}).
		Updates(map[string]any{
			"status":      outbox.StatusProcessing,
			"claim_token": token,
			"claimed_at":  now,
		}).Error
	if err != nil {
		return fmt.Errorf("mysql: flash claim failed: %w", err)
	}
	return nil
}

// claimedByToken 重查本 token 真正抢中的行，CreatedAt 升序返回。
func (s *OutboxStore) claimedByToken(ctx context.Context, token string) ([]*outbox.OutboxMessage, error) {
	var pos []OutboxMessagePO
	err := s.db.WithContext(ctx).
		Where("claim_token = ? AND status = ?", token, outbox.StatusProcessing).
		Order("created_at").Find(&pos).Error
	if err != nil {
		return nil, fmt.Errorf("mysql: reload claimed messages failed: %w", err)
	}
	msgs := make([]*outbox.OutboxMessage, 0, len(pos))
	for i := range pos {
		msgs = append(msgs, poToOutbox(&pos[i]))
	}
	return msgs, nil
}

// retryCount 在 token 持有下读取当前重试次数；ok=false 表示已丢失认领。
func (s *OutboxStore) retryCount(ctx context.Context, id, token string) (int, bool, error) {
	var po struct {
		RetryCount int
	}
	err := s.db.WithContext(ctx).Model(&OutboxMessagePO{}).
		Select("retry_count").Where("id = ? AND claim_token = ?", id, token).Take(&po).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, err
	}
	return po.RetryCount, true, nil
}

// ---- PO 映射 ----

func toOutboxPO(m *outbox.OutboxMessage) OutboxMessagePO {
	return OutboxMessagePO{
		ID:          m.ID,
		EventName:   m.EventName,
		AggregateID: m.AggregateID,
		Version:     m.Version,
		Payload:     m.Payload,
		Status:      string(m.Status),
		RetryCount:  m.RetryCount,
		LastError:   m.LastError,
		CreatedAt:   m.CreatedAt,
		ClaimToken:  m.ClaimToken,
		ClaimedAt:   timePtr(m.ClaimedAt),
		SentAt:      timePtr(m.SentAt),
	}
}

func poToOutbox(po *OutboxMessagePO) *outbox.OutboxMessage {
	return &outbox.OutboxMessage{
		ID:          po.ID,
		EventName:   po.EventName,
		AggregateID: po.AggregateID,
		Version:     po.Version,
		Payload:     po.Payload,
		Status:      outbox.OutboxStatus(po.Status),
		RetryCount:  po.RetryCount,
		LastError:   po.LastError,
		CreatedAt:   po.CreatedAt,
		ClaimToken:  po.ClaimToken,
		ClaimedAt:   timeValue(po.ClaimedAt),
		SentAt:      timeValue(po.SentAt),
	}
}

// timePtr 零值时间映射为 NULL（避免 MySQL 严格模式拒绝 0000-00-00），非零取地址。
func timePtr(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	return &t
}

// timeValue 把可空时间还原为值；NULL 对应 time.Time 零值。
func timeValue(p *time.Time) time.Time {
	if p == nil {
		return time.Time{}
	}
	return *p
}
