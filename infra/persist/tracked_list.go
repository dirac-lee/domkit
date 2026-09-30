package persist

// TrackState 子实体跟踪状态
type TrackState string

const (
	TrackAdded     TrackState = "added"
	TrackModified  TrackState = "modified"
	TrackDeleted   TrackState = "deleted"
	TrackUnchanged TrackState = "unchanged"
)

// TrackedItem 跟踪条目
type TrackedItem[T any] struct {
	State TrackState
	Item  T
}

// TrackedList 子实体集合变更跟踪，仓储据此做增量持久化，避免全删全建。
//
// 元素身份通过构造时提供的相等性判定识别：
//   - 优先使用 NewTrackedListByKey 以主键/唯一键构造；
//   - 无合适键时使用 NewTrackedList 提供自定义相等判定。
type TrackedList[T any] struct {
	equal func(a, b T) bool
	items []*TrackedItem[T]
}

// NewTrackedList 以自定义相等判定构造跟踪集合，equal 不得为 nil。
func NewTrackedList[T any](equal func(a, b T) bool) *TrackedList[T] {
	return &TrackedList[T]{equal: equal}
}

// NewTrackedListByKey 以键提取函数构造跟踪集合，键类型 K 必须可比较。
func NewTrackedListByKey[T any, K comparable](keyOf func(item T) K) *TrackedList[T] {
	return &TrackedList[T]{
		equal: func(a, b T) bool { return keyOf(a) == keyOf(b) },
	}
}

// Attach 挂载从数据库装载的既有子实体，初始状态为 unchanged。
func (tl *TrackedList[T]) Attach(item T) {
	tl.items = append(tl.items, &TrackedItem[T]{State: TrackUnchanged, Item: item})
}

// Add 登记一个新增子实体。
func (tl *TrackedList[T]) Add(item T) {
	tl.items = append(tl.items, &TrackedItem[T]{State: TrackAdded, Item: item})
}

// Update 标记身份匹配的子实体为已修改：
// unchanged 项转为 modified；尚未持久化的 added 项仅原地替换内容，状态保持 added；
// 身份未命中或已删除的项忽略。
func (tl *TrackedList[T]) Update(item T) {
	i := tl.indexOf(item)
	if i < 0 {
		return
	}
	switch tl.items[i].State {
	case TrackUnchanged:
		tl.items[i].State = TrackModified
		tl.items[i].Item = item
	case TrackAdded:
		tl.items[i].Item = item
	}
}

// Delete 删除身份匹配的子实体：
// added 项尚未落库，直接移出集合；其余项标记为 deleted；身份未命中时忽略。
func (tl *TrackedList[T]) Delete(item T) {
	i := tl.indexOf(item)
	if i < 0 {
		return
	}
	if tl.items[i].State == TrackAdded {
		tl.items = append(tl.items[:i], tl.items[i+1:]...)
		return
	}
	tl.items[i].State = TrackDeleted
	tl.items[i].Item = item
}

// GetChanges 返回需要持久化的变更条目（added/modified/deleted），不含 unchanged 项。
func (tl *TrackedList[T]) GetChanges() []*TrackedItem[T] {
	changes := make([]*TrackedItem[T], 0, len(tl.items))
	for _, item := range tl.items {
		if item.State != TrackUnchanged {
			changes = append(changes, item)
		}
	}
	return changes
}

// indexOf 返回身份匹配元素的下标，未命中返回 -1。
func (tl *TrackedList[T]) indexOf(item T) int {
	for i, existing := range tl.items {
		if tl.equal(existing.Item, item) {
			return i
		}
	}
	return -1
}
