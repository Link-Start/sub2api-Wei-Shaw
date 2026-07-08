//go:build unit

package routes

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/handler"
	"github.com/Wei-Shaw/sub2api/internal/pluginhost"
	"github.com/Wei-Shaw/sub2api/internal/pluginkit"
	"github.com/Wei-Shaw/sub2api/internal/pluginkit/kittest"
	"github.com/Wei-Shaw/sub2api/internal/plugins"
	"github.com/Wei-Shaw/sub2api/internal/plugins/demo"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// newPluginRoutesManager 用真实 Builtin 清单（含 demo 插件）与内存 StateStore
// 构造 Manager（未 Bootstrap，由调用方按需驱动）。
func newPluginRoutesManager(t *testing.T) (*pluginkit.Manager, pluginkit.StateStore) {
	t.Helper()
	states := kittest.NewMemoryStateStore()
	host := pluginkit.HostDeps{Logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	manager, err := pluginkit.NewManager(host, states, nil, plugins.Builtin())
	require.NoError(t, err)
	return manager, states
}

// pluginTestAdminAuth 是 AdminAuth 的测试替身，写入与真实中间件一致的上下文键。
func pluginTestAdminAuth(userID int64) middleware.AdminAuthMiddleware {
	return middleware.AdminAuthMiddleware(func(c *gin.Context) {
		c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: userID})
		c.Set(string(middleware.ContextKeyUserRole), "admin")
		c.Next()
	})
}

// pluginTestJWTAuth 是 JWTAuth 的测试替身。
func pluginTestJWTAuth(userID int64) middleware.JWTAuthMiddleware {
	return middleware.JWTAuthMiddleware(func(c *gin.Context) {
		c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: userID})
		c.Set(string(middleware.ContextKeyUserRole), "user")
		c.Next()
	})
}

// newPluginIntegrationRouter 构造贴近生产接线的最小全链路：
// 真实 Manager（已 Bootstrap）+ 内存 StateStore + RegisterPluginRoutes。
// settingService 传 nil：合规/后台模式守卫按实现放行，等价于常规运行态。
func newPluginIntegrationRouter(t *testing.T) (*gin.Engine, *pluginkit.Manager, pluginkit.StateStore) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	manager, states := newPluginRoutesManager(t)
	require.NoError(t, manager.Bootstrap(context.Background()))
	t.Cleanup(func() { _ = manager.StopAll(context.Background()) })

	engine := gin.New()
	v1 := engine.Group("/api/v1")
	RegisterPluginRoutes(v1, pluginTestJWTAuth(2), pluginTestAdminAuth(1), nil, manager, states)
	return engine, manager, states
}

func doPluginRequest(t *testing.T, engine *gin.Engine, method, path string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, nil)
	w := httptest.NewRecorder()
	engine.ServeHTTP(w, req)
	return w
}

// decodePluginData 解开 response.Success 的响应包裹并反序列化 data 字段。
func decodePluginData(t *testing.T, body []byte, out any) {
	t.Helper()
	var envelope struct {
		Code int             `json:"code"`
		Data json.RawMessage `json:"data"`
	}
	require.NoError(t, json.Unmarshal(body, &envelope))
	require.Equal(t, 0, envelope.Code)
	require.NoError(t, json.Unmarshal(envelope.Data, out))
}

