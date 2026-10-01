# domkit 完整指南

本文保留 domkit 的完整说明，包括包能力细节、端到端示例、HTTP 契约、运行环境和测试命令。首次了解项目建议先阅读仓库根目录的 [README](../README.md)。

用 Go 泛型实现的 DDD（领域驱动设计）战术建模框架。以「窄端口 + 泛型参数对象 + 显式组合根」为核心范式，提供从领域基座、CQRS 读写分离、对账自愈、配置开关到对外集成的一整套可运行能力，并附带一个端到端示例 `example/order`。

- **Module**：`github.com/dirac-lee/domkit`
- **Go 版本**：1.27
- **第三方依赖**：无 —— `go.mod` 不声明任何 `require`，仅使用 Go 1.27 工具链（含其内置 `uuid` 包）
- **设计对齐**：Java 参考实现 `pragmatic-ddd`，但以 Go 惯用方式落地（结构化类型替代继承、泛型在编译期钉死类型）

---

## 快速开始

前置条件：Go 1.27。**框架本体**零第三方依赖，拿到源码即可构建，不依赖任何数据库或消息中间件；下方 order 示例为贴近生产，则会真实使用 MySQL 与 Redis（用容器一键拉起，本机无需另装）。

### 第一步：1 分钟跑通示例

进入示例目录，用容器一键拉起 MySQL 与 Redis（**首次使用请先按第 5.2、5.3 节安装运行时并配置镜像加速**）：

```bash
cd example/order
make up                 # 拉起 MySQL :3306 + Redis :6379
make ps                 # 查看依赖健康状态
```

MySQL 首次启动约需 20~30 秒初始化，**请等两个容器都变为 healthy 再启动服务**，否则可能抢跑连不上库：

```bash
docker compose ps       # 两个服务的 STATUS 均显示 (healthy)
```

然后**仍在当前目录（example/order）**启动服务（连接配置已内置在 [`main.go`](example/order/cmd/orderserver/main.go)，首次启动自动建表）：

```bash
make run                # 等价 go run ./cmd/orderserver
# order server listening on :8080
```

另开一个终端，体验写链路（创建）与读链路（列表查询）：

```bash
# 创建订单：amount 必须为正，否则被领域规则拦截、不会落库
curl -s -X POST localhost:8080/orders \
  -H 'Content-Type: application/json' \
  -d '{"orderNo":"NO1001","amount":5000}'

# 分页查看订单列表（走读模型）
curl -s 'localhost:8080/orders'
```

> 提示：用相同 `orderNo` 重复创建会返回 409「订单号已存在」（`orders.uk_no` 唯一键约束，重试/重发时常见）。想清空数据从头体验，可在 `example/order` 执行 `make reset`（停容器、删数据卷后重新初始化）。

返回统一信封，形如 `{"code":0,"data":{...}}`。完整路由清单见第 4 节。

### 第二步：从零写一个最小程序

先建一个独立模块，并从远端安装 domkit：

```bash
mkdir hello-domkit && cd hello-domkit
go mod init hello-domkit
go get github.com/dirac-lee/domkit
```

如果你正在本机同时开发 domkit 和示例程序，可以临时使用 `go mod edit -replace=github.com/dirac-lee/domkit=/你的本地路径/domkit` 指向本地源码；普通使用不需要 `replace`。

一条能落库的命令只需三样东西：嵌入基座的聚合、自动接线的内存仓储、一个命令执行器。把下面内容存为 `main.go`：

```go
package main

import (
	"context"
	"fmt"

	"github.com/dirac-lee/domkit/application"
	"github.com/dirac-lee/domkit/domain"
	"github.com/dirac-lee/domkit/infra/persist"
)

// TodoID 强类型主键，避免不同实体的 ID 互相串用
type TodoID string

// Todo 聚合：嵌入 AggregateRoot 即获得版本控制、规则收集、事件等能力
type Todo struct {
	domain.AggregateRoot[TodoID]
	Title string
	Done  bool
}

// NewTodo 工厂方法：初始化聚合审计时间，并生成新建版本。
func NewTodo(id TodoID, title string) *Todo {
	t := &Todo{AggregateRoot: domain.AggregateRoot[TodoID]{ID: id}, Title: title}
	t.BeginCreate()
	return t
}

// Complete 领域行为：修改状态后，通过 BeginModify 刷新审计时间并取得本次变更版本。
// 注意：乐观锁版本必须由领域行为通过 BeginCreate/BeginModify 主动取，执行器不会自动 +1；
// 若漏调，仓储行版本会一直停在 0，并发更新时 CAS 恒为 0==0，检测不到丢失更新。
// 真实项目里还可在此 CheckRule / CollectEvent。
func (t *Todo) Complete() error {
	t.Done = true
        t.BeginModify()
	return nil
}

func main() {
	ctx := context.Background()

	// 仓储：聚合内嵌基座，版本读写接缝由编译器自动接线，只需给主键提取函数
	repo := persist.NewAggregateRepository(func(t *Todo) TodoID { return t.ID })

        todo := NewTodo("t1", "买菜")

	// 执行器统一编排：领域逻辑 → 规则校验 → 持久化（→ 事件分发）
	_, err := application.NewCommandExecutor(nil).
		Execute(ctx, todo, repo, func(t *Todo) error { return t.Complete() })
	fmt.Println("err:", err, "done:", todo.Done, "version:", todo.Version)
}
```

