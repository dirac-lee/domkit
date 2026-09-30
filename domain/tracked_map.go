package domain

// TrackedMap 键值集合的变更追踪容器：把一个 Map 的状态拆成「基线 / 待写入 / 待删除」三部分，
// 使持久化只发增量 INSERT/UPDATE/DELETE，而不是"先全删再全插"。
//
//	K 既是 Map 的键，也是持久化行标识（因此可原地定位）；
//	V 为行数据类型。
//
// 零值不可直接使用，请用 NewTrackedMap / NewTrackedMapFrom 构造。
type TrackedMap[K comparable, V any] struct {
	init    map[K]V        // 从存储加载的基线
	put     map[K]V        // 待写入（新增 + 替换）
	removed map[K]struct{} // 待删除的键
}

// NewTrackedMap 创建空基线的追踪 Map。
func NewTrackedMap[K comparable, V any]() *TrackedMap[K, V] {
	return newTrackedMapWith(map[K]V{})
}

// NewTrackedMapFrom 以已有基线构造。直接持有传入 map 的引用、不遍历不拷贝
//（适配 ORM 懒加载、避免触发额外查询）；如需与外部隔离，请调用方先自行拷贝。
func NewTrackedMapFrom[K comparable, V any](base map[K]V) *TrackedMap[K, V] {
	if base == nil {
		base = map[K]V{}
	}
	return newTrackedMapWith(base)
}

// newTrackedMapWith 统一初始化待写入/待删除桶。
func newTrackedMapWith[K comparable, V any](base map[K]V) *TrackedMap[K, V] {
	return &TrackedMap[K, V]{
		init:    base,
		put:     map[K]V{},
		removed: map[K]struct{}{},
	}
}

// Put 放入或替换一个条目：
// 键在基线 → UPDATE 候选；键不在基线 → INSERT 候选。
func (m *TrackedMap[K, V]) Put(key K, value V) { m.put[key] = value }

// Remove 按键移除：
// 键在基线 → 进待删除桶（发 DELETE）；键仅为刚写入、尚未持久化 → 直接撤销，不进待删除。
func (m *TrackedMap[K, V]) Remove(key K) {
	if _, ok := m.init[key]; ok {
		m.removed[key] = struct{}{}
	}
	delete(m.put, key)
}

// Clear 清空整个集合：全部基线键进待删除，同时撤销所有待写入。
func (m *TrackedMap[K, V]) Clear() {
	for k := range m.init {
		m.removed[k] = struct{}{}
	}
	m.put = map[K]V{}
}

// Inserted 待插入条目：待写入中「键不在基线」或「键在基线但已被标记删除」
//（删了又加 = 净重新插入，发 INSERT 新行）。
func (m *TrackedMap[K, V]) Inserted() map[K]V {
	out := map[K]V{}
	for k, v := range m.put {
		_, inInit := m.init[k]
		_, isRemoved := m.removed[k]
		if !inInit || isRemoved {
			out[k] = v
		}
	}
	return out
}

// Updated 待更新条目：待写入中「键在基线且未被标记删除」（同一行原地 UPDATE）。
func (m *TrackedMap[K, V]) Updated() map[K]V {
	out := map[K]V{}
	for k, v := range m.put {
		_, inInit := m.init[k]
		_, isRemoved := m.removed[k]
		if inInit && !isRemoved {
			out[k] = v
		}
	}
	return out
}

// Removed 待删除条目：已标记删除、且未被重新写入的键（附带基线原值）。
// 删后又重新写入的键不在此出现——它净算重新插入，由 Inserted 承载。
func (m *TrackedMap[K, V]) Removed() map[K]V {
	out := map[K]V{}
	for k := range m.removed {
		if _, rePut := m.put[k]; rePut {
			continue
		}
		out[k] = m.init[k]
	}
	return out
}

// All 当前逻辑视图：基线中未被删除的条目，与全部待写入条目的并集。
func (m *TrackedMap[K, V]) All() map[K]V {
	out := make(map[K]V, len(m.init)+len(m.put))
	for k, v := range m.init {
		if _, isRemoved := m.removed[k]; !isRemoved {
			out[k] = v
		}
	}
	for k, v := range m.put {
		out[k] = v
	}
	return out
}
