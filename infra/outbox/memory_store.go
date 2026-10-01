package outbox

import (
	"context"
	"sort"
	"sync"
	"time"
)

// MemoryOutboxStore 线程安全的内存发件箱存储，作为 OutboxStore 的参考实现，
// 供测试与无数据库环境使用。认领/状态推进在互斥区内原子完成，
// 语义等价于数据库实现中「单条带守卫的 UPDATE ... LIMIT」。
type MemoryOutboxStore struct {
	mu  sync.Mutex
	now func() time.Time // 可注入时钟（测试租约过期）
	row map[string]*OutboxMessage

	// 观察钩子（测试用）
	Saved []*OutboxMessage
}

// NewMemoryOutboxStore 创建内存发件箱存储。
func NewMemoryOutboxStore() *MemoryOutboxStore {
	return &MemoryOutboxStore{
		now:   time.Now,
		row:   make(map[string]*OutboxMessage),
		Saved: nil,
	}
}

// SetClock 注入时钟函数（测试用，模拟租约超时）。
func (s *MemoryOutboxStore) SetClock(now func() time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.now = now
}

// SaveMessage 写入待发消息（内存实现忽略 tx，调用方仍应保证与聚合落库同事务的语义约定）。
func (s *MemoryOutboxStore) SaveMessage(_ context.Context, _ any, msg *OutboxMessage) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	cp := *msg
	if len(msg.Payload) > 0 {
		cp.Payload = append([]byte(nil), msg.Payload...)
	}
	s.row[msg.ID] = &cp
	s.Saved = append(s.Saved, s.copyRow(&cp))
	return nil
}

// ClaimPending 原子认领：持锁扫描候选行并翻转，多 goroutine/多实例并发时
// 同一行只会被一个 token 抢中（先行翻 status 再返回，等价于行级 UPDATE 原子性）。
func (s *MemoryOutboxStore) ClaimPending(_ context.Context, batchSize int, grace, lease time.Duration, token string) ([]*OutboxMessage, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	pendingCutoff := now.Add(-grace)
	leaseCutoff := now.Add(-lease)

	var candidates []*OutboxMessage
	for _, m := range s.row {
		switch m.Status {
		case StatusPending:
			if !m.CreatedAt.After(pendingCutoff) {
				candidates = append(candidates, m)
			}
		case StatusProcessing:
			// 持有者崩溃/卡住：租约过期后允许被其他 token 重新认领。
			if !m.ClaimedAt.After(leaseCutoff) {
				candidates = append(candidates, m)
			}
		}
	}
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].CreatedAt.Before(candidates[j].CreatedAt)
	})
	if batchSize > 0 && len(candidates) > batchSize {
		candidates = candidates[:batchSize]
	}

	out := make([]*OutboxMessage, 0, len(candidates))
	for _, m := range candidates {
		m.Status = StatusProcessing
		m.ClaimToken = token
		m.ClaimedAt = now
		out = append(out, s.copyRow(m))
	}
	return out, nil
}

// MarkSent token+状态守卫的成功标记。
func (s *MemoryOutboxStore) MarkSent(_ context.Context, id, token string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	m, ok := s.row[id]
	if !ok || m.ClaimToken != token || m.Status != StatusProcessing {
		return ErrClaimLost
	}
	m.Status = StatusSent
	m.ClaimToken = ""
	m.SentAt = s.now()
	return nil
}

// Release token 守卫的释放回 pending，返回递增后的重试次数。
func (s *MemoryOutboxStore) Release(_ context.Context, id, token, reason string) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	m, ok := s.row[id]
	if !ok || m.ClaimToken != token || m.Status != StatusProcessing {
		return 0, ErrClaimLost
	}
	m.RetryCount++
	m.LastError = reason
	m.Status = StatusPending
	m.ClaimToken = ""
	m.ClaimedAt = time.Time{}
	return m.RetryCount, nil
}

// MoveToDeadLetter token 守卫的死信转移。
func (s *MemoryOutboxStore) MoveToDeadLetter(_ context.Context, id, token, reason string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	m, ok := s.row[id]
	if !ok || m.ClaimToken != token || m.Status != StatusProcessing {
		return ErrClaimLost
	}
	m.Status = StatusDeadLetter
	m.LastError = reason
	m.ClaimToken = ""
	return nil
}

// Get 按 ID 查询消息快照（测试/运维观察用，不存在返回 nil）。
func (s *MemoryOutboxStore) Get(id string) *OutboxMessage {
	s.mu.Lock()
	defer s.mu.Unlock()
	m, ok := s.row[id]
	if !ok {
		return nil
	}
	return s.copyRow(m)
}

func (s *MemoryOutboxStore) copyRow(m *OutboxMessage) *OutboxMessage {
	cp := *m
	if m.Payload != nil {
		cp.Payload = append([]byte(nil), m.Payload...)
	}
	return &cp
}
