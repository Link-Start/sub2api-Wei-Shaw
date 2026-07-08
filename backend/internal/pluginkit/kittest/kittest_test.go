//go:build unit

package kittest

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/pluginkit"
	"github.com/Wei-Shaw/sub2api/internal/plugins"

	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

func newManager(t *testing.T, store pluginkit.StateStore, factories ...pluginkit.Factory) *pluginkit.Manager {
	t.Helper()
	deps := pluginkit.HostDeps{Logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	m, err := pluginkit.NewManager(deps, store, nil, factories)
	require.NoError(t, err)
	return m
}

// ============================================================
// NewHost
// ============================================================

func TestNewHost_Defaults(t *testing.T) {
	host := NewHost(t)
	require.NotNil(t, host.Logger)
	require.Nil(t, host.DB)
	require.Nil(t, host.Redis)

	// 默认 Config 等价于未配置：零值成功，不触碰 out。
	out := struct {
		Name string `mapstructure:"name"`
	}{Name: "keep"}
	require.NoError(t, host.Config(&out))
	require.Equal(t, "keep", out.Name)

	// 默认 logger 可用（输出到 t.Logf）。
	host.Logger.Info("kittest_host_smoke", "ok", true)
}

func TestNewHost_Options(t *testing.T) {
	cfg, err := pluginkit.ParsePluginsConfig(map[string]any{
		"demo": map[string]any{"name": "hola", "interval": "5s"},
	})
	require.NoError(t, err)

	db := ent.NewClient()
	rdb := redis.NewClient(&redis.Options{Addr: "127.0.0.1:0"})
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	host := NewHost(t,
		WithDB(db),
		WithRedis(rdb),
		WithLogger(logger),
		WithConfig(cfg.DecoderFor("demo")),
	)
	require.Same(t, db, host.DB)
	require.Same(t, rdb, host.Redis)
	require.Same(t, logger, host.Logger)

	var out struct {
		Name     string        `mapstructure:"name"`
		Interval time.Duration `mapstructure:"interval"`
	}
	require.NoError(t, host.Config(&out))
	require.Equal(t, "hola", out.Name)
	require.Equal(t, 5*time.Second, out.Interval)
}

// ============================================================
// NewMemoryStateStore
// ============================================================

func TestMemoryStateStore_EnabledDefaultFalse(t *testing.T) {
	store := NewMemoryStateStore()
	require.False(t, store.Enabled("unknown"), "无记录 = disabled")
}

func TestMemoryStateStore_SetEnabledAndSubscribe(t *testing.T) {
	store := NewMemoryStateStore()
	var mu sync.Mutex
	var got []pluginkit.StateChange
	var snapshotInCallback []bool
	store.Subscribe(func(ch pluginkit.StateChange) {
		mu.Lock()
		defer mu.Unlock()
		got = append(got, ch)
		// 契约：回调在非持锁上下文调用，回调内允许再调 Enabled。
		snapshotInCallback = append(snapshotInCallback, store.Enabled(ch.ID))
	})

	ctx := context.Background()
	require.NoError(t, store.SetEnabled(ctx, "demo", true, "t"))
	require.True(t, store.Enabled("demo"))
	require.NoError(t, store.SetEnabled(ctx, "demo", false, "t"))
	require.False(t, store.Enabled("demo"))
	// 同值重复设置也逐次触发回调（驱动 Manager 幂等路径）。
	require.NoError(t, store.SetEnabled(ctx, "demo", false, "t"))

	mu.Lock()
	defer mu.Unlock()
	require.Equal(t, []pluginkit.StateChange{
		{ID: "demo", Enabled: true},
		{ID: "demo", Enabled: false},
		{ID: "demo", Enabled: false},
	}, got)
	require.Equal(t, []bool{true, false, false}, snapshotInCallback,
		"回调触发时内存快照已更新")
}

func TestMemoryStateStore_MultipleSubscribers(t *testing.T) {
	store := NewMemoryStateStore()
	var mu sync.Mutex
	calls := map[string]int{}
	for _, name := range []string{"a", "b"} {
		store.Subscribe(func(pluginkit.StateChange) {
			mu.Lock()
			calls[name]++
			mu.Unlock()
		})
	}
	require.NoError(t, store.SetEnabled(context.Background(), "demo", true, "t"))
	mu.Lock()
	defer mu.Unlock()
	require.Equal(t, map[string]int{"a": 1, "b": 1}, calls,
		"每个订阅者对同一 StateChange 恰好触发一次")
}

func TestMemoryStateStore_CloseIdempotent(t *testing.T) {
	store := NewMemoryStateStore()
	require.NoError(t, store.Close())
	require.NoError(t, store.Close())
}

func TestMemoryStateStore_Concurrent(t *testing.T) {
	store := NewMemoryStateStore()
	store.Subscribe(func(ch pluginkit.StateChange) { _ = store.Enabled(ch.ID) })
	var wg sync.WaitGroup
	for g := 0; g < 8; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			id := pluginkit.ID("a")
			if g%2 == 0 {
				id = "b"
			}
			for i := 0; i < 50; i++ {
				_ = store.SetEnabled(context.Background(), id, i%2 == 0, "t")
				_ = store.Enabled(id)
			}
		}(g)
	}
	wg.Wait()
	require.NoError(t, store.Close())
}

