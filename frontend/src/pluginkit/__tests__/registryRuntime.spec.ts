/**
 * 外部插件运行时注册守护测试：
 *   - 安全闸：未登记期望 ID / 宿主未绑定 / 描述符非法 / 归属不一致
 *     （非 loader 绑定的脚本元素、异步注册、冒用他插件 ID）→ 注册被拒（warn+跳过）
 *   - 注册生效：路由经宿主 addRoute 注入且 meta.pluginId 强制覆写；
 *     导航参与 pluginNav 聚合（仍受 enabled 门控）；文案两语言 merge
 *   - 幂等：同 ID 重复注册不重复施加贡献
 *   - 对账：syncRuntimeActivation 停用（removeRoute+导航消失+期望撤销）
 *     与复活（无需重新注册）
 */
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { RouteRecordRaw } from 'vue-router'

import {
  bindRuntimeScript,
  expectRuntimePlugin,
  pluginMessages,
  pluginNav,
  registerRuntime,
  setRuntimeHost,
  syncRuntimeActivation,
  _resetPluginsForTest,
  _resetRuntimeForTest,
  _setPluginsForTest
} from '../registry'
import type { PluginDescriptor } from '../types'

// 宿主假件：addRoute 记录注入的路由并返回可断言的移除函数
let addedRoutes: RouteRecordRaw[]
let removers: Array<ReturnType<typeof vi.fn>>
const addRoute = vi.fn((route: RouteRecordRaw) => {
  addedRoutes.push(route)
  const remover = vi.fn()
  removers.push(remover)
  return remover
})
const mergeLocaleMessage = vi.fn()

function bindHost(): void {
  setRuntimeHost({ addRoute, mergeLocaleMessage })
}

function extDescriptor(overrides: Partial<PluginDescriptor> = {}): PluginDescriptor {
  return {
    id: 'ext-hello',
    routes: [
      {
        path: '/admin/ext-hello',
        name: 'ExtHello',
        component: () => Promise.resolve({ default: { template: '<div />' } }),
        meta: { pluginId: 'forged-id', requiresAuth: true }
      } as RouteRecordRaw
    ],
    adminNav: [{ path: '/admin/ext-hello', labelKey: 'plugins.ext-hello.navLabel' }],
    i18n: {
      zh: { navLabel: '外部演示' },
      en: { navLabel: 'External Demo' }
    },
    ...overrides
  }
}

// registerAs 模拟"loader 注入的脚本在同步求值期间注册"：绑定元素身份并把
// document.currentScript 指向该元素（归属契约的合法路径）。
function registerAs(scriptOwnerId: string, descriptor: PluginDescriptor): void {
  const scriptEl = document.createElement('script')
  scriptEl.dataset.pluginId = scriptOwnerId
  bindRuntimeScript(scriptEl, scriptOwnerId)
  const currentScriptSpy = vi.spyOn(document, 'currentScript', 'get').mockReturnValue(scriptEl)
  try {
    registerRuntime(descriptor)
  } finally {
    currentScriptSpy.mockRestore()
  }
}

let warnSpy: ReturnType<typeof vi.spyOn>

beforeEach(() => {
  addedRoutes = []
  removers = []
  addRoute.mockClear()
  mergeLocaleMessage.mockClear()
  // 隔离内建清单：仅观察运行时注册的贡献
  _setPluginsForTest([])
  warnSpy = vi.spyOn(console, 'warn').mockImplementation(() => {})
})

afterEach(() => {
  _resetRuntimeForTest()
  _resetPluginsForTest()
  warnSpy.mockRestore()
})

