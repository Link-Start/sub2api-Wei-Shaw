/**
 * enabled 插件清单 store
 *
 * 唯一事实源是后端 GET /api/v1/plugins（仅返回 enabled 插件 ID 列表）。
 * fail-closed 语义：未加载（loaded=false）或拉取失败时，isEnabled 一律
 * 返回 false —— 插件路由被守卫拦到 NotFound、插件导航项不渲染。
 */

import { defineStore } from 'pinia'
import { ref } from 'vue'
import { apiClient } from '@/api/client'

export const useEnabledPluginsStore = defineStore('enabledPlugins', () => {
  const ids = ref<Set<string>>(new Set())
  const loaded = ref(false)

  // 并发拉取的代际计数：仅最新一次 fetchEnabled 的完成可以写入状态，
  // 防止先发出的过期响应晚到后覆盖新结果（如登录预拉取与启停后 refresh 交错）
  let fetchGeneration = 0

  /**
   * 拉取 enabled 插件 ID 清单（登录态建立后由 App.vue 触发）。
   * 失败时不抛错：清空集合并保持 loaded=false（fail-closed）。
   */
  async function fetchEnabled(): Promise<void> {
    const generation = ++fetchGeneration
    try {
      const { data } = await apiClient.get<string[]>('/plugins')
      if (generation !== fetchGeneration) return
      ids.value = new Set(Array.isArray(data) ? data : [])
      loaded.value = true
    } catch {
      if (generation !== fetchGeneration) return
      ids.value = new Set()
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
    loaded.value = false
  }

  return {
    ids,
    loaded,
    fetchEnabled,
    isEnabled,
    refresh,
    reset
  }
})
