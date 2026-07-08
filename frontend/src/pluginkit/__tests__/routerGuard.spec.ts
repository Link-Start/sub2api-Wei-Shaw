/**
 * 路由守卫守护测试（③ fail-closed 门控的真实形态）：
 * 使用真实 @/router（真实 beforeEach 守卫 + 真实路由表），仅 mock 登录态
 * 相关 store 与外部 API。
 *
 *   - 清单含 demo 但未启用/未加载 → /admin/plugins-demo 被拦到 NotFound
 *   - enabled 后同一 URL 直达 PluginDemo
 *   - 守卫只影响带 meta.pluginId 的路由，核心路由不受 enabled 状态影响
 */
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'

import { useEnabledPluginsStore } from '../enabled'

const { authState, appState } = vi.hoisted(() => ({
  authState: {
    isAuthenticated: true,
    isAdmin: true,
    isSimpleMode: false,
    hasPendingAuthSession: false,
    checkAuth: () => {}
  },
  appState: {
    siteName: 'Test',
    cachedPublicSettings: null as Record<string, unknown> | null,
    backendModeEnabled: false
  }
}))

vi.mock('@/stores/auth', () => ({
  useAuthStore: () => authState
}))

vi.mock('@/stores/app', () => ({
  useAppStore: () => appState
}))

vi.mock('@/stores/adminSettings', () => ({
  useAdminSettingsStore: () => ({ customMenuItems: [] })
}))

vi.mock('@/stores/adminCompliance', () => ({
  useAdminComplianceStore: () => ({ initialized: true, fetchStatus: vi.fn() })
}))

vi.mock('@/api/setup', () => ({
  getSetupStatus: vi.fn().mockResolvedValue({ needs_setup: false })
}))

// 预加载会在 afterEach 空闲回调里批量 import 相邻路由组件，测试中禁用
vi.mock('@/composables/useRoutePrefetch', () => ({
  useRoutePrefetch: () => ({
    triggerPrefetch: vi.fn(),
    prefetchedRoutes: { value: new Set() }
  })
}))

// enabled store 使用真实实现（fail-closed 语义正是被测对象），仅 mock 传输层
vi.mock('@/api/client', () => ({
  apiClient: { get: vi.fn().mockRejectedValue({ status: 0, message: 'no network in test' }) },
  default: { get: vi.fn().mockRejectedValue({ status: 0, message: 'no network in test' }) }
}))

async function freshRouter() {
  const { default: router } = await import('@/router')
  return router
}

// freshRouterWithPublicPluginRoute 在 @/router 求值前注入一个
// requiresAuth:false 的插件路由，用于钉住"公开插件路由同样 fail-closed"
async function freshRouterWithPublicPluginRoute() {
  const registry = await import('@/pluginkit/registry')
  registry._setPluginsForTest([
    {
      id: 'pub',
      routes: [
        {
          path: '/plugin-public',
          name: 'PluginPublic',
          component: () => Promise.resolve({ default: { template: '<div />' } }),
          meta: { requiresAuth: false }
        }
      ]
    }
  ])
  const { default: router } = await import('@/router')
  return router
}

beforeEach(() => {
  vi.resetModules()
  setActivePinia(createPinia())
  authState.isAuthenticated = true
  authState.isAdmin = true
})

describe('router 插件门控守卫（fail-closed）', () => {
  it('enabled 清单未加载（loaded=false）→ 插件路由拦到 NotFound，URL 保持不变', async () => {
    const router = await freshRouter()

    await router.push('/admin/plugins-demo')

    expect(router.currentRoute.value.name).toBe('NotFound')
    expect(router.currentRoute.value.path).toBe('/admin/plugins-demo')
  })

  it('清单已加载但 demo 未启用 → 仍拦到 NotFound', async () => {
    const router = await freshRouter()
    const store = useEnabledPluginsStore()
    store.ids = new Set<string>()
    store.loaded = true

    await router.push('/admin/plugins-demo')

    expect(router.currentRoute.value.name).toBe('NotFound')
  })

  it('demo 已启用 → 同一 URL 直达 PluginDemo', async () => {
    const router = await freshRouter()
    const store = useEnabledPluginsStore()
    store.ids = new Set(['demo'])
    store.loaded = true

    await router.push('/admin/plugins-demo')

    expect(router.currentRoute.value.name).toBe('PluginDemo')
    expect(router.currentRoute.value.meta.pluginId).toBe('demo')
  })

  it('守卫只影响带 meta.pluginId 的路由：清单未加载时核心路由不受影响', async () => {
    const router = await freshRouter()

    await router.push('/admin/plugins')

    expect(router.currentRoute.value.name).toBe('AdminPlugins')
  })

  it('登录态判定优先于插件门控：未登录访问插件路由 → 重定向登录页', async () => {
    authState.isAuthenticated = false
    authState.isAdmin = false
    const router = await freshRouter()

    await router.push('/admin/plugins-demo')

    expect(router.currentRoute.value.path).toBe('/login')
    expect(router.currentRoute.value.query.redirect).toBe('/admin/plugins-demo')
  })

  it('requiresAuth:false 的插件路由同样 fail-closed：未启用时不可达', async () => {
    const router = await freshRouterWithPublicPluginRoute()
    const store = useEnabledPluginsStore()
    store.ids = new Set<string>()
    store.loaded = true

    await router.push('/plugin-public')

    expect(router.currentRoute.value.name).not.toBe('PluginPublic')
  })

  it('requiresAuth:false 的插件路由启用后可达（已登录）', async () => {
    const router = await freshRouterWithPublicPluginRoute()
    const store = useEnabledPluginsStore()
    store.ids = new Set(['pub'])
    store.loaded = true

    await router.push('/plugin-public')

    expect(router.currentRoute.value.name).toBe('PluginPublic')
  })

  it('未登录访问未启用的公开插件路由：不可达，且表现与直连未知 URL 一致（无法探测启用状态）', async () => {
    authState.isAuthenticated = false
    authState.isAdmin = false
    const router = await freshRouterWithPublicPluginRoute()

    await router.push('/plugin-public')
    const pluginRouteOutcome = router.currentRoute.value.name
    expect(pluginRouteOutcome).not.toBe('PluginPublic')

    await router.push('/definitely-not-a-route')
    expect(router.currentRoute.value.name).toBe(pluginRouteOutcome)
  })

  it('启停切换后无需刷新：同一会话内 disabled→enabled→disabled 即时生效', async () => {
    const router = await freshRouter()
    const store = useEnabledPluginsStore()

    await router.push('/admin/plugins-demo')
    expect(router.currentRoute.value.name).toBe('NotFound')

    store.ids = new Set(['demo'])
    store.loaded = true
    await router.push({ path: '/admin/plugins-demo', force: true })
    expect(router.currentRoute.value.name).toBe('PluginDemo')

    store.ids = new Set<string>()
    await router.push('/admin/plugins')
    await router.push('/admin/plugins-demo')
    expect(router.currentRoute.value.name).toBe('NotFound')
  })
})
