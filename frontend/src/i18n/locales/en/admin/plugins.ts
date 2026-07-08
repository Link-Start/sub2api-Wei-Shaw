export default {
  plugins: {
    title: 'Plugin Management',
    description: 'Inspect registered plugin runtime status and enable or disable plugins without restart',
    empty: 'No registered plugins',
    columns: {
      id: 'Plugin ID',
      tier: 'Tier',
      enabled: 'Enabled',
      state: 'State',
      error: 'Error',
      startedAt: 'Started At',
      actions: 'Actions'
    },
    stateLabels: {
      disabled: 'Disabled',
      running: 'Running',
      failed: 'Failed',
      stopped: 'Stopped'
    },
    tierLabels: {
      builtin: 'Builtin',
      external: 'External'
    },
    enableSuccess: 'Plugin enabled',
    disableSuccess: 'Plugin disabled',
    toggleFailed: 'Failed to toggle plugin',
    loadFailed: 'Failed to load plugins',
    upload: 'Upload Plugin',
    uploading: 'Uploading {percent}%',
    uploadSuccess: 'Plugin package installed',
    uploadFailed: 'Failed to upload plugin package',
    uninstall: 'Uninstall',
    uninstallConfirmTitle: 'Uninstall Plugin',
    uninstallConfirmMessage:
      'Uninstall plugin {id}? Its files and installation record will be deleted. This cannot be undone.',
    uninstallSuccess: 'Plugin uninstalled',
    uninstallFailed: 'Failed to uninstall plugin',
    config: 'Config',
    configTitle: 'Plugin Config: {id}',
    configSchema: 'Config Schema (declared by manifest)',
    configLoadFailed: 'Failed to load plugin config',
    configInvalidJson: 'Config must be valid JSON',
    configSaveSuccess: 'Plugin config saved; restart the plugin to take effect',
    configSaveFailed: 'Failed to save plugin config'
  }
}
