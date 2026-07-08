import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'

import PluginsView from '../PluginsView.vue'
import type { PluginStatus } from '@/api/admin/plugins'

const { listPlugins, enablePlugin, disablePlugin, refreshEnabled, showError, showSuccess } =
  vi.hoisted(() => ({
    listPlugins: vi.fn(),
    enablePlugin: vi.fn(),
    disablePlugin: vi.fn(),
    refreshEnabled: vi.fn(),
    showError: vi.fn(),
    showSuccess: vi.fn()
  }))

vi.mock('@/api/admin/plugins', () => ({
  adminPluginsAPI: { listPlugins, enablePlugin, disablePlugin },
  default: { listPlugins, enablePlugin, disablePlugin }
}))

vi.mock('@/pluginkit/enabled', () => ({
  useEnabledPluginsStore: () => ({ refresh: refreshEnabled })
}))

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({ showError, showSuccess })
}))

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({ t: (key: string) => key })
  }
})

const AppLayoutStub = { template: '<div><slot /></div>' }
const TablePageLayoutStub = {
  template: '<div><slot name="filters" /><slot name="table" /><slot name="pagination" /></div>'
}
// DataTable 桌面路径依赖虚拟滚动（jsdom 无高度），跟随相邻 spec 惯例以 stub 渲染 cell slots
const DataTableStub = {
  props: ['columns', 'data', 'loading', 'rowKey'],
  template: `
    <div>
      <div data-test="columns">{{ columns.map(col => col.key).join(',') }}</div>
      <div v-for="row in data" :key="row.id">
        <template v-for="col in columns" :key="col.key">
          <slot :name="'cell-' + col.key" :value="row[col.key]" :row="row" />
        </template>
      </div>
      <slot v-if="!loading && data.length === 0" name="empty" />
    </div>
  `
}

const demoStatus = (overrides: Partial<PluginStatus> = {}): PluginStatus => ({
  id: 'demo',
  enabled: false,
  state: 'disabled',
  ...overrides
})

function mountView() {
  return mount(PluginsView, {
    global: {
      stubs: {
        AppLayout: AppLayoutStub,
        TablePageLayout: TablePageLayoutStub,
        DataTable: DataTableStub,
        EmptyState: true,
        Icon: true
      }
    }
  })
}

beforeEach(() => {
  listPlugins.mockReset()
  enablePlugin.mockReset()
  disablePlugin.mockReset()
  refreshEnabled.mockReset()
  showError.mockReset()
  showSuccess.mockReset()

  listPlugins.mockResolvedValue([demoStatus()])
  enablePlugin.mockResolvedValue(demoStatus({ enabled: true, state: 'running' }))
  disablePlugin.mockResolvedValue(demoStatus())
  refreshEnabled.mockResolvedValue(undefined)
})

describe('admin PluginsView', () => {
  it('挂载时加载插件快照并展示 id/状态', async () => {
    listPlugins.mockResolvedValue([
      demoStatus({
        enabled: true,
        state: 'running',
        started_at: '2026-07-08T10:00:00Z'
      })
    ])

    const wrapper = mountView()
    await flushPromises()

    expect(listPlugins).toHaveBeenCalledTimes(1)
    expect(wrapper.text()).toContain('demo')
    expect(wrapper.text()).toContain('admin.plugins.stateLabels.running')
    wrapper.unmount()
  })

  it('failed 状态展示错误信息', async () => {
    listPlugins.mockResolvedValue([
      demoStatus({ enabled: true, state: 'failed', error: 'demo: greeting must not be blank' })
    ])

    const wrapper = mountView()
    await flushPromises()

    expect(wrapper.text()).toContain('admin.plugins.stateLabels.failed')
    expect(wrapper.text()).toContain('demo: greeting must not be blank')
    wrapper.unmount()
  })

  it('开关开启 → enablePlugin + 刷新列表与 enabled store', async () => {
    const wrapper = mountView()
    await flushPromises()

    await wrapper.get('[data-test="plugin-toggle-demo"]').trigger('click')
    await flushPromises()

    expect(enablePlugin).toHaveBeenCalledWith('demo')
    expect(disablePlugin).not.toHaveBeenCalled()
    expect(showSuccess).toHaveBeenCalledWith('admin.plugins.enableSuccess')
    // 启停后回读快照 + 同步 enabled 门控清单
    expect(listPlugins).toHaveBeenCalledTimes(2)
    expect(refreshEnabled).toHaveBeenCalledTimes(1)
    wrapper.unmount()
  })

  it('开关关闭 → disablePlugin + 刷新列表与 enabled store', async () => {
    listPlugins.mockResolvedValue([demoStatus({ enabled: true, state: 'running' })])

    const wrapper = mountView()
    await flushPromises()

    await wrapper.get('[data-test="plugin-toggle-demo"]').trigger('click')
    await flushPromises()

    expect(disablePlugin).toHaveBeenCalledWith('demo')
    expect(enablePlugin).not.toHaveBeenCalled()
    expect(showSuccess).toHaveBeenCalledWith('admin.plugins.disableSuccess')
    expect(refreshEnabled).toHaveBeenCalledTimes(1)
    wrapper.unmount()
  })

  it('启停失败 → showError，且仍回读快照与门控清单（保持前后端状态一致）', async () => {
    enablePlugin.mockRejectedValue({ status: 500, message: 'boom' })

    const wrapper = mountView()
    await flushPromises()

    await wrapper.get('[data-test="plugin-toggle-demo"]').trigger('click')
    await flushPromises()

    expect(showError).toHaveBeenCalledWith('admin.plugins.toggleFailed')
    expect(listPlugins).toHaveBeenCalledTimes(2)
    expect(refreshEnabled).toHaveBeenCalledTimes(1)
    wrapper.unmount()
  })

  it('列表加载失败 → showError', async () => {
    listPlugins.mockRejectedValue({ status: 500, message: 'boom' })

    const wrapper = mountView()
    await flushPromises()

    expect(showError).toHaveBeenCalledWith('admin.plugins.loadFailed')
    wrapper.unmount()
  })
})
