<template>
  <div class="task-detail-page">
    <!-- Compact header: breadcrumb + refresh button -->
    <div class="detail-header">
      <TaskBreadcrumb />
      <RefreshButton class="header-btn refresh-btn" :loading="refreshing" :disabled="refreshing" :title="t('common.refresh')" @click="onRefresh" />
    </div>
    <!-- Unified scrollable content: task overview + execution history in one scroll container -->
    <div ref="scrollRef" class="detail-scroll">
      <TaskOverviewTab :task="task" />
      <div class="history-section">
        <div class="history-section-title">
          <History :size="14" />
          <span>{{ t('task.exec.title') }}</span>
          <div class="history-section-actions">
            <!-- Disabled rather than hidden when nothing is unread: the pair
                 shares one row, so hiding it would shift "Clear all" sideways
                 the moment the user clicks it. -->
            <button v-if="historyTabRef?.hasExecutions" class="clear-all-btn mark-read-btn" :disabled="!historyTabRef?.hasUnread" @click="onMarkAllRead" :title="t('task.exec.markAllRead')">
              <CheckCheck :size="12" />
              <span>{{ t('task.exec.markAllRead') }}</span>
            </button>
            <button v-if="historyTabRef?.hasExecutions" class="clear-all-btn" @click="onClearAll" :title="t('task.exec.clearAll')">
              <Trash2 :size="12" />
              <span>{{ t('task.exec.clearAll') }}</span>
            </button>
          </div>
        </div>
        <TaskHistoryTab ref="historyTabRef" :task="task" :scroll-root="scrollRef" />
      </div>
    </div>
    <!-- Fixed bottom action bar (moved up from TaskOverviewTab) -->
    <div class="detail-actions">
      <button class="fbtn" @click="$emit('edit')" :title="t('common.edit')">
        <Pencil :size="14" />
        <span class="action-text">{{ t('common.edit') }}</span>
      </button>

      <template v-if="taskStatus === 'active'">
        <!-- While a run is in flight the manual-run button is replaced by a
             terminate button, for every task including event tasks. A disabled
             "Run" reads as "you cannot do anything", but the user's intent when
             a task is running is to stop it — offering that directly is the
             conventional affordance. An event task has no manual run to offer,
             but it can still be running (an event fired), so it gets the
             terminate button too. Gated on isTaskRunning (not runningCount) so
             the pre-AI script phase — which runningCount excludes — also gets a
             terminate button instead of a Run button that only 409s. -->
        <button v-if="isTaskRunning" class="fbtn fbtn-danger" :disabled="actionLoading" @click="stopTask" :title="t('task.stop')">
          <Square :size="14" />
          <span class="action-text">{{ t('task.stop') }}</span>
        </button>
        <!-- An event task has no event to inject on a manual run, so its
             {{TITLE}}/{{URL}} placeholders would go unsubstituted. The backend
             rejects such a trigger (409); hide the button rather than offer an
             action that can only fail. -->
        <button v-else-if="!isEventTriggered" class="fbtn fbtn-primary" :disabled="actionLoading" @click="triggerTask" :title="t('task.run')">
          <Zap :size="14" />
          <span class="action-text">{{ t('task.run') }}</span>
        </button>
        <button class="fbtn fbtn-warn" :disabled="actionLoading" @click="pauseTask" :title="t('task.pause')">
          <Pause :size="14" />
          <span class="action-text">{{ t('task.pause') }}</span>
        </button>
        <button class="fbtn fbtn-danger" :disabled="actionLoading" @click="deleteTask" :title="t('task.delete')">
          <Trash2 :size="14" />
          <span class="action-text">{{ t('task.delete') }}</span>
        </button>
      </template>
      <template v-else-if="taskStatus === 'paused'">
        <!-- A task can be paused while a run is still in flight, so the
             terminate button applies here too. -->
        <button v-if="isTaskRunning" class="fbtn fbtn-danger" :disabled="actionLoading" @click="stopTask" :title="t('task.stop')">
          <Square :size="14" />
          <span class="action-text">{{ t('task.stop') }}</span>
        </button>
        <button v-else-if="!isEventTriggered" class="fbtn fbtn-primary" :disabled="actionLoading" @click="triggerTask" :title="t('task.run')">
          <Zap :size="14" />
          <span class="action-text">{{ t('task.run') }}</span>
        </button>
        <button class="fbtn fbtn-success" :disabled="actionLoading" @click="resumeTask" :title="t('task.resume')">
          <Power :size="14" />
          <span class="action-text">{{ t('task.resume') }}</span>
        </button>
        <button class="fbtn fbtn-danger" :disabled="actionLoading" @click="deleteTask" :title="t('task.delete')">
          <Trash2 :size="14" />
          <span class="action-text">{{ t('task.delete') }}</span>
        </button>
      </template>
      <template v-else-if="taskStatus === 'completed'">
        <button class="fbtn fbtn-danger" :disabled="actionLoading" @click="deleteTask" :title="t('task.delete')">
          <Trash2 :size="14" />
          <span class="action-text">{{ t('task.delete') }}</span>
        </button>
      </template>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, computed } from 'vue'
