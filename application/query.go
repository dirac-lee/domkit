package application

import "context"

// Query 查询接口（CQRS 读模型）
type Query[T any] interface {
	Execute(ctx context.Context) (*T, error)
}
