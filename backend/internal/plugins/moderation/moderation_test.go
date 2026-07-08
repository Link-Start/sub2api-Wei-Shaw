//go:build unit

package moderation

import (
	"context"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pluginkit"

	"github.com/stretchr/testify/require"
)

// TestPlugin_Contract 锁定内容审计插件壳的契约：ID 合法、默认启用、Runner，
// 且 Start/Stop 往返绑定/解绑句柄。nil 依赖下服务构造不 spawn worker
// （NewContentModerationService 对 nil settingRepo/repo 有守卫），
// 此处只验证壳的生命周期与句柄语义；服务业务逻辑由其自身测试覆盖。
func TestPlugin_Contract(t *testing.T) {
	handle := NewContentModerationHandle()
	p := New(Deps{Handle: handle})()

	require.NoError(t, p.ID().Validate())
	require.Equal(t, PluginID, p.ID())

	de, ok := p.(pluginkit.DefaultEnabler)
	require.True(t, ok)
	require.True(t, de.DefaultEnabled(), "迁移型插件必须默认启用（零行为变更）")

	r, ok := p.(pluginkit.Runner)
	require.True(t, ok)

	// 同实例启停循环：每轮 Start 绑定新服务实例，Stop 解绑并回收。
	for i := 0; i < 3; i++ {
		require.Nil(t, handle.Get(), "round %d: 启动前句柄应为空", i)
		require.NoError(t, r.Start(context.Background()))
		require.NotNil(t, handle.Get(), "round %d: 启动后句柄应绑定", i)

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		require.NoError(t, r.Stop(ctx))
		cancel()
		require.Nil(t, handle.Get(), "round %d: 停止后句柄应解绑", i)
	}

	// 重复 Start 拒绝（Manager 串行保证下不应发生，防御性锁定）。
	require.NoError(t, r.Start(context.Background()))
	require.Error(t, r.Start(context.Background()))
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	require.NoError(t, r.Stop(ctx))

	// Stop 幂等：未启动时返回 nil 且不 panic。
	require.NoError(t, r.Stop(context.Background()))
}
