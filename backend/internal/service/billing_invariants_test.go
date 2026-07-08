//go:build unit

package service

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// 计费不变量测试（Phase-1 回归防线）。
// 条目编号对应 .claude/plugin-system/phases/phase-1_safety-net/INVARIANTS.md。
// 期望值为手工推算的固定金额：任何计算链路的行为漂移都会在此处显形。

const billingInvariantEps = 1e-10

// I-2.4 倍率叠加语义：serviceTier 倍率作用于各维度成本（体现在 breakdown 分项与
// TotalCost 中），rateMultiplier 只作用于 ActualCost；TotalCost 不含 rateMultiplier。
func TestBillingInvariant_TierScalesDimensionsRateScalesTotal(t *testing.T) {
	svc := newTestBillingService()
	pricing := &ModelPricing{
		InputPricePerToken:         1e-6,
		OutputPricePerToken:        2e-6,
		CacheReadPricePerToken:     5e-7,
		CacheCreationPricePerToken: 2e-6,
	}
	tokens := UsageTokens{
		InputTokens:         1000,
		OutputTokens:        500,
		CacheReadTokens:     2000,
		CacheCreationTokens: 300,
	}

	bd := svc.computeTokenBreakdown(pricing, tokens, 3.0, "flex", false)

	// flex 倍率 0.5 逐维度生效
	require.InDelta(t, 5e-4, bd.InputCost, billingInvariantEps)         // 1000×1e-6×0.5
	require.InDelta(t, 5e-4, bd.OutputCost, billingInvariantEps)        // 500×2e-6×0.5
	require.InDelta(t, 5e-4, bd.CacheReadCost, billingInvariantEps)     // 2000×5e-7×0.5
	require.InDelta(t, 3e-4, bd.CacheCreationCost, billingInvariantEps) // 300×2e-6×0.5
	// TotalCost = 分项之和，不含 rateMultiplier
	require.InDelta(t, 1.8e-3, bd.TotalCost, billingInvariantEps)
	// ActualCost = TotalCost × rateMultiplier
	require.InDelta(t, 5.4e-3, bd.ActualCost, billingInvariantEps)
}

// I-2.4 priority 专属定价优先于 tier 倍率：
//   - 配置了任一 priority 单价 → 已配置维度用 priority 单价、未配置维度保持标准单价，
//     且全维度不再叠加 2.0 倍率；
//   - 未配置 priority 单价 → 全维度标准单价 × 2.0；
//   - tier 归一化：大小写与首尾空白不敏感。
func TestBillingInvariant_PriorityPricingPrecedesTierMultiplier(t *testing.T) {
	svc := newTestBillingService()
	tokens := UsageTokens{InputTokens: 1000, OutputTokens: 1000, CacheReadTokens: 1000}

	t.Run("priority prices configured", func(t *testing.T) {
		pricing := &ModelPricing{
			InputPricePerToken:          1e-6,
			InputPricePerTokenPriority:  1.5e-6,
			OutputPricePerToken:         2e-6,
			OutputPricePerTokenPriority: 3e-6,
			CacheReadPricePerToken:      5e-7, // 未配置 priority 档
		}
		bd := svc.computeTokenBreakdown(pricing, tokens, 1.0, "priority", false)
		require.InDelta(t, 1.5e-3, bd.InputCost, billingInvariantEps)   // priority 单价，无 ×2
		require.InDelta(t, 3e-3, bd.OutputCost, billingInvariantEps)    // priority 单价，无 ×2
		require.InDelta(t, 5e-4, bd.CacheReadCost, billingInvariantEps) // 标准单价，也无 ×2
		require.InDelta(t, 5e-3, bd.TotalCost, billingInvariantEps)
	})

	t.Run("no priority prices falls back to 2x multiplier", func(t *testing.T) {
		pricing := &ModelPricing{
			InputPricePerToken:  1e-6,
			OutputPricePerToken: 2e-6,
		}
		bd := svc.computeTokenBreakdown(pricing, tokens, 1.0, "priority", false)
		require.InDelta(t, 2e-3, bd.InputCost, billingInvariantEps)  // ×2
		require.InDelta(t, 4e-3, bd.OutputCost, billingInvariantEps) // ×2
		require.InDelta(t, 6e-3, bd.TotalCost, billingInvariantEps)
	})

	t.Run("tier name normalization", func(t *testing.T) {
		pricing := &ModelPricing{
			InputPricePerToken:         1e-6,
			InputPricePerTokenPriority: 1.5e-6,
		}
		in := UsageTokens{InputTokens: 1000}
		bd := svc.computeTokenBreakdown(pricing, in, 1.0, "  PRIORITY  ", false)
		require.InDelta(t, 1.5e-3, bd.InputCost, billingInvariantEps)
	})

	t.Run("unknown tier means 1x", func(t *testing.T) {
		pricing := &ModelPricing{InputPricePerToken: 1e-6}
		in := UsageTokens{InputTokens: 1000}
		bd := svc.computeTokenBreakdown(pricing, in, 1.0, "batch-nonsense", false)
		require.InDelta(t, 1e-3, bd.InputCost, billingInvariantEps)
	})
}

