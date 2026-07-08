//go:build unit

package service

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

// I-2.5 结算分支不变量（Phase-1 回归防线）。
// 条目编号对应 .claude/plugin-system/phases/phase-1_safety-net/INVARIANTS.md。
//
// 锁定 finalizePostUsageBilling（gateway_usage_billing.go）当前实现的三路分支语义：
//  1. IsSubscriptionBill=true 且 ActualCost>0 且 GroupID!=nil
//     → 只入订阅用量队列 QueueUpdateSubscriptionUsage(user, group, ActualCost)，绝不碰余额；
//     GroupID=nil 时订阅用量静默不结算，也不会回退到余额扣减（现状怪癖，一并锁定）。
//  2. IsSubscriptionBill=false 且 ActualCost>0
//     → 余额路径二选一：result.NewBalance 高于资格阈值时异步 QueueDeductBalance(ActualCost)；
//     低于阈值（<=0，或 0<NewBalance<MinimumBalanceReserve）时同步 InvalidateUserBalance；
//     result=nil（legacy 无 NewBalance 信息）时回退异步扣减。
//  3. Platform 配额累加仅在余额模式发生（订阅模式豁免）：同步 Redis incr + 异步 DB 直写。
//  4. APIKey 限流用量计量（QueueUpdateAPIKeyRateLimitUsage）与计费分支无关，两种模式都发生。
//  5. ActualCost=0（免费账号）→ 订阅/余额/限流三路均不结算；
//     ScheduleLastUsedUpdate 与分支无关、无条件发生。
//
// 观察方法：真实 BillingCacheService（NewBillingCacheService + 录参 cache stub），
// svc.Stop() 关闭写队列并等待 worker 全部退出，使异步队列副作用可确定性断言；
// 同步副作用（InvalidateUserBalance / 平台配额 Redis incr）在 finalize 返回后立即断言。
// 期望值为手工写死的固定金额（成本值全程透传、无算术），断言容差 <=1e-10。

type e2eSubUsageCall struct {
	userID  int64
	groupID int64
	cost    float64
}

type e2eDeductCall struct {
	userID int64
	amount float64
}

type e2eRateLimitCall struct {
	keyID int64
	cost  float64
}

type e2ePlatformIncrCall struct {
	userID   int64
	platform string
	cost     float64
}

// e2eBillingCacheRecorder 复用 billingCacheWorkerStub（billing_cache_service_test.go）
// 的 BillingCache 全量实现，仅覆写 I-2.5 关心的观察点并录参。
type e2eBillingCacheRecorder struct {
	billingCacheWorkerStub

	mu            sync.Mutex
	subUsage      []e2eSubUsageCall
	deducts       []e2eDeductCall
	invalidations []int64
	rateLimits    []e2eRateLimitCall
	platformIncrs []e2ePlatformIncrCall
}

func (r *e2eBillingCacheRecorder) UpdateSubscriptionUsage(_ context.Context, userID, groupID int64, cost float64) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.subUsage = append(r.subUsage, e2eSubUsageCall{userID: userID, groupID: groupID, cost: cost})
	return nil
}

func (r *e2eBillingCacheRecorder) DeductUserBalance(_ context.Context, userID int64, amount float64) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.deducts = append(r.deducts, e2eDeductCall{userID: userID, amount: amount})
	return nil
}

func (r *e2eBillingCacheRecorder) InvalidateUserBalance(_ context.Context, userID int64) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.invalidations = append(r.invalidations, userID)
	return nil
}

func (r *e2eBillingCacheRecorder) UpdateAPIKeyRateLimitUsage(_ context.Context, keyID int64, cost float64) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.rateLimits = append(r.rateLimits, e2eRateLimitCall{keyID: keyID, cost: cost})
	return nil
}

func (r *e2eBillingCacheRecorder) IncrUserPlatformQuotaUsageCache(_ context.Context, userID int64, platform string, cost float64, _ time.Duration, _ bool) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.platformIncrs = append(r.platformIncrs, e2ePlatformIncrCall{userID: userID, platform: platform, cost: cost})
	return nil
}

func (r *e2eBillingCacheRecorder) snapSubUsage() []e2eSubUsageCall {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]e2eSubUsageCall(nil), r.subUsage...)
}

