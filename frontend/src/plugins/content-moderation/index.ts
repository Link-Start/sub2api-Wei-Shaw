/**
 * content-moderation 插件前端半场：与后端 internal/plugins/moderation 同 ID，
 * 前后端由同一个启停开关门控。
 *
 * 原位收编（Phase-6 决策）：风控中心路由（/admin/risk-control）与导航项
 * 仍在核心（手写 meta.pluginId + 导航叠加 isEnabled），本描述符不重复贡献
 * 路由/导航——只贡献插件设置面板（插件管理页卡片的"设置"入口）。
 */
import type { PluginDescriptor } from '@/pluginkit/types'
import { contentModerationMessages } from './i18n'

export const contentModerationPlugin: PluginDescriptor = {
  id: 'content-moderation',
  settings: () => import('./SettingsPanel.vue'),
  i18n: contentModerationMessages
}

export default contentModerationPlugin
