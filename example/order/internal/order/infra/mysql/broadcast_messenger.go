package mysql

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/dirac-lee/domkit/broadcast"
	"github.com/dirac-lee/domkit/example/order/internal/order/messenger"
	"gorm.io/gorm"
)

// envelopeHead 仅解析信封顶部的路由字段：聚合 ID 与版本。
type envelopeHead struct {
	AggregateID string `json:"aggregateId"`
	Version     uint64 `json:"version"`
}

// BroadcastMessenger 对外广播信使的 MySQL 实现：
// 发送即把信封落 broadcast_records 留痕，并支持按聚合 ID 回查。
type BroadcastMessenger struct {
	db *gorm.DB
}

// NewBroadcastMessenger 创建 MySQL 信使。
func NewBroadcastMessenger(db *gorm.DB) *BroadcastMessenger {
	return &BroadcastMessenger{db: db}
}

// 编译期断言：BroadcastMessenger 必须满足 broadcast.Messenger。
var _ broadcast.Messenger = (*BroadcastMessenger)(nil)

// Send 解析信封路由字段并落库一条广播记录。
func (m *BroadcastMessenger) Send(ctx context.Context, topic, senderCode, envelope string) error {
	head, err := parseEnvelopeHead(envelope)
	if err != nil {
		return fmt.Errorf("mysql: parse broadcast envelope failed: %w", err)
	}
	po := BroadcastRecordPO{
		Topic:       topic,
		SenderCode:  senderCode,
		AggregateID: head.AggregateID,
		Version:     head.Version,
		Envelope:    envelope,
	}
	// afterCommit 回调使用后台 ctx（无事务）→ 走基础连接；保留 DBOrTx 以兼容事务内调用。
	if err := DBOrTx(ctx, m.db).Create(&po).Error; err != nil {
		return fmt.Errorf("mysql: persist broadcast record failed: %w", err)
	}
	return nil
}

// ByAggregate 按聚合 ID 时间序回查广播记录；无记录返回空切片。
func (m *BroadcastMessenger) ByAggregate(aggregateID string) ([]messenger.Record, error) {
	var pos []BroadcastRecordPO
	err := m.db.WithContext(context.Background()).
		Where("aggregate_id = ?", aggregateID).Order("id").Find(&pos).Error
	if err != nil {
		return nil, fmt.Errorf("mysql: query broadcasts %q failed: %w", aggregateID, err)
	}
	records := make([]messenger.Record, 0, len(pos))
	for _, po := range pos {
		records = append(records, messenger.Record{
			Topic:      po.Topic,
			SenderCode: po.SenderCode,
			Envelope:   po.Envelope,
		})
	}
	return records, nil
}

// parseEnvelopeHead 解析信封路由字段。
func parseEnvelopeHead(envelope string) (envelopeHead, error) {
	var head envelopeHead
	if err := json.Unmarshal([]byte(envelope), &head); err != nil {
		return envelopeHead{}, err
	}
	return head, nil
}
