export default {
  plugins: {
    title: 'Plugin Management',
    description: 'Inspect registered plugin runtime status and enable or disable plugins without restart',
    empty: 'No registered plugins',
    noDescription: 'No description',
    startedAt: 'Started at',
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
    // Display metadata for builtin plugins (external plugins carry name/description in their manifest)
    meta: {
      demo: {
        name: 'Demo Plugin',
        description:
          'Showcase plugin for the builtin plugin system: config injection, lifecycle start/stop and plugin API dispatch.'
      },
      'content-moderation': {
        name: 'Content Moderation',
        description:
          'Content safety domain: request content review, automated risk enforcement and the risk-control console.'
      }
    },
    settings: 'Settings',
    settingsTitle: 'Plugin Settings: {name}',
    enableSuccess: 'Plugin enabled',
    disableSuccess: 'Plugin disabled',
    toggleFailed: 'Failed to toggle plugin',
    loadFailed: 'Failed to load plugins',
    install: 'Install Plugin',
    uploadDialogTitle: 'Install Plugin',
    uploadDropHint: 'Drag & drop a plugin package here, or click to browse',
    uploadRequirements: 'Only .zip packages (with manifest.json), default size limit 64MB',
    uploadSecurityNote:
      "Only install plugin packages from trusted sources: the plugin backend runs as a separate process with the capabilities declared in its manifest, and any bundled frontend script, once enabled, runs unsandboxed in every signed-in user's browser and can act with that user's identity.",
    uploadDefaultDisabled: 'Installed plugins start disabled; enable them from the list.',
    uploadInvalidType: 'Only .zip plugin packages are supported',
    uploadConfirm: 'Upload & Install',
    removeFile: 'Remove file',
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
