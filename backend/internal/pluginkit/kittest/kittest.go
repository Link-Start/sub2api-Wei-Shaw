//go:build unit

// Package kittest 提供 pluginkit 插件与内核共享的单元测试夹具：
// 内存版 Host、内存版 StateStore，以及启停循环泄漏断言。
// 所有文件使用 //go:build unit 标签，确保不会被生产构建包含。
package kittest

import (
	"bytes"
	"context"
	"log/slog"
	"runtime"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/pluginkit"

	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

// ============================================================
// NewHost — 内存版宿主能力面
// ============================================================

// HostOption 定制 NewHost 产出的宿主能力面。
type HostOption func(*pluginkit.Host)

// WithDB 注入 ent 客户端（默认 nil，不依赖数据库的插件测试无需提供）。
func WithDB(db *ent.Client) HostOption {
	return func(h *pluginkit.Host) { h.DB = db }
}

// WithRedis 注入 Redis 客户端（默认 nil，通常配合 miniredis 使用）。
func WithRedis(rdb *redis.Client) HostOption {
	return func(h *pluginkit.Host) { h.Redis = rdb }
}

// WithLogger 覆盖默认的测试 logger（如需捕获日志内容做断言）。
func WithLogger(logger *slog.Logger) HostOption {
	return func(h *pluginkit.Host) { h.Logger = logger }
}

// WithConfig 注入私有配置解码函数，通常来自 PluginsConfig.DecoderFor(id)。
func WithConfig(decode func(out any) error) HostOption {
	return func(h *pluginkit.Host) { h.Config = decode }
}

// NewHost 构造内存版 *pluginkit.Host：Logger 输出到 t.Logf（测试结束后
// 自动静音，避免插件残留 goroutine 触发 "Log in goroutine after test finished"
// panic），DB/Redis 默认 nil，Config 默认零值成功（等价于未配置该插件）。
func NewHost(t testing.TB, opts ...HostOption) *pluginkit.Host {
	t.Helper()
	w := &testLogWriter{t: t}
	t.Cleanup(w.mute)
	h := &pluginkit.Host{
		Logger: slog.New(slog.NewTextHandler(w, nil)),
		Config: func(any) error { return nil },
	}
	for _, opt := range opts {
		opt(h)
	}
	return h
}

// testLogWriter 把日志写入 t.Logf，测试结束后丢弃后续写入。
type testLogWriter struct {
	mu    sync.Mutex
	t     testing.TB
	muted bool
}

func (w *testLogWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if !w.muted {
		w.t.Logf("%s", bytes.TrimRight(p, "\n"))
	}
	return len(p), nil
}

func (w *testLogWriter) mute() {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.muted = true
}

// ============================================================
// NewMemoryStateStore — 纯内存 StateStore
// ============================================================

// NewMemoryStateStore 构造 pluginkit.StateStore 的纯内存实现（仅单测用）。
//
// 与 repository 层真实实现的差异：无 DB 持久化、无跨实例广播与对账；
// 回调在 SetEnabled 内非持锁上下文同步交付（满足 StateStore 契约，
// 且让测试在 SetEnabled 返回后即可确定性断言 Manager 状态）。
// 每次 SetEnabled 调用都触发回调（含重复设置同值），以便测试驱动
// Manager 的幂等路径。
func NewMemoryStateStore() pluginkit.StateStore {
	return &memoryStateStore{enabled: make(map[pluginkit.ID]bool)}
}

type memoryStateStore struct {
	mu      sync.Mutex
	enabled map[pluginkit.ID]bool
	subs    []func(pluginkit.StateChange)
}

var _ pluginkit.StateStore = (*memoryStateStore)(nil)

func (s *memoryStateStore) Enabled(id pluginkit.ID) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.enabled[id]
}

func (s *memoryStateStore) SetEnabled(_ context.Context, id pluginkit.ID, enabled bool, _ string) error {
	s.mu.Lock()
	s.enabled[id] = enabled
	subs := slices.Clone(s.subs)
	s.mu.Unlock()
	// 契约：回调在非持锁上下文调用，回调内允许再调 Enabled。
	for _, fn := range subs {
		fn(pluginkit.StateChange{ID: id, Enabled: enabled})
	}
	return nil
}

func (s *memoryStateStore) Subscribe(fn func(pluginkit.StateChange)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.subs = append(s.subs, fn)
}

func (s *memoryStateStore) Close() error { return nil }

// ============================================================
// AssertToggleCycleClean — 启停循环泄漏断言
// ============================================================

// 泄漏断言参数。var 而非 const：本包测试会临时收窄窗口。
var (
	// settleTolerance 是 goroutine 数回落的容差（runtime 自身的
	// finalizer/timer goroutine 会带来少量抖动）。
	settleTolerance = 2
	// settleWindow 是等待 goroutine 数回落的重试窗口（-race 下调度较慢，从宽）。
	settleWindow = 3 * time.Second
	// statusWindow 是等待插件状态收敛的重试窗口（兼容回调异步交付的 StateStore 实现）。
	statusWindow = 5 * time.Second
	// pollInterval 是上述两类等待的轮询间隔。
	pollInterval = 10 * time.Millisecond
)

