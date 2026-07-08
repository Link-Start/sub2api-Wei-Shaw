/**
 * demo 插件前端半场：与后端 internal/plugins/demo 同 ID（"demo"），
 * 前后端由同一个启停开关门控——后端 disabled 时本路由被守卫拦到 NotFound、
 * 导航项不渲染，API 分发器返回 404。
 *
 * 作为新前端插件的起点模板：路由组件必须懒加载（未启用时首屏不引用
 * 插件 chunk），meta.pluginId 无需手写（registry 聚合时自动补）。
 */
import type { PluginDescriptor } from '@/pluginkit/types'
import { demoMessages } from './i18n'

export const demoPlugin: PluginDescriptor = {
  id: 'demo',
  routes: [
    {
      path: '/admin/plugins-demo',
      name: 'PluginDemo',
      component: () => import('./DemoView.vue'),
      meta: {
        requiresAuth: true,
        requiresAdmin: true,
        title: 'Plugin Demo',
        titleKey: 'plugins.demo.title',
        descriptionKey: 'plugins.demo.description'
      }
    }
  ],
  adminNav: [
    {
      path: '/admin/plugins-demo',
      labelKey: 'plugins.demo.navLabel'
    }
  ],
  i18n: demoMessages
}

export default demoPlugin
