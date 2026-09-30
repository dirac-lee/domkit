package application

import (
	"context"

	"github.com/dirac-lee/domkit/domain"
)

// Command 命令接口
type Command interface {
	Execute(ctx context.Context, uow domain.UnitOfWork) error
}
