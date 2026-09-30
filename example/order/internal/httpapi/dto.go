package httpapi

import (
	"github.com/dirac-lee/domkit/example/order/internal/order/domain"
	"github.com/dirac-lee/domkit/readmodel"
)

// orderDTO 订单出参对象（写模型）。
type orderDTO struct {
	ID      string `json:"id"`
	OrderNo string `json:"orderNo"`
	Amount  int64  `json:"amount"`
	Status  string `json:"status"`
	Version uint64 `json:"version"`
}

// toOrderDTO 聚合 → DTO，强类型主键降级为普通 string，枚举降为字符串编码。
func toOrderDTO(o *domain.Order) orderDTO {
	return orderDTO{
		ID:      string(o.ID),
		OrderNo: o.OrderNo,
		Amount:  o.Amount,
		Status:  o.Status.Value(),
		Version: o.Version,
	}
}

// orderSummaryDTO 订单概要出参对象（读模型）。
type orderSummaryDTO struct {
	ID      string `json:"id"`
	OrderNo string `json:"orderNo"`
	Status  string `json:"status"`
	Amount  int64  `json:"amount"`
	Version uint64 `json:"version"`
}

// toSummaryDTO 读模型投影 → DTO。
func toSummaryDTO(s domain.OrderSummary) orderSummaryDTO {
	return orderSummaryDTO{
		ID:      string(s.ID),
		OrderNo: s.OrderNo,
		Status:  s.Status,
		Amount:  s.Amount,
		Version: s.Version,
	}
}

// pageResultDTO 分页响应体：当前页数据 + 总条数 + 分页回显。
type pageResultDTO struct {
	List     []orderSummaryDTO `json:"list"`
	Total    int64             `json:"total"`
	Page     int               `json:"page"`
	PageSize int               `json:"pageSize"`
}

// toPageResultDTO 分页结果 → 响应 DTO，逐条把投影转为概要 DTO。
func toPageResultDTO(result readmodel.PageResult[domain.OrderSummary]) pageResultDTO {
	items := make([]orderSummaryDTO, 0, len(result.Data()))
	for _, s := range result.Data() {
		items = append(items, toSummaryDTO(s))
	}
	return pageResultDTO{
		List:     items,
		Total:    result.TotalCount(),
		Page:     result.Request().PageNumber(),
		PageSize: result.Request().PageSize(),
	}
}
