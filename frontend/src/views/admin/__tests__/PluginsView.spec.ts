import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'

import PluginsView from '../PluginsView.vue'
import type { PluginStatus } from '@/api/admin/plugins'

const {
  listPlugins,
  enablePlugin,
  disablePlugin,
  uploadPlugin,
  uninstallPlugin,
  getPluginConfig,
  putPluginConfig,
  refreshEnabled,
  showError,
  showSuccess,
  settingsLoader
} = vi.hoisted(() => ({
  listPlugins: vi.fn(),
  enablePlugin: vi.fn(),
  disablePlugin: vi.fn(),
  uploadPlugin: vi.fn(),
  uninstallPlugin: vi.fn(),
  getPluginConfig: vi.fn(),
  putPluginConfig: vi.fn(),
  refreshEnabled: vi.fn(),
  showError: vi.fn(),
  showSuccess: vi.fn(),
  settingsLoader: vi.fn()
}))

vi.mock('@/api/admin/plugins', () => {
  const api = {
    listPlugins,
    enablePlugin,
    disablePlugin,
    uploadPlugin,
    uninstallPlugin,
    getPluginConfig,
    putPluginConfig
  }
  return { adminPluginsAPI: api, default: api }
})

vi.mock('@/pluginkit/enabled', () => ({
  useEnabledPluginsStore: () => ({ refresh: refreshEnabled })
}))

vi.mock('@/pluginkit/registry', () => ({
  pluginSettingsLoader: settingsLoader
}))

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({ showError, showSuccess })
}))

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    // te 恒 false：卡片名称/描述走 manifest/ID 回退路径，断言不依赖语言包内容
    useI18n: () => ({ t: (key: string) => key, te: () => false })
  }
})

const AppLayoutStub = { template: '<div><slot /></div>' }
const ConfirmDialogStub = {
  props: ['show', 'title', 'message', 'confirmText', 'cancelText', 'danger'],
  emits: ['confirm', 'cancel'],
  template: `
    <div v-if="show" data-test="confirm-dialog">
      <span data-test="confirm-message">{{ message }}</span>
      <button data-test="confirm-ok" @click="$emit('confirm')" />
      <button data-test="confirm-cancel" @click="$emit('cancel')" />
    </div>
  `
}
// 两个 BaseDialog（上传/配置）共用 stub：断言依赖各自内部唯一的 data-test 元素
const BaseDialogStub = {
  props: ['show', 'title'],
  emits: ['close'],
  template: '<div v-if="show"><slot /><slot name="footer" /></div>'
}

const demoStatus = (overrides: Partial<PluginStatus> = {}): PluginStatus => ({
  id: 'demo',
  enabled: false,
  state: 'disabled',
  ...overrides
})

const externalStatus = (overrides: Partial<PluginStatus> = {}): PluginStatus => ({
  id: 'ext-hello',
  enabled: false,
  state: 'disabled',
  tier: 'external',
  ...overrides
})

function mountView() {
  return mount(PluginsView, {
    global: {
      stubs: {
        AppLayout: AppLayoutStub,
        ConfirmDialog: ConfirmDialogStub,
        BaseDialog: BaseDialogStub,
        EmptyState: true,
        Icon: true
      }
    }
  })
}

async function openUploadDialog(wrapper: ReturnType<typeof mountView>) {
  await wrapper.get('[data-test="plugin-upload-btn"]').trigger('click')
}

function pickFile(wrapper: ReturnType<typeof mountView>, file: File) {
  const input = wrapper.get('[data-test="plugin-upload-input"]')
  Object.defineProperty(input.element, 'files', { value: [file], configurable: true })
  return input.trigger('change')
}

const zipFile = () => new File(['zip-bytes'], 'plugin.zip', { type: 'application/zip' })

