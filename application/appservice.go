package application

import (
	"context"

	"github.com/dirac-lee/domkit/domain"
)

// BaseAppService 应用服务基类
type BaseAppService struct {
	UoWFactory func() domain.UnitOfWork
}

// ExecuteCommand 执行命令
func (s *BaseAppService) ExecuteCommand(ctx context.Context, cmd Command) error {
	uow := s.UoWFactory()
	defer func() {
		if r := recover(); r != nil {
			_ = uow.Rollback(ctx)
		}
	}()

	err := cmd.Execute(ctx, uow)
	if err != nil {
		_ = uow.Rollback(ctx)
		return err
	}
	return uow.Commit(ctx)
}
