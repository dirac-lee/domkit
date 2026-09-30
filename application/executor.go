package application

import (
	"context"
	"errors"

	"github.com/dirac-lee/domkit/domain"
)

// aggregatePtr 命令执行器对聚合指针的约束：*T 且满足 domain.Aggregate 最小能力。
// 约束本身与主键类型无关，主键 ID 由各泛型方法单独绑定。
type aggregatePtr[T any] interface {
	*T
	domain.Aggregate
}

// TxUnitOfWorkFactory 依据底层事务句柄构造绑定该事务的工作单元，
// 例如 outbox 场景：func(tx any) domain.UnitOfWork { return outbox.NewOutboxUnitOfWork(tx, store, ser) }。
type TxUnitOfWorkFactory func(tx any) domain.UnitOfWork

// persistMode 决定执行器以何种方式落库。
type persistMode uint8

const (
	// persistSimple 每次执行内部新建 SimpleUnitOfWork，落库后直接分发事件。
	persistSimple persistMode = iota
	// persistTransactional 在 TransactionManager 事务内运行工厂创建的 UoW
	//（如 outbox UoW：聚合与发件箱消息同事务落库）。
	persistTransactional
)

// CommandExecutor 命令执行器（非泛型类型，一个实例服务全部聚合/主键类型），
// Execute/TryExecute 以 Go 1.27 泛型方法在调用点绑定具体主键与聚合类型。固定标准管线：
//
//	领域逻辑 → 规则校验 → 持久化（+事件分发）→ 返回聚合
//
// 并提供与 Execute 同源、零副作用的 TryExecute 试跑入口。
// 持久化方式通过构造函数选择：
//   - NewCommandExecutor：SimpleUnitOfWork，落库后经 dispatcher 直接发布事件；
//   - NewTxCommandExecutor：在 TransactionManager 事务内运行工厂创建的 UoW
//     （如 outbox UoW：聚合与发件箱消息同事务落库，事件不走 dispatcher）。
type CommandExecutor struct {
	mode       persistMode
	dispatcher EventDispatcher
	tm         TransactionManager
	prop       Propagation
	newUoW     TxUnitOfWorkFactory
}

// NewCommandExecutor 创建默认执行器：每次执行内部新建 SimpleUnitOfWork，
// 聚合落库成功后调用 dispatcher 分发事件；dispatcher 为 nil 时不分发。
func NewCommandExecutor(dispatcher EventDispatcher) *CommandExecutor {
	return &CommandExecutor{mode: persistSimple, dispatcher: dispatcher}
}

// NewTxCommandExecutor 创建事务执行器：在 tm 开启的事务内由 newUoW(tx) 构造工作单元
// 并提交；传播行为通常用 PropagationRequired。outbox 等场景用它保证聚合与消息同事务。
func NewTxCommandExecutor(tm TransactionManager, p Propagation, newUoW TxUnitOfWorkFactory) *CommandExecutor {
	return &CommandExecutor{mode: persistTransactional, tm: tm, prop: p, newUoW: newUoW}
}

// Execute 执行命令：
//  1. 运行领域逻辑 logic（逻辑内可通过聚合收集器累积规则违反）；
//  2. logic 返回领域规则错误时直接失败，不触达持久化；
//  3. 可选 rules：任一规则不通过即返回聚合规则错误，不触达持久化；
//  4. 调用持久化钩子：新建聚合走 Insert，已存在聚合走 Update；
//  5. 返回执行后的聚合（携带提交后的版本基线）。
//
// ID 为聚合主键类型，T 为聚合值类型，二者均从 agg/repo 实参推断，调用点无需显式给出。
func (e *CommandExecutor) Execute[ID comparable, T any, P aggregatePtr[T]](
	ctx context.Context, agg P, repo domain.Repository[ID, T],
	logic func(P) error, rules ...domain.BusinessRule,
) (P, error) {
	// 1. 领域逻辑
	if err := logic(agg); err != nil {
		// 领域逻辑主动返回的规则错误，丢弃其暂存事件后直接返回。
		if domain.IsDomainRuleError(err) {
			agg.PullEvents()
		}
		return agg, err
	}

	// 2. 规则校验（聚合内聚收集）
	for _, rule := range rules {
		agg.CheckRule(rule)
	}
	if agg.HasBrokenRules() {
		agg.PullEvents() // 规则未通过，事件不得外泄
		return agg, agg.AggregateRuleError()
	}

	// 3. 持久化（+ 事件分发），内部完成 RegisterNew/Modified 与版本基线推进。
	if err := e.persist(ctx, agg, repo); err != nil {
		return agg, err
	}
	return agg, nil
}

// TryExecute 试跑命令：执行与 Execute 相同的领域逻辑与规则校验，
// 但跳过持久化与事件分发，以结构化 DryRunResult 返回校验结论。
//   - 规则类失败：返回 (reject结果, nil)；
//   - 领域逻辑抛出的非规则错误：返回 (零值结果, err)，由调用方处理；
//   - 试跑期间暂存的事件保证被丢弃（零副作用）；试跑后的聚合实例不得再用于真实执行。
func (e *CommandExecutor) TryExecute[ID comparable, T any, P aggregatePtr[T]](
	_ context.Context, agg P, _ domain.Repository[ID, T],
	logic func(P) error, rules ...domain.BusinessRule,
) (result DryRunResult, err error) {
	// 无论结论如何，丢弃试跑暂存的事件，保证零副作用外泄。
	defer func() {
		agg.PullEvents()
	}()

	// 1. 领域逻辑
	if logicErr := logic(agg); logicErr != nil {
		if domain.IsDomainRuleError(logicErr) {
			return e.rejectResult(agg, logicErr), nil
		}
		return DryRunResult{}, logicErr
	}

	// 2. 规则校验
	for _, rule := range rules {
		agg.CheckRule(rule)
	}
	if agg.HasBrokenRules() {
		return e.rejectResult(agg, nil), nil
	}
	return DryRunPass(), nil
}

// rejectResult 优先取聚合收集器中的违反项；聚合未收集时从错误中提取明细。
func (e *CommandExecutor) rejectResult[T any, P aggregatePtr[T]](agg P, cause error) DryRunResult {
	if rules := agg.BrokenRules(); len(rules) > 0 {
		return DryRunReject(rules)
	}
	var dre *domain.DomainRuleError
	if cause != nil && errors.As(cause, &dre) {
		return DryRunReject(dre.BrokenRules)
	}
	return DryRunReject(nil)
}

// persist 按执行器模式选择落库路径；新建聚合登记 Insert，已存在聚合登记 Update。
func (e *CommandExecutor) persist[ID comparable, T any, P aggregatePtr[T]](
	ctx context.Context, agg P, repo domain.Repository[ID, T]) error {
	if e.mode == persistTransactional {
		return e.tm.DoInTx(ctx, e.prop, func(ctx context.Context, tx any) error {
			uow := e.newUoW(tx)
			registerChange(uow, agg, repo)
			return uow.Commit(ctx)
		})
	}
	uow := NewSimpleUnitOfWork(e.dispatcher)
	registerChange(uow, agg, repo)
	return uow.Commit(ctx)
}

// registerChange 按聚合是否已持久化登记 Insert/Update 变更。
func registerChange[ID comparable, T any, P aggregatePtr[T]](uow domain.UnitOfWork, agg P, repo domain.Repository[ID, T]) {
	if agg.IsNew() {
		domain.RegisterNew(uow, agg, repo)
	} else {
		domain.RegisterModified(uow, agg, repo)
	}
}
