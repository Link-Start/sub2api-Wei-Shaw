import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'

import SettingsPanel from '../SettingsPanel.vue'

const { getSettings, updateSettings, showError, showSuccess } = vi.hoisted(() => ({
  getSettings: vi.fn(),
  updateSettings: vi.fn(),
  showError: vi.fn(),
  showSuccess: vi.fn()
}))

vi.mock('@/api/admin/settings', () => ({
  getSettings,
  updateSettings
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

function mountPanel() {
  return mount(SettingsPanel, {
    global: {
      stubs: {
        RouterLink: { template: '<a><slot /></a>' }
      }
    }
  })
}

beforeEach(() => {
  getSettings.mockReset()
  updateSettings.mockReset()
  showError.mockReset()
  showSuccess.mockReset()

  getSettings.mockResolvedValue({
    risk_control_enabled: true,
    cyber_session_block_enabled: true,
    cyber_session_block_ttl_seconds: 1200
  })
  updateSettings.mockResolvedValue({})
})

describe('content-moderation SettingsPanel', () => {
  it('挂载时加载设置并预填三个字段', async () => {
    const wrapper = mountPanel()
    await flushPromises()

    expect(getSettings).toHaveBeenCalledTimes(1)
    expect(
      wrapper.get('[data-test="moderation-settings-enabled"]').attributes('aria-checked')
    ).toBe('true')
    expect(
      wrapper.get('[data-test="moderation-settings-cyber-block"]').attributes('aria-checked')
    ).toBe('true')
    expect(
      (wrapper.get('[data-test="moderation-settings-cyber-ttl"]').element as HTMLInputElement)
        .value
    ).toBe('1200')
    wrapper.unmount()
  })

  it('cyber 屏蔽关闭时不展示 TTL 输入', async () => {
    getSettings.mockResolvedValue({
      risk_control_enabled: false,
      cyber_session_block_enabled: false,
      cyber_session_block_ttl_seconds: 3600
    })

    const wrapper = mountPanel()
    await flushPromises()

    expect(wrapper.find('[data-test="moderation-settings-cyber-ttl"]').exists()).toBe(false)
    wrapper.unmount()
  })

  it('保存 → 只提交本插件的三个键（部分更新），成功提示', async () => {
    const wrapper = mountPanel()
    await flushPromises()

    await wrapper.get('[data-test="moderation-settings-enabled"]').trigger('click')
    await wrapper.get('[data-test="moderation-settings-save"]').trigger('click')
    await flushPromises()

    expect(updateSettings).toHaveBeenCalledTimes(1)
    expect(updateSettings.mock.calls[0][0]).toEqual({
      risk_control_enabled: false,
      cyber_session_block_enabled: true,
      cyber_session_block_ttl_seconds: 1200
    })
    expect(showSuccess).toHaveBeenCalledWith('plugins.content-moderation.settings.saved')
    wrapper.unmount()
  })

  it('加载失败 → showError', async () => {
    getSettings.mockRejectedValue({ status: 500 })

    const wrapper = mountPanel()
    await flushPromises()

    expect(showError).toHaveBeenCalledWith('plugins.content-moderation.settings.loadFailed')
    wrapper.unmount()
  })

  it('保存失败 → showError', async () => {
    updateSettings.mockRejectedValue({ status: 500 })

    const wrapper = mountPanel()
    await flushPromises()

    await wrapper.get('[data-test="moderation-settings-save"]').trigger('click')
    await flushPromises()

    expect(showError).toHaveBeenCalledWith('plugins.content-moderation.settings.saveFailed')
    wrapper.unmount()
  })
})
