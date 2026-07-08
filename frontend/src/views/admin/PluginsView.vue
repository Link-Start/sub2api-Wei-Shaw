<template>
  <AppLayout>
    <TablePageLayout>
      <template #filters>
        <div class="flex flex-wrap items-center justify-end gap-2">
          <button
            @click="loadPlugins"
            :disabled="loading"
            class="btn btn-secondary"
            :title="t('common.refresh')"
          >
            <Icon name="refresh" size="md" :class="loading ? 'animate-spin' : ''" />
          </button>
        </div>
      </template>

      <template #table>
        <DataTable :columns="columns" :data="plugins" :loading="loading" row-key="id">
          <template #cell-id="{ value }">
            <span class="font-medium text-gray-900 dark:text-white">{{ value }}</span>
          </template>

          <template #cell-enabled="{ row }">
            <Toggle
              :model-value="row.enabled"
              :data-test="`plugin-toggle-${row.id}`"
              @update:model-value="(next: boolean) => handleToggle(row, next)"
            />
          </template>

          <template #cell-state="{ value }">
            <span :class="['badge', stateBadgeClass(value)]">
              {{ stateLabel(value) }}
            </span>
          </template>

          <template #cell-error="{ row }">
            <span
              v-if="row.error"
              class="text-sm text-red-600 dark:text-red-400"
              :title="row.error"
            >
              {{ row.error }}
            </span>
            <span v-else class="text-sm text-gray-400 dark:text-dark-500">-</span>
          </template>

          <template #cell-started_at="{ row }">
            <span class="text-sm text-gray-500 dark:text-dark-400">
              {{ row.started_at ? formatDateTime(row.started_at) : '-' }}
            </span>
          </template>

          <template #empty>
            <EmptyState :title="t('empty.noData')" :description="t('admin.plugins.empty')" />
          </template>
        </DataTable>
      </template>
    </TablePageLayout>
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useAppStore } from '@/stores/app'
import { useEnabledPluginsStore } from '@/pluginkit/enabled'
import { adminPluginsAPI, type PluginState, type PluginStatus } from '@/api/admin/plugins'
import { formatDateTime } from '@/utils/format'
import type { Column } from '@/components/common/types'

import AppLayout from '@/components/layout/AppLayout.vue'
import TablePageLayout from '@/components/layout/TablePageLayout.vue'
import DataTable from '@/components/common/DataTable.vue'
import EmptyState from '@/components/common/EmptyState.vue'
import Toggle from '@/components/common/Toggle.vue'
import Icon from '@/components/icons/Icon.vue'

const { t } = useI18n()
const appStore = useAppStore()
const enabledPluginsStore = useEnabledPluginsStore()

const plugins = ref<PluginStatus[]>([])
const loading = ref(false)
// 启停请求进行中的插件 ID 集合，防止重复点击造成的并发启停
const toggling = ref<Set<string>>(new Set())

const columns = computed<Column[]>(() => [
  { key: 'id', label: t('admin.plugins.columns.id') },
  { key: 'enabled', label: t('admin.plugins.columns.enabled') },
  { key: 'state', label: t('admin.plugins.columns.state') },
  { key: 'error', label: t('admin.plugins.columns.error') },
  { key: 'started_at', label: t('admin.plugins.columns.startedAt') }
])

function stateLabel(state: PluginState): string {
  return t(`admin.plugins.stateLabels.${state}`)
}

function stateBadgeClass(state: PluginState): string {
  if (state === 'running') return 'badge-success'
  if (state === 'failed') return 'badge-danger'
  return 'badge-gray'
}

async function loadPlugins() {
  loading.value = true
  try {
    plugins.value = await adminPluginsAPI.listPlugins()
  } catch (error: unknown) {
    console.error('Failed to load plugins:', error)
    appStore.showError(t('admin.plugins.loadFailed'))
  } finally {
    loading.value = false
  }
}

async function handleToggle(row: PluginStatus, next: boolean) {
  if (toggling.value.has(row.id)) return
  toggling.value.add(row.id)
  try {
    if (next) {
      await adminPluginsAPI.enablePlugin(row.id)
      appStore.showSuccess(t('admin.plugins.enableSuccess'))
    } else {
      await adminPluginsAPI.disablePlugin(row.id)
      appStore.showSuccess(t('admin.plugins.disableSuccess'))
    }
  } catch (error: unknown) {
    console.error('Failed to toggle plugin:', error)
    appStore.showError(t('admin.plugins.toggleFailed'))
  } finally {
    toggling.value.delete(row.id)
  }
  // 无论成败都回读快照，并同步 enabled 门控清单（侧边栏/路由守卫即时生效）
  await Promise.all([loadPlugins(), enabledPluginsStore.refresh()])
}

onMounted(() => {
  void loadPlugins()
})
</script>
