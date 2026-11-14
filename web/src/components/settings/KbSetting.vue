<template>
  <div class="kb-setting">
    <SettingsCard :title="t('settings.items.kbStatusSection')">
      <div v-if="status" class="kb-stats">
        <div class="kb-stat">
          <span class="kb-stat__label">{{ t('settings.items.kbCollections') }}</span>
          <strong class="kb-stat__value">{{ status.stats.collections }}</strong>
        </div>
        <div class="kb-stat">
          <span class="kb-stat__label">{{ t('settings.items.kbDocuments') }}</span>
          <strong class="kb-stat__value">{{ status.stats.documents }}</strong>
        </div>
        <div class="kb-stat">
          <span class="kb-stat__label">{{ t('settings.items.kbReadyDocs') }}</span>
          <strong class="kb-stat__value">{{ status.stats.ready_docs }}</strong>
        </div>
        <div class="kb-stat">
          <span class="kb-stat__label">{{ t('settings.items.kbFailedDocs') }}</span>
          <strong class="kb-stat__value" :class="{ 'is-bad': status.stats.failed_docs > 0 }">
            {{ status.stats.failed_docs }}
          </strong>
        </div>
        <div class="kb-stat">
          <span class="kb-stat__label">{{ t('settings.items.kbPendingDocs') }}</span>
          <strong class="kb-stat__value">{{ status.stats.pending_docs }}</strong>
        </div>
        <div class="kb-stat">
          <span class="kb-stat__label">{{ t('settings.items.kbChunks') }}</span>
          <strong class="kb-stat__value">{{ status.stats.chunks }}</strong>
        </div>
        <div class="kb-stat">
          <span class="kb-stat__label">{{ t('settings.items.kbEmbeddedChunks') }}</span>
          <strong class="kb-stat__value">{{ status.stats.embedded_chunks }}</strong>
        </div>
        <div class="kb-stat">
          <span class="kb-stat__label">{{ t('settings.items.kbStoredBytes') }}</span>
          <strong class="kb-stat__value">{{ formatBytes(status.stats.stored_bytes) }}</strong>
        </div>
      </div>
      <div v-else class="kb-loading">
        {{ statusError || t('settings.items.kbLoading') }}
      </div>
      <div class="kb-actions">
        <button class="fbtn" type="button" :disabled="loading" @click="load">
          {{ t('settings.items.kbRefresh') }}
        </button>
        <button
          class="fbtn fbtn-primary"
          type="button"
          :disabled="reindexing || !status?.available"
          @click="reindex"
        >
          {{ reindexing ? t('settings.items.kbReindexing') : t('settings.items.kbReindex') }}
        </button>
      </div>
    </SettingsCard>

    <SettingsCard :title="t('settings.items.kbManageSection')">
      <p class="kb-note">{{ t('settings.items.kbManageDesc') }}</p>
    </SettingsCard>
  </div>
</template>

<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { apiGet, apiPost } from '@/utils/api'
import { useToast } from '@/composables/useToast'
import { useDialog } from '@/composables/useDialog'
import SettingsCard from './SettingsCard.vue'
import '@/assets/modal-footer-btn.css'

interface KbStats {
  collections: number
  documents: number
  ready_docs: number
  failed_docs: number
  pending_docs: number
  chunks: number
  embedded_chunks: number
  stored_bytes: number
}

interface KbStatus {
  available: boolean
  vector_enabled: boolean
  stats: KbStats
}

const { t } = useI18n()
const toast = useToast()
const dialog = useDialog()

const status = ref<KbStatus | null>(null)
const statusError = ref('')
const loading = ref(false)
const reindexing = ref(false)

function formatBytes(bytes: number): string {
  if (!bytes) return '0 B'
  const units = ['B', 'KB', 'MB', 'GB']
  let value = bytes
  let unit = 0
  while (value >= 1024 && unit < units.length - 1) {
    value /= 1024
    unit++
  }
  return `${value.toFixed(unit === 0 ? 0 : 1)} ${units[unit]}`
}

async function load(): Promise<void> {
  loading.value = true
  statusError.value = ''
  try {
    status.value = await apiGet<KbStatus>('/api/kb/status')
  } catch (err) {
    status.value = null
    statusError.value = err instanceof Error ? err.message : String(err)
  } finally {
    loading.value = false
  }
}

async function reindex(): Promise<void> {
  const ok = await dialog.confirm(t('settings.items.kbReindexConfirm'), {
    title: t('settings.items.kbReindex'),
    confirmText: t('settings.items.kbReindex'),
    dangerous: true,
  })
  if (!ok) return
  reindexing.value = true
  try {
    await apiPost('/api/kb/reindex', {})
    toast.show(t('settings.items.kbReindexDone'), { icon: 'success', type: 'success' })
    await load()
  } catch {
    toast.show(t('settings.saveFailed'), { icon: 'warning', type: 'error' })
  } finally {
    reindexing.value = false
  }
}

onMounted(load)
</script>

<style scoped>
.kb-setting {
  padding: var(--space-4);
  background: var(--bg-secondary);
  min-height: 100%;
}

.kb-setting > :deep(.settings-card:last-child) {
  margin-bottom: 0;
}

.kb-stats {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(calc(130 * var(--dp)), 1fr));
  gap: var(--space-3);
  padding: var(--space-6) var(--space-7);
}

.kb-stat {
  display: flex;
  flex-direction: column;
  gap: var(--space-1);
  padding: var(--space-3) var(--space-4);
  border-radius: var(--radius-sm);
  background: var(--bg-tertiary);
}

.kb-stat__label {
  font-size: var(--font-size-sm);
  color: var(--text-muted);
}

.kb-stat__value {
  font-size: var(--font-size-lg);
  color: var(--text-primary);
}

.kb-stat__value.is-bad {
  color: var(--color-red);
}

.kb-loading {
  padding: var(--space-6) var(--space-7);
  font-size: var(--font-size-sm);
  color: var(--text-muted);
}

.kb-actions {
  display: flex;
  flex-wrap: wrap;
  gap: var(--space-4);
  padding: 0 var(--space-7) var(--space-6);
}

.kb-note {
  margin: 0;
  padding: var(--space-4) var(--space-7) var(--space-6);
  font-size: var(--font-size-sm);
  line-height: 1.5;
  color: var(--text-muted);
}
</style>
