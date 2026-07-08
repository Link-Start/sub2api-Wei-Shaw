//go:build unit

package routes

import (
	"fmt"
	"net/http"
	"sort"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// pluginAnyMethods 对齐 gin RouterGroup.Any 注册的方法集合（gin 内部 anyMethods 未导出）。
var pluginAnyMethods = []string{
	http.MethodGet, http.MethodPost, http.MethodPut, http.MethodPatch,
	http.MethodHead, http.MethodOptions, http.MethodDelete, http.MethodConnect,
	http.MethodTrace,
}

// TestRegisterPluginRoutesRouteDiffGuard 是接合点的路由 diff 守护：
// RegisterPluginRoutes 注册后新增的路由必须恰为 phase-2 PHASE_PLAN §2.4 约定的 4 组端点
// （快照 / enable|disable / admin 分发器 / user 分发器）加 phase-3 PHASE_PLAN §2.5 的
// 用户态 enabled 清单端点，以及 phase-4 PHASE_PLAN §3.2 的外部插件层端点
// （upload / 卸载 / config 读写 / 前端资产），且不得移除任何既有路由，
// 防止未来改动在宿主路由面上静默增删插件端点。
func TestRegisterPluginRoutesRouteDiffGuard(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	v1 := engine.Group("/api/v1")

	// 预置与插件端点共享前缀的代表性既有路由：
	// gin 路由树冲突在注册时 panic，此处同时守护 static/param 兼容性。
	stub := func(c *gin.Context) { c.Status(http.StatusOK) }
	v1.GET("/admin/settings", stub)
	v1.GET("/admin/users/:id", stub)
	v1.GET("/user/profile", stub)
	v1.GET("/keys/:id", stub)

	before := pluginRouteKeys(engine)

	manager, states := newPluginRoutesManager(t)
	RegisterPluginRoutes(v1, pluginTestJWTAuth(2), pluginTestAdminAuth(1), nil, manager, states)

	after := pluginRouteKeys(engine)
	added := make([]string, 0, len(after))
	for key := range after {
		if !before[key] {
			added = append(added, key)
		}
	}

	expected := []string{
		"GET /api/v1/admin/plugins",
		"POST /api/v1/admin/plugins/upload",
		"DELETE /api/v1/admin/plugins/:id",
		"GET /api/v1/admin/plugins/:id/config",
		"PUT /api/v1/admin/plugins/:id/config",
		"POST /api/v1/admin/plugins/:id/enable",
		"POST /api/v1/admin/plugins/:id/disable",
		"GET /api/v1/plugins",
		"GET /api/v1/plugins/:id/assets/*asset",
	}
	for _, method := range pluginAnyMethods {
		expected = append(expected,
			fmt.Sprintf("%s /api/v1/admin/plugins/:id/api/*path", method),
			fmt.Sprintf("%s /api/v1/plugins/:id/api/*path", method),
		)
	}

	sort.Strings(added)
	sort.Strings(expected)
	require.Equal(t, expected, added, "插件路由 diff 与约定端点不一致")

	for key := range before {
		require.True(t, after[key], "既有路由 %s 不应被移除", key)
	}
}

// pluginRouteKeys 把路由表折叠为 "METHOD PATH" 集合，便于 diff。
func pluginRouteKeys(engine *gin.Engine) map[string]bool {
	routes := engine.Routes()
	keys := make(map[string]bool, len(routes))
	for _, r := range routes {
		keys[r.Method+" "+r.Path] = true
	}
	return keys
}
