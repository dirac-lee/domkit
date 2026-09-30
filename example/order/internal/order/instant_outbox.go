package order

import (
	"context"
	"fmt"
	"time"

	"github.com/dirac-lee/domkit/example/order/internal/order/infra/mysql"
	"github.com/dirac-lee/domkit/infra/outbox"
)

// instantToken 即时发布路径使用的认领令牌约定：空串。
// 消息刚在主事务内以 pending、空 token 落库，宽限期内 Relay 不会触碰，
// 因此即时路径可在「仍为 pending、token 为空」的守卫下直接推进状态。
const instantToken = ""

// instantOutboxStore 发件箱装饰器：在任意 OutboxStore（MySQL / 内存）之上
// 增加「提交成功后即时发布」能力，而具体落库仍委托给 inner，自身不碰存储细节。
type instantOutboxStore struct {
	inner      outbox.OutboxStore
	serializer outbox.DomainEventSerializer
	publisher  outbox.EventPublisher
}

// newInstantOutboxStore 创建即时发布装饰器。
func newInstantOutboxStore(inner outbox.OutboxStore,
	ser outbox.DomainEventSerializer, pub outbox.EventPublisher) *instantOutboxStore {
	return &instantOutboxStore{inner: inner, serializer: ser, publisher: pub}
}

// 编译期断言：装饰器必须仍是一个合法的 OutboxStore。
var _ outbox.OutboxStore = (*instantOutboxStore)(nil)

// SaveMessage 先委托 inner 落库，再登记提交后即时发布。
func (s *instantOutboxStore) SaveMessage(ctx context.Context, tx any, msg *outbox.OutboxMessage) error {
	if err := s.inner.SaveMessage(ctx, tx, msg); err != nil {
		return err
	}
	snapshot := *msg // 复制快照：回调在提交后异步执行，避免与聚合后续状态相互影响
	// 有真实事务时延迟到提交后执行；无事务（内存测试）时 OnAfterCommit 立即执行。
	mysql.OnAfterCommit(ctx, func() error {
		return s.publishInstant(snapshot)
	})
	return nil
}

// publishInstant 还原事件 → 发布 → 标记 sent。
// 消息体损坏直接进死信；发布失败保留 pending，交 Relay 过宽限期后重投。
func (s *instantOutboxStore) publishInstant(msg outbox.OutboxMessage) error {
	ctx := context.Background()

	evt, err := s.serializer.Deserialize(msg.Payload, msg.EventName)
	if err != nil {
		reason := fmt.Sprintf("instant deserialize failed: %v", err)
		if dlErr := s.inner.MoveToDeadLetter(ctx, msg.ID, instantToken, reason); dlErr != nil {
			return fmt.Errorf("order: move dead-letter %q failed: %w", msg.ID, dlErr)
		}
		return nil
	}

	if err := s.publisher.Publish(ctx, evt); err != nil {
		// 状态保持 pending：Relay 兜底，此处错误供提交钩子聚合记录。
		return fmt.Errorf("order: instant publish %q failed: %w", msg.ID, err)
	}
	if err := s.inner.MarkSent(ctx, msg.ID, instantToken); err != nil {
		return fmt.Errorf("order: mark sent %q failed: %w", msg.ID, err)
	}
	return nil
}

// ClaimPending 委托 inner（Relay 路径，装饰器不改变认领语义）。
func (s *instantOutboxStore) ClaimPending(ctx context.Context, batchSize int,
	grace, lease time.Duration, token string) ([]*outbox.OutboxMessage, error) {
	return s.inner.ClaimPending(ctx, batchSize, grace, lease, token)
}

// MarkSent 委托 inner。
func (s *instantOutboxStore) MarkSent(ctx context.Context, id, token string) error {
	return s.inner.MarkSent(ctx, id, token)
}

// Release 委托 inner。
func (s *instantOutboxStore) Release(ctx context.Context, id, token, reason string) (int, error) {
	return s.inner.Release(ctx, id, token, reason)
}

// MoveToDeadLetter 委托 inner。
func (s *instantOutboxStore) MoveToDeadLetter(ctx context.Context, id, token, reason string) error {
	return s.inner.MoveToDeadLetter(ctx, id, token, reason)
}
