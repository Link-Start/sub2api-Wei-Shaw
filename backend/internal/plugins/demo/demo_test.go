//go:build unit

package demo

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pluginkit"
	"github.com/Wei-Shaw/sub2api/internal/pluginkit/kittest"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func init() {
	gin.SetMode(gin.TestMode)
}

// decoderFor 用 ParsePluginsConfig 走真实解码链路，构造 plugins.demo 的私有配置解码函数。
func decoderFor(t *testing.T, conf map[string]any) func(out any) error {
	t.Helper()
	cfg, err := pluginkit.ParsePluginsConfig(map[string]any{"demo": conf})
	require.NoError(t, err)
	return cfg.DecoderFor(PluginID)
}

// syncBuffer 是并发安全的日志捕获缓冲。
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// ============================================================
// 配置解码与校验
// ============================================================

func TestInit_DefaultsWhenUnconfigured(t *testing.T) {
	p := New().(*Plugin)
	require.NoError(t, p.Init(context.Background(), kittest.NewHost(t)))
	require.Equal(t, defaultGreeting, p.cfg.Greeting)
	require.Equal(t, defaultInterval, p.cfg.Interval)
}

func TestInit_DecodesPrivateConfig(t *testing.T) {
	host := kittest.NewHost(t, kittest.WithConfig(decoderFor(t, map[string]any{
		"greeting": "hola",
		"interval": "50ms",
	})))
	p := New().(*Plugin)
	require.NoError(t, p.Init(context.Background(), host))
	require.Equal(t, "hola", p.cfg.Greeting)
	require.Equal(t, 50*time.Millisecond, p.cfg.Interval)
}

func TestInit_PartialConfigKeepsDefaults(t *testing.T) {
	host := kittest.NewHost(t, kittest.WithConfig(decoderFor(t, map[string]any{
		"greeting": "hola",
	})))
	p := New().(*Plugin)
	require.NoError(t, p.Init(context.Background(), host))
	require.Equal(t, "hola", p.cfg.Greeting)
	require.Equal(t, defaultInterval, p.cfg.Interval, "未配置字段保持默认")
}

func TestInit_InvalidConfig(t *testing.T) {
	tests := []struct {
		name    string
		conf    map[string]any
		wantErr string
	}{
		{"interval 为负", map[string]any{"interval": "-1s"}, "interval must be positive"},
		{"interval 为零", map[string]any{"interval": "0s"}, "interval must be positive"},
		{"interval 非时长", map[string]any{"interval": "not-a-duration"}, "plugins.demo"},
		{"greeting 空白", map[string]any{"greeting": "   "}, "greeting must not be blank"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			host := kittest.NewHost(t, kittest.WithConfig(decoderFor(t, tt.conf)))
			p := New().(*Plugin)
			err := p.Init(context.Background(), host)
			require.Error(t, err)
			require.Contains(t, err.Error(), tt.wantErr)
		})
	}
}

// ============================================================
// Runner：心跳产生、停止与同实例重启
// ============================================================

func TestRunner_HeartbeatStopAndRestart(t *testing.T) {
	buf := &syncBuffer{}
	host := kittest.NewHost(t,
		kittest.WithLogger(slog.New(slog.NewTextHandler(buf, nil))),
		kittest.WithConfig(decoderFor(t, map[string]any{"greeting": "hola", "interval": "10ms"})),
	)
	heartbeats := func() int { return strings.Count(buf.String(), "demo_heartbeat") }
	ctx := context.Background()
	p := New().(*Plugin)

	require.NoError(t, p.Init(ctx, host))
	require.NoError(t, p.Start(ctx))
	require.Eventually(t, func() bool { return heartbeats() >= 2 },
		3*time.Second, 5*time.Millisecond, "应按 interval 持续产生心跳")
	require.Contains(t, buf.String(), "greeting=hola", "心跳携带配置的问候语")

	require.NoError(t, p.Stop(ctx))
	afterStop := heartbeats()
	time.Sleep(100 * time.Millisecond) // 10 倍 interval
	require.Equal(t, afterStop, heartbeats(), "Stop 后心跳应完全停止")

	// 幂等：重复 Stop 与未启动时 Stop 均为 no-op。
	require.NoError(t, p.Stop(ctx))

	// 同一实例重启（enable→disable→enable 语义）：重新 Init→Start 后心跳恢复。
	require.NoError(t, p.Init(ctx, host))
	require.NoError(t, p.Start(ctx))
	require.Eventually(t, func() bool { return heartbeats() >= afterStop+2 },
		3*time.Second, 5*time.Millisecond, "重启后心跳应恢复")
	require.NoError(t, p.Stop(ctx))
}

