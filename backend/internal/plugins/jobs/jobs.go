// Package jobs 收编既有后台常驻 worker 为 job.* 命名空间的内建插件。
//
// 迁移工序（Phase-5 建立，供后续 worker 复制）：
//  1. worker 的业务逻辑（扫描循环）保留在 service 包内，一行不改；
//  2. 本包为每个 worker 提供一个 Runner 型插件壳，Start 时新建一个 worker
//     实例并启动、Stop 时停止——壳被复用，内层 worker 每轮重建，因此无需
//     改造 worker 自身非可重入的 stopOnce/stopCh；
//  3. 插件声明 DefaultEnabled()=true：迁移前 worker 一直在运行，迁移后必须
//     默认开启以零行为变更；管理员可经插件系统运行时启停（免重启）；
//  4. Wire 经 Factories(JobDeps) 提供工厂闭包（捕获 worker 所需的 ports），
//     追加到内建装配清单；worker 从 Wire graph 的启动/清理链中摘除。
//
// 依赖方向：jobs → service（单向，取 worker 类型与其 ports 接口）；service
// 不 import 本包，无环。
package jobs

import (
	"context"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pluginkit"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

// JobDeps 是构造全部 job 插件所需的 ports 依赖（由 Wire 装配注入）。
type JobDeps struct {
	AccountRepo     service.AccountRepository
	ProxyRepo       service.ProxyRepository
	IdempotencyRepo service.IdempotencyRepository
	Config          *config.Config
}

// Factories 返回本包全部 job 插件的工厂闭包，追加到 plugins.Builtin()。
func Factories(deps JobDeps) []pluginkit.Factory {
	return []pluginkit.Factory{
		func() pluginkit.Plugin { return &accountExpiryJob{repo: deps.AccountRepo, interval: time.Minute} },
		func() pluginkit.Plugin { return &proxyExpiryJob{repo: deps.ProxyRepo, interval: time.Minute} },
		func() pluginkit.Plugin { return &idempotencyCleanupJob{repo: deps.IdempotencyRepo, cfg: deps.Config} },
	}
}

// worker 是本包壳插件共享的最小抽象：可启停的后台 worker。
type worker interface {
	Start()
	Stop()
}

// ── job.account-expiry ───────────────────────────────────────────────────

type accountExpiryJob struct {
	repo     service.AccountRepository
	interval time.Duration
	w        worker
}

func (j *accountExpiryJob) ID() pluginkit.ID     { return "job.account-expiry" }
func (j *accountExpiryJob) DefaultEnabled() bool { return true }

func (j *accountExpiryJob) Start(context.Context) error {
	w := service.NewAccountExpiryService(j.repo, j.interval)
	w.Start()
	j.w = w
	return nil
}

func (j *accountExpiryJob) Stop(context.Context) error {
	if j.w != nil {
		j.w.Stop()
		j.w = nil
	}
	return nil
}

// ── job.proxy-expiry ─────────────────────────────────────────────────────

type proxyExpiryJob struct {
	repo     service.ProxyRepository
	interval time.Duration
	w        worker
}

func (j *proxyExpiryJob) ID() pluginkit.ID     { return "job.proxy-expiry" }
func (j *proxyExpiryJob) DefaultEnabled() bool { return true }

func (j *proxyExpiryJob) Start(context.Context) error {
	w := service.NewProxyExpiryService(j.repo, j.interval)
	w.Start()
	j.w = w
	return nil
}

func (j *proxyExpiryJob) Stop(context.Context) error {
	if j.w != nil {
		j.w.Stop()
		j.w = nil
	}
	return nil
}

// ── job.idempotency-cleanup ──────────────────────────────────────────────

type idempotencyCleanupJob struct {
	repo service.IdempotencyRepository
	cfg  *config.Config
	w    worker
}

func (j *idempotencyCleanupJob) ID() pluginkit.ID     { return "job.idempotency-cleanup" }
func (j *idempotencyCleanupJob) DefaultEnabled() bool { return true }

func (j *idempotencyCleanupJob) Start(context.Context) error {
	w := service.NewIdempotencyCleanupService(j.repo, j.cfg)
	w.Start()
	j.w = w
	return nil
}

func (j *idempotencyCleanupJob) Stop(context.Context) error {
	if j.w != nil {
		j.w.Stop()
		j.w = nil
	}
	return nil
}