补齐依赖并运行：

```bash
go mod tidy
go run .
```

输出：

```text
err: <nil> done: true version: 1
```

落库后版本基线为 1：版本号由领域行为里的 `BeginCreate/BeginModify` 主动取，执行器在持久化成功后据此把基线从 0 推进到 1。规则不通过时执行器会提前中断，不触达持久化。

### 第三步：下一步

你已经跑通了最小命令，继续深入：

- 整体分层与 10 个包的职责，见第 1 节；
- 一个真实分层业务进程如何协作（CQRS 读写分离、对账自愈、配置开关、对外广播），直接读 `example/order` 源码，装配入口是 `wiring.go`，业务能力与 HTTP 路由见第 4 节；
- 各能力的完整说明见第 2、3 节。

---

## 1. 分层架构

```
                         ┌──────────────────────────────────────────┐
   入站适配器            │  HTTP (example/order/internal/httpapi)    │
                         └───────────────────┬──────────────────────┘
                                             │ 调用用例
                         ┌───────────────────▼──────────────────────┐
   应用层 application    │  CommandExecutor / SimpleUnitOfWork       │
                         │  领域逻辑 → 规则校验 → 持久化 → 事件分发   │
                         └───────────────────┬──────────────────────┘
                                             │ 依赖端口
   ┌──────────────┬──────────────────────────▼───────────────────┬──────────────┐
   │  readmodel   │                  domain（核心）                │ reconciliation│
   │ 读模型/分页   │  AggregateRoot / Entity / ValueObject / Event │  对账 + 自愈  │
   └──────────────┴───────────────┬──────────────────────────────┴──────────────┘
                                  │ Repository / IDGenerator / UoW（端口）
                  ┌───────────────▼────────────────┐
   infra 基础设施  │ persist · eventbus · outbox · acl │
                  └───────────────┬────────────────┘
                                  │ ACL 防腐 / 消息 / 发号
                              外部系统
```

另有两个横切能力包：[`config`](config)（配置源、类型化绑定、特性开关）与 [`broadcast`](broadcast)（出域消息）。

### 包职责速览

| 包 | 职责 |
|---|---|
| [`domain`](domain) | DDD 战术建模基座：聚合根、实体、值对象、领域事件、业务规则、仓储/工作单元端口、枚举目录、号段端口 |
| [`application`](application) | 应用编排：命令执行器标准管线、零副作用试跑、事务 SPI、基础工作单元、命令/查询抽象 |
| [`readmodel`](readmodel) | CQRS 读侧：投影标记契约、纯映射投影器、读模型副本、分页值对象与内存副本实现 |
| [`reconciliation`](reconciliation) | 读副本与写模型的一致性对账：状态判定、检测 + 立即补救、去重、对账管理器 |
| [`config`](config) | 配置源、类型转换内核、`Get/GetOr`、反射 tag 绑定、三态特性开关与白名单灰度 |
| [`broadcast`](broadcast) | 出域消息：统一信封、序列化器、发送端口、事件订阅者，错误按可/不可重试分类 |
| [`infra/acl`](infra/acl) | 防腐层调用套路：三段转换模板、先查后写幂等、错误二分、调用日志 |
| [`infra/eventbus`](infra/eventbus) | 本地事件总线：同步顺序 fan-out 与 worker 池异步投递（有界队列/重试/死信） |
| [`infra/outbox`](infra/outbox) | 事务发件箱：消息模型、存储 SPI、Outbox 工作单元、认领补偿中继 Relay |
| [`infra/persist`](infra/persist) | 内存聚合仓储（乐观锁 CAS）、UUID/号段发号器、发号器注册中心、子实体变更跟踪 |

---

## 2. 核心能力

### 2.1 domain —— 领域建模基座

**聚合根 [`AggregateRoot[ID]`](domain/aggregate.go)**（业务聚合以嵌入方式获得全部能力）

- 身份与审计：`ID`、`Version`（乐观锁基线，新建为 0）、`CreatedAt/UpdatedAt`、`Deleted`（软删）。
- 规则违反收集：`AddBrokenRule / AddBrokenRuleWithParams / CheckRule`；`BrokenRules / HasBrokenRules / ClearBrokenRules`；`FirstRuleError / AggregateRuleError`。消息描述经注册表解析，未注册回退到消息码自带描述。
- 审计时间：`MarkCreated / MarkModified` 由领域对象基类取当前时间；推荐通过 `BeginCreate / BeginModify` 在领域工厂与领域行为中同时完成审计标记与版本生成，仓储只保存聚合上的时间字段。
- 版本控制：`IsNew / CurrentVersion / NextVersion`（同一操作内幂等，基线 +1，不提前推进）、`BeginCreate / BeginModify`（返回本次变更版本）、`PendingVersion / CommitVersion`（持久化成功后推进基线）、`MarkPersisted`（仓储装配时重建基线）。
- 成因操作：`SetOperationRegistry / RecordOperation`（未注册返回 `ErrOperationNotRegistered`）；`CollectEvent` 时把当前操作码经框架内部接缝自动回填到事件；`PullEvents` 取出并清空。

