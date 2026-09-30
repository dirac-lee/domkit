package outbox

import (
	"context"
	"errors"
	"time"

	"github.com/dirac-lee/domkit/domain"
)

// OutboxStatus 发件箱消息状态。
type OutboxStatus string

const (
	// StatusPending 待发布（含发布失败被释放回来的消息）。
	StatusPending OutboxStatus = "pending"
	// StatusProcessing 已被某 Relay 实例通过 claim token 认领，发布中。
	// 多实例部署的防重复投递核心状态：同一时刻一行只属于一个 token。
	StatusProcessing OutboxStatus = "processing"
	// StatusSent 发布成功（终态）。
	StatusSent OutboxStatus = "sent"
	// StatusDeadLetter 超过重试上限或消息体损坏，进入死信人工处理（终态）。
	StatusDeadLetter OutboxStatus = "deadletter"
)

// ErrClaimLost 认领丢失：消息已不被当前 claim token 持有
// （租约超时后被其他实例重新认领），当前实例不得再推进其状态。
var ErrClaimLost = errors.New("outbox: claim lost, message is owned by another token")

// OutboxMessage 发件箱数据库模型。
type OutboxMessage struct {
	ID          string       // UUID 主键
	EventName   string       // 事件名，同时作为反序列化的类型键
	AggregateID string       // 所属聚合主键（字符串归一），便于排查/按聚合有序消费
	Version     uint64       // 事件对应的聚合版本
	Payload     []byte       // 序列化后的事件体
	Status      OutboxStatus // pending / processing / sent / deadletter
	RetryCount  int          // 已重试次数
	LastError   string       // 最近一次失败原因
	CreatedAt   time.Time    // 事件发生时间
	ClaimToken  string       // 当前认领令牌；空表示未被认领
	ClaimedAt   time.Time    // 最近一次认领时间（租约起点，零值表示未被认领）
	SentAt      time.Time    // 发布成功时间
}

// OutboxStore 发件箱存储 SPI，由 MySQL/Postgres 等基础设施实现。
//
// 事务边界契约（对标 Java IOutboxStore）：
//   - SaveMessage 在业务事务 tx 内执行，与聚合落库同事务；
//   - ClaimPending/MarkSent/Release/MoveToDeadLetter 各自为独立短事务，
//     且绝不包含 MQ 发送动作；
//   - 所有状态推进都带 status/token 守卫，天然幂等。
type OutboxStore interface {
	// SaveMessage 在事务 tx 内写入一条待发消息。
	SaveMessage(ctx context.Context, tx any, msg *OutboxMessage) error

	// ClaimPending 原子认领一批消息并打上本实例的 claim token：
	//   - pending 且 CreatedAt 早于 now-grace（宽限期留给即时发布路径）；
	//   - 或 processing 但 ClaimedAt 早于 now-lease（持有时例崩溃，租约过期重认领）。
	// 实现对应单条 UPDATE ... LIMIT 的行级原子翻转，多实例并发时每行只被一个
	// token 抢中；返回该 token 本次抢到的消息，CreatedAt 升序、最多 batchSize 条。
	ClaimPending(ctx context.Context, batchSize int, grace, lease time.Duration, token string) ([]*OutboxMessage, error)

	// MarkSent 在本 token 持有下把消息置为 sent（状态守卫幂等）；
	// 消息已被其他 token 重认领时返回 ErrClaimLost。
	MarkSent(ctx context.Context, id, token string) error

	// Release 在本 token 持有下释放回 pending，RetryCount+1 并记录原因，
	// 返回递增后的重试次数；非本 token 持有返回 ErrClaimLost。
	Release(ctx context.Context, id, token, reason string) (attempts int, err error)

	// MoveToDeadLetter 在本 token 持有下转入死信；非本 token 持有返回 ErrClaimLost。
	MoveToDeadLetter(ctx context.Context, id, token, reason string) error
}

// DomainEventSerializer 领域事件序列化 SPI。
type DomainEventSerializer interface {
	Serialize(evt domain.DomainEvent) ([]byte, error)
	Deserialize(payload []byte, eventName string) (domain.DomainEvent, error)
}
