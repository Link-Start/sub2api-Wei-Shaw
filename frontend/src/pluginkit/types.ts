/**
 * 前端插件系统类型契约
 *
 * 与后端 internal/pluginkit 同构：插件以 PluginDescriptor 声明自己贡献的
 * 路由 / 导航 / 文案，registry 负责编译期聚合，enabled store 负责运行时
 * 门控（fail-closed：清单未加载或不含该插件 ID 时一律视为禁用）。
 */

import type { RouteRecordRaw } from 'vue-router'

/**
 * 插件贡献的导航项。
 *
 * 与核心 NavItem 的区别：label 是 i18n key（labelKey），
 * 接入点（AppSidebar）在渲染时经 t(labelKey) 解析为已翻译字符串。
 */
export interface PluginNavItem {
  path: string
  /** i18n key，通常位于 plugins.<id>.* 命名空间 */
  labelKey: string
  /** 可选图标组件（渲染函数对象等）；缺省时接入点使用默认插件图标 */
  icon?: unknown
  /** 可选内联 SVG 图标（与核心 NavItem.iconSvg 语义一致） */
  iconSvg?: string
  /** 排序权重，越小越靠前；缺省按 0 处理，同权重保持注册顺序 */
  order?: number
}

/**
 * 插件描述符：一个前端插件的全部编译期贡献。
 *
 * 外部插件的运行时注册（window.__SUB2API_PLUGIN_REGISTER__）使用同一结构：
 * 内建插件在编译期经 builtinPlugins 装配，外部插件在运行时经 registerRuntime
 * 注册，二者贡献形态完全一致（路由 / 导航 / 文案）。
 */
export interface PluginDescriptor {
  /** 插件 ID，必须与后端 pluginkit.ID 一致（前后端同一开关门控） */
  id: string
  /** 插件路由（组件必须懒加载）；registry 聚合时自动补 meta.pluginId */
  routes?: RouteRecordRaw[]
  /** 管理端侧边栏导航项（仅插件 enabled 时渲染） */
  adminNav?: PluginNavItem[]
  /** 用户端侧边栏导航项（仅插件 enabled 时渲染） */
  userNav?: PluginNavItem[]
  /** 插件文案，聚合挂载到 plugins.<id>.* 命名空间（zh/en 键集必须一致） */
  i18n?: { zh: object; en: object }
}

/**
 * GET /api/v1/plugins 的清单项契约（phase-4 决策 6）：
 * 外部插件带前端资产时 assets 指向其运行时入口脚本
 * （/api/v1/plugins/:id/assets/plugin.js）；内建插件无 assets 字段。
 */
export interface EnabledPluginEntry {
  id: string
  assets?: string
}

/** 外部插件脚本调用的运行时注册回调签名 */
export type RegisterRuntimeFn = (descriptor: PluginDescriptor) => void

declare global {
  interface Window {
    /**
     * 外部插件前端半场的注册入口（由 pluginkit registry 挂载）。
     * 插件 IIFE 脚本加载后调用它提交自己的 PluginDescriptor；
     * 未经 loader 登记为期望 ID 的注册一律被拒绝（fail-closed）。
     */
    __SUB2API_PLUGIN_REGISTER__?: RegisterRuntimeFn
  }
}

declare module 'vue-router' {
  interface RouteMeta {
    /**
     * 所属插件 ID（由 pluginkit registry 聚合路由时自动补写）。
     * 存在时路由守卫按 enabled 清单做 fail-closed 门控：
     * 清单未加载或不含该 ID → NotFound。
     */
    pluginId?: string
  }
}