// TestPluginRoutesEnableDisableFullChain 全链路演练免重启启停：
// 快照 → 未启用 404 → enable → 插件 API 200 → 幂等 enable → disable → 404 → 终态快照。
func TestPluginRoutesEnableDisableFullChain(t *testing.T) {
	engine, manager, _ := newPluginIntegrationRouter(t)

	// 初始快照：demo 已注册、未启用
	w := doPluginRequest(t, engine, http.MethodGet, "/api/v1/admin/plugins")
	require.Equal(t, http.StatusOK, w.Code)
	var list []pluginkit.PluginStatus
	decodePluginData(t, w.Body.Bytes(), &list)
	require.Len(t, list, 1)
	require.Equal(t, demo.PluginID, list[0].ID)
	require.False(t, list[0].Enabled)
	require.Equal(t, pluginkit.StateDisabled, list[0].State)

	// 未启用 → admin 分发器 404
	w = doPluginRequest(t, engine, http.MethodGet, "/api/v1/admin/plugins/demo/api/hello")
	require.Equal(t, http.StatusNotFound, w.Code)

	// enable → 200 且状态 running（内存 StateStore 回调同步交付，返回即收敛）
	w = doPluginRequest(t, engine, http.MethodPost, "/api/v1/admin/plugins/demo/enable")
	require.Equal(t, http.StatusOK, w.Code)
	var status pluginkit.PluginStatus
	decodePluginData(t, w.Body.Bytes(), &status)
	require.True(t, status.Enabled)
	require.Equal(t, pluginkit.StateRunning, status.State)
	require.NotNil(t, status.StartedAt)
	require.True(t, manager.GateAllows(demo.PluginID))

	// 分发器放行插件 API
	w = doPluginRequest(t, engine, http.MethodGet, "/api/v1/admin/plugins/demo/api/hello")
	require.Equal(t, http.StatusOK, w.Code)
	require.Contains(t, w.Body.String(), "hello from demo plugin")

	// 重复 enable → 幂等 200，仍 running
	w = doPluginRequest(t, engine, http.MethodPost, "/api/v1/admin/plugins/demo/enable")
	require.Equal(t, http.StatusOK, w.Code)
	decodePluginData(t, w.Body.Bytes(), &status)
	require.Equal(t, pluginkit.StateRunning, status.State)

	// disable → 200 且状态 stopped
	w = doPluginRequest(t, engine, http.MethodPost, "/api/v1/admin/plugins/demo/disable")
	require.Equal(t, http.StatusOK, w.Code)
	decodePluginData(t, w.Body.Bytes(), &status)
	require.False(t, status.Enabled)
	require.Equal(t, pluginkit.StateStopped, status.State)

	// 停用后分发器关闭
	w = doPluginRequest(t, engine, http.MethodGet, "/api/v1/admin/plugins/demo/api/hello")
	require.Equal(t, http.StatusNotFound, w.Code)

	// 终态快照：无错误残留、StartedAt 清零、门控关闭
	snap := manager.Snapshot()
	require.Len(t, snap, 1)
	require.False(t, snap[0].Enabled)
	require.Equal(t, pluginkit.StateStopped, snap[0].State)
	require.Empty(t, snap[0].Err)
	require.Nil(t, snap[0].StartedAt)
	require.False(t, manager.GateAllows(demo.PluginID))
}

// TestPluginRoutesUnknownID 未注册 ID：启停端点与分发器一律 404。
func TestPluginRoutesUnknownID(t *testing.T) {
	engine, _, _ := newPluginIntegrationRouter(t)

	for _, path := range []string{
		"/api/v1/admin/plugins/nope/enable",
		"/api/v1/admin/plugins/nope/disable",
	} {
		w := doPluginRequest(t, engine, http.MethodPost, path)
		require.Equal(t, http.StatusNotFound, w.Code, "path=%s", path)
	}

	w := doPluginRequest(t, engine, http.MethodGet, "/api/v1/admin/plugins/nope/api/hello")
	require.Equal(t, http.StatusNotFound, w.Code)
}

// TestPluginRoutesUserDispatcher demo 只挂 admin 面：
// user 分发器即使在插件 running 时也命中其私有子路由器的 NoRoute → 404。
func TestPluginRoutesUserDispatcher(t *testing.T) {
	engine, _, states := newPluginIntegrationRouter(t)
	require.NoError(t, states.SetEnabled(context.Background(), demo.PluginID, true, "test"))

	w := doPluginRequest(t, engine, http.MethodGet, "/api/v1/plugins/demo/api/hello")
	require.Equal(t, http.StatusNotFound, w.Code)
}

// fetchEnabledPluginIDs 请求用户态 enabled 清单端点（契约 [{id, assets?}]）并抽出 id 列表。
func fetchEnabledPluginIDs(t *testing.T, engine *gin.Engine) []string {
	t.Helper()
	w := doPluginRequest(t, engine, http.MethodGet, "/api/v1/plugins")
	require.Equal(t, http.StatusOK, w.Code)
	var items []handler.EnabledPlugin
	decodePluginData(t, w.Body.Bytes(), &items)
	ids := make([]string, 0, len(items))
	for _, item := range items {
		ids = append(ids, item.ID)
	}
	return ids
}

