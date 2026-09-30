package domain

import "context"

// ChangeKind 聚合变更类型，决定工作单元提交时调用仓储的哪个方法。
type ChangeKind uint8

const (
	// ChangeNew 新建聚合，提交时调用 Repository.Insert。
	ChangeNew ChangeKind = iota + 1
	// ChangeModified 修改聚合，提交时调用 Repository.Update。
	ChangeModified
	// ChangeDeleted 删除聚合，提交时调用 Repository.Delete。
	ChangeDeleted
)

// Aggregate 工作单元/命令执行器对聚合根要求的最小能力：
// 领域事件收集、版本基线推进、新建判定与规则违反收集。
// 该接口与主键类型无关：*AggregateRoot[任意ID] 以及内嵌它的用户聚合指针都自动满足，
// 因此不同主键类型（string/int64/...）的聚合可以登记进同一个工作单元。
type Aggregate interface {
	// 领域事件
	PullEvents() []DomainEvent
	// 版本与生命周期
	CommitVersion()
	IsNew() bool
	// 规则违反收集
	CheckRule(rule BusinessRule) bool
	HasBrokenRules() bool
	BrokenRules() []BrokenRule
	AggregateRuleError() error
}

// PersistFunc 将单个聚合落库的动作，由泛型注册函数依据仓储构造，
// 使工作单元无需感知具体聚合类型即可统一提交。
type PersistFunc func(ctx context.Context) error

// UnitOfWork 工作单元：登记一个用例内的全部聚合变更，
// Commit 时按登记顺序统一执行「持久化聚合 → 收集事件 → 事件落库/分发」。
type UnitOfWork interface {
	// RegisterChange 由 RegisterNew/RegisterModified/RegisterDeleted 泛型函数调用，
	// 业务代码不应直接使用该方法。
	RegisterChange(kind ChangeKind, agg Aggregate, persist PersistFunc)
	// Commit 提交全部变更。
	Commit(ctx context.Context) error
	// Rollback 放弃本次登记的全部变更。
	Rollback(ctx context.Context) error
}

// RegisterNew 登记新建聚合及其仓储，提交时执行 Insert。
// ID 从仓储类型推断，P 从聚合实参推断，调用点无需给出类型实参。
func RegisterNew[ID comparable, T any, P interface {
	*T
	Aggregate
}](uow UnitOfWork, agg P, repo Repository[ID, T]) {
	uow.RegisterChange(ChangeNew, agg, func(ctx context.Context) error {
		return repo.Insert(ctx, agg)
	})
}

// RegisterModified 登记已修改聚合及其仓储，提交时执行 Update。
func RegisterModified[ID comparable, T any, P interface {
	*T
	Aggregate
}](uow UnitOfWork, agg P, repo Repository[ID, T]) {
	uow.RegisterChange(ChangeModified, agg, func(ctx context.Context) error {
		return repo.Update(ctx, agg)
	})
}

// RegisterDeleted 登记删除聚合及其仓储，提交时执行 Delete。
func RegisterDeleted[ID comparable, T any, P interface {
	*T
	Aggregate
}](uow UnitOfWork, agg P, repo Repository[ID, T]) {
	uow.RegisterChange(ChangeDeleted, agg, func(ctx context.Context) error {
		return repo.Delete(ctx, agg)
	})
}
