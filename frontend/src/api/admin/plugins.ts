import { apiClient } from '@/api/client'

/** 插件运行状态（对齐后端 pluginkit.State） */
export type PluginState = 'disabled' | 'running' | 'failed' | 'stopped'

/** 插件状态快照（对齐后端 pluginkit.PluginStatus） */
export interface PluginStatus {
  id: string
  enabled: boolean
  state: PluginState
  error?: string
  started_at?: string
}

export const adminPluginsAPI = {
  /** 全部注册插件的状态快照（按 ID 稳定排序） */
  async listPlugins(): Promise<PluginStatus[]> {
    const { data } = await apiClient.get<PluginStatus[]>('/admin/plugins')
    return data
  },

  /** 免重启启用插件，返回该插件最新状态快照 */
  async enablePlugin(id: string): Promise<PluginStatus> {
    const { data } = await apiClient.post<PluginStatus>(
      `/admin/plugins/${encodeURIComponent(id)}/enable`
    )
    return data
  },

  /** 免重启停用插件，返回该插件最新状态快照 */
  async disablePlugin(id: string): Promise<PluginStatus> {
    const { data } = await apiClient.post<PluginStatus>(
      `/admin/plugins/${encodeURIComponent(id)}/disable`
    )
    return data
  }
}

export default adminPluginsAPI
