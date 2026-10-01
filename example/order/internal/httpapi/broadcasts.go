package httpapi

import (
	"encoding/json"
	"net/http"

	"github.com/dirac-lee/domkit/example/order/internal/order/messenger"
)

// broadcastDTO 单条对外广播消息出参。
// Envelope 用 json.RawMessage 承载信封原文：外层序列化时原样嵌入为嵌套 JSON，
// 不会被转义成字符串。
type broadcastDTO struct {
	Topic      string          `json:"topic"`
	SenderCode string          `json:"senderCode"`
	Envelope   json.RawMessage `json:"envelope"`
}

// broadcasts 返回指定订单已发送的对外广播消息（本演示中由支付触发）。
func (h *orderHandler) broadcasts(w http.ResponseWriter, r *http.Request) {
	// 信使按字符串聚合 ID 建索引，路径强类型 ID 需归一为 string。
	records, err := h.app.Broadcasts.ByAggregate(r.Context(), string(pathOrderID(r)))
	if err != nil {
		writeError(w, err)
		return
	}
	writeOK(w, http.StatusOK, toBroadcastDTOs(records))
}

// toBroadcastDTOs 把信使记录转为出参切片；无消息时返回空切片（输出 [] 而非 null）。
func toBroadcastDTOs(records []messenger.Record) []broadcastDTO {
	items := make([]broadcastDTO, 0, len(records))
	for _, rec := range records {
		items = append(items, broadcastDTO{
			Topic:      rec.Topic,
			SenderCode: rec.SenderCode,
			Envelope:   json.RawMessage(rec.Envelope),
		})
	}
	return items
}
