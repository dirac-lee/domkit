package eventbus

import "github.com/dirac-lee/domkit/domain"

// FuncHandler 包装函数为 EventHandler。
type FuncHandler func(evt domain.DomainEvent) error

func (f FuncHandler) Handle(evt domain.DomainEvent) error {
	return f(evt)
}
