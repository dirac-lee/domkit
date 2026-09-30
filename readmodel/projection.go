// Package readmodel 提供 CQRS 读侧核心抽象：
// 读模型标记契约、聚合投影器（Projector）、读模型副本（ReadModelReplica）与分页值对象。
//
// 设计要点：
//   - 读模型（Projection）与写模型（domain.Aggregate）在类型层面严格分离；
//   - Projector 是「聚合 → 投影」的纯映射，不含任何存储细节，可独立单测；
//   - Go 以泛型类型参数在编译期钉死投影类型，因此无需 Java 版的运行时 projectionType()。
package readmodel

import "github.com/dirac-lee/domkit/domain"

// Projection 读模型（查询模型）标记契约。
// 只有面向查询的投影视图实现它，从而与写模型聚合在编译期区分开。
// ProjectionKind 返回投影逻辑名，供注册、调试与对账定位使用。
type Projection interface {
	ProjectionKind() string
}

// AggregateWithVersion 投影器对写模型聚合的最小约束：
// 具备工作单元要求的聚合能力，并能读取最近一次已持久化的基线版本。
type AggregateWithVersion interface {
	domain.Aggregate
	CurrentVersion() uint64
}

// Projector 聚合 → 投影 的纯映射器（函数类型）。
// 类型参数 AGG（写模型聚合）、P（读模型投影）在调用点被编译期推断、钉死。
// 实现必须无状态、无副作用：只做字段取值与派生，不访问存储或远程。
type Projector[AGG AggregateWithVersion, P Projection] func(agg AGG) P