import { History, Pencil, Pause, Power, Square, Zap, Trash2, CheckCheck } from 'lucide-vue-next'
import { useI18n } from 'vue-i18n'
import TaskBreadcrumb from '@/components/task/TaskBreadcrumb.vue'
import RefreshButton from '@/components/common/RefreshButton.vue'
import TaskOverviewTab from '@/components/task/TaskOverviewTab.vue'
import TaskHistoryTab from '@/components/task/TaskHistoryTab.vue'
import { useTaskTab } from '@/composables/useTaskTab'
import { useTaskOverview } from '@/composables/useTaskOverview.ts'
import '@/assets/modal-footer-btn.css'

const { t } = useI18n()
const { loadTasks } = useTaskTab()

const props = defineProps<{
  task: Record<string, unknown>
}>()

const emit = defineEmits<{
  edit: []
  deleted: []
}>()

// Task actions (moved up from TaskOverviewTab so the action bar can be
// fixed at the bottom of the unified scroll container)
const { actionLoading, triggerTask, stopTask, pauseTask, resumeTask, deleteTask } = useTaskOverview({
  task: computed(() => props.task),
  emit: {
    deleted: () => emit('deleted'),
    edit: () => emit('edit'),
  },
})

const taskStatus = computed(() => props.task.status as string)
const taskRunningCount = computed(() => props.task.runningCount as number)

// An absent triggerMode is the cron default (tasks created before event mode
// existed carry no field). Same reading as TaskListPage / TaskOverviewTab.
const isEventTriggered = computed(() => (props.task.triggerMode as string) === 'event')

const refreshing = ref(false)
const scrollRef = ref<HTMLElement | null>(null)
const historyTabRef = ref<InstanceType<typeof TaskHistoryTab> | null>(null)

// Whether a run is currently in flight, driving the Terminate button.
//
// Two sources, OR'd: the task list's runningCount is available immediately and
// covers the AI phase; the history tab's hasAnyRunning additionally covers the
// pre-AI gating-script phase, which runningCount deliberately excludes (so a
// script's start/stop cannot fire a false "completed" toast). Without the
// second source a script-phase task showed a Run button that only 409'd.
const isTaskRunning = computed(() =>
  taskRunningCount.value > 0 || (historyTabRef.value?.hasAnyRunning ?? false),
)

function onClearAll() {
  historyTabRef.value?.deleteAllExecutions()
}

function onMarkAllRead() {
  historyTabRef.value?.markAllRead()
}

