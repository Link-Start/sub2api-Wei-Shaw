/**
 * 注册表守护测试：
 *   ① 测试注入/清空清单——清空时三个聚合函数零贡献（"零行为变更"的根基）
 *   ⑤ demo descriptor 结构完整性（接口契约 + zh/en 键集一致）
 */
import { afterEach, describe, expect, it } from 'vitest'
import type { RouteRecordRaw } from 'vue-router'

import {
  builtinPlugins,
  pluginMessages,
  pluginNav,
  pluginRoutes,
  pluginSettingsLoader,
  _resetPluginsForTest,
  _setPluginsForTest
} from '../registry'
import type { PluginDescriptor, PluginSettingsLoader } from '../types'
import { contentModerationPlugin } from '@/plugins/content-moderation'
import { demoPlugin } from '@/plugins/demo'

// flattenKeys 展平嵌套文案对象为 "a.b.c" 键列表，用于 zh/en 键集一致性断言
function flattenKeys(messages: object, prefix = ''): string[] {
  const keys: string[] = []
  for (const [key, value] of Object.entries(messages)) {
    const path = prefix ? `${prefix}.${key}` : key
    if (value !== null && typeof value === 'object') {
      keys.push(...flattenKeys(value as object, path))
    } else {
      keys.push(path)
    }
  }
  return keys.sort()
}

afterEach(() => {
  _resetPluginsForTest()
})

