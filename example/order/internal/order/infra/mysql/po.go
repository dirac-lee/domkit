// Package mysql 是 order 示例的真实持久化适配层：用 GORM 落 MySQL。
//
// 第三方依赖（gorm/mysql driver）只允许出现在本适配包内；
// domain/application/port 层不 import 任何第三方库，从而保证框架与领域内核纯净。
//
// PO（Persistent Object，持久化对象）与领域聚合严格分离：
// 表结构怎么长，PO 就怎么长；它不携带任何领域行为，仅在仓储边界与聚合互转。
package mysql

import "time"

// OrderPO 订单写模型表 orders，一行对应一个订单聚合的当前快照。
type OrderPO struct {
	ID        string    `gorm:"primaryKey;column:id;size:64"`               // 订单主键（强类型 OrderID 归一为字符串）
	OrderNo   string    `gorm:"column:order_no;size:128;uniqueIndex:uk_no"` // 业务订单号，唯一
	Amount    int64     `gorm:"column:amount"`                              // 金额（分为单位）
	Status    string    `gorm:"column:status;size:32;index"`                // 状态编码 created/paid/cancelled
	Version   uint64    `gorm:"column:version"`                             // 乐观锁版本（已持久化基线）
	CreatedAt time.Time `gorm:"column:created_at;autoCreateTime:false"`     // 创建时间（由领域对象 MarkCreated 维护）
	UpdatedAt time.Time `gorm:"column:updated_at;autoUpdateTime:false"`     // 更新时间（由领域对象 MarkModified 维护）
}

// TableName 指定表名，避免 GORM 按复数约定推导出非预期名称。
func (OrderPO) TableName() string { return "orders" }

// OrderSummaryPO 订单概要投影表 order_summaries，CQRS 读侧的物化视图。
type OrderSummaryPO struct {
	ID        string    `gorm:"primaryKey;column:id;size:64"` // 与订单同主键
	OrderNo   string    `gorm:"column:order_no;size:128"`     // 订单号
	Status    string    `gorm:"column:status;size:32;index"`  // 状态编码
	Amount    int64     `gorm:"column:amount"`                // 金额
	Version   uint64    `gorm:"column:version;index"`         // 已物化版本（对账基线 V'）
	CreatedAt time.Time `gorm:"column:created_at"`            // 首次物化时间
	UpdatedAt time.Time `gorm:"column:updated_at"`            // 最近物化时间
}

// TableName 指定投影表名。
func (OrderSummaryPO) TableName() string { return "order_summaries" }

// OutboxMessagePO 事务发件箱表 outbox_messages，与订单写同事务落库。
type OutboxMessagePO struct {
	ID          string     `gorm:"primaryKey;column:id;size:64"`                // 消息 UUID
	EventName   string     `gorm:"column:event_name;size:128;index"`            // 事件名（反序列化类型键）
	AggregateID string     `gorm:"column:aggregate_id;size:64;index:idx_agg"`   // 所属订单主键
	Version     uint64     `gorm:"column:version"`                              // 事件对应的订单版本
	Payload     []byte     `gorm:"column:payload;type:longblob"`                // 事件 JSON
	Status      string     `gorm:"column:status;size:16;index:idx_status_time"` // pending/processing/sent/deadletter
	RetryCount  int        `gorm:"column:retry_count"`                          // 已重试次数
	LastError   string     `gorm:"column:last_error;size:1024"`                 // 最近失败原因
	CreatedAt   time.Time  `gorm:"column:created_at;index:idx_status_time"`     // 事件发生时间（认领排序用）
	ClaimToken  string     `gorm:"column:claim_token;size:64;index"`            // 当前认领令牌，空表示未认领
	ClaimedAt   *time.Time `gorm:"column:claimed_at"`                           // 最近认领时间（租约起点），未认领为 NULL
	SentAt      *time.Time `gorm:"column:sent_at"`                              // 发布成功时间，未发送为 NULL
}

// TableName 指定发件箱表名。
func (OutboxMessagePO) TableName() string { return "outbox_messages" }

// BroadcastRecordPO 对外广播记录表 broadcast_records，信使把已发送信封落库留痕。
type BroadcastRecordPO struct {
	ID          uint64    `gorm:"primaryKey;autoIncrement;column:id"`        // 自增主键
	Topic       string    `gorm:"column:topic;size:128;index"`               // 广播主题
	SenderCode  string    `gorm:"column:sender_code;size:64"`                // 发送方标识
	AggregateID string    `gorm:"column:aggregate_id;size:64;index:idx_agg"` // 关联订单主键
	Version     uint64    `gorm:"column:version"`                            // 触发时订单版本
	Envelope    string    `gorm:"column:envelope;type:json"`                 // 信封 JSON 原文
	CreatedAt   time.Time `gorm:"column:created_at;index:idx_agg"`           // 发送时间
}

// TableName 指定广播记录表名。
func (BroadcastRecordPO) TableName() string { return "broadcast_records" }
