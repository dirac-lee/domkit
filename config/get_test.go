package config

import (
	"errors"
	"testing"
)

// TestGet 验证命中、缺失（ErrKeyNotFound）、非法值三种情形。
func TestGet(t *testing.T) {
	src := NewMapSourceFrom(map[string]string{"size": "20", "bad-size": "abc"})

	v, err := Get[int](src, "size")
	if err != nil || v != 20 {
		t.Fatalf("命中场景异常: v=%d err=%v", v, err)
	}
	if _, err := Get[int](src, "missing"); !errors.Is(err, ErrKeyNotFound) {
		t.Fatalf("缺失应返回 ErrKeyNotFound，得到 %v", err)
	}
	if _, err := Get[int](src, "bad-size"); err == nil {
		t.Fatal("非法值应报错")
	}
}

// TestGetOr 缺失或转换失败都回退默认值；命中返回配置值。
func TestGetOr(t *testing.T) {
	src := NewMapSourceFrom(map[string]string{"size": "20", "bad-size": "abc"})

	if v := GetOr(src, "size", 10); v != 20 {
		t.Fatalf("命中应取配置值，得到 %d", v)
	}
	if v := GetOr(src, "missing", 10); v != 10 {
		t.Fatalf("缺失应取默认值，得到 %d", v)
	}
	if v := GetOr(src, "bad-size", 10); v != 10 {
		t.Fatalf("非法值应取默认值，得到 %d", v)
	}
}
