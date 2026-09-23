<template>
  <div ref="listRef" class="task-history-tab">
    <div v-if="loading && allExecutions.length === 0" class="history-empty">
      <LoadingIndicator size="sm" inline />
      <span>{{ t('common.loading') }}</span>
    </div>
    <div v-else-if="allExecutions.length === 0" class="history-empty">
      <History class="empty-icon" :size="32" />
      <span>{{ t('task.exec.noExecutions') }}</span>
    </div>
    <div v-else class="history-list">
      <div v-for="exec in allExecutions" :key="exec.id" class="execution-item" :class="{ running: isRunning(exec), unread: !isRunning(exec) && isUnreadDisplay(exec), 'just-completed': isJustCompleted(exec) }" @click="openDetail(exec)">
        <div class="execution-row">
          <div class="execution-info">
            <div class="execution-time-row">
              <template v-if="!isRunning(exec) && isUnreadDisplay(exec)">
                <span class="exec-unread-dot"></span>
              </template>
              <span v-if="exec.triggerType === 'manual'" class="exec-trigger-type manual">{{ t('task.exec.manual') }}</span>
              <span v-else class="exec-trigger-type auto">{{ t('task.exec.auto') }}</span>
              <template v-if="isRunning(exec)">
                <span class="exec-status-badge running">
                  <span class="exec-running-dot"></span>
                  <!-- A script-phase entry is a precondition, not an AI run:
                       it is never counted as "running" in the task list, so
                       labelling it plainly "running" here would contradict the
                       list. Say what it actually is. -->
                  <span>{{ isScriptPhase(exec) ? t('task.exec.statusScriptPhase') : t('task.exec.running') }}</span>
                </span>
                <span class="exec-start-time">{{ formatDateTime(exec.startedAt) }}</span>
                <span class="exec-duration" :title="formatDuration(elapsedMs(exec.startedAt))">{{ formatElapsed(exec.startedAt) }}</span>
              </template>
              <template v-else>
                <span v-if="exec.status === 'cancelled'" class="exec-status-badge cancelled">{{ t('task.exec.statusCancelled') }}</span>
                <span v-else-if="exec.status === 'skipped'" class="exec-status-badge skipped">{{ t('task.exec.statusSkipped') }}</span>
                <span v-else-if="exec.status === 'failed'" class="exec-status-badge failed">{{ t('task.exec.statusFailed') }}</span>
                <span class="exec-start-time">{{ formatDateTime(exec.createdAt) }}</span>
                <span v-if="exec.metadata?.wallMs" class="exec-duration">{{ formatDuration(exec.metadata.wallMs) }}</span>
              </template>
            </div>
            <template v-if="!isRunning(exec)">
              <!-- Event-triggered runs trace back to the issue/PR that fired
                   them; without this the row only shows a summary and the user
                   cannot tell what caused the run. -->
              <div v-if="eventSource(exec)" class="exec-event-row">
                <Zap :size="11" class="exec-event-icon" />
                <span class="exec-event-source" :title="exec.eventUrl">{{ eventSource(exec) }}</span>
              </div>
              <div class="exec-summary-row">
                <div v-if="exec.preview" class="exec-summary">{{ exec.preview }}</div>
                <!-- A skipped run produced no AI output by design, so "no text
                     output" would read as a failure. Say why it is empty. -->
                <div v-else-if="exec.status === 'skipped'" class="exec-summary empty">{{ t('task.exec.skippedHint') }}</div>
                <div v-else class="exec-summary empty">{{ t('task.exec.noTextOutput') }}</div>
              </div>
              <div v-if="exec.metadata && (exec.metadata.model || exec.metadata.inputTokens || exec.metadata.outputTokens)" class="exec-meta-row">
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
        <LoadingIndicator size="sm" inline />
        <span>{{ t('common.loading') }}</span>
      </div>
    </div>
  </div>
</template>

<script setup>
import { ref, watch, onUnmounted, computed, nextTick } from 'vue'
import { useI18n } from 'vue-i18n'
import { Square, History, Trash2, Zap } from 'lucide-vue-next'
import { useTaskHistory } from '@/composables/useTaskHistory.ts'
import { useGlobalEvents } from '@/composables/useGlobalEvents.ts'
import { eventSourceLabel } from '@/utils/forgeEventLabels'
import { formatDuration, formatDateTime } from '@/utils/format.ts'
import LoadingIndicator from '@/components/common/LoadingIndicator.vue'

const props = defineProps({
  task: Object,
  /** External scroll container ref (provided by parent when merged into a single scroll view) */
  scrollRoot: Object,
})

const { t } = useI18n()

// WS subscription used to drive running-status sync (replaces a 3s poll).
const { onEvent } = useGlobalEvents()

// Scroll container and sentinel refs for IntersectionObserver
const listRef = ref(null)
const sentinelRef = ref(null)
let observer = null

