package messenger

import (
	"context"
	"encoding/json"
	"sync"
)

// Record 一条已发送的对外广播消息。
type Record struct {
	Topic      string `json:"topic"`      // 目标 topic
	SenderCode string `json:"senderCode"` // 发送方标识
	Envelope   string `json:"envelope"`   // 已序列化信封原文
}

// MemoryMessenger 演示用内存 Messenger：把消息留在进程内并按聚合 ID 建立索引。
// 真实 MQ 实现可直接以聚合 ID 作为分区 key，无需像这里一样解析信封内容。
type MemoryMessenger struct {
	mu    sync.RWMutex
	byAgg map[string][]Record
}

// NewMemoryMessenger 创建空的内存 Messenger。
func NewMemoryMessenger() *MemoryMessenger {
	return &MemoryMessenger{byAgg: map[string][]Record{}}
}

// Send 记录消息并按信封中的 aggregateId 建索引；信封 JSON 非法时返回错误。
func (m *MemoryMessenger) Send(_ context.Context, topic, senderCode, envelope string) error {
	aggregateID, err := extractAggregateID(envelope)
	if err != nil {
		return err
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	m.byAgg[aggregateID] = append(m.byAgg[aggregateID], Record{
		Topic:      topic,
		SenderCode: senderCode,
		Envelope:   envelope,
	})
	return nil
}

// ByAggregate 返回指定聚合已发送消息的副本；无消息时返回空切片（非 nil）。
func (m *MemoryMessenger) ByAggregate(_ context.Context, aggregateID string) ([]Record, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	source := m.byAgg[aggregateID]
	records := make([]Record, len(source))
	copy(records, source)
	return records, nil
}

// extractAggregateID 从信封 JSON 读取 aggregateId 字段。
func extractAggregateID(envelope string) (string, error) {
	var head struct {
		AggregateID string `json:"aggregateId"`
	}
	if err := json.Unmarshal([]byte(envelope), &head); err != nil {
		return "", err
	}
	return head.AggregateID, nil
}