**其他建模原语**

- 子实体 [`Entity[ID]`](domain/entity.go)：审计字段、`MarkCreated/MarkModified`、`SameIdentityAs`。
- 值对象 [`ValueObject`](domain/valueobject.go)：以 `EqualityComponents() []any` 声明相等分量，包函数 `Equal` 基于 `reflect.DeepEqual` 比较（含 nil 与分量长度短路）。
- 领域事件：擦除接口 [`DomainEvent`](domain/domainevent.go)（`EventID/AggregateKey/AggregateVersion/EventName/OccurredAt/OperationCode`）与泛型基座 [`BaseDomainEvent[ID]`](domain/domainevent.go)；`NewEventID()` 生成 UUIDv7。
- 业务规则：[`BusinessRule`](domain/rule.go)（`IsSatisfied/BrokenRule`）、[`BrokenRule`](domain/rule.go)、并发安全的 [`BrokenRuleRegistry`](domain/rule_registry.go)、独立校验器 [`RuleValidator`](domain/rule.go)（支持 fail-fast 与全量收集）；消息码 [`MessageCode`](domain/message_code.go)。
- 操作体系：[`EntityOperation`](domain/operation.go)（内置 `OperationNew/OperationDelete`）、[`OperationRegistry`](domain/operation.go)、[`TriggeredOperations`](domain/operation.go)（`Contains/ContainsAny/ContainsAll`）。
- 仓储端口 [`Repository[ID,T]`](domain/repository.go)：`Insert/Update/Delete/GetByID/CurrentVersion`；包函数 `CheckVersion` 复用乐观锁比较。
- 工作单元 [`UnitOfWork`](domain/unitofwork.go)：`RegisterChange/Commit/Rollback`；泛型登记函数 `RegisterNew/RegisterModified/RegisterDeleted`；与主键类型无关的最小聚合接口 `Aggregate` 与 `PersistFunc`。
- 富枚举：[`EnumValue[CODE]`](domain/enum.go)（`Value/Name`）、可选 `DescribedEnum`、不可变目录 [`EnumCatalog`](domain/enum.go)（`ByCode/Resolve/All`，未命中 `ErrEnumCodeNotFound`）。
- 标识生成端口 [`IDGenerator[ID]`](domain/id.go)（`BizKey/NextID/NextIDs`）、号段值对象 [`Segment`](domain/id.go)（`HasNext/Take/Remaining`，耗尽 `ErrSegmentExhausted`）、号段分配器端口 [`SegmentAllocator`](domain/id.go)。
- 变更追踪容器 [`TrackedMap[K,V]`](domain/tracked_map.go)：按「新增/更新/删除」三桶追踪，`Inserted/Updated/Removed/All` 输出净增量。

**领域错误**：[`DomainRuleError`](domain/errors.go)（单条/聚合，`IsDomainRuleError` 判定）与 [`ConcurrencyConflictError[ID]`](domain/errors.go)（期望/实际版本、缺失标记，`Unwrap` 支持 `errors.Is`，`IsConcurrencyConflict` 判定）。

### 2.2 application —— 应用编排

- 命令执行器 [`CommandExecutor`](application/executor.go)：非泛型实例服务全部聚合类型，以泛型方法在调用点绑定类型。
  - `Execute` 固定管线：领域逻辑 → 规则校验 → 持久化（+ 事件分发）→ 返回聚合；规则不通过时丢弃暂存事件、不触达持久化。
  - `TryExecute`：与 `Execute` 同源但跳过持久化与分发，以 [`DryRunResult`](application/dryrun.go) 返回结论，`defer` 保证零副作用。
  - 两种持久化模式：`NewCommandExecutor`（内部新建 [`SimpleUnitOfWork`](application/unitofwork.go)）与 `NewTxCommandExecutor`（在事务内由工厂构造工作单元，如 outbox 场景）。
- 基础工作单元 `SimpleUnitOfWork`：防重复提交；**两阶段提交** —— 先全部聚合落库并推进版本基线，全部成功后再统一分发事件（dispatcher 为 nil 则丢弃）。
- 事务 SPI [`TransactionManager`](application/tx.go)：`DoInTx`（提交/回滚、tx 句柄透传）；传播行为 `PropagationRequired/PropagationRequiresNew`；泛型封装 `InTx` 把无返回值事务适配为带类型返回；`NoopTxManager` 不开真实事务。
- 命令/查询抽象：[`Command`](application/command.go)、[`Query[T]`](application/query.go)、应用服务基类 [`BaseAppService`](application/appservice.go)（执行失败或 panic 时回滚）。

### 2.3 readmodel —— CQRS 读侧

