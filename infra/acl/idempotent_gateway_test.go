package acl

import (
	"context"
	"errors"
	"testing"
)

// idempotentSetup 构造幂等写装配；queryFound 控制查重结果，doWriteCall 记录写入是否发生。
func idempotentSetup(
	queryFound bool,
	queryErr error,
	doWriteCall func(),
) IdempotentWrite[string, string, string, string, string] {
	return IdempotentWrite[string, string, string, string, string]{
		CallPipeline: stringPipeline(
			func(p string) (string, error) { return "REQ:" + p, nil },
			func(context.Context, string) (string, error) {
				if doWriteCall != nil {
					doWriteCall()
				}
				return "NEW", nil
			},
			func(s string) (string, error) { return "WROTE:" + s, nil },
		),
		UniqueKey: func(p string) string { return "KEY:" + p },
		QueryByKey: func(_ context.Context, key string) (string, bool, error) {
			return "EXIST:" + key, queryFound, queryErr
		},
		FromExisting: func(s string) (string, error) { return "DONE-EXISTING:" + s, nil },
	}
}

// 查重命中：短路返回已存在记录，不再写入。
func TestWriteIdempotentShortCircuitsExisting(t *testing.T) {
	log := &recordedLogger{}
	writeCalled := false

	r, err := WriteIdempotent(
		context.Background(), "p", idempotentSetup(true, nil, func() { writeCalled = true }), log,
	)
	if err != nil {
		t.Fatalf("WriteIdempotent 返回错误: %v", err)
	}
	if r != "DONE-EXISTING:EXIST:KEY:p" {
		t.Fatalf("结果 = %q", r)
	}
	if writeCalled {
		t.Fatal("查重命中时不应再写入")
	}
	if len(log.responses) != 1 || log.responses[0] != "EXIST:KEY:p" {
		t.Fatalf("短路响应钩子 = %v，期望记录查重返回的已存在记录", log.responses)
	}
}

// 查重未命中：执行正常写入。
func TestWriteIdempotentWritesWhenAbsent(t *testing.T) {
	writeCalled := false
	_, err := WriteIdempotent(
		context.Background(), "p", idempotentSetup(false, nil, func() { writeCalled = true }),
		NopCallLogger[string, string]{},
	)
	if err != nil {
		t.Fatalf("WriteIdempotent 返回错误: %v", err)
	}
	if !writeCalled {
		t.Fatal("查重未命中应执行写入")
	}
}

// 查重查询失败：归为 CommunicationError（可重试）。
func TestWriteIdempotentQueryError(t *testing.T) {
	errQuery := errors.New("query failed")
	_, err := WriteIdempotent(
		context.Background(), "p", idempotentSetup(false, errQuery, nil), nil,
	)
	var commErr *CommunicationError
	if !errors.As(err, &commErr) {
		t.Fatalf("期望 CommunicationError，得到 %T: %v", err, err)
	}
}
