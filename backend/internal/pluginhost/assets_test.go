//go:build unit

package pluginhost

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pluginkit"
	"github.com/Wei-Shaw/sub2api/internal/pluginkit/kittest"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// newAssetsFixture 构造只做资产服务的 Supervisor（不 Start：资产面不依赖
// 能力服务与状态订阅，索引经 NotifyInstalled 直填）。
func newAssetsFixture(t *testing.T) (*Supervisor, pluginkit.StateStore, *memInstallStore) {
	t.Helper()
	states := kittest.NewMemoryStateStore()
	installs := newMemInstallStore()
	s := NewSupervisor(SupervisorDeps{
		Installs: installs,
		States:   states,
		Logger:   muteLogger(t),
	})
	return s, states, installs
}

// installFrontendPlugin 落一份带前端资产的安装：entry + locale 声明文件，
// 外加一个未声明文件与清单本体（都不得被服务）。
func installFrontendPlugin(t *testing.T, s *Supervisor, installs *memInstallStore, id pluginkit.ID) *Installation {
	t.Helper()
	dir := t.TempDir()
	files := map[string]string{
		"webapp/plugin.js":  "(function(){/* entry */})()\n",
		"webapp/zh.json":    `{"nav":"演示"}`,
		"webapp/secret.js":  "// declared nowhere, must 404\n",
		"webapp/inner.txt":  "wrong extension\n",
		"manifest.json":     `{"leak":"no"}`,
		"webapp/notes.json": `{"leak":"undeclared json"}`,
	}
	for rel, body := range files {
		full := filepath.Join(dir, filepath.FromSlash(rel))
		require.NoError(t, os.MkdirAll(filepath.Dir(full), 0o755))
		require.NoError(t, os.WriteFile(full, []byte(body), 0o644))
	}

	inst := &Installation{
		ID:      id,
		Version: "1.0.0",
		Manifest: &Manifest{
			ID: id, Name: "Frontend", Version: "1.0.0", Protocol: ProtocolHTTP1,
			Frontend: &FrontendSpec{
				Entry:   "webapp/plugin.js",
				Locales: map[string]string{"zh": "webapp/zh.json"},
			},
		},
		InstallPath: dir,
	}
	require.NoError(t, installs.Upsert(context.Background(), inst))
	s.NotifyInstalled(inst)
	return inst
}

// assetsEngine 挂资产路由（对齐宿主路由形状）。
func assetsEngine(s *Supervisor) *gin.Engine {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.GET("/api/v1/plugins/:id/assets/*asset", s.ServeAsset)
	return engine
}

func getAsset(engine *gin.Engine, path string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	engine.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
	return w
}

// TestServeAssetGatingAndWhitelist 资产服务安全边界：
// enabled 门控 fail-closed、仅清单声明文件、Content-Type 白名单、防穿越。
func TestServeAssetGatingAndWhitelist(t *testing.T) {
	s, states, installs := newAssetsFixture(t)
	installFrontendPlugin(t, s, installs, "ext.web")
	engine := assetsEngine(s)
	ctx := context.Background()

	const entryURL = "/api/v1/plugins/ext.web/assets/webapp/plugin.js"

	// 未启用 → 404（与不存在同形）
	require.Equal(t, http.StatusNotFound, getAsset(engine, entryURL).Code)
	// 未安装 ID → 404
	require.Equal(t, http.StatusNotFound,
		getAsset(engine, "/api/v1/plugins/ext.ghost/assets/webapp/plugin.js").Code)

	require.NoError(t, states.SetEnabled(ctx, "ext.web", true, "test"))

	// entry：200 + 白名单 Content-Type + nosniff + no-cache
	w := getAsset(engine, entryURL)
	require.Equal(t, http.StatusOK, w.Code)
	require.Equal(t, "text/javascript; charset=utf-8", w.Header().Get("Content-Type"))
	require.Equal(t, "nosniff", w.Header().Get("X-Content-Type-Options"))
	require.Equal(t, "no-cache", w.Header().Get("Cache-Control"))
	require.Contains(t, w.Body.String(), "entry")

	// locale：200 + JSON Content-Type
	w = getAsset(engine, "/api/v1/plugins/ext.web/assets/webapp/zh.json")
	require.Equal(t, http.StatusOK, w.Code)
	require.Equal(t, "application/json; charset=utf-8", w.Header().Get("Content-Type"))

	// 未声明的包内文件（即使扩展名合法）→ 404
	for _, path := range []string{
		"/api/v1/plugins/ext.web/assets/webapp/secret.js",
		"/api/v1/plugins/ext.web/assets/webapp/notes.json",
		"/api/v1/plugins/ext.web/assets/manifest.json",
		"/api/v1/plugins/ext.web/assets/webapp/inner.txt",
	} {
		require.Equal(t, http.StatusNotFound, getAsset(engine, path).Code, "path=%s", path)
	}

	// 路径穿越 → 404（清单声明白名单天然拒绝）
	for _, path := range []string{
		"/api/v1/plugins/ext.web/assets/webapp/../manifest.json",
		"/api/v1/plugins/ext.web/assets/../../../etc/passwd",
		"/api/v1/plugins/ext.web/assets/",
	} {
		require.Equal(t, http.StatusNotFound, getAsset(engine, path).Code, "path=%s", path)
	}

	// disable → fail-closed
	require.NoError(t, states.SetEnabled(ctx, "ext.web", false, "test"))
	require.Equal(t, http.StatusNotFound, getAsset(engine, entryURL).Code)
}

// TestServeAssetNoFrontendPlugin 无前端声明的插件：资产面一律 404。
func TestServeAssetNoFrontendPlugin(t *testing.T) {
	s, states, installs := newAssetsFixture(t)
	inst := &Installation{
		ID:      "ext.backend",
		Version: "1.0.0",
		Manifest: &Manifest{
			ID: "ext.backend", Name: "Backend Only", Version: "1.0.0", Protocol: ProtocolHTTP1,
			Backend: &BackendSpec{Executables: map[string]string{CurrentPlatform(): "bin/plugin"}},
		},
		InstallPath: t.TempDir(),
	}
	require.NoError(t, installs.Upsert(context.Background(), inst))
	s.NotifyInstalled(inst)
	require.NoError(t, states.SetEnabled(context.Background(), "ext.backend", true, "test"))

	w := getAsset(assetsEngine(s), "/api/v1/plugins/ext.backend/assets/bin/plugin")
	require.Equal(t, http.StatusNotFound, w.Code)
}

// TestAssetURLPath enabled 清单 assets 字段的构造。
func TestAssetURLPath(t *testing.T) {
	require.Equal(t, "/api/v1/plugins/demo-external/assets/webapp/plugin.js",
		AssetURLPath("demo-external", "webapp/plugin.js"))
}