async function onRefresh() {
  if (refreshing.value) return
  refreshing.value = true
  try {
    // Minimum spin duration so the refresh animation is always visible,
    // even when the API responds almost instantly.
    await Promise.all([
      loadTasks().then(() => historyTabRef.value?.reload()),
      new Promise(resolve => setTimeout(resolve, 600)),
    ])
  } finally {
    refreshing.value = false
  }
}
</script>

<style scoped>
.task-detail-page {
  display: flex;
  flex-direction: column;
  height: 100%;
  overflow: hidden;
  background: var(--bg-primary, #ffffff);
}

.detail-header {
  display: flex;
  align-items: center;
  height: var(--header-height);
  padding:0 var(--space-2) 0 var(--space-6);
  flex-shrink: 0;
  background: var(--bg-primary);
  border-bottom: 1px solid var(--border-color, #e5e5e5);
  gap: var(--space-3);
}

.header-btn {
  width: 28px;
  height: 28px;
  border: none;
  border-radius: var(--radius-lg);
  background: var(--bg-secondary, #f1f3f5);
  color: var(--text-secondary, #666);
  cursor: pointer;
  display: flex;
  align-items: center;
  justify-content: center;
  flex-shrink: 0;
  transition: all var(--duration-slow) ease;
}

.header-btn:disabled {
  opacity: var(--opacity-muted);
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

/* Single scroll container: overview + history flow together */
.detail-scroll {
  flex: 1;
  overflow-y: auto;
  min-height: 0;
  display: flex;
  flex-direction: column;
}

.history-section {
  display: flex;
  flex-direction: column;
  border-top: 1px solid var(--border-color, #e5e5e5);
  padding: var(--space-4);
  gap: var(--space-3);
}

.history-section-title {
  display: flex;
  align-items: center;
  gap: var(--space-3);
  font-size: var(--font-size-md);
  font-weight: var(--font-weight-semibold);
  color: var(--text-primary, #1a1a1a);
  padding: var(--space-1) 0;
  flex-shrink: 0;
}

/* Header action cluster: "mark all read" sits left of "clear all", both
   right-aligned against the section title. The margin lives on the cluster
   (not the buttons) so the pair stays together. */
.history-section-actions {
  margin-left: auto;
  display: inline-flex;
  align-items: center;
  gap: var(--space-1);
}

.clear-all-btn {
  display: inline-flex;
  align-items: center;
  gap: var(--space-2);
  border: none;
  background: transparent;
  color: var(--text-muted, #9ca3af);
  font-size: var(--font-size-sm);
  font-weight: var(--font-weight-medium);
  cursor: pointer;
  padding: var(--space-2) var(--space-4);
  border-radius: var(--radius-sm);
  transition: all var(--duration-slow);
}

.clear-all-btn:disabled {
  opacity: var(--opacity-muted);
  cursor: not-allowed;
}

@media (hover: hover) {
  .clear-all-btn:not(:disabled):hover {
    color: #ef4444;
    background: rgba(239, 68, 68, 0.06);
  }
}

/* Mark-all-read is not destructive, so it must not borrow Clear all's red
   hover — that colour is the signal for "this removes data". The selector
   repeats both classes to match `.clear-all-btn:not(:disabled):hover`'s
   specificity; at equal specificity the later rule wins. */
@media (hover: hover) {
  .clear-all-btn.mark-read-btn:not(:disabled):hover {
    color: var(--accent-color, #0066cc);
    background: color-mix(in srgb, var(--accent-color, #0066cc) 8%, transparent);
  }
}

.clear-all-btn:active {
  transform: scale(0.95);
}

/* Fixed bottom action bar */
.detail-actions {
  display: flex;
  align-items: center;
  gap: var(--space-3);
  padding: var(--space-3) var(--space-4);
  background: var(--bg-primary, #ffffff);
  border-top: 1px solid var(--border-color, #e5e5e5);
  flex-shrink: 0;
  overflow-x: auto;
}

.action-text {
  line-height: 1;
}
</style>
