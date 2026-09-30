package persist

import "testing"

type lineItem struct {
	ID   int
	Name string
}

func newKeyedList() *TrackedList[lineItem] {
	return NewTrackedListByKey[lineItem](func(i lineItem) int { return i.ID })
}

func TestTrackedListAttachAddUpdateDelete(t *testing.T) {
	tl := newKeyedList()
	tl.Attach(lineItem{ID: 1, Name: "a"}) // 既有项
	tl.Add(lineItem{ID: 2, Name: "b"})    // 新增项
	tl.Update(lineItem{ID: 1, Name: "a2"})

	changes := tl.GetChanges()
	if len(changes) != 2 {
		t.Fatalf("expected 2 changes, got %d", len(changes))
	}
	states := map[int]TrackState{}
	for _, c := range changes {
		states[c.Item.ID] = c.State
	}
	if states[1] != TrackModified || states[2] != TrackAdded {
		t.Fatalf("unexpected states: %v", states)
	}
	if got := tl.items[0].Item.Name; got != "a2" {
		t.Fatalf("updated item content not replaced, got %q", got)
	}

	// 删除尚未落库的新增项：直接移出集合，不再出现在变更里。
	tl.Delete(lineItem{ID: 2})
	changes = tl.GetChanges()
	if len(changes) != 1 || changes[0].Item.ID != 1 {
		t.Fatalf("expected only modified id=1 after deleting added item, got %+v", changes)
	}

	// 删除既有项：标记 deleted。
	tl.Delete(lineItem{ID: 1})
	changes = tl.GetChanges()
	if len(changes) != 1 || changes[0].State != TrackDeleted {
		t.Fatalf("expected deleted change, got %+v", changes)
	}
}

func TestTrackedListUpdateAddedItemKeepsAddedState(t *testing.T) {
	tl := newKeyedList()
	tl.Add(lineItem{ID: 1, Name: "v1"})
	tl.Update(lineItem{ID: 1, Name: "v2"})

	changes := tl.GetChanges()
	if len(changes) != 1 || changes[0].State != TrackAdded || changes[0].Item.Name != "v2" {
		t.Fatalf("expected single added change with new content, got %+v", changes)
	}
}

func TestTrackedListUnknownIdentityIgnored(t *testing.T) {
	tl := newKeyedList()
	tl.Attach(lineItem{ID: 1})
	tl.Update(lineItem{ID: 99}) // 未命中：不应误伤 id=1
	tl.Delete(lineItem{ID: 99})

	for _, it := range tl.items {
		if it.State != TrackUnchanged {
			t.Fatalf("unmatched identity must not change state, got %v for id=%d", it.State, it.Item.ID)
		}
	}
	if len(tl.GetChanges()) != 0 {
		t.Fatal("expected no changes")
	}
}

func TestTrackedListCustomEqual(t *testing.T) {
	tl := NewTrackedList[lineItem](func(a, b lineItem) bool { return a.Name == b.Name })
	tl.Attach(lineItem{ID: 1, Name: "same"})
	tl.Update(lineItem{ID: 2, Name: "same"}) // 按 Name 判等，命中 id=1 的项

	changes := tl.GetChanges()
	if len(changes) != 1 || changes[0].State != TrackModified {
		t.Fatalf("expected match by custom equal, got %+v", changes)
	}
}
