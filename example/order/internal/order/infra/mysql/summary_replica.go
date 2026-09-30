package mysql

import (
	"context"
	"errors"
	"fmt"
	"time"

	orderdomain "github.com/dirac-lee/domkit/example/order/internal/order/domain"
	"github.com/dirac-lee/domkit/readmodel"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// SummaryReplica 订单概要投影副本：实现 readmodel.ReadModelReplica，
// 同时提供面向 HTTP 的点查 / 分页读能力（写维护与读查询收敛于同一对象）。
type SummaryReplica struct {
	replicaID string
	db        *gorm.DB
	// load 从写模型加载订单；未找到时需返回 readmodel.ErrAggregateNotFound。
	load func(ctx context.Context, id orderdomain.OrderID) (*orderdomain.Order, error)
}

// NewSummaryReplica 创建投影副本。
func NewSummaryReplica(db *gorm.DB,
	load func(context.Context, orderdomain.OrderID) (*orderdomain.Order, error)) *SummaryReplica {
	return &SummaryReplica{replicaID: "mysql:order-summary", db: db, load: load}
}

// 编译期断言：SummaryReplica 必须满足读模型副本契约。
var _ readmodel.ReadModelReplica[orderdomain.OrderID] = (*SummaryReplica)(nil)

// ReplicaID 返回副本逻辑标识。
func (r *SummaryReplica) ReplicaID() string { return r.replicaID }

// ReadVersion 读取投影中该订单已物化的版本；副本缺失返回 0。
func (r *SummaryReplica) ReadVersion(ctx context.Context, id orderdomain.OrderID) (int64, error) {
	var po struct {
		Version uint64
	}
	err := DBOrTx(ctx, r.db).Model(&OrderSummaryPO{}).
		Select("version").Where("id = ?", string(id)).Take(&po).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("mysql: read summary version %q failed: %w", id, err)
	}
	return int64(po.Version), nil
}

// Rebuild 从写模型当前快照重建该订单的投影条目。
func (r *SummaryReplica) Rebuild(ctx context.Context, id orderdomain.OrderID) error {
	o, err := r.load(ctx, id)
	if errors.Is(err, readmodel.ErrAggregateNotFound) {
		// 写模型已删除：清理投影残留，避免对账长期报 ORPHAN。
		return r.PurgeOrphan(ctx, id)
	}
	if err != nil {
		return fmt.Errorf("mysql: rebuild summary %q failed: %w", id, err)
	}
	return r.upsert(ctx, o)
}

// Sync 实时投影入口（事件提交后由订阅者调用），语义等同 Rebuild。
func (r *SummaryReplica) Sync(ctx context.Context, id orderdomain.OrderID) error {
	return r.Rebuild(ctx, id)
}

// PurgeOrphan 删除该订单的投影残留条目。
func (r *SummaryReplica) PurgeOrphan(ctx context.Context, id orderdomain.OrderID) error {
	err := DBOrTx(ctx, r.db).Where("id = ?", string(id)).Delete(&OrderSummaryPO{}).Error
	if err != nil {
		return fmt.Errorf("mysql: purge summary %q failed: %w", id, err)
	}
	return nil
}

// upsert 重算投影并按主键 upsert（INSERT ... ON DUPLICATE KEY UPDATE），
// 无论新增订单还是版本更新都复用同一路径。
func (r *SummaryReplica) upsert(ctx context.Context, o *orderdomain.Order) error {
	summary := orderdomain.ProjectOrderSummary(o)
	po := OrderSummaryPO{
		ID:        string(summary.ID),
		OrderNo:   summary.OrderNo,
		Status:    summary.Status,
		Amount:    summary.Amount,
		Version:   summary.Version,
		CreatedAt: o.CreatedAt,
		UpdatedAt: time.Now(),
	}
	err := DBOrTx(ctx, r.db).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "id"}},
		DoUpdates: clause.AssignmentColumns([]string{"order_no", "status", "amount", "version", "updated_at"}),
	}).Create(&po).Error
	if err != nil {
		return fmt.Errorf("mysql: upsert summary %q failed: %w", o.ID, err)
	}
	return nil
}

// GetByID 点查投影；未命中 ok=false。此方法直连库，Redis 缓存由上层 cache-aside 包裹。
func (r *SummaryReplica) GetByID(ctx context.Context, id orderdomain.OrderID) (orderdomain.OrderSummary, bool, error) {
	var po OrderSummaryPO
	err := r.db.WithContext(ctx).Where("id = ?", string(id)).Take(&po).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return orderdomain.OrderSummary{}, false, nil
	}
	if err != nil {
		return orderdomain.OrderSummary{}, false, fmt.Errorf("mysql: get summary %q failed: %w", id, err)
	}
	return summaryPOToDomain(&po), true, nil
}

// Page 按投影表分页（列表不缓存），返回总数与当前页。
func (r *SummaryReplica) Page(ctx context.Context, req readmodel.PageRequest,
) (readmodel.PageResult[orderdomain.OrderSummary], error) {
	var total int64
	if err := r.db.WithContext(ctx).Model(&OrderSummaryPO{}).Count(&total).Error; err != nil {
		return readmodel.PageResult[orderdomain.OrderSummary]{}, fmt.Errorf("mysql: count summaries failed: %w", err)
	}

	var pos []OrderSummaryPO
	err := r.db.WithContext(ctx).Model(&OrderSummaryPO{}).
		Order("id").Limit(req.PageSize()).Offset(req.Offset()).Find(&pos).Error
	if err != nil {
		return readmodel.PageResult[orderdomain.OrderSummary]{}, fmt.Errorf("mysql: page summaries failed: %w", err)
	}

	items := make([]orderdomain.OrderSummary, 0, len(pos))
	for i := range pos {
		items = append(items, summaryPOToDomain(&pos[i]))
	}
	return readmodel.NewPageResult(items, total, req), nil
}

// summaryPOToDomain PO → 读模型。
func summaryPOToDomain(po *OrderSummaryPO) orderdomain.OrderSummary {
	return orderdomain.OrderSummary{
		ID:      orderdomain.OrderID(po.ID),
		OrderNo: po.OrderNo,
		Status:  po.Status,
		Amount:  po.Amount,
		Version: po.Version,
	}
}
