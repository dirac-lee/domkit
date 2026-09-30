package httpapi

import (
	"net/http"
	"sort"

	"github.com/dirac-lee/domkit/reconciliation"
)

// reconciliationItemDTO 单个副本的对账结果出参。
type reconciliationItemDTO struct {
	Replica      string `json:"replica"`      // 副本逻辑 ID
	Status       string `json:"status"`       // 一致性状态：CONSISTENT/STALE/ORPHAN/UNTRACKED
	ReadVersion  int64  `json:"readVersion"`  // 读副本版本 V'
	WriteVersion int64  `json:"writeVersion"` // 写模型版本 V
}

// reconcile 对指定订单的全部副本做「检测 + 自愈」，返回每副本对账结果。
// 事件投影通常已让副本保持同步，正常情况下各副本均为 CONSISTENT。
func (h *orderHandler) reconcile(w http.ResponseWriter, r *http.Request) {
	results, err := h.app.Reconciler.Reconcile(r.Context(), pathOrderID(r))
	if err != nil {
		writeError(w, err)
		return
	}
	writeOK(w, http.StatusOK, toReconciliationItems(results))
}

// toReconciliationItems 把「副本ID → 对账结果」的 map 转为有序切片：
// map 遍历顺序随机，按副本 ID 排序保证接口输出稳定、可断言。
func toReconciliationItems(results map[string]reconciliation.Reconciliation) []reconciliationItemDTO {
	replicaIDs := make([]string, 0, len(results))
	for id := range results {
		replicaIDs = append(replicaIDs, id)
	}
	sort.Strings(replicaIDs)

	items := make([]reconciliationItemDTO, 0, len(replicaIDs))
	for _, id := range replicaIDs {
		r := results[id]
		items = append(items, reconciliationItemDTO{
			Replica:      id,
			Status:       r.Status.String(),
			ReadVersion:  r.ReadVersion,
			WriteVersion: r.WriteVersion,
		})
	}
	return items
}