// Task history composable (ISS-011 + ISS-015 + ISS-016)
const {
  loading,
  loadingMore,
  hasMore,
  allExecutions,
  isRunning,
  isScriptPhase,
  isJustCompleted,
  loadExecutions,
  loadMoreExecutions,
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

/** Compact "owner/repo PR #123" for an event-triggered run, or '' when the run
 *  was not triggered by a forge event. Derived from the stored source URL. */
function eventSource(exec) {
  return eventSourceLabel(exec?.eventUrl)
}

/** Set up IntersectionObserver for infinite scroll.
 *  Uses the parent scroll container as root when provided (merged view),
 *  otherwise falls back to the nearest scrollable ancestor. */
function setupObserver() {
  if (observer) {
    observer.disconnect()
    observer = null
  }
  if (!sentinelRef.value) return
  const root = props.scrollRoot?.value || listRef.value
  if (!root) return
  observer = new IntersectionObserver((entries) => {
    if (entries[0].isIntersecting && hasMore.value && !loadingMore.value) {
      loadMoreExecutions()
    }
  }, { threshold: 0.1, rootMargin: '100px', root })
  observer.observe(sentinelRef.value)
}

// ── Running-status sync ──
// Driven by the backend's task_update broadcast rather than a 3s poll: the
// scheduler already emits `running` on start (OnStarted) and a terminal status
// on completion, so polling only ever re-learned what the server had just told
// us. This component is the sole consumer of runningExecutions, so it subscribes
// for its own task only.
let unsubscribeTaskUpdate = null
let resyncTimer = null

/** Coalesce a burst of task_update events (running → completed arrive
 *  back-to-back) into a single GET /api/tasks/{id}. */
function scheduleRunningStatusSync() {
  if (resyncTimer) clearTimeout(resyncTimer)
  resyncTimer = setTimeout(() => {
    resyncTimer = null
    loadRunningStatus()
  }, 200)
}

// ── Script-phase fallback poll ──
// The backend emits NO task_update for the pre-AI script phase (a "running"
// event would fire a "task started" notification for a run that may be skipped
// silently), so the WS-driven sync above can never learn that a script started
// or ended. Without a fallback the script-phase row would never appear, making
// its cancel button unreachable, and a finished script's `skipped` record would
// not show up until the next unrelated refresh.
//
// Gated on the task actually having a script: a task with no script keeps the
// original event-driven design, and this component is the only poller. It is a
// low-frequency poll while the task's history is on screen, stops on unmount,
// and skips a tick while the page is hidden (a background tab has no user to
// show the row to).
const SCRIPT_POLL_MS = 5000
let scriptPollTimer = null

function startScriptPoll() {
  if (scriptPollTimer !== null) return
  scriptPollTimer = setInterval(() => {
    if (document.hidden) return
    loadRunningStatus()
  }, SCRIPT_POLL_MS)
}

function stopScriptPoll() {
  if (scriptPollTimer !== null) {
    clearInterval(scriptPollTimer)
    scriptPollTimer = null
  }
}

/** Start the poll only for a task that has a script, stop it otherwise. */
function syncScriptPoll() {
  if (props.task?.script) startScriptPoll()
  else stopScriptPoll()
}

// ── Live elapsed-time display for running executions ──
// Ticks every second so running entries show a live "time elapsed" counter.
const elapsedNow = ref(0)
let elapsedTimer = null

function elapsedMs(startedAt) {
  if (!startedAt) return 0
  // Reference the live ticker so this re-evaluates every second
  void elapsedNow.value
  const start = new Date(startedAt).getTime()
  if (Number.isNaN(start)) return 0
  return Math.max(0, Date.now() - start)
}

function formatElapsed(startedAt) {
  return formatDuration(elapsedMs(startedAt))
}

function startElapsedTicker() {
  if (elapsedTimer) return
  elapsedTimer = setInterval(() => {
    elapsedNow.value = Date.now()
  }, 1000)
}

function stopElapsedTicker() {
  if (elapsedTimer !== null) {
    clearInterval(elapsedTimer)
    elapsedTimer = null
  }
}

// Start/stop the ticker based on whether any execution is running.
// Uses flush: 'sync' so the computed re-evaluates as soon as runningExecutions changes.
// immediate: true covers the case where runningExecutions is already populated on mount.
watch(
  () => allExecutions.value.some(isRunning),
  (hasRunning) => {
    if (hasRunning) startElapsedTicker()
    else stopElapsedTicker()
  },
  { flush: 'sync', immediate: true },
)

watch(() => props.task?.id, (newId) => {
  if (!newId) {
    stopElapsedTicker()
    stopScriptPoll()
    return
  }
  onTaskChange()
  loadExecutions().then(() => nextTick(setupObserver))
  // Authoritative initial sync. `running` events are not persisted for offline
  // replay (IsNotifiableEvent keeps only completed/failed/cancelled), so the
  // live subscription alone could miss a run that started while we were away.
  loadRunningStatus()
  // The script phase emits no task_update, so a poll is the only way its row
  // (and a finished script's `skipped` record) can appear. Only for tasks that
  // actually have a script. See startScriptPoll.
  syncScriptPoll()
}, { immediate: true })

// A task's script can be added/removed by an edit while its detail is open.
watch(() => props.task?.script, () => {
  if (props.task?.id) syncScriptPoll()
})

// Subscribe to this task's updates. A `running` event is emitted by the
// scheduler's OnStarted hook, so a run started after mount appears without
// polling.
unsubscribeTaskUpdate = onEvent((event, data) => {
  if (event !== 'task_update') return
  // props.task.id is a number; the server sends task_id as a string.
  if (String(data?.task_id ?? '') !== String(props.task?.id ?? '')) return
  scheduleRunningStatusSync()
})

// WS reconnect is the sole state-sync trigger (see useGlobalEvents): resync the
// running list so a run that started while disconnected is not missing.
window.addEventListener('clawbench-reconnect', scheduleRunningStatusSync)

// Re-setup observer when the external scroll root becomes available.
// Note: this relies on the scroll root (TaskDetailPage.detail-scroll) staying
// mounted while the task detail view is active. If a future refactor unmounts
// that container, the observer would need re-wiring here.
watch(() => props.scrollRoot?.value, () => {
  if (props.task?.id) {
    nextTick(setupObserver)
  }
})

onUnmounted(() => {
  if (unsubscribeTaskUpdate) {
    unsubscribeTaskUpdate()
    unsubscribeTaskUpdate = null
  }
  if (resyncTimer) {
    clearTimeout(resyncTimer)
    resyncTimer = null
  }
  window.removeEventListener('clawbench-reconnect', scheduleRunningStatusSync)
  stopElapsedTicker()
  stopScriptPoll()
  onTaskChange() // Abort in-flight requests (ISS-016)
  if (observer) {
    observer.disconnect()
    observer = null
  }
})

// Expose clear-all and history-state to the parent (TaskDetailPage) so the
// "Clear all" button can live in the history section header bar.
// hasExecutions is a computed ref so the parent's v-if stays reactive.
const hasExecutions = computed(() => allExecutions.value.length > 0)

/** Reload execution history + running status (used by the page refresh button) */
async function reload() {
  await loadExecutions()
  loadRunningStatus()
}

defineExpose({
  deleteAllExecutions,
  hasExecutions,
  reload,
})
</script>

<style scoped>
/* Merged view: content flows into the parent scroll container, no own scrolling */
.task-history-tab {
  min-height: 0;
}

/* ── Empty state ── */
.history-empty {
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: var(--space-6);
  padding: 24px 0;
  color: var(--text-muted, #999);
  font-size: var(--font-size-lg);
}

.empty-icon {
  opacity: var(--opacity-muted);
}

/* ── Execution items ── */
.history-list {
  display: flex;
  flex-direction: column;
  gap: var(--space-5);
}

.execution-item {
  background: var(--bg-secondary, #f8f9fa);
  border: 1px solid var(--border-color, #e5e5e5);
  border-radius: 0;
  overflow: hidden;
  transition: all var(--duration-slow) ease;
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
  background: color-mix(in srgb, var(--color-green) 5%, var(--bg-secondary, #f8f9fa));
  border-color: color-mix(in srgb, var(--color-green) 30%, transparent);
  animation: exec-card-running 2s ease-in-out infinite;
}

@keyframes exec-card-running {
  0%, 100% { border-color: color-mix(in srgb, var(--color-green) 30%, transparent); }
  50% { border-color: color-mix(in srgb, var(--color-green) 55%, transparent); }
}

.execution-row {
  display: flex;
  align-items: center;
  gap: var(--space-3);
  padding: var(--space-5) var(--space-6);
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
  gap: var(--space-3);
}

.execution-time-row {
  display: flex;
  align-items: center;
  gap: var(--space-4);
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
  font-size: var(--font-size-2xs);
  padding: var(--space-1) var(--space-3);
  border-radius: var(--radius-xs);
  font-weight: var(--font-weight-semibold);
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
  font-size: var(--font-size-2xs);
  padding: var(--space-1) var(--space-3);
  border-radius: var(--radius-xs);
  font-weight: var(--font-weight-semibold);
  text-transform: uppercase;
  letter-spacing: 0.02em;
  flex-shrink: 0;
  display: inline-flex;
  align-items: center;
  gap: var(--space-2);
  white-space: nowrap;
}
.exec-status-badge.running {
  background: rgba(34, 197, 94, 0.12);
  color: #16a34a;
}
.exec-status-badge.cancelled {
  background: var(--bg-tertiary, #e5e7eb);
  color: var(--text-secondary, #4b5563);
}
/* A skip is a deliberate no-op (the script found nothing to do), not a
   failure and not a cancellation — a neutral tint, distinct from both. */
.exec-status-badge.skipped {
  background: rgba(100, 116, 139, 0.12);
  color: #64748b;
}
.exec-status-badge.failed {
  background: rgba(239, 68, 68, 0.12);
  color: #dc2626;
}

/* ── Start time (top row, before the right-aligned duration) ── */
.exec-start-time {
  font-size: var(--font-size-xs);
  color: var(--text-muted, #9ca3af);
  white-space: nowrap;
  font-variant-numeric: tabular-nums;
  flex-shrink: 0;
}

/* ── Duration (top row, right-aligned next to trigger type) ── */
.exec-duration {
  font-size: var(--font-size-xs);
  font-weight: var(--font-weight-semibold);
  color: var(--text-primary, #111827);
  background: rgba(0, 102, 204, 0.05);
  padding: var(--space-1) var(--space-3);
  border-radius: var(--radius-xs);
  white-space: nowrap;
  font-variant-numeric: tabular-nums;
  flex-shrink: 0;
  margin-left: auto;
}

/* ── Summary ── */
.exec-summary-row {
  display: flex;
  align-items: center;
}

/* ── Event source (event-triggered runs) ── */
.exec-event-row {
  display: flex;
  align-items: center;
  gap: var(--space-2);
  min-width: 0;
}

.exec-event-icon {
  color: var(--text-muted, #9ca3af);
  flex-shrink: 0;
}

.exec-event-source {
  font-size: var(--font-size-xs);
  color: var(--text-secondary, #4b5563);
  background: var(--bg-primary, #fff);
  border: 1px solid var(--border-color, #e5e7eb);
  border-radius: var(--radius-xs);
  padding:1px var(--space-3);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  max-width: 100%;
  font-family: var(--font-mono);
}

.exec-summary {
  font-size: var(--font-size-md);
  color: var(--text-secondary, #4b5563);
  line-height: var(--line-height-snug);
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
  gap: var(--space-4);
  flex-wrap: wrap;
  margin-top: var(--space-1);
}

.exec-meta-tag {
  font-size: var(--font-size-xs);
  padding: var(--space-1) var(--space-3);
  border-radius: var(--radius-xs);
  background: var(--bg-primary, #ffffff);
  border: 1px solid var(--border-color, #e5e7eb);
  color: var(--text-secondary, #6b7280);
  white-space: nowrap;
  font-variant-numeric: tabular-nums;
  display: inline-flex;
  align-items: center;
}

/* ── Running execution indicator (inside status badge) ── */
.exec-running-dot {
  width: 6px;
  height: 6px;
  border-radius: 50%;
  background: #16a34a;
  flex-shrink: 0;
  animation: exec-running-pulse 0.8s ease-in-out infinite;
}

@keyframes exec-running-pulse {
  0%, 100% { opacity: 1; box-shadow: 0 0 0 0 rgba(22, 163, 74, 0.5); }
  50% { opacity: var(--opacity-soft); box-shadow: 0 0 6px 3px rgba(22, 163, 74, 0.3); }
}

/* ── Just-completed execution flash ── */
.execution-item.just-completed {
  animation: exec-just-completed 0.6s ease-out forwards;
}

@keyframes exec-just-completed {
  0% { background: color-mix(in srgb, var(--accent-color, #0066cc) 15%, var(--bg-secondary, #f8f9fa)); transform: translateX(8px); opacity: var(--opacity-soft); }
  100% { background: var(--bg-secondary, #f8f9fa); transform: translateX(0); opacity: 1; }
}

/* ── Cancel button ── */
.cancel-exec-btn {
  width: 32px;
  height: 32px;
  border: none;
  background: rgba(239, 68, 68, 0.1);
  color: #ef4444;
  border-radius: var(--radius-sm);
  cursor: pointer;
  display: flex;
  align-items: center;
  justify-content: center;
  flex-shrink: 0;
  transition: all var(--duration-slow);
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
  border-radius: var(--radius-sm);
  cursor: pointer;
  display: flex;
  align-items: center;
  justify-content: center;
  flex-shrink: 0;
  transition: all var(--duration-slow);
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
    opacity: var(--opacity-muted);
  }
}

.delete-exec-btn:active {
  transform: scale(0.9);
}

/* ── Infinite scroll sentinel ── */
.history-list-sentinel {
  height: 1px;
}

.history-loading-more {
  display: flex;
  align-items: center;
  justify-content: center;
  gap: var(--space-3);
  padding: var(--space-4);
  color: var(--text-muted, #9ca3af);
  font-size: var(--font-size-sm);
}
</style>
