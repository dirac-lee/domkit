package domain

import (
	"fmt"
	"time"
)

// AggregateRoot 聚合根基类，业务聚合嵌入该结构体。
// 类型参数 ID 为聚合主键类型（string 系强类型 ID、int64、uint64 等任意可比较类型），
// 由嵌入方在声明时固定，例如：
//
//	type OrderID string
//	type Order struct { domain.AggregateRoot[OrderID] }
//	type Counter struct { domain.AggregateRoot[int64] }
//
// 集中维护聚合不变性所需能力：主键、规则违反收集、乐观锁版本、领域事件收集。
//
// 重要约定：乐观锁版本不会在持久化时自动 +1，必须由领域行为调用 NextVersion
// 主动推进；漏调会让并发更新的乐观锁静默失效（编译器与运行期均不报错），详见 NextVersion 文档。
type AggregateRoot[ID comparable] struct {
	ID        ID
	Version   uint64 // 最近一次已持久化版本（乐观锁基线，新建聚合为 0）
	CreatedAt time.Time
	UpdatedAt time.Time
	Deleted   bool // 软删除标记

	events []DomainEvent // 领域事件集合，聚合内部收集

	// ---- 成因操作 ----
	// operationRegistry 校验「允许触发」的操作；currentOperation 记录本次业务
	// 操作内最近记录的操作码，CollectEvent 时回填到事件。
	operationRegistry *OperationRegistry
	currentOperation  string

	// ---- 规则违反收集器 ----
	ruleRegistry *BrokenRuleRegistry
	brokenRules  []BrokenRule

	// ---- 幂等版本 ----
	// 一次业务操作内 NextVersion 只递增一次：首次调用基于基线算出 pendingVersion，
	// 之后重复调用返回同一值；CommitVersion 在持久化成功后把基线推进到 pendingVersion。
	versionGenerated bool
	pendingVersion   uint64
}

// 主键发放不在聚合根基类内固化：业务主键通过注入的 IDGenerator 端口获得
//（UUID/号段/雪花等策略由基础设施决定），见 domain/id.go。

// MarkCreated 标记聚合已创建，并初始化创建/更新时间。
// 聚合工厂通常在完成初始状态赋值后调用一次。
func (ar *AggregateRoot[ID]) MarkCreated() {
	now := time.Now()
	ar.CreatedAt = now
	ar.UpdatedAt = now
}

// MarkModified 标记聚合已修改，只刷新更新时间。
// 会改变持久化状态的领域行为应在收集事件前调用。
func (ar *AggregateRoot[ID]) MarkModified() {
	ar.UpdatedAt = time.Now()
}

// BeginCreate 开始一次新建变更：初始化审计时间并返回本次变更版本。
// 聚合工厂可用返回值作为创建事件的聚合版本。
func (ar *AggregateRoot[ID]) BeginCreate() uint64 {
	ar.MarkCreated()
	return ar.NextVersion()
}

// BeginModify 开始一次修改变更：刷新审计时间并返回本次变更版本。
// 领域行为可用返回值作为修改事件的聚合版本。
func (ar *AggregateRoot[ID]) BeginModify() uint64 {
	ar.MarkModified()
	return ar.NextVersion()
}

// ============ 规则注册表 ============

// SetRuleRegistry 注入规则消息注册表，通常在聚合工厂方法中调用一次。
func (ar *AggregateRoot[ID]) SetRuleRegistry(registry *BrokenRuleRegistry) {
	ar.ruleRegistry = registry
}

// ============ 规则违反收集 ============

// AddBrokenRule 追加一条规则违反，描述从注册表解析，未注册时回退到消息码自带描述。
func (ar *AggregateRoot[ID]) AddBrokenRule(code MessageCode) {
	ar.brokenRules = append(ar.brokenRules, BrokenRule{
		Code:    code.Code,
		Message: ar.resolveDescription(code),
	})
}

// AddBrokenRuleWithParams 追加一条参数化规则违反，params 非空时按 fmt.Sprintf 渲染模板。
func (ar *AggregateRoot[ID]) AddBrokenRuleWithParams(code MessageCode, params ...any) {
	message := ar.resolveDescription(code)
	if len(params) > 0 {
		message = fmt.Sprintf(message, params...)
	}
	ar.brokenRules = append(ar.brokenRules, BrokenRule{
		Code:    code.Code,
		Message: message,
		Params:  params,
	})
}

// CheckRule 执行业务规则：通过返回 true；不通过时把规则违反收集进聚合并返回 false。
// rule 为 nil 时视为通过。
func (ar *AggregateRoot[ID]) CheckRule(rule BusinessRule) bool {
	if rule == nil {
		return true
	}
	if rule.IsSatisfied() {
		return true
	}
	if br := rule.BrokenRule(); br != nil {
		ar.brokenRules = append(ar.brokenRules, *br)
	}
	return false
}

// BrokenRules 返回已收集规则违反的副本。
func (ar *AggregateRoot[ID]) BrokenRules() []BrokenRule {
	out := make([]BrokenRule, len(ar.brokenRules))
	copy(out, ar.brokenRules)
	return out
}

