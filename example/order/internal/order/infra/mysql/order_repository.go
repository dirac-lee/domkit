package mysql

import (
	"context"
	"errors"
	"fmt"

	"github.com/dirac-lee/domkit/domain"
	"github.com/dirac-lee/domkit/example/order/internal/order/app"
	orderdomain "github.com/dirac-lee/domkit/example/order/internal/order/domain"
	mysqldrv "github.com/go-sql-driver/mysql"
	"gorm.io/gorm"
)

// OrderRepository 订单仓储：实现框架的 domain.Repository[OrderID, Order]。
type OrderRepository struct {
	db *gorm.DB
}

// NewOrderRepository 创建订单仓储。
func NewOrderRepository(db *gorm.DB) *OrderRepository {
	return &OrderRepository{db: db}
}

// 编译期断言：OrderRepository 必须满足框架仓储契约。
var _ domain.Repository[orderdomain.OrderID, orderdomain.Order] = (*OrderRepository)(nil)

// Insert 持久化新建订单。
func (r *OrderRepository) Insert(ctx context.Context, o *orderdomain.Order) error {
	po := toOrderPO(o)
	if err := DBOrTx(ctx, r.db).Create(&po).Error; err != nil {
		// orders 表除主键外仅 uk_no 一个唯一键，1062 即订单号重复：翻译为业务哨兵。
		if isDupEntry(err) {
			return app.ErrOrderNoDuplicate
		}
		return fmt.Errorf("mysql: insert order %q failed: %w", o.ID, err)
	}
	return nil
}

// isDupEntry 判断是否为 MySQL 唯一键冲突（错误号 1062，ER_DUP_ENTRY）。
func isDupEntry(err error) bool {
	var drvErr *mysqldrv.MySQLError
	return errors.As(err, &drvErr) && drvErr.Number == 1062
}

// Update 以乐观锁更新订单：仅当行版本仍等于装载基线时才写入待提交版本。
func (r *OrderRepository) Update(ctx context.Context, o *orderdomain.Order) error {
	expected := o.Version // 装载时的基线版本
	res := DBOrTx(ctx, r.db).Model(&OrderPO{}).
		Where("id = ? AND version = ?", string(o.ID), expected).
		UpdateColumns(map[string]any{
			"order_no":   o.OrderNo,
			"amount":     o.Amount,
			"status":     o.Status.Value(),
			"version":    o.PendingVersion(),
			"updated_at": o.UpdatedAt,
		})
	if res.Error != nil {
		return fmt.Errorf("mysql: update order %q failed: %w", o.ID, res.Error)
	}
	// 无行被更新：行已删除或版本被他人抢先推进，转为冲突错误。
	if res.RowsAffected == 0 {
		return r.conflictErr(ctx, o.ID, expected)
	}
	return nil
}

// Delete 以乐观锁硬删除订单（当前订单示例不使用软删除）。
func (r *OrderRepository) Delete(ctx context.Context, o *orderdomain.Order) error {
	expected := o.Version
	res := DBOrTx(ctx, r.db).
		Where("id = ? AND version = ?", string(o.ID), expected).
		Delete(&OrderPO{})
	if res.Error != nil {
		return fmt.Errorf("mysql: delete order %q failed: %w", o.ID, res.Error)
	}
	if res.RowsAffected == 0 {
		return r.conflictErr(ctx, o.ID, expected)
	}
	return nil
}

// GetByID 按主键装载订单；未命中按契约返回 (nil, nil)。
func (r *OrderRepository) GetByID(ctx context.Context, id orderdomain.OrderID) (*orderdomain.Order, error) {
	var po OrderPO
	err := DBOrTx(ctx, r.db).Where("id = ?", string(id)).Take(&po).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("mysql: get order %q failed: %w", id, err)
	}
	o, err := poToOrder(&po)
	if err != nil {
		return nil, fmt.Errorf("mysql: map order %q failed: %w", id, err)
	}
	return o, nil
}

// CurrentVersion 只查版本列，供对账与乐观锁失败时定位实际版本。
func (r *OrderRepository) CurrentVersion(ctx context.Context, id orderdomain.OrderID) (uint64, bool, error) {
	var po struct {
		Version uint64
	}
	err := DBOrTx(ctx, r.db).Model(&OrderPO{}).
		Select("version").Where("id = ?", string(id)).Take(&po).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, fmt.Errorf("mysql: current version %q failed: %w", id, err)
	}
	return po.Version, true, nil
}

// conflictErr 在乐观锁未命中行时补查实际状态，交给框架生成标准冲突错误。
func (r *OrderRepository) conflictErr(ctx context.Context, id orderdomain.OrderID, expected uint64) error {
	actual, exists, err := r.CurrentVersion(ctx, id)
	if err != nil {
		return err
	}
	return domain.CheckVersion(id, expected, actual, exists)
}

// toOrderPO 聚合 → PO。落库版本取 PendingVersion：业务操作后的待提交版本，
// 未发生操作时等于基线，保证写入行版本与事件携带版本一致。
func toOrderPO(o *orderdomain.Order) OrderPO {
	return OrderPO{
		ID:        string(o.ID),
		OrderNo:   o.OrderNo,
		Amount:    o.Amount,
		Status:    o.Status.Value(),
		Version:   o.PendingVersion(),
		CreatedAt: o.CreatedAt,
		UpdatedAt: o.UpdatedAt,
	}
}

// poToOrder PO → 聚合：重建一个「已持久化、无待提交瞬态」的聚合。
// 并补回规则/操作注册表，使装载出的聚合同样可执行后续业务行为。
func poToOrder(po *OrderPO) (*orderdomain.Order, error) {
	status, err := statusFromValue(po.Status)
	if err != nil {
		return nil, err
	}
	o := &orderdomain.Order{
		AggregateRoot: domain.AggregateRoot[orderdomain.OrderID]{
			ID:        orderdomain.OrderID(po.ID),
			CreatedAt: po.CreatedAt,
			UpdatedAt: po.UpdatedAt,
		},
		OrderNo: po.OrderNo,
		Amount:  po.Amount,
		Status:  status,
	}
	o.MarkPersisted(po.Version)
	o.SetRuleRegistry(orderdomain.OrderRuleRegistry)
	o.SetOperationRegistry(orderdomain.OrderOperationRegistry)
	return o, nil
}

// statusFromValue 状态编码 → 富枚举；未知状态直接报错，避免坏数据静默降级。
func statusFromValue(value string) (orderdomain.OrderStatus, error) {
	switch value {
	case orderdomain.StatusCreated.Value():
		return orderdomain.StatusCreated, nil
	case orderdomain.StatusPaid.Value():
		return orderdomain.StatusPaid, nil
	case orderdomain.StatusCancelled.Value():
		return orderdomain.StatusCancelled, nil
	}
	return orderdomain.OrderStatus{}, fmt.Errorf("unknown order status %q", value)
}
