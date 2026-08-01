<template>
  <BottomSheet :open="drawer.effectiveOpen.value" auto :title="t('chat.messageClusters.title')" @close="drawer.close()">
    <template #header>
      <SparklesIcon :size="16" class="bs-header-icon" />
      <span class="bs-header-title">{{ t('chat.messageClusters.title') }}</span>
      <span class="bs-header-actions">
        <button v-if="loaded && clusters.length > 0 && !computing && progress.status !== 'error'" class="mc-header-btn" @click.stop="handleStartCompute" :title="t('chat.messageClusters.reanalyze')">
          <RefreshCwIcon :size="16" />
        </button>
      </span>
    </template>

    <div class="mc-content">
      <!-- Computing state -->
      <div v-if="computing" class="mc-computing">
        <div class="mc-progress-bar">
          <div class="mc-progress-fill" :style="{ width: progressPercent + '%' }" />
        </div>
        <div class="mc-computing-info">
          <span class="mc-phase-text">{{ phaseText }}</span>
          <span class="mc-progress-detail">{{ Math.round(progressPercent) }}% · {{ elapsedText }}</span>
        </div>
      </div>

      <!-- Error state -->
      <div v-else-if="progress.status === 'error'" class="mc-error">
        <span>{{ t('chat.messageClusters.error') }}</span>
        <button class="mc-btn" @click="handleStartCompute">{{ t('chat.messageClusters.retry') }}</button>
      </div>

      <!-- Loading state -->
      <div v-else-if="loading" class="mc-loading">
        <span class="mc-load-spinner"></span>
        <span>{{ t('chat.messageClusters.loading') }}</span>
      </div>

      <!-- No cache (idle) -->
      <div v-else-if="!loaded || clusters.length === 0 && progress.status === 'idle'" class="mc-empty">
        <span>{{ t('chat.messageClusters.noCache') }}</span>
        <button class="mc-btn primary" @click="handleStartCompute"><PlayIcon :size="14" />{{ t('chat.messageClusters.firstAnalyze') }}</button>
      </div>

      <!-- Has cached results -->
      <div v-else class="mc-results">
        <div class="mc-results-header">
          <span class="mc-cache-status">{{ t('chat.messageClusters.cacheStatus', { mode: localizedMode, updatedAt: updatedAt }) }}</span>
        </div>
        <div class="mc-cluster-list">
          <div v-for="cluster in clusters" :key="cluster.id" class="mc-cluster-item" @click="showVariants(cluster)">
            <span class="mc-cluster-representative">{{ cluster.representative }}</span>
            <span class="mc-cluster-count">{{ cluster.total_count }}</span>
          </div>
        </div>
      </div>
    </div>

    <!-- Variants detail dialog -->
    <ModalDialog :open="variantsDialogOpen" @close="variantsDialogOpen = false">
      <template #header>
        <ListIcon :size="16" class="modal-header-icon" />
        <span class="modal-title">{{ t('chat.messageClusters.variantsTitle') }}</span>
      </template>
      <div class="mc-variants-dialog-content">
        <div v-for="(v, i) in variantsDialogItems" :key="i" class="mc-variant-item">
          <span class="mc-variant-text">{{ v }}</span>
          <button class="mc-btn add" @click="addVariantToQuickSend(v)">
            <PlusIcon :size="14" />
            {{ t('chat.messageClusters.add') }}
          </button>
        </div>
      </div>
    </ModalDialog>

    <!-- QuickSend edit modal (pre-filled add mode) -->
    <QuickSendEditModal
      :open="quickSendEditOpen"
      :editing-item="null"
      :initial-values="quickSendInitialValues"
      @close="quickSendEditOpen = false; quickSendInitialValues = undefined"
      @saved="onQuickSendSaved"
    />
  </BottomSheet>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { Sparkles as SparklesIcon, Plus as PlusIcon, List as ListIcon, Play as PlayIcon, RefreshCw as RefreshCwIcon } from 'lucide-vue-next'
import BottomSheet from '@/components/common/BottomSheet.vue'
import ModalDialog from '@/components/common/ModalDialog.vue'
import QuickSendEditModal from '@/components/chat/QuickSendEditModal.vue'
import { useMessageClusters, type MessageCluster } from '@/composables/useMessageClusters'
import { useToast } from '@/composables/useToast'
import { useTabDrawer } from '@/composables/useTabDrawer'
import { formatDuration } from '@/utils/format'