beforeEach(() => {
  listPlugins.mockReset()
  enablePlugin.mockReset()
  disablePlugin.mockReset()
  uploadPlugin.mockReset()
  uninstallPlugin.mockReset()
  getPluginConfig.mockReset()
  putPluginConfig.mockReset()
  refreshEnabled.mockReset()
  showError.mockReset()
  showSuccess.mockReset()
  settingsLoader.mockReset()
  settingsLoader.mockReturnValue(null)

  listPlugins.mockResolvedValue([demoStatus()])
  enablePlugin.mockResolvedValue(demoStatus({ enabled: true, state: 'running' }))
  disablePlugin.mockResolvedValue(demoStatus())
  uploadPlugin.mockResolvedValue({
    id: 'ext-hello',
    version: '1.0.0',
    checksum: 'abc',
    installed_by: 'admin:1',
    installed_at: '2026-07-08T10:00:00Z'
  })
  uninstallPlugin.mockResolvedValue(undefined)
  getPluginConfig.mockResolvedValue({ config: { greeting: 'hi' }, config_schema: { type: 'object' } })
  putPluginConfig.mockResolvedValue({ greeting: 'yo' })
  refreshEnabled.mockResolvedValue(undefined)
})

describe('admin PluginsView', () => {
  it('挂载时加载插件快照并以卡片展示 id/状态', async () => {
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
    expect(wrapper.find('[data-test="plugin-card-demo"]').exists()).toBe(true)
    expect(wrapper.text()).toContain('demo')
    expect(wrapper.text()).toContain('admin.plugins.stateLabels.running')
    wrapper.unmount()
  })

  it('外部插件卡片展示 manifest 名称/描述与版本', async () => {
    listPlugins.mockResolvedValue([
      externalStatus({
        name: 'Hello Plugin',
        description: 'Say hello from an external process',
        version: '1.2.3'
      })
    ])

    const wrapper = mountView()
    await flushPromises()

    const card = wrapper.get('[data-test="plugin-card-ext-hello"]')
    expect(card.text()).toContain('Hello Plugin')
    expect(card.text()).toContain('Say hello from an external process')
    expect(card.text()).toContain('v1.2.3')
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

  describe('tier 徽标与操作可见性', () => {
    it('外部插件展示 external 徽标与卸载/配置按钮；内建（含缺省 tier）展示 builtin 且无操作', async () => {
      listPlugins.mockResolvedValue([demoStatus(), externalStatus()])

      const wrapper = mountView()
      await flushPromises()

      expect(wrapper.get('[data-test="plugin-tier-demo"]').text()).toBe(
        'admin.plugins.tierLabels.builtin'
      )
      expect(wrapper.get('[data-test="plugin-tier-ext-hello"]').text()).toBe(
        'admin.plugins.tierLabels.external'
      )
      expect(wrapper.find('[data-test="plugin-uninstall-ext-hello"]').exists()).toBe(true)
      expect(wrapper.find('[data-test="plugin-config-ext-hello"]').exists()).toBe(true)
      expect(wrapper.find('[data-test="plugin-uninstall-demo"]').exists()).toBe(false)
      expect(wrapper.find('[data-test="plugin-config-demo"]').exists()).toBe(false)
      wrapper.unmount()
    })
  })

  describe('插件设置面板（描述符声明驱动）', () => {
    it('声明了设置面板的插件卡片显示设置按钮，点击后弹窗渲染懒加载面板', async () => {
      const PanelStub = { template: '<div data-test="settings-panel-stub">panel</div>' }
      // 直接 resolve 组件（defineAsyncComponent 仅对真实 ES module 解包 default）
      settingsLoader.mockImplementation((id: string) =>
        id === 'demo' ? () => Promise.resolve(PanelStub) : null
      )

      const wrapper = mountView()
      await flushPromises()

      await wrapper.get('[data-test="plugin-settings-demo"]').trigger('click')
      // 异步组件：loader 的 promise 解析 + Vue 重渲染各占一轮微任务
      await flushPromises()
      await flushPromises()

      expect(wrapper.find('[data-test="settings-panel-stub"]').exists()).toBe(true)
      wrapper.unmount()
    })

    it('未声明设置面板的插件卡片不显示设置按钮', async () => {
      const wrapper = mountView()
      await flushPromises()

      expect(wrapper.find('[data-test="plugin-settings-demo"]').exists()).toBe(false)
      wrapper.unmount()
    })
  })

  describe('上传安装（弹窗：拖拽/点选 → 确认）', () => {
    it('点击安装 → 弹窗展示拖拽区与提示，未选文件时确认按钮禁用', async () => {
      const wrapper = mountView()
      await flushPromises()

      expect(wrapper.find('[data-test="plugin-upload-dropzone"]').exists()).toBe(false)
      await openUploadDialog(wrapper)

      expect(wrapper.find('[data-test="plugin-upload-dropzone"]').exists()).toBe(true)
      expect(wrapper.text()).toContain('admin.plugins.uploadSecurityNote')
      const confirm = wrapper.get('[data-test="plugin-upload-confirm"]')
      expect((confirm.element as HTMLButtonElement).disabled).toBe(true)
      expect(uploadPlugin).not.toHaveBeenCalled()
      wrapper.unmount()
    })

    it('选择 zip → 确认 → uploadPlugin(file) → 成功提示、关闭弹窗并刷新列表', async () => {
      const wrapper = mountView()
      await flushPromises()

      await openUploadDialog(wrapper)
      const file = zipFile()
      await pickFile(wrapper, file)

      expect(wrapper.get('[data-test="plugin-upload-file"]').text()).toContain('plugin.zip')
      const confirm = wrapper.get('[data-test="plugin-upload-confirm"]')
      expect((confirm.element as HTMLButtonElement).disabled).toBe(false)

      await confirm.trigger('click')
      await flushPromises()

      expect(uploadPlugin).toHaveBeenCalledTimes(1)
      expect(uploadPlugin.mock.calls[0][0]).toBe(file)
      expect(showSuccess).toHaveBeenCalledWith('admin.plugins.uploadSuccess')
      expect(listPlugins).toHaveBeenCalledTimes(2)
      // 成功后弹窗关闭
      expect(wrapper.find('[data-test="plugin-upload-dropzone"]').exists()).toBe(false)
      wrapper.unmount()
    })

    it('拖拽 zip 到拖拽区 → 文件被选中', async () => {
      const wrapper = mountView()
      await flushPromises()

      await openUploadDialog(wrapper)
      const file = zipFile()
      await wrapper
        .get('[data-test="plugin-upload-dropzone"]')
        .trigger('drop', { dataTransfer: { files: [file] } })

      expect(wrapper.get('[data-test="plugin-upload-file"]').text()).toContain('plugin.zip')
      expect(
        (wrapper.get('[data-test="plugin-upload-confirm"]').element as HTMLButtonElement).disabled
      ).toBe(false)
      expect(uploadPlugin).not.toHaveBeenCalled()
      wrapper.unmount()
    })

    it('非 .zip 文件 → 就地报错且不收下文件', async () => {
      const wrapper = mountView()
      await flushPromises()

      await openUploadDialog(wrapper)
      await pickFile(wrapper, new File(['x'], 'evil.exe', { type: 'application/octet-stream' }))

      expect(showError).toHaveBeenCalledWith('admin.plugins.uploadInvalidType')
      expect(wrapper.find('[data-test="plugin-upload-file"]').exists()).toBe(false)
      expect(
        (wrapper.get('[data-test="plugin-upload-confirm"]').element as HTMLButtonElement).disabled
      ).toBe(true)
      wrapper.unmount()
    })

    it('移除已选文件 → 确认按钮回到禁用', async () => {
      const wrapper = mountView()
      await flushPromises()

      await openUploadDialog(wrapper)
      await pickFile(wrapper, zipFile())
      await wrapper.get('[data-test="plugin-upload-remove"]').trigger('click')

      expect(wrapper.find('[data-test="plugin-upload-file"]').exists()).toBe(false)
      expect(
        (wrapper.get('[data-test="plugin-upload-confirm"]').element as HTMLButtonElement).disabled
      ).toBe(true)
      wrapper.unmount()
    })

    it('上传失败 → 展示后端 message，弹窗保持打开可重试', async () => {
      uploadPlugin.mockRejectedValue({ status: 400, message: 'invalid plugin package: bad manifest' })

      const wrapper = mountView()
      await flushPromises()

      await openUploadDialog(wrapper)
      await pickFile(wrapper, zipFile())
      await wrapper.get('[data-test="plugin-upload-confirm"]').trigger('click')
      await flushPromises()

      expect(showError).toHaveBeenCalledWith('invalid plugin package: bad manifest')
      expect(listPlugins).toHaveBeenCalledTimes(1)
      // 弹窗与已选文件保留，可修正后直接重试
      expect(wrapper.find('[data-test="plugin-upload-dropzone"]').exists()).toBe(true)
      expect(wrapper.find('[data-test="plugin-upload-file"]').exists()).toBe(true)
      wrapper.unmount()
    })

    it('后端错误缺 message → 回退兜底文案', async () => {
      uploadPlugin.mockRejectedValue({ status: 0 })

      const wrapper = mountView()
      await flushPromises()

      await openUploadDialog(wrapper)
      await pickFile(wrapper, zipFile())
      await wrapper.get('[data-test="plugin-upload-confirm"]').trigger('click')
      await flushPromises()

      expect(showError).toHaveBeenCalledWith('admin.plugins.uploadFailed')
      wrapper.unmount()
    })
  })

  describe('卸载（二次确认）', () => {
    beforeEach(() => {
      listPlugins.mockResolvedValue([demoStatus(), externalStatus()])
    })

    it('点击卸载 → 先弹确认对话，未确认前不调 API', async () => {
      const wrapper = mountView()
      await flushPromises()

      expect(wrapper.find('[data-test="confirm-dialog"]').exists()).toBe(false)
      await wrapper.get('[data-test="plugin-uninstall-ext-hello"]').trigger('click')

      expect(wrapper.find('[data-test="confirm-dialog"]').exists()).toBe(true)
      expect(wrapper.get('[data-test="confirm-message"]').text()).toBe(
        'admin.plugins.uninstallConfirmMessage'
      )
      expect(uninstallPlugin).not.toHaveBeenCalled()
      wrapper.unmount()
    })

    it('确认 → uninstallPlugin(id) + 刷新列表与 enabled store', async () => {
      const wrapper = mountView()
      await flushPromises()

      await wrapper.get('[data-test="plugin-uninstall-ext-hello"]').trigger('click')
      await wrapper.get('[data-test="confirm-ok"]').trigger('click')
      await flushPromises()

      expect(uninstallPlugin).toHaveBeenCalledWith('ext-hello')
      expect(showSuccess).toHaveBeenCalledWith('admin.plugins.uninstallSuccess')
      expect(listPlugins).toHaveBeenCalledTimes(2)
      expect(refreshEnabled).toHaveBeenCalledTimes(1)
      wrapper.unmount()
    })

    it('取消 → 不调 API', async () => {
      const wrapper = mountView()
      await flushPromises()

      await wrapper.get('[data-test="plugin-uninstall-ext-hello"]').trigger('click')
      await wrapper.get('[data-test="confirm-cancel"]').trigger('click')
      await flushPromises()

      expect(uninstallPlugin).not.toHaveBeenCalled()
      wrapper.unmount()
    })

    it('卸载失败 → 展示后端 message，仍刷新状态', async () => {
      uninstallPlugin.mockRejectedValue({ status: 404, message: 'plugin not installed' })

      const wrapper = mountView()
      await flushPromises()

      await wrapper.get('[data-test="plugin-uninstall-ext-hello"]').trigger('click')
      await wrapper.get('[data-test="confirm-ok"]').trigger('click')
      await flushPromises()

      expect(showError).toHaveBeenCalledWith('plugin not installed')
      expect(listPlugins).toHaveBeenCalledTimes(2)
      expect(refreshEnabled).toHaveBeenCalledTimes(1)
      wrapper.unmount()
    })
  })

  describe('配置 JSON 编辑器', () => {
    beforeEach(() => {
      listPlugins.mockResolvedValue([externalStatus()])
    })

    it('打开 → 拉取配置并以格式化 JSON 预填 textarea', async () => {
      const wrapper = mountView()
      await flushPromises()

      await wrapper.get('[data-test="plugin-config-ext-hello"]').trigger('click')
      await flushPromises()

      expect(getPluginConfig).toHaveBeenCalledWith('ext-hello')
      const textarea = wrapper.get('[data-test="plugin-config-textarea"]')
      expect((textarea.element as HTMLTextAreaElement).value).toContain('"greeting": "hi"')
      wrapper.unmount()
    })

    it('config 为空（null）时预填空对象', async () => {
      getPluginConfig.mockResolvedValue({ config: null })

      const wrapper = mountView()
      await flushPromises()

      await wrapper.get('[data-test="plugin-config-ext-hello"]').trigger('click')
      await flushPromises()

      expect((wrapper.get('[data-test="plugin-config-textarea"]').element as HTMLTextAreaElement).value).toBe('{}')
      wrapper.unmount()
    })

    it('非法 JSON → 就地校验报错，不调 PUT', async () => {
      const wrapper = mountView()
      await flushPromises()

      await wrapper.get('[data-test="plugin-config-ext-hello"]').trigger('click')
      await flushPromises()
      await wrapper.get('[data-test="plugin-config-textarea"]').setValue('{ not json')
      await wrapper.get('[data-test="plugin-config-save"]').trigger('click')
      await flushPromises()

      expect(putPluginConfig).not.toHaveBeenCalled()
      expect(wrapper.get('[data-test="plugin-config-error"]').text()).toBe(
        'admin.plugins.configInvalidJson'
      )
      wrapper.unmount()
    })

    it('合法 JSON → putPluginConfig(id, 原始文本) → 成功提示并关闭', async () => {
      const wrapper = mountView()
      await flushPromises()

      await wrapper.get('[data-test="plugin-config-ext-hello"]').trigger('click')
      await flushPromises()
      await wrapper.get('[data-test="plugin-config-textarea"]').setValue('{"greeting":"yo"}')
      await wrapper.get('[data-test="plugin-config-save"]').trigger('click')
      await flushPromises()

      expect(putPluginConfig).toHaveBeenCalledWith('ext-hello', '{"greeting":"yo"}')
      expect(showSuccess).toHaveBeenCalledWith('admin.plugins.configSaveSuccess')
      expect(wrapper.find('[data-test="plugin-config-textarea"]').exists()).toBe(false)
      wrapper.unmount()
    })

    it('保存被后端拒绝（schema 校验失败）→ 错误就地展示，编辑器保持打开', async () => {
      putPluginConfig.mockRejectedValue({ status: 400, message: 'config: greeting is required' })

      const wrapper = mountView()
      await flushPromises()

      await wrapper.get('[data-test="plugin-config-ext-hello"]').trigger('click')
      await flushPromises()
      await wrapper.get('[data-test="plugin-config-textarea"]').setValue('{}')
      await wrapper.get('[data-test="plugin-config-save"]').trigger('click')
      await flushPromises()

      expect(wrapper.get('[data-test="plugin-config-error"]').text()).toBe(
        'config: greeting is required'
      )
      expect(wrapper.find('[data-test="plugin-config-textarea"]').exists()).toBe(true)
      wrapper.unmount()
    })
  })
})
