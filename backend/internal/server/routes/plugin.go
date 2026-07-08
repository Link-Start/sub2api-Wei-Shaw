package routes

import (
	"github.com/Wei-Shaw/sub2api/internal/handler/admin"
	"github.com/Wei-Shaw/sub2api/internal/pluginkit"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"

	"github.com/gin-gonic/gin"
)

// RegisterPluginRoutes 注册插件系统的宿主侧路由：
//
//	GET  /api/v1/admin/plugins                插件状态快照
//	POST /api/v1/admin/plugins/:id/enable     免重启启用
//	POST /api/v1/admin/plugins/:id/disable    免重启停用
//	ANY  /api/v1/admin/plugins/:id/api/*path  admin 侧分发器（转发到插件私有子路由器）
//	ANY  /api/v1/plugins/:id/api/*path        user 侧分发器
//
// 插件 API 走 :id 分发器而非静态注册：鉴权中间件在宿主侧统一施加，
// 启停门控（enabled 且 running）在 Manager.Dispatch 内收口，
// 未知 ID / 未启用 / 无 API 面一律同一个 404。
func RegisterPluginRoutes(
	v1 *gin.RouterGroup,
	jwtAuth middleware.JWTAuthMiddleware,
	adminAuth middleware.AdminAuthMiddleware,
	settingService *service.SettingService,
	manager *pluginkit.Manager,
	states pluginkit.StateStore,
) {
	pluginHandler := admin.NewPluginHandler(manager, states)

	// 管理端点与 admin 侧分发器：AdminAuth + 合规守卫（对齐 RegisterAdminRoutes）
	adminPlugins := v1.Group("/admin/plugins")
	adminPlugins.Use(gin.HandlerFunc(adminAuth))
	adminPlugins.Use(middleware.AdminComplianceGuard(settingService))
	{
		adminPlugins.GET("", pluginHandler.List)
		adminPlugins.POST("/:id/enable", pluginHandler.Enable)
		adminPlugins.POST("/:id/disable", pluginHandler.Disable)
		adminPlugins.Any("/:id/api/*path", func(c *gin.Context) {
			manager.Dispatch(pluginkit.SideAdmin, c.Param("id"), c)
		})
	}

	// user 侧分发器：JWT 认证 + 后台模式守卫（对齐 RegisterUserRoutes）
	userPlugins := v1.Group("/plugins")
	userPlugins.Use(gin.HandlerFunc(jwtAuth))
	userPlugins.Use(middleware.BackendModeUserGuard(settingService))
	{
		userPlugins.Any("/:id/api/*path", func(c *gin.Context) {
			manager.Dispatch(pluginkit.SideUser, c.Param("id"), c)
		})
	}
}
