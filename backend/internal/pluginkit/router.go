package pluginkit

import "github.com/gin-gonic/gin"

// Side 区分插件 API 暴露的鉴权面。
type Side string

const (
	// SideAdmin 管理员面：对外路径 /api/v1/admin/plugins/:id/api/*path，套 AdminAuth+合规守卫。
	SideAdmin Side = "admin"
	// SideUser 用户面：对外路径 /api/v1/plugins/:id/api/*path，套用户认证。
	SideUser Side = "user"
)

// Router 是插件可见的受限路由面。Admin()/User() 返回该插件私有子路由器，
// 插件只注册相对路径（如 r.Admin().GET("/hello", ...)）；
// 对外暴露、鉴权中间件与启停门控全部由宿主分发器完成，插件无法绕过。
type Router interface {
	Admin() gin.IRouter
	User() gin.IRouter
}
