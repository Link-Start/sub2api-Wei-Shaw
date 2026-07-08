/**
 * enabled 插件清单 store
 *
 * 唯一事实源是后端 GET /api/v1/plugins（仅返回 enabled 插件清单，
 * 契约为 [{id, assets?}]：外部插件带前端资产时 assets 指向运行时入口）。
 * fail-closed 语义：未加载（loaded=false）或拉取失败时，isEnabled 一律
 * 返回 false —— 插件路由被守卫拦到 NotFound、插件导航项不渲染；
 * externalAssets 同步清空 —— 已加载的外部运行时贡献被 loader 对账移除。
 */

import { defineStore } from 'pinia'
import { ref } from 'vue'
import { apiClient } from '@/api/client'
import type { EnabledPluginEntry } from './types'

// isEnabledPluginEntry 校验清单项形态：非对象/缺 id 的脏数据一律丢弃（fail-closed）
function isEnabledPluginEntry(value: unknown): value is EnabledPluginEntry {
  if (value === null || typeof value !== 'object') {
    return false
  }
  const entry = value as { id?: unknown }
  return typeof entry.id === 'string' && entry.id !== ''
}

export const useEnabledPluginsStore = defineStore('enabledPlugins', () => {
  const ids = ref<Set<string>>(new Set())
  // 外部插件的前端资产入口映射（id → assets URL）；每次拉取整体重建
  // （替换引用），供 App.vue watch 驱动 loader 对账加载/停用
  const externalAssets = ref<ReadonlyMap<string, string>>(new Map())
  const loaded = ref(false)

  // 并发拉取的代际计数：仅最新一次 fetchEnabled 的完成可以写入状态，
  // 防止先发出的过期响应晚到后覆盖新结果（如登录预拉取与启停后 refresh 交错）
  let fetchGeneration = 0

  /**
   * 拉取 enabled 插件清单（登录态建立后由 App.vue 触发）。
   * 失败时不抛错：清空集合与资产映射并保持 loaded=false（fail-closed）。
   */
  async function fetchEnabled(): Promise<void> {
    const generation = ++fetchGeneration
    try {
      const { data } = await apiClient.get<EnabledPluginEntry[]>('/plugins')
      if (generation !== fetchGeneration) return
      const entries = Array.isArray(data) ? data.filter(isEnabledPluginEntry) : []
      ids.value = new Set(entries.map((entry) => entry.id))
      const assets = new Map<string, string>()
      for (const entry of entries) {
        if (typeof entry.assets === 'string' && entry.assets !== '') {
          assets.set(entry.id, entry.assets)
        }
      }
      externalAssets.value = assets
      loaded.value = true
    } catch {
      if (generation !== fetchGeneration) return
      ids.value = new Set()
      externalAssets.value = new Map()
      loaded.value = false
    }
  }

  /** 插件是否启用；清单未加载一律返回 false（fail-closed） */
  function isEnabled(id: string): boolean {
    return loaded.value && ids.value.has(id)
  }

  /** 重新拉取清单（插件管理页启停操作后调用） */
  async function refresh(): Promise<void> {
    await fetchEnabled()
  }

  /** 登出时清空（回到 fail-closed 初始态）；同时作废所有在途拉取 */
  function reset(): void {
    fetchGeneration++
    ids.value = new Set()
    externalAssets.value = new Map()
    loaded.value = false
  }

  return {
    ids,
    externalAssets,
    loaded,
    fetchEnabled,
    isEnabled,
    refresh,
    reset
  }
})
