package broadcast

import (
	"time"

	"github.com/dirac-lee/domkit/domain"
)

// currentSchemaVersion 当前信封协议版本，后续不兼容演进时递增。
const currentSchemaVersion = 1

// Envelope 聚合对外广播消息的统一信封：固定元数据（框架从领域事件填充）
// 加自由消息体 Payload（由引用方定义）。泛型 P 为消息体类型。
type Envelope[P any] struct {
	MessageID      string    `json:"messageId"`      // 全局唯一消息标识，对外幂等去重主键
	AggregateType  string    `json:"aggregateType"`  // 聚合根类型（简单类名）
	AggregateID    string    `json:"aggregateId"`    // 聚合实体标识，顺序消费分区键/反查主键
	Version        uint64    `json:"version"`        // 发布时刻聚合版本，供对接方丢弃乱序旧版本
	CauseOperation string    `json:"causeOperation"` // 成因操作编码
	OccurredAt     time.Time `json:"occurredAt"`     // 事件发生时间，供对账与时效判断
	SchemaVersion  int       `json:"schemaVersion"`  // 信封协议版本，用于兼容演进
	SourceEventID  string    `json:"sourceEventId"`  // 触发此消息的领域事件标识，用于溯源
	Payload        P         `json:"payload"`        // 消息体，承载对接方约定的业务字段
}

// NewEnvelope 以领域事件元数据组装信封：聚合类型由调用方提供、消息标识新生成，
// 其余元数据均取自触发事件，无需聚合根额外参与回填。
func NewEnvelope[P any](aggregateType string, evt domain.DomainEvent, payload P) Envelope[P] {
	return Envelope[P]{
		MessageID:      domain.NewEventID(),
		AggregateType:  aggregateType,
		AggregateID:    evt.AggregateKey(),
		Version:        evt.AggregateVersion(),
		CauseOperation: evt.OperationCode(),
		OccurredAt:     evt.OccurredAt(),
		SchemaVersion:  currentSchemaVersion,
		SourceEventID:  evt.EventID(),
		Payload:        payload,
	}
}