// HasBrokenRules 判断是否存在已收集的规则违反。
func (ar *AggregateRoot[ID]) HasBrokenRules() bool {
	return len(ar.brokenRules) > 0
}

// ClearBrokenRules 清空已收集的规则违反。
func (ar *AggregateRoot[ID]) ClearBrokenRules() {
	ar.brokenRules = nil
}

// FirstRuleError 返回首条违反对应的单条规则错误，无违反时返回 nil。
func (ar *AggregateRoot[ID]) FirstRuleError() error {
	if !ar.HasBrokenRules() {
		return nil
	}
	return NewSingleDomainRuleError(ar.brokenRules[0])
}

// AggregateRuleError 返回包含全部违反的聚合规则错误，无违反时返回 nil。
func (ar *AggregateRoot[ID]) AggregateRuleError() error {
	if !ar.HasBrokenRules() {
		return nil
	}
	return NewDomainRuleError(ar.BrokenRules())
}

func (ar *AggregateRoot[ID]) resolveDescription(code MessageCode) string {
	if ar.ruleRegistry != nil {
		if c, ok := ar.ruleRegistry.Lookup(code.Code); ok {
			return c.Description
		}
	}
	return code.Description
}

// ============ 版本控制 ============

// IsNew 判断聚合是否尚未持久化（基线版本为 0）。
func (ar *AggregateRoot[ID]) IsNew() bool {
	return ar.Version == 0
}

// CurrentVersion 返回最近一次已持久化的基线版本。
func (ar *AggregateRoot[ID]) CurrentVersion() uint64 {
	return ar.Version
}

// NextVersion 返回本次业务操作的新版本号（幂等）：
// 首次调用在基线版本上 +1，同一操作内重复调用返回相同值，基线不被提前推进。
//
// 用法约定（务必遵守）：任何会修改聚合、且随后要被持久化（Insert/Update）的
// 领域行为，都必须主动调用本方法，或使用 BeginCreate/BeginModify 这类组合入口；
// 执行器与工作单元都不会代为 +1。
// 若漏调，聚合版本将一直停在基线：新建行版本恒为 0，仓储更新时的乐观锁比较
// 会出现「旧基线 == 旧基线」恒成立，丢失更新无法被检测——且编译器与运行期都不会报错。
func (ar *AggregateRoot[ID]) NextVersion() uint64 {
	if !ar.versionGenerated {
		ar.pendingVersion = ar.Version + 1
		ar.versionGenerated = true
	}
	return ar.pendingVersion
}

// PendingVersion 返回已生成的待提交版本；本操作尚未生成版本时返回基线版本。
func (ar *AggregateRoot[ID]) PendingVersion() uint64 {
	if ar.versionGenerated {
		return ar.pendingVersion
	}
	return ar.Version
}

// CommitVersion 在聚合持久化成功后把基线推进到待提交版本。
// 由工作单元在提交阶段调用；业务代码一般不需要直接调用。
func (ar *AggregateRoot[ID]) CommitVersion() {
	if ar.versionGenerated {
		ar.Version = ar.pendingVersion
		ar.versionGenerated = false
	}
}

// MarkPersisted 供仓储在把数据库行装配回聚合时调用：
// 以行版本重建基线，并清除「待提交版本」瞬态（该状态不入库）。
func (ar *AggregateRoot[ID]) MarkPersisted(version uint64) {
	ar.Version = version
	ar.versionGenerated = false
	ar.pendingVersion = 0
}

// ============ 成因操作 ============

// SetOperationRegistry 注入操作注册表，通常在聚合工厂方法中调用一次。
func (ar *AggregateRoot[ID]) SetOperationRegistry(registry *OperationRegistry) {
	ar.operationRegistry = registry
}

// RecordOperation 记录当前业务操作：操作必须先在注册表声明，否则返回
// ErrOperationNotRegistered（提前返回，不更新当前操作）；校验通过后把它记为
// 「当前操作」，供随后 CollectEvent 回填到事件。
func (ar *AggregateRoot[ID]) RecordOperation(op EntityOperation) error {
	if ar.operationRegistry == nil || !ar.operationRegistry.Contains(op.Code()) {
		return fmt.Errorf("%w: %s", ErrOperationNotRegistered, op.Code())
	}
	ar.currentOperation = op.Code()
	return nil
}

// operationCodeFiller 事件操作码回填接缝：仅框架包内可断言，业务事件经指针
// 嵌入 BaseDomainEvent 自动满足，因此业务方无需也无法自行实现。
type operationCodeFiller interface {
	setOperationCode(code string)
}

// ============ 领域事件 ============

// CollectEvent 聚合内部收集领域事件，并把「当前操作」编码回填到事件；
// 值形态事件（无指针回填接缝）不回填，操作码保持为空，其余流程不受影响。
func (ar *AggregateRoot[ID]) CollectEvent(evt DomainEvent) {
	if filler, ok := evt.(operationCodeFiller); ok {
		filler.setOperationCode(ar.currentOperation)
	}
	ar.events = append(ar.events, evt)
}

// PullEvents 取出所有事件，取出后清空事件列表（UoW 调用）。
func (ar *AggregateRoot[ID]) PullEvents() []DomainEvent {
	evts := ar.events
	ar.events = nil
	return evts
}
