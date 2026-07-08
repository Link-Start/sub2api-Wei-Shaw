import { apiClient } from '@/api/client'

/** 插件运行状态（对齐后端 pluginkit.State） */
export type PluginState = 'disabled' | 'running' | 'failed' | 'stopped'

/** 插件层级：builtin=编译期装配的内建插件，external=上传安装的外部插件 */
export type PluginTier = 'builtin' | 'external'

/** 插件状态快照（对齐后端 pluginkit.PluginStatus + 外部层合并快照） */
export interface PluginStatus {
  id: string
  enabled: boolean
  state: PluginState
  error?: string
  started_at?: string
  /** 外部层快照合并后透出；缺省按 builtin 处理（内建快照不带该字段时兼容） */
  tier?: PluginTier
}

/** 上传安装/升级成功的响应载荷（对齐后端 externalPluginInstalled） */
export interface ExternalPluginInstalled {
  id: string
  version: string
  checksum: string
  installed_by: string
  installed_at: string
}

/** config GET 的响应载荷：当前配置 + 清单声明的 schema（对齐后端 pluginConfigPayload） */
export interface PluginConfigPayload {
  config: unknown
  config_schema?: unknown
}

// 上传大包（默认上限 64MB）耗时远超 apiClient 默认 30s 超时，单独放宽
const UPLOAD_TIMEOUT_MS = 300_000

export const adminPluginsAPI = {
  /** 全部注册插件的状态快照（内建 + 外部，按 ID 稳定排序） */
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
  },

  /** 上传 zip 插件包并安装（同 ID 已安装时走升级路径）；安装后默认 disabled */
  async uploadPlugin(
    file: File,
    onProgress?: (percent: number) => void
  ): Promise<ExternalPluginInstalled> {
    const form = new FormData()
    form.append('file', file)
    const { data } = await apiClient.post<ExternalPluginInstalled>('/admin/plugins/upload', form, {
      headers: { 'Content-Type': 'multipart/form-data' },
      timeout: UPLOAD_TIMEOUT_MS,
      onUploadProgress: (event) => {
        if (onProgress && typeof event.total === 'number' && event.total > 0) {
          onProgress(Math.round((event.loaded / event.total) * 100))
        }
      }
    })
    return data
  },

  /** 卸载外部插件（仅外部层；若启用先停 → 删文件 → 删登记） */
  async uninstallPlugin(id: string): Promise<void> {
    await apiClient.delete(`/admin/plugins/${encodeURIComponent(id)}`)
  },

  /** 读取外部插件的私有配置与清单声明的 config_schema */
  async getPluginConfig(id: string): Promise<PluginConfigPayload> {
    const { data } = await apiClient.get<PluginConfigPayload>(
      `/admin/plugins/${encodeURIComponent(id)}/config`
    )
    return data
  },

  /**
   * 写入外部插件的私有配置。configJson 为原始 JSON 文本（请求体即配置本体，
   * 调用方须先校验合法性）；变更对运行中的插件进程在重启后生效。
   */
  async putPluginConfig(id: string, configJson: string): Promise<unknown> {
    const { data } = await apiClient.put<unknown>(
      `/admin/plugins/${encodeURIComponent(id)}/config`,
      configJson,
      { headers: { 'Content-Type': 'application/json' } }
    )
    return data
  }
}

export default adminPluginsAPI
