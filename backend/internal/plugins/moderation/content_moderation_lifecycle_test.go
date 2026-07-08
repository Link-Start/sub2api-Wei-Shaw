package moderation

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TestContentModerationService_StopReclaimsWorkers 锁定插件化引入的最小生命
// 周期契约：构造即拉起的审查 worker 与清理 worker 必须被 Stop 完全回收——
// Stop 仅在全部后台 goroutine 退出后返回 nil（content-moderation 插件停用
// 依赖该语义，泄漏会被 kittest 启停循环断言拒收）。
func TestContentModerationService_StopReclaimsWorkers(t *testing.T) {
	svc := NewContentModerationService(
		&contentModerationTestSettingRepo{values: map[string]string{}},
		&contentModerationTestRepo{},
		&contentModerationTestHashCache{},
		nil,
		nil,
		nil,
		nil,
	)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	start := time.Now()
	require.NoError(t, svc.Stop(ctx), "全部 worker 应在限时内退出")
	require.Less(t, time.Since(start), 3*time.Second, "worker 空转等待上限 1s，Stop 不应长时间阻塞")

	// 幂等：重复 Stop 直接返回。
	require.NoError(t, svc.Stop(context.Background()))
}

// TestContentModerationService_StopOnZeroValueIsNoop 零值实例（未经构造函数，
// 无 stopCh/worker）上 Stop 必须安全返回——特征化测试与调用方防御路径依赖零值可用。
func TestContentModerationService_StopOnZeroValueIsNoop(t *testing.T) {
	var nilSvc *ContentModerationService
	require.NoError(t, nilSvc.Stop(context.Background()))
	require.NoError(t, (&ContentModerationService{}).Stop(context.Background()))
}
