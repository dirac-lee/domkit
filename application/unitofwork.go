package application

import (
	"context"
	"errors"

	"github.com/dirac-lee/domkit/domain"
)

// ErrUnitOfWorkCommitted 工作单元已提交，不能重复 Commit。
var ErrUnitOfWorkCommitted = errors.New("unit of work already committed")

// EventDispatcher 聚合持久化完成后的领域事件分发回调，
// 例如把内存事件总线的 Publish 适配进来。为 nil 时事件在提交后丢弃。
type EventDispatcher func(ctx context.Context, evt domain.DomainEvent) error

type trackedChange struct {
	kind    domain.ChangeKind
	agg     domain.Aggregate
	persist domain.PersistFunc
}

// SimpleUnitOfWork 基础工作单元（非 outbox 一致性语义）：
// Commit 时按登记顺序先完成全部聚合持久化，再统一分发领域事件。
// 它不保证消息中间件可靠投递，跨服务可靠事件请使用 outbox 实现。
type SimpleUnitOfWork struct {
	changes    []trackedChange
	dispatcher EventDispatcher
	committed  bool
}

// NewSimpleUnitOfWork 创建基础工作单元，dispatcher 可为 nil。
func NewSimpleUnitOfWork(dispatcher EventDispatcher) *SimpleUnitOfWork {
	return &SimpleUnitOfWork{dispatcher: dispatcher}
}

// RegisterChange 由 domain.RegisterNew/Modified/Deleted 泛型函数调用。
func (u *SimpleUnitOfWork) RegisterChange(kind domain.ChangeKind, agg domain.Aggregate, persist domain.PersistFunc) {
	u.changes = append(u.changes, trackedChange{kind: kind, agg: agg, persist: persist})
}

// Commit 按登记顺序持久化聚合，全部成功后再分发事件。
func (u *SimpleUnitOfWork) Commit(ctx context.Context) error {
	if u.committed {
		return ErrUnitOfWorkCommitted
	}
	// 第一阶段：全部聚合落库，任一失败立即中断（事务回滚由仓储/事务拥有者负责）。
	for _, ch := range u.changes {
		if err := ch.persist(ctx); err != nil {
			return err
		}
		// 持久化成功，推进聚合内存中的版本基线。
		ch.agg.CommitVersion()
	}
	// 第二阶段：持久化全部成功后，再分发事件，避免"事件已发但聚合没落库"。
	if u.dispatcher != nil {
		for _, ch := range u.changes {
			for _, evt := range ch.agg.PullEvents() {
				if err := u.dispatcher(ctx, evt); err != nil {
					return err
				}
			}
		}
	}
	u.committed = true
	return nil
}

// Rollback 清空登记条目。数据库事务本身的回滚由事务拥有者负责。
func (u *SimpleUnitOfWork) Rollback(_ context.Context) error {
	u.changes = nil
	u.committed = false
	return nil
}
