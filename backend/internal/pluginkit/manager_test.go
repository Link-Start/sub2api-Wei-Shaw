//go:build unit

package pluginkit

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func init() {
	gin.SetMode(gin.TestMode)
}

// fakeStateStore 是测试内联的最小内存版 StateStore
// （公共版夹具由 kittest 提供，属 TASK-004 范围）。
type fakeStateStore struct {
	mu      sync.Mutex
	enabled map[ID]bool
	subs    []func(StateChange)
}

func newFakeStateStore() *fakeStateStore {
	return &fakeStateStore{enabled: make(map[ID]bool)}
}

func (s *fakeStateStore) Enabled(id ID) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.enabled[id]
}

func (s *fakeStateStore) SetEnabled(_ context.Context, id ID, enabled bool, _ string) error {
	s.mu.Lock()
	s.enabled[id] = enabled
	subs := slices.Clone(s.subs)
	s.mu.Unlock()
	// 契约：回调在非持锁上下文调用。
	for _, fn := range subs {
		fn(StateChange{ID: id, Enabled: enabled})
	}
	return nil
}

func (s *fakeStateStore) Subscribe(fn func(StateChange)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.subs = append(s.subs, fn)
}

func (s *fakeStateStore) Close() error { return nil }

// testPlugin 覆盖全部扩展面（Initializer + Runner + APIProvider）。
type testPlugin struct {
	id ID

	mu         sync.Mutex
	initCalls  int
	startCalls int
	stopCalls  int
	mountCalls int
	initErr    error
	startErr   error
	stopBlock  chan struct{} // 非 nil 时 Stop 阻塞至 channel 关闭（无视 ctx，模拟卡死插件）
	onStop     func()
	host       *Host
}

func (p *testPlugin) ID() ID { return p.id }

func (p *testPlugin) Init(_ context.Context, host *Host) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.initCalls++
	p.host = host
	return p.initErr
}

func (p *testPlugin) Start(_ context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.startCalls++
	return p.startErr
}

func (p *testPlugin) Stop(_ context.Context) error {
	p.mu.Lock()
	p.stopCalls++
	block := p.stopBlock
	onStop := p.onStop
	p.mu.Unlock()
	if onStop != nil {
		onStop()
	}
	if block != nil {
		<-block
	}
	return nil
}

func (p *testPlugin) MountRoutes(r Router) {
	p.mu.Lock()
	p.mountCalls++
	p.mu.Unlock()
	r.Admin().GET("/hello", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"plugin": string(p.id), "path": c.Request.URL.Path})
	})
	r.User().GET("/ping", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"pong": true})
	})
}

func (p *testPlugin) counters() (initCalls, startCalls, stopCalls, mountCalls int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.initCalls, p.startCalls, p.stopCalls, p.mountCalls
}

// barePlugin 只实现最小契约（无 Initializer/Runner/APIProvider）。
type barePlugin struct{ id ID }

func (p *barePlugin) ID() ID { return p.id }