// TestPluginRoutesUserEnabledList 用户态 enabled 清单端点全链路：
// 初始空数组（非 null）→ enable 后仅含 demo 且不泄露内部状态字段 → disable 后回到空。
// 契约为 phase-4 决策 6 的 [{id, assets?}]：内建插件无 assets 字段。
func TestPluginRoutesUserEnabledList(t *testing.T) {
	engine, _, states := newPluginIntegrationRouter(t)
	ctx := context.Background()

	// 初始：demo 未启用 → 空数组
	require.Equal(t, []string{}, fetchEnabledPluginIDs(t, engine))

	// enable → 仅含 demo；载荷是 [{id}] 对象列表，不泄露 state/error/started_at，
	// 内建插件也不携带 assets 字段
	require.NoError(t, states.SetEnabled(ctx, demo.PluginID, true, "test"))
	require.Equal(t, []string{string(demo.PluginID)}, fetchEnabledPluginIDs(t, engine))
	w := doPluginRequest(t, engine, http.MethodGet, "/api/v1/plugins")
	require.Contains(t, w.Body.String(), `"id":"`+string(demo.PluginID)+`"`)
	require.NotContains(t, w.Body.String(), "assets")
	require.NotContains(t, w.Body.String(), "state")
	require.NotContains(t, w.Body.String(), "started_at")

	// disable → 回到空数组
	require.NoError(t, states.SetEnabled(ctx, demo.PluginID, false, "test"))
	require.Equal(t, []string{}, fetchEnabledPluginIDs(t, engine))
}

// TestPluginRoutesUserEnabledListUnauthorized 未登录（JWT 中间件拒绝）→ 401：
// 守护该端点必须挂在用户组认证中间件之后。
func TestPluginRoutesUserEnabledListUnauthorized(t *testing.T) {
	gin.SetMode(gin.TestMode)
	manager, states := newPluginRoutesManager(t)

	engine := gin.New()
	v1 := engine.Group("/api/v1")
	reject := middleware.JWTAuthMiddleware(func(c *gin.Context) {
		c.AbortWithStatus(http.StatusUnauthorized)
	})
	RegisterPluginRoutes(v1, reject, pluginTestAdminAuth(1), nil, manager, states)

	w := doPluginRequest(t, engine, http.MethodGet, "/api/v1/plugins")
	require.Equal(t, http.StatusUnauthorized, w.Code)
}

// fakeListPlugin 只实现最小契约（ID），用于多插件场景的排序断言。
type fakeListPlugin struct{ id pluginkit.ID }

func (p fakeListPlugin) ID() pluginkit.ID { return p.id }

// TestPluginRoutesUserEnabledListStableOrder 多插件时清单按 ID 稳定排序：
// 注册顺序故意乱序，排序由 Manager.Snapshot 的 ID 稳定序保证，且多次请求结果一致。
func TestPluginRoutesUserEnabledListStableOrder(t *testing.T) {
	gin.SetMode(gin.TestMode)
	states := kittest.NewMemoryStateStore()
	host := pluginkit.HostDeps{Logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	factories := []pluginkit.Factory{
		func() pluginkit.Plugin { return fakeListPlugin{id: "zeta"} },
		func() pluginkit.Plugin { return fakeListPlugin{id: "alpha"} },
		func() pluginkit.Plugin { return fakeListPlugin{id: "midway"} },
	}
	manager, err := pluginkit.NewManager(host, states, nil, factories)
	require.NoError(t, err)

	engine := gin.New()
	v1 := engine.Group("/api/v1")
	RegisterPluginRoutes(v1, pluginTestJWTAuth(2), pluginTestAdminAuth(1), nil, manager, states)

	ctx := context.Background()
	for _, id := range []pluginkit.ID{"zeta", "alpha", "midway"} {
		require.NoError(t, states.SetEnabled(ctx, id, true, "test"))
	}

	for i := 0; i < 3; i++ {
		require.Equal(t, []string{"alpha", "midway", "zeta"}, fetchEnabledPluginIDs(t, engine), "第 %d 次请求", i+1)
	}
}

// ============================================================
// 外部插件层端点（phase-4 TASK-001）
// ============================================================

// routesMemInstallStore 是 pluginhost.InstallationStore 的内存替身（仅路由集成测试用；
// 真实 DB 实现的行为由 repository 层的 plugin_installation_repo_test.go 覆盖）。
type routesMemInstallStore struct {
	mu   sync.Mutex
	rows map[pluginkit.ID]pluginhost.Installation
}

var _ pluginhost.InstallationStore = (*routesMemInstallStore)(nil)

func newRoutesMemInstallStore() *routesMemInstallStore {
	return &routesMemInstallStore{rows: make(map[pluginkit.ID]pluginhost.Installation)}
}

func (s *routesMemInstallStore) Upsert(_ context.Context, inst *pluginhost.Installation) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.rows[inst.ID] = *inst
	return nil
}

