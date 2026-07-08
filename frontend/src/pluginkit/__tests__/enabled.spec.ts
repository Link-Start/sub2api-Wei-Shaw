/**
 * enabled store 守护测试（④ fail-closed）：
 * 未加载 / 拉取失败 / 非法响应，isEnabled 一律 false。
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
  it('初始态未加载：isEnabled 一律 false（fail-closed）', () => {
    const store = useEnabledPluginsStore()
    expect(store.loaded).toBe(false)
    expect(store.ids.size).toBe(0)
    expect(store.isEnabled('demo')).toBe(false)
  })

  it('fetchEnabled 成功：loaded=true，仅清单内 ID 返回 true', async () => {
    get.mockResolvedValue({ data: ['demo'] })
    const store = useEnabledPluginsStore()
    await store.fetchEnabled()

    expect(get).toHaveBeenCalledWith('/plugins')
    expect(store.loaded).toBe(true)
    expect(store.isEnabled('demo')).toBe(true)
    expect(store.isEnabled('other')).toBe(false)
  })

  it('fetchEnabled 失败：不抛错，清空集合且 loaded=false（fail-closed）', async () => {
    get.mockResolvedValueOnce({ data: ['demo'] })
    const store = useEnabledPluginsStore()
    await store.fetchEnabled()
    expect(store.isEnabled('demo')).toBe(true)

    get.mockRejectedValueOnce({ status: 0, message: 'Network error' })
    await expect(store.fetchEnabled()).resolves.toBeUndefined()
    expect(store.loaded).toBe(false)
    expect(store.ids.size).toBe(0)
    expect(store.isEnabled('demo')).toBe(false)
  })

  it('非数组响应按空清单处理', async () => {
    get.mockResolvedValue({ data: null })
    const store = useEnabledPluginsStore()
    await store.fetchEnabled()

    expect(store.loaded).toBe(true)
    expect(store.ids.size).toBe(0)
    expect(store.isEnabled('demo')).toBe(false)
  })

  it('refresh 重新拉取并覆盖旧清单', async () => {
    get.mockResolvedValueOnce({ data: ['demo'] })
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
    // 请求 A 先发出但后完成（携带过期的 ['demo']）；请求 B 后发出先完成（最新 []）
    let resolveA!: (value: { data: string[] }) => void
    const pendingA = new Promise<{ data: string[] }>((resolve) => {
      resolveA = resolve
    })
    get.mockReturnValueOnce(pendingA)
    get.mockResolvedValueOnce({ data: [] })

    const store = useEnabledPluginsStore()
    const fetchA = store.fetchEnabled()
    await store.refresh()
    expect(store.loaded).toBe(true)
    expect(store.isEnabled('demo')).toBe(false)

    resolveA({ data: ['demo'] })
    await fetchA
    expect(store.isEnabled('demo')).toBe(false)
    expect(store.loaded).toBe(true)
  })

  it('登出 reset 作废在途拉取：晚到的成功响应不再回填清单', async () => {
    let resolveFetch!: (value: { data: string[] }) => void
    get.mockReturnValueOnce(
      new Promise<{ data: string[] }>((resolve) => {
        resolveFetch = resolve
      })
    )

    const store = useEnabledPluginsStore()
    const inflight = store.fetchEnabled()
    store.reset()

    resolveFetch({ data: ['demo'] })
    await inflight
    expect(store.loaded).toBe(false)
    expect(store.ids.size).toBe(0)
    expect(store.isEnabled('demo')).toBe(false)
  })

  it('reset 回到 fail-closed 初始态', async () => {
    get.mockResolvedValue({ data: ['demo'] })
    const store = useEnabledPluginsStore()
    await store.fetchEnabled()
    expect(store.isEnabled('demo')).toBe(true)

    store.reset()
    expect(store.loaded).toBe(false)
    expect(store.ids.size).toBe(0)
    expect(store.isEnabled('demo')).toBe(false)
  })
})
