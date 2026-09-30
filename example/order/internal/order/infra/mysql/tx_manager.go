package mysql

import (
	"context"
	"errors"

	"github.com/dirac-lee/domkit/application"
	"gorm.io/gorm"
)

// ctx 键：均为不导出的空结构体，避免键冲突与误读。
type (
	txCtxKey    struct{} // 存当前事务句柄 *gorm.DB
	hooksCtxKey struct{} // 存当前事务的提交后钩子收集器
)

// GormTxManager 基于 GORM 的事务管理器，实现 application.TransactionManager。
type GormTxManager struct {
	db *gorm.DB
}

// NewGormTxManager 创建事务管理器。
func NewGormTxManager(db *gorm.DB) *GormTxManager {
	return &GormTxManager{db: db}
}

// DoInTx 执行事务回调，支持两种传播行为：
//   - Required：ctx 已带事务则复用（含其提交后钩子），否则新建；
//   - RequiresNew：总是用基础连接开独立新事务（补偿/对账等场景）。
func (m *GormTxManager) DoInTx(ctx context.Context, p application.Propagation,
	fn func(context.Context, any) error) error {
	if p == application.PropagationRequired {
		// 提前返回：复用外层事务，避免无谓嵌套。
		if tx := txFromContext(ctx); tx != nil {
			return fn(ctx, tx)
		}
	}
	return m.runNewTx(ctx, fn)
}

// runNewTx 开一个新事务：随事务建立独立的提交后钩子收集器，
// 事务提交成功后统一执行钩子；任一步失败都不会触发钩子。
func (m *GormTxManager) runNewTx(ctx context.Context, fn func(context.Context, any) error) error {
	hooks := newTxHooks()
	ctx = contextWithHooks(ctx, hooks)

	err := m.db.Transaction(func(tx *gorm.DB) error {
		// 事务句柄注入 ctx：事务内仓储/发件箱据此共享同一 tx。
		return fn(contextWithTx(ctx, tx), tx)
	})
	if err != nil {
		return err
	}
	// 事务已落库：执行「提交成功后」动作（如即时发布事件）。
	return hooks.runAfterCommit()
}

// DBOrTx 取出 ctx 中的事务句柄，无事务时回退基础连接。
// 仓储据此在「事务内 / 事务外」两种路径下使用同一套读写代码。
func DBOrTx(ctx context.Context, db *gorm.DB) *gorm.DB {
	if tx := txFromContext(ctx); tx != nil {
		return tx.WithContext(ctx)
	}
	return db.WithContext(ctx)
}

// OnAfterCommit 登记一个「当前事务提交成功后执行」的回调。
// ctx 不在事务中（测试/非事务路径）时立即执行，保证回调不丢。
func OnAfterCommit(ctx context.Context, fn func() error) {
	hooks, ok := ctx.Value(hooksCtxKey{}).(*txHooks)
	if !ok {
		_ = fn()
		return
	}
	hooks.add(fn)
}

// ---- ctx 读写小工具 ----

func contextWithTx(ctx context.Context, tx *gorm.DB) context.Context {
	return context.WithValue(ctx, txCtxKey{}, tx)
}

func txFromContext(ctx context.Context) *gorm.DB {
	tx, _ := ctx.Value(txCtxKey{}).(*gorm.DB)
	return tx
}

func contextWithHooks(ctx context.Context, hooks *txHooks) context.Context {
	return context.WithValue(ctx, hooksCtxKey{}, hooks)
}

// ---- 提交后钩子收集器 ----

type txHooks struct {
	afterCommit []func() error
}

func newTxHooks() *txHooks {
	return &txHooks{}
}

func (h *txHooks) add(fn func() error) {
	h.afterCommit = append(h.afterCommit, fn)
}

// runAfterCommit 依次执行钩子并聚合错误：此时主事务已提交无法回滚，
// 失败的事件仍留在 outbox（status=pending），由 Relay 扫描兜底。
func (h *txHooks) runAfterCommit() error {
	var joined error
	for _, fn := range h.afterCommit {
		joined = errors.Join(joined, fn())
	}
	return joined
}
