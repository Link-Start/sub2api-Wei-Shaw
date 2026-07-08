<template>
  <AppLayout>
    <div class="space-y-6">
      <!-- 工具栏：页面说明 + 操作 -->
      <div class="flex flex-wrap items-center justify-between gap-3">
        <p class="text-sm text-gray-500 dark:text-dark-400">
          {{ t('admin.plugins.description') }}
        </p>
        <div class="flex items-center gap-2">
          <button
            @click="loadPlugins"
            :disabled="loading"
            class="btn btn-secondary"
            :title="t('common.refresh')"
          >
            <Icon name="refresh" size="md" :class="loading ? 'animate-spin' : ''" />
          </button>
          <button class="btn btn-primary" data-test="plugin-upload-btn" @click="openUploadDialog">
            <Icon name="upload" size="sm" />
            {{ t('admin.plugins.install') }}
          </button>
        </div>
      </div>

      <!-- 首屏加载骨架 -->
      <div
        v-if="loading && plugins.length === 0"
        class="grid gap-4 sm:grid-cols-2 xl:grid-cols-3"
      >
        <div v-for="i in 3" :key="i" class="card animate-pulse p-5">
          <div class="flex items-start gap-3">
            <div class="h-11 w-11 rounded-xl bg-gray-200 dark:bg-dark-700" />
            <div class="flex-1 space-y-2 py-1">
              <div class="h-4 w-1/2 rounded bg-gray-200 dark:bg-dark-700" />
              <div class="h-3 w-1/3 rounded bg-gray-100 dark:bg-dark-700/60" />
            </div>
          </div>
          <div class="mt-4 space-y-2">
            <div class="h-3 w-full rounded bg-gray-100 dark:bg-dark-700/60" />
            <div class="h-3 w-2/3 rounded bg-gray-100 dark:bg-dark-700/60" />
          </div>
        </div>
      </div>

      <!-- 空态 -->
      <div v-else-if="plugins.length === 0" class="card p-12">
        <EmptyState :title="t('empty.noData')" :description="t('admin.plugins.empty')" />
      </div>

      <!-- 插件卡片网格 -->
      <div v-else class="grid gap-4 sm:grid-cols-2 xl:grid-cols-3">
        <div
          v-for="row in plugins"
          :key="row.id"
          class="card card-hover flex flex-col p-5"
          :data-test="`plugin-card-${row.id}`"
        >
          <!-- 头部：图标 + 名称/ID + 启停开关 -->
          <div class="flex items-start gap-3">
            <div
              :class="[
                'flex h-11 w-11 shrink-0 items-center justify-center rounded-xl bg-gradient-to-br text-white shadow-sm',
                visualOf(row).accent
              ]"
            >
              <Icon :name="visualOf(row).icon" size="md" />
            </div>
            <div class="min-w-0 flex-1">
              <h3 class="truncate text-base font-semibold text-gray-900 dark:text-white">
                {{ displayName(row) }}
              </h3>
              <p class="mt-0.5 flex items-center gap-1.5 text-xs text-gray-400 dark:text-dark-500">
                <span class="truncate font-mono">{{ row.id }}</span>
                <span
                  v-if="row.version"
                  class="shrink-0 rounded bg-gray-100 px-1.5 py-0.5 font-mono text-[10px] text-gray-500 dark:bg-dark-700 dark:text-dark-300"
                >
                  v{{ row.version }}
                </span>
              </p>
            </div>
            <Toggle
              :model-value="row.enabled"
              :data-test="`plugin-toggle-${row.id}`"
              @update:model-value="(next: boolean) => handleToggle(row, next)"
            />
          </div>

          <!-- 描述（两行截断，撑起等高卡片） -->
          <p
            class="mt-3 line-clamp-2 min-h-[2.5rem] flex-1 text-sm leading-5 text-gray-500 dark:text-dark-400"
            :title="displayDescription(row)"
          >
            {{ displayDescription(row) || t('admin.plugins.noDescription') }}
          </p>

          <!-- 错误信息条 -->
          <div
            v-if="row.error"
            class="mt-3 flex items-start gap-1.5 rounded-lg bg-red-50 px-2.5 py-2 dark:bg-red-900/20"
            :title="row.error"
          >
            <Icon name="exclamationTriangle" size="sm" class="mt-0.5 shrink-0 text-red-500" />
            <span class="line-clamp-2 text-xs leading-4 text-red-600 dark:text-red-400">
              {{ row.error }}
            </span>
          </div>

          <!-- 底部：层级/状态徽章 + 启动时间 + 外部插件操作 -->
          <div
            class="mt-4 space-y-3 border-t border-gray-100 pt-3 dark:border-dark-700"
          >
            <div class="flex flex-wrap items-center gap-2">
              <span
                :class="['badge', isExternal(row) ? 'badge-purple' : 'badge-gray']"
                :data-test="`plugin-tier-${row.id}`"
              >
                {{ t(`admin.plugins.tierLabels.${isExternal(row) ? 'external' : 'builtin'}`) }}
              </span>
              <span :class="['badge', stateBadgeClass(row.state)]">
                <span :class="['h-1.5 w-1.5 rounded-full', stateDotClass(row.state)]" />
                {{ stateLabel(row.state) }}
              </span>
              <span
                v-if="row.started_at"
                class="ml-auto flex items-center gap-1 text-xs text-gray-400 dark:text-dark-500"
                :title="t('admin.plugins.startedAt')"
              >
                <Icon name="clock" size="xs" />
                {{ formatDateTime(row.started_at) }}
              </span>
            </div>
            <div v-if="hasSettings(row) || isExternal(row)" class="flex gap-2">
              <button
                v-if="hasSettings(row)"
                class="btn btn-secondary btn-sm flex-1"
                :data-test="`plugin-settings-${row.id}`"
                @click="openSettings(row)"
              >
                <Icon name="cog" size="sm" />
                {{ t('admin.plugins.settings') }}
              </button>
              <button
                v-if="isExternal(row)"
                class="btn btn-secondary btn-sm flex-1"
                :data-test="`plugin-config-${row.id}`"
                @click="openConfig(row)"
              >
                <Icon name="cog" size="sm" />
                {{ t('admin.plugins.config') }}
              </button>
              <button
                v-if="isExternal(row)"
                class="btn btn-danger btn-sm flex-1"
                :data-test="`plugin-uninstall-${row.id}`"
                @click="askUninstall(row)"
              >
                <Icon name="trash" size="sm" />
                {{ t('admin.plugins.uninstall') }}
              </button>
            </div>
          </div>
        </div>
      </div>
    </div>

    <!-- 上传安装弹窗（拖拽/点选 + 要求与安全提示 + 进度） -->
    <BaseDialog
      :show="showUploadDialog"
      :title="t('admin.plugins.uploadDialogTitle')"
      @close="closeUploadDialog"
    >
      <div class="space-y-4">
        <input
          ref="uploadInput"
          type="file"
          accept=".zip,application/zip"
          class="hidden"
          data-test="plugin-upload-input"
          @change="handleUploadChange"
        />
        <div
          data-test="plugin-upload-dropzone"
          :class="[
            'flex cursor-pointer flex-col items-center justify-center gap-2 rounded-2xl border-2 border-dashed px-6 py-10 text-center transition-colors duration-200',
            dragActive
              ? 'border-primary-500 bg-primary-50/60 dark:border-primary-500 dark:bg-primary-900/10'
              : 'border-gray-300 hover:border-primary-400 hover:bg-gray-50 dark:border-dark-600 dark:hover:border-primary-500 dark:hover:bg-dark-800/50'
          ]"
          @click="triggerUpload"
          @dragenter.prevent="dragActive = true"
          @dragover.prevent="dragActive = true"
          @dragleave.prevent="dragActive = false"
          @drop.prevent="handleDrop"
        >
          <div
            class="flex h-12 w-12 items-center justify-center rounded-full bg-primary-100 text-primary-600 dark:bg-primary-900/30 dark:text-primary-400"
          >
            <Icon name="upload" size="lg" />
          </div>
          <p class="text-sm font-medium text-gray-700 dark:text-gray-200">
            {{ t('admin.plugins.uploadDropHint') }}
          </p>
          <p class="text-xs text-gray-400 dark:text-dark-500">
            {{ t('admin.plugins.uploadRequirements') }}
          </p>
        </div>

        <!-- 已选文件 -->
        <div
          v-if="pendingFile"
          data-test="plugin-upload-file"
          class="flex items-center gap-3 rounded-xl border border-gray-200 bg-gray-50 px-3 py-2.5 dark:border-dark-600 dark:bg-dark-800"
        >
          <Icon name="document" size="md" class="shrink-0 text-primary-500" />
          <div class="min-w-0 flex-1">
            <p class="truncate text-sm font-medium text-gray-800 dark:text-gray-100">
              {{ pendingFile.name }}
            </p>
            <p class="text-xs text-gray-400 dark:text-dark-500">
              {{ formatBytes(pendingFile.size) }}
            </p>
          </div>
          <button
            v-if="!uploading"
            data-test="plugin-upload-remove"
            class="rounded-lg p-1.5 text-gray-400 transition-colors hover:bg-gray-200 hover:text-gray-600 dark:hover:bg-dark-700 dark:hover:text-dark-200"
            :title="t('admin.plugins.removeFile')"
            @click="pendingFile = null"
          >
            <Icon name="x" size="sm" />
          </button>
        </div>

        <!-- 上传进度 -->
        <div v-if="uploading" class="space-y-1.5">
          <div class="flex justify-between text-xs text-gray-500 dark:text-dark-400">
            <span>{{ t('admin.plugins.uploading', { percent: uploadPercent }) }}</span>
          </div>
          <div class="h-1.5 overflow-hidden rounded-full bg-gray-100 dark:bg-dark-700">
            <div
              class="h-full rounded-full bg-gradient-to-r from-primary-500 to-primary-600 transition-all duration-200"
              :style="{ width: `${uploadPercent}%` }"
            />
          </div>
        </div>

        <!-- 安全提示与说明 -->
        <div class="flex gap-1.5 rounded-xl bg-amber-50 px-3 py-2.5 dark:bg-amber-900/15">
          <Icon
            name="exclamationTriangle"
            size="sm"
            class="mt-0.5 shrink-0 text-amber-500 dark:text-amber-400"
          />
          <p class="text-xs leading-5 text-amber-700 dark:text-amber-400">
            {{ t('admin.plugins.uploadSecurityNote') }}
          </p>
        </div>
        <p class="flex gap-1.5 text-xs leading-5 text-gray-500 dark:text-dark-400">
          <Icon name="infoCircle" size="sm" class="mt-0.5 shrink-0" />
          {{ t('admin.plugins.uploadDefaultDisabled') }}
        </p>
      </div>
      <template #footer>
        <button class="btn btn-secondary" :disabled="uploading" @click="closeUploadDialog">
          {{ t('common.cancel') }}
        </button>
        <button
          class="btn btn-primary"
          :disabled="!pendingFile || uploading"
          data-test="plugin-upload-confirm"
          @click="confirmUpload"
        >
          <span v-if="uploading">{{ t('admin.plugins.uploading', { percent: uploadPercent }) }}</span>
          <span v-else>{{ t('admin.plugins.uploadConfirm') }}</span>
        </button>
      </template>
    </BaseDialog>

    <!-- 插件设置面板弹窗：渲染插件声明的懒加载设置组件（自包含加载/保存） -->
    <BaseDialog
      :show="showSettingsDialog"
      :title="
        t('admin.plugins.settingsTitle', {
          name: settingsTarget ? displayName(settingsTarget) : ''
        })
      "
      @close="showSettingsDialog = false"
    >
      <component :is="settingsComponent" v-if="settingsComponent && showSettingsDialog" />
    </BaseDialog>

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
import { defineAsyncComponent, onMounted, ref, shallowRef, type Component } from 'vue'
import { useI18n } from 'vue-i18n'
import { useAppStore } from '@/stores/app'
import { useEnabledPluginsStore } from '@/pluginkit/enabled'
import { pluginSettingsLoader } from '@/pluginkit/registry'
import { adminPluginsAPI, type PluginState, type PluginStatus } from '@/api/admin/plugins'
import { formatBytes, formatDateTime } from '@/utils/format'