- 投影契约 [`Projection`](readmodel/projection.go)（`ProjectionKind`）与 [`AggregateWithVersion`](readmodel/projection.go)（聚合 + 当前版本）。
- 纯映射投影器 [`Projector[AGG,P]`](readmodel/projection.go)：`聚合 → 投影`，无状态、无存储细节，可独立单测。
- 读模型副本契约 [`ReadModelReplica[ID]`](readmodel/memory_replica.go)：`ReplicaID/ReadVersion/Rebuild/PurgeOrphan`。
- 线程安全内存副本 [`MemoryReplica[ID,P]`](readmodel/memory_replica.go)：
  - `Sync`（事件触发实时投影）/`Rebuild`（从写模型快照重建）/`PurgeOrphan`（写模型已无则清残留，其他错误透传）。
  - 查询：`GetByID/List/Page`（offset 越界返空页、截断末页，均携带 total）、`ReadVersion`。
- 分页值对象：[`PageRequest`](readmodel/paging.go)（构造期校验：页码 ≥ 1，页大小 ∈ [1,200]；方法 `PageNumber/PageSize/Offset`）与 [`PageResult[T]`](readmodel/paging.go)（以下三者均为**方法而非字段**：`Data()/TotalCount()/Request()`）。

**事件驱动投影的三处接缝**：独立组装 CQRS 时，执行器↔总线、事件↔强类型 ID、仓储↔副本之间需在组合根补三层适配（`example/order` 的 [`wiring.go`](example/order/internal/order/wiring.go) 即如此接线）：

```go
// 接缝① EventDispatcher 是函数类型，桥接到总线 Publish
executor := application.NewCommandExecutor(
	func(_ context.Context, evt domain.DomainEvent) error { return bus.Publish(evt) })

// 接缝② 事件边界主键统一收敛为 string（AggregateKey），消费侧转回强类型 ID 再 Sync
bus.Subscribe("task.completed", "summary-projector", eventbus.FuncHandler(
	func(evt domain.DomainEvent) error { return replica.Sync(ctx, TaskID(evt.AggregateKey())) }), 0)

// 接缝③ 仓储未命中约定为 (nil,nil)，副本 load 契约要求 ErrAggregateNotFound
load := func(ctx context.Context, id TaskID) (*Task, error) {
	t, err := repo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if t == nil {
		return nil, readmodel.ErrAggregateNotFound
	}
	return t, nil
}
replica := readmodel.NewMemoryReplica("task-summary", load, projector)
```

### 2.4 reconciliation —— 对账与自愈

- 一致性状态 [`Status`](reconciliation/status.go)：`CONSISTENT / STALE / ORPHAN / UNTRACKED`（零值非法，避免未初始化误判为一致）。
- 结果值对象 [`Reconciliation`](reconciliation/status.go)：携带读/写版本与 `IsConsistent/IsStale/IsOrphan/IsUntracked`；纯函数 `Judge(readV, writeV)` 按固定优先级判定。
- 对账原语（[reconciler.go](reconciliation/reconciler.go)）：写模型端口 `VersionReader`（仓储自动满足）；`Reconcile` 仅检测；`ReconcileAndResync` 检测后 STALE → `Rebuild`、ORPHAN → `PurgeOrphan`。
- 管理器 [`Manager[ID]`](reconciliation/manager.go)：`RegisterReplica`（按副本自报 ID 保序登记）、`Reconcile`（全副本）、`ReconcileReplica`（单副本，未注册 `ErrReplicaNotFound`）、`ReconcileBatch`（候选 ID 批量，结果按聚合分组）。
- 去重：[`Dedup`](reconciliation/dedup.go)（`ShouldSkip/Mark`）与默认 `NoOpDedup`，可经 `SetDedup` 注入。

### 2.5 config —— 配置与特性开关

- 配置源 [`Source`](config/source.go)（`Lookup/Keys`）与并发安全内存实现 [`MapSource`](config/map_source.go)：`NewMapSource/NewMapSourceFrom`（独立拷贝）、链式 `Set`、`Keys` 返回升序副本。
- 类型转换：泛型 [`Convert[T]`](config/convert.go) —— 内建类型（string/bool/Duration/int 全族/uint 全族/float）零反射，自定义类型走 `encoding.TextUnmarshaler`；反射路径用 `convertForType`（Binder 专用，支持命名类型）。
- 读取：[`Get[T]`](config/get.go)（缺失返回 `ErrKeyNotFound`）、`GetOr[T]`（兜底默认值）。
- 反射绑定 [`Bind[T]`](config/bind.go)：tag 形态 `config:"name,required,default=xxx"`、`config:"-"` 跳过；无 tag 字段名自动驼峰→kebab；支持嵌套/嵌入与 `WithDefault`；错误为 `BindingError`（`Unwrap`）。
- 特性开关 [`Toggle`](config/toggle.go)：三态 [`State`](config/state.go)（Off/Rollout/On）；`Enabled/EnabledOr/EnabledFor/StateOf`；灰度维度 [`FeatureContext`](config/feature_context.go) 与策略 [`GrayStrategy`](config/gray.go)（内置 [`WhitelistStrategy`](config/gray.go)，配置格式 `{key}.allow.{dimension}=v1,v2`）。
- 三态配置取值（[`state.go`](config/state.go) 的 `parseState`）：原始值先 `TrimSpace` 再转大写，故**大小写不敏感**——`ON` 全量开启、`ROLLOUT` 进入灰度判定；其余（`OFF`、空白、无法识别、`true/false`、键缺失）一律按关闭。
- 聚合门面 [`Context`](config/context.go)：`NewContext` 聚合 Source 与 Toggle，提供 `Source/Toggle`。

