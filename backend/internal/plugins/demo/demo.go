// Package demo 是内建插件的最小教学样例：演示一个插件如何组合 pluginkit
// 的全部扩展面（Initializer / Runner / APIProvider），可作为新插件的起点模板。
//
// 面向插件作者的关键契约（完整定义见 internal/pluginkit/plugin.go）：
//   - Factory（本包的 New）构造必须无副作用，只分配内存；
//     资源获取、配置解码与校验全部放到 Init；
//   - enable→disable→enable 复用同一实例：Init 必须可在 Stop 之后再次调用，
//     Start 拉起的后台资源（goroutine/ticker/连接）必须由 Stop 完全回收；
//   - 后台 goroutine 的生命周期不挂在 Start 的 ctx 上（那是宿主的启动上下文），
//     而是由插件自持 cancel、在 Stop 中显式终止并等待退出；
//   - MountRoutes 仅在宿主 Bootstrap 时调用一次，注册相对路径；
//     对外路径、鉴权中间件与启停门控由宿主分发器统一施加，
//     插件无需（也不能）自查 enabled 状态。
package demo

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pluginkit"

	"github.com/gin-gonic/gin"
)

// PluginID 是 demo 插件在全局命名空间中的 ID。
const PluginID pluginkit.ID = "demo"

// 未配置时的默认值：配置子树整体缺省 = 全默认；显式配置非法值 = Init 报错。
const (
	defaultGreeting = "hello from demo plugin"
	defaultInterval = 30 * time.Second
)

// Config 是 demo 插件的私有配置，对应配置文件的 plugins.demo 子树。
// 字段用 mapstructure 标签，由宿主经 Host.Config 解码（语义对齐主配置）。
type Config struct {
	// Greeting 是心跳日志与 /hello 接口返回的问候语；不允许为空白。
	Greeting string `mapstructure:"greeting"`
	// Interval 是心跳间隔（如 "30s"、"1m"）；必须为正。
	Interval time.Duration `mapstructure:"interval"`
}

// New 是 demo 插件的 Factory，签名必须匹配 pluginkit.Factory。
// 只分配内存，无任何副作用。
func New() pluginkit.Plugin { return &Plugin{} }

// Plugin 是 demo 插件实例。所有可变字段由 mu 保护：
// 生命周期方法（Init/Start/Stop）由 Manager 按插件串行调用，
// 但 HTTP handler 可能与下一轮 Init 并发（在途请求 vs 重启），不能裸读。
type Plugin struct {
	mu        sync.Mutex
	host      *pluginkit.Host
	cfg       Config
	startedAt time.Time
	cancel    context.CancelFunc // 心跳 goroutine 的终止开关；nil = 未启动
	done      chan struct{}      // 心跳 goroutine 退出信号，Stop 据此等待完全回收
}

// 编译期断言：声明本插件实现的全部扩展面。
var (
	_ pluginkit.Plugin      = (*Plugin)(nil)
	_ pluginkit.Initializer = (*Plugin)(nil)
	_ pluginkit.Runner      = (*Plugin)(nil)
	_ pluginkit.APIProvider = (*Plugin)(nil)
)

// ID 返回插件 ID。
func (p *Plugin) ID() pluginkit.ID { return PluginID }

// Init 解码并校验私有配置，保存宿主能力面。
// 失败时插件进入 failed 态（不影响宿主与其他插件）；
// 可在 Stop 之后被再次调用（同一实例重启）。
func (p *Plugin) Init(_ context.Context, host *pluginkit.Host) error {
	// 先填默认值再解码：配置中未出现的字段保持默认。
	cfg := Config{
		Greeting: defaultGreeting,
		Interval: defaultInterval,
	}
	if err := host.Config(&cfg); err != nil {
		return err
	}
	if strings.TrimSpace(cfg.Greeting) == "" {
		return errors.New("demo: greeting must not be blank")
	}
	if cfg.Interval <= 0 {
		return fmt.Errorf("demo: interval must be positive, got %s", cfg.Interval)
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.host = host
	p.cfg = cfg
	return nil
}

// Start 拉起心跳 goroutine。注意用 context.Background 派生后台上下文：
// 心跳的生命周期由 Stop 终止，与宿主传入的启动 ctx 无关。
func (p *Plugin) Start(_ context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.cancel != nil {
		return errors.New("demo: already started")
	}
	ctx, cancel := context.WithCancel(context.Background())
	p.cancel = cancel
	p.done = make(chan struct{})
	p.startedAt = time.Now()
	// 心跳所需数据以参数传入，goroutine 不回读实例字段，避免与重启期间的写入竞争。
	go heartbeat(ctx, p.done, p.host.Logger, p.cfg)
	return nil
}

// Stop 终止心跳并等待 goroutine 完全退出（受 ctx deadline 约束，
// 宿主默认给 10s）。未启动时幂等返回 nil。
func (p *Plugin) Stop(ctx context.Context) error {
	p.mu.Lock()
	cancel, done := p.cancel, p.done
	p.cancel, p.done = nil, nil
	p.mu.Unlock()
	if cancel == nil {
		return nil
	}
	cancel()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("demo: waiting for heartbeat to exit: %w", ctx.Err())
	}
}

// heartbeat 按固定间隔打结构化心跳日志，直到 ctx 取消。
// ticker 在函数退出时释放，done 关闭即代表全部后台资源已回收。
func heartbeat(ctx context.Context, done chan<- struct{}, logger *slog.Logger, cfg Config) {
	defer close(done)
	ticker := time.NewTicker(cfg.Interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			logger.Info("demo_heartbeat", "greeting", cfg.Greeting)
		}
	}
}

// MountRoutes 在插件私有子路由器上注册相对路径。
// 对外经宿主分发器暴露为 GET /api/v1/admin/plugins/demo/api/hello，
// AdminAuth 与启停门控由宿主统一施加。
func (p *Plugin) MountRoutes(r pluginkit.Router) {
	r.Admin().GET("/hello", p.handleHello)
}

// handleHello 返回问候语与最近一次启动以来的运行秒数。
func (p *Plugin) handleHello(c *gin.Context) {
	p.mu.Lock()
	greeting := p.cfg.Greeting
	startedAt := p.startedAt
	p.mu.Unlock()
	c.JSON(http.StatusOK, gin.H{
		"greeting":       greeting,
		"uptime_seconds": int64(time.Since(startedAt) / time.Second),
	})
}
