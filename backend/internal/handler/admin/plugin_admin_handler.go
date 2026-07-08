package admin

import (
	"fmt"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/pluginkit"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"

	"github.com/gin-gonic/gin"
)

// PluginHandler 提供插件运行时的管理端点：状态快照查询与免重启启停。
// 启停的唯一事实源是 DB（经 StateStore 写入），Manager 经订阅回调驱动生命周期，
// 本 handler 只做参数校验与状态透出，不直接操作插件实例。
type PluginHandler struct {
	manager *pluginkit.Manager
	states  pluginkit.StateStore
}

// NewPluginHandler 创建插件管理处理器。
func NewPluginHandler(manager *pluginkit.Manager, states pluginkit.StateStore) *PluginHandler {
	return &PluginHandler{manager: manager, states: states}
}

// List 返回全部注册插件的状态快照（按 ID 稳定排序）。
func (h *PluginHandler) List(c *gin.Context) {
	response.Success(c, h.manager.Snapshot())
}

// Enable 启用插件（免重启）。未注册的 ID 返回 404；重复启用幂等返回 200。
func (h *PluginHandler) Enable(c *gin.Context) {
	h.setEnabled(c, true)
}

// Disable 停用插件（免重启）。未注册的 ID 返回 404；重复停用幂等返回 200。
func (h *PluginHandler) Disable(c *gin.Context) {
	h.setEnabled(c, false)
}

// setEnabled 校验插件已注册后写入启停状态，并回传该插件的最新状态快照。
// SetEnabled 本身幂等（DB upsert + Manager 幂等 start/stop），重复设置同值返回 200。
func (h *PluginHandler) setEnabled(c *gin.Context, enabled bool) {
	id := pluginkit.ID(c.Param("id"))
	if _, ok := h.statusOf(id); !ok {
		response.NotFound(c, "plugin not found")
		return
	}
	subject, ok := middleware.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	if err := h.states.SetEnabled(c.Request.Context(), id, enabled, fmt.Sprintf("admin:%d", subject.UserID)); err != nil {
		response.ErrorFrom(c, err)
		return
	}
	// 本实例的生效不依赖广播链路，SetEnabled 返回后快照即反映最新生命周期状态。
	status, _ := h.statusOf(id)
	response.Success(c, status)
}

// statusOf 从 Manager 快照中取指定插件的状态；不存在（未注册）返回 false。
func (h *PluginHandler) statusOf(id pluginkit.ID) (pluginkit.PluginStatus, bool) {
	for _, st := range h.manager.Snapshot() {
		if st.ID == id {
			return st, true
		}
	}
	return pluginkit.PluginStatus{}, false
}
