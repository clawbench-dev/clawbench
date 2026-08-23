<template>
  <div class="task-detail-page">
    <!-- Compact header: breadcrumb + refresh button -->
    <div class="detail-header">
      <TaskBreadcrumb />
      <button class="header-btn refresh-btn" :class="{ spinning: refreshing }" :disabled="refreshing" @click="onRefresh" :title="t('common.refresh')">
        <RefreshCw :size="14" />
      </button>
    </div>
    <!-- Unified scrollable content: task overview + execution history in one scroll container -->
    <div ref="scrollRef" class="detail-scroll">
      <TaskOverviewTab :task="task" />
      <div class="history-section">
        <div class="history-section-title">
          <History :size="14" />
          <span>{{ t('task.exec.title') }}</span>
          <button v-if="historyTabRef?.hasExecutions" class="clear-all-btn" @click="onClearAll" :title="t('task.exec.clearAll')">
            <Trash2 :size="12" />
            <span>{{ t('task.exec.clearAll') }}</span>
          </button>
        </div>
        <TaskHistoryTab ref="historyTabRef" :task="task" :scroll-root="scrollRef" />
      </div>
    </div>
    <!-- Fixed bottom action bar (moved up from TaskOverviewTab) -->
    <div class="detail-actions">
      <button class="action-btn" @click="$emit('edit')" :title="t('common.edit')">
        <Pencil :size="14" />
        <span class="action-text">{{ t('common.edit') }}</span>
      </button>

      <template v-if="taskStatus === 'active'">
        <button class="action-btn accent" :disabled="actionLoading || taskRunningCount > 0" @click="triggerTask" :title="taskRunningCount > 0 ? t('chat.contentBlocks.statusRunning') : t('task.run')">
          <Zap :size="14" />
          <span class="action-text">{{ t('task.run') }}</span>
        </button>
        <button class="action-btn warn" :disabled="actionLoading" @click="pauseTask" :title="t('task.pause')">
          <Pause :size="14" />
          <span class="action-text">{{ t('task.pause') }}</span>
        </button>
        <button class="action-btn danger" :disabled="actionLoading" @click="deleteTask" :title="t('task.delete')">
          <Trash2 :size="14" />
          <span class="action-text">{{ t('task.delete') }}</span>
        </button>
      </template>
      <template v-else-if="taskStatus === 'paused'">
        <button class="action-btn accent" :disabled="actionLoading || taskRunningCount > 0" @click="triggerTask" :title="taskRunningCount > 0 ? t('chat.contentBlocks.statusRunning') : t('task.run')">
          <Zap :size="14" />
          <span class="action-text">{{ t('task.run') }}</span>
        </button>
        <button class="action-btn success" :disabled="actionLoading" @click="resumeTask" :title="t('task.resume')">
          <Power :size="14" />
          <span class="action-text">{{ t('task.resume') }}</span>
        </button>
        <button class="action-btn danger" :disabled="actionLoading" @click="deleteTask" :title="t('task.delete')">
          <Trash2 :size="14" />
          <span class="action-text">{{ t('task.delete') }}</span>
        </button>
      </template>
      <template v-else-if="taskStatus === 'completed'">
        <button class="action-btn danger" :disabled="actionLoading" @click="deleteTask" :title="t('task.delete')">
          <Trash2 :size="14" />
          <span class="action-text">{{ t('task.delete') }}</span>
        </button>
      </template>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, computed } from 'vue'
import { RefreshCw, History, Pencil, Pause, Power, Zap, Trash2 } from 'lucide-vue-next'
import { useI18n } from 'vue-i18n'
import TaskBreadcrumb from '@/components/task/TaskBreadcrumb.vue'
import TaskOverviewTab from '@/components/task/TaskOverviewTab.vue'
import TaskHistoryTab from '@/components/task/TaskHistoryTab.vue'
import { useTaskTab } from '@/composables/useTaskTab'
import { useTaskOverview } from '@/composables/useTaskOverview.ts'

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
const { actionLoading, triggerTask, pauseTask, resumeTask, deleteTask } = useTaskOverview({
  task: computed(() => props.task),
  emit: {
    deleted: () => emit('deleted'),
    edit: () => emit('edit'),
  },
})

const taskStatus = computed(() => props.task.status as string)
const taskRunningCount = computed(() => props.task.runningCount as number)

const refreshing = ref(false)
const scrollRef = ref<HTMLElement | null>(null)
const historyTabRef = ref<InstanceType<typeof TaskHistoryTab> | null>(null)

function onClearAll() {
  historyTabRef.value?.deleteAllExecutions()
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

@keyframes spin {
  100% { transform: rotate(360deg); }
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
  padding: 8px;
  gap: 6px;
}

.history-section-title {
  display: flex;
  align-items: center;
  gap: 6px;
  font-size: 13px;
  font-weight: 600;
  color: var(--text-primary, #1a1a1a);
  padding: 2px 0;
  flex-shrink: 0;
}

.clear-all-btn {
  margin-left: auto;
  display: inline-flex;
  align-items: center;
  gap: 4px;
  border: none;
  background: transparent;
  color: var(--text-muted, #9ca3af);
  font-size: 12px;
  font-weight: 500;
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

/* Fixed bottom action bar */
.detail-actions {
  display: flex;
  align-items: center;
  gap: 6px;
  padding: 6px 8px;
  background: var(--bg-primary, #ffffff);
  border-top: 1px solid var(--border-color, #e5e5e5);
  flex-shrink: 0;
  overflow-x: auto;
}

.action-btn {
  height: 28px;
  border: none;
  border-radius: 14px;
  background: var(--bg-secondary, #f1f3f5);
  color: var(--text-primary, #1a1a1a);
  cursor: pointer;
  transition: all 0.2s ease;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  gap: 4px;
  padding: 0 10px;
  flex-shrink: 0;
  font-size: 12px;
  font-weight: 500;
  white-space: nowrap;
}

.action-text {
  line-height: 1;
}

.action-btn:disabled {
  opacity: 0.5;
  cursor: not-allowed;
}

@media (hover: hover) {
  .action-btn:hover:not(:disabled) {
    background: var(--border-color, #e5e5e5);
    transform: translateY(-1px);
  }
}

.action-btn:active:not(:disabled) {
  transform: scale(0.96);
}

.action-btn.accent {
  background: var(--accent-color, #0066cc);
  color: #fff;
}

@media (hover: hover) {
  .action-btn.accent:hover:not(:disabled) {
    background: color-mix(in srgb, var(--accent-color, #0066cc) 85%, black);
    color: #fff;
  }
}

.action-btn.warn {
  background: color-mix(in srgb, #ca8a04 15%, var(--bg-secondary, #f1f3f5));
  color: #ca8a04;
}

@media (hover: hover) {
  .action-btn.warn:hover:not(:disabled) {
    background: color-mix(in srgb, #ca8a04 30%, var(--bg-secondary, #f1f3f5));
  }
}

.action-btn.success {
  background: color-mix(in srgb, #16a34a 15%, var(--bg-secondary, #f1f3f5));
  color: #16a34a;
}

@media (hover: hover) {
  .action-btn.success:hover:not(:disabled) {
    background: color-mix(in srgb, #16a34a 30%, var(--bg-secondary, #f1f3f5));
  }
}

.action-btn.danger {
  background: color-mix(in srgb, #ef4444 10%, var(--bg-secondary, #f1f3f5));
  color: #b91c1c;
}

@media (hover: hover) {
  .action-btn.danger:hover:not(:disabled) {
    background: color-mix(in srgb, #ef4444 25%, var(--bg-secondary, #f1f3f5));
  }
}
</style>