func (s *routesMemInstallStore) Get(_ context.Context, id pluginkit.ID) (*pluginhost.Installation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	row, ok := s.rows[id]
	if !ok {
		return nil, pluginhost.ErrNotInstalled
	}
	out := row
	return &out, nil
}

func (s *routesMemInstallStore) List(_ context.Context) ([]*pluginhost.Installation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]*pluginhost.Installation, 0, len(s.rows))
	for _, row := range s.rows {
		r := row
		out = append(out, &r)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

func (s *routesMemInstallStore) Delete(_ context.Context, id pluginkit.ID) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.rows, id)
	return nil
}

func (s *routesMemInstallStore) SetConfig(_ context.Context, id pluginkit.ID, config json.RawMessage) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	row, ok := s.rows[id]
	if !ok {
		return pluginhost.ErrNotInstalled
	}
	row.Config = config
	s.rows[id] = row
	return nil
}

// newExternalPluginRouter 构造带外部插件层的最小全链路：
// 真实 Installer + 真实 Supervisor（临时存储根目录 + 内存登记/状态）
// 经 WithExternalPluginLayer 注入。
func newExternalPluginRouter(t *testing.T) (*gin.Engine, *pluginhost.PackageStore) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	manager, states := newPluginRoutesManager(t)
	require.NoError(t, manager.Bootstrap(context.Background()))
	t.Cleanup(func() { _ = manager.StopAll(context.Background()) })

	discard := slog.New(slog.NewTextHandler(io.Discard, nil))
	store := pluginhost.NewPackageStore(t.TempDir())
	installs := newRoutesMemInstallStore()
	supervisor := pluginhost.NewSupervisor(pluginhost.SupervisorDeps{
		Installs: installs,
		States:   states,
		Logger:   discard,
	})
	require.NoError(t, supervisor.Start(context.Background()))
	t.Cleanup(func() { _ = supervisor.StopAll(context.Background()) })
	installer := pluginhost.NewInstaller(pluginhost.InstallerDeps{
		Store:         store,
		Installations: installs,
		States:        states,
		Runtime:       supervisor,
		Reserved:      pluginhost.ReservedIDs(plugins.Builtin()),
		Logger:        discard,
	})

	engine := gin.New()
	v1 := engine.Group("/api/v1")
	RegisterPluginRoutes(v1, pluginTestJWTAuth(2), pluginTestAdminAuth(1), nil, manager, states,
		WithExternalPluginLayer(installer, installs, supervisor))
	return engine, store
}

// buildExternalPluginZip 生成一个含当前平台二进制与 config_schema 的插件包（zip 字节）。
func buildExternalPluginZip(t *testing.T, id, version string) []byte {
	t.Helper()
	manifest, err := json.Marshal(map[string]any{
		"id":       id,
		"name":     "External Test Plugin",
		"version":  version,
		"protocol": pluginhost.ProtocolHTTP1,
		"backend": map[string]any{
			"executables": map[string]string{pluginhost.CurrentPlatform(): "bin/plugin"},
		},
		"config_schema": map[string]any{
			"type":       "object",
			"required":   []string{"port"},
			"properties": map[string]any{"port": map[string]any{"type": "integer"}},
		},
	})
	require.NoError(t, err)

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, body := range map[string]string{
		"manifest.json": string(manifest),
		"bin/plugin":    "#!/bin/sh\necho " + version + "\n",
	} {
		w, err := zw.Create(name)
		require.NoError(t, err)
		_, err = w.Write([]byte(body))
		require.NoError(t, err)
	}
	require.NoError(t, zw.Close())
	return buf.Bytes()
}

