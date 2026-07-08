export default {
  plugins: {
    title: '插件管理',
    description: '查看已注册插件的运行状态，免重启启用或停用插件',
    empty: '暂无已注册插件',
    columns: {
      id: '插件 ID',
      tier: '层级',
      enabled: '启用',
      state: '状态',
      error: '错误信息',
      startedAt: '启动时间',
      actions: '操作'
    },
    stateLabels: {
      disabled: '未启用',
      running: '运行中',
      failed: '失败',
      stopped: '已停止'
    },
    tierLabels: {
      builtin: '内建',
      external: '外部'
    },
    enableSuccess: '插件已启用',
    disableSuccess: '插件已停用',
    toggleFailed: '插件启停操作失败',
    loadFailed: '加载插件列表失败',
    upload: '上传插件',
    uploading: '上传中 {percent}%',
    uploadSuccess: '插件包已安装',
    uploadFailed: '插件包上传失败',
    uninstall: '卸载',
    uninstallConfirmTitle: '卸载插件',
    uninstallConfirmMessage: '确定要卸载插件 {id} 吗？插件文件与安装记录将被删除，此操作不可撤销。',
    uninstallSuccess: '插件已卸载',
    uninstallFailed: '插件卸载失败',
    config: '配置',
    configTitle: '插件配置：{id}',
    configSchema: '配置 Schema（清单声明）',
    configLoadFailed: '加载插件配置失败',
    configInvalidJson: '配置必须是合法的 JSON',
    configSaveSuccess: '插件配置已保存，重启插件后生效',
    configSaveFailed: '插件配置保存失败'
  }
}
