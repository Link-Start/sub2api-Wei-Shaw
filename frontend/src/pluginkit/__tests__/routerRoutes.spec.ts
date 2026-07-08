/**
 * 路由名单守护测试（②）：
 * 清单清空时，路由表与"未引入插件系统"的基线完全一致（本阶段核心新增仅
 * AdminPlugins 插件管理页一条，已计入基线）；默认清单相对基线的增量恰为
 * demo 插件路由一条，且带 meta.pluginId。
 */
import { beforeEach, describe, expect, it, vi } from 'vitest'

// 每个用例重置模块缓存：registry 的注入需发生在 @/router 模块求值
// （静态路由表展开 pluginRoutes()）之前。
beforeEach(() => {
  vi.resetModules()
})

// routeSignatures 以 "name:path" 唯一标识每条路由记录（含别名与 redirect 记录）
function routeSignatures(router: {
  getRoutes(): Array<{ name?: unknown; path: string }>
}): string[] {
  return router
    .getRoutes()
    .map((route) => `${String(route.name ?? '')}:${route.path}`)
    .sort()
}

async function importRouterWithPlugins(plugins: 'empty' | 'default') {
  const registry = await import('@/pluginkit/registry')
  if (plugins === 'empty') {
    registry._setPluginsForTest([])
  }
  const { default: router } = await import('@/router')
  return router
}

/**
 * 核心路由基线（插件清单为空时的完整路由名单）。
 * ⚠️ 本清单是"零行为变更"的锚点：新增核心路由需在此显式登记；
 * 插件路由绝不允许出现在这里（应经 builtinPlugins 装配）。
 */
const CORE_ROUTE_SIGNATURES = [
  ':/',
  ':/admin',
  ':/admin/affiliates',
  ':/admin/channels',
  'AdminAccounts:/admin/accounts',
  'AdminAffiliateInvites:/admin/affiliates/invites',
  'AdminAffiliateRebates:/admin/affiliates/rebates',
  'AdminAffiliateTransfers:/admin/affiliates/transfers',
  'AdminAnnouncements:/admin/announcements',
  'AdminChannelMonitor:/admin/channels/monitor',
  'AdminChannels:/admin/channels/pricing',
  'AdminDashboard:/admin/dashboard',
  'AdminGroups:/admin/groups',
  'AdminOps:/admin/ops',
  'AdminOrders:/admin/orders',
  'AdminPaymentDashboard:/admin/orders/dashboard',
  'AdminPaymentPlans:/admin/orders/plans',
  'AdminPlugins:/admin/plugins',
  'AdminPromoCodes:/admin/promo-codes',
  'AdminProxies:/admin/proxies',
  'AdminRedeem:/admin/redeem',
  'AdminRiskControl:/admin/risk-control',
  'AdminSettings:/admin/settings',
  'AdminSubscriptions:/admin/subscriptions',
  'AdminUsage:/admin/usage',
  'AdminUsers:/admin/users',
  'Affiliate:/affiliate',
  'AirwallexPayment:/payment/airwallex',
  'BatchImageGuide:/batch-image',
  'BatchImageGuide:/docs/batch-image',
  'ChannelStatus:/monitor',
  'CustomPage:/custom/:id',
  'Dashboard:/dashboard',
  'DingTalkOAuthCallback:/auth/dingtalk/callback',
  'EmailVerify:/email-verify',
  'ForgotPassword:/forgot-password',
  'Home:/home',
  'KeyUsage:/key-usage',
  'Keys:/keys',
  'LegalDocument:/legal/:documentId',
  'LinuxDoOAuthCallback:/auth/linuxdo/callback',
  'Login:/login',
  'NotFound:/:pathMatch(.*)*',
  'OAuthCallback:/auth/callback',
  'OAuthCallback:/auth/oauth/callback',
  'OIDCOAuthCallback:/auth/oidc/callback',
  'OrderList:/orders',
  'PaymentQRCode:/payment/qrcode',
  'PaymentResult:/payment/result',
  'Profile:/profile',
  'PurchaseSubscription:/purchase',
  'Redeem:/redeem',
  'Register:/register',
  'ResetPassword:/reset-password',
  'Setup:/setup',
  'StripePayment:/payment/stripe',
  'StripePopup:/payment/stripe-popup',
  'Subscriptions:/subscriptions',
  'Usage:/usage',
  'UserAvailableChannels:/available-channels',
  'WeChatOAuthCallback:/auth/wechat/callback',
  'WeChatPaymentOAuthCallback:/auth/wechat/payment/callback',
  'dingtalk-email-completion:/auth/dingtalk/email-completion'
]

describe('router 路由名单（插件系统守护）', () => {
  it('清单清空 → 路由名单与核心基线逐条一致，且无任何 meta.pluginId', async () => {
    const router = await importRouterWithPlugins('empty')

    expect(routeSignatures(router)).toEqual(CORE_ROUTE_SIGNATURES)
    for (const route of router.getRoutes()) {
      expect(route.meta.pluginId).toBeUndefined()
    }
  })

  it('默认清单 → 相对基线的增量恰为 demo 插件路由一条（带 meta.pluginId）', async () => {
    const router = await importRouterWithPlugins('default')
    const signatures = routeSignatures(router)

    const extras = signatures.filter((sig) => !CORE_ROUTE_SIGNATURES.includes(sig))
    expect(extras).toEqual(['PluginDemo:/admin/plugins-demo'])
    expect(signatures.filter((sig) => CORE_ROUTE_SIGNATURES.includes(sig))).toEqual(
      CORE_ROUTE_SIGNATURES
    )

    const pluginGated = router.getRoutes().filter((route) => route.meta.pluginId !== undefined)
    expect(pluginGated.map((route) => `${String(route.name)}:${route.meta.pluginId}`)).toEqual([
      'PluginDemo:demo'
    ])
  })
})
