package acl

import (
	"context"
	"fmt"
	"sync"
)

// PreAuthRequest 支付网关「预授权」接口请求（对方协议 DTO，非领域模型）。
type PreAuthRequest struct {
	OutTradeNo string // 商户单号：对方去重的唯一键
	AmountFen  int64  // 金额，单位：分
}

// PreAuthResponse 支付网关响应 DTO。
type PreAuthResponse struct {
	PreAuthID string // 支付侧预授权流水号
}

// PaymentGateway 模拟外部支付服务：内存记录已受理预授权，按商户单号可查，
// 复刻「对方接口按唯一键去重」的真实语义。自带互斥锁，支持并发/重复调用演示。
type PaymentGateway struct {
	mu      sync.Mutex
	records map[string]PreAuthResponse
	seq     int
}

// NewPaymentGateway 构造空的模拟支付网关。
func NewPaymentGateway() *PaymentGateway {
	return &PaymentGateway{records: make(map[string]PreAuthResponse)}
}

// QueryByKey 按商户单号查询已受理预授权；found=false 表示尚未受理。
func (g *PaymentGateway) QueryByKey(_ context.Context, outTradeNo string) (PreAuthResponse, bool, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	resp, found := g.records[outTradeNo]
	return resp, found, nil
}

// Create 受理预授权并落记录（真实系统此处是 HTTP/SDK 调用，属通信步骤）。
func (g *PaymentGateway) Create(_ context.Context, req PreAuthRequest) (PreAuthResponse, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.seq++
	resp := PreAuthResponse{PreAuthID: fmt.Sprintf("preauth-%d", g.seq)}
	g.records[req.OutTradeNo] = resp
	return resp, nil
}
