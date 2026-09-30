package domain

import "errors"

// EnumValue 富枚举值规约：业务编码 Value（持久化/传输用）+ 展示名 Name（下拉/日志/可视化用）。
// CODE 为业务编码类型（string/int 等可比较类型），由具体枚举在声明时固定。
type EnumValue[CODE comparable] interface {
	Value() CODE
	Name() string
}

// DescribedEnum 可选能力：枚举值除展示名外还附带详细描述。不强制每个枚举实现。
type DescribedEnum interface {
	Desc() string
}

// ErrEnumCodeNotFound 枚举目录中查无此业务编码。
var ErrEnumCodeNotFound = errors.New("domain: enum code not found")

// EnumCatalog 枚举目录：承载某一枚举类型的全部成员，按业务编码建立索引。
// 目录在构造后不可变，其读方法可被并发调用。
type EnumCatalog[E EnumValue[CODE], CODE comparable] struct {
	members []E
	byCode  map[CODE]E
}

// NewEnumCatalog 由全部枚举成员构造目录（重复编码以后者覆盖前者）。
func NewEnumCatalog[E EnumValue[CODE], CODE comparable](members ...E) EnumCatalog[E, CODE] {
	catalog := EnumCatalog[E, CODE]{
		members: members,
		byCode:  make(map[CODE]E, len(members)),
	}
	for _, m := range members {
		catalog.byCode[m.Value()] = m
	}
	return catalog
}

// ByCode 按业务编码查找枚举值；未命中返回零值与 false。
func (c EnumCatalog[E, CODE]) ByCode(code CODE) (E, bool) {
	m, ok := c.byCode[code]
	return m, ok
}

// Resolve 按编码查找；未命中返回包装了 ErrEnumCodeNotFound 的错误，便于向上游传播。
func (c EnumCatalog[E, CODE]) Resolve(code CODE) (E, error) {
	if m, ok := c.byCode[code]; ok {
		return m, nil
	}
	var zero E
	return zero, ErrEnumCodeNotFound
}

// All 返回全部枚举成员。
func (c EnumCatalog[E, CODE]) All() []E { return c.members }