### 2.6 broadcast —— 出域消息

- 统一信封 [`Envelope[P]`](broadcast/envelope.go)：`MessageID/AggregateType/AggregateID/Version/CauseOperation/OccurredAt/SchemaVersion/SourceEventID` + 自由消息体 `Payload`；`NewEnvelope` 从领域事件填充元数据。
- 序列化端口 [`Serializer`](broadcast/serializer.go) 与零依赖 `JSONSerializer`。
- 发送端口 [`Messenger`](broadcast/messenger.go)：`Send(ctx, topic, senderCode, serializedEnvelope)`，与内部事件 MQ 链路解耦。
- 事件订阅者 [`Subscriber[P]`](broadcast/subscriber.go)：参数对象内聚「构消息体 → 组信封 → 序列化 → 发送」，其 `Handle` 满足 `infra/eventbus.EventHandler`，可直接注册（本包仅依赖 domain）。
- 装配期校验 [`Subscriber.Validate`](broadcast/subscriber.go)：注册到事件总线前可显式调用，配置缺失在启动期即以不可重试错误暴露（fail-fast）；`Handle` 内也会再校验一次兜底。
- 错误分类：[`SendError`](broadcast/errors.go)（发送阶段，**可重试**）、[`EnvelopeError`](broadcast/errors.go)（构消息/序列化/配置缺失，**不可重试**）；`IsRetryable` 判定，重复包装不嵌套。

---

## 3. 基础设施能力（infra）

### 3.1 infra/acl —— 防腐层

- 三段转换模板 [`CallPipeline[P,R,Q,S]`](infra/acl/external_call.go)：`ToRequest`（领域入参→对方请求）→ `DoCall`（外部调用）→ `ToResult`（对方响应→领域结果）；包入口 `Query`（无副作用）与 `Write`（有副作用）。
- 先查后写幂等 [`WriteIdempotent`](infra/acl/idempotent_gateway.go)：提取唯一键 → 查重，命中则经 `FromExisting` 转换已存在记录并短路，未命中复用标准写入流程。
- 错误二分：[`ConversionError`](infra/acl/exception.go)（本地转换，不可重试）与 [`CommunicationError`](infra/acl/exception.go)（外部通信，可重试），均 `Unwrap`；`IsRetryableErr` 统一裁决（领域规则错误不可重试，未知异常默认可重试）。
- 调用日志 [`CallLogger`](infra/acl/logger.go)：`OnRequest/OnResponse/OnError` 三个钩子，nil 时回退 `NopCallLogger`。

### 3.2 infra/eventbus —— 本地事件总线

- SPI [`EventBus`](infra/eventbus/eventbus.go)：`Subscribe(eventName, subscriberName, handler, priority)`、`Publish`；函数适配器 `FuncHandler` 与类型安全订阅 `SubscribeTo`。
- 同步总线 [`MemoryEventBus`](infra/eventbus/memory_bus.go)：在调用方 goroutine 按 **priority 降序** fan-out（同序保登记顺序），任一处理器失败即中断。
- 异步总线 [`AsyncEventBus`](infra/eventbus/async_bus.go)：worker 池 + 有界队列（默认 1 worker / 1024 容量，满则阻塞背压）；固定延时重试，重试耗尽进死信；`QueueLen/Shutdown`（优雅等待队列排空与在途处理）。
- 监控 [`Metrics`](infra/eventbus/metrics.go)：`Published/Consumed/DeadLettered`，默认 `NopMetrics`。

### 3.3 infra/outbox —— 事务发件箱

- 消息模型 [`OutboxMessage`](infra/outbox/outbox_store.go) 与四状态 `pending / processing / sent / deadletter`。
- 存储 SPI [`OutboxStore`](infra/outbox/outbox_store.go)：`SaveMessage/ClaimPending/MarkSent/Release/MoveToDeadLetter` —— 认领与状态推进全程带 **claim token 守卫**；参考实现 [`MemoryOutboxStore`](infra/outbox/memory_store.go)（支持注入时钟、租约过期重认领）。
- 工作单元 [`OutboxUnitOfWork`](infra/outbox/outbox_uow.go)：同一事务内先落库聚合、再逐条序列化事件 `SaveMessage`；JSON 序列化器 [`JsonEventSerializer`](infra/outbox/outbox_uow.go)（按「事件名→工厂」注册）。
- 补偿中继 [`Relay`](infra/outbox/relay.go)：后台定时 `ClaimPending` 原子认领并发布，失败按 `MaxRetry` 释放回 pending 或转死信；语义 **at-least-once**（依赖消费端幂等）。

