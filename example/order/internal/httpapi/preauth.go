package httpapi

import (
	"net/http"

	"github.com/dirac-lee/domkit/example/order/internal/order/port"
)

// preauth 对指定订单向外部支付网关发起预授权。
// 先经 Commands.Get 取订单（金额/订单号），再通过 Payments 用例走先查后写的端口；
// HTTP 层不直接接触 ACL/支付协议，重复调用由端口实现保证幂等。
func (h *orderHandler) preauth(w http.ResponseWriter, r *http.Request) {
	orderID := pathOrderID(r)
	o, err := h.app.Commands.Get(r.Context(), orderID)
	if err != nil {
		writeError(w, err)
		return
	}

	result, err := h.app.Payments.Authorize(r.Context(), port.PreAuthCommand{
		OrderID: o.ID,
		OrderNo: o.OrderNo,
		Amount:  o.Amount,
	})
	if err != nil {
		writeError(w, err)
		return
	}
	writeOK(w, http.StatusOK, result)
}
