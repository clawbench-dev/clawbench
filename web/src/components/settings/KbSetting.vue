<template>
  <!-- Card BODY, not a page: the 知识库索引 card is opened by a placeholder item
       in settingsFieldMap (see PLACEHOLDER_ITEMS in SettingsCategory), and the
       kb.* configuration rows live in the cards below it. -->
  <div class="kb-setting">
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
    <!-- Two-phase index progress. Sits between the counters and the actions:
         the counters say what exists, this says how far the indexer has got. -->
    <KbIndexProgress
      v-if="status"
      class="kb-progress"
      :stats="status.stats"
      :vector-enabled="status.vector_enabled"
    />
    <div class="kb-actions">
      <button class="fbtn" type="button" :disabled="loading" @click="load()">
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
    <p class="kb-note">{{ t('settings.items.kbManageDesc') }}</p>
  </div>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { apiGet, apiPost } from '@/utils/api'
import { useToast } from '@/composables/useToast'
import { useDialog } from '@/composables/useDialog'
import KbIndexProgress from '@/components/kb/KbIndexProgress.vue'
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

async function load(silent = false): Promise<void> {
  if (!silent) loading.value = true
  statusError.value = ''
  try {
    // Installation-wide on purpose: this page reports the whole knowledge base,
    // unlike the panel which is scoped to the current project.
    status.value = await apiGet<KbStatus>('/api/kb/status')
  } catch (err) {
    if (!silent) {
      status.value = null
      statusError.value = err instanceof Error ? err.message : String(err)
    }
  } finally {
    if (!silent) loading.value = false
    syncPolling()
  }
}

/**
 * Keep the counters and the progress bar moving while the indexer works.
 *
 * Polls silently: reusing `loading` would swap the whole card for a skeleton
 * every few seconds (and spin the refresh button). Stops by itself once the
 * queue and the embedding backlog are both empty.
 */
const indexActive = computed(() => {
  const s = status.value?.stats
  if (!s) return false
  if (s.pending_docs > 0) return true
  return !!status.value?.vector_enabled && s.chunks > s.embedded_chunks
})

let pollTimer: ReturnType<typeof setInterval> | null = null

function syncPolling(): void {
  if (indexActive.value) {
    if (pollTimer == null) {
      pollTimer = setInterval(() => void load(true), 3000)
    }
  } else if (pollTimer != null) {
    clearInterval(pollTimer)
    pollTimer = null
  }
}

onBeforeUnmount(() => {
  if (pollTimer != null) {
    clearInterval(pollTimer)
    pollTimer = null
  }
})

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

onMounted(() => {
  void load()
})
</script>

<style scoped>
.kb-setting {
  /* Card body: the surface and the title come from SettingsCard, so this only
     lays out its own contents. */
  background: transparent;
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

/* The component brings its own rows; only the card's horizontal rhythm is
   added here (it has no idea which container it lands in). */
.kb-progress {
  padding: 0 var(--space-7) var(--space-5);
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
