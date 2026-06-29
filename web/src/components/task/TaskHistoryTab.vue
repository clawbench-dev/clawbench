<template>
  <div class="task-history-page">
    <!-- Header with breadcrumb + refresh -->
    <div class="history-header">
      <TaskBreadcrumb />
      <button class="header-btn refresh-btn" :class="{ spinning: refreshing }" :disabled="refreshing || loading" @click="onRefresh" :title="t('common.refresh')">
        <RefreshCw :size="14" />
      </button>
    </div>
    <!-- History content -->
    <div ref="listRef" class="task-history-tab">
    <div v-if="loading && allExecutions.length === 0" class="history-empty">
      <Loader2 class="spin-icon" :size="20" />
      <span>{{ t('common.loading') }}</span>
    </div>
    <div v-else-if="allExecutions.length === 0" class="history-empty">
      <History class="empty-icon" :size="32" />
      <span>{{ t('task.exec.noExecutions') }}</span>
    </div>
    <div v-else class="history-list">
      <div v-if="executions.length > 0" class="clear-all-row">
        <button class="clear-all-btn" @click="deleteAllExecutions">{{ t('task.exec.clearAll') }}</button>
      </div>
      <div v-for="exec in allExecutions" :key="exec.id" class="execution-item" :class="{ running: isRunning(exec), unread: !isRunning(exec) && isUnreadDisplay(exec), 'just-completed': isJustCompleted(exec) }" @click="openDetail(exec)">
        <div class="execution-row">
          <div class="execution-info">
            <div class="execution-time-row">
              <template v-if="isRunning(exec)">
                <span class="exec-running-dot"></span>
                <span class="exec-running-label">{{ t('task.exec.running') }}</span>
              </template>
              <template v-else>
                <span v-if="isUnreadDisplay(exec)" class="exec-unread-dot"></span>
              </template>
              <span v-if="exec.triggerType === 'manual'" class="exec-trigger-type manual">{{ t('task.exec.manual') }}</span>
              <span v-else class="exec-trigger-type auto">{{ t('task.exec.auto') }}</span>
              <template v-if="!isRunning(exec)">
                <span v-if="exec.status === 'cancelled'" class="exec-status-badge cancelled">{{ t('task.exec.statusCancelled') }}</span>
                <span v-else-if="exec.status === 'failed'" class="exec-status-badge failed">{{ t('task.exec.statusFailed') }}</span>
              </template>
            </div>
            <template v-if="!isRunning(exec)">
              <div class="exec-summary-row">
                <div v-if="exec.preview" class="exec-summary">{{ exec.preview }}</div>
                <div v-else class="exec-summary empty">{{ t('task.exec.noTextOutput') }}</div>
              </div>
              <div v-if="exec.metadata" class="exec-meta-row">
                <span v-if="exec.metadata.wallMs" class="exec-meta-tag exec-meta-duration">{{ formatDuration(exec.metadata.wallMs) }}</span>
                <span v-if="exec.metadata.model" class="exec-meta-tag">{{ exec.metadata.model }}</span>
                <span v-if="exec.metadata.inputTokens || exec.metadata.outputTokens" class="exec-meta-tag">{{ formatTokens(exec.metadata) }}</span>
              </div>
            </template>
          </div>
          <template v-if="isRunning(exec)">
            <button class="cancel-exec-btn" @click.stop="cancelExecution(exec.id)" :title="t('task.exec.cancel')">
              <Square :size="12" />
            </button>
          </template>
          <template v-else>
            <button class="delete-exec-btn" @click.stop="deleteExecution(exec.id)" :title="t('task.delete')">
              <Trash2 :size="14" />
            </button>
          </template>
        </div>
      </div>
      <!-- Infinite scroll sentinel -->
      <div ref="sentinelRef" class="history-list-sentinel"></div>
      <div v-if="loadingMore" class="history-loading-more">
        <Loader2 class="spin-icon" :size="14" />
        <span>{{ t('common.loading') }}</span>
      </div>
    </div>
    </div>
  </div>
</template>

