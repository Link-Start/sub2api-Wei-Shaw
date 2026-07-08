// Package moderation 把"内容审计"（Content Moderation，前端名"风控中心"）
// 作为一个完整功能模块收编为内建插件（Phase-6 首个功能域竖切）。
//
// 竖切机制（区别于 jobs 包的单 worker 壳）：
//   - 服务实现（审查/封号/异步 worker/清理/输入提取/邮件/脱敏）整体居于本包
//     （content_moderation*.go，自 service 包纯移动搬入）；对 service 包的
//     依赖只剩 ports/模型与 EmailService 导出出口，service 不反向 import 本包；
//   - 热路径（网关挂钩点）与 8 个 /admin/risk-control/* 端点经
//     ContentModerationHandle（原子句柄）解析服务：插件 Start 时
//     Bind 新建实例、Stop 时 Unbind 并完全回收 worker——启用 = 迁移前行为，
//     停用 = 热路径直通 + 后台端点 404 + worker 退出（功能整体熄灭）；
//   - DefaultEnabled()=true：迁移前服务总是构造，迁移后必须默认启用以零行为变更；
//   - 业务配置仍在 DB setting 表（content_moderation_config / risk_control_enabled），
//     插件无私有配置子树。
package moderation

import (
	"context"
	"errors"

	"github.com/Wei-Shaw/sub2api/internal/pluginkit"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

// PluginID 是内容审计插件在全局命名空间中的 ID。
const PluginID pluginkit.ID = "content-moderation"

// Deps 是构造内容审计服务所需的全部依赖（由 Wire 装配注入闭包）。
type Deps struct {
	SettingRepo          service.SettingRepository
	Repo                 ContentModerationRepository
	HashCache            ContentModerationHashCache
	GroupRepo            service.GroupRepository
	UserRepo             service.UserRepository
	AuthCacheInvalidator service.APIKeyAuthCacheInvalidator
	EmailService         *service.EmailService
	// Handle 是热路径与后台 handler 持有的可切换引用，Start/Stop 时绑定/解绑。
	Handle *ContentModerationHandle
}

// New 返回内容审计插件的工厂闭包（追加到 plugins.Builtin() 装配清单）。
func New(deps Deps) pluginkit.Factory {
	return func() pluginkit.Plugin { return &Plugin{deps: deps} }
}

// Plugin 是内容审计插件壳。壳被 Manager 复用（enable→disable→enable 同一实例），
// 内层服务每轮 Start 重建，规避服务自身 stopOnce 不可重入。
type Plugin struct {
	deps Deps
	svc  *ContentModerationService
}

var (
	_ pluginkit.Plugin         = (*Plugin)(nil)
	_ pluginkit.Runner         = (*Plugin)(nil)
	_ pluginkit.DefaultEnabler = (*Plugin)(nil)
)

// ID 返回插件 ID。
func (p *Plugin) ID() pluginkit.ID { return PluginID }

// DefaultEnabled 声明默认启用：迁移前内容审计服务总是随宿主构造运行，
// 迁移后保持默认开启以零行为变更（Bootstrap 自播种，显式停用不被翻开）。
func (p *Plugin) DefaultEnabled() bool { return true }

// Start 新建内容审计服务（构造即拉起异步审查 worker 与清理 worker）并绑定句柄，
// 此后热路径与后台端点立即可见。
func (p *Plugin) Start(context.Context) error {
	if p.svc != nil {
		return errors.New("moderation: already started")
	}
	svc := NewContentModerationService(
		p.deps.SettingRepo,
		p.deps.Repo,
		p.deps.HashCache,
		p.deps.GroupRepo,
		p.deps.UserRepo,
		p.deps.AuthCacheInvalidator,
		p.deps.EmailService,
	)
	p.svc = svc
	p.deps.Handle.Bind(svc)
	return nil
}

// Stop 先解绑句柄（新请求立即直通/404），再停止服务并等待 worker 完全退出
// （受宿主 Stop 超时约束）。已解析到旧实例的在途请求自然完成，不受影响。
func (p *Plugin) Stop(ctx context.Context) error {
	p.deps.Handle.Unbind()
	svc := p.svc
	p.svc = nil
	if svc == nil {
		return nil
	}
	return svc.Stop(ctx)
}
