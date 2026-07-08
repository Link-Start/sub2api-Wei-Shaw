package handler

import (
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/pluginkit"

	"github.com/gin-gonic/gin"
)

// PluginEnabledHandler 提供用户态的已启用插件清单端点。
// 与 admin 侧快照端点不同：仅透出 enabled 的插件 ID 列表，
// 不泄露 state/error/started_at 等内部运行信息，供前端做插件门控。
type PluginEnabledHandler struct {
	manager *pluginkit.Manager
}

// NewPluginEnabledHandler 创建用户态插件清单处理器。
func NewPluginEnabledHandler(manager *pluginkit.Manager) *PluginEnabledHandler {
	return &PluginEnabledHandler{manager: manager}
}

// ListEnabled 返回已启用插件的 ID 列表。
// GET /api/v1/plugins
//
// 排序稳定性由 Manager.Snapshot 保证（按 ID 稳定排序）；
// 无启用插件时返回空数组而非 null。
func (h *PluginEnabledHandler) ListEnabled(c *gin.Context) {
	snapshot := h.manager.Snapshot()
	ids := make([]string, 0, len(snapshot))
	for _, st := range snapshot {
		if st.Enabled {
			ids = append(ids, string(st.ID))
		}
	}
	response.Success(c, ids)
}
