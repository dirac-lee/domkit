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
	// 分页参数缺失或非法时回退默认值；构造分页请求时再做边界校验。
	page, err := readmodel.NewPageRequest(
		queryInt(r, "page", defaultPageNumber),
		queryInt(r, "pageSize", h.app.Options.DefaultPageSize),
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

// queryInt 解析查询参数为 int；缺失或非法时回退默认值。
func queryInt(r *http.Request, key string, fallback int) int {
	v, err := strconv.Atoi(r.URL.Query().Get(key))
	if err != nil {
		return fallback
	}
	return v
}
