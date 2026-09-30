package eventbus

import (
	"sort"
	"sync"
)

// subItem 一个订阅登记项。
type subItem struct {
	name     string
	handler  EventHandler
	priority int
}

// registry 并发安全的订阅者注册表，同步/异步总线共用。
type registry struct {
	mu          sync.RWMutex
	subscribers map[string][]subItem
}

func newRegistry() *registry {
	return &registry{subscribers: make(map[string][]subItem)}
}

// Subscribe 登记订阅者并按 priority 降序重排（同优先级保持登记顺序）。
func (r *registry) Subscribe(eventName, subscriberName string, handler EventHandler, priority int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.subscribers[eventName] = append(r.subscribers[eventName], subItem{
		name:     subscriberName,
		handler:  handler,
		priority: priority,
	})
	sort.SliceStable(r.subscribers[eventName], func(i, j int) bool {
		return r.subscribers[eventName][i].priority > r.subscribers[eventName][j].priority
	})
}

// snapshot 返回某事件订阅者的副本，投递期间持锁时间最短。
func (r *registry) snapshot(eventName string) []subItem {
	r.mu.RLock()
	defer r.mu.RUnlock()
	items := r.subscribers[eventName]
	out := make([]subItem, len(items))
	copy(out, items)
	return out
}