func (r *e2eBillingCacheRecorder) snapDeducts() []e2eDeductCall {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]e2eDeductCall(nil), r.deducts...)
}

func (r *e2eBillingCacheRecorder) snapInvalidations() []int64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]int64(nil), r.invalidations...)
}

func (r *e2eBillingCacheRecorder) snapRateLimits() []e2eRateLimitCall {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]e2eRateLimitCall(nil), r.rateLimits...)
}

func (r *e2eBillingCacheRecorder) snapPlatformIncrs() []e2ePlatformIncrCall {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]e2ePlatformIncrCall(nil), r.platformIncrs...)
}

// e2eQuotaRepoRecorder 复用 fakeQuotaRepo（billing_cache_service_user_platform_quota_test.go），
// 仅覆写 IncrementUsageWithReset 录参（finalize 的 DB 直写 goroutine 落点）。
type e2eQuotaRepoRecorder struct {
	fakeQuotaRepo

	mu    sync.Mutex
	incrs []e2ePlatformIncrCall
}

func (r *e2eQuotaRepoRecorder) IncrementUsageWithReset(_ context.Context, userID int64, platform string, cost float64, _ time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.incrs = append(r.incrs, e2ePlatformIncrCall{userID: userID, platform: platform, cost: cost})
	return nil
}

func (r *e2eQuotaRepoRecorder) snapIncrs() []e2ePlatformIncrCall {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]e2ePlatformIncrCall(nil), r.incrs...)
}

// requireE2EOneOrNone 断言某观察点恰好发生 want 指定的一次调用（want=nil 表示绝不发生）。
func requireE2EOneOrNone[T any](t *testing.T, op string, want *T, got []T, check func(t *testing.T, want, got T)) {
	t.Helper()
	if want == nil {
		require.Empty(t, got, "%s 不应发生", op)
		return
	}
	require.Len(t, got, 1, "%s 应恰好发生一次", op)
	check(t, *want, got[0])
}

