<template>
  <div class="task-list-page">
    <!-- Compact header: breadcrumb + refresh + create button -->
    <div class="list-header">
      <TaskBreadcrumb />
      <button class="header-btn refresh-btn" :class="{ spinning: loading }" :disabled="loading" @click="refresh" :title="t('common.refresh')">
        <RefreshCw :size="14" />
      </button>
      <button class="header-btn clear-unread-btn" :class="{ active: hasUnread }" :disabled="!hasUnread" @click="markAllTasksRead" :title="t('task.clearUnread')">
        <CheckCheck :size="14" />
      </button>
      <button class="create-btn" @click="$emit('create')" :title="t('task.form.createTitle')">
        <Plus :size="16" />
      </button>
    </div>
    <div class="task-list-body">
      <div v-if="loading && tasks.length === 0" class="task-loading">
        <Loader2 class="loading-icon" :size="20" />
        <span>{{ t('common.loading') }}</span>
      </div>
      <div v-else-if="tasks.length === 0" class="task-empty">
        <CalendarX class="empty-icon" :size="32" />
        <span>{{ t('task.noTasks') }}</span>
      </div>
      <div v-else class="task-items-container">
        <div
          v-for="task in tasks"
          :key="task.id"
          class="task-item"
          :class="[task.status, { 'has-unread': task.unreadCount > 0, 'is-running': task.runningCount > 0 }]"
          @click="$emit('select', task.id)"
        >
          <div class="task-item-main">
            <div class="task-item-header">
              <AgentIcon class="task-item-icon" :backend="getAgentBackend(task.agentId)" :name="getAgentName(task.agentId)" :size="16" />
              <span class="task-item-name">{{ task.name }}</span>
              <span v-if="task.runningCount > 0" class="task-item-running-dot" :title="t('task.exec.running')"></span>
              <span v-if="task.unreadCount > 0" class="task-item-unread">{{ task.unreadCount }}</span>
            </div>
            <div class="task-item-meta">
              <div class="meta-item cron" :title="task.cronExpr">
                <Clock class="meta-icon" :size="12" />
                <span>{{ humanizeCron(task.cronExpr) }}</span>
              </div>
              <div class="meta-item repeat">
                <Repeat class="meta-icon" :size="12" />
                <span>{{ repeatLabel(task.repeatMode, task.maxRuns) }}</span>
                <span v-if="task.repeatMode !== 'unlimited'" class="task-progress">({{ task.runCount }}/{{ task.maxRuns || 1 }})</span>
              </div>
            </div>
            <div v-if="task.nextRunAt" class="task-item-next">
              <Clock class="meta-icon" :size="12" />
              <span>{{ t('task.nextRun', { time: formatDateTime(task.nextRunAt) }) }}</span>
            </div>
          </div>
          <div class="task-item-right">
            <span class="task-item-status" :class="task.status">{{ statusLabel(task.status) }}</span>
            <button
              v-if="task.runCount > 0 || task.runningCount > 0"
              class="task-item-history-btn"
              :class="{ 'has-unread-flash': task.unreadCount > 0 }"
              @click.stop="$emit('latestExec', task.id)"
              :title="t('task.viewLatest')"
            >
              <Eye :size="16" />
            </button>
          </div>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { Plus, Loader2, CalendarX, Clock, Repeat, Eye, RefreshCw, CheckCheck } from 'lucide-vue-next'
import { ref, computed, onMounted } from 'vue'
import { useI18n } from 'vue-i18n'
import { useTaskTab } from '@/composables/useTaskTab'
import { useAgents } from '@/composables/useAgents'
import { humanizeCron, repeatLabel, statusLabel, formatDateTime } from '@/utils/format'
import { store } from '@/stores/app'
import TaskBreadcrumb from '@/components/task/TaskBreadcrumb.vue'
import AgentIcon from '@/components/common/AgentIcon.vue'

const { t } = useI18n()
const { loadTasks, markAllTasksRead } = useTaskTab()
const { loadAgents, getAgentBackend, getAgentName } = useAgents()

interface TaskItem {
  id: number
  name: string
  agentId: string
  status: string
  cronExpr: string
  repeatMode: string
  maxRuns: number
  runCount: number
  runningCount: number
  unreadCount: number
  nextRunAt?: string
}

