/**
 * 侧边栏接入守护测试（③ nav 形态）：
 *   - "插件管理"是核心项：不 gated，admin 恒可见
 *   - 插件贡献的导航项按 enabled 门控：未启用/未加载不渲染（fail-closed），
 *     启用后出现且 labelKey 经 t() 解析
 */
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { ref } from 'vue'
import { RouterLinkStub, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'

import AppSidebar from '@/components/layout/AppSidebar.vue'
import { useEnabledPluginsStore } from '@/pluginkit/enabled'

vi.mock('@/stores', () => ({
  useAppStore: () => ({
    sidebarCollapsed: false,
    mobileOpen: false,
    siteName: 'Test',
    siteLogo: '',
    siteVersion: '',
    publicSettingsLoaded: true,
    cachedPublicSettings: null,
    backendModeEnabled: false,
    sidebarScrollTop: 0,
    toggleSidebar: vi.fn(),
    setMobileOpen: vi.fn()
  }),
  useAuthStore: () => ({
    isAdmin: true,
    isSimpleMode: false
  }),
  useOnboardingStore: () => ({
    isCurrentStep: () => false,
    nextStep: vi.fn()
  }),
  useAdminSettingsStore: () => ({
    fetch: vi.fn(),
    customMenuItems: [],
    opsMonitoringEnabled: false,
    paymentEnabled: false
  })
}))

vi.mock('vue-router', async () => {
  const actual = await vi.importActual<typeof import('vue-router')>('vue-router')
  return {
    ...actual,
    useRoute: () => ({ path: '/admin/dashboard' }),
    useRouter: () => ({ push: vi.fn() })
  }
})

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({ t: (key: string) => key })
  }
})

vi.mock('@/composables/useBatchImageAccess', () => ({
  useBatchImageAccess: () => ({
    canUseBatchImage: ref(false),
    refreshBatchImageAccess: vi.fn()
  })
}))

// featureFlags 走真实 pinia app store（cachedPublicSettings 未加载 → 宽容显示），
// 与被 mock 的 '@/stores'（组件内直接使用的实例）互不影响。
vi.mock('@/api/client', () => ({
  apiClient: { get: vi.fn().mockRejectedValue({ status: 0 }) },
  default: { get: vi.fn().mockRejectedValue({ status: 0 }) }
}))

function mountSidebar() {
  return mount(AppSidebar, {
    global: {
      stubs: {
        RouterLink: RouterLinkStub,
        VersionBadge: true
      }
    }
  })
}

function navTargets(wrapper: ReturnType<typeof mountSidebar>): string[] {
  return wrapper
    .findAllComponents(RouterLinkStub)
    .map((link) => String(link.props('to')))
}

beforeEach(() => {
  setActivePinia(createPinia())
})

describe('AppSidebar 插件接入', () => {
  it('插件管理是核心项：enabled 清单未加载时也渲染', () => {
    const wrapper = mountSidebar()
    expect(navTargets(wrapper)).toContain('/admin/plugins')
    wrapper.unmount()
  })

  it('demo 未启用（清单未加载，fail-closed）→ 不渲染插件导航项', () => {
    const wrapper = mountSidebar()
    expect(navTargets(wrapper)).not.toContain('/admin/plugins-demo')
    wrapper.unmount()
  })

  it('demo 已启用 → 渲染插件导航项，labelKey 经 t() 解析', async () => {
    const store = useEnabledPluginsStore()
    store.ids = new Set(['demo'])
    store.loaded = true

    const wrapper = mountSidebar()
    expect(navTargets(wrapper)).toContain('/admin/plugins-demo')
    // mock 的 t 原样返回 key：证明 label 确实走了 t(labelKey) 解析
    expect(wrapper.text()).toContain('plugins.demo.navLabel')
    wrapper.unmount()
  })

  it('启停切换驱动导航项响应式增删（无需重新挂载）', async () => {
    const store = useEnabledPluginsStore()
    const wrapper = mountSidebar()
    expect(navTargets(wrapper)).not.toContain('/admin/plugins-demo')

    store.ids = new Set(['demo'])
    store.loaded = true
    await wrapper.vm.$nextTick()
    expect(navTargets(wrapper)).toContain('/admin/plugins-demo')

    store.ids = new Set<string>()
    await wrapper.vm.$nextTick()
    expect(navTargets(wrapper)).not.toContain('/admin/plugins-demo')
    wrapper.unmount()
  })
})
