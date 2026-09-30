package config

import (
	"sync"
	"testing"
)

// TestMapSource 验证写入/读取、存在性与按键升序。
func TestMapSource(t *testing.T) {
	src := NewMapSource().Set("b", "2").Set("a", "1")

	if v, ok := src.Lookup("a"); !ok || v != "1" {
		t.Fatalf("Lookup(a) = %q,%v", v, ok)
	}
	if _, ok := src.Lookup("missing"); ok {
		t.Fatal("不存在的键 ok 应为 false")
	}

	keys := src.Keys()
	if len(keys) != 2 || keys[0] != "a" || keys[1] != "b" {
		t.Fatalf("Keys 应升序，得到 %v", keys)
	}
}

// TestMapSource_InitialCopied 初始 map 应被独立拷贝：外部修改不影响配置源。
func TestMapSource_InitialCopied(t *testing.T) {
	initial := map[string]string{"k": "v"}
	src := NewMapSourceFrom(initial)
	initial["k"] = "changed"

	if v, _ := src.Lookup("k"); v != "v" {
		t.Fatalf("初始条目应已拷贝隔离，得到 %q", v)
	}
}

// TestMapSource_Concurrent 并发读写不产生数据竞争（配合 -race 验证）。
func TestMapSource_Concurrent(t *testing.T) {
	src := NewMapSource()
	var wg sync.WaitGroup

	for i := 0; i < 50; i++ {
		wg.Add(2)
		go func(i int) {
			defer wg.Done()
			src.Set("k", "v")
		}(i)
		go func() {
			defer wg.Done()
			_, _ = src.Lookup("k")
			_ = src.Keys()
		}()
	}
	wg.Wait()
}