### 3.4 infra/persist —— 持久化参考实现

- 内存仓储 [`MemoryRepository[ID,T]`](infra/persist/memory_repository.go)：经 [`VersionAccess`](infra/persist/memory_repository.go) 接缝读写版本；`Insert/Update/Delete` 严格执行**行版本 == 聚合基线**的乐观锁 CAS；`GetByID` 返回副本并重建版本瞬态；`CurrentVersion/Len`。
- 零样板构造器 [`NewAggregateRepository`](infra/persist/memory_repository.go)：聚合内嵌 `domain.AggregateRoot` 时，版本接缝由编译器按方法集自动接线，调用方仅提供主键提取函数；未嵌基座或需自定义版本读写时再用 `NewMemoryRepository`。
- 发号器（[id_generator.go](infra/persist/id_generator.go)）：
  - [`UUIDGenerator`](infra/persist/id_generator.go)（UUIDv7，约束 `~string`，支持批量）。
  - [`SegmentIDGenerator`](infra/persist/id_generator.go)：段内取号、边界自动跨段；成品 `NewInt64SegmentGenerator` 与 `NewFormattedStringSegmentGenerator`（如 `ORD-%08d`）。
  - 号段分配器：内存实现 [`MemorySegmentAllocator`](infra/persist/id_generator.go)（默认步长 1000，可 Seed/SetStep）与数据库端口 `SegmentStore` + [`StoreSegmentAllocator`](infra/persist/id_generator.go)。
  - 多渠道注册中心 [`IdGeneratorRegistry`](infra/persist/id_generator.go)：泛型 `Get[ID]` 恢复类型，未注册/类型不匹配返回错误。
- 子实体变更跟踪 [`TrackedList[T]`](infra/persist/tracked_list.go)：四状态（`added/modified/deleted/unchanged`），按自定义相等或 key 判定；`Attach/Add/Update/Delete/GetChanges` 输出增量。

---

## 4. 端到端示例 example/order

示例按 DDD 分层组织，展示框架全部能力如何在一个真实业务进程中协作（装配入口 [`wiring.go`](example/order/internal/order/wiring.go) 的 `NewApplication`）：

```
example/order/                       # 独立子 module（go.mod），第三方依赖仅出现在此
├── docker-compose.yml               # 本地 MySQL 8.4 + Redis 7
├── Makefile                         # up / down / reset / test / test-integration / run
├── cmd/orderserver/main.go          # 仅负责启动：建配置源 → NewApplication → 启动 HTTP
├── integration/                     # 真实依赖集成测试（仅 -tags=integration 编译）
└── internal
    ├── httpapi/                     # 入站适配器（HTTP ↔ 用例）
    └── order/
        ├── domain/                  # 订单聚合、事件、状态枚举、投影、规则、操作
        ├── app/                     # 应用用例（命令服务 / 支付服务）
        ├── port/                    # 支付预授权端口
        ├── acl/                     # 支付网关 + 先查后写适配器
        ├── messenger/               # 内存对外信使（仅单测装配使用）
        ├── infra/
        │   ├── mysql/               # GORM：写模型 / 投影 / outbox / 广播记录
        │   └── redisx/              # 支付幂等锁 + 概要缓存（cache-aside）
        ├── contracts.go             # 组合根端口（存储 / 缓存 / 幂等守卫）
        ├── options.go               # 配置绑定的类型化选项
        ├── broadcast.go             # 支付对外广播组装
        ├── instant_outbox.go        # 后端无关：提交后即时分发 outbox 装饰器
        └── wiring.go                # 组合根
```

> **模块边界**：domkit 框架本体与 `domain/app/port` 层保持**零第三方依赖**；GORM、MySQL 驱动、go-redis 只在 `example/order` 子 module 的 `infra/mysql`、`infra/redisx` 适配包内 import。根目录通过 `go.work` 同时串联框架与示例。

**已演示的业务能力**

- 订单创建（金额必须为正的领域规则）、支付、取消（含参数化规则）。
- 生命周期状态 `created / paid / cancelled` 使用类型安全的富枚举 [`OrderStatus`](example/order/internal/order/domain/status.go)。
- 真实持久化：订单写模型经 GORM 落 MySQL `orders` 表，更新按 `version` 乐观锁；单测装配换内存仓储，领域与应用代码不变。
- 写 → 读实时投影：订阅订单事件把聚合同步到 MySQL `order_summaries`；详情经 Redis 概要缓存（cache-aside，写后失效），列表分页只读读侧投影。
- 支付幂等：支付入口先经 Redis `SET NX EX` 守卫拦截时间窗口内的重复请求；业务失败主动释放，成功保留至 TTL 过期。
- 事务发件箱：领域事件与订单写在同一事务落 `outbox_messages`，提交后即时同步分发——成功置 `sent`、失败保留 `pending`、反序列化失败转 `deadletter`。
- 预授权：支付经端口 [`PaymentAuthorizer`](example/order/internal/order/port/payment.go) 与 ACL 先查后写实现，**重复调用返回同一 `preAuthId` 且 `reused:true`**。
- 对账自愈：模拟读侧条目丢失后对账检测为 STALE 并立即重建（见 `reconciliation_test.go`）。
- 出域广播：支付后订阅 `order.paid`，组装「订单已支付」信封，经 MySQL `broadcast_records` 留痕（见 `broadcast_test.go`）。