// TestBillingInvariant_FinalizeUsageSettlementBranches 锁定 I-2.5：
// usage 结算时 IsSubscriptionBill 三路分支的外部可观测副作用。
func TestBillingInvariant_FinalizeUsageSettlementBranches(t *testing.T) {
	const (
		userID    = int64(11)
		apiKeyID  = int64(21)
		accountID = int64(31)
	)
	groupID := int64(7)
	newBalance := func(v float64) *float64 { return &v }

	type wants struct {
		subUsage     *e2eSubUsageCall
		deduct       *e2eDeductCall
		invalidated  bool // InvalidateUserBalance(userID) 恰好一次
		rateLimit    *e2eRateLimitCall
		platformIncr *e2ePlatformIncrCall
		dbIncr       *e2ePlatformIncrCall
	}

	tests := []struct {
		name           string
		isSubscription bool
		groupID        *int64
		totalCost      float64
		actualCost     float64
		platform       string
		rateLimit5h    float64
		reserve        float64 // cfg.Billing.MinimumBalanceReserve
		result         *UsageBillingApplyResult
		want           wants
	}{
		{
			// I-2.5 分支1：订阅结算只入订阅用量队列（金额=ActualCost），不碰余额；
			// 即使 Platform 非空且 quota repo 可用，平台配额也豁免；限流计量照常。
			name:           "I-2.5 subscription bill queues subscription usage only",
			isSubscription: true,
			groupID:        &groupID,
			totalCost:      0.25,
			actualCost:     0.375, // 0.25 × 1.5 倍率，验证入队金额取 ActualCost 而非 TotalCost
			platform:       "anthropic",
			rateLimit5h:    5,
			result:         &UsageBillingApplyResult{Applied: true},
			want: wants{
				subUsage:  &e2eSubUsageCall{userID: userID, groupID: groupID, cost: 0.375},
				rateLimit: &e2eRateLimitCall{keyID: apiKeyID, cost: 0.375},
			},
		},
		{
			// I-2.5 分支1怪癖：订阅模式但 GroupID=nil → 订阅用量静默不入队，
			// 且不会回退到余额扣减（if/else 互斥，余额分支不评估）。
			name:           "I-2.5 subscription bill without group id settles nothing",
			isSubscription: true,
			groupID:        nil,
			totalCost:      0.25,
			actualCost:     0.375,
			result:         &UsageBillingApplyResult{Applied: true},
			want:           wants{},
		},
		{
			// I-2.5 分支2正常路径：NewBalance 高于阈值 → 异步 QueueDeductBalance(ActualCost)，
			// 不失效缓存；订阅用量不入队；限流计量照常。
			name:        "I-2.5 balance bill above threshold queues deduction",
			totalCost:   0.25,
			actualCost:  0.25,
			rateLimit5h: 5,
			result:      &UsageBillingApplyResult{Applied: true, NewBalance: newBalance(5.0)},
			want: wants{
				deduct:    &e2eDeductCall{userID: userID, amount: 0.25},
				rateLimit: &e2eRateLimitCall{keyID: apiKeyID, cost: 0.25},
			},
		},
		{
			// I-2.5 分支2低余额路径：NewBalance<=0 → 同步 InvalidateUserBalance，
			// 不再入扣减队列（二选一，绝不同时）。
			name:       "I-2.5 balance bill exhausted invalidates instead of deducting",
			totalCost:  0.75,
			actualCost: 0.75,
			result:     &UsageBillingApplyResult{Applied: true, NewBalance: newBalance(-0.25), BalanceOverdrafted: true},
			want:       wants{invalidated: true},
		},
		{
			// I-2.5 分支2低余额路径（保留金变体）：0 < NewBalance < MinimumBalanceReserve
			// 同样判定为低于资格阈值 → 失效缓存而非扣减。
			name:       "I-2.5 balance bill below minimum reserve invalidates",
			totalCost:  0.495,
			actualCost: 0.495,
			reserve:    0.01,
			result:     &UsageBillingApplyResult{Applied: true, NewBalance: newBalance(0.005)},
			want:       wants{invalidated: true},
		},
		{
			// I-2.5 分支2 legacy 回退：result=nil（无 NewBalance 信息）→ 仍走异步扣减。
			name:       "I-2.5 balance bill with nil result falls back to queue deduction",
			totalCost:  0.25,
			actualCost: 0.25,
			result:     nil,
			want:       wants{deduct: &e2eDeductCall{userID: userID, amount: 0.25}},
		},
		{
			// I-2.5 免费账号（multiplier=0 → ActualCost=0）：余额模式下三路均不结算，
			// 即使 TotalCost>0、限流窗口已配置。
			name:        "I-2.5 zero actual cost settles nothing in balance mode",
			totalCost:   0.5,
			actualCost:  0,
			platform:    "anthropic",
			rateLimit5h: 5,
			result:      &UsageBillingApplyResult{Applied: true},
			want:        wants{},
		},
		{
			// I-2.5 免费账号订阅模式：同样不入订阅用量队列。
			name:           "I-2.5 zero actual cost settles nothing in subscription mode",
			isSubscription: true,
			groupID:        &groupID,
			totalCost:      0.5,
			actualCost:     0,
			rateLimit5h:    5,
			result:         &UsageBillingApplyResult{Applied: true},
			want:           wants{},
		},
		{
			// I-2.5 分支3：余额模式 + Platform 非空 + quota repo 可用 + 有 limit（stub 缺省
			// fail-safe 返回 true）→ 同步 Redis incr(ActualCost) + 异步 DB IncrementUsageWithReset。
			name:       "I-2.5 balance bill accrues platform quota",
			totalCost:  0.6,
			actualCost: 0.6,
			platform:   "anthropic",
			result:     &UsageBillingApplyResult{Applied: true, NewBalance: newBalance(5.0)},
			want: wants{
				deduct:       &e2eDeductCall{userID: userID, amount: 0.6},
				platformIncr: &e2ePlatformIncrCall{userID: userID, platform: "anthropic", cost: 0.6},
				dbIncr:       &e2ePlatformIncrCall{userID: userID, platform: "anthropic", cost: 0.6},
			},
		},
	}

	checkSubUsage := func(t *testing.T, want, got e2eSubUsageCall) {
		t.Helper()
		require.Equal(t, want.userID, got.userID)
		require.Equal(t, want.groupID, got.groupID)
		require.InDelta(t, want.cost, got.cost, billingInvariantEps)
	}
	checkDeduct := func(t *testing.T, want, got e2eDeductCall) {
		t.Helper()
		require.Equal(t, want.userID, got.userID)
		require.InDelta(t, want.amount, got.amount, billingInvariantEps)
	}
	checkRateLimit := func(t *testing.T, want, got e2eRateLimitCall) {
		t.Helper()
		require.Equal(t, want.keyID, got.keyID)
		require.InDelta(t, want.cost, got.cost, billingInvariantEps)
	}
	checkPlatformIncr := func(t *testing.T, want, got e2ePlatformIncrCall) {
		t.Helper()
		require.Equal(t, want.userID, got.userID)
		require.Equal(t, want.platform, got.platform)
		require.InDelta(t, want.cost, got.cost, billingInvariantEps)
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cache := &e2eBillingCacheRecorder{}
			quotaRepo := &e2eQuotaRepoRecorder{}
			cfg := &config.Config{}
			cfg.Billing.MinimumBalanceReserve = tc.reserve
			svc := NewBillingCacheService(cache, nil, nil, nil, nil, nil, cfg, nil)

			deferred := &DeferredService{}
			deps := &billingDeps{
				billingCacheService:   svc,
				deferredService:       deferred,
				userPlatformQuotaRepo: quotaRepo,
				// cfg=nil：finalize 的平台配额 DB 分支按 "flusher 未启用" 处理（降级直写 DB）
			}
			p := &postUsageBillingParams{
				Cost:               &CostBreakdown{TotalCost: tc.totalCost, ActualCost: tc.actualCost},
				User:               &User{ID: userID},
				APIKey:             &APIKey{ID: apiKeyID, GroupID: tc.groupID, RateLimit5h: tc.rateLimit5h},
				Account:            &Account{ID: accountID},
				IsSubscriptionBill: tc.isSubscription,
				Platform:           tc.platform,
			}
			if tc.isSubscription {
				p.Subscription = &UserSubscription{ID: 42}
			}

			finalizePostUsageBilling(context.Background(), p, deps, tc.result)

			// 同步副作用：finalize 返回即已发生（或确定未发生）。
			// 平台配额 Redis incr 是同步直写；InvalidateUserBalance 是同步调用。
			requireE2EOneOrNone(t, "platform quota redis incr", tc.want.platformIncr, cache.snapPlatformIncrs(), checkPlatformIncr)
			if tc.want.invalidated {
				require.Equal(t, []int64{userID}, cache.snapInvalidations(), "低于阈值应同步失效余额缓存")
			} else {
				require.Empty(t, cache.snapInvalidations(), "InvalidateUserBalance 不应发生")
			}

			// 关闭写队列并等待 worker 全部退出：异步队列副作用确定性落地后再断言。
			svc.Stop()

			requireE2EOneOrNone(t, "subscription usage queue", tc.want.subUsage, cache.snapSubUsage(), checkSubUsage)
			requireE2EOneOrNone(t, "balance deduction queue", tc.want.deduct, cache.snapDeducts(), checkDeduct)
			requireE2EOneOrNone(t, "api key rate limit queue", tc.want.rateLimit, cache.snapRateLimits(), checkRateLimit)

			// 平台配额 DB 直写走独立 goroutine：正向用 Eventually 等待；
			// 反向依赖 "同步 incr 未发生 ⇒ 守卫分支未进入 ⇒ goroutine 根本未启动"，可立即断言。
			if tc.want.dbIncr != nil {
				require.Eventually(t, func() bool {
					return len(quotaRepo.snapIncrs()) == 1
				}, 2*time.Second, 10*time.Millisecond, "平台配额 DB 直写应异步发生")
				checkPlatformIncr(t, *tc.want.dbIncr, quotaRepo.snapIncrs()[0])
			} else {
				require.Empty(t, quotaRepo.snapIncrs(), "平台配额 DB 直写不应发生")
			}

			// ScheduleLastUsedUpdate 与计费分支无关，无条件发生。
			_, scheduled := deferred.lastUsedUpdates.Load(accountID)
			require.True(t, scheduled, "last_used 调度应与分支无关、无条件发生")
		})
	}
}