// doPluginUpload 以 multipart 单文件字段 file 上传插件包。
func doPluginUpload(t *testing.T, engine *gin.Engine, zipData []byte) *httptest.ResponseRecorder {
	t.Helper()
	body := &bytes.Buffer{}
	mw := multipart.NewWriter(body)
	fw, err := mw.CreateFormFile("file", "plugin.zip")
	require.NoError(t, err)
	_, err = fw.Write(zipData)
	require.NoError(t, err)
	require.NoError(t, mw.Close())

	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/plugins/upload", body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	w := httptest.NewRecorder()
	engine.ServeHTTP(w, req)
	return w
}

// doPluginConfigPut 以 JSON 请求体写插件配置。
func doPluginConfigPut(t *testing.T, engine *gin.Engine, id, config string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPut, "/api/v1/admin/plugins/"+id+"/config", strings.NewReader(config))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	engine.ServeHTTP(w, req)
	return w
}

// TestPluginRoutesExternalLayerUnavailable 外部插件层未装配（生产在 TASK-002 接入前的形态）：
// 端点保持注册但统一 503，既有内建端点零行为变更。
func TestPluginRoutesExternalLayerUnavailable(t *testing.T) {
	engine, _, _ := newPluginIntegrationRouter(t)

	for _, tc := range []struct{ method, path string }{
		{http.MethodPost, "/api/v1/admin/plugins/upload"},
		{http.MethodDelete, "/api/v1/admin/plugins/acme.hello"},
		{http.MethodGet, "/api/v1/admin/plugins/acme.hello/config"},
		{http.MethodPut, "/api/v1/admin/plugins/acme.hello/config"},
	} {
		w := doPluginRequest(t, engine, tc.method, tc.path)
		require.Equal(t, http.StatusServiceUnavailable, w.Code, "%s %s", tc.method, tc.path)
	}
}

// TestPluginRoutesExternalUploadFullChain 外部插件层全链路：
// 上传安装（默认 disabled）→ config 读写与 schema 校验 → 同 ID 升级保留 config
// → 卸载 → 登记与配置随之消失。
func TestPluginRoutesExternalUploadFullChain(t *testing.T) {
	engine, store := newExternalPluginRouter(t)

	// 上传安装 → 200，默认不出现在 enabled 清单（disabled）
	w := doPluginUpload(t, engine, buildExternalPluginZip(t, "acme.hello", "1.0.0"))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var installed struct {
		ID      string `json:"id"`
		Version string `json:"version"`
	}
	decodePluginData(t, w.Body.Bytes(), &installed)
	require.Equal(t, "acme.hello", installed.ID)
	require.Equal(t, "1.0.0", installed.Version)
	require.DirExists(t, store.Dir("acme.hello", "1.0.0"))
	require.Equal(t, []string{}, fetchEnabledPluginIDs(t, engine), "安装后默认 disabled")

	// config 初始为 null，schema 一并透出
	w = doPluginRequest(t, engine, http.MethodGet, "/api/v1/admin/plugins/acme.hello/config")
	require.Equal(t, http.StatusOK, w.Code)
	var cfg struct {
		Config       json.RawMessage `json:"config"`
		ConfigSchema json.RawMessage `json:"config_schema"`
	}
	decodePluginData(t, w.Body.Bytes(), &cfg)
	require.Equal(t, "null", string(cfg.Config))
	require.NotEmpty(t, cfg.ConfigSchema)

	// PUT：非法 JSON → 400；违反 schema → 400；合法 → 200 并可读回
	require.Equal(t, http.StatusBadRequest, doPluginConfigPut(t, engine, "acme.hello", `{oops`).Code)
	require.Equal(t, http.StatusBadRequest, doPluginConfigPut(t, engine, "acme.hello", `{"port":"not-int"}`).Code)
	require.Equal(t, http.StatusOK, doPluginConfigPut(t, engine, "acme.hello", `{"port":8080}`).Code)
	w = doPluginRequest(t, engine, http.MethodGet, "/api/v1/admin/plugins/acme.hello/config")
	decodePluginData(t, w.Body.Bytes(), &cfg)
	require.JSONEq(t, `{"port":8080}`, string(cfg.Config))

	// 同 ID 上传 → 升级路径：版本替换、config 保留、旧版本目录清理
	w = doPluginUpload(t, engine, buildExternalPluginZip(t, "acme.hello", "2.0.0"))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	decodePluginData(t, w.Body.Bytes(), &installed)
	require.Equal(t, "2.0.0", installed.Version)
	require.DirExists(t, store.Dir("acme.hello", "2.0.0"))
	require.NoDirExists(t, store.Dir("acme.hello", "1.0.0"))
	w = doPluginRequest(t, engine, http.MethodGet, "/api/v1/admin/plugins/acme.hello/config")
	decodePluginData(t, w.Body.Bytes(), &cfg)
	require.JSONEq(t, `{"port":8080}`, string(cfg.Config), "升级必须保留配置")

	// 卸载 → 200；登记与文件消失，config 端点 404，重复卸载 404
	w = doPluginRequest(t, engine, http.MethodDelete, "/api/v1/admin/plugins/acme.hello")
	require.Equal(t, http.StatusOK, w.Code)
	require.NoDirExists(t, store.Dir("acme.hello", "2.0.0"))
	require.Equal(t, http.StatusNotFound,
		doPluginRequest(t, engine, http.MethodGet, "/api/v1/admin/plugins/acme.hello/config").Code)
	require.Equal(t, http.StatusNotFound,
		doPluginRequest(t, engine, http.MethodDelete, "/api/v1/admin/plugins/acme.hello").Code)
}