const { t } = useI18n()
const toast = useToast()
const { clusters, loaded, loading, computing, progress, mode, updatedAt, fetchClusters, startCompute: rawStartCompute, syncProgressOnce } = useMessageClusters()

// ── QuickSend edit modal ──
const quickSendEditOpen = ref(false)
const quickSendInitialValues = ref<{ label: string; command: string } | undefined>(undefined)

// ── Variants detail dialog ──
const variantsDialogOpen = ref(false)
const variantsDialogItems = ref<string[]>([])
const variantsDialogRepresentative = ref('')

function showVariants(cluster: MessageCluster) {
  if (cluster.variants.length === 0) return
  variantsDialogRepresentative.value = cluster.representative
  variantsDialogItems.value = cluster.variants
  variantsDialogOpen.value = true
}

// ── Tab binding (useTabDrawer) ──
// BottomSheet is teleported to <body>, so it survives v-show tab-panel hiding
// and would remain visible when switching to other tabs (file manager, etc.).
// useTabDrawer('chat', { autoRestore: false }) ensures:
//   1. effectiveOpen = false when activeTab !== 'chat' (drawer auto-hides)
//   2. autoRestore: false — won't auto-reopen when switching back to chat tab
//      (it's a recommendation picker, not a persistent panel)
// IMPORTANT: All BottomSheet-based drawers on the chat tab MUST use useTabDrawer
// to stay hidden on non-chat tabs. See useTabDrawer.ts for details.
const drawer = useTabDrawer('chat', { autoRestore: false })

// No polling stop needed — drawer close just closes dialog
watch(() => drawer.effectiveOpen.value, (isOpen) => {
  if (!isOpen) {
    variantsDialogOpen.value = false
  }
})

const progressPercent = computed(() => {
  // Use server-provided progress_pct when available (fine-grained clustering progress)
  if (progress.value.progress_pct > 0) {
    // Map phase + pct to overall: extracting=0-20%, clustering=20-80%, saving=80-90%, done=100%
    const phase = progress.value.phase
    const pct = progress.value.progress_pct
    if (phase === 'extracting') return pct * 20 / 100
    if (phase === 'clustering') return 20 + pct * 60 / 100
    if (phase === 'saving') return 80 + pct * 10 / 100
    return pct
  }
  // Fallback: no fine-grained progress, use phase-based estimates
  const phase = progress.value.phase
  const status = progress.value.status
  if (status === 'done') return 100
  if (phase === 'extracting') return 10
  if (phase === 'clustering') return 50
  if (phase === 'saving') return 90
  return 0
})

const localizedMode = computed(() => {
  const m = mode.value
  if (!m) return ''
  const key = `chat.messageClusters.mode_${m}`
  // If i18n has the key, use it; otherwise fall back to raw value
  return t(key) !== key ? t(key) : m
})

const phaseText = computed(() => {
  const p = progress.value
  const phase = p.phase
  if (phase === 'extracting') return t('chat.messageClusters.phase_extracting', { msgCount: p.msg_count })
  if (phase === 'clustering') return t('chat.messageClusters.phase_clustering')
  if (phase === 'saving') return t('chat.messageClusters.phase_saving')
  return t('chat.messageClusters.computing')
})

const elapsedText = computed(() => {
  return formatDuration(progress.value.elapsed_ms)
})

async function open() {
  drawer.open()
  await fetchClusters()
  // Sync with server state once to detect ongoing computation
  await syncProgressOnce()
}

async function handleStartCompute() {
  const result = await rawStartCompute()
  if (result === 'already_running') {
    toast.show(t('chat.messageClusters.computing'), { type: 'info' })
  } else if (result === 'error') {
    toast.show(t('chat.messageClusters.error'), { type: 'error' })
  }
}

function addVariantToQuickSend(variant: string) {
  quickSendInitialValues.value = { label: variantsDialogRepresentative.value, command: variant }
  quickSendEditOpen.value = true
}

async function onQuickSendSaved() {
  quickSendEditOpen.value = false
  quickSendInitialValues.value = undefined
  await fetchClusters()
}

defineExpose({ open })
</script>

<style>
.mc-content {
  display: flex;
  flex-direction: column;
  height: 100%;
  overflow: hidden;
  padding: 12px;
}

.mc-computing {
  display: flex;
  flex-direction: column;
  gap: 8px;
  padding: 16px 0;
}

