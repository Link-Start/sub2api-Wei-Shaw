//go:build unit

package jobs

import (
	"context"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pluginkit"

	"github.com/stretchr/testify/require"
)

// TestFactories_Contract 锁定 job 插件壳的契约：ID 合法且唯一、声明默认启用、
// 实现 Runner。用 nil 依赖构造——worker 的 Start 对 nil repo 有守卫（不 spawn），
// 故此处只验证壳的生命周期契约；worker 扫描逻辑由其自身测试与真实环境冒烟覆盖。
func TestFactories_Contract(t *testing.T) {
	factories := Factories(JobDeps{})
	require.Len(t, factories, 3)

	seen := map[pluginkit.ID]bool{}
	for _, f := range factories {
		p := f()
		require.NoError(t, p.ID().Validate(), "job id must be valid: %s", p.ID())
		require.False(t, seen[p.ID()], "duplicate job id: %s", p.ID())
		seen[p.ID()] = true

		de, ok := p.(pluginkit.DefaultEnabler)
		require.True(t, ok, "%s must implement DefaultEnabler", p.ID())
		require.True(t, de.DefaultEnabled(), "%s must default to enabled (zero-behavior migration)", p.ID())

		r, ok := p.(pluginkit.Runner)
		require.True(t, ok, "%s must implement Runner", p.ID())

		// 同实例启停循环：nil repo 下 worker.Start 守卫为 no-op，壳仍应干净往返。
		for i := 0; i < 3; i++ {
			require.NoError(t, r.Start(context.Background()))
			require.NoError(t, r.Stop(context.Background()))
		}
	}

	require.Equal(t, map[pluginkit.ID]bool{
		"job.account-expiry":      true,
		"job.proxy-expiry":        true,
		"job.idempotency-cleanup": true,
	}, seen)
}