import AppLayout from '@/components/layout/AppLayout.vue'
import BaseDialog from '@/components/common/BaseDialog.vue'
import ConfirmDialog from '@/components/common/ConfirmDialog.vue'
import EmptyState from '@/components/common/EmptyState.vue'
import Toggle from '@/components/common/Toggle.vue'
import Icon from '@/components/icons/Icon.vue'

const { t, te } = useI18n()
const appStore = useAppStore()
const enabledPluginsStore = useEnabledPluginsStore()

const plugins = ref<PluginStatus[]>([])
const loading = ref(false)
// 启停请求进行中的插件 ID 集合，防止重复点击造成的并发启停
const toggling = ref<Set<string>>(new Set())

// tier 缺省按内建处理（内建快照不带该字段）；仅外部插件可卸载/编辑配置
function isExternal(row: PluginStatus): boolean {
  return row.tier === 'external'
}

// ==================== 展示元数据（名称/描述/图标） ====================

type PluginVisual = { icon: 'beaker' | 'shield' | 'cube'; accent: string }

// 内建插件的图标与配色按 ID 静态映射（内建插件与前端同仓发布，随插件新增维护）
const builtinVisuals: Record<string, PluginVisual> = {
  demo: { icon: 'beaker', accent: 'from-violet-500 to-purple-600' },
  'content-moderation': { icon: 'shield', accent: 'from-emerald-500 to-teal-600' }
}

