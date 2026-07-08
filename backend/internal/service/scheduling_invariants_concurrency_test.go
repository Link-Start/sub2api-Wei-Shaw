//go:build unit

// Phase-1 回归防线（TASK-004）：并发槽获取-释放配平不变量锁定（I-4.7）。
// 正常路径的 ReleaseDecrements 已由 concurrency_service_test.go 覆盖；
// 本文件补齐失败路径 / 多次 Release / nil-ReleaseFunc / 无限并发旁路的配平语义。
//
// 诚实可断言范围（读 concurrency_service.go AcquireAccountSlot/AcquireUserSlot 推导）：
//   - Acquired=false 时 ReleaseFunc 为 nil（调用方无释放义务，且无槽位被占）；
//   - Acquire 出错时不产生任何释放义务；
//   - maxConcurrency<=0 时完全旁路缓存（acquire/release 均零缓存流量）；
//   - ReleaseFunc 每次调用都会向缓存下发同 requestID 的 Release（服务层不做幂等，
//     幂等性依赖 Redis ZREM 语义——以 Characterization 锁定该现状）；
//   - 缓存 Release 报错被吞掉（日志告警，不 panic 不上抛）。
//
// I-4.8（5s 兜底释放后台 goroutine）时序敏感，按 INVARIANTS 登记为 [不覆盖]。
package service

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

// countingSlotCache 复用既有 stubConcurrencyCacheForTest（concurrency_service_test.go），
// 仅追加 acquire 调用计数与 user 槽位释放记录，用于严格配平断言。
type countingSlotCache struct {
	stubConcurrencyCacheForTest

	accountAcquireCalls   int
	accountAcquireLastReq string
	userAcquireCalls      int
	userAcquireLastReq    string

	releasedUserIDs        []int64
	releasedUserRequestIDs []string
}

func (c *countingSlotCache) AcquireAccountSlot(_ context.Context, _ int64, _ int, requestID string) (bool, error) {
	c.accountAcquireCalls++
	c.accountAcquireLastReq = requestID
	return c.acquireResult, c.acquireErr
}

func (c *countingSlotCache) AcquireUserSlot(_ context.Context, _ int64, _ int, requestID string) (bool, error) {
	c.userAcquireCalls++
	c.userAcquireLastReq = requestID
	return c.acquireResult, c.acquireErr
}

func (c *countingSlotCache) ReleaseUserSlot(_ context.Context, userID int64, requestID string) error {
	c.releasedUserIDs = append(c.releasedUserIDs, userID)
	c.releasedUserRequestIDs = append(c.releasedUserRequestIDs, requestID)
	return c.releaseErr
}

// TestInvariant_I47_AccountSlotAcquireFailure_NoReleaseObligation 锁定失败路径配平（I-4.7）：
// 抢槽失败（缓存返回 false）时 Acquired=false 且 ReleaseFunc 为 nil——
// 无槽位被占，也不给调用方任何释放义务；缓存端口零 Release 流量。
func TestInvariant_I47_AccountSlotAcquireFailure_NoReleaseObligation(t *testing.T) {
	cache := &countingSlotCache{}
	cache.acquireResult = false
	svc := NewConcurrencyService(cache)

	result, err := svc.AcquireAccountSlot(context.Background(), 1, 5)
	require.NoError(t, err)
	require.False(t, result.Acquired)
	require.Nil(t, result.ReleaseFunc, "I-4.7: 未获得槽位时 ReleaseFunc 必须为 nil")
	require.Equal(t, 1, cache.accountAcquireCalls)
	require.Empty(t, cache.releasedAccountIDs, "I-4.7: 抢槽失败不得产生 Release 流量")
}

// TestInvariant_I47_AccountSlotAcquireError_NoSlotNoRelease 锁定缓存错误路径配平（I-4.7）：
// AcquireAccountSlot 缓存报错时错误上抛、result 为 nil，无任何释放义务残留。
func TestInvariant_I47_AccountSlotAcquireError_NoSlotNoRelease(t *testing.T) {
	cacheErr := errors.New("redis down")
	cache := &countingSlotCache{}
	cache.acquireErr = cacheErr
	svc := NewConcurrencyService(cache)

	result, err := svc.AcquireAccountSlot(context.Background(), 1, 5)
	require.ErrorIs(t, err, cacheErr)
	require.Nil(t, result, "I-4.7: 缓存错误时不得返回半初始化的 AcquireResult")
	require.Empty(t, cache.releasedAccountIDs)
}

// TestInvariant_I47_UnlimitedConcurrencyBypassesCache 锁定无限并发旁路配平（I-4.7）：
// maxConcurrency<=0 时 Acquired=true、ReleaseFunc 为非 nil 的 no-op，
// acquire 与 release 全程零缓存流量（配平在旁路上同样严格成立：0 获取 = 0 释放）。
func TestInvariant_I47_UnlimitedConcurrencyBypassesCache(t *testing.T) {
	for _, maxConcurrency := range []int{0, -1} {
		cache := &countingSlotCache{}
		svc := NewConcurrencyService(cache)

		result, err := svc.AcquireAccountSlot(context.Background(), 1, maxConcurrency)
		require.NoError(t, err)
		require.True(t, result.Acquired, "maxConcurrency=%d 应视为无限制", maxConcurrency)
		require.NotNil(t, result.ReleaseFunc, "maxConcurrency=%d 的 ReleaseFunc 应为 no-op 而非 nil", maxConcurrency)
		require.Equal(t, 0, cache.accountAcquireCalls, "I-4.7: 无限并发不得触碰缓存获取槽位")

		result.ReleaseFunc()
		require.Empty(t, cache.releasedAccountIDs, "I-4.7: no-op ReleaseFunc 不得触碰缓存")
	}
}