describe('registerRuntime 安全闸（fail-closed）', () => {
  it('window.__SUB2API_PLUGIN_REGISTER__ 即 registerRuntime', () => {
    expect(window.__SUB2API_PLUGIN_REGISTER__).toBe(registerRuntime)
  })

  it('未登记期望 ID 的注册被拒：无路由/导航/文案贡献', () => {
    bindHost()
    registerRuntime(extDescriptor())

    expect(addRoute).not.toHaveBeenCalled()
    expect(mergeLocaleMessage).not.toHaveBeenCalled()
    expect(pluginNav('admin', new Set(['ext-hello']))).toEqual([])
    expect(warnSpy).toHaveBeenCalledTimes(1)
  })

  it('宿主未绑定时注册被拒', () => {
    expectRuntimePlugin('ext-hello')
    registerAs('ext-hello', extDescriptor())

    expect(pluginNav('admin', new Set(['ext-hello']))).toEqual([])
    expect(warnSpy).toHaveBeenCalledTimes(1)
  })

  it('描述符非法（缺 id / 空 id / null）一律被拒且不抛错', () => {
    bindHost()
    expect(() => registerRuntime(null as unknown as PluginDescriptor)).not.toThrow()
    expect(() => registerRuntime({ id: '' })).not.toThrow()
    expect(() => registerRuntime({} as PluginDescriptor)).not.toThrow()
    expect(addRoute).not.toHaveBeenCalled()
    expect(warnSpy).toHaveBeenCalledTimes(3)
  })

  it('注入脚本冒用他插件 ID 抢注被拒（元素身份归属绑定）', () => {
    bindHost()
    expectRuntimePlugin('ext-hello')
    expectRuntimePlugin('ext-evil')

    // 模拟由 ext-evil 的注入脚本执行时冒用 ext-hello 的 ID 注册。
    registerAs('ext-evil', extDescriptor()) // extDescriptor().id === 'ext-hello'

    expect(addRoute).not.toHaveBeenCalled()
    expect(pluginNav('admin', new Set(['ext-hello']))).toEqual([])
    expect(warnSpy).toHaveBeenCalledTimes(1)
  })

  it('异步注册（currentScript 为 null）被拒：期望集合命中也不放行', () => {
    bindHost()
    expectRuntimePlugin('ext-hello')

    // 直接调用即模拟 .then/setTimeout 里的注册：currentScript 已不指向任何脚本。
    registerRuntime(extDescriptor())

    expect(addRoute).not.toHaveBeenCalled()
    expect(pluginNav('admin', new Set(['ext-hello']))).toEqual([])
    expect(warnSpy).toHaveBeenCalledTimes(1)
  })

  it('自造 script 元素伪造 data-plugin-id 不放行（归属只认 loader 绑定的元素身份）', () => {
    bindHost()
    expectRuntimePlugin('ext-hello')
    // 攻击者脚本可自造带任意 data-* 的元素并诱导执行，但该元素未经
    // bindRuntimeScript 绑定，注册必须被拒。
    const forged = document.createElement('script')
    forged.dataset.pluginId = 'ext-hello'
    const currentScriptSpy = vi.spyOn(document, 'currentScript', 'get').mockReturnValue(forged)

    registerRuntime(extDescriptor())

    expect(addRoute).not.toHaveBeenCalled()
    expect(warnSpy).toHaveBeenCalledTimes(1)
    currentScriptSpy.mockRestore()
  })

  it('注入脚本在同步求值期间注册自身 ID 放行（归属一致）', () => {
    bindHost()
    expectRuntimePlugin('ext-hello')

    registerAs('ext-hello', extDescriptor())

    expect(addRoute).toHaveBeenCalled()
  })
})

