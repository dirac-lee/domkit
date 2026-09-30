package acl

import "context"

// IdempotentWrite 「先查后写」幂等写的装配（参数对象）。
// 内嵌 CallPipeline 复用标准写入三段转换，另加查重三要素。
//
//	注意：本套路只降低重复概率，真正幂等仍需对方写入接口按唯一键去重。
//	P 领域入参  R 领域返回  Q 对方请求  S 对方响应  K 查重唯一键
type IdempotentWrite[P, R, Q, S any, K comparable] struct {
	CallPipeline[P, R, Q, S]
	UniqueKey    func(param P) K                                   // 从领域入参提取查重键
	QueryByKey   func(ctx context.Context, key K) (S, bool, error) // 用键查重，found=true 表示已处理
	FromExisting func(existing S) (R, error)                       // 已存在记录 → 领域返回
}

// WriteIdempotent 先查后写套路：
// 用唯一键查对方，已存在则把已存在记录转为领域结果短路返回；否则才真正写入。
func WriteIdempotent[P, R, Q, S any, K comparable](
	ctx context.Context, param P, setup IdempotentWrite[P, R, Q, S, K], logger CallLogger[Q, S],
) (R, error) {
	log := ensureLogger(logger)
	key := setup.UniqueKey(param)

	// 查重走「通信」归类（对方查询失败通常可重试）；found 用外层变量经闭包带出，
	// 从而仍能复用二值签名的 classify，无需另写三值助手。
	var found bool
	existing, err := classify(log, asCommunicationError, "查重查询", func() (S, error) {
		response, isFound, queryErr := setup.QueryByKey(ctx, key)
		found = isFound
		return response, queryErr
	})
	if err != nil {
		var zero R
		return zero, err
	}

	// 已处理过：短路返回已存在记录对应的领域结果，不再重复写入。
	if found {
		log.OnResponse(existing)
		return classify(log, asConversionError, "已存在记录转换", func() (R, error) {
			return setup.FromExisting(existing)
		})
	}

	// 未处理过：复用标准写入套路落库。
	return run(ctx, param, setup.CallPipeline, log)
}
