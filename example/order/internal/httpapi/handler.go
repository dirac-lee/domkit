package httpapi

import (
	"encoding/json"
	"io"
	"log"
	"net/http"

	"github.com/dirac-lee/domkit/example/order/internal/order"
	"github.com/dirac-lee/domkit/example/order/internal/order/app"
	orderdomain "github.com/dirac-lee/domkit/example/order/internal/order/domain"
)

const maxRequestBodyBytes = 1 << 20 // 1 MiB，防止异常大请求体占用过多内存。

// orderHandler 入站适配器：把 HTTP 请求翻译成对应用用例（Commands/Payments）的调用。
type orderHandler struct {
	app *order.Application
}

// NewRouter 装配全部路由并套上 panic 兜底中间件，返回可直接挂到 http.Server 的处理器。
// logger 用于 panic 记录；调用方（main）提供标准日志。
func NewRouter(app *order.Application, logger *log.Logger) http.Handler {
	mux := http.NewServeMux()
	h := &orderHandler{app: app}

	// Go 1.22+ 的「方法 + 路径」路由模式。
	mux.HandleFunc("POST /orders", h.create)
	mux.HandleFunc("GET /orders", h.list)
	mux.HandleFunc("GET /orders/{id}", h.get)
	mux.HandleFunc("POST /orders/{id}/pay", h.pay)
	mux.HandleFunc("POST /orders/{id}/cancel", h.cancel)
	mux.HandleFunc("POST /orders/{id}/reconcile", h.reconcile)
	mux.HandleFunc("POST /orders/{id}/preauth", h.preauth)
	mux.HandleFunc("GET /orders/{id}/broadcasts", h.broadcasts)

	return recoverer(logger)(mux)
}

// createOrderRequest 创建订单请求体。
type createOrderRequest struct {
	OrderNo string `json:"orderNo"`
	Amount  int64  `json:"amount"`
}

func (h *orderHandler) create(w http.ResponseWriter, r *http.Request) {
	var req createOrderRequest
	// 请求体非法：返回 400，不进入应用层。
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, errBadRequest)
		return
	}
	o, err := h.app.Commands.Create(r.Context(), req.OrderNo, req.Amount)
	if err != nil {
		writeError(w, err)
		return
	}
	writeOK(w, http.StatusCreated, toOrderDTO(o))
}

func (h *orderHandler) get(w http.ResponseWriter, r *http.Request) {
	id := pathOrderID(r)
	// cache-aside：先 Redis 后投影表，命中回填、未命中 404。
	summary, ok, err := h.app.SummaryCache.Get(r.Context(), id, h.app.ReadModel.GetByID)
	if err != nil {
		writeError(w, err)
		return
	}
	if !ok {
		writeError(w, app.ErrOrderNotFound)
		return
	}
	writeOK(w, http.StatusOK, toSummaryDTO(summary))
}

func (h *orderHandler) pay(w http.ResponseWriter, r *http.Request) {
	id := pathOrderID(r)
	// 支付幂等守卫：SET NX EX，窗口内重复支付直接 409。
	acquired, err := h.app.PayGuard.TryAcquire(r.Context(), id)
	if err != nil {
		writeError(w, err)
		return
	}
	if !acquired {
		writeError(w, errDuplicatePay)
		return
	}

	o, err := h.app.Commands.Pay(r.Context(), id)
	if err != nil {
		// 业务失败：主动释放守卫，允许用户立即重试，不必等 TTL。
		_ = h.app.PayGuard.Release(r.Context(), id)
		writeError(w, err)
		return
	}
	writeOK(w, http.StatusOK, toOrderDTO(o))
}

func (h *orderHandler) cancel(w http.ResponseWriter, r *http.Request) {
	o, err := h.app.Commands.Cancel(r.Context(), pathOrderID(r))
	if err != nil {
		writeError(w, err)
		return
	}
	writeOK(w, http.StatusOK, toOrderDTO(o))
}

// pathOrderID 读取路径中的订单主键，转为本域强类型 ID。
func pathOrderID(r *http.Request) orderdomain.OrderID {
	return orderdomain.OrderID(r.PathValue("id"))
}

// decodeJSON 解析请求体：限制大小、拒绝空 body、未知字段与尾随内容。
func decodeJSON(w http.ResponseWriter, r *http.Request, dst any) error {
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBodyBytes)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return err
	}

	// 再读一次，确保 body 中只有一个 JSON 值；若还能解出内容或遇到非 EOF 错误，都视为非法请求。
	var extra struct{}
	if err := dec.Decode(&extra); err != io.EOF {
		return errBadRequest
	}
	return nil
}
