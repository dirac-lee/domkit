package config

import (
	"errors"
	"testing"
	"time"
)

// testColor 用于验证自定义类型经 TextUnmarshaler 转换（指针接收）。
type testColor string

func (c *testColor) UnmarshalText(text []byte) error {
	if len(text) == 0 {
		return errors.New("空颜色")
	}
	*c = testColor("color:" + string(text))
	return nil
}

// TestConvert_Builtins 逐类型直接调用 Convert，验证内建类型的零反射转换。
func TestConvert_Builtins(t *testing.T) {
	if v, _ := Convert[string]("abc"); v != "abc" {
		t.Fatalf("string 得到 %v", v)
	}
	if v, _ := Convert[bool]("true"); v != true {
		t.Fatalf("bool 得到 %v", v)
	}
	if v, _ := Convert[int]("-12"); v != -12 {
		t.Fatalf("int 得到 %v", v)
	}
	if v, _ := Convert[int64]("9000000000"); v != 9000000000 {
		t.Fatalf("int64 得到 %v", v)
	}
	if v, _ := Convert[uint]("42"); v != 42 {
		t.Fatalf("uint 得到 %v", v)
	}
	if v, _ := Convert[float64]("1.5"); v != 1.5 {
		t.Fatalf("float64 得到 %v", v)
	}
	if v, _ := Convert[time.Duration]("5s"); v != 5*time.Second {
		t.Fatalf("duration 得到 %v", v)
	}
}

// TestConvert_InvalidValues 非法输入与溢出均应返回错误。
func TestConvert_InvalidValues(t *testing.T) {
	if _, err := Convert[int]("not-a-number"); err == nil {
		t.Fatal("int 非法值应报错")
	}
	if _, err := Convert[bool]("yes"); err == nil {
		t.Fatal("bool 非法值应报错")
	}
	if _, err := Convert[time.Duration]("5minute"); err == nil {
		t.Fatal("duration 非法值应报错")
	}
	// int8 位宽 8，128 溢出。
	if _, err := Convert[int8]("128"); err == nil {
		t.Fatal("int8 溢出应报错")
	}
	if _, err := Convert[float32]("1.2.3"); err == nil {
		t.Fatal("float 非法值应报错")
	}
}

// TestConvert_TextUnmarshaler 验证值/指针两种形态的自定义类型转换。
func TestConvert_TextUnmarshaler(t *testing.T) {
	color, err := Convert[testColor]("red")
	if err != nil {
		t.Fatalf("值形态转换失败: %v", err)
	}
	if color != "color:red" {
		t.Fatalf("值形态得到 %q", color)
	}

	p, err := Convert[*testColor]("blue")
	if err != nil || p == nil || *p != "color:blue" {
		t.Fatalf("指针形态转换异常: %v %v", p, err)
	}
}

// TestConvert_UnsupportedType 未实现 TextUnmarshaler 的 struct 应归类为 ErrUnsupportedType。
func TestConvert_UnsupportedType(t *testing.T) {
	_, err := Convert[struct{ Name string }]("x")
	if !errors.Is(err, ErrUnsupportedType) {
		t.Fatalf("期望 ErrUnsupportedType，得到 %v", err)
	}
}
