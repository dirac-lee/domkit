package domain

import (
	"fmt"
	"time"
	"uuid"
)

// NewEventID 生成事件/消息的技术标识（UUIDv7）。
// 这是框架内部标识（非业务主键），与 Java 蓝本 BaseDomainEvent 直接使用
// UUID.randomUUID() 同构：纯计算、无 I/O，无需抽象为可替换端口。
func NewEventID() string {
	return uuid.NewV7().String()
}

// DomainEvent 领域事件的类型擦除接口，供工作单元、事件总线、outbox 等
// 不感知具体聚合/主键类型的组件使用。
//
// 主键类型在该边界上统一收敛为 string（AggregateKey）：outbox 表的
// aggregate_id 列、日志、消息中间件头都以字符串承载异构聚合主键。
// 需要强类型主键的业务代码请使用具体事件类型上的 AggregateID() ID 方法。
type DomainEvent interface {
	EventID() string
	// AggregateKey 返回字符串形式的所属聚合主键（fmt.Sprint 归一）。
	AggregateKey() string
	AggregateVersion() uint64
	EventName() string
	OccurredAt() time.Time
	// OperationCode 返回触发此事件的成因操作编码；
	// 由 AggregateRoot.CollectEvent 在收集时按「当前操作」自动回填。
	OperationCode() string
}

// BaseDomainEvent 领域事件基础结构体，具体事件嵌入并固定主键类型 ID，
// 例如 OrderCreatedEvent 嵌入 BaseDomainEvent[OrderID]。
type BaseDomainEvent[ID comparable] struct {
	EventIDVal          string
	AggregateIDVal      ID
	AggregateVersionVal uint64
	OccurredAtVal       time.Time
	// OperationCodeVal 成因操作编码，由 AggregateRoot.CollectEvent 回填，业务代码不应主动赋值。
	OperationCodeVal string
}

func (e BaseDomainEvent[ID]) EventID() string          { return e.EventIDVal }
func (e BaseDomainEvent[ID]) AggregateID() ID          { return e.AggregateIDVal }
func (e BaseDomainEvent[ID]) AggregateKey() string     { return fmt.Sprint(e.AggregateIDVal) }
func (e BaseDomainEvent[ID]) AggregateVersion() uint64 { return e.AggregateVersionVal }
func (e BaseDomainEvent[ID]) OccurredAt() time.Time    { return e.OccurredAtVal }
func (e BaseDomainEvent[ID]) OperationCode() string    { return e.OperationCodeVal }

// setOperationCode 框架内部回填接缝（非导出）：业务事件经嵌入 BaseDomainEvent
// 并以指针形态被 CollectEvent 收集时，自动获得该方法；业务方无法自行实现或绕过。
func (e *BaseDomainEvent[ID]) setOperationCode(code string) { e.OperationCodeVal = code }

// NewBaseDomainEvent 构造基础事件，aggID 为强类型聚合主键。
func NewBaseDomainEvent[ID comparable](aggID ID, aggVer uint64) BaseDomainEvent[ID] {
	return BaseDomainEvent[ID]{
		EventIDVal:          NewEventID(),
		AggregateIDVal:      aggID,
		AggregateVersionVal: aggVer,
		OccurredAtVal:       time.Now(),
	}
}