.mc-computing-info {
  display: flex;
  align-items: center;
  justify-content: space-between;
}

.mc-progress-bar {
  height: 6px;
  background: var(--bg-tertiary, #e5e5e5);
  border-radius: 3px;
  overflow: hidden;
}

.mc-progress-fill {
  height: 100%;
  background: var(--accent-color, #0066cc);
  border-radius: 3px;
  transition: width 0.3s ease;
}

.mc-phase-text {
  font-size: 13px;
  color: var(--text-secondary, #666);
  flex: 1;
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.mc-progress-detail {
  font-size: 11px;
  color: var(--text-muted, #999);
  flex-shrink: 0;
}

.mc-error {
  display: flex;
  align-items: center;
  gap: 12px;
  padding: 16px 0;
  color: #e53e3e;
  font-size: 13px;
}

.mc-loading {
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 8px;
  padding: 32px 0;
  color: var(--text-muted, #999);
  font-size: 13px;
}

.mc-load-spinner {
  width: 14px;
  height: 14px;
  border: 2px solid var(--border-color, #e5e5e5);
  border-top-color: var(--text-secondary, #666);
  border-radius: 50%;
  animation: mc-spin 0.6s linear infinite;
}

@keyframes mc-spin {
  to { transform: rotate(360deg); }
}

.mc-empty {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 12px;
  padding: 24px 0;
  color: var(--text-muted, #999);
  font-size: 13px;
}

.mc-results {
  display: flex;
  flex-direction: column;
  gap: 8px;
  height: 100%;
  overflow: hidden;
}

.mc-results-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  font-size: 12px;
  color: var(--text-muted, #999);
}

.mc-cache-status {
  flex: 1;
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.mc-cluster-list {
  flex: 1;
  overflow-y: auto;
}

.mc-cluster-item {
  display: flex;
  align-items: center;
  gap: 6px;
  padding: 8px 0;
  border-bottom: 1px solid var(--border-color, #e5e5e5);
  font-size: 13px;
  cursor: pointer;
}

.mc-cluster-item:last-child {
  border-bottom: none;
}

.mc-cluster-item:hover {
  background: var(--bg-tertiary, rgba(0,0,0,0.04));
}

.mc-cluster-representative {
  flex: 1;
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  color: var(--text-primary);
}

.mc-cluster-count {
  flex-shrink: 0;
  background: var(--bg-tertiary, #e5e5e5);
  border-radius: 8px;
  padding: 2px 6px;
  font-size: 11px;
  color: var(--text-secondary, #666);
}

.mc-btn {
  padding: 4px 10px;
  border: 1px solid var(--border-color, #ddd);
  border-radius: 4px;
  font-size: 12px;
  cursor: pointer;
  background: var(--bg-primary, #fff);
  color: var(--text-primary);
  flex-shrink: 0;
  display: inline-flex;
  align-items: center;
  gap: 4px;
}

.mc-btn:hover {
  background: var(--bg-tertiary, #f0f0f0);
}

.mc-btn.primary {
  background: var(--accent-color, #0066cc);
  color: #fff;
  border-color: var(--accent-color, #0066cc);
}

.mc-btn.primary:hover {
  opacity: 0.9;
}

.mc-btn.add {
  color: var(--accent-color, #0066cc);
  border-color: var(--accent-color, #0066cc);
  background: none;
  display: inline-flex;
  align-items: center;
  gap: 2px;
  padding: 2px 6px;
  font-size: 11px;
}

.mc-btn.add:hover {
  background: rgba(0, 102, 204, 0.1);
}

.mc-variants-dialog-content {
  display: flex;
  flex-direction: column;
}

.mc-variant-item {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 10px 12px;
  border-bottom: 1px solid var(--border-color, #e5e5e5);
  font-size: 13px;
  color: var(--text-primary);
}

.mc-variant-item:last-child {
  border-bottom: none;
}

.mc-variant-text {
  flex: 1;
  min-width: 0;
}

.bs-header-actions {
  margin-left: auto;
  display: flex;
  align-items: center;
  gap: 4px;
}

.mc-header-btn {
  width: 24px;
  height: 24px;
  border: none;
  background: none;
  color: var(--accent-color, #0066cc);
  cursor: pointer;
  display: flex;
  align-items: center;
  justify-content: center;
  border-radius: 4px;
  transition: background 0.15s;
}

.mc-header-btn:hover {
  background: rgba(0, 102, 204, 0.1);
}
</style>