function visualOf(row: PluginStatus): PluginVisual {
  const visual = builtinVisuals[row.id]
  if (visual) return visual
  return isExternal(row)
    ? { icon: 'cube', accent: 'from-sky-500 to-blue-600' }
    : { icon: 'cube', accent: 'from-primary-500 to-primary-600' }
}

// 内建插件名称/描述走 i18n（admin.plugins.meta.<id>），外部插件用 manifest 声明，
// 均缺失时回退到 ID / 空描述占位
function displayName(row: PluginStatus): string {
  const key = `admin.plugins.meta.${row.id}.name`
  if (te(key)) return t(key)
  return row.name || row.id
}

function displayDescription(row: PluginStatus): string {
  const key = `admin.plugins.meta.${row.id}.description`
  if (te(key)) return t(key)
  return row.description ?? ''
}

// ==================== 状态展示 ====================

function stateLabel(state: PluginState): string {
  return t(`admin.plugins.stateLabels.${state}`)
}

function stateBadgeClass(state: PluginState): string {
  if (state === 'running') return 'badge-success'
  if (state === 'failed') return 'badge-danger'
  return 'badge-gray'
}

function stateDotClass(state: PluginState): string {
  if (state === 'running') return 'bg-emerald-500'
  if (state === 'failed') return 'bg-red-500'
  return 'bg-gray-400 dark:bg-dark-500'
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

// ==================== 插件设置面板 ====================

const showSettingsDialog = ref(false)
const settingsTarget = ref<PluginStatus | null>(null)
const settingsComponent = shallowRef<Component | null>(null)
// 每插件缓存 async 组件包装，重复打开不重建（chunk 本身由 Vite 缓存）
const settingsComponentCache = new Map<string, Component>()

// 设置入口完全由插件描述符声明驱动（registry 查找），不对插件 ID 特判
function hasSettings(row: PluginStatus): boolean {
  return pluginSettingsLoader(row.id) !== null
}

function openSettings(row: PluginStatus) {
  const loader = pluginSettingsLoader(row.id)
  if (!loader) return
  let component = settingsComponentCache.get(row.id)
  if (!component) {
    component = defineAsyncComponent(loader)
    settingsComponentCache.set(row.id, component)
  }
  settingsTarget.value = row
  settingsComponent.value = component
  showSettingsDialog.value = true
}

// ==================== 上传安装（弹窗：拖拽/点选 → 确认上传） ====================

const showUploadDialog = ref(false)
const uploadInput = ref<HTMLInputElement | null>(null)
const pendingFile = ref<File | null>(null)
const dragActive = ref(false)
const uploading = ref(false)
const uploadPercent = ref(0)

function openUploadDialog() {
  pendingFile.value = null
  dragActive.value = false
  uploadPercent.value = 0
  showUploadDialog.value = true
}

function closeUploadDialog() {
  if (uploading.value) return
  showUploadDialog.value = false
  pendingFile.value = null
  dragActive.value = false
}

function triggerUpload() {
  uploadInput.value?.click()
}

// 收下拖拽/点选的文件：仅接受 .zip，非法类型就地提示
function takeFile(file: File | null | undefined) {
  if (!file) return
  if (!file.name.toLowerCase().endsWith('.zip')) {
    appStore.showError(t('admin.plugins.uploadInvalidType'))
    return
  }
  pendingFile.value = file
}

function handleUploadChange(event: Event) {
  const input = event.target as HTMLInputElement
  const file = input.files?.[0]
  // 立即清空选择：同一文件二次选择（如修复后重新打包同名 zip）仍触发 change
  input.value = ''
  takeFile(file)
}

function handleDrop(event: DragEvent) {
  dragActive.value = false
  if (uploading.value) return
  takeFile(event.dataTransfer?.files?.[0])
}

async function confirmUpload() {
  const file = pendingFile.value
  if (!file || uploading.value) return

  uploading.value = true
  uploadPercent.value = 0
  try {
    await adminPluginsAPI.uploadPlugin(file, (percent) => {
      uploadPercent.value = percent
    })
    appStore.showSuccess(t('admin.plugins.uploadSuccess'))
    showUploadDialog.value = false
    pendingFile.value = null
    await loadPlugins()
  } catch (error: unknown) {
    console.error('Failed to upload plugin:', error)
    // 失败保留弹窗与已选文件，展示后端拒绝原因后可直接重试
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
