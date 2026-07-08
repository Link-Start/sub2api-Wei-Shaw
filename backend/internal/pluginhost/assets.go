package pluginhost

import (
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/pluginkit"

	"github.com/gin-gonic/gin"
)

// assetContentTypes 是前端资产的 Content-Type 白名单（按扩展名）；
// 名单外的文件一律不服务，配合 nosniff 防止插件包内容被当作任意资源执行。
var assetContentTypes = map[string]string{
	".js":   "text/javascript; charset=utf-8",
	".mjs":  "text/javascript; charset=utf-8",
	".css":  "text/css; charset=utf-8",
	".json": "application/json; charset=utf-8",
	".map":  "application/json; charset=utf-8",
}

// AssetURLPath 构造插件前端资产的对外 URL 路径（enabled 清单的 assets 字段）。
func AssetURLPath(id pluginkit.ID, entry string) string {
	return "/api/v1/plugins/" + string(id) + "/assets/" + entry
}

// ServeAsset 服务外部插件的前端静态资产：
// GET /api/v1/plugins/:id/assets/*asset
//
// 安全边界（phase-4 §3.1 assets 契约）：
//   - 仅 enabled 的已安装插件（fail-closed，未启用与不存在同一个 404）；
//   - 仅服务 manifest.frontend 显式声明的文件（entry + locales），
//     声明路径已在清单校验中强制为包内干净相对路径，天然防穿越；
//   - Content-Type 按扩展名白名单显式设置并加 nosniff；
//   - 该端点不挂用户认证：前端经 <script src> 注入运行时入口，
//     浏览器脚本请求无法携带 Authorization 头（等价于核心前端静态资源）。
func (s *Supervisor) ServeAsset(c *gin.Context) {
	id := pluginkit.ID(c.Param("id"))
	inst, ok := s.InstallationOf(id)
	if !ok || inst.Manifest == nil || inst.Manifest.Frontend == nil || !s.states.Enabled(id) {
		response.NotFound(c, "plugin asset not found")
		return
	}

	rel := strings.TrimPrefix(c.Param("asset"), "/")
	if !assetDeclared(inst.Manifest.Frontend, rel) {
		response.NotFound(c, "plugin asset not found")
		return
	}
	ctype, ok := assetContentTypes[strings.ToLower(path.Ext(rel))]
	if !ok {
		response.NotFound(c, "plugin asset not found")
		return
	}

	full := filepath.Join(inst.InstallPath, filepath.FromSlash(rel))
	info, err := os.Stat(full)
	if err != nil || info.IsDir() {
		response.NotFound(c, "plugin asset not found")
		return
	}

	c.Header("Content-Type", ctype)
	c.Header("X-Content-Type-Options", "nosniff")
	// 资产 URL 不含版本号，升级后必须回源校验（http.ServeFile 的
	// Last-Modified 条件请求仍可命中 304）。
	c.Header("Cache-Control", "no-cache")
	c.File(full)
}

// assetDeclared 检查路径是否为 manifest.frontend 显式声明的资产。
func assetDeclared(f *FrontendSpec, rel string) bool {
	if rel == "" {
		return false
	}
	if rel == f.Entry {
		return true
	}
	for _, locale := range f.Locales {
		if rel == locale {
			return true
		}
	}
	return false
}
