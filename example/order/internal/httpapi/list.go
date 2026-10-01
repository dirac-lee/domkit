package httpapi

import (
	"net/http"
	"strconv"

	"github.com/dirac-lee/domkit/readmodel"
)

// defaultPageNumber 缺省页码：分页大小默认值改为读配置（app.Options.DefaultPageSize）。
const defaultPageNumber = 1

// list 列表/分页查询：直接走读模型副本，不扫描写模型。
func (h *orderHandler) list(w http.ResponseWriter, r *http.Request) {
	// 分页参数缺失时回退默认值；格式非法或越界时返回 400。
	pageNumber, err := queryInt(r, "page", defaultPageNumber)
	if err != nil {
		writeError(w, errBadRequest)
		return
	}
	pageSize, err := queryInt(r, "pageSize", h.app.Options.DefaultPageSize)
	if err != nil {
		writeError(w, errBadRequest)
		return
	}
	page, err := readmodel.NewPageRequest(
		pageNumber,
		pageSize,
	)
	if err != nil {
		writeError(w, errBadRequest)
		return
	}

	result, err := h.app.ReadModel.Page(r.Context(), page)
	if err != nil {
		writeError(w, err)
		return
	}
	writeOK(w, http.StatusOK, toPageResultDTO(result))
}

// queryInt 解析查询参数为 int；缺失时使用默认值，格式非法时返回错误。
func queryInt(r *http.Request, key string, fallback int) (int, error) {
	raw := r.URL.Query().Get(key)
	if raw == "" {
		return fallback, nil
	}
	v, err := strconv.Atoi(raw)
	if err != nil {
		return 0, err
	}
	return v, nil
}
