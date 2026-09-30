package application

import (
	"context"
	"errors"

	"github.com/dirac-lee/domkit/domain"
)

// errBoom 模拟持久化等外部依赖返回的通用错误。
var errBoom = errors.New("boom")

// ---- 假聚合 ----

// fakeAggregate 测试用聚合，内嵌 AggregateRoot 获得版本/事件等能力。
type fakeAggregate struct {
	domain.AggregateRoot[string]
}

// newAggregate 构造一个尚未持久化（基线版本 0）的假聚合。
func newAggregate(id string) *fakeAggregate {
	return &fakeAggregate{AggregateRoot: domain.AggregateRoot[string]{ID: id}}
}

// ---- 假事件 ----

// fakeEvent 测试用事件，固定事件名 fake.event。
type fakeEvent struct {
	domain.BaseDomainEvent[string]
}

func (fakeEvent) EventName() string { return "fake.event" }

// newEvent 构造归属聚合为 id 的假事件。
func newEvent(id string) *fakeEvent {
	return &fakeEvent{BaseDomainEvent: domain.NewBaseDomainEvent(id, 1)}
}

// ---- 假仓储 ----

// fakeRepo 记录被调用的 Insert/Update，并可按 failOn 注入对应方法的失败。
type fakeRepo struct {
	inserts []*fakeAggregate
	updates []*fakeAggregate
	failOn  string // 取值 insert/update/delete 时，对应方法返回 errBoom
}

func (r *fakeRepo) Insert(_ context.Context, agg *fakeAggregate) error {
	if r.failOn == "insert" {
		return errBoom
	}
	r.inserts = append(r.inserts, agg)
	return nil
}

func (r *fakeRepo) Update(_ context.Context, agg *fakeAggregate) error {
	if r.failOn == "update" {
		return errBoom
	}
	r.updates = append(r.updates, agg)
	return nil
}

func (r *fakeRepo) Delete(context.Context, *fakeAggregate) error {
	if r.failOn == "delete" {
		return errBoom
	}
	return nil
}

func (r *fakeRepo) GetByID(context.Context, string) (*fakeAggregate, error) {
	return nil, nil
}

func (r *fakeRepo) CurrentVersion(context.Context, string) (uint64, bool, error) {
	return 0, true, nil
}

// 编译期断言：fakeRepo 必须满足仓储接口，签名漂移会在编译期暴露。
var _ domain.Repository[string, fakeAggregate] = (*fakeRepo)(nil)