describe('registerRuntime 注册生效', () => {
  beforeEach(() => {
    bindHost()
    expectRuntimePlugin('ext-hello')
  })

  it('路由经宿主注入且 meta.pluginId 强制覆写为 descriptor.id（伪造无效）', () => {
    registerAs('ext-hello', extDescriptor())

    expect(addRoute).toHaveBeenCalledTimes(1)
    expect(addedRoutes[0].meta?.pluginId).toBe('ext-hello')
    expect(addedRoutes[0].meta?.requiresAuth).toBe(true)
  })

  it('宿主 addRoute 抛错（如与核心路由冲突）：该路由 warn 跳过，其余贡献照常生效', () => {
    addRoute.mockImplementationOnce(() => {
      throw new Error('route name "Login" conflicts with an existing route')
    })

    registerAs('ext-hello', extDescriptor())

    expect(pluginNav('admin', new Set(['ext-hello']))).toHaveLength(1)
    expect(mergeLocaleMessage).toHaveBeenCalledTimes(2)
    expect(warnSpy).toHaveBeenCalledWith(
      expect.stringContaining('route'),
      expect.any(Error)
    )
  })

  it('导航项参与 pluginNav 聚合，仍受 enabled 集合门控', () => {
    registerAs('ext-hello', extDescriptor())

    const items = pluginNav('admin', new Set(['ext-hello']))
    expect(items).toHaveLength(1)
    expect(items[0].labelKey).toBe('plugins.ext-hello.navLabel')
    // enabled 集合不含该 ID → fail-closed 不渲染
    expect(pluginNav('admin', new Set())).toEqual([])
    expect(pluginNav('user', new Set(['ext-hello']))).toEqual([])
  })

  it('文案对 zh/en 各 merge 一次，且 pluginMessages 聚合含运行时插件（locale 懒加载补回路径）', () => {
    registerAs('ext-hello', extDescriptor())

    expect(mergeLocaleMessage).toHaveBeenCalledWith('zh', {
      plugins: { 'ext-hello': { navLabel: '外部演示' } }
    })
    expect(mergeLocaleMessage).toHaveBeenCalledWith('en', {
      plugins: { 'ext-hello': { navLabel: 'External Demo' } }
    })
    expect(pluginMessages('zh')).toEqual({
      plugins: { 'ext-hello': { navLabel: '外部演示' } }
    })
  })

  it('重复注册幂等：贡献只施加一次，且 warn 留痕不再静默', () => {
    registerAs('ext-hello', extDescriptor())
    registerAs('ext-hello', extDescriptor())

    expect(addRoute).toHaveBeenCalledTimes(1)
    expect(mergeLocaleMessage).toHaveBeenCalledTimes(2)
    expect(pluginNav('admin', new Set(['ext-hello']))).toHaveLength(1)
    expect(warnSpy).toHaveBeenCalledWith(expect.stringContaining('duplicate'))
  })

  it('无 i18n 的描述符不触发 merge', () => {
    registerAs('ext-hello', extDescriptor({ i18n: undefined }))
    expect(mergeLocaleMessage).not.toHaveBeenCalled()
  })
})

describe('syncRuntimeActivation 对账', () => {
  beforeEach(() => {
    bindHost()
    expectRuntimePlugin('ext-hello')
    registerAs('ext-hello', extDescriptor())
  })

  it('停用：removeRoute 逐一调用、导航与文案聚合移除', () => {
    syncRuntimeActivation(new Set())

    expect(removers).toHaveLength(1)
    expect(removers[0]).toHaveBeenCalledTimes(1)
    expect(pluginNav('admin', new Set(['ext-hello']))).toEqual([])
    expect(pluginMessages('zh')).toEqual({ plugins: {} })
  })

  it('复活：重新进入集合时直接复用缓存描述符注入路由，无需重新注册', () => {
    syncRuntimeActivation(new Set())
    expect(pluginNav('admin', new Set(['ext-hello']))).toEqual([])

    syncRuntimeActivation(new Set(['ext-hello']))

    expect(addRoute).toHaveBeenCalledTimes(2)
    expect(addedRoutes[1].meta?.pluginId).toBe('ext-hello')
    expect(pluginNav('admin', new Set(['ext-hello']))).toHaveLength(1)
  })

  it('停用撤销注册期望：晚到的注册被拒（fail-closed）', () => {
    expectRuntimePlugin('ext-late')
    syncRuntimeActivation(new Set(['ext-hello']))

    registerAs('ext-late', extDescriptor({ id: 'ext-late' }))
    expect(pluginNav('admin', new Set(['ext-late']))).toEqual([])
    expect(warnSpy).toHaveBeenCalled()
  })

  it('对账幂等：同集合重复调用无新增贡献', () => {
    syncRuntimeActivation(new Set(['ext-hello']))
    syncRuntimeActivation(new Set(['ext-hello']))

    expect(addRoute).toHaveBeenCalledTimes(1)
    expect(pluginNav('admin', new Set(['ext-hello']))).toHaveLength(1)
  })
})