<script setup>
import { ref, watch, onUnmounted, computed, nextTick } from 'vue'
import { useI18n } from 'vue-i18n'
import { Square, Loader2, History, Trash2, RefreshCw } from 'lucide-vue-next'
import TaskBreadcrumb from '@/components/task/TaskBreadcrumb.vue'
import { useTaskHistory } from '@/composables/useTaskHistory.ts'
import { formatDuration } from '@/utils/format.ts'

const props = defineProps({
  task: Object,
})

const emit = defineEmits(['open-file'])

const { t } = useI18n()

const refreshing = ref(false)

// Scroll container and sentinel refs for IntersectionObserver
const listRef = ref(null)
const sentinelRef = ref(null)
let observer = null

async function onRefresh() {
  refreshing.value = true
  try {
    await Promise.all([loadExecutions(), loadRunningStatus()])
  } finally {
    refreshing.value = false
  }
}

// Task history composable (ISS-011 + ISS-015 + ISS-016)
const {
  loading,
  loadingMore,
  hasMore,
  allExecutions,
  executions,
  isRunning,
  isJustCompleted,
  locallyReadIds,
  loadExecutions,
  loadMoreExecutions,
  reloadExecutions,
  loadRunningStatus,
  cancelExecution,
  deleteExecution,
  deleteAllExecutions,
  openDetail,
  isUnreadDisplay,
  onTaskChange,
} = useTaskHistory({ task: computed(() => props.task) })

function formatTokens(meta) {
  const parts = []
  if (meta.inputTokens) parts.push(`${meta.inputTokens.toLocaleString()}↑`)
  if (meta.outputTokens) parts.push(`${meta.outputTokens.toLocaleString()}↓`)
  return parts.join(' ')
}

/** Set up IntersectionObserver for infinite scroll */
function setupObserver() {
  if (observer) {
    observer.disconnect()
    observer = null
  }
  if (!sentinelRef.value || !listRef.value) return
  observer = new IntersectionObserver((entries) => {
    if (entries[0].isIntersecting && hasMore.value && !loadingMore.value) {
      loadMoreExecutions()
    }
  }, { threshold: 0.1, rootMargin: '100px', root: listRef.value })
  observer.observe(sentinelRef.value)
}

let pollTimer = null

function startPolling() {
  stopPolling()
  pollTimer = setInterval(loadRunningStatus, 3000)
}

function stopPolling() {
  if (pollTimer !== null) {
    clearInterval(pollTimer)
    pollTimer = null
  }
}

function onOpenFile(filePath) {
  emit('open-file', filePath)
}

watch(() => props.task?.id, (newId) => {
  if (!newId) {
    stopPolling()
    return
  }
  onTaskChange()
  loadExecutions().then(() => nextTick(setupObserver))
  loadRunningStatus()
  startPolling()
}, { immediate: true })

onUnmounted(() => {
  stopPolling()
  onTaskChange() // Abort in-flight requests (ISS-016)
  if (observer) {
    observer.disconnect()
    observer = null
  }
})
</script>

