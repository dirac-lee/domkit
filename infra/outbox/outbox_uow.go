package outbox

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"

	"github.com/dirac-lee/domkit/domain"
)

// ErrOutboxUoWCommitted 发件箱工作单元已提交，不能重复 Commit。
var ErrOutboxUoWCommitted = errors.New("outbox unit of work already committed")

type trackedChange struct {
	kind    domain.ChangeKind
	agg     domain.Aggregate
	persist domain.PersistFunc
}

// OutboxUnitOfWork 事务发件箱工作单元。
//
// Commit 时对每个已登记的变更，在同一事务 tx 内依次完成：
//  1. 调用仓储的 Insert/Update/Delete 持久化聚合；
//  2. 取出聚合收集的领域事件，序列化后写入 outbox 表。
//
// 因此注册时传入的仓储必须与 OutboxStore 共享同一事务 tx，
// 事务的开启/提交/回滚由业务基础设施层拥有。
type OutboxUnitOfWork struct {
	tx         any
	store      OutboxStore
	serializer DomainEventSerializer
	changes    []trackedChange
	committed  bool
}

// NewOutboxUnitOfWork 创建事务发件箱工作单元。
func NewOutboxUnitOfWork(tx any, store OutboxStore, ser DomainEventSerializer) *OutboxUnitOfWork {
	return &OutboxUnitOfWork{
		tx:         tx,
		store:      store,
		serializer: ser,
	}
}

// RegisterChange 由 domain.RegisterNew/Modified/Deleted 泛型函数调用。
func (u *OutboxUnitOfWork) RegisterChange(kind domain.ChangeKind, agg domain.Aggregate, persist domain.PersistFunc) {
	u.changes = append(u.changes, trackedChange{kind: kind, agg: agg, persist: persist})
}

// Commit 在事务内持久化全部聚合并写入 outbox 消息。
func (u *OutboxUnitOfWork) Commit(ctx context.Context) error {
	if u.committed {
		return ErrOutboxUoWCommitted
	}
	for _, ch := range u.changes {
		// 1. 聚合先落库。
		if err := ch.persist(ctx); err != nil {
			return fmt.Errorf("outbox: persist aggregate failed: %w", err)
		}
		// 持久化成功，推进聚合内存中的版本基线（事件版本在业务操作时已确定）。
		ch.agg.CommitVersion()
		// 2. 事件写入同事务 outbox 表。
		for _, evt := range ch.agg.PullEvents() {
			payload, err := u.serializer.Serialize(evt)
			if err != nil {
				return fmt.Errorf("outbox: serialize event %q failed: %w", evt.EventName(), err)
			}
			msg := &OutboxMessage{
				ID:          domain.NewEventID(),
				EventName:   evt.EventName(),
				AggregateID: evt.AggregateKey(),
				Version:     evt.AggregateVersion(),
				Payload:     payload,
				Status:      StatusPending,
				CreatedAt:   evt.OccurredAt(),
			}
			if err := u.store.SaveMessage(ctx, u.tx, msg); err != nil {
				return fmt.Errorf("outbox: save message %q failed: %w", evt.EventName(), err)
			}
		}
	}
	u.committed = true
	return nil
}

// Rollback 清空登记条目。数据库事务本身的回滚由事务拥有者负责；
// 已提交（committed）的工作单元不可回滚。
func (u *OutboxUnitOfWork) Rollback(_ context.Context) error {
	u.changes = nil
	u.committed = false
	return nil
}

// JsonEventSerializer 基于 encoding/json 的事件序列化器。
//
// 反序列化需要通过 Register 注册「事件名 -> 具体事件构造工厂」，
// 否则无法把 JSON 还原为具体领域事件（直接反序列化进接口只会得到 map）：
//
//	ser.Register("order.created", func() domain.DomainEvent { return &OrderCreatedEvent{} })
type JsonEventSerializer struct {
	mu       sync.RWMutex
	registry map[string]func() domain.DomainEvent
}

// NewJsonEventSerializer 创建空的 JSON 序列化器。
func NewJsonEventSerializer() *JsonEventSerializer {
	return &JsonEventSerializer{registry: make(map[string]func() domain.DomainEvent)}
}

// Register 注册事件构造工厂，重复注册同名事件会覆盖旧工厂。
func (s *JsonEventSerializer) Register(eventName string, factory func() domain.DomainEvent) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.registry[eventName] = factory
}

// Serialize 将具体领域事件序列化为 JSON。
func (s *JsonEventSerializer) Serialize(evt domain.DomainEvent) ([]byte, error) {
	return json.Marshal(evt)
}

// Deserialize 依据 eventName 找到工厂并把 JSON 还原为具体事件。
func (s *JsonEventSerializer) Deserialize(payload []byte, eventName string) (domain.DomainEvent, error) {
	s.mu.RLock()
	factory, ok := s.registry[eventName]
	s.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("outbox: event %q is not registered in serializer", eventName)
	}
	evt := factory()
	if err := json.Unmarshal(payload, evt); err != nil {
		return nil, fmt.Errorf("outbox: deserialize event %q failed: %w", eventName, err)
	}
	return evt, nil
}