func TestRunner_DoubleStartRejected(t *testing.T) {
	host := kittest.NewHost(t, kittest.WithConfig(decoderFor(t, map[string]any{"interval": "1h"})))
	ctx := context.Background()
	p := New().(*Plugin)
	require.NoError(t, p.Init(ctx, host))
	require.NoError(t, p.Start(ctx))
	t.Cleanup(func() { require.NoError(t, p.Stop(context.Background())) })

	require.Error(t, p.Start(ctx), "未 Stop 的重复 Start 应报错而非泄漏前一轮 goroutine")
}

// ============================================================
// APIProvider：/hello 经 Manager.Dispatch 全链路
// ============================================================

// newDemoManager 用真实 Manager + kittest 内存 StateStore 装配 demo 插件。
func newDemoManager(t *testing.T, conf map[string]any) (*pluginkit.Manager, pluginkit.StateStore) {
	t.Helper()
	cfg, err := pluginkit.ParsePluginsConfig(map[string]any{"demo": conf})
	require.NoError(t, err)
	store := kittest.NewMemoryStateStore()
	deps := pluginkit.HostDeps{Logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	m, err := pluginkit.NewManager(deps, store, cfg, []pluginkit.Factory{New})
	require.NoError(t, err)
	require.NoError(t, m.Bootstrap(context.Background()))
	t.Cleanup(func() { require.NoError(t, m.StopAll(context.Background())) })
	return m, store
}

// newDispatchHost 模拟宿主侧分发器路由（真实挂载在接合点任务完成）。
func newDispatchHost(m *pluginkit.Manager) *gin.Engine {
	host := gin.New()
	host.Any("/api/v1/admin/plugins/:id/api/*path", func(c *gin.Context) {
		m.Dispatch(pluginkit.SideAdmin, c.Param("id"), c)
	})
	host.Any("/api/v1/plugins/:id/api/*path", func(c *gin.Context) {
		m.Dispatch(pluginkit.SideUser, c.Param("id"), c)
	})
	return host
}

func doRequest(host *gin.Engine, path string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	host.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
	return w
}

func TestHello_FullChainViaDispatch(t *testing.T) {
	m, store := newDemoManager(t, map[string]any{"greeting": "hola", "interval": "1h"})
	host := newDispatchHost(m)
	ctx := context.Background()
	const helloPath = "/api/v1/admin/plugins/demo/api/hello"

	// 未启用 → 分发器门控 404。
	require.Equal(t, http.StatusNotFound, doRequest(host, helloPath).Code)

	require.NoError(t, store.SetEnabled(ctx, PluginID, true, "test"))
	w := doRequest(host, helloPath)
	require.Equal(t, http.StatusOK, w.Code)
	var body struct {
		Greeting      string `json:"greeting"`
		UptimeSeconds *int64 `json:"uptime_seconds"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	require.Equal(t, "hola", body.Greeting)
	require.NotNil(t, body.UptimeSeconds)
	require.GreaterOrEqual(t, *body.UptimeSeconds, int64(0))

	// admin 面路由不暴露在 user 面。
	require.Equal(t, http.StatusNotFound, doRequest(host, "/api/v1/plugins/demo/api/hello").Code)
	// 插件内部未注册路径 → 子路由器 404。
	require.Equal(t, http.StatusNotFound, doRequest(host, "/api/v1/admin/plugins/demo/api/nope").Code)

	// disable 后门控关闭。
	require.NoError(t, store.SetEnabled(ctx, PluginID, false, "test"))
	require.Equal(t, http.StatusNotFound, doRequest(host, helloPath).Code)

	// 再次 enable 免重启恢复（同实例、同子路由器）。
	require.NoError(t, store.SetEnabled(ctx, PluginID, true, "test"))
	require.Equal(t, http.StatusOK, doRequest(host, helloPath).Code)
}

// ============================================================
// 启停循环泄漏断言（阶段验收标准 3：≥3 轮）
// ============================================================

func TestToggleCycleClean(t *testing.T) {
	m, store := newDemoManager(t, map[string]any{"interval": "10ms"})
	kittest.AssertToggleCycleClean(t, m, store, PluginID, 3)
}
