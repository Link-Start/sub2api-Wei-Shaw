//go:build unit

package routes

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

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

// fetchEnabledPluginIDs 请求用户态 enabled 清单端点并解出 data: string[]。
func fetchEnabledPluginIDs(t *testing.T, engine *gin.Engine) []string {
	t.Helper()
	w := doPluginRequest(t, engine, http.MethodGet, "/api/v1/plugins")
	require.Equal(t, http.StatusOK, w.Code)
	var ids []string
	decodePluginData(t, w.Body.Bytes(), &ids)
	return ids
}

// TestPluginRoutesUserEnabledList 用户态 enabled 清单端点全链路：
// 初始空数组（非 null）→ enable 后仅含 demo 且不泄露内部状态字段 → disable 后回到空。
func TestPluginRoutesUserEnabledList(t *testing.T) {
	engine, _, states := newPluginIntegrationRouter(t)
	ctx := context.Background()

	// 初始：demo 未启用 → 空数组
	require.Equal(t, []string{}, fetchEnabledPluginIDs(t, engine))

	// enable → 仅含 demo；载荷是纯 ID 列表，不泄露 state/error/started_at
	require.NoError(t, states.SetEnabled(ctx, demo.PluginID, true, "test"))
	require.Equal(t, []string{string(demo.PluginID)}, fetchEnabledPluginIDs(t, engine))
	w := doPluginRequest(t, engine, http.MethodGet, "/api/v1/plugins")
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