const tasks = computed(() => store.state.tasks as unknown as TaskItem[])
const hasUnread = computed(() => store.state.taskUnreadCount > 0)
const loading = ref(false)

defineEmits<{
  create: []
  select: [taskId: number]
  latestExec: [taskId: number]
}>()

async function refresh() {
  loading.value = true
  try {
    await Promise.all([loadTasks(), loadAgents()])
  } finally {
    loading.value = false
  }
}

defineExpose({ refresh })

onMounted(refresh)
</script>

<style scoped>
.task-list-page {
  height: 100%;
  display: flex;
  flex-direction: column;
  overflow: hidden;
  background: var(--bg-primary, #ffffff);
}

/* Compact header — matches detail/form/history pages */
.list-header {
  display: flex;
  align-items: center;
  padding: 4px 8px;
  flex-shrink: 0;
  border-bottom: 1px solid var(--border-color, #e5e5e5);
  gap: 6px;
}

/* Create button in header toolbar */
.create-btn {
  width: 28px;
  height: 28px;
  border: none;
  border-radius: 14px;
  background: var(--accent-color, #0066cc);
  color: #fff;
  cursor: pointer;
  display: flex;
  align-items: center;
  justify-content: center;
  flex-shrink: 0;
  transition: all 0.2s ease;
}

/* Header icon button (refresh, etc.) */
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

/* Clear-unread button accent when unread messages exist */
.clear-unread-btn.active {
  color: var(--accent-color, #0066cc);
  background: color-mix(in srgb, var(--accent-color, #0066cc) 10%, var(--bg-secondary, #f1f3f5));
}

.header-btn.spinning svg {
  animation: spin 1s linear infinite;
}

@media (hover: hover) {
  .create-btn:hover {
    background: color-mix(in srgb, var(--accent-color, #0066cc) 85%, black);
    transform: translateY(-1px);
  }
}

.create-btn:active {
  transform: scale(0.9);
}

.task-list-body {
  flex: 1;
  overflow-y: auto;
  padding: 8px;
}

.task-items-container {
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.task-loading,
.task-empty {
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 12px;
  height: 100%;
  color: var(--text-muted, #999);
  font-size: 14px;
}

.loading-icon {
  animation: spin 1s linear infinite;
}

@keyframes spin {
  100% { transform: rotate(360deg); }
}

.empty-icon {
  opacity: 0.5;
}

.task-item {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 10px;
  background: var(--bg-secondary, #f8f9fa);
  border: 1px solid var(--border-color, #e5e5e5);
  border-radius: 0;
  cursor: pointer;
  transition: all 0.2s ease;
  position: relative;
  overflow: hidden;
}

@media (hover: hover) {
  .task-item:hover {
    border-color: var(--accent-color, #0066cc);
    box-shadow: 0 2px 8px rgba(0, 0, 0, 0.04);
  }
}

.task-item:active {
  background: var(--bg-tertiary, #eef1f4);
  transform: translateY(0);
}

.task-item.completed {
  opacity: 0.65;
  background: var(--bg-tertiary, #f1f3f5);
}

.task-item-main {
  flex: 1;
  display: flex;
  flex-direction: column;
  gap: 6px;
  min-width: 0;
}

.task-item-header {
  display: flex;
  align-items: center;
  gap: 6px;
  min-width: 0;
}

.task-item-icon {
  flex-shrink: 0;
  vertical-align: middle;
}

.task-item-name {
  font-size: 14px;
  font-weight: 600;
  color: var(--text-primary, #1a1a1a);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  flex: 1;
  min-width: 0;
}

.task-item-unread {
  font-size: 10px;
  padding: 2px 6px;
  border-radius: 10px;
  font-weight: 600;
  background: var(--accent-color, #0066cc);
  color: #fff;
  flex-shrink: 0;
  min-width: 16px;
  text-align: center;
  line-height: 1.2;
}

.task-item.has-unread {
  border-left: 3px solid var(--accent-color, #0066cc);
}

.task-item.has-unread .task-item-icon {
  /* static accent highlight, no animation */
  filter: drop-shadow(0 0 3px color-mix(in srgb, var(--accent-color, #0066cc) 40%, transparent));
}

/* When both unread and running, keep the unread left border + icon highlight
 * but also apply running border pulse via box-shadow to avoid animation conflict */
.task-item.has-unread.is-running {
  border-left: 3px solid var(--accent-color, #0066cc);
  animation: task-card-running 2s ease-in-out infinite;
}

.task-item.has-unread.is-running .task-item-icon {
  filter: drop-shadow(0 0 3px color-mix(in srgb, var(--accent-color, #0066cc) 40%, transparent));
}

.task-item-status {
  font-size: 10px;
  padding: 3px 6px;
  border-radius: 4px;
  font-weight: 600;
  flex-shrink: 0;
  text-transform: uppercase;
  letter-spacing: 0.02em;
}

.task-item-status.active {
  background: rgba(34, 197, 94, 0.12);
  color: #16a34a;
}

.task-item-status.paused {
  background: rgba(234, 179, 8, 0.12);
  color: #ca8a04;
}

.task-item-status.completed {
  background: rgba(156, 163, 175, 0.15);
  color: #6b7280;
}

.task-item-running-dot {
  width: 10px;
  height: 10px;
  border-radius: 50%;
  background: #22c55e;
  flex-shrink: 0;
  animation: task-running-pulse 0.8s ease-in-out infinite;
}

@keyframes task-running-pulse {
  0%, 100% { opacity: 1; box-shadow: 0 0 0 0 rgba(34, 197, 94, 0.5); }
  50% { opacity: 0.7; box-shadow: 0 0 10px 4px rgba(34, 197, 94, 0.3); }
}

.task-item.is-running {
  background: rgba(34, 197, 94, 0.05);
  animation: task-card-running 2s ease-in-out infinite;
}

@keyframes task-card-running {
  0%, 100% { border-color: var(--border-color, #e5e5e5); }
  50% { border-color: rgba(34, 197, 94, 0.35); }
}

.task-item-meta {
  display: flex;
  align-items: center;
  gap: 8px;
  font-size: 12px;
  color: var(--text-secondary, #666);
  min-width: 0;
  flex-wrap: wrap;
}

.meta-item {
  display: flex;
  align-items: center;
  gap: 4px;
}

.meta-icon {
  color: var(--text-muted, #999);
}

.cron span {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  max-width: 140px;
}

.task-progress {
  color: var(--accent-color, #0066cc);
  font-weight: 500;
  margin-left: 2px;
}

.task-item-next {
  display: flex;
  align-items: center;
  gap: 4px;
  font-size: 11px;
  color: var(--text-muted, #999);
  background: var(--bg-primary, #fff);
  padding: 4px 8px;
  border-radius: 4px;
  border: 1px solid var(--border-color, #e5e5e5);
  width: fit-content;
}

.task-item-right {
  display: flex;
  flex-direction: column;
  align-items: flex-end;
  gap: 6px;
  flex-shrink: 0;
  align-self: flex-start;
  margin-top: 2px;
  margin-left: 10px;
}

.task-item-history-btn {
  width: 34px;
  height: 34px;
  border: none;
  border-radius: 17px;
  background: var(--bg-tertiary, #eef1f4);
  color: var(--text-secondary, #666);
  cursor: pointer;
  display: flex;
  align-items: center;
  justify-content: center;
  flex-shrink: 0;
  transition: all 0.2s ease;
  box-shadow: 0 1px 3px rgba(0, 0, 0, 0.06);
}

@media (hover: hover) {
  .task-item-history-btn:hover {
    background: var(--accent-color, #0066cc);
    color: #fff;
    box-shadow: 0 2px 8px rgba(0, 102, 204, 0.3);
    transform: translateY(-1px);
  }
}

.task-item-history-btn:active {
  transform: scale(0.9);
  background: var(--border-color, #e5e5e5);
}

/* Static indicator for history button when task has unread messages */
.task-item-history-btn.has-unread-flash {
  color: var(--accent-color, #0066cc);
  background: color-mix(in srgb, var(--accent-color, #0066cc) 12%, var(--bg-tertiary, #eef1f4));
}
</style>
