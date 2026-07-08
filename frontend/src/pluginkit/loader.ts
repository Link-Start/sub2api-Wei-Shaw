/**
 * 外部插件前端运行时加载器
 *
 * loadExternalPlugins(list) 是唯一入口（App.vue 在 enabled 清单每次落盘后
 * 调用）：对 enabled 且带 assets 的外部插件注入 <script>（IIFE），脚本内部
 * 调用 window.__SUB2API_PLUGIN_REGISTER__ 提交描述符完成注册。
 *
 * fail-closed 与隔离语义：
 *   - 注入前经 expectRuntimePlugin 登记期望 ID，registerRuntime 回调时核对，
 *     任意脚本注册任意 ID 被拒；
 *   - 单个脚本加载失败/超时仅 console.warn 后跳过，不影响其他插件与核心应用；
 *   - 脚本按插件 ID 只注入一次（含失败：本会话不重试，页面刷新自然重置）；
 *   - 停用（清单不再包含）时经 syncRuntimeActivation 移除路由与导航贡献，
 *     同一会话内重新启用直接复活缓存描述符，不重复注入脚本。
 */

import router from '@/router'
import { i18n } from '@/i18n'
import { buildApiUrl } from '@/api/url'
import { expectRuntimePlugin, setRuntimeHost, syncRuntimeActivation } from './registry'
import type { EnabledPluginEntry } from './types'

// 脚本加载超时：超过后视为失败（脚本若之后仍完成加载并注册，
// 期望 ID 仍在集合内，晚到的注册照常生效，仅告警时序不同）
const SCRIPT_LOAD_TIMEOUT_MS = 15_000

// 本会话已注入过脚本的插件 ID（成功与否均记录）
const injectedScriptIds = new Set<string>()

let runtimeHostBound = false

// ensureRuntimeHost 把真实 router / i18n 以最小接口绑定进 registry。
// 绑定收敛在 loader 内：registry 与各接入点/测试不因此拖入整棵应用模块图。
function ensureRuntimeHost(): void {
  if (runtimeHostBound) {
    return
  }
  setRuntimeHost({
    addRoute: (route) => router.addRoute(route),
    mergeLocaleMessage: (locale, messages) => i18n.global.mergeLocaleMessage(locale, messages)
  })
  runtimeHostBound = true
}

// resolveAssetUrl 把后端下发的 assets 路径（/api/v1/...）按 API base 解析：
// 前后端分离部署（VITE_API_BASE_URL 为完整 URL）时指向后端源，绝对 URL 原样使用
function resolveAssetUrl(assets: string): string {
  if (/^https?:\/\//i.test(assets)) {
    return assets
  }
  return buildApiUrl(assets)
}

// injectPluginScript 注入单个插件脚本，onload/onerror/超时三态收敛为一次 settle
function injectPluginScript(id: string, assets: string): Promise<void> {
  return new Promise<void>((resolve, reject) => {
    const script = document.createElement('script')
    script.async = true
    script.dataset.pluginId = id

    let settled = false
    const timer = window.setTimeout(() => {
      settle(new Error(`script load timed out after ${SCRIPT_LOAD_TIMEOUT_MS}ms`))
    }, SCRIPT_LOAD_TIMEOUT_MS)

    function settle(error: Error | null): void {
      if (settled) {
        return
      }
      settled = true
      window.clearTimeout(timer)
      if (error === null) {
        resolve()
      } else {
        reject(error)
      }
    }

    script.onload = () => settle(null)
    script.onerror = () => settle(new Error(`failed to load ${script.src}`))
    script.src = resolveAssetUrl(assets)
    document.head.appendChild(script)
  })
}

/**
 * 按最新 enabled 清单加载/对账外部插件前端半场。
 *
 * 幂等：重复调用同一清单不产生新脚本注入；清单收缩时移除对应运行时贡献。
 * 不抛错：所有失败均降级为 console.warn（enabled 拉取链路不因插件受阻）。
 */
export async function loadExternalPlugins(plugins: readonly EnabledPluginEntry[]): Promise<void> {
  ensureRuntimeHost()

  const withAssets = plugins.filter(
    (plugin): plugin is EnabledPluginEntry & { assets: string } =>
      typeof plugin.id === 'string' &&
      plugin.id !== '' &&
      typeof plugin.assets === 'string' &&
      plugin.assets !== ''
  )

  // 先对账既有运行时状态：停用集合外的贡献并撤销其注册期望，
  // 再为新插件登记期望、注入脚本
  syncRuntimeActivation(new Set(withAssets.map((plugin) => plugin.id)))

  const injections: Promise<void>[] = []
  for (const plugin of withAssets) {
    if (injectedScriptIds.has(plugin.id)) {
      continue
    }
    injectedScriptIds.add(plugin.id)
    expectRuntimePlugin(plugin.id)
    injections.push(
      injectPluginScript(plugin.id, plugin.assets).catch((error: unknown) => {
        console.warn(`[pluginkit] failed to load external plugin "${plugin.id}":`, error)
      })
    )
  }
  await Promise.all(injections)
}

/** 仅测试使用：清空脚本注入记录与宿主绑定标记 */
export function _resetLoaderForTest(): void {
  injectedScriptIds.clear()
  runtimeHostBound = false
}