// buildFrontendOnlyPluginZip 生成一个纯前端插件包（无后端子进程）。
func buildFrontendOnlyPluginZip(t *testing.T, id, version string) []byte {
	t.Helper()
	manifest, err := json.Marshal(map[string]any{
		"id":       id,
		"name":     "Frontend Only Plugin",
		"version":  version,
		"protocol": pluginhost.ProtocolHTTP1,
		"frontend": map[string]any{"entry": "webapp/plugin.js"},
	})
	require.NoError(t, err)

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, body := range map[string]string{
		"manifest.json":    string(manifest),
		"webapp/plugin.js": "(function(){/* runtime entry */})()\n",
		"webapp/inner.txt": "not declared, must never be served\n",
	} {
		w, err := zw.Create(name)
		require.NoError(t, err)
		_, err = w.Write([]byte(body))
		require.NoError(t, err)
	}
	require.NoError(t, zw.Close())
	return buf.Bytes()
}

// TestPluginRoutesExternalFrontendFullChain 纯前端外部插件全链路（phase-4 决策 6）：
// 上传 → enable（tier=external）→ enabled 清单携带 assets → 资产服务（白名单
// Content-Type + nosniff、未声明文件 404）→ disable 后资产 fail-closed。
func TestPluginRoutesExternalFrontendFullChain(t *testing.T) {
	engine, _ := newExternalPluginRouter(t)

	w := doPluginUpload(t, engine, buildFrontendOnlyPluginZip(t, "acme.web", "1.0.0"))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	// 安装后默认 disabled：不在 enabled 清单，资产 404
	require.Equal(t, []string{}, fetchEnabledPluginIDs(t, engine))
	require.Equal(t, http.StatusNotFound,
		doPluginRequest(t, engine, http.MethodGet, "/api/v1/plugins/acme.web/assets/webapp/plugin.js").Code)

	// enable → 200，快照 tier=external、running
	w = doPluginRequest(t, engine, http.MethodPost, "/api/v1/admin/plugins/acme.web/enable")
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var status struct {
		ID      string `json:"id"`
		Enabled bool   `json:"enabled"`
		State   string `json:"state"`
		Tier    string `json:"tier"`
		Version string `json:"version"`
	}
	decodePluginData(t, w.Body.Bytes(), &status)
	require.True(t, status.Enabled)
	require.Equal(t, "running", status.State)
	require.Equal(t, "external", status.Tier)
	require.Equal(t, "1.0.0", status.Version)

	// admin 快照合并：内建 demo（tier=builtin）+ 外部 acme.web（tier=external），按 ID 排序
	w = doPluginRequest(t, engine, http.MethodGet, "/api/v1/admin/plugins")
	require.Equal(t, http.StatusOK, w.Code)
	var merged []struct {
		ID   string `json:"id"`
		Tier string `json:"tier"`
	}
	decodePluginData(t, w.Body.Bytes(), &merged)
	require.Len(t, merged, 2)
	require.Equal(t, "acme.web", merged[0].ID)
	require.Equal(t, "external", merged[0].Tier)
	require.Equal(t, string(demo.PluginID), merged[1].ID)
	require.Equal(t, "builtin", merged[1].Tier)

	// enabled 清单携带 assets 入口
	w = doPluginRequest(t, engine, http.MethodGet, "/api/v1/plugins")
	require.Equal(t, http.StatusOK, w.Code)
	var items []handler.EnabledPlugin
	decodePluginData(t, w.Body.Bytes(), &items)
	require.Len(t, items, 1)
	require.Equal(t, "acme.web", items[0].ID)
	require.Equal(t, "/api/v1/plugins/acme.web/assets/webapp/plugin.js", items[0].Assets)

	// 资产服务：声明文件 200 + 白名单 Content-Type + nosniff；未声明文件/穿越 404
	w = doPluginRequest(t, engine, http.MethodGet, "/api/v1/plugins/acme.web/assets/webapp/plugin.js")
	require.Equal(t, http.StatusOK, w.Code)
	require.Equal(t, "text/javascript; charset=utf-8", w.Header().Get("Content-Type"))
	require.Equal(t, "nosniff", w.Header().Get("X-Content-Type-Options"))
	require.Contains(t, w.Body.String(), "runtime entry")
	require.Equal(t, http.StatusNotFound,
		doPluginRequest(t, engine, http.MethodGet, "/api/v1/plugins/acme.web/assets/webapp/inner.txt").Code)
	require.Equal(t, http.StatusNotFound,
		doPluginRequest(t, engine, http.MethodGet, "/api/v1/plugins/acme.web/assets/webapp/../manifest.json").Code)

	// disable → 资产与清单 fail-closed
	w = doPluginRequest(t, engine, http.MethodPost, "/api/v1/admin/plugins/acme.web/disable")
	require.Equal(t, http.StatusOK, w.Code)
	require.Equal(t, []string{}, fetchEnabledPluginIDs(t, engine))
	require.Equal(t, http.StatusNotFound,
		doPluginRequest(t, engine, http.MethodGet, "/api/v1/plugins/acme.web/assets/webapp/plugin.js").Code)
}

