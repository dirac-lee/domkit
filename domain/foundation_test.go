package domain

import (
	"errors"
	"testing"
)

// ============ Entity 子实体基类 ============

// MarkCreated/MarkModified 应正确填充审计时间。
func TestEntityMarkTimestamps(t *testing.T) {
	e := Entity[string]{}
	e.MarkCreated()
	if e.CreatedAt.IsZero() || e.UpdatedAt.IsZero() {
		t.Fatal("MarkCreated 后创建/修改时间均应非零")
	}
	e.MarkModified()
	if e.UpdatedAt.IsZero() {
		t.Fatal("MarkModified 后修改时间应非零")
	}
}

// 基于身份标识的等同性判定（表驱动覆盖：相等/不同/零值）。
func TestEntitySameIdentityAs(t *testing.T) {
	cases := []struct {
		name string
		a, b Entity[string]
		want bool
	}{
		{"相同非零ID", Entity[string]{ID: "1"}, Entity[string]{ID: "1"}, true},
		{"不同ID", Entity[string]{ID: "1"}, Entity[string]{ID: "2"}, false},
		{"一方为零值", Entity[string]{ID: "1"}, Entity[string]{}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := c.a.SameIdentityAs(&c.b); got != c.want {
				t.Fatalf("SameIdentityAs = %v，期望 %v", got, c.want)
			}
		})
	}
}

// ============ EnumValue 枚举规约 ============

// color 演示用 string 编码富枚举：实现 EnumValue[string]。
type color struct {
	code string
	name string
}

func (c color) Value() string { return c.code }
func (c color) Name() string  { return c.name }

// colorCatalog 由全部颜色成员构造的枚举目录。
var colorCatalog = NewEnumCatalog(
	color{code: "R", name: "红色"},
	color{code: "G", name: "绿色"},
)

func TestEnumCatalogByCode(t *testing.T) {
	c, ok := colorCatalog.ByCode("R")
	if !ok || c.Name() != "红色" {
		t.Fatalf("ByCode(R) = %v,%v，期望 红色,true", c, ok)
	}
	if _, ok := colorCatalog.ByCode("X"); ok {
		t.Fatal("未注册编码应返回 false")
	}
}

// Resolve 未命中应返回可被 errors.Is 识别的 ErrEnumCodeNotFound。
func TestEnumCatalogResolveError(t *testing.T) {
	_, err := colorCatalog.Resolve("X")
	if !errors.Is(err, ErrEnumCodeNotFound) {
		t.Fatalf("Resolve 未命中错误 = %v，期望 ErrEnumCodeNotFound", err)
	}
}

func TestEnumCatalogAll(t *testing.T) {
	if len(colorCatalog.All()) != 2 {
		t.Fatal("All 应返回全部 2 个成员")
	}
}

// ============ Operation 操作体系 ============

// 新注册表应预注册内置 NEW/DELETE。
func TestOperationRegistryBuiltins(t *testing.T) {
	r := NewOperationRegistry()
	if !r.Contains("NEW") || !r.Contains("DELETE") {
		t.Fatal("新注册表应预注册 NEW/DELETE")
	}
}

// TriggeredOperations 的记录校验与包含性判断。
func TestTriggeredOperations(t *testing.T) {
	r := NewOperationRegistry()
	ship := NewEntityOperation("SHIP", "发货")
	r.Register(ship)

	triggered := NewTriggeredOperations(r)
	// 已注册操作可正常记录。
	if err := triggered.Record(ship); err != nil {
		t.Fatalf("Record 已注册操作失败: %v", err)
	}
	// 未注册操作必须被拒绝。
	if err := triggered.Record(NewEntityOperation("BOGUS", "")); !errors.Is(err, ErrOperationNotRegistered) {
		t.Fatalf("Record 未注册操作错误 = %v，期望 ErrOperationNotRegistered", err)
	}

	if !triggered.Contains(ship) {
		t.Fatal("Contains 应能查到已记录操作")
	}
	if !triggered.ContainsAny(OperationNew, ship) {
		t.Fatal("ContainsAny：NEW/SHIP 中应至少命中 SHIP")
	}
	if triggered.ContainsAll(OperationNew, ship) {
		t.Fatal("ContainsAll：NEW 尚未记录，应为 false")
	}
	if err := triggered.Record(OperationNew); err != nil {
		t.Fatalf("Record NEW 失败: %v", err)
	}
	if !triggered.ContainsAll(OperationNew, ship) {
		t.Fatal("ContainsAll：NEW 与 SHIP 均已记录，应为 true")
	}

	triggered.Clear()
	if triggered.Contains(ship) {
		t.Fatal("Clear 后不应再含已记录操作")
	}
}