**HTTP 路由**

| 方法 | 路径 | 说明 |
|---|---|---|
| POST | `/orders` | 创建订单 |
| GET | `/orders` | 分页列表（读模型；`page`/`pageSize` 参数） |
| GET | `/orders/{id}` | 查看订单概要（Redis 缓存 → MySQL 投影，cache-aside） |
| POST | `/orders/{id}/pay` | 支付订单 |
| POST | `/orders/{id}/cancel` | 取消订单 |
| POST | `/orders/{id}/preauth` | 发起支付预授权（幂等） |
| POST | `/orders/{id}/reconcile` | 对账并自愈，返回各副本状态（HTTP 无故障注入入口，正常仅返回 CONSISTENT；STALE 自愈见 `reconciliation_test.go`） |
| GET | `/orders/{id}/broadcasts` | 查看该订单已发送的对外广播信封 |

**HTTP 响应契约**（实现见 [`response.go`](example/order/internal/httpapi/response.go)、[`dto.go`](example/order/internal/httpapi/dto.go)）

统一信封：成功为 `{"code":0,"data": ...}`，失败为 `{"code": <错误码>,"msg": "..." }`。

错误码与 HTTP 状态对应：

| code | HTTP 状态 | 触发场景 |
|---|---|---|
| 40000 | 400 | 请求 JSON 非法（含未知字段）/ 分页越界（`page<1` 或 `pageSize` 不在 [1,200]） |
| 40400 | 404 | 订单不存在（`order not found`） |
| 42200 | 422 | 领域规则不通过（多条违反时取首条文案） |
| 40900 | 409 | 乐观锁并发冲突 / 订单号已存在（唯一键冲突）/ 支付窗口内重复请求（幂等守卫） |
| 50000 | 500 | 未预期内部错误 / panic（不向外泄漏细节；诊断请看启动服务的那个终端日志） |

各路由成功时 `data` 的结构：

| 路由 | `data` 结构 |
|---|---|
| `POST /orders`（HTTP 201）、`pay`、`cancel` | 订单对象 `{"id","orderNo","amount","status","version"}` |
| `GET /orders/{id}` | 订单概要 `{"id","orderNo","status","amount","version"}`（读缓存/投影） |
| `GET /orders` | 分页体 `{"list":[订单概要],"total","page","pageSize"}`；订单概要为 `{"id","orderNo","status","amount","version"}` |
| `preauth` | `{"preAuthId","reused"}` |
| `reconcile` | 数组，元素 `{"replica","status","readVersion","writeVersion"}` |
| `broadcasts` | 数组，元素 `{"topic","senderCode","envelope": { ...信封原文... } }` |

入参与边界约定：

- 仅 `POST /orders` 需要请求体 `{"orderNo","amount"}`（`DisallowUnknownFields`，多传字段报 400）；`pay/cancel/preauth/reconcile` 均不带请求体。
- `page`/`pageSize` **缺失或填非数字时静默回退默认值**（page=1、pageSize 取配置默认 20）；但填了越界数字（如 `pageSize=0/201`、`page=0`）则返回 400。
- `GET /orders/{id}/broadcasts` 对不存在的订单也返回空数组 `[]`（而非 404）；未注册路径返回 Go 原生纯文本 `404 page not found`，不经统一信封。

**配置项**（前缀 `order`，见 [`options.go`](example/order/internal/order/options.go)）

| 键 | 含义 | 必填 |
|---|---|---|
| `order.http-addr` | HTTP 监听地址 | 是 |
| `order.default-page-size` | 列表默认分页大小（默认 20） | 否 |
| `order.mysql.dsn` | MySQL 连接串（GORM，启动时自动建表） | 是 |
| `order.redis.addr` | Redis 地址 | 是 |
| `order.redis.password` | Redis 密码（默认空） | 否 |
| `order.cache.summary-ttl` | 概要缓存 TTL，秒（默认 30） | 否 |
| `order.idempotency.pay-ttl` | 支付幂等锁 TTL，秒（默认 300） | 否 |

---

## 5. 运行与测试

### 5.1 环境依赖

| 依赖 | 版本/要求 | 用途 |
|---|---|---|
| Go | 1.27+ | 构建框架与示例（框架本体零第三方依赖） |
| 容器运行时 | 提供 `docker` CLI 与守护进程 | 经 `docker compose` 跑起 MySQL / Redis |
| MySQL | 8.4（由 compose 提供） | 写模型 / 读投影 / outbox / 广播留痕 |
| Redis | 7（由 compose 提供） | 概要缓存 / 支付幂等锁 |

> 上表的 MySQL、Redis 已由 [docker-compose.yml](example/order/docker-compose.yml) 固定版本，本机**无需**另装；你只需要准备一个能运行 Linux 容器的运行时。

