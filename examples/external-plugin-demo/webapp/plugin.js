/**
 * demo-external 前端运行时入口（手写 IIFE，不依赖任何构建器）。
 *
 * 宿主前端（frontend/src/pluginkit）在插件 enabled 时经 <script src> 注入本文件，
 * 并暴露两个全局：
 *   - window.__SUB2API_PLUGIN_REGISTER__  registerRuntime(descriptor)
 *   - window.__SUB2API_PLUGIN_VUE__       { defineComponent, h }（供无构建器的
 *     外部插件内联渲染组件；缺失时本插件退化为只注册 i18n，不挂路由/导航）
 *
 * descriptor 契约与 frontend/src/pluginkit/types.ts 的 PluginDescriptor 一致。
 */
;(function () {
  'use strict'

  var PLUGIN_ID = 'demo-external'

  var register = window.__SUB2API_PLUGIN_REGISTER__
  if (typeof register !== 'function') {
    console.warn('[' + PLUGIN_ID + '] __SUB2API_PLUGIN_REGISTER__ missing, skip registration')
    return
  }

  var descriptor = {
    id: PLUGIN_ID,
    i18n: {
      zh: {
        nav: '外部演示',
        title: '外部演示插件',
        intro: '本页面由外部插件包在运行时注入（webapp/plugin.js），无需重新构建前端。',
      },
      en: {
        nav: 'External Demo',
        title: 'External Demo Plugin',
        intro: 'This view is injected at runtime by an external plugin package (webapp/plugin.js), no frontend rebuild required.',
      },
    },
  }

  var vue = window.__SUB2API_PLUGIN_VUE__
  if (vue && typeof vue.h === 'function' && typeof vue.defineComponent === 'function') {
    var h = vue.h
    var DemoView = vue.defineComponent({
      name: 'ExternalDemoView',
      data: function () {
        return { info: null, error: '' }
      },
      mounted: function () {
        var self = this
        // 经宿主分发器调用本插件的 user 侧 API（同源请求自动携带前端凭据头，
        // 由宿主 axios/fetch 拦截器之外的场景可能 401——演示容错展示）。
        fetch('/api/v1/plugins/' + PLUGIN_ID + '/api/info', { credentials: 'same-origin' })
          .then(function (resp) {
            if (!resp.ok) throw new Error('HTTP ' + resp.status)
            return resp.json()
          })
          .then(function (data) {
            self.info = data
          })
          .catch(function (err) {
            self.error = String(err)
          })
      },
      render: function () {
        return h('div', { style: 'padding:24px;display:flex;flex-direction:column;gap:12px' }, [
          h('h1', { style: 'font-size:20px;font-weight:600' }, this.$t('plugins.' + PLUGIN_ID + '.title')),
          h('p', null, this.$t('plugins.' + PLUGIN_ID + '.intro')),
          this.info
            ? h('pre', { style: 'font-size:12px;background:rgba(127,127,127,.1);padding:12px;border-radius:8px' },
                JSON.stringify(this.info, null, 2))
            : h('p', { style: 'opacity:.6' }, this.error || 'loading…'),
        ])
      },
    })

    descriptor.routes = [
      {
        path: '/plugins/demo-external',
        name: 'plugin-demo-external',
        component: DemoView,
      },
    ]
    descriptor.userNav = [
      {
        path: '/plugins/demo-external',
        labelKey: 'plugins.' + PLUGIN_ID + '.nav',
      },
    ]
  } else {
    console.warn('[' + PLUGIN_ID + '] __SUB2API_PLUGIN_VUE__ missing, register i18n only')
  }

  register(descriptor)
})()
