/**
 * i18n 接入守护测试（⑥）：
 * locale 懒加载完成后插件文案已 merge 到 plugins.<id>.* 命名空间，
 * zh/en 两种语言均已就位，运行时切换语言后仍可解析；
 * 核心插件管理页文案（admin.plugins.* / nav.plugins）同步就位。
 *
 * 注意：vitest 环境使用 vue-i18n runtime 构建且未开 JIT 编译（对齐
 * 既有 i18n specs 的约定），因此断言消息树与 te() 可解析性而非 t() 产出。
 */
import { describe, expect, it } from 'vitest'

import { i18n, loadLocaleMessages } from '@/i18n'

type MessageTree = Record<string, any>

describe('i18n 插件文案 merge', () => {
  it('zh/en 加载后插件文案均已 merge 到 plugins.demo.* 命名空间', async () => {
    await loadLocaleMessages('zh')
    await loadLocaleMessages('en')

    const zh = i18n.global.getLocaleMessage('zh') as MessageTree
    const en = i18n.global.getLocaleMessage('en') as MessageTree

    expect(zh.plugins.demo.title).toBe('插件演示')
    expect(zh.plugins.demo.navLabel).toBe('插件演示')
    expect(en.plugins.demo.title).toBe('Plugin Demo')
    expect(en.plugins.demo.navLabel).toBe('Plugin Demo')
  })

  it('核心插件管理页文案（admin.plugins.* / nav.plugins）两种语言均就位', async () => {
    await loadLocaleMessages('zh')
    await loadLocaleMessages('en')

    const zh = i18n.global.getLocaleMessage('zh') as MessageTree
    const en = i18n.global.getLocaleMessage('en') as MessageTree

    expect(zh.admin.plugins.title).toBe('插件管理')
    expect(zh.nav.plugins).toBe('插件管理')
    expect(en.admin.plugins.title).toBe('Plugin Management')
    expect(en.nav.plugins).toBe('Plugins')
  })

  it('运行时切换语言后插件文案在目标语言下仍可解析（te 为真）', async () => {
    await loadLocaleMessages('zh')
    await loadLocaleMessages('en')

    i18n.global.locale.value = 'zh'
    expect(i18n.global.te('plugins.demo.title')).toBe(true)
    expect(i18n.global.te('admin.plugins.title')).toBe(true)

    i18n.global.locale.value = 'en'
    expect(i18n.global.te('plugins.demo.title')).toBe(true)
    expect(i18n.global.te('admin.plugins.title')).toBe(true)

    // 切回 zh 仍生效（merge 不因切换丢失）
    i18n.global.locale.value = 'zh'
    expect(i18n.global.te('plugins.demo.navLabel')).toBe(true)
  })

  it('merge 只新增 plugins.* 子树，不覆盖核心文案', async () => {
    await loadLocaleMessages('zh')

    const zh = i18n.global.getLocaleMessage('zh') as MessageTree
    expect(zh.nav.settings).toBe('系统设置')
    expect(zh.nav.dashboard).toBe('仪表盘')
  })
})