// TestPluginRoutesExternalGuards 外部层守卫：内建 ID 拒绝卸载与占用，坏包拒绝安装。
func TestPluginRoutesExternalGuards(t *testing.T) {
	engine, _ := newExternalPluginRouter(t)

	// 卸载内建插件 → 400
	w := doPluginRequest(t, engine, http.MethodDelete, "/api/v1/admin/plugins/"+string(demo.PluginID))
	require.Equal(t, http.StatusBadRequest, w.Code)

	// 上传占用内建 ID 的包 → 400
	w = doPluginUpload(t, engine, buildExternalPluginZip(t, string(demo.PluginID), "1.0.0"))
	require.Equal(t, http.StatusBadRequest, w.Code)

	// 非 zip 内容 → 400
	w = doPluginUpload(t, engine, []byte("definitely not a zip"))
	require.Equal(t, http.StatusBadRequest, w.Code)

	// 缺 multipart file 字段 → 400
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/plugins/upload", strings.NewReader("{}"))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)
	require.Equal(t, http.StatusBadRequest, rec.Code)

	// 未安装插件的 config 读写 → 404
	require.Equal(t, http.StatusNotFound,
		doPluginRequest(t, engine, http.MethodGet, "/api/v1/admin/plugins/nope/config").Code)
	require.Equal(t, http.StatusNotFound, doPluginConfigPut(t, engine, "nope", `{}`).Code)
}
