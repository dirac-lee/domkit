package application

import (
	"context"
	"errors"
)

var (
	// ErrNilTransactionManager reports a missing transaction manager.
	ErrNilTransactionManager = errors.New("application: transaction manager is nil")
	// ErrNilTxFunc reports a missing transaction callback.
	ErrNilTxFunc = errors.New("application: transaction function is nil")
)

// Propagation 事务传播行为（技术无关，由基础设施模块解释执行）。
type Propagation uint8

const (
	// PropagationRequired 加入当前事务，无则新建（聚合写 + outbox 写同事务的默认选择）。
	PropagationRequired Propagation = iota
	// PropagationRequiresNew 挂起当前事务并新建独立事务（补偿/对账等独立短事务）。
	PropagationRequiresNew
)

// TransactionManager 最小事务 SPI（应用层定义、基础设施实现），
// 把工作单元的持久化动作绑定到同一数据库事务。
type TransactionManager interface {
	// DoInTx 在事务内执行 fn：
	// fn 返回 nil 时提交事务并返回 nil；fn 返回错误时回滚事务并原样返回该错误。
	// tx 为底层事务句柄（如 *sql.Tx），参与同事务的仓储/outbox 由 fn 闭包捕获绑定；
	// 无事务能力的实现可传 nil。
	DoInTx(ctx context.Context, p Propagation, fn func(ctx context.Context, tx any) error) error
}

// InTx 在事务内执行带类型返回值的回调，是 TransactionManager 的泛型便利封装。
func InTx[R any](ctx context.Context, tm TransactionManager, p Propagation,
	fn func(ctx context.Context, tx any) (R, error)) (R, error) {
	var result R
	if tm == nil {
		return result, ErrNilTransactionManager
	}
	if fn == nil {
		return result, ErrNilTxFunc
	}
	err := tm.DoInTx(ctx, p, func(ctx context.Context, tx any) error {
		r, e := fn(ctx, tx)
		result = r
		return e
	})
	return result, err
}

// NoopTxManager 空事务实现：不开启真实事务，直接执行回调并透传 tx=nil。
// 适用于内存仓储/测试/无需事务保证的场景。
type NoopTxManager struct{}

// DoInTx 实现 TransactionManager。
func (NoopTxManager) DoInTx(ctx context.Context, _ Propagation, fn func(context.Context, any) error) error {
	return fn(ctx, nil)
}