describe('pluginkit registry', () => {
  describe('清单清空（守护①：零行为变更）', () => {
    it('清空清单后 pluginRoutes() 返回空数组', () => {
      _setPluginsForTest([])
      expect(pluginRoutes()).toEqual([])
    })

    it('清空清单后 pluginNav() 两侧均为空', () => {
      _setPluginsForTest([])
      const allEnabled = new Set(['demo'])
      expect(pluginNav('admin', allEnabled)).toEqual([])
      expect(pluginNav('user', allEnabled)).toEqual([])
    })

    it('清空清单后 pluginMessages() 无任何插件文案', () => {
      _setPluginsForTest([])
      expect(pluginMessages('zh')).toEqual({ plugins: {} })
      expect(pluginMessages('en')).toEqual({ plugins: {} })
    })

    it('_resetPluginsForTest 恢复编译期装配清单', () => {
      _setPluginsForTest([])
      expect(pluginRoutes()).toEqual([])
      _resetPluginsForTest()
      expect(pluginRoutes().length).toBeGreaterThan(0)
    })
  })

  describe('enabled 门控（导航项）', () => {
    it('enabled 空集时不产出任何插件导航项（fail-closed 形态）', () => {
      expect(pluginNav('admin', new Set())).toEqual([])
      expect(pluginNav('user', new Set())).toEqual([])
    })

    it('enabled 含 demo 时产出 demo 的 adminNav，user 侧仍为空', () => {
      const enabled = new Set(['demo'])
      const adminItems = pluginNav('admin', enabled)
      expect(adminItems).toHaveLength(1)
      expect(adminItems[0].path).toBe('/admin/plugins-demo')
      expect(adminItems[0].labelKey).toBe('plugins.demo.navLabel')
      expect(pluginNav('user', enabled)).toEqual([])
    })

    it('导航项按 order 升序排序，缺省按 0 处理', () => {
      const plugins: PluginDescriptor[] = [
        { id: 'a', adminNav: [{ path: '/a', labelKey: 'plugins.a.nav', order: 5 }] },
        { id: 'b', adminNav: [{ path: '/b', labelKey: 'plugins.b.nav' }] },
        { id: 'c', adminNav: [{ path: '/c', labelKey: 'plugins.c.nav', order: -1 }] }
      ]
      _setPluginsForTest(plugins)
      const items = pluginNav('admin', new Set(['a', 'b', 'c']))
      expect(items.map((item) => item.path)).toEqual(['/c', '/b', '/a'])
    })
  })

  describe('路由聚合', () => {
    it('自动补写 meta.pluginId，且不覆盖路由其余 meta', () => {
      const routes = pluginRoutes()
      const demoRoute = routes.find((route) => route.path === '/admin/plugins-demo')
      expect(demoRoute).toBeDefined()
      expect(demoRoute!.meta?.pluginId).toBe('demo')
      expect(demoRoute!.meta?.requiresAuth).toBe(true)
      expect(demoRoute!.meta?.requiresAdmin).toBe(true)
    })

    it('meta.pluginId 以 descriptor.id 为准（即使插件路由手写了别的值）', () => {
      const plugins: PluginDescriptor[] = [
        {
          id: 'real-id',
          routes: [
            {
              path: '/x',
              component: () => Promise.resolve({ default: {} }),
              meta: { pluginId: 'forged-id' }
            } as RouteRecordRaw
          ]
        }
      ]
      _setPluginsForTest(plugins)
      expect(pluginRoutes()[0].meta?.pluginId).toBe('real-id')
    })

    it('children 路由同样被递归覆写 pluginId（子路由伪造别的插件 ID 无效）', () => {
      const plugins: PluginDescriptor[] = [
        {
          id: 'real-id',
          routes: [
            {
              path: '/x',
              component: () => Promise.resolve({ default: {} }),
              children: [
                {
                  path: 'child',
                  component: () => Promise.resolve({ default: {} }),
                  meta: { pluginId: 'forged-id', requiresAuth: true },
                  children: [
                    {
                      path: 'grandchild',
                      component: () => Promise.resolve({ default: {} })
                    }
                  ]
                }
              ]
            } as RouteRecordRaw
          ]
        }
      ]
      _setPluginsForTest(plugins)
      const [route] = pluginRoutes()
      const child = route.children![0]
      expect(child.meta?.pluginId).toBe('real-id')
      expect(child.meta?.requiresAuth).toBe(true)
      expect(child.children![0].meta?.pluginId).toBe('real-id')
    })
  })

  describe('插件设置面板查找（pluginSettingsLoader）', () => {
    it('声明了 settings 的插件返回其懒加载器', () => {
      const loader: PluginSettingsLoader = () =>
        Promise.resolve({ default: {} as never })
      _setPluginsForTest([{ id: 'with-settings', settings: loader }])
      expect(pluginSettingsLoader('with-settings')).toBe(loader)
    })

    it('未声明 settings 的插件与未注册的 ID 均返回 null', () => {
      _setPluginsForTest([{ id: 'plain' }])
      expect(pluginSettingsLoader('plain')).toBeNull()
      expect(pluginSettingsLoader('unknown')).toBeNull()
    })

    it('清空清单后一律返回 null（零行为变更）', () => {
      _setPluginsForTest([])
      expect(pluginSettingsLoader('content-moderation')).toBeNull()
    })
  })

  describe('content-moderation descriptor 结构完整性', () => {
    it('在编译期装配清单中，ID 与后端一致', () => {
      expect(builtinPlugins).toContain(contentModerationPlugin)
      expect(contentModerationPlugin.id).toBe('content-moderation')
    })

    it('原位收编：不贡献路由/导航，仅贡献设置面板（懒加载函数）', () => {
      expect(contentModerationPlugin.routes).toBeUndefined()
      expect(contentModerationPlugin.adminNav).toBeUndefined()
      expect(contentModerationPlugin.userNav).toBeUndefined()
      expect(typeof contentModerationPlugin.settings).toBe('function')
      expect(pluginSettingsLoader('content-moderation')).toBe(contentModerationPlugin.settings)
    })

    it('i18n zh/en 键集完全一致', () => {
      expect(contentModerationPlugin.i18n).toBeDefined()
      const zhKeys = flattenKeys(contentModerationPlugin.i18n!.zh)
      const enKeys = flattenKeys(contentModerationPlugin.i18n!.en)
      expect(zhKeys).toEqual(enKeys)
    })
  })

  describe('demo descriptor 结构完整性（守护⑤）', () => {
    it('demo 在编译期装配清单中', () => {
      expect(builtinPlugins).toContain(demoPlugin)
    })

    it('id 与后端 pluginkit.ID 一致', () => {
      expect(demoPlugin.id).toBe('demo')
    })

    it('路由声明完整且组件为懒加载函数', () => {
      expect(demoPlugin.routes).toHaveLength(1)
      const route = demoPlugin.routes![0]
      expect(route.path).toBe('/admin/plugins-demo')
      expect(route.name).toBe('PluginDemo')
      expect(typeof (route as { component?: unknown }).component).toBe('function')
      expect(route.meta?.requiresAuth).toBe(true)
      expect(route.meta?.requiresAdmin).toBe(true)
      expect(route.meta?.titleKey).toBe('plugins.demo.title')
    })

    it('adminNav 指向自己的路由且 labelKey 落在 plugins.demo.* 命名空间', () => {
      expect(demoPlugin.adminNav).toHaveLength(1)
      expect(demoPlugin.adminNav![0].path).toBe('/admin/plugins-demo')
      expect(demoPlugin.adminNav![0].labelKey.startsWith('plugins.demo.')).toBe(true)
      expect(demoPlugin.userNav).toBeUndefined()
    })

    it('i18n zh/en 键集完全一致且覆盖 nav/title 关键键', () => {
      expect(demoPlugin.i18n).toBeDefined()
      const zhKeys = flattenKeys(demoPlugin.i18n!.zh)
      const enKeys = flattenKeys(demoPlugin.i18n!.en)
      expect(zhKeys).toEqual(enKeys)
      expect(zhKeys).toContain('navLabel')
      expect(zhKeys).toContain('title')
    })

    it('pluginMessages 将 demo 文案挂到 plugins.demo 命名空间', () => {
      const zh = pluginMessages('zh') as { plugins: Record<string, object> }
      const en = pluginMessages('en') as { plugins: Record<string, object> }
      expect(zh.plugins.demo).toBe(demoPlugin.i18n!.zh)
      expect(en.plugins.demo).toBe(demoPlugin.i18n!.en)
    })
  })
})
