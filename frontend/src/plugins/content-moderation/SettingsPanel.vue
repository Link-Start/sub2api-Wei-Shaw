<template>
  <div v-if="loading" class="py-8 text-center text-sm text-gray-500 dark:text-dark-400">
    {{ t('common.loading') }}
  </div>
  <div v-else class="space-y-5">
    <div class="flex items-center justify-between gap-4">
      <div>
        <label class="text-sm font-medium text-gray-700 dark:text-gray-300">
          {{ t('plugins.content-moderation.settings.enabled') }}
        </label>
        <p class="mt-0.5 text-xs text-gray-500 dark:text-gray-400">
          {{ t('plugins.content-moderation.settings.enabledHint') }}
        </p>
      </div>
      <Toggle v-model="form.risk_control_enabled" data-test="moderation-settings-enabled" />
    </div>

    <div class="flex items-center justify-between gap-4">
      <div>
        <label class="text-sm font-medium text-gray-700 dark:text-gray-300">
          {{ t('plugins.content-moderation.settings.cyberSessionBlock') }}
        </label>
        <p class="mt-0.5 text-xs text-gray-500 dark:text-gray-400">
          {{ t('plugins.content-moderation.settings.cyberSessionBlockHint') }}
        </p>
      </div>
      <Toggle
        v-model="form.cyber_session_block_enabled"
        data-test="moderation-settings-cyber-block"
      />
    </div>

    <div v-if="form.cyber_session_block_enabled">
      <label class="input-label">
        {{ t('plugins.content-moderation.settings.cyberSessionBlockTTL') }}
        <span class="text-red-500">*</span>
      </label>
      <input
        v-model.number="form.cyber_session_block_ttl_seconds"
        type="number"
        min="1"
        class="input"
        data-test="moderation-settings-cyber-ttl"
      />
    </div>

    <div
      class="flex items-center justify-between gap-3 border-t border-gray-100 pt-4 dark:border-dark-700"
    >
      <router-link
        to="/admin/risk-control"
        class="inline-flex items-center gap-1 text-sm text-primary-600 hover:underline dark:text-primary-400"
        data-test="moderation-settings-console-link"
      >
        {{ t('plugins.content-moderation.settings.consoleLink') }}
        <span aria-hidden="true">→</span>
      </router-link>
      <button
        class="btn btn-primary"
        :disabled="saving"
        data-test="moderation-settings-save"
        @click="save"
      >
        {{ t('common.save') }}
      </button>
    </div>
  </div>
</template>

<script setup lang="ts">
import { onMounted, reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useAppStore } from '@/stores/app'
import { getSettings, updateSettings } from '@/api/admin/settings'

import Toggle from '@/components/common/Toggle.vue'

const { t } = useI18n()
const appStore = useAppStore()

const loading = ref(true)
const saving = ref(false)

const form = reactive({
  risk_control_enabled: false,
  cyber_session_block_enabled: false,
  cyber_session_block_ttl_seconds: 3600
})

onMounted(async () => {
  try {
    const settings = await getSettings()
    form.risk_control_enabled = settings.risk_control_enabled
    form.cyber_session_block_enabled = settings.cyber_session_block_enabled
    form.cyber_session_block_ttl_seconds = settings.cyber_session_block_ttl_seconds || 3600
  } catch (error: unknown) {
    console.error('Failed to load moderation settings:', error)
    appStore.showError(t('plugins.content-moderation.settings.loadFailed'))
  } finally {
    loading.value = false
  }
})

async function save() {
  if (saving.value) return
  saving.value = true
  try {
    await updateSettings({
      risk_control_enabled: form.risk_control_enabled,
      cyber_session_block_enabled: form.cyber_session_block_enabled,
      cyber_session_block_ttl_seconds: Number(form.cyber_session_block_ttl_seconds) || 3600
    })
    appStore.showSuccess(t('plugins.content-moderation.settings.saved'))
  } catch (error: unknown) {
    console.error('Failed to save moderation settings:', error)
    appStore.showError(t('plugins.content-moderation.settings.saveFailed'))
  } finally {
    saving.value = false
  }
}
</script>
