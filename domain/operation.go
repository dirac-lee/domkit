package domain

import (
	"errors"
	"fmt"
)

// 内置操作：任何启用操作体系的实体，都天然具备「新建 / 删除」两个操作。
var (
	OperationNew    = NewEntityOperation("NEW", "新建")
	OperationDelete = NewEntityOperation("DELETE", "删除")
)

// ErrOperationNotRegistered 触发的操作未在注册表中声明。
var ErrOperationNotRegistered = errors.New("domain: operation not registered")

// EntityOperation 实体业务操作描述符（不可变值对象，刻意设计为非枚举）。
// code 是操作的唯一标识，description 用于审计与展示；以 code 决定相等性。
type EntityOperation struct {
	code        string
	description string
}

// NewEntityOperation 创建操作描述符。
func NewEntityOperation(code, description string) EntityOperation {
	return EntityOperation{code: code, description: description}
}

func (o EntityOperation) Code() string        { return o.code }
func (o EntityOperation) Description() string { return o.description }

// OperationRegistry 操作注册表：集中声明某类实体「允许触发」的全部操作码。
// Go 版采用显式注册（不像 Java 用反射扫描静态字段）：
// 未声明即不可触发，注册关系在源码中一目了然，也不存在反射静默失败。
type OperationRegistry struct {
	ops map[string]EntityOperation
}

// NewOperationRegistry 创建注册表，并预注册内置的 NEW / DELETE。
func NewOperationRegistry() *OperationRegistry {
	r := &OperationRegistry{ops: map[string]EntityOperation{}}
	r.Register(OperationNew, OperationDelete)
	return r
}

// Register 追加注册一组操作，以 code 为键（重复注册以后者覆盖）。
func (r *OperationRegistry) Register(ops ...EntityOperation) {
	for _, o := range ops {
		r.ops[o.code] = o
	}
}

// Contains 判断操作码是否已注册。
func (r *OperationRegistry) Contains(code string) bool {
	_, ok := r.ops[code]
	return ok
}

// TriggeredOperations 一次业务操作内「已触发操作」的收集器：
// 记录前先校验操作确已在注册表声明，并提供各类包含性判断。
type TriggeredOperations struct {
	registry  *OperationRegistry
	triggered map[string]EntityOperation
}

// NewTriggeredOperations 绑定一个操作注册表，创建收集器。
func NewTriggeredOperations(registry *OperationRegistry) *TriggeredOperations {
	return &TriggeredOperations{registry: registry, triggered: map[string]EntityOperation{}}
}

// Record 记录一个已触发操作；操作未注册时返回 ErrOperationNotRegistered（提前返回，不入表）。
func (t *TriggeredOperations) Record(op EntityOperation) error {
	if !t.registry.Contains(op.code) {
		return fmt.Errorf("%w: %s", ErrOperationNotRegistered, op.code)
	}
	t.triggered[op.code] = op
	return nil
}

// Contains 判断指定操作是否已触发。
func (t *TriggeredOperations) Contains(op EntityOperation) bool {
	_, ok := t.triggered[op.code]
	return ok
}

// ContainsAny 给定操作中是否「至少触发了一个」。
func (t *TriggeredOperations) ContainsAny(ops ...EntityOperation) bool {
	for _, op := range ops {
		if t.Contains(op) {
			return true
		}
	}
	return false
}

// ContainsAll 给定操作是否「全部都已触发」。
func (t *TriggeredOperations) ContainsAll(ops ...EntityOperation) bool {
	for _, op := range ops {
		if !t.Contains(op) {
			return false
		}
	}
	return true
}

// Clear 清空已收集的操作。
func (t *TriggeredOperations) Clear() { t.triggered = map[string]EntityOperation{} }
