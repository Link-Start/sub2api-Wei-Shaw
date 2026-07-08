export default {
  plugins: {
    title: 'Plugin Management',
    description: 'Inspect registered plugin runtime status and enable or disable plugins without restart',
    empty: 'No registered plugins',
    columns: {
      id: 'Plugin ID',
      enabled: 'Enabled',
      state: 'State',
      error: 'Error',
      startedAt: 'Started At'
    },
    stateLabels: {
      disabled: 'Disabled',
      running: 'Running',
      failed: 'Failed',
      stopped: 'Stopped'
    },
    enableSuccess: 'Plugin enabled',
    disableSuccess: 'Plugin disabled',
    toggleFailed: 'Failed to toggle plugin',
    loadFailed: 'Failed to load plugins'
  }
}