// ============================================================
// AssertToggleCycleClean
// ============================================================

// cleanRunnerPlugin 是行为良好的后台插件：Start 拉起 ticker goroutine，
// Stop 取消并等待其完全退出。
type cleanRunnerPlugin struct {
	id     pluginkit.ID
	mu     sync.Mutex
	cancel context.CancelFunc
	done   chan struct{}
}

func (p *cleanRunnerPlugin) ID() pluginkit.ID { return p.id }

func (p *cleanRunnerPlugin) Start(_ context.Context) error {
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	p.mu.Lock()
	p.cancel, p.done = cancel, done
	p.mu.Unlock()
	go func() {
		defer close(done)
		ticker := time.NewTicker(5 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
	return nil
}

func (p *cleanRunnerPlugin) Stop(_ context.Context) error {
	p.mu.Lock()
	cancel, done := p.cancel, p.done
	p.cancel, p.done = nil, nil
	p.mu.Unlock()
	if cancel == nil {
		return nil
	}
	cancel()
	<-done
	return nil
}

// leakyPlugin 故意违反 Stop 完全回收契约：Start 泄漏 goroutine，Stop 不回收。
// 泄漏的 goroutine 阻塞在 release 上，由测试 Cleanup 统一释放。
type leakyPlugin struct {
	id      pluginkit.ID
	release chan struct{}
}

func (p *leakyPlugin) ID() pluginkit.ID { return p.id }

func (p *leakyPlugin) Start(_ context.Context) error {
	for i := 0; i < 4; i++ {
		go func() { <-p.release }()
	}
	return nil
}

func (p *leakyPlugin) Stop(_ context.Context) error { return nil }

func TestAssertToggleCycleClean_CleanRunnerPasses(t *testing.T) {
	store := NewMemoryStateStore()
	p := &cleanRunnerPlugin{id: "clean"}
	m := newManager(t, store, func() pluginkit.Plugin { return p })
	require.NoError(t, m.Bootstrap(context.Background()))

	AssertToggleCycleClean(t, m, store, "clean", 3)
}

// recordTB 捕获断言失败而不使宿主测试失败，用于验证泄漏断言真的能报警。
type recordTB struct {
	testing.TB
	mu    sync.Mutex
	fatal bool
	msgs  []string
}

func (r *recordTB) Helper() {}

func (r *recordTB) Errorf(format string, args ...any) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.msgs = append(r.msgs, fmt.Sprintf(format, args...))
}

func (r *recordTB) Fatalf(format string, args ...any) {
	r.mu.Lock()
	r.fatal = true
	r.msgs = append(r.msgs, fmt.Sprintf(format, args...))
	r.mu.Unlock()
	runtime.Goexit()
}

func (r *recordTB) FailNow() {
	r.mu.Lock()
	r.fatal = true
	r.mu.Unlock()
	runtime.Goexit()
}

func (r *recordTB) failedWith() (bool, string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.fatal, fmt.Sprint(r.msgs)
}

func TestAssertToggleCycleClean_DetectsLeak(t *testing.T) {
	// 收窄回落窗口，让泄漏测试快速到达超时分支。
	origWindow := settleWindow
	settleWindow = 300 * time.Millisecond
	t.Cleanup(func() { settleWindow = origWindow })

	store := NewMemoryStateStore()
	p := &leakyPlugin{id: "leaky", release: make(chan struct{})}
	t.Cleanup(func() { close(p.release) })
	m := newManager(t, store, func() pluginkit.Plugin { return p })
	require.NoError(t, m.Bootstrap(context.Background()))

	rec := &recordTB{TB: t}
	done := make(chan struct{})
	// Fatalf 经 runtime.Goexit 终止执行，须在独立 goroutine 中驱动断言。
	go func() {
		defer close(done)
		AssertToggleCycleClean(rec, m, store, "leaky", 1)
	}()
	<-done

	fatal, msgs := rec.failedWith()
	require.True(t, fatal, "泄漏插件应触发 Fatalf")
	require.Contains(t, msgs, "goroutine 未回落")
}

// ============================================================
// Builtin 清单健康检查
// ============================================================

// Builtin 清单可被 NewManager 完整实例化：Factory 无副作用、
// ID 合法且互不重复（校验逻辑在 NewManager 内），默认全部 disabled。
func TestBuiltinManifest(t *testing.T) {
	m := newManager(t, NewMemoryStateStore(), plugins.Builtin()...)
	snap := m.Snapshot()
	require.Len(t, snap, 1, "Phase-2 清单仅含 demo")
	require.Equal(t, pluginkit.ID("demo"), snap[0].ID)
	require.False(t, snap[0].Enabled)
	require.Equal(t, pluginkit.StateDisabled, snap[0].State)
}
