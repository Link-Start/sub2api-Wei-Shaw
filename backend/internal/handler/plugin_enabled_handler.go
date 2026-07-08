package handler

import (
	"sort"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/pluginhost"
	"github.com/Wei-Shaw/sub2api/internal/pluginkit"

	"github.com/gin-gonic/gin"
)

// EnabledPlugin 是用户态清单里的一项（phase-4 决策 6 的契约）：
// 外部插件带前端资产时 assets 指向其运行时入口
// （/api/v1/plugins/:id/assets/<entry>）；内建插件与无前端的外部插件
// 无 assets 字段。
type EnabledPlugin struct {
	ID     string `json:"id"`
	Assets string `json:"assets,omitempty"`
}

// PluginEnabledHandler 提供用户态的已启用插件清单端点。
// 与 admin 侧快照端点不同：仅透出 enabled 的插件 ID（及可选 assets 入口），
// 不泄露 state/error/started_at 等内部运行信息，供前端做插件门控与运行时加载。
type PluginEnabledHandler struct {
	manager *pluginkit.Manager
	// supervisor 是外部插件层依赖，未装配时为 nil（清单只含内建插件）。
	supervisor *pluginhost.Supervisor
}

// NewPluginEnabledHandler 创建用户态插件清单处理器（supervisor 允许为 nil）。
func NewPluginEnabledHandler(manager *pluginkit.Manager, supervisor *pluginhost.Supervisor) *PluginEnabledHandler {
	return &PluginEnabledHandler{manager: manager, supervisor: supervisor}
}

// ListEnabled 返回已启用插件的清单：data 为 [{id, assets?}]，
// 内建（Manager）与外部（Supervisor）合并后按 ID 稳定排序。
// GET /api/v1/plugins
//
// 无启用插件时返回空数组而非 null。
func (h *PluginEnabledHandler) ListEnabled(c *gin.Context) {
	snapshot := h.manager.Snapshot()
	items := make([]EnabledPlugin, 0, len(snapshot))
	for _, st := range snapshot {
		if st.Enabled {
			items = append(items, EnabledPlugin{ID: string(st.ID)})
		}
	}
	if h.supervisor != nil {
		for _, ep := range h.supervisor.EnabledExternalPlugins() {
			items = append(items, EnabledPlugin{ID: string(ep.ID), Assets: ep.Assets})
		}
	}
	sort.Slice(items, func(i, j int) bool { return items[i].ID < items[j].ID })
	response.Success(c, items)
}
