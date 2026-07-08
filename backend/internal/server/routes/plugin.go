package routes

import (
	"github.com/Wei-Shaw/sub2api/internal/handler"
	"github.com/Wei-Shaw/sub2api/internal/handler/admin"
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/pluginhost"
	"github.com/Wei-Shaw/sub2api/internal/pluginkit"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"

	"github.com/gin-gonic/gin"
)

// pluginRouteDeps 收敛插件路由的可选依赖（外部插件层，phase-4）。
type pluginRouteDeps struct {
	installer  *pluginhost.Installer
	installs   pluginhost.InstallationStore
	supervisor *pluginhost.Supervisor
}

// PluginRouteOption 定制 RegisterPluginRoutes 的可选依赖。
type PluginRouteOption func(*pluginRouteDeps)

// WithExternalPluginLayer 注入外部插件层依赖：上传/卸载/config 端点、
// 外部插件分发与前端资产服务据此工作。未注入时管理端点仍注册但返回 503，
// 分发器与资产端点对外部 ID 一律 404（路由面保持稳定，便于路由 diff 守护）。
func WithExternalPluginLayer(installer *pluginhost.Installer, installs pluginhost.InstallationStore, supervisor *pluginhost.Supervisor) PluginRouteOption {
	return func(deps *pluginRouteDeps) {
		deps.installer = installer
		deps.installs = installs
		deps.supervisor = supervisor
	}
}

// RegisterPluginRoutes 注册插件系统的宿主侧路由：
//
//	GET    /api/v1/admin/plugins                插件状态快照
//	POST   /api/v1/admin/plugins/upload         上传 zip 安装/升级外部插件
//	DELETE /api/v1/admin/plugins/:id            卸载外部插件（仅外部层）
//	GET    /api/v1/admin/plugins/:id/config     读取外部插件配置
//	PUT    /api/v1/admin/plugins/:id/config     写入外部插件配置
//	POST   /api/v1/admin/plugins/:id/enable     免重启启用
//	POST   /api/v1/admin/plugins/:id/disable    免重启停用
//	ANY    /api/v1/admin/plugins/:id/api/*path  admin 侧分发器（转发到插件私有子路由器）
//	GET    /api/v1/plugins                      用户态 enabled 插件清单 [{id, assets?}]（前端门控用）
//	ANY    /api/v1/plugins/:id/api/*path        user 侧分发器
//	GET    /api/v1/plugins/:id/assets/*asset    外部插件前端资产（无用户认证，见 ServeAsset 注释）
//
// 插件 API 走 :id 分发器而非静态注册：鉴权中间件在宿主侧统一施加，
// 启停门控（enabled 且 running）在 Manager.Dispatch 内收口，
// 未知 ID / 未启用 / 无 API 面一律同一个 404。
//
// 外部插件分发裁决（phase-4）：已安装的外部 ID 先于内建 Manager 命中
// （安装器保证外部 ID 不会占用内建清单，两层不重叠），
// 其余请求落回 Manager.Dispatch（未知 ID 在其内收口为 404）。
func RegisterPluginRoutes(
	v1 *gin.RouterGroup,
	jwtAuth middleware.JWTAuthMiddleware,
	adminAuth middleware.AdminAuthMiddleware,
	settingService *service.SettingService,
	manager *pluginkit.Manager,
	states pluginkit.StateStore,
	opts ...PluginRouteOption,
) {
	deps := &pluginRouteDeps{}
	for _, opt := range opts {
		opt(deps)
	}
	pluginHandler := admin.NewPluginHandler(manager, states, deps.installer, deps.installs, deps.supervisor)
	enabledHandler := handler.NewPluginEnabledHandler(manager, deps.supervisor)

	// dispatch 是两层分发的统一入口：外部已安装 ID → Supervisor 反代，
	// 否则 → 内建 Manager（含未知 ID 的 404 收口）。
	dispatch := func(side pluginkit.Side, c *gin.Context) {
		id := c.Param("id")
		if deps.supervisor != nil && deps.supervisor.Handles(pluginkit.ID(id)) {
			deps.supervisor.Dispatch(side, id, c)
			return
		}
		manager.Dispatch(side, id, c)
	}

	// 管理端点与 admin 侧分发器：AdminAuth + 合规守卫（对齐 RegisterAdminRoutes）
	adminPlugins := v1.Group("/admin/plugins")
	adminPlugins.Use(gin.HandlerFunc(adminAuth))
	adminPlugins.Use(middleware.AdminComplianceGuard(settingService))
	{
		adminPlugins.GET("", pluginHandler.List)
		adminPlugins.POST("/upload", pluginHandler.Upload)
		adminPlugins.DELETE("/:id", pluginHandler.Uninstall)
		adminPlugins.GET("/:id/config", pluginHandler.GetConfig)
		adminPlugins.PUT("/:id/config", pluginHandler.PutConfig)
		adminPlugins.POST("/:id/enable", pluginHandler.Enable)
		adminPlugins.POST("/:id/disable", pluginHandler.Disable)
		adminPlugins.Any("/:id/api/*path", func(c *gin.Context) {
			dispatch(pluginkit.SideAdmin, c)
		})
	}

	// user 侧端点与分发器：JWT 认证 + 后台模式守卫（对齐 RegisterUserRoutes）
	userPlugins := v1.Group("/plugins")
	userPlugins.Use(gin.HandlerFunc(jwtAuth))
	userPlugins.Use(middleware.BackendModeUserGuard(settingService))
	{
		userPlugins.GET("", enabledHandler.ListEnabled)
		userPlugins.Any("/:id/api/*path", func(c *gin.Context) {
			dispatch(pluginkit.SideUser, c)
		})
	}

	// 外部插件前端资产：不挂用户认证（<script src> 无法携带 Authorization 头，
	// 等价于核心前端静态资源），门控收口在 ServeAsset（仅 enabled + 清单声明文件）。
	v1.GET("/plugins/:id/assets/*asset", func(c *gin.Context) {
		if deps.supervisor == nil {
			response.NotFound(c, "plugin asset not found")
			return
		}
		deps.supervisor.ServeAsset(c)
	})
}