<style scoped>
.task-history-page {
  display: flex;
  flex-direction: column;
  height: 100%;
  overflow: hidden;
  background: var(--bg-primary, #ffffff);
}

.history-header {
  display: flex;
  align-items: center;
  padding: 4px 8px;
  flex-shrink: 0;
  border-bottom: 1px solid var(--border-color, #e5e5e5);
  gap: 6px;
}

.header-btn {
  width: 28px;
  height: 28px;
  border: none;
  border-radius: 14px;
  background: var(--bg-secondary, #f1f3f5);
  color: var(--text-secondary, #666);
  cursor: pointer;
  display: flex;
  align-items: center;
  justify-content: center;
  flex-shrink: 0;
  transition: all 0.2s ease;
}

.header-btn:disabled {
  opacity: 0.5;
  cursor: not-allowed;
}

@media (hover: hover) {
  .header-btn:hover:not(:disabled) {
    background: var(--bg-tertiary, #eef1f4);
    color: var(--accent-color, #0066cc);
  }
}

.header-btn:active:not(:disabled) {
  transform: scale(0.9);
}

.header-btn.spinning svg {
  animation: spin 1s linear infinite;
}

.task-history-tab {
  flex: 1;
  overflow-y: auto;
  min-height: 0;
  padding: 8px;
}

/* ── Empty state ── */
.history-empty {
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 12px;
  height: 100%;
  color: var(--text-muted, #999);
  font-size: 14px;
}

.spin-icon {
  animation: spin 1s linear infinite;
}

@keyframes spin {
  100% { transform: rotate(360deg); }
}

.empty-icon {
  opacity: 0.5;
}

/* ── Execution items ── */
.history-list {
  display: flex;
  flex-direction: column;
  gap: 10px;
}

.execution-item {
  background: var(--bg-secondary, #f8f9fa);
  border: 1px solid var(--border-color, #e5e5e5);
  border-radius: 10px;
  overflow: hidden;
  transition: all 0.2s ease;
}

@media (hover: hover) {
  .execution-item:not(.running):hover {
    border-color: var(--accent-color, #0066cc);
    box-shadow: 0 2px 8px rgba(0, 0, 0, 0.04);
    transform: translateY(-1px);
  }
}

.execution-item:active:not(.running) {
  background: var(--bg-tertiary, #eef1f4);
  transform: translateY(0);
}

.execution-item.running {
  background: color-mix(in srgb, var(--success-color, #16a34a) 5%, var(--bg-secondary, #f8f9fa));
  border-color: color-mix(in srgb, var(--success-color, #16a34a) 30%, transparent);
  animation: exec-card-running 2s ease-in-out infinite;
}

@keyframes exec-card-running {
  0%, 100% { border-color: color-mix(in srgb, var(--success-color, #16a34a) 30%, transparent); }
  50% { border-color: color-mix(in srgb, var(--success-color, #16a34a) 55%, transparent); }
}

.execution-row {
  display: flex;
  align-items: center;
  gap: 6px;
  padding: 10px 12px;
  cursor: pointer;
}

.execution-item.running .execution-row {
  cursor: pointer;
}

.execution-info {
  flex: 1;
  min-width: 0;
  display: flex;
  flex-direction: column;
  gap: 6px;
}

.execution-time-row {
  display: flex;
  align-items: center;
  gap: 8px;
}

/* ── Unread dot (static) ── */
.exec-unread-dot {
  width: 6px;
  height: 6px;
  border-radius: 50%;
  background: var(--accent-color, #0066cc);
  flex-shrink: 0;
}

.execution-item.unread {
  border-left: 3px solid var(--accent-color, #0066cc);
}

/* ── Trigger type badges ── */
.exec-trigger-type {
  font-size: 10px;
  padding: 2px 6px;
  border-radius: 4px;
  font-weight: 600;
  flex-shrink: 0;
  white-space: nowrap;
  text-transform: uppercase;
  letter-spacing: 0.02em;
}

.exec-trigger-type.manual {
  background: rgba(59, 130, 246, 0.12);
  color: #2563eb;
}

.exec-trigger-type.auto {
  background: rgba(34, 197, 94, 0.12);
  color: #16a34a;
}

/* ── Status badges ── */
.exec-status-badge {
  font-size: 10px;
  padding: 2px 6px;
  border-radius: 4px;
  font-weight: 600;
  margin-left: auto;
  text-transform: uppercase;
  letter-spacing: 0.02em;
}
.exec-status-badge.cancelled {
  background: var(--bg-tertiary, #e5e7eb);
  color: var(--text-secondary, #4b5563);
}
.exec-status-badge.failed {
  background: rgba(239, 68, 68, 0.12);
  color: #dc2626;
}

/* ── Summary ── */
.exec-summary-row {
  display: flex;
  align-items: center;
}

.exec-summary {
  font-size: 13px;
  color: var(--text-secondary, #4b5563);
  line-height: 1.4;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  flex: 1;
  min-width: 0;
}

.exec-summary.empty {
  color: var(--text-muted, #9ca3af);
  font-style: italic;
}

/* ── Meta tags ── */
.exec-meta-row {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-wrap: wrap;
  margin-top: 2px;
}

.exec-meta-tag {
  font-size: 11px;
  padding: 2px 6px;
  border-radius: 4px;
  background: var(--bg-primary, #ffffff);
  border: 1px solid var(--border-color, #e5e7eb);
  color: var(--text-secondary, #6b7280);
  white-space: nowrap;
  font-variant-numeric: tabular-nums;
  display: inline-flex;
  align-items: center;
}

.exec-meta-duration {
  font-weight: 600;
  color: var(--text-primary, #111827);
  background: rgba(0, 102, 204, 0.05);
  border-color: rgba(0, 102, 204, 0.1);
}

/* ── Running execution indicator ── */
.exec-running-dot {
  width: 10px;
  height: 10px;
  border-radius: 50%;
  background: #16a34a;
  flex-shrink: 0;
  animation: exec-running-pulse 0.8s ease-in-out infinite;
}

@keyframes exec-running-pulse {
  0%, 100% { opacity: 1; box-shadow: 0 0 0 0 rgba(22, 163, 74, 0.5); }
  50% { opacity: 0.7; box-shadow: 0 0 10px 4px rgba(22, 163, 74, 0.3); }
}

/* ── Just-completed execution flash ── */
.execution-item.just-completed {
  animation: exec-just-completed 0.6s ease-out forwards;
}

@keyframes exec-just-completed {
  0% { background: color-mix(in srgb, var(--accent-color, #0066cc) 15%, var(--bg-secondary, #f8f9fa)); transform: translateX(8px); opacity: 0.7; }
  100% { background: var(--bg-secondary, #f8f9fa); transform: translateX(0); opacity: 1; }
}

.exec-running-label {
  font-size: 13px;
  font-weight: 600;
  color: #16a34a;
}

/* ── Cancel button ── */
.cancel-exec-btn {
  width: 32px;
  height: 32px;
  border: none;
  background: rgba(239, 68, 68, 0.1);
  color: #ef4444;
  border-radius: 8px;
  cursor: pointer;
  display: flex;
  align-items: center;
  justify-content: center;
  flex-shrink: 0;
  transition: all 0.2s;
}

@media (hover: hover) {
  .cancel-exec-btn:hover {
    background: rgba(239, 68, 68, 0.2);
    transform: scale(1.05);
  }
}

.cancel-exec-btn:active {
  transform: scale(0.95);
}

/* ── Delete button ── */
.delete-exec-btn {
  width: 28px;
  height: 28px;
  border: none;
  background: transparent;
  color: var(--text-muted, #9ca3af);
  border-radius: 6px;
  cursor: pointer;
  display: flex;
  align-items: center;
  justify-content: center;
  flex-shrink: 0;
  transition: all 0.2s;
  opacity: 0;
}

@media (hover: hover) {
  .execution-item:not(.running):hover .delete-exec-btn {
    opacity: 1;
  }
  .delete-exec-btn:hover {
    background: rgba(239, 68, 68, 0.1);
    color: #ef4444;
  }
}

/* Touch devices: always visible but subtle */
@media (hover: none) {
  .delete-exec-btn {
    opacity: 0.5;
  }
}

.delete-exec-btn:active {
  transform: scale(0.9);
}

/* ── Clear all row ── */
.clear-all-row {
  display: flex;
  justify-content: flex-end;
  margin-bottom: 2px;
}

.clear-all-btn {
  border: none;
  background: transparent;
  color: var(--text-muted, #9ca3af);
  font-size: 12px;
  cursor: pointer;
  padding: 4px 8px;
  border-radius: 6px;
  transition: all 0.2s;
}

@media (hover: hover) {
  .clear-all-btn:hover {
    color: #ef4444;
    background: rgba(239, 68, 68, 0.06);
  }
}

.clear-all-btn:active {
  transform: scale(0.95);
}

/* ── Infinite scroll sentinel ── */
.history-list-sentinel {
  height: 1px;
}

.history-loading-more {
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 6px;
  padding: 8px;
  color: var(--text-muted, #9ca3af);
  font-size: 12px;
}
</style>
