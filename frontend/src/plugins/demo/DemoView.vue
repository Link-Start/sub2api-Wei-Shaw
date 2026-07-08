<template>
  <AppLayout>
    <div class="card p-6">
      <div class="flex flex-wrap items-start justify-between gap-3">
        <div>
          <h2 class="text-lg font-semibold text-gray-900 dark:text-white">
            {{ t('plugins.demo.title') }}
          </h2>
          <p class="mt-1 text-sm text-gray-500 dark:text-dark-400">
            {{ t('plugins.demo.description') }}
          </p>
        </div>
        <button
          @click="loadHello"
          :disabled="loading"
          class="btn btn-secondary"
          :title="t('plugins.demo.refresh')"
        >
          <Icon name="refresh" size="md" :class="loading ? 'animate-spin' : ''" />
          <span class="ml-1">{{ t('plugins.demo.refresh') }}</span>
        </button>
      </div>

      <div class="mt-6 grid grid-cols-1 gap-4 sm:grid-cols-2">
        <div class="rounded-lg border border-gray-200 p-4 dark:border-dark-700">
          <div class="text-xs font-medium uppercase tracking-wider text-gray-500 dark:text-dark-400">
            {{ t('plugins.demo.greeting') }}
          </div>
          <div class="mt-1 text-base font-medium text-gray-900 dark:text-white" data-test="demo-greeting">
            {{ hello?.greeting ?? '-' }}
          </div>
        </div>
        <div class="rounded-lg border border-gray-200 p-4 dark:border-dark-700">
          <div class="text-xs font-medium uppercase tracking-wider text-gray-500 dark:text-dark-400">
            {{ t('plugins.demo.uptimeSeconds') }}
          </div>
          <div class="mt-1 text-base font-medium text-gray-900 dark:text-white" data-test="demo-uptime">
            {{ hello?.uptime_seconds ?? '-' }}
          </div>
        </div>
      </div>
    </div>
  </AppLayout>
</template>

<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useAppStore } from '@/stores/app'
import { apiClient } from '@/api/client'
import AppLayout from '@/components/layout/AppLayout.vue'
import Icon from '@/components/icons/Icon.vue'

/**
 * demo 插件 hello 接口响应（后端 internal/plugins/demo handleHello）。
 * 注意：插件 API 返回裸 JSON（非项目信封），apiClient 拦截器原样透传。
 */
interface DemoHello {
  greeting: string
  uptime_seconds: number
}

const { t } = useI18n()
const appStore = useAppStore()

const hello = ref<DemoHello | null>(null)
const loading = ref(false)

async function loadHello() {
  loading.value = true
  try {
    const { data } = await apiClient.get<DemoHello>('/admin/plugins/demo/api/hello')
    hello.value = data
  } catch (error: unknown) {
    console.error('Failed to load demo plugin hello:', error)
    hello.value = null
    appStore.showError(t('plugins.demo.loadFailed'))
  } finally {
    loading.value = false
  }
}

onMounted(() => {
  void loadHello()
})
</script>