### 5.2 macOS：安装容器运行时（一次性）

推荐命令行方案 [Colima](https://github.com/abiosoft/colima)（轻量、无需图形授权）。用 Homebrew 安装三件套：

```bash
brew install colima docker docker-compose
```

让 Docker CLI 找到 Homebrew 安装的 compose 插件（写入 `~/.docker/config.json`）：

```bash
mkdir -p ~/.docker
python3 - <<'PY'
import json, os
p = os.path.expanduser("~/.docker/config.json")
cfg = json.load(open(p)) if os.path.exists(p) else {}
extra = "/opt/homebrew/lib/docker/cli-plugins"
dirs = cfg.setdefault("cliPluginsExtraDirs", [])
extra not in dirs and dirs.append(extra)
json.dump(cfg, open(p, "w"), indent=2)
PY
```

启动虚拟机（参数可按需调整；首次启动会自动下载 Linux 镜像）：

```bash
colima start --cpu 2 --memory 4 --disk 30
docker info >/dev/null && echo "docker ready"
```

> 备选：也可用 **Docker Desktop**（`brew install --cask docker`，安装后手动打开 App 完成授权）。二者都提供标准 `docker` / `docker compose`，后续步骤完全一致。

### 5.3 国内网络：配置镜像加速

直连 Docker Hub 常被拒绝（`registry-1.docker.io ... connection refused`）。在 Colima 虚拟机内写入镜像加速器并重启：

```bash
colima ssh -- sudo sh -c 'cat > /etc/docker/daemon.json <<EOF
{
  "registry-mirrors": [
    "https://docker.m.daocloud.io",
    "https://docker.1ms.run",
    "https://docker.xuanyuan.me",
    "https://dockerproxy.net"
  ]
}
EOF'
colima ssh -- sudo systemctl restart docker
docker info | grep -A4 "Registry Mirrors"   # 确认已生效
```

公共加速器可能随时失效，按需增删；该配置写入虚拟机磁盘，重启 Colima 后仍然保留。

> ⚠️ **请务必在 `make up` 之前执行本节**：`systemctl restart docker` 会重启守护进程，把当时正在运行的容器一并停止。若你已经执行过 `make up`，重启后请回到 `example/order` 重新 `make up`（或 `docker compose start`）拉起 MySQL / Redis。

### 5.4 启动依赖与服务

```bash
cd example/order
make up          # 拉起 MySQL :3306 + Redis :6379（首次会拉取镜像）
make ps          # 等两个服务均为 healthy
make run         # 等价 go run ./cmd/orderserver；首次启动自动建表
# order server listening on :8080
```

### 5.5 构建与测试

框架本体（仓库根目录，零第三方依赖，含竞态检测）：

```bash
go build ./...
go vet ./...
go test -race ./...
```

示例为独立子 module，在 `example/order` 目录下执行：

```bash
make test               # 纯单元测试：全内存装配，不依赖容器
make test-integration   # GOWORK=off + -tags=integration：真实 MySQL + Redis 端到端
```

推荐流程：

```bash
cd example/order
make up
make ps
make test-integration
make reset              # 或 make down，按需保留数据卷
```

> 集成测试启动前会探测 MySQL/Redis，探测不到则该批用例 `SKIP`（而非失败）；可用环境变量 `ORDER_TEST_MYSQL_DSN`、`ORDER_TEST_REDIS_ADDR` 指向远端实例。`make test-integration` 固定使用 `GOWORK=off`，确保示例作为独立 Go module 也能通过。

测试覆盖：框架各主要包配备单元测试——领域模型与规则、命令执行器（含失败与事务路径）、配置转换/绑定/开关、出域广播各阶段错误分类、读副本与分页、对账状态与自愈、ACL 错误分类与幂等、事件总线优先级/异步重试死信、Outbox 认领与中继、内存仓储乐观锁与发号器等。示例纯单测经全内存装配验证对账自愈、支付广播、取消规则；`integration/` 另验证缓存回填与写后失效、重复支付 409、outbox 状态流转、广播落库。

---

## 6. 当前实现范围说明

- `example/order` 已示范把端口接到真实 MySQL（GORM 写模型 / 投影 / outbox / 广播留痕）与 Redis（概要缓存 / 支付幂等锁），而框架本体与领域内核保持零第三方依赖；同一装配内核换内存后端即可跑纯单元测试。
- 仓储、读副本、事件总线、Outbox 存储均提供**进程内内存参考实现**，可直接用于测试与无中间件环境；`Repository / ReadModelReplica / EventBus / OutboxStore / SegmentStore / Messenger / Serializer` 等均为可替换端口，接入真实数据库或消息中间件时在组合根替换实现即可，领域与应用层代码不变。
- `infra/persist` 中的数据库号段能力当前以端口 `SegmentStore` + 适配器 `StoreSegmentAllocator` 形式提供接缝；`config` 当前内置 `MapSource` 一种源。这些接缝是已实现的端口契约，具体外部介质实现由使用方按需提供。