// TestInvariant_I47_AccountSlotReleaseBalance_SameRequestID 锁定获取-释放身份配平（I-4.7）：
// 成功获取后调用一次 ReleaseFunc，缓存端口必须收到恰好一次 Release，
// 且释放的 requestID 与获取时下发的 requestID 完全一致（释放的就是抢到的那个槽）。
func TestInvariant_I47_AccountSlotReleaseBalance_SameRequestID(t *testing.T) {
	cache := &countingSlotCache{}
	cache.acquireResult = true
	svc := NewConcurrencyService(cache)

	result, err := svc.AcquireAccountSlot(context.Background(), 42, 5)
	require.NoError(t, err)
	require.True(t, result.Acquired)
	require.Equal(t, 1, cache.accountAcquireCalls)
	require.NotEmpty(t, cache.accountAcquireLastReq)

	result.ReleaseFunc()

	require.Equal(t, []int64{42}, cache.releasedAccountIDs, "I-4.7: 1 次获取必须恰好配平 1 次释放")
	require.Equal(t, []string{cache.accountAcquireLastReq}, cache.releasedRequestIDs,
		"I-4.7: 释放的 requestID 必须与获取时一致")
}

// TestCharacterization_I47_ReleaseFuncNotIdempotent 锁定现状怪癖（I-4.7）：
// 服务层 ReleaseFunc 不做幂等保护，每次调用都会向缓存重复下发同 requestID 的 Release；
// 实际幂等性依赖 Redis ZREM 的天然幂等 + handler 层 wrapReleaseOnDone 的 once 包装。
// （若未来服务层加了幂等保护，本测试变红提示语义变化，需同步评估 handler 层包装是否冗余。）
func TestCharacterization_I47_ReleaseFuncNotIdempotent(t *testing.T) {
	cache := &countingSlotCache{}
	cache.acquireResult = true
	svc := NewConcurrencyService(cache)

	result, err := svc.AcquireAccountSlot(context.Background(), 42, 5)
	require.NoError(t, err)
	require.True(t, result.Acquired)

	result.ReleaseFunc()
	result.ReleaseFunc()

	require.Equal(t, []int64{42, 42}, cache.releasedAccountIDs,
		"Characterization: 二次调用 ReleaseFunc 当前会重复下发 Release")
	require.Len(t, cache.releasedRequestIDs, 2)
	require.Equal(t, cache.releasedRequestIDs[0], cache.releasedRequestIDs[1],
		"Characterization: 重复释放使用同一 requestID（Redis ZREM 幂等兜底）")
}

// TestInvariant_I47_ReleaseErrorSwallowed_NoPanic 锁定释放失败路径（I-4.7）：
// 缓存 Release 报错时 ReleaseFunc 只记日志、不 panic 不上抛；
// 释放尝试仍恰好下发一次（槽位泄漏由缓存侧 TTL 清理兜底，见 I-4.8 不覆盖说明）。
func TestInvariant_I47_ReleaseErrorSwallowed_NoPanic(t *testing.T) {
	cache := &countingSlotCache{}
	cache.acquireResult = true
	cache.releaseErr = errors.New("redis down")
	svc := NewConcurrencyService(cache)

	result, err := svc.AcquireAccountSlot(context.Background(), 42, 5)
	require.NoError(t, err)
	require.True(t, result.Acquired)

	require.NotPanics(t, result.ReleaseFunc, "I-4.7: 释放失败必须被吞掉，不得 panic")
	require.Equal(t, []int64{42}, cache.releasedAccountIDs, "I-4.7: 释放尝试仍必须恰好下发一次")
}

// TestInvariant_I47_UserSlotBalanceParity 锁定用户槽位与账号槽位的配平对称性（I-4.7）：
// 失败路径 ReleaseFunc 为 nil；成功路径 1 获取配平 1 释放且 requestID 一致。
func TestInvariant_I47_UserSlotBalanceParity(t *testing.T) {
	t.Run("抢槽失败无释放义务", func(t *testing.T) {
		cache := &countingSlotCache{}
		cache.acquireResult = false
		svc := NewConcurrencyService(cache)

		result, err := svc.AcquireUserSlot(context.Background(), 100, 3)
		require.NoError(t, err)
		require.False(t, result.Acquired)
		require.Nil(t, result.ReleaseFunc)
		require.Empty(t, cache.releasedUserIDs)
	})

	t.Run("成功路径严格配平", func(t *testing.T) {
		cache := &countingSlotCache{}
		cache.acquireResult = true
		svc := NewConcurrencyService(cache)

		result, err := svc.AcquireUserSlot(context.Background(), 100, 3)
		require.NoError(t, err)
		require.True(t, result.Acquired)
		require.Equal(t, 1, cache.userAcquireCalls)

		result.ReleaseFunc()

		require.Equal(t, []int64{100}, cache.releasedUserIDs)
		require.Equal(t, []string{cache.userAcquireLastReq}, cache.releasedUserRequestIDs,
			"I-4.7: 用户槽位释放的 requestID 必须与获取时一致")
	})
}