// AssertToggleCycleClean 对指定插件执行 cycles 轮 enable→disable，断言启停配平且无泄漏：
//   - 每轮 enable 后进入 running（Start 成功、StartedAt 置位、门控放行）；
//   - 每轮 disable 后进入 stopped（Stop 成功、错误与 StartedAt 清零、门控关闭）；
//   - 全部循环结束后 goroutine 数回落到基线（容差 settleTolerance、窗口 settleWindow），
//     证明 Stop 完全回收了后台资源（goroutine/ticker）；
//   - 终态 Snapshot 干净：Enabled=false、State=stopped、无错误、无 StartedAt。
//
// states 必须是驱动 m 的同一个 StateStore（Manager 不对外暴露内部 store，
// 故由调用方显式传入）。插件必须已注册且已 Bootstrap。
func AssertToggleCycleClean(t testing.TB, m *pluginkit.Manager, states pluginkit.StateStore, id pluginkit.ID, cycles int) {
	t.Helper()
	require.GreaterOrEqual(t, cycles, 1, "cycles 至少 1 轮")
	if _, ok := statusOf(m, id); !ok {
		t.Fatalf("插件 %s 未注册于 Manager", id)
	}
	ctx := context.Background()

	// 从确定的非 running 基态出发，goroutine 基线才有意义。
	require.NoError(t, states.SetEnabled(ctx, id, false, "kittest"))
	waitStatus(t, m, id, func(st pluginkit.PluginStatus) bool {
		return st.State != pluginkit.StateRunning
	}, "初始 disable 后应离开 running")
	baseline := settledGoroutineBaseline()

	for i := 1; i <= cycles; i++ {
		require.NoError(t, states.SetEnabled(ctx, id, true, "kittest"), "第 %d 轮 enable", i)
		st := waitStatus(t, m, id, func(st pluginkit.PluginStatus) bool {
			return st.State == pluginkit.StateRunning
		}, "enable 后应进入 running")
		require.Empty(t, st.Err, "第 %d 轮 running 态不应携带错误", i)
		require.NotNil(t, st.StartedAt, "第 %d 轮 running 态应有 StartedAt", i)
		require.True(t, m.GateAllows(id), "第 %d 轮 running 态门控应放行", i)

		require.NoError(t, states.SetEnabled(ctx, id, false, "kittest"), "第 %d 轮 disable", i)
		st = waitStatus(t, m, id, func(st pluginkit.PluginStatus) bool {
			return st.State == pluginkit.StateStopped
		}, "disable 后应进入 stopped")
		require.Empty(t, st.Err, "第 %d 轮 stopped 态不应携带错误", i)
		require.Nil(t, st.StartedAt, "第 %d 轮 stopped 态应清零 StartedAt", i)
		require.False(t, m.GateAllows(id), "第 %d 轮 stopped 态门控应关闭", i)
	}

	// goroutine 数回落断言：带重试窗口吸收 Stop 后的调度延迟。
	deadline := time.Now().Add(settleWindow)
	for {
		n := runtime.NumGoroutine()
		if n <= baseline+settleTolerance {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("插件 %s 经 %d 轮启停后 goroutine 未回落: 基线 %d, 当前 %d（Stop 未完全回收后台资源）",
				id, cycles, baseline, n)
		}
		time.Sleep(pollInterval)
	}

	// 终态 Snapshot 干净。
	st, _ := statusOf(m, id)
	require.False(t, st.Enabled, "终态应为未启用")
	require.Equal(t, pluginkit.StateStopped, st.State, "终态应为 stopped")
	require.Empty(t, st.Err, "终态不应携带错误")
	require.Nil(t, st.StartedAt, "终态不应携带 StartedAt")
}

// waitStatus 轮询 Snapshot 直到指定插件状态满足谓词，超时 Fatalf。
func waitStatus(t testing.TB, m *pluginkit.Manager, id pluginkit.ID, ok func(pluginkit.PluginStatus) bool, msg string) pluginkit.PluginStatus {
	t.Helper()
	deadline := time.Now().Add(statusWindow)
	for {
		st, found := statusOf(m, id)
		if found && ok(st) {
			return st
		}
		if time.Now().After(deadline) {
			t.Fatalf("%s: 等待插件 %s 状态超时, 当前 %+v", msg, id, st)
		}
		time.Sleep(pollInterval)
	}
}

// statusOf 从 Snapshot 中取指定插件的状态。
func statusOf(m *pluginkit.Manager, id pluginkit.ID) (pluginkit.PluginStatus, bool) {
	for _, st := range m.Snapshot() {
		if st.ID == id {
			return st, true
		}
	}
	return pluginkit.PluginStatus{}, false
}

// settledGoroutineBaseline 在短窗口内多次采样 goroutine 数取最小值，
// 吸收进入断言前尚未退出的无关 goroutine 抖动。
func settledGoroutineBaseline() int {
	minN := runtime.NumGoroutine()
	deadline := time.Now().Add(200 * time.Millisecond)
	for time.Now().Before(deadline) {
		time.Sleep(pollInterval)
		if n := runtime.NumGoroutine(); n < minN {
			minN = n
		}
	}
	return minN
}
