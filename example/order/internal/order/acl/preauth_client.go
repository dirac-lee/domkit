package acl

import (
	"context"
	"errors"
	"log"

	"github.com/dirac-lee/domkit/example/order/internal/order/port"
	frameworkacl "github.com/dirac-lee/domkit/infra/acl"
)

// PreAuthClient 支付预授权防腐层适配器：用「先查后写」幂等写套路对接 PaymentGateway，
// 把对方协议隔离在 acl 包，对外实现端口 port.PaymentAuthorizer。
type PreAuthClient struct {
	gateway *PaymentGateway
	logger  frameworkacl.CallLogger[PreAuthRequest, PreAuthResponse]
}

// 编译期断言：PreAuthClient 必须满足端口契约，装配错误提前在编译期暴露。
var _ port.PaymentAuthorizer = (*PreAuthClient)(nil)

// NewPreAuthClient 构造客户端；logger 传 nil 时套路内部回退空钩子。
func NewPreAuthClient(
	gateway *PaymentGateway, logger frameworkacl.CallLogger[PreAuthRequest, PreAuthResponse],
) *PreAuthClient {
	return &PreAuthClient{gateway: gateway, logger: logger}
}

// Authorize 发起预授权：对方已受理则短路返回原流水，否则写入新预授权。
func (c *PreAuthClient) Authorize(ctx context.Context, cmd port.PreAuthCommand) (port.PreAuthResult, error) {
	setup := frameworkacl.IdempotentWrite[port.PreAuthCommand, port.PreAuthResult, PreAuthRequest, PreAuthResponse, string]{
		CallPipeline: frameworkacl.CallPipeline[port.PreAuthCommand, port.PreAuthResult, PreAuthRequest, PreAuthResponse]{
			ToRequest: toPreAuthRequest,
			DoCall:    c.gateway.Create,
			ToResult:  newPreAuthResult,
		},
		UniqueKey:    outTradeNoOf,
		QueryByKey:   c.gateway.QueryByKey,
		FromExisting: existingPreAuthResult,
	}
	return frameworkacl.WriteIdempotent(ctx, cmd, setup, c.logger)
}

// toPreAuthRequest 端口命令 → 对方请求；订单号缺失属本地转换错误，提前返回。
func toPreAuthRequest(cmd port.PreAuthCommand) (PreAuthRequest, error) {
	if cmd.OrderNo == "" {
		return PreAuthRequest{}, errors.New("订单号为空，无法生成支付商户单号")
	}
	return PreAuthRequest{OutTradeNo: outTradeNoOf(cmd), AmountFen: cmd.Amount}, nil
}

// outTradeNoOf 生成对外唯一商户单号作为查重键；先查、后写共用此函数，保证两步针对同一键。
func outTradeNoOf(cmd port.PreAuthCommand) string {
	return "orderno-" + cmd.OrderNo
}

// newPreAuthResult 正常写入响应 → 端口结果（标记非复用）。
func newPreAuthResult(resp PreAuthResponse) (port.PreAuthResult, error) {
	return port.PreAuthResult{PreAuthID: resp.PreAuthID, Reused: false}, nil
}

// existingPreAuthResult 查重命中的已存在记录 → 端口结果（标记复用）。
func existingPreAuthResult(resp PreAuthResponse) (port.PreAuthResult, error) {
	return port.PreAuthResult{PreAuthID: resp.PreAuthID, Reused: true}, nil
}

// PreAuthLogger 把 ACL 三钩子输出到标准日志，演示外部调用的可观测性。
type PreAuthLogger struct{ logger *log.Logger }

// NewPreAuthLogger 构造日志钩子；logger 传 nil 使用默认标准日志。
func NewPreAuthLogger(logger *log.Logger) *PreAuthLogger {
	if logger == nil {
		logger = log.Default()
	}
	return &PreAuthLogger{logger: logger}
}

func (l *PreAuthLogger) OnRequest(req PreAuthRequest) {
	l.logger.Printf("支付预授权请求 outTradeNo=%s amountFen=%d", req.OutTradeNo, req.AmountFen)
}

func (l *PreAuthLogger) OnResponse(resp PreAuthResponse) {
	l.logger.Printf("支付预授权响应 preAuthID=%s", resp.PreAuthID)
}

func (l *PreAuthLogger) OnError(err error) {
	l.logger.Printf("支付预授权异常 %v", err)
}
