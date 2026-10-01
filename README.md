# domkit

domkit 是一个用 Go 泛型实现的 DDD（领域驱动设计）战术建模工具包。它以「窄端口 + 泛型参数对象 + 显式组合根」为核心范式，提供领域建模基座、应用编排、CQRS 读写分离、事务 outbox、对账自愈、配置开关和对外集成等能力。

- **Module**: `github.com/dirac-lee/domkit`
- **Go**: 1.27+
- **框架本体依赖**: 零第三方依赖
- **示例工程**: [`example/order`](example/order)，演示 MySQL、Redis、GORM、outbox、读模型和对账链路

## 适合什么场景

domkit 适合希望在 Go 项目中落地 DDD 战术模式，但又不想把业务代码绑死在具体数据库、消息队列或 Web 框架上的团队。

它重点解决：

- 聚合根、实体、值对象、领域事件、业务规则等基础模型重复搭建的问题。
- 命令执行、规则校验、持久化、事件分发之间缺少统一管线的问题。
- 写模型、读模型、outbox、对账补偿等工程能力难以组合的问题。
- Go 泛型下如何让仓储、工作单元、事件和 ID 类型保持类型安全的问题。

## 安装

在你的业务模块中直接安装：

```bash
go get github.com/dirac-lee/domkit
```

如果你在本仓库内开发 domkit 或运行内置示例，根目录已经通过 `go.work` 串联框架模块和 `example/order` 子模块。

## 最小示例

下面示例展示一个最小 Todo 聚合：领域行为负责修改状态，并通过 `BeginCreate` / `BeginModify` 显式生成审计时间和本次变更版本；执行器负责规则校验、持久化和提交版本。

```go
package main

import (
	"context"
	"fmt"

	"github.com/dirac-lee/domkit/application"
	"github.com/dirac-lee/domkit/domain"
	"github.com/dirac-lee/domkit/infra/persist"
)

type TodoID string

type Todo struct {
	domain.AggregateRoot[TodoID]
	Title string
	Done  bool
}

func NewTodo(id TodoID, title string) *Todo {
	t := &Todo{AggregateRoot: domain.AggregateRoot[TodoID]{ID: id}, Title: title}
	t.BeginCreate()
	return t
}

func (t *Todo) Complete() error {
	t.Done = true
	t.BeginModify()
	return nil
}

func main() {
	ctx := context.Background()
	repo := persist.NewAggregateRepository(func(t *Todo) TodoID { return t.ID })
	todo := NewTodo("t1", "buy milk")

	_, err := application.NewCommandExecutor(nil).
		Execute(ctx, todo, repo, func(t *Todo) error { return t.Complete() })

	fmt.Println("err:", err, "done:", todo.Done, "version:", todo.Version)
}
```

运行：

```bash
go mod tidy
go run .
```

输出类似：

```text
err: <nil> done: true version: 1
```

## 端到端示例

[`example/order`](example/order) 是一个独立子模块，演示订单创建、支付、取消、读模型投影、Redis 缓存、支付幂等、事务 outbox、对外广播和对账自愈。

启动依赖和服务：

```bash
cd example/order
make up
make run
```

另开终端调用接口：

```bash
curl -s -X POST localhost:8080/orders \
  -H 'Content-Type: application/json' \
  -d '{"orderNo":"NO1001","amount":5000}'

curl -s 'localhost:8080/orders'
```

常用命令：

```bash
make test
make test-integration
make reset
```

详细运行说明、HTTP 路由、配置项和容器环境准备见 [完整指南](docs/guide.md)。

## 架构概览

```text
入站适配器
  -> application.CommandExecutor
  -> domain 聚合 / 规则 / 事件
  -> domain.Repository / UnitOfWork 端口
  -> infra 持久化 / 事件总线 / outbox / ACL
  -> readmodel / reconciliation / broadcast
```

核心原则：

- **领域优先**: 领域行为显式推进版本、维护审计时间、收集事件。
- **端口隔离**: domain/application 不依赖数据库、Redis、HTTP 或消息队列。
- **组合根装配**: 事务、outbox、投影、广播等基础设施能力在应用边界组合。
- **类型安全**: ID、仓储、事件和枚举通过泛型约束减少运行期错配。

## 包目录

| 路径 | 说明 |
|---|---|
| [`domain`](domain) | 聚合根、实体、值对象、领域事件、规则、仓储端口、工作单元端口、枚举、ID 生成端口 |
| [`application`](application) | 命令执行器、dry-run、事务 SPI、基础工作单元、命令/查询抽象 |
| [`infra/persist`](infra/persist) | 内存聚合仓储、乐观锁 CAS、UUID/号段发号器、子实体变更跟踪 |
| [`infra/eventbus`](infra/eventbus) | 同步/异步本地事件总线、重试、死信和指标接口 |
| [`infra/outbox`](infra/outbox) | 事务发件箱、outbox 工作单元、认领补偿 relay |
| [`infra/acl`](infra/acl) | 防腐层调用模板、先查后写幂等、错误分类、调用日志 |
| [`readmodel`](readmodel) | 读模型投影、副本、分页对象 |
| [`reconciliation`](reconciliation) | 写模型和读副本的一致性检测与自愈 |
| [`config`](config) | 配置源、类型转换、结构体绑定、三态特性开关 |
| [`broadcast`](broadcast) | 出域消息信封、序列化器、发送端口和事件订阅者 |

## 文档

- [完整指南](docs/guide.md): 原 README 的完整手册版本，包含包能力说明、order 示例细节、Docker/Colima 准备和测试命令。
- [Wiki](docs/wiki.md): 设计背景和更长篇的项目说明。

## 开发

框架本体：

```bash
go build ./...
go vet ./...
go test ./...
```

order 示例：

```bash
cd example/order
make test
```

集成测试需要 MySQL 和 Redis，可通过 `make up` 拉起；探测不到依赖时集成用例会跳过。

## 项目状态

domkit 处于早期演进阶段，API 仍可能根据真实业务使用反馈调整。当前仓库尚未声明许可证；对外正式复用前建议补充 `LICENSE` 并打 tag 发布版本。

