package order

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/dirac-lee/domkit/application"
	"github.com/dirac-lee/domkit/config"
	"github.com/dirac-lee/domkit/domain"
	"github.com/dirac-lee/domkit/example/order/internal/order/acl"
	"github.com/dirac-lee/domkit/example/order/internal/order/app"
	orderdomain "github.com/dirac-lee/domkit/example/order/internal/order/domain"
	"github.com/dirac-lee/domkit/example/order/internal/order/infra/mysql"
	"github.com/dirac-lee/domkit/example/order/internal/order/infra/redisx"
	"github.com/dirac-lee/domkit/infra/eventbus"
	"github.com/dirac-lee/domkit/infra/outbox"
	"github.com/dirac-lee/domkit/readmodel"
	"github.com/dirac-lee/domkit/reconciliation"
)

// Application 订单域组合根的装配产物：聚合该域全部进程级单例。
// 入站侧统一通过 Commands / Payments 用例入口访问，不直接触碰 Repo/ACL。
type Application struct {
	Options  *Options
	Commands *app.OrderCommandService
	Payments *app.PaymentService

	// Repo / ReadModel / Reconciler 暴露以便测试、运维观察与对账。
	Repo       domain.Repository[orderdomain.OrderID, orderdomain.Order]
	ReadModel  OrderSummaryStore
	Reconciler *reconciliation.Manager[orderdomain.OrderID]
	Broadcasts BroadcastStore
	// SummaryCache / PayGuard 供入站层做详情缓存读取与重复支付拦截。
	SummaryCache SummaryCache
	PayGuard     PayGuard

	closers []func() error
}

// orderDeps 装配内核所需的后端依赖（参数对象，避免过长参数列表）。
// 真实运行由 MySQL/Redis 提供，单元测试由内存实现提供，二者装配流程完全一致。
type orderDeps struct {
	repo      domain.Repository[orderdomain.OrderID, orderdomain.Order]
	tm        application.TransactionManager
	outbox    outbox.OutboxStore // 事务内写发件箱
	summary   OrderSummaryStore  // 读侧副本 + 查询
	broadcast BroadcastStore     // 对外信使
	cache     SummaryCache       // 详情缓存
	payGuard  PayGuard           // 支付幂等守卫
	closers   []func() error     // 进程退出时按逆序释放的资源
}

// NewApplication 订单域组合根（真实依赖）：进程启动调用一次。
// 依次连接并迁移 MySQL、连接 Redis，再交由装配内核完成事件订阅与用例构造，
// 任一前置依赖失败立即返回（启动期 fail-fast）。
func NewApplication(cfg *config.Context) (*Application, error) {
	// 绑定本域配置（键前缀 order）。
	options, err := config.Bind[Options](cfg.Source(), "order")
	if err != nil {
		return nil, err
	}
	err = options.Validate()
	if err != nil {
		return nil, err
	}

	// 真实 MySQL：连接 → AutoMigrate 建表。
	db, err := mysql.Open(options.MySQLDSN)
	if err != nil {
		return nil, err
	}
	closers := []func() error{
		func() error { return mysql.Close(db) },
	}
	err = mysql.Migrate(db)
	if err != nil {
		_ = closeAll(closers)
		return nil, err
	}

	// 真实 Redis：连接并探活。
	redisClient, err := redisx.NewClient(options.RedisAddr, options.RedisPassword)
	if err != nil {
		_ = closeAll(closers)
		return nil, err
	}
	closers = append(closers, redisClient.Close)

	repo := mysql.NewOrderRepository(db)
	app, err := assembleApplication(options, orderDeps{
		repo:      repo,
		tm:        mysql.NewGormTxManager(db),
		outbox:    mysql.NewOutboxStore(db),
		summary:   mysql.NewSummaryReplica(db, loadOrderFromRepo(repo)),
		broadcast: mysql.NewBroadcastMessenger(db),
		cache:     redisx.NewSummaryCache(redisClient, options.SummaryCacheTTL()),
		payGuard:  redisx.NewPayIdempotencyGuard(redisClient, options.PayIdempotencyTTL()),
		closers:   closers,
	})
	if err != nil {
		if closeErr := closeAll(closers); closeErr != nil {
			return nil, errors.Join(err, closeErr)
		}
		return nil, err
	}
	return app, nil
}

