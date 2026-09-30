package app

import (
	"context"
	"errors"

	"github.com/dirac-lee/domkit/application"
	"github.com/dirac-lee/domkit/domain"
	orderdomain "github.com/dirac-lee/domkit/example/order/internal/order/domain"
)

// ErrOrderNotFound 仓储中不存在指定订单。
var ErrOrderNotFound = errors.New("order not found")

// ErrOrderNoDuplicate 订单号已存在：命中 orders.uk_no 唯一键（常见于重试/重发）。
// 由 MySQL 适配层把驱动 1062 错误翻译为此业务哨兵，HTTP 统一映射为 409。
var ErrOrderNoDuplicate = errors.New("订单号已存在")

// OrderCommandService 基于 CommandExecutor 的函数式应用服务：
// 由执行器统一编排"加载聚合 → 领域行为 → 规则校验 → 持久化 → 事件分发"管线。
type OrderCommandService struct {
	Exec *application.CommandExecutor
	Repo domain.Repository[orderdomain.OrderID, orderdomain.Order]
	// IDs 订单发号器：由组合根装配一次后显式注入，服务内部不再自建注册中心。
	IDs domain.IDGenerator[orderdomain.OrderID]
}

// NewOrderCommandService 默认构造：SimpleUnitOfWork 落库后经 dispatcher 直接发布事件；
// dispatcher 传 nil 表示不分发。
func NewOrderCommandService(
	ids domain.IDGenerator[orderdomain.OrderID],
	repo domain.Repository[orderdomain.OrderID, orderdomain.Order],
	dispatcher application.EventDispatcher,
) *OrderCommandService {
	return &OrderCommandService{
		Exec: application.NewCommandExecutor(dispatcher),
		Repo: repo,
		IDs:  ids,
	}
}

// NewTransactionalOrderCommandService outbox 构造：聚合与发件箱消息在同一事务内落库。
func NewTransactionalOrderCommandService(
	ids domain.IDGenerator[orderdomain.OrderID],
	tm application.TransactionManager,
	newUoW application.TxUnitOfWorkFactory,
	repo domain.Repository[orderdomain.OrderID, orderdomain.Order],
) *OrderCommandService {
	return &OrderCommandService{
		Exec: application.NewTxCommandExecutor(tm, application.PropagationRequired, newUoW),
		Repo: repo,
		IDs:  ids,
	}
}

// Create 创建订单：放号 → 聚合工厂校验/建单事件 → 执行器落库（新建走 Insert）。
func (s *OrderCommandService) Create(ctx context.Context, orderNo string, amount int64) (*orderdomain.Order, error) {
	// 1. 放号；失败直接返回，不构造聚合。
	id, err := s.IDs.NextID(ctx)
	if err != nil {
		return nil, err
	}
	// 2. 工厂构造：内部已做金额规则校验并收集 order.created 事件。
	o, err := orderdomain.CreateOrder(id, orderNo, amount)
	if err != nil {
		return nil, err
	}
	// 3. 领域动作已在工厂内完成，复用执行器管线，logic 传 no-op。
	return s.Exec.Execute(ctx, o, s.Repo, func(*orderdomain.Order) error { return nil })
}

// Pay 支付订单：规则不通过返回 *domain.DomainRuleError，不触达持久化。
func (s *OrderCommandService) Pay(ctx context.Context, orderID orderdomain.OrderID) (*orderdomain.Order, error) {
	o, err := s.load(ctx, orderID)
	if err != nil {
		return nil, err
	}
	return s.Exec.Execute(ctx, o, s.Repo, func(o *orderdomain.Order) error { return o.Pay() })
}

// Cancel 取消订单。
func (s *OrderCommandService) Cancel(ctx context.Context, orderID orderdomain.OrderID) (*orderdomain.Order, error) {
	o, err := s.load(ctx, orderID)
	if err != nil {
		return nil, err
	}
	return s.Exec.Execute(ctx, o, s.Repo, func(o *orderdomain.Order) error { return o.Cancel() })
}

// TryPay 试跑支付：不落库、不发事件，以结构化结果返回规则校验结论。
// 试跑实例不得再用于真实执行；真实执行必须重新加载。
func (s *OrderCommandService) TryPay(ctx context.Context, orderID orderdomain.OrderID) (application.DryRunResult, error) {
	o, err := s.load(ctx, orderID)
	if err != nil {
		return application.DryRunResult{}, err
	}
	return s.Exec.TryExecute(ctx, o, s.Repo, func(o *orderdomain.Order) error { return o.Pay() })
}

// Get 查看订单写模型当前状态；订单不存在返回 ErrOrderNotFound。
// 列表/分页查询走读模型，不经过这里。
func (s *OrderCommandService) Get(ctx context.Context, orderID orderdomain.OrderID) (*orderdomain.Order, error) {
	return s.load(ctx, orderID)
}

// load 加载并兜底未命中；提前返回，不做多层嵌套。
func (s *OrderCommandService) load(ctx context.Context, orderID orderdomain.OrderID) (*orderdomain.Order, error) {
	o, err := s.Repo.GetByID(ctx, orderID)
	if err != nil {
		return nil, err
	}
	if o == nil {
		return nil, ErrOrderNotFound
	}
	return o, nil
}
