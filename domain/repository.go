package domain

import "context"

// Repository 聚合仓储接口，领域层定义、基础设施层实现。
//
// 类型参数：
//   - ID：聚合主键类型（string 系强类型 ID、int64、uint64 等任意可比较类型）；
//   - T：聚合根的值类型（接口方法内部统一使用 *T），例如订单聚合声明
//     Repository[OrderID, Order]，而不是 Repository[OrderID, *Order]，
//     否则方法签名会退化为 **Order。
type Repository[ID comparable, T any] interface {
	// Insert 持久化新建聚合。
	Insert(ctx context.Context, agg *T) error
	// Update 更新已存在聚合，必须做乐观锁检查：
	// 当存储中聚合版本与 agg.Version 不一致（或聚合已被删除）时，
	// 返回包装了 ErrConcurrencyConflict 的 *ConcurrencyConflictError[ID]。
	Update(ctx context.Context, agg *T) error
	// Delete 删除聚合；聚合已不存在时返回 ErrConcurrencyConflict（ORPHAN）。
	Delete(ctx context.Context, agg *T) error
	// GetByID 按主键装载聚合，未命中返回 (nil, nil)。
	GetByID(ctx context.Context, id ID) (*T, error)
	// CurrentVersion 返回写模型当前版本；exists=false 表示聚合不存在（供 ORPHAN 判定）。
	// 高频对账场景可实现为只查版本列，而非装载整聚合。
	CurrentVersion(ctx context.Context, id ID) (version uint64, exists bool, err error)
}

// CheckVersion 仓储 Update/Delete 实现可复用的乐观锁比较：
//   - 聚合不存在：返回 AggregateMissing 的冲突错误；
//   - 版本不一致：返回携带期望/实际版本的冲突错误；
//   - 一致：返回 nil。
func CheckVersion[ID comparable](id ID, expected, actual uint64, exists bool) error {
	if !exists {
		return NewConcurrencyConflictError(id, expected, actual, true)
	}
	if expected != actual {
		return NewConcurrencyConflictError(id, expected, actual, false)
	}
	return nil
}
