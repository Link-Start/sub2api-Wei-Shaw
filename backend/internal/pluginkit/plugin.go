// Package pluginkit 是内建层插件内核：命名空间化的插件契约、宿主能力面、
// DB 驱动的运行时启停状态机与生命周期驱动器。
//
// 设计要点（详见 .claude/plugin-system/PROPOSAL.md 与 phase-2 PHASE_PLAN）：
//   - 编译期装配：插件经 internal/plugins/builtin.go 显式清单注册，无 init() 副作用；
//   - 启停唯一事实源是 DB（plugin_states 表），配置文件只承载插件私有配置；
//   - enable→disable→enable 复用同一实例：构造必须无副作用，Stop 必须完全回收，
//     使实例回到可再次 Init/Start 的状态；
//   - 单插件 Init/Start 失败进 failed 态并隔离，不拖垮宿主启动。
package pluginkit

import (
	"context"
	"fmt"
	"regexp"
	"time"
)

// ID 是插件的全局唯一标识，点分命名空间风格（如 "demo"、"job.backup"）。
// 与前端插件、外部插件共享同一命名空间。
type ID string

var idPattern = regexp.MustCompile(`^[a-z0-9]+([.-][a-z0-9]+)*$`)

// Validate 校验 ID 合法性：小写字母/数字，段间以 . 或 - 分隔，长度 1-64。
func (id ID) Validate() error {
	if len(id) == 0 || len(id) > 64 {
		return fmt.Errorf("pluginkit: invalid plugin id %q: length must be 1-64", id)
	}
	if !idPattern.MatchString(string(id)) {
		return fmt.Errorf("pluginkit: invalid plugin id %q: must match %s", id, idPattern.String())
	}
	return nil
}

// Factory 构造一个插件实例。构造必须无副作用（仅分配内存），
// 一切资源获取与校验放在 Initializer.Init 中。
type Factory func() Plugin

// Plugin 是插件的最小契约。生命周期能力经可选接口（Initializer/Runner/APIProvider）声明。
type Plugin interface {
	ID() ID
}

// Initializer 由需要依赖注入与配置校验的插件实现。
// Init 失败时该插件进入 StateFailed，宿主继续启动其他插件。
// 契约：Init 必须可在 Stop 之后再次调用（同一实例的重启复用同一对象）。
type Initializer interface {
	Init(ctx context.Context, host *Host) error
}

// Runner 由后台常驻型插件实现。
// Start 拉起后台工作；Stop 必须完全回收（goroutine/ticker/连接），
// 且在 Stop 返回后实例可被再次 Init/Start。Stop 受 ctx deadline 约束。
type Runner interface {
	Start(ctx context.Context) error
	Stop(ctx context.Context) error
}

// APIProvider 由自带 HTTP API 的插件实现。
// MountRoutes 仅在宿主 Bootstrap 时调用一次；插件在自己的私有子路由器上
// 注册相对路径，鉴权与启停门控由宿主分发器统一施加，插件无法绕过。
type APIProvider interface {
	MountRoutes(r Router)
}

// State 是插件的运行状态。
type State string

const (
	// StateDisabled 插件未启用（无实例活动）。
	StateDisabled State = "disabled"
	// StateRunning 插件已启用且 Init/Start 成功。
	StateRunning State = "running"
	// StateFailed 插件启用但 Init/Start 失败，或 Stop 超时。
	StateFailed State = "failed"
	// StateStopped 插件曾运行、已被停用且 Stop 成功（与 disabled 的区别仅在于历史；对门控等价）。
	StateStopped State = "stopped"
)

// PluginStatus 是单个插件的状态快照（admin 可观测）。
type PluginStatus struct {
	ID        ID         `json:"id"`
	Enabled   bool       `json:"enabled"`
	State     State      `json:"state"`
	Err       string     `json:"error,omitempty"`
	StartedAt *time.Time `json:"started_at,omitempty"`
}
