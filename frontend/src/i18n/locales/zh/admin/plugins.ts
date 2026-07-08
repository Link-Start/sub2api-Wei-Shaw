export default {
  plugins: {
    title: '插件管理',
    description: '查看已注册插件的运行状态，免重启启用或停用插件',
    empty: '暂无已注册插件',
    columns: {
      id: '插件 ID',
      enabled: '启用',
      state: '状态',
      error: '错误信息',
      startedAt: '启动时间'
    },
    stateLabels: {
      disabled: '未启用',
      running: '运行中',
      failed: '失败',
      stopped: '已停止'
    },
    enableSuccess: '插件已启用',
    disableSuccess: '插件已停用',
    toggleFailed: '插件启停操作失败',
    loadFailed: '加载插件列表失败'
  }
}