// Close 释放订单应用装配时持有的进程级资源。
func (a *Application) Close() error {
	if a == nil {
		return nil
	}
	return closeAll(a.closers)
}

func closeAll(closers []func() error) error {
	var err error
	for i, closer := range slices.Backward(closers) {
		if closer == nil {
			continue
		}
		if closeErr := closer(); closeErr != nil {
			err = errors.Join(err, fmt.Errorf("order: close resource %d failed: %w", i, closeErr))
		}
	}
	return err
}

// assembleApplication 后端无关的装配内核：订阅事件、构造 outbox 与用例。
// 不感知 MySQL/Redis，真实与单元测试复用同一流程，避免两套装配产生重复与漂移。
func assembleApplication(options *Options, deps orderDeps) (*Application, error) {
	bus := eventbus.NewMemoryEventBus()

	// 1. 支付 → 对外广播订阅；装配期校验，配置缺失启动即暴露。
	paidSubscriber := newOrderPaidSubscriber(deps.broadcast, deps.repo)
	if err := paidSubscriber.Validate(); err != nil {
		return nil, err
	}
	bus.Subscribe("order.paid", "order-paid-broadcaster", paidSubscriber, 0)

	// 2. 生命周期事件 → 投影 + 缓存失效订阅。
	project := eventbus.FuncHandler(func(evt domain.DomainEvent) error {
		id := orderdomain.OrderID(evt.AggregateKey())
		ctx, cancel := postCommitContext()
		defer cancel()
		return syncSummaryAndInvalidate(ctx, id, deps.summary, deps.cache)
	})
	for _, eventName := range []string{"order.created", "order.paid", "order.cancelled"} {
		bus.Subscribe(eventName, "order-summary-projector", project, 0)
	}

	// 3. outbox：序列化器 + 即时发布装饰（发布目标为进程内 bus）。
	serializer := newOrderEventSerializer()
	store := newInstantOutboxStore(deps.outbox, serializer, eventBusPublisher{bus: bus})

	// 4. 事务命令服务：聚合与 outbox 同事务落库，提交成功后即时发布事件。
	newUoW := func(tx any) domain.UnitOfWork {
		return outbox.NewOutboxUnitOfWork(tx, store, serializer)
	}
	commands := app.NewTransactionalOrderCommandService(
		newOrderIDGenerator(), deps.tm, newUoW, deps.repo,
	)

	// 5. 支付预授权用例（模拟渠道，保持既有装配）。
	paymentClient := acl.NewPreAuthClient(acl.NewPaymentGateway(), acl.NewPreAuthLogger(nil))

	return &Application{
		Options:      options,
		Commands:     commands,
		Payments:     app.NewPaymentService(paymentClient),
		Repo:         deps.repo,
		ReadModel:    deps.summary,
		Reconciler:   reconciliation.NewManager(deps.repo).RegisterReplica(deps.summary),
		Broadcasts:   deps.broadcast,
		SummaryCache: deps.cache,
		PayGuard:     deps.payGuard,
		closers:      deps.closers,
	}, nil
}

// loadOrderFromRepo 构造投影副本的写模型加载器；只依赖仓储端口。
// 仓储未命中翻译为 ErrAggregateNotFound，供副本清理残留条目。
func loadOrderFromRepo(repo domain.Repository[orderdomain.OrderID, orderdomain.Order],
) func(context.Context, orderdomain.OrderID) (*orderdomain.Order, error) {
	return func(ctx context.Context, id orderdomain.OrderID) (*orderdomain.Order, error) {
		o, err := repo.GetByID(ctx, id)
		if err != nil {
			return nil, err
		}
		if o == nil {
			return nil, readmodel.ErrAggregateNotFound
		}
		return o, nil
	}
}
