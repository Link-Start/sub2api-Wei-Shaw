<template>
  <AppLayout>
    <TablePageLayout>
      <template #filters>
        <div class="flex flex-wrap items-center justify-end gap-2">
          <input
            ref="uploadInput"
            type="file"
            accept=".zip,application/zip"
            class="hidden"
            data-test="plugin-upload-input"
            @change="handleUploadChange"
          />
          <button
            @click="triggerUpload"
            :disabled="uploading"
            class="btn btn-primary"
            data-test="plugin-upload-btn"
          >
            <span v-if="uploading">{{ t('admin.plugins.uploading', { percent: uploadPercent }) }}</span>
            <span v-else>{{ t('admin.plugins.upload') }}</span>
          </button>
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

          <template #cell-tier="{ row }">
            <span
              :class="['badge', isExternal(row) ? 'badge-primary' : 'badge-gray']"
              :data-test="`plugin-tier-${row.id}`"
            >
              {{ t(`admin.plugins.tierLabels.${isExternal(row) ? 'external' : 'builtin'}`) }}
            </span>
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

          <template #cell-actions="{ row }">
            <div v-if="isExternal(row)" class="flex items-center gap-2">
              <button
                class="btn btn-secondary btn-sm"
                :data-test="`plugin-config-${row.id}`"
                @click="openConfig(row)"
              >
                {{ t('admin.plugins.config') }}
              </button>
              <button
                class="btn btn-danger btn-sm"
                :data-test="`plugin-uninstall-${row.id}`"
                @click="askUninstall(row)"
              >
                {{ t('admin.plugins.uninstall') }}
              </button>
            </div>
            <span v-else class="text-sm text-gray-400 dark:text-dark-500">-</span>
          </template>

          <template #empty>
            <EmptyState :title="t('empty.noData')" :description="t('admin.plugins.empty')" />
          </template>
        </DataTable>
      </template>
    </TablePageLayout>

    <!-- 卸载二次确认 -->
    <ConfirmDialog
      :show="showUninstallDialog"
      :title="t('admin.plugins.uninstallConfirmTitle')"
      :message="t('admin.plugins.uninstallConfirmMessage', { id: uninstallTarget?.id ?? '' })"
      :confirm-text="t('admin.plugins.uninstall')"
      :cancel-text="t('common.cancel')"
      :danger="true"
      @confirm="confirmUninstall"
      @cancel="showUninstallDialog = false"
    />

    <!-- 外部插件配置 JSON 编辑器（MVP：textarea + 合法性校验） -->
    <BaseDialog
      :show="showConfigDialog"
      :title="t('admin.plugins.configTitle', { id: configTarget?.id ?? '' })"
      @close="showConfigDialog = false"
    >
      <div v-if="configLoading" class="py-6 text-center text-sm text-gray-500 dark:text-dark-400">
        {{ t('common.loading') }}
      </div>
      <div v-else class="space-y-3">
        <textarea
          v-model="configText"
          rows="12"
          spellcheck="false"
          class="input w-full font-mono text-sm"
          data-test="plugin-config-textarea"
        />
        <p
          v-if="configError"
          class="text-sm text-red-600 dark:text-red-400"
          data-test="plugin-config-error"
        >
          {{ configError }}
        </p>
        <details v-if="configSchemaText">
          <summary class="cursor-pointer text-sm text-gray-500 dark:text-dark-400">
            {{ t('admin.plugins.configSchema') }}
          </summary>
          <pre
            class="mt-2 max-h-48 overflow-auto rounded-lg bg-gray-50 p-3 font-mono text-xs text-gray-600 dark:bg-dark-800 dark:text-dark-300"
            >{{ configSchemaText }}</pre
          >
        </details>
      </div>
      <template #footer>
        <button class="btn btn-secondary" @click="showConfigDialog = false">
          {{ t('common.cancel') }}
        </button>
        <button
          class="btn btn-primary"
          :disabled="configLoading || configSaving"
          data-test="plugin-config-save"
          @click="saveConfig"
        >
          {{ t('common.save') }}
        </button>
      </template>
    </BaseDialog>
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
import BaseDialog from '@/components/common/BaseDialog.vue'
import ConfirmDialog from '@/components/common/ConfirmDialog.vue'
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
  { key: 'tier', label: t('admin.plugins.columns.tier') },
  { key: 'enabled', label: t('admin.plugins.columns.enabled') },
  { key: 'state', label: t('admin.plugins.columns.state') },
  { key: 'error', label: t('admin.plugins.columns.error') },
  { key: 'started_at', label: t('admin.plugins.columns.startedAt') },
  { key: 'actions', label: t('admin.plugins.columns.actions') }
])

