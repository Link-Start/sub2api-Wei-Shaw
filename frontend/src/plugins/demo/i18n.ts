/**
 * demo 插件文案，经 registry 聚合挂载到 plugins.demo.* 命名空间。
 * zh/en 键集必须一致（registry 守护测试锁定）。
 */
export const demoMessages = {
  zh: {
    navLabel: '插件演示',
    title: '插件演示',
    description: 'demo 插件的前端半场：本页数据来自 demo 插件后端 API，验证前后端同一开关联动',
    greeting: '问候语',
    uptimeSeconds: '运行时长（秒）',
    loadFailed: '加载 demo 插件数据失败',
    refresh: '刷新'
  },
  en: {
    navLabel: 'Plugin Demo',
    title: 'Plugin Demo',
    description: 'Frontend half of the demo plugin: this page reads from the demo plugin backend API, proving the full-stack single-switch wiring',
    greeting: 'Greeting',
    uptimeSeconds: 'Uptime (seconds)',
    loadFailed: 'Failed to load demo plugin data',
    refresh: 'Refresh'
  }
}

export default demoMessages
