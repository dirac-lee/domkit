package domain

import "testing"

// seededTrackedMap 构造含两个基线条目的追踪 Map，供多个用例复用，避免重复搭建。
func seededTrackedMap() *TrackedMap[string, int] {
	return NewTrackedMapFrom(map[string]int{"a": 1, "b": 2})
}

// 空基线放入：应只进 Inserted。
func TestTrackedMapInsert(t *testing.T) {
	m := NewTrackedMap[string, int]()
	m.Put("x", 9)

	inserted := m.Inserted()
	if len(inserted) != 1 || inserted["x"] != 9 {
		t.Fatalf("Inserted = %v，期望 {x:9}", inserted)
	}
	if len(m.Updated()) != 0 || len(m.Removed()) != 0 {
		t.Fatal("新增键不应出现在 Updated/Removed")
	}
}

// 替换基线键：应只进 Updated（同一行原地更新）。
func TestTrackedMapUpdate(t *testing.T) {
	m := seededTrackedMap()
	m.Put("a", 10)

	updated := m.Updated()
	if len(updated) != 1 || updated["a"] != 10 {
		t.Fatalf("Updated = %v，期望 {a:10}", updated)
	}
	if len(m.Inserted()) != 0 {
		t.Fatal("基线键替换不应算作 Inserted")
	}
}

// 删除基线键：应进 Removed 并附带原值。
func TestTrackedMapRemoveBaseline(t *testing.T) {
	m := seededTrackedMap()
	m.Remove("a")

	removed := m.Removed()
	if len(removed) != 1 || removed["a"] != 1 {
		t.Fatalf("Removed = %v，期望 {a:1}", removed)
	}
}

// 撤销「刚写入、尚未持久化」的键：三桶应全空。
func TestTrackedMapRemoveFreshPut(t *testing.T) {
	m := NewTrackedMap[string, int]()
	m.Put("x", 9)
	m.Remove("x")

	if len(m.Inserted()) != 0 || len(m.Removed()) != 0 {
		t.Fatal("撤销刚写入的键后三桶应均为空")
	}
}

// 先删基线键、再加回：净算重新插入（进 Inserted），不再发 DELETE。
func TestTrackedMapRemoveThenPutReinserts(t *testing.T) {
	m := seededTrackedMap()
	m.Remove("a")
	m.Put("a", 11)

	inserted := m.Inserted()
	if len(inserted) != 1 || inserted["a"] != 11 {
		t.Fatalf("删后再加 Inserted = %v，期望 {a:11}", inserted)
	}
	if len(m.Removed()) != 0 || len(m.Updated()) != 0 {
		t.Fatal("删后再加净算插入，不应出现在 Removed/Updated")
	}
}

// 清空：全部基线键进 Removed。
func TestTrackedMapClear(t *testing.T) {
	m := seededTrackedMap()
	m.Clear()

	if len(m.Removed()) != 2 {
		t.Fatalf("Clear 后 Removed = %v，期望含全部 2 个基线键", m.Removed())
	}
}

// 逻辑视图：删除基线 a、新增 c，最终应为 {b, c}。
func TestTrackedMapAll(t *testing.T) {
	m := seededTrackedMap()
	m.Remove("a")
	m.Put("c", 3)

	all := m.All()
	want := map[string]int{"b": 2, "c": 3}
	if len(all) != len(want) {
		t.Fatalf("All = %v，期望 %v", all, want)
	}
	for k, v := range want {
		if all[k] != v {
			t.Fatalf("All[%s] = %d，期望 %d", k, all[k], v)
		}
	}
}
