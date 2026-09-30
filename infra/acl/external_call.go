package acl

import "context"

// CallPipeline 一次外部调用的三段转换（参数对象）。
// 用结构体聚合三个函数，避免套路调用出现「四五个函数参数」的过长参数列表坏味道。
//
//	P：领域入参  R：领域返回  Q：对方请求  S：对方响应
type CallPipeline[P, R, Q, S any] struct {
	ToRequest func(param P) (Q, error)                        // 领域入参 → 对方请求
	DoCall    func(ctx context.Context, request Q) (S, error) // 真正调用对方
	ToResult  func(response S) (R, error)                     // 对方响应 → 领域返回
}

// Query 查询套路：无副作用，调用失败可由上层直接重试。
// logger 传 nil 时使用空实现。
func Query[P, R, Q, S any](
	ctx context.Context, param P, pipeline CallPipeline[P, R, Q, S], logger CallLogger[Q, S],
) (R, error) {
	return run(ctx, param, pipeline, ensureLogger(logger))
}

// Write 写入套路：有副作用。通信异常向上抛出由上层重试；
// 本地转换异常归为 ConversionError，不应被误判为「可重试的写超时」。
func Write[P, R, Q, S any](
	ctx context.Context, param P, pipeline CallPipeline[P, R, Q, S], logger CallLogger[Q, S],
) (R, error) {
	return run(ctx, param, pipeline, ensureLogger(logger))
}

// run Query / Write 共用的编排核心，统一执行「请求转换 → 调用 → 响应转换」，
// 任一阶段失败立即返回（提前返回，不做多层嵌套），并在对应节点触发日志钩子。
func run[P, R, Q, S any](
	ctx context.Context, param P, pipeline CallPipeline[P, R, Q, S], logger CallLogger[Q, S],
) (R, error) {
	request, err := classify(logger, asConversionError, "请求转换", func() (Q, error) {
		return pipeline.ToRequest(param)
	})
	if err != nil {
		var zero R
		return zero, err
	}
	logger.OnRequest(request)

	response, err := classify(logger, asCommunicationError, "外部调用", func() (S, error) {
		return pipeline.DoCall(ctx, request)
	})
	if err != nil {
		var zero R
		return zero, err
	}
	logger.OnResponse(response)

	return classify(logger, asConversionError, "响应转换", func() (R, error) {
		return pipeline.ToResult(response)
	})
}

// ensureLogger 保证钩子非 nil：调用方未提供 logger 时回退空实现，避免空接口调用 panic。
func ensureLogger[Q, S any](logger CallLogger[Q, S]) CallLogger[Q, S] {
	if logger != nil {
		return logger
	}
	return NopCallLogger[Q, S]{}
}

// errorWrapper 把「阶段 + 原始错误」包装为某一类 ACL 错误。
type errorWrapper func(stage string, err error) error

// asConversionError / asCommunicationError 供 classify 选择错误归类。
func asConversionError(stage string, err error) error {
	return &ConversionError{Stage: stage, Err: err}
}

func asCommunicationError(stage string, err error) error {
	return &CommunicationError{Stage: stage, Err: err}
}

// classify 执行一个步骤：成功直接返回；失败先触发 OnError（原始错误），
// 再经 wrap 归类为转换/通信错误。转换与通信两条路径共用本函数，消除重复样板。
func classify[T, Q, S any](
	logger CallLogger[Q, S], wrap errorWrapper, stage string, step func() (T, error),
) (T, error) {
	value, err := step()
	if err == nil {
		return value, nil
	}
	logger.OnError(err)
	var zero T
	return zero, wrap(stage, err)
}
