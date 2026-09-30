package acl

import (
	"context"
	"errors"
	"testing"
)

// recordedLogger 记录三类钩子被调用时的快照，供断言触发内容与次数。
// 测试中统一使用 string 作为请求/响应类型，便于比对。
type recordedLogger struct {
	requests  []string
	responses []string
	errors    []error
}

func (l *recordedLogger) OnRequest(q string)  { l.requests = append(l.requests, q) }
func (l *recordedLogger) OnResponse(s string) { l.responses = append(l.responses, s) }
func (l *recordedLogger) OnError(e error)     { l.errors = append(l.errors, e) }

// stringPipeline 构造一组字符串类型的三段转换；doCalled 用于确认调用阶段是否发生。
func stringPipeline(
	toRequest func(string) (string, error),
	doCall func(context.Context, string) (string, error),
	toResult func(string) (string, error),
) CallPipeline[string, string, string, string] {
	return CallPipeline[string, string, string, string]{
		ToRequest: toRequest,
		DoCall:    doCall,
		ToResult:  toResult,
	}
}

// 正常链路：三段转换串接，钩子按序触发。
func TestQueryHappyPath(t *testing.T) {
	log := &recordedLogger{}
	pipeline := stringPipeline(
		func(p string) (string, error) { return "REQ:" + p, nil },
		func(_ context.Context, q string) (string, error) { return "RESP:" + q, nil },
		func(s string) (string, error) { return "OUT:" + s, nil },
	)

	r, err := Query(context.Background(), "p1", pipeline, log)
	if err != nil {
		t.Fatalf("Query 返回错误: %v", err)
	}
	if r != "OUT:RESP:REQ:p1" {
		t.Fatalf("结果 = %q，期望 OUT:RESP:REQ:p1", r)
	}
	if len(log.requests) != 1 || log.requests[0] != "REQ:p1" {
		t.Fatalf("OnRequest 钩子 = %v，期望 [REQ:p1]", log.requests)
	}
	if len(log.responses) != 1 || log.responses[0] != "RESP:REQ:p1" {
		t.Fatalf("OnResponse 钩子 = %v", log.responses)
	}
	if len(log.errors) != 0 {
		t.Fatalf("正常链路不应触发 OnError，得到 %v", log.errors)
	}
}

// 请求转换失败：归为 ConversionError（不可重试），且不再调用对方。
func TestQueryRequestConversionError(t *testing.T) {
	errToRequest := errors.New("bad param")
	log := &recordedLogger{}
	doCalled := false
	pipeline := stringPipeline(
		func(string) (string, error) { return "", errToRequest },
		func(context.Context, string) (string, error) { doCalled = true; return "", nil },
		func(string) (string, error) { return "", nil },
	)

	_, err := Query(context.Background(), "p", pipeline, log)
	if doCalled {
		t.Fatal("请求转换失败不应调用对方")
	}
	assertConversionError(t, err, errToRequest)
	if IsRetryableErr(err) {
		t.Fatal("转换错误不可重试")
	}
}

// 通信失败：归为 CommunicationError（可重试），且不做响应转换。
func TestQueryCommunicationError(t *testing.T) {
	errCall := errors.New("timeout")
	pipeline := stringPipeline(
		func(p string) (string, error) { return p, nil },
		func(context.Context, string) (string, error) { return "", errCall },
		func(string) (string, error) { t.Fatal("通信失败不应做响应转换"); return "", nil },
	)

	_, err := Query(context.Background(), "p", pipeline, &recordedLogger{})
	var commErr *CommunicationError
	if !errors.As(err, &commErr) {
		t.Fatalf("期望 CommunicationError，得到 %T: %v", err, err)
	}
	if !IsRetryableErr(err) {
		t.Fatal("通信错误应可重试")
	}
	if !errors.Is(err, errCall) {
		t.Fatal("应保留通信原始错误")
	}
}

// 响应转换失败：归为 ConversionError。
func TestQueryResponseConversionError(t *testing.T) {
	errToResult := errors.New("bad mapping")
	pipeline := stringPipeline(
		func(p string) (string, error) { return p, nil },
		func(context.Context, string) (string, error) { return "s", nil },
		func(string) (string, error) { return "", errToResult },
	)

	_, err := Query(context.Background(), "p", pipeline, &recordedLogger{})
	assertConversionError(t, err, errToResult)
}

// nil logger 时应安全回退空实现，不 panic。
func TestQueryNilLoggerSafe(t *testing.T) {
	pipeline := stringPipeline(
		func(p string) (string, error) { return p, nil },
		func(_ context.Context, q string) (string, error) { return q, nil },
		func(s string) (string, error) { return s, nil },
	)
	if _, err := Query(context.Background(), "p", pipeline, nil); err != nil {
		t.Fatalf("nil logger 不应导致错误: %v", err)
	}
}

// Write 与 Query 共用核心，验证写入套路同样可用。
func TestWriteHappyPath(t *testing.T) {
	pipeline := stringPipeline(
		func(p string) (string, error) { return p, nil },
		func(_ context.Context, q string) (string, error) { return q, nil },
		func(s string) (string, error) { return s, nil },
	)
	if _, err := Write(context.Background(), "p", pipeline, nil); err != nil {
		t.Fatalf("Write 返回错误: %v", err)
	}
}

// assertConversionError 断言 err 是包裹了 cause 的 ConversionError，供多个用例复用。
func assertConversionError(t *testing.T, err error, cause error) {
	t.Helper()
	var convErr *ConversionError
	if !errors.As(err, &convErr) {
		t.Fatalf("期望 *ConversionError，得到 %T: %v", err, err)
	}
	if !errors.Is(err, cause) {
		t.Fatal("应保留转换原始错误")
	}
}
