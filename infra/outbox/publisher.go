package outbox

import (
	"context"

	"github.com/dirac-lee/domkit/domain"
)

// EventPublisher 事件发布器接口
type EventPublisher interface {
	Publish(ctx context.Context, evt domain.DomainEvent) error
}
