<template>
  <BottomSheet :open="visible" auto :title="t('chat.messageClusters.title')" @close="visible = false">
    <template #header>
      <SparklesIcon :size="16" class="bs-header-icon" />
      <span class="bs-header-title">{{ t('chat.messageClusters.title') }}</span>
    </template>

    <div class="mc-content">
      <!-- Computing state -->
      <div v-if="computing" class="mc-computing">
        <div class="mc-progress-bar">
          <div class="mc-progress-fill" :style="{ width: progressPercent + '%' }" />
        </div>
        <span class="mc-phase-text">{{ phaseText }}</span>
      </div>

      <!-- Error state -->
      <div v-else-if="progress.status === 'error'" class="mc-error">
        <span>{{ t('chat.messageClusters.error') }}</span>
        <button class="mc-btn" @click="startCompute">{{ t('chat.messageClusters.retry') }}</button>
      </div>

      <!-- Loading state -->
      <div v-else-if="loading" class="mc-loading">
        <span>{{ t('chat.messageClusters.loading') }}</span>
      </div>

      <!-- No cache (idle) -->
      <div v-else-if="!loaded || clusters.length === 0 && progress.status === 'idle'" class="mc-empty">
        <span>{{ t('chat.messageClusters.noCache') }}</span>
        <button class="mc-btn primary" @click="startCompute">{{ t('chat.messageClusters.firstAnalyze') }}</button>
      </div>

      <!-- Has cached results -->
      <div v-else class="mc-results">
        <div class="mc-results-header">
          <span class="mc-cache-status">{{ t('chat.messageClusters.cacheStatus', { mode: mode, updatedAt: updatedAt }) }}</span>
          <button class="mc-btn" @click="startCompute">{{ t('chat.messageClusters.reanalyze') }}</button>
        </div>
        <div class="mc-cluster-list">
          <div v-for="cluster in clusters" :key="cluster.id" class="mc-cluster-item">
            <span class="mc-cluster-representative">{{ cluster.representative }}</span>
            <span class="mc-cluster-count">{{ cluster.total_count }}</span>
            <div v-if="cluster.variants.length > 0" class="mc-variant-tags">
              <span v-for="v in cluster.variants.slice(0, 3)" :key="v" class="mc-variant-tag">{{ v }}</span>
            </div>
            <button class="mc-btn add" @click="addToQuickSend(cluster)">
              {{ t('chat.messageClusters.addQuickSend') }}
            </button>
          </div>
        </div>
      </div>
    </div>
  </BottomSheet>
</template>

<script setup lang="ts">
import { ref, computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { Sparkles as SparklesIcon } from 'lucide-vue-next'
import BottomSheet from '@/components/common/BottomSheet.vue'
import { useMessageClusters, type MessageCluster } from '@/composables/useMessageClusters'
import { useQuickSend } from '@/composables/useQuickSend'
import { useToast } from '@/composables/useToast'
import { appLog } from '@/utils/appLog'

const TAG = 'MsgClusterDrawer'

const { t } = useI18n()
const toast = useToast()
const { clusters, loaded, loading, computing, progress, mode, updatedAt, fetchClusters, startCompute } = useMessageClusters()
const { addItem } = useQuickSend()

const visible = ref(false)

const progressPercent = computed(() => {
  const phase = progress.value.phase
  const status = progress.value.status
  if (status === 'done') return 100
  if (phase === 'extracting') return 20
  if (phase === 'clustering') return 50
  if (phase === 'saving') return 90
  return 0
})

const phaseText = computed(() => {
  const p = progress.value
  const phase = p.phase
  const elapsed = formatElapsed(p.elapsed_ms)
  if (phase === 'extracting') return t('chat.messageClusters.phase_extracting', { msgCount: p.msg_count, elapsed })
  if (phase === 'clustering') return t('chat.messageClusters.phase_clustering', { elapsed })
  if (phase === 'saving') return t('chat.messageClusters.phase_saving', { elapsed })
  return t('chat.messageClusters.computing')
})

function formatElapsed(ms: number): string {
  if (ms < 1000) return `${ms}ms`
  return `${(ms / 1000).toFixed(1)}s`
}

async function open() {
  visible.value = true
  await fetchClusters()
}

async function addToQuickSend(cluster: MessageCluster) {
  const ok = await addItem({ label: cluster.representative, command: cluster.representative })
  if (ok) {
    toast.show(t('chat.quickSend.itemSaved'), { icon: '✅', type: 'success' })
    await fetchClusters()
  } else {
    appLog.e(TAG, `Failed to add cluster ${cluster.id} to quick send`)
  }
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
  padding: 16px 0;
  color: var(--text-muted, #999);
  font-size: 13px;
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
}

.mc-cluster-item:last-child {
  border-bottom: none;
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

.mc-variant-tags {
  display: flex;
  gap: 4px;
  flex-shrink: 0;
}

.mc-variant-tag {
  font-size: 11px;
  color: var(--text-muted, #999);
  background: var(--bg-tertiary, #f5f5f5);
  padding: 1px 4px;
  border-radius: 3px;
  max-width: 60px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
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
}

.mc-btn.add:hover {
  background: rgba(0, 102, 204, 0.1);
}
</style>
