/**
 * enabled store 守护测试（④ fail-closed）：
 * 未加载 / 拉取失败 / 非法响应，isEnabled 一律 false。
 *
 * phase-4 契约演进：GET /api/v1/plugins 的 data 由 string[] 改为
 * [{id, assets?}]，ids 集合语义照旧，外部插件的 assets 收敛进
 * externalAssets 映射（驱动前端运行时加载器）。
 */
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'

import { useEnabledPluginsStore } from '../enabled'

const { get } = vi.hoisted(() => ({
  get: vi.fn()
}))

vi.mock('@/api/client', () => ({
  apiClient: { get },
  default: { get }
}))

beforeEach(() => {
  setActivePinia(createPinia())
  get.mockReset()
})

describe('useEnabledPluginsStore', () => {
  it('初始态未加载：isEnabled 一律 false（fail-closed），externalAssets 为空', () => {
    const store = useEnabledPluginsStore()
    expect(store.loaded).toBe(false)
    expect(store.ids.size).toBe(0)
    expect(store.externalAssets.size).toBe(0)
    expect(store.isEnabled('demo')).toBe(false)
  })

  it('fetchEnabled 成功：loaded=true，仅清单内 ID 返回 true', async () => {
    get.mockResolvedValue({ data: [{ id: 'demo' }] })
    const store = useEnabledPluginsStore()
    await store.fetchEnabled()

    expect(get).toHaveBeenCalledWith('/plugins')
    expect(store.loaded).toBe(true)
    expect(store.isEnabled('demo')).toBe(true)
    expect(store.isEnabled('other')).toBe(false)
  })

  it('带 assets 的外部插件进入 externalAssets 映射，不带的不进入', async () => {
    get.mockResolvedValue({
      data: [
        { id: 'demo' },
        { id: 'ext-hello', assets: '/api/v1/plugins/ext-hello/assets/plugin.js' }
      ]
    })
    const store = useEnabledPluginsStore()
    await store.fetchEnabled()

    expect(store.isEnabled('demo')).toBe(true)
    expect(store.isEnabled('ext-hello')).toBe(true)
    expect(store.externalAssets.size).toBe(1)
    expect(store.externalAssets.get('ext-hello')).toBe(
      '/api/v1/plugins/ext-hello/assets/plugin.js'
    )
    expect(store.externalAssets.has('demo')).toBe(false)
  })

  it('形态非法的清单项被丢弃（fail-closed）：字符串项/缺 id/空 id/null', async () => {
    get.mockResolvedValue({
      data: ['legacy-string', { id: '' }, { assets: '/x.js' }, null, { id: 'valid' }]
    })
    const store = useEnabledPluginsStore()
    await store.fetchEnabled()

    expect(store.loaded).toBe(true)
    expect(store.ids.size).toBe(1)
    expect(store.isEnabled('valid')).toBe(true)
    expect(store.isEnabled('legacy-string')).toBe(false)
    expect(store.externalAssets.size).toBe(0)
  })

  it('fetchEnabled 失败：不抛错，清空集合与资产映射且 loaded=false（fail-closed）', async () => {
    get.mockResolvedValueOnce({
      data: [{ id: 'demo' }, { id: 'ext', assets: '/api/v1/plugins/ext/assets/plugin.js' }]
    })
    const store = useEnabledPluginsStore()
    await store.fetchEnabled()
    expect(store.isEnabled('demo')).toBe(true)
    expect(store.externalAssets.size).toBe(1)

    get.mockRejectedValueOnce({ status: 0, message: 'Network error' })
    await expect(store.fetchEnabled()).resolves.toBeUndefined()
    expect(store.loaded).toBe(false)
    expect(store.ids.size).toBe(0)
    expect(store.externalAssets.size).toBe(0)
    expect(store.isEnabled('demo')).toBe(false)
  })

  it('非数组响应按空清单处理', async () => {
    get.mockResolvedValue({ data: null })
    const store = useEnabledPluginsStore()
    await store.fetchEnabled()

    expect(store.loaded).toBe(true)
    expect(store.ids.size).toBe(0)
    expect(store.externalAssets.size).toBe(0)
    expect(store.isEnabled('demo')).toBe(false)
  })

  it('refresh 重新拉取并覆盖旧清单', async () => {
    get.mockResolvedValueOnce({ data: [{ id: 'demo' }] })
    const store = useEnabledPluginsStore()
    await store.fetchEnabled()
    expect(store.isEnabled('demo')).toBe(true)

    get.mockResolvedValueOnce({ data: [] })
    await store.refresh()
    expect(get).toHaveBeenCalledTimes(2)
    expect(store.loaded).toBe(true)
    expect(store.isEnabled('demo')).toBe(false)
  })

  it('并发拉取乱序完成：过期响应晚到不覆盖最新结果（代际守卫）', async () => {
    // 请求 A 先发出但后完成（携带过期的 demo 清单）；请求 B 后发出先完成（最新 []）
    let resolveA!: (value: { data: Array<{ id: string }> }) => void
    const pendingA = new Promise<{ data: Array<{ id: string }> }>((resolve) => {
      resolveA = resolve
    })
    get.mockReturnValueOnce(pendingA)
    get.mockResolvedValueOnce({ data: [] })

    const store = useEnabledPluginsStore()
    const fetchA = store.fetchEnabled()
    await store.refresh()
    expect(store.loaded).toBe(true)
    expect(store.isEnabled('demo')).toBe(false)

    resolveA({ data: [{ id: 'demo' }] })
    await fetchA
    expect(store.isEnabled('demo')).toBe(false)
    expect(store.loaded).toBe(true)
  })

  it('登出 reset 作废在途拉取：晚到的成功响应不再回填清单', async () => {
    let resolveFetch!: (value: { data: Array<{ id: string }> }) => void
    get.mockReturnValueOnce(
      new Promise<{ data: Array<{ id: string }> }>((resolve) => {
        resolveFetch = resolve
      })
    )

    const store = useEnabledPluginsStore()
    const inflight = store.fetchEnabled()
    store.reset()

    resolveFetch({ data: [{ id: 'demo' }] })
    await inflight
    expect(store.loaded).toBe(false)
    expect(store.ids.size).toBe(0)
    expect(store.isEnabled('demo')).toBe(false)
  })

  it('reset 回到 fail-closed 初始态（含清空 externalAssets）', async () => {
    get.mockResolvedValue({
      data: [{ id: 'demo' }, { id: 'ext', assets: '/api/v1/plugins/ext/assets/plugin.js' }]
    })
    const store = useEnabledPluginsStore()
    await store.fetchEnabled()
    expect(store.isEnabled('demo')).toBe(true)
    expect(store.externalAssets.size).toBe(1)

    store.reset()
    expect(store.loaded).toBe(false)
    expect(store.ids.size).toBe(0)
    expect(store.externalAssets.size).toBe(0)
    expect(store.isEnabled('demo')).toBe(false)
  })
})
