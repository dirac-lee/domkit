package order

import (
	"context"
	"testing"

	"github.com/dirac-lee/domkit/application"
	orderdomain "github.com/dirac-lee/domkit/example/order/internal/order/domain"
	"github.com/dirac-lee/domkit/example/order/internal/order/messenger"
	"github.com/dirac-lee/domkit/infra/outbox"
	"github.com/dirac-lee/domkit/infra/persist"
	"github.com/dirac-lee/domkit/readmodel"
)

// newTestApplication 装配一个全内存后端的应用：不连 MySQL/Redis，
// 但与真实装配共用同一装配内核与 outbox 流程，供纯单元测试使用。
func newTestApplication(t *testing.T) *Application {
	t.Helper()
	repo := newMemoryOrderRepo()
	replica := memorySummaryStore{
		MemoryReplica: readmodel.NewMemoryReplica(
			"memory:order-summary", loadOrderFromRepo(repo), orderdomain.ProjectOrderSummary,
		),
	}
	deps := orderDeps{
		repo:      repo,
		tm:        application.NoopTxManager{}, // 无真实事务；afterCommit 钩子退化为立即执行
		outbox:    outbox.NewMemoryOutboxStore(),
		summary:   replica,
		broadcast: messenger.NewMemoryMessenger(),
		cache:     nopSummaryCache{},
		payGuard:  nopPayGuard{},
	}

	app, err := assembleApplication(&Options{HTTPAddr: ":8080", DefaultPageSize: 20}, deps)
	if err != nil {
		t.Fatalf("装配测试应用失败: %v", err)
	}
	return app
}

// newMemoryOrderRepo 创建内存订单仓储。
func newMemoryOrderRepo() *persist.MemoryRepository[orderdomain.OrderID, orderdomain.Order] {
	return persist.NewAggregateRepository(func(o *orderdomain.Order) orderdomain.OrderID { return o.ID })
}

// memorySummaryStore 把框架内存副本适配为带 error 的读侧契约。
type memorySummaryStore struct {
	*readmodel.MemoryReplica[orderdomain.OrderID, orderdomain.OrderSummary]
}

func (s memorySummaryStore) GetByID(ctx context.Context, id orderdomain.OrderID,
) (orderdomain.OrderSummary, bool, error) {
	summary, ok := s.MemoryReplica.GetByID(ctx, id)
	return summary, ok, nil
}

func (s memorySummaryStore) Page(ctx context.Context, req readmodel.PageRequest,
) (readmodel.PageResult[orderdomain.OrderSummary], error) {
	return s.MemoryReplica.Page(ctx, req), nil
}

// ---- Nop 缓存 / 守卫：未配置 Redis 的测试与降级路径 ----

type nopPayGuard struct{}

func (nopPayGuard) TryAcquire(context.Context, orderdomain.OrderID) (bool, error) {
	return true, nil
}
func (nopPayGuard) Release(context.Context, orderdomain.OrderID) error { return nil }

type nopSummaryCache struct{}

// Get 始终未命中，直接回源。
func (nopSummaryCache) Get(ctx context.Context, id orderdomain.OrderID,
	loader func(context.Context, orderdomain.OrderID) (orderdomain.OrderSummary, bool, error),
) (orderdomain.OrderSummary, bool, error) {
	return loader(ctx, id)
}
func (nopSummaryCache) Invalidate(context.Context, orderdomain.OrderID) error { return nil }