// I-2.4 rateMultiplier=0（免费账号）→ ActualCost=0 但 TotalCost 保留；
// 负数（缓存/迁移残留）钳制为 0，绝不按 1x 误扣。
func TestBillingInvariant_RateMultiplierZeroAndNegative(t *testing.T) {
	svc := newTestBillingService()
	pricing := &ModelPricing{InputPricePerToken: 1e-6, OutputPricePerToken: 2e-6}
	tokens := UsageTokens{InputTokens: 1000, OutputTokens: 500}

	t.Run("zero", func(t *testing.T) {
		bd := svc.computeTokenBreakdown(pricing, tokens, 0, "", false)
		require.InDelta(t, 2e-3, bd.TotalCost, billingInvariantEps)
		require.Zero(t, bd.ActualCost)
	})

	t.Run("negative clamped to zero", func(t *testing.T) {
		bd := svc.computeTokenBreakdown(pricing, tokens, -5, "", false)
		require.InDelta(t, 2e-3, bd.TotalCost, billingInvariantEps)
		require.Zero(t, bd.ActualCost)
	})
}

// I-2.2 缓存创建两档计价：
//   - 支持明细且配置两档价 → 5m/1h 分别按各自单价；
//   - 支持明细但 API 未返回 ephemeral 明细 → 全量按 5m 单价（防少计）；
//   - 不支持明细 → 标准 CacheCreationPricePerToken。
func TestBillingInvariant_CacheCreationTwoTierPricing(t *testing.T) {
	svc := newTestBillingService()

	t.Run("breakdown with both tiers", func(t *testing.T) {
		pricing := &ModelPricing{
			SupportsCacheBreakdown: true,
			CacheCreation5mPrice:   2e-6,
			CacheCreation1hPrice:   4e-6,
		}
		tokens := UsageTokens{CacheCreation5mTokens: 1000, CacheCreation1hTokens: 500}
		cost := svc.computeCacheCreationCost(pricing, tokens, 1.0)
		require.InDelta(t, 4e-3, cost, billingInvariantEps) // 1000×2e-6 + 500×4e-6
	})

	t.Run("no ephemeral detail falls back to 5m price for all", func(t *testing.T) {
		pricing := &ModelPricing{
			SupportsCacheBreakdown: true,
			CacheCreation5mPrice:   2e-6,
			CacheCreation1hPrice:   4e-6,
		}
		tokens := UsageTokens{CacheCreationTokens: 1500}
		cost := svc.computeCacheCreationCost(pricing, tokens, 1.0)
		require.InDelta(t, 3e-3, cost, billingInvariantEps) // 1500×2e-6
	})

	t.Run("breakdown unsupported uses standard price", func(t *testing.T) {
		pricing := &ModelPricing{
			SupportsCacheBreakdown:     false,
			CacheCreationPricePerToken: 3e-6,
			CacheCreation5mPrice:       2e-6, // 即使配置了也不生效
		}
		tokens := UsageTokens{CacheCreationTokens: 1000}
		cost := svc.computeCacheCreationCost(pricing, tokens, 1.0)
		require.InDelta(t, 3e-3, cost, billingInvariantEps) // 1000×3e-6
	})
}
