package order

import (
	"context"

	"github.com/dirac-lee/domkit/broadcast"
	orderdomain "github.com/dirac-lee/domkit/example/order/internal/order/domain"
	"github.com/dirac-lee/domkit/example/order/internal/order/messenger"
	"github.com/dirac-lee/domkit/readmodel"
)

// OrderSummaryStore 读侧契约：既是可对账副本，也提供点查 / 分页与实时投影。
// MySQL 投影与内存实现都满足它，装配内核据此与具体存储解耦。
type OrderSummaryStore interface {
	readmodel.ReadModelReplica[orderdomain.OrderID]
	// GetByID 点查投影，未命中 ok=false。
	GetByID(ctx context.Context, id orderdomain.OrderID) (orderdomain.OrderSummary, bool, error)
	// Page 投影分页。
	Page(ctx context.Context, req readmodel.PageRequest) (readmodel.PageResult[orderdomain.OrderSummary], error)
	// Sync 实时投影入口。
	Sync(ctx context.Context, id orderdomain.OrderID) error
}

// BroadcastStore 对外信使契约：可发送信封并按聚合回查留痕。
type BroadcastStore interface {
	broadcast.Messenger
	ByAggregate(ctx context.Context, aggregateID string) ([]messenger.Record, error)
}

// SummaryCache 详情缓存契约（cache-aside）：读时回填、写后失效。
type SummaryCache interface {
	// Get 先缓存后回源；loader 未命中返回 (零值, false, nil)。
	Get(ctx context.Context, id orderdomain.OrderID,
		loader func(context.Context, orderdomain.OrderID) (orderdomain.OrderSummary, bool, error),
	) (orderdomain.OrderSummary, bool, error)
	// Invalidate 删除缓存。
	Invalidate(ctx context.Context, id orderdomain.OrderID) error
}

// PayGuard 支付幂等守卫契约。
type PayGuard interface {
	// TryAcquire 尝试占用支付名额，重复支付返回 false。
	TryAcquire(ctx context.Context, id orderdomain.OrderID) (bool, error)
	// Release 业务失败时释放名额。
	Release(ctx context.Context, id orderdomain.OrderID) error
}
