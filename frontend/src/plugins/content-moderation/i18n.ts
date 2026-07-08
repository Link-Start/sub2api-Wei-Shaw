/**
 * content-moderation 插件文案，经 registry 聚合挂载到
 * plugins.content-moderation.* 命名空间（zh/en 键集必须一致）。
 *
 * 注意：风控中心控制台（RiskControlView）是原位收编的核心路由，其文案
 * 仍在 admin.riskControl.* 命名空间；本文件只承载插件自身贡献的界面
 * （当前为设置面板）的文案。
 */
export const contentModerationMessages = {
  zh: {
    settings: {
      enabled: '启用风控中心',
      enabledHint: '关闭后管理员侧边栏入口隐藏，网关内容审计不会执行。',
      cyberSessionBlock: 'cyber 会话自动屏蔽',
      cyberSessionBlockHint:
        '开启后，被上游网络安全策略（cyber_policy）拦截的会话将在 TTL 内被本地屏蔽，不再发往上游。仅屏蔽该会话，不影响同 Key 其他会话。',
      cyberSessionBlockTTL: '屏蔽时长（秒）',
      consoleLink: '前往风控中心配置内容审计',
      loadFailed: '加载设置失败',
      saved: '设置已保存',
      saveFailed: '设置保存失败'
    }
  },
  en: {
    settings: {
      enabled: 'Enable Risk Control',
      enabledHint: 'When off, the admin sidebar entry is hidden and gateway moderation is skipped.',
      cyberSessionBlock: 'Cyber session auto-block',
      cyberSessionBlockHint:
        'When enabled, sessions hit by upstream cyber_policy are blocked locally for the TTL and no longer forwarded. Only the offending session is blocked; other sessions on the same key are unaffected.',
      cyberSessionBlockTTL: 'Block TTL (seconds)',
      consoleLink: 'Configure content moderation in Risk Control',
      loadFailed: 'Failed to load settings',
      saved: 'Settings saved',
      saveFailed: 'Failed to save settings'
    }
  }
}

export default contentModerationMessages
