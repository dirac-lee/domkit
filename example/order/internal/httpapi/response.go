package httpapi

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"

	"github.com/dirac-lee/domkit/domain"
	"github.com/dirac-lee/domkit/example/order/internal/order/app"
)

// 业务错误码：0 表示成功；非 0 与 HTTP 状态解耦，便于前端按码分支处理。
const (
	codeSuccess    = 0
	codeBadRequest = 40000
	codeRuleBroken = 42200
	codeNotFound   = 40400
	codeConflict   = 40900
	codeInternal   = 50000
)

var (
	// errBadRequest 由 handler 在请求参数非法时返回，统一映射为 400。
	errBadRequest = errors.New("invalid request parameters")
	// errDuplicatePay 支付幂等守卫命中：窗口内重复支付，映射为 409。
	errDuplicatePay = errors.New("duplicate pay request in progress")
)

// Result 统一响应包装：Code=0 时取 Data；否则 Msg 携带错误信息。
type Result[T any] struct {
	Code int    `json:"code"`
	Msg  string `json:"msg,omitempty"`
	Data T      `json:"data,omitempty"`
}

// writeJSON 序列化 JSON 写回，先置响应头再写状态行。
func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

// writeOK 写成功响应。
func writeOK(w http.ResponseWriter, status int, data any) {
	writeJSON(w, status, Result[any]{Code: codeSuccess, Data: data})
}

// writeError 全局错误映射：把领域/基础设施错误翻译成 HTTP 状态 + 统一响应体，
// 未预期错误不向外泄漏内部细节。注意订单未命中取应用层 app.ErrOrderNotFound。
func writeError(w http.ResponseWriter, err error) {
	var ruleErr *domain.DomainRuleError
	switch {
	case errors.Is(err, errBadRequest):
		writeJSON(w, http.StatusBadRequest, Result[any]{Code: codeBadRequest, Msg: err.Error()})
	case errors.Is(err, errDuplicatePay):
		writeJSON(w, http.StatusConflict, Result[any]{Code: codeConflict, Msg: err.Error()})
	case errors.As(err, &ruleErr):
		// 领域规则不通过：422；聚合错误可能含多条违反，取首条文案返回。
		writeJSON(w, http.StatusUnprocessableEntity,
			Result[any]{Code: codeRuleBroken, Msg: firstRuleMessage(ruleErr)})
	case errors.Is(err, app.ErrOrderNotFound):
		writeJSON(w, http.StatusNotFound, Result[any]{Code: codeNotFound, Msg: err.Error()})
	case domain.IsConcurrencyConflict(err):
		writeJSON(w, http.StatusConflict, Result[any]{Code: codeConflict, Msg: err.Error()})
	case errors.Is(err, app.ErrOrderNoDuplicate):
		// 订单号命中唯一键：409；err 经 outbox 持久化层包了内部前缀，直接取哨兵干净文案。
		writeJSON(w, http.StatusConflict, Result[any]{Code: codeConflict, Msg: app.ErrOrderNoDuplicate.Error()})
	default:
		writeJSON(w, http.StatusInternalServerError,
			Result[any]{Code: codeInternal, Msg: "internal server error"})
	}
}

// firstRuleMessage 取规则错误首条违反的文案，无明细时回退到整串错误。
func firstRuleMessage(e *domain.DomainRuleError) string {
	if len(e.BrokenRules) > 0 {
		return e.BrokenRules[0].Message
	}
	return e.Error()
}

// recoverer 兜底中间件：下游 handler panic 时记录并返回 500，避免连接裸崩。
func recoverer(logger *log.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if rec := recover(); rec != nil {
					logger.Printf("panic recovered: %v %s %s", rec, r.Method, r.URL.Path)
					writeJSON(w, http.StatusInternalServerError,
						Result[any]{Code: codeInternal, Msg: "internal server error"})
				}
			}()
			next.ServeHTTP(w, r)
		})
	}
}
