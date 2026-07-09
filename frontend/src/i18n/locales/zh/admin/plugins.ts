export default {
  plugins: {
    title: '插件管理',
    description: '查看已注册插件的运行状态，免重启启用或停用插件',
    empty: '暂无已注册插件',
    noDescription: '暂无描述',
    startedAt: '启动时间',
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
    // 内建插件的展示元数据（外部插件的名称/描述来自其 manifest）
    meta: {
      demo: {
        name: '示例插件',
        description: '内建插件体系的演示插件：验证配置注入、生命周期启停与插件 API 分发。'
      },
      'content-moderation': {
        name: '内容审计',
        description: '内容安全功能域：请求内容审查、风险自动处置与风控中心后台。'
      }
    },
    settings: '设置',
    settingsTitle: '插件设置：{name}',
    enableSuccess: '插件已启用',
    disableSuccess: '插件已停用',
    toggleFailed: '插件启停操作失败',
    loadFailed: '加载插件列表失败',
    install: '安装插件',
    uploadDialogTitle: '安装插件',
    uploadDropHint: '拖拽插件包到此处，或点击选择文件',
    uploadRequirements: '仅支持 .zip 插件包（需包含 manifest.json），默认大小上限 64MB',
    uploadSecurityNote:
      '请仅安装可信来源的插件包：插件后端以独立进程运行，并获得其清单声明的能力；若插件带有前端脚本，启用后将在所有登录用户的浏览器中无沙箱执行，可以该用户的身份访问页面与 API。',
    uploadDefaultDisabled: '安装后插件默认处于停用状态，需在列表中手动启用。',
    uploadInvalidType: '仅支持 .zip 格式的插件包',
    uploadConfirm: '上传并安装',
    removeFile: '移除文件',
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
