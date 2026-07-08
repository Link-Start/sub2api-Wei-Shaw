/**
 * 前端插件注册表
 *
 * builtinPlugins 是唯一的编译期装配清单（对齐后端
 * internal/plugins/builtin.go 的角色）：新增前端插件 = 在清单中加一行。
 *
 * 三个聚合函数是四个接入点的唯一数据来源：
 *   - pluginRoutes()   → router/index.ts 静态路由表末尾展开
 *   - pluginNav()      → AppSidebar admin/user 导航计算属性 concat
 *   - pluginMessages() → i18n locale 懒加载完成后 merge
 *
 * 清单为空时三者均返回空贡献，前端行为与未引入插件系统时一致
 * （由 pluginkit/__tests__ 守护测试锁定）。
 */

import type { RouteRecordRaw } from 'vue-router'
import type { PluginDescriptor, PluginNavItem } from './types'
import { demoPlugin } from '@/plugins/demo'

/** ⭐ 唯一编译期装配清单：本阶段仅 demo 插件 */
export const builtinPlugins: PluginDescriptor[] = [demoPlugin]

// activePlugins 是聚合函数实际读取的清单；生产环境恒等于 builtinPlugins，
// 仅守护测试可经 _setPluginsForTest 注入/清空，用于锁定"零行为变更"。
let activePlugins: readonly PluginDescriptor[] = builtinPlugins

/** 仅测试使用：注入替代清单（传 [] 即模拟"未装配任何插件"） */
export function _setPluginsForTest(plugins: readonly PluginDescriptor[]): void {
  activePlugins = plugins
}

/** 仅测试使用：恢复编译期装配清单 */
export function _resetPluginsForTest(): void {
  activePlugins = builtinPlugins
}

// stampPluginId 以 descriptor.id 递归覆写路由记录（含 children）的 meta.pluginId：
// 插件路由无论在哪一层手写 pluginId 均无效，守卫门控的归属不可伪造。
function stampPluginId(route: RouteRecordRaw, pluginId: string): RouteRecordRaw {
  const stamped = {
    ...route,
    meta: { ...route.meta, pluginId }
  } as RouteRecordRaw
  if (route.children && route.children.length > 0) {
    stamped.children = route.children.map((child) => stampPluginId(child, pluginId))
  }
  return stamped
}

/**
 * 全部插件路由（meta.pluginId 已自动补写，供路由守卫做 enabled 门控）。
 */
export function pluginRoutes(): RouteRecordRaw[] {
  const routes: RouteRecordRaw[] = []
  for (const plugin of activePlugins) {
    for (const route of plugin.routes ?? []) {
      routes.push(stampPluginId(route, plugin.id))
    }
  }
  return routes
}

/**
 * 指定侧（admin/user）的插件导航项，仅包含 enabled 集合内的插件。
 * 按 order 升序（缺省 0），同权重保持清单注册顺序（Array.sort 稳定）。
 */
export function pluginNav(side: 'admin' | 'user', enabled: ReadonlySet<string>): PluginNavItem[] {
  const items: PluginNavItem[] = []
  for (const plugin of activePlugins) {
    if (!enabled.has(plugin.id)) continue
    const nav = side === 'admin' ? plugin.adminNav : plugin.userNav
    if (nav) {
      items.push(...nav)
    }
  }
  return items.sort((a, b) => (a.order ?? 0) - (b.order ?? 0))
}

/**
 * 指定语言的全部插件文案，聚合为 { plugins: { <id>: <messages> } }，
 * 由 i18n 接入点 mergeLocaleMessage 挂载到 plugins.<id>.* 命名空间。
 */
export function pluginMessages(locale: 'zh' | 'en'): Record<string, unknown> {
  const byId: Record<string, object> = {}
  for (const plugin of activePlugins) {
    const messages = plugin.i18n?.[locale]
    if (messages) {
      byId[plugin.id] = messages
    }
  }
  return { plugins: byId }
}