// tier 缺省按内建处理（内建快照不带该字段）；仅外部插件可卸载/编辑配置
function isExternal(row: PluginStatus): boolean {
  return row.tier === 'external'
}

function stateLabel(state: PluginState): string {
  return t(`admin.plugins.stateLabels.${state}`)
}

function stateBadgeClass(state: PluginState): string {
  if (state === 'running') return 'badge-success'
  if (state === 'failed') return 'badge-danger'
  return 'badge-gray'
}

// errorMessage 优先透出后端错误 message（如 zip 校验失败原因），无则回退兜底文案
function errorMessage(error: unknown, fallback: string): string {
  const message = (error as { message?: unknown } | null)?.message
  return typeof message === 'string' && message !== '' ? message : fallback
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

// ==================== 上传安装 ====================

const uploadInput = ref<HTMLInputElement | null>(null)
const uploading = ref(false)
const uploadPercent = ref(0)

function triggerUpload() {
  uploadInput.value?.click()
}

async function handleUploadChange(event: Event) {
  const input = event.target as HTMLInputElement
  const file = input.files?.[0]
  // 立即清空选择：同一文件二次上传（如修复后重新打包同名 zip）仍触发 change
  input.value = ''
  if (!file || uploading.value) return

  uploading.value = true
  uploadPercent.value = 0
  try {
    await adminPluginsAPI.uploadPlugin(file, (percent) => {
      uploadPercent.value = percent
    })
    appStore.showSuccess(t('admin.plugins.uploadSuccess'))
    await loadPlugins()
  } catch (error: unknown) {
    console.error('Failed to upload plugin:', error)
    appStore.showError(errorMessage(error, t('admin.plugins.uploadFailed')))
  } finally {
    uploading.value = false
  }
}

// ==================== 卸载 ====================

const showUninstallDialog = ref(false)
const uninstallTarget = ref<PluginStatus | null>(null)
const uninstalling = ref(false)

function askUninstall(row: PluginStatus) {
  uninstallTarget.value = row
  showUninstallDialog.value = true
}

async function confirmUninstall() {
  const target = uninstallTarget.value
  showUninstallDialog.value = false
  if (!target || uninstalling.value) return

  uninstalling.value = true
  try {
    await adminPluginsAPI.uninstallPlugin(target.id)
    appStore.showSuccess(t('admin.plugins.uninstallSuccess'))
  } catch (error: unknown) {
    console.error('Failed to uninstall plugin:', error)
    appStore.showError(errorMessage(error, t('admin.plugins.uninstallFailed')))
  } finally {
    uninstalling.value = false
  }
  // 卸载会顺带停用运行中的插件：与启停一致，回读快照并同步门控清单
  await Promise.all([loadPlugins(), enabledPluginsStore.refresh()])
}

// ==================== 配置编辑（JSON MVP） ====================

const showConfigDialog = ref(false)
const configTarget = ref<PluginStatus | null>(null)
const configLoading = ref(false)
const configSaving = ref(false)
const configText = ref('')
const configSchemaText = ref('')
const configError = ref('')

async function openConfig(row: PluginStatus) {
  configTarget.value = row
  configText.value = ''
  configSchemaText.value = ''
  configError.value = ''
  showConfigDialog.value = true
  configLoading.value = true
  try {
    const payload = await adminPluginsAPI.getPluginConfig(row.id)
    configText.value = payload.config == null ? '{}' : JSON.stringify(payload.config, null, 2)
    configSchemaText.value =
      payload.config_schema == null ? '' : JSON.stringify(payload.config_schema, null, 2)
  } catch (error: unknown) {
    console.error('Failed to load plugin config:', error)
    appStore.showError(errorMessage(error, t('admin.plugins.configLoadFailed')))
    showConfigDialog.value = false
  } finally {
    configLoading.value = false
  }
}

async function saveConfig() {
  const target = configTarget.value
  if (!target || configSaving.value) return

  const text = configText.value.trim()
  try {
    JSON.parse(text)
  } catch {
    configError.value = t('admin.plugins.configInvalidJson')
    return
  }
  configError.value = ''

  configSaving.value = true
  try {
    await adminPluginsAPI.putPluginConfig(target.id, text)
    appStore.showSuccess(t('admin.plugins.configSaveSuccess'))
    showConfigDialog.value = false
  } catch (error: unknown) {
    console.error('Failed to save plugin config:', error)
    // schema 校验等后端拒绝原因就地展示，编辑内容保留供修正
    configError.value = errorMessage(error, t('admin.plugins.configSaveFailed'))
  } finally {
    configSaving.value = false
  }
}

onMounted(() => {
  void loadPlugins()
})
</script>
