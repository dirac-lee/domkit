package config

import (
	"sort"
	"sync"
)

// MapSource 基于内存 Map 的配置源，可作为测试替身或轻量运行态配置。
// 内部用读写锁保护，支持并发读取与动态装配，对齐 Java 的 ConcurrentHashMap 语义。
type MapSource struct {
	mu    sync.RWMutex
	store map[string]string
}

// NewMapSource 构造空配置源。
func NewMapSource() *MapSource {
	return &MapSource{store: make(map[string]string)}
}

// NewMapSourceFrom 构造并批量载入初始条目（入参不会被内部修改，内部独立拷贝一份）。
func NewMapSourceFrom(initial map[string]string) *MapSource {
	store := make(map[string]string, len(initial))
	for k, v := range initial {
		store[k] = v
	}
	return &MapSource{store: store}
}

// Set 写入一个条目，返回当前实例便于链式装配；写操作加锁。
func (m *MapSource) Set(key, value string) *MapSource {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.store[key] = value
	return m
}

// Lookup 实现 Source，读操作加读锁。
func (m *MapSource) Lookup(key string) (string, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	v, ok := m.store[key]
	return v, ok
}

// Keys 实现 Source，返回按键名升序的副本，保证扫描/输出顺序稳定。
func (m *MapSource) Keys() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	keys := make([]string, 0, len(m.store))
	for k := range m.store {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