func newTestManager(t *testing.T, store StateStore, cfg PluginsConfig, factories ...Factory) *Manager {
	t.Helper()
	deps := HostDeps{Logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	m, err := NewManager(deps, store, cfg, factories)
	require.NoError(t, err)
	return m
}

func pluginState(m *Manager, id ID) PluginStatus {
	for _, st := range m.Snapshot() {
		if st.ID == id {
			return st
		}
	}
	return PluginStatus{}
}

func TestNewManager_InvalidID(t *testing.T) {
	deps := HostDeps{Logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	_, err := NewManager(deps, newFakeStateStore(), nil, []Factory{
		func() Plugin { return &barePlugin{id: "Bad_ID"} },
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "invalid plugin id")
}

func TestNewManager_DuplicateID(t *testing.T) {
	deps := HostDeps{Logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	_, err := NewManager(deps, newFakeStateStore(), nil, []Factory{
		func() Plugin { return &barePlugin{id: "demo"} },
		func() Plugin { return &testPlugin{id: "demo"} },
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "duplicate plugin id")
}

func TestBootstrap_FailureIsolation(t *testing.T) {
	store := newFakeStateStore()
	bad := &testPlugin{id: "bad", initErr: errors.New("boom")}
	good := &testPlugin{id: "good"}
	m := newTestManager(t, store, nil,
		func() Plugin { return bad },
		func() Plugin { return good },
	)
	require.NoError(t, store.SetEnabled(context.Background(), "bad", true, "t"))
	require.NoError(t, store.SetEnabled(context.Background(), "good", true, "t"))
	require.NoError(t, m.Bootstrap(context.Background()))

	badSt := pluginState(m, "bad")
	require.Equal(t, StateFailed, badSt.State)
	require.Contains(t, badSt.Err, "boom")
	require.Nil(t, badSt.StartedAt)

	goodSt := pluginState(m, "good")
	require.Equal(t, StateRunning, goodSt.State)
	require.Empty(t, goodSt.Err)
	require.NotNil(t, goodSt.StartedAt)

	require.False(t, m.GateAllows("bad"))
	require.True(t, m.GateAllows("good"))
}

func TestBootstrap_Twice(t *testing.T) {
	m := newTestManager(t, newFakeStateStore(), nil)
	require.NoError(t, m.Bootstrap(context.Background()))
	require.Error(t, m.Bootstrap(context.Background()))
}

func TestToggle_EnableDisableEnable_SameInstance(t *testing.T) {
	store := newFakeStateStore()
	factoryCalls := 0
	p := &testPlugin{id: "demo"}
	m := newTestManager(t, store, nil, func() Plugin {
		factoryCalls++
		return p
	})
	require.NoError(t, m.Bootstrap(context.Background()))
	require.Equal(t, 1, factoryCalls, "factory 只在 NewManager 实例化一次")

	ctx := context.Background()
	require.NoError(t, store.SetEnabled(ctx, "demo", true, "t"))
	initCalls, startCalls, stopCalls, _ := p.counters()
	require.Equal(t, [3]int{1, 1, 0}, [3]int{initCalls, startCalls, stopCalls})
	require.Equal(t, StateRunning, pluginState(m, "demo").State)

	require.NoError(t, store.SetEnabled(ctx, "demo", false, "t"))
	initCalls, startCalls, stopCalls, _ = p.counters()
	require.Equal(t, [3]int{1, 1, 1}, [3]int{initCalls, startCalls, stopCalls})
	require.Equal(t, StateStopped, pluginState(m, "demo").State)
	require.Nil(t, pluginState(m, "demo").StartedAt)

	// 再次 enable：同一实例重新 Init→Start。
	require.NoError(t, store.SetEnabled(ctx, "demo", true, "t"))
	initCalls, startCalls, stopCalls, _ = p.counters()
	require.Equal(t, [3]int{2, 2, 1}, [3]int{initCalls, startCalls, stopCalls})
	require.Equal(t, StateRunning, pluginState(m, "demo").State)
	require.Equal(t, 1, factoryCalls)
}

func TestToggle_Idempotent(t *testing.T) {
	store := newFakeStateStore()
	p := &testPlugin{id: "demo"}
	m := newTestManager(t, store, nil, func() Plugin { return p })
	require.NoError(t, m.Bootstrap(context.Background()))
	ctx := context.Background()

	// 未启用时 disable 为 no-op。
	require.NoError(t, store.SetEnabled(ctx, "demo", false, "t"))
	_, _, stopCalls, _ := p.counters()
	require.Zero(t, stopCalls)
	require.Equal(t, StateDisabled, pluginState(m, "demo").State)

	require.NoError(t, store.SetEnabled(ctx, "demo", true, "t"))
	require.NoError(t, store.SetEnabled(ctx, "demo", true, "t"))
	initCalls, startCalls, _, _ := p.counters()
	require.Equal(t, 1, initCalls)
	require.Equal(t, 1, startCalls)

	require.NoError(t, store.SetEnabled(ctx, "demo", false, "t"))
	require.NoError(t, store.SetEnabled(ctx, "demo", false, "t"))
	_, _, stopCalls, _ = p.counters()
	require.Equal(t, 1, stopCalls)
	require.Equal(t, StateStopped, pluginState(m, "demo").State)
}

func TestToggle_UnknownID(t *testing.T) {
	store := newFakeStateStore()
	m := newTestManager(t, store, nil)
	require.NoError(t, m.Bootstrap(context.Background()))
	// 未注册插件的 toggle 只打日志，不 panic。
	require.NoError(t, store.SetEnabled(context.Background(), "ghost", true, "t"))
	require.Empty(t, m.Snapshot())
}

func TestToggle_Concurrent(t *testing.T) {
	store := newFakeStateStore()
	a := &testPlugin{id: "a"}
	b := &testPlugin{id: "b"}
	m := newTestManager(t, store, nil,
		func() Plugin { return a },
		func() Plugin { return b },
	)
	require.NoError(t, m.Bootstrap(context.Background()))
	ctx := context.Background()

	var wg sync.WaitGroup
	for g := 0; g < 16; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			id := ID("a")
			if g%2 == 0 {
				id = "b"
			}
			for i := 0; i < 20; i++ {
				_ = store.SetEnabled(ctx, id, i%2 == 0, "t")
				_ = m.GateAllows(id)
				_ = m.Snapshot()
			}
		}(g)
	}
	wg.Wait()

	require.NoError(t, store.SetEnabled(ctx, "a", false, "t"))
	require.NoError(t, store.SetEnabled(ctx, "b", false, "t"))

	for _, p := range []*testPlugin{a, b} {
		initCalls, startCalls, stopCalls, _ := p.counters()
		st := pluginState(m, p.id).State
		require.NotEqual(t, StateRunning, st)
		require.Equal(t, initCalls, startCalls, "无失败注入时 Init/Start 成对")
		require.Equal(t, startCalls, stopCalls, "终态非 running 时每次 Start 都有配对的 Stop")
	}
}

func TestStop_TimeoutEntersFailed(t *testing.T) {
	store := newFakeStateStore()
	block := make(chan struct{})
	p := &testPlugin{id: "demo", stopBlock: block}
	m := newTestManager(t, store, nil, func() Plugin { return p })
	m.stopTimeout = 30 * time.Millisecond
	require.NoError(t, m.Bootstrap(context.Background()))
	ctx := context.Background()

	require.NoError(t, store.SetEnabled(ctx, "demo", true, "t"))
	require.NoError(t, store.SetEnabled(ctx, "demo", false, "t"))

	st := pluginState(m, "demo")
	require.Equal(t, StateFailed, st.State)
	require.Contains(t, st.Err, "timed out")
	require.False(t, m.GateAllows("demo"))
	close(block) // 释放被泄漏的 Stop goroutine

	// failed 态可经 enable 重试恢复。
	p.mu.Lock()
	p.stopBlock = nil
	p.mu.Unlock()
	require.NoError(t, store.SetEnabled(ctx, "demo", true, "t"))
	require.Equal(t, StateRunning, pluginState(m, "demo").State)
}

func TestStopAll_ReverseOrderAndAggregatesErrors(t *testing.T) {
	store := newFakeStateStore()
	var orderMu sync.Mutex
	var stopped []ID
	record := func(id ID) func() {
		return func() {
			orderMu.Lock()
			stopped = append(stopped, id)
			orderMu.Unlock()
		}
	}
	a := &testPlugin{id: "a", onStop: record("a")}
	b := &testPlugin{id: "b", onStop: record("b")}
	c := &testPlugin{id: "c", onStop: record("c")}
	m := newTestManager(t, store, nil,
		func() Plugin { return b },
		func() Plugin { return c },
		func() Plugin { return a },
	)
	require.NoError(t, m.Bootstrap(context.Background()))
	ctx := context.Background()
	for _, id := range []ID{"a", "b", "c"} {
		require.NoError(t, store.SetEnabled(ctx, id, true, "t"))
	}

	require.NoError(t, m.StopAll(ctx))
	require.Equal(t, []ID{"c", "b", "a"}, stopped, "逆 ID 序停止")
	for _, id := range []ID{"a", "b", "c"} {
		require.Equal(t, StateStopped, pluginState(m, id).State)
	}
}

func TestStopAll_TimeoutIsolation(t *testing.T) {
	store := newFakeStateStore()
	block := make(chan struct{})
	stuck := &testPlugin{id: "stuck", stopBlock: block}
	ok := &testPlugin{id: "ok"}
	m := newTestManager(t, store, nil,
		func() Plugin { return stuck },
		func() Plugin { return ok },
	)
	m.stopTimeout = 30 * time.Millisecond
	require.NoError(t, m.Bootstrap(context.Background()))
	ctx := context.Background()
	require.NoError(t, store.SetEnabled(ctx, "stuck", true, "t"))
	require.NoError(t, store.SetEnabled(ctx, "ok", true, "t"))

	err := m.StopAll(ctx)
	require.Error(t, err)
	require.Contains(t, err.Error(), "stuck")
	require.Equal(t, StateFailed, pluginState(m, "stuck").State)
	require.Equal(t, StateStopped, pluginState(m, "ok").State)
	close(block)
}

// newDispatchHost 模拟宿主侧分发器路由（真实挂载在 TASK-005 完成）。
func newDispatchHost(m *Manager) *gin.Engine {
	host := gin.New()
	host.Any("/api/v1/admin/plugins/:id/api/*path", func(c *gin.Context) {
		m.Dispatch(SideAdmin, c.Param("id"), c)
	})
	host.Any("/api/v1/plugins/:id/api/*path", func(c *gin.Context) {
		m.Dispatch(SideUser, c.Param("id"), c)
	})
	return host
}

func doRequest(host *gin.Engine, method, path string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	req := httptest.NewRequest(method, path, nil)
	host.ServeHTTP(w, req)
	return w
}

func requireEnvelope404(t *testing.T, w *httptest.ResponseRecorder) {
	t.Helper()
	require.Equal(t, http.StatusNotFound, w.Code)
	var body struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	require.Equal(t, http.StatusNotFound, body.Code)
	require.NotEmpty(t, body.Message)
}

func TestDispatch_GateAndPathRewrite(t *testing.T) {
	store := newFakeStateStore()
	p := &testPlugin{id: "demo"}
	bare := &barePlugin{id: "bare"}
	m := newTestManager(t, store, nil,
		func() Plugin { return p },
		func() Plugin { return bare },
	)
	require.NoError(t, m.Bootstrap(context.Background()))
	host := newDispatchHost(m)
	ctx := context.Background()

	// 未启用 → 404（项目标准错误体）。
	requireEnvelope404(t, doRequest(host, http.MethodGet, "/api/v1/admin/plugins/demo/api/hello"))

	require.NoError(t, store.SetEnabled(ctx, "demo", true, "t"))

	// admin 面命中，且插件看到的是私有子路由器内的重写路径。
	w := doRequest(host, http.MethodGet, "/api/v1/admin/plugins/demo/api/hello")
	require.Equal(t, http.StatusOK, w.Code)
	var body map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	require.Equal(t, "demo", body["plugin"])
	require.Equal(t, "/admin/hello", body["path"])

	// user 面命中；admin 路由不可从 user 面访问。
	require.Equal(t, http.StatusOK, doRequest(host, http.MethodGet, "/api/v1/plugins/demo/api/ping").Code)
	requireEnvelope404(t, doRequest(host, http.MethodGet, "/api/v1/plugins/demo/api/hello"))

	// 插件内部未注册路径 → 子路由器 404（同样的标准错误体）。
	requireEnvelope404(t, doRequest(host, http.MethodGet, "/api/v1/admin/plugins/demo/api/nope"))

	// 未知插件 ID → 404。
	requireEnvelope404(t, doRequest(host, http.MethodGet, "/api/v1/admin/plugins/ghost/api/hello"))

	// 已启用但无 API 面 → 404。
	require.NoError(t, store.SetEnabled(ctx, "bare", true, "t"))
	requireEnvelope404(t, doRequest(host, http.MethodGet, "/api/v1/admin/plugins/bare/api/hello"))

	// disable 后门控关闭。
	require.NoError(t, store.SetEnabled(ctx, "demo", false, "t"))
	requireEnvelope404(t, doRequest(host, http.MethodGet, "/api/v1/admin/plugins/demo/api/hello"))
}

func TestDispatch_RestoresHostPath(t *testing.T) {
	store := newFakeStateStore()
	p := &testPlugin{id: "demo"}
	m := newTestManager(t, store, nil, func() Plugin { return p })
	require.NoError(t, m.Bootstrap(context.Background()))
	require.NoError(t, store.SetEnabled(context.Background(), "demo", true, "t"))

	host := gin.New()
	var afterPath string
	host.Use(func(c *gin.Context) {
		c.Next()
		afterPath = c.Request.URL.Path
	})
	host.Any("/api/v1/admin/plugins/:id/api/*path", func(c *gin.Context) {
		m.Dispatch(SideAdmin, c.Param("id"), c)
	})

	w := doRequest(host, http.MethodGet, "/api/v1/admin/plugins/demo/api/hello")
	require.Equal(t, http.StatusOK, w.Code)
	require.Equal(t, "/api/v1/admin/plugins/demo/api/hello", afterPath,
		"转发后宿主侧后置中间件应看到原始路径")
}

func TestDispatch_MountRoutesOnceAcrossRestarts(t *testing.T) {
	store := newFakeStateStore()
	p := &testPlugin{id: "demo"}
	m := newTestManager(t, store, nil, func() Plugin { return p })
	require.NoError(t, m.Bootstrap(context.Background()))
	ctx := context.Background()

	require.NoError(t, store.SetEnabled(ctx, "demo", true, "t"))
	require.NoError(t, store.SetEnabled(ctx, "demo", false, "t"))
	require.NoError(t, store.SetEnabled(ctx, "demo", true, "t"))

	_, _, _, mountCalls := p.counters()
	require.Equal(t, 1, mountCalls, "MountRoutes 仅在 Bootstrap 时调用一次")

	// 重启后路由仍可命中（同一子路由器）。
	host := newDispatchHost(m)
	require.Equal(t, http.StatusOK, doRequest(host, http.MethodGet, "/api/v1/admin/plugins/demo/api/hello").Code)
}

func TestSnapshot_SortedByID(t *testing.T) {
	store := newFakeStateStore()
	m := newTestManager(t, store, nil,
		func() Plugin { return &barePlugin{id: "b"} },
		func() Plugin { return &barePlugin{id: "a.x"} },
		func() Plugin { return &barePlugin{id: "c"} },
	)
	require.NoError(t, m.Bootstrap(context.Background()))
	require.NoError(t, store.SetEnabled(context.Background(), "b", true, "t"))

	snap := m.Snapshot()
	ids := make([]ID, 0, len(snap))
	for _, st := range snap {
		ids = append(ids, st.ID)
	}
	require.Equal(t, []ID{"a.x", "b", "c"}, ids)

	require.True(t, snap[1].Enabled)
	require.Equal(t, StateRunning, snap[1].State, "无生命周期扩展面的插件 enable 后即 running")
	require.False(t, snap[0].Enabled)
	require.Equal(t, StateDisabled, snap[0].State)
}

func TestGateAllows(t *testing.T) {
	store := newFakeStateStore()
	failing := &testPlugin{id: "failing", initErr: errors.New("nope")}
	ok := &testPlugin{id: "ok"}
	m := newTestManager(t, store, nil,
		func() Plugin { return failing },
		func() Plugin { return ok },
	)
	require.NoError(t, m.Bootstrap(context.Background()))
	ctx := context.Background()

	require.False(t, m.GateAllows("ghost"), "未注册")
	require.False(t, m.GateAllows("ok"), "未启用")

	require.NoError(t, store.SetEnabled(ctx, "ok", true, "t"))
	require.True(t, m.GateAllows("ok"))

	require.NoError(t, store.SetEnabled(ctx, "failing", true, "t"))
	require.False(t, m.GateAllows("failing"), "enabled 但 failed 不放行")
}

func TestHostConfigDecoding(t *testing.T) {
	store := newFakeStateStore()
	p := &testPlugin{id: "demo"}
	cfg, err := ParsePluginsConfig(map[string]any{
		"demo": map[string]any{"greeting": "hola", "interval": "3s"},
	})
	require.NoError(t, err)
	m := newTestManager(t, store, cfg, func() Plugin { return p })
	require.NoError(t, m.Bootstrap(context.Background()))
	require.NoError(t, store.SetEnabled(context.Background(), "demo", true, "t"))

	p.mu.Lock()
	host := p.host
	p.mu.Unlock()
	require.NotNil(t, host)
	require.NotNil(t, host.Logger)

	var out demoPluginConfig
	require.NoError(t, host.Config(&out))
	require.Equal(t, "hola", out.Greeting)
	require.Equal(t, 3*time.Second, out.Interval)
}
