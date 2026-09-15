<template>
  <div class="task-list-page">
    <!-- Compact header: breadcrumb + refresh + create button -->
    <div class="list-header">
      <TaskBreadcrumb />
      <RefreshButton class="header-btn refresh-btn" :loading="loading" :disabled="loading" :title="t('common.refresh')" @click="refresh" />
      <button class="header-btn clear-unread-btn" :class="{ active: hasUnread }" :disabled="!hasUnread" @click="markAllTasksRead" :title="t('task.clearUnread')">
        <CheckCheck :size="14" />
      </button>
      <button class="create-btn" @click="$emit('create')" :title="t('task.form.createTitle')">
        <Plus :size="16" />
      </button>
    </div>
    <div class="task-list-body">
      <div v-if="loading && tasks.length === 0" class="task-loading">
        <LoadingIndicator size="sm" inline />
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
              <!-- Trigger-type badge. This is the only element that names *what*
                   starts the task: the meta line below is cron-only, and the
                   summary line only ever shows a schedule or a repository. -->
              <span
                class="task-trigger-badge"
                :class="task.triggerMode === 'event' ? 'is-event' : 'is-cron'"
              >{{ task.triggerMode === 'event' ? t('task.form.triggerEvent') : t('task.form.triggerCron') }}</span>
              <span class="task-item-name">{{ task.name }}</span>
              <span v-if="task.runningCount > 0" class="task-item-running-dot" :title="t('task.exec.running')"></span>
              <span v-if="task.unreadCount > 0" class="task-item-unread count-badge">{{ task.unreadCount }}</span>
            </div>
            <!-- Schedule + repeat. Cron-only, for two reasons:
                 - An event task has no schedule, and its repeat mode is inert —
                   the backend never exhausts it (scheduler.go's event-task
                   branch), so "不限次数" would be fabricated.
                 - Its subscription list can run to dozens of characters, which
                   the .cron span's max-width then truncated mid-word while
                   shoving the neighbouring repeat label far to the right, so
                   event rows never lined up with cron rows.
                 The subscription detail lives in the task overview. -->
            <div v-if="task.triggerMode !== 'event'" class="task-item-meta">
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
            <!-- Trigger summary line. The icon sits inside each branch because
                 the two modes mean different things: a cron task is defined by
                 *when* it runs (a clock), an event task by *what* it watches.

                 The event branch names the project's bound repository: every
                 event task in a project watches that one binding, so the row
                 states what will actually fire the task rather than repeating
                 "event-triggered", which the badge above already says.

                 The icon stays inside each branch because an unconditional
                 Clock told an event task's user it has a schedule, which it
                 never does — the backend leaves nextRunAt null for event tasks
                 (scheduler.go's event-task branch). -->
            <div v-if="task.triggerMode === 'event'" class="task-item-next">
              <GitBranch class="meta-icon" :size="12" />
              <span class="task-item-repo" :title="repoRowLabel">{{ repoRowLabel }}</span>
            </div>
            <div v-else class="task-item-next">
              <Clock class="meta-icon" :size="12" />
              <span v-if="task.nextRunAt">{{ t('task.nextRun', { time: formatDateTimeWithYear(task.nextRunAt) }) }}</span>
              <span v-else>{{ t('task.nextRunNone') }}</span>
            </div>
          </div>
          <div class="task-item-right">
            <span class="task-item-status" :class="task.status">{{ statusLabel(task.status) }}</span>
          </div>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { Plus, CalendarX, Clock, Repeat, CheckCheck, GitBranch } from 'lucide-vue-next'
import { ref, computed, onMounted } from 'vue'
import { useI18n } from 'vue-i18n'
import { useTaskTab } from '@/composables/useTaskTab'
import { useAgents } from '@/composables/useAgents'
import { humanizeCron, repeatLabel, statusLabel, formatDateTimeWithYear } from '@/utils/format'
import { useForgeBinding } from '@/composables/useForgeBinding'
import { store } from '@/stores/app'
import TaskBreadcrumb from '@/components/task/TaskBreadcrumb.vue'
import RefreshButton from '@/components/common/RefreshButton.vue'
import AgentIcon from '@/components/common/AgentIcon.vue'
import LoadingIndicator from '@/components/common/LoadingIndicator.vue'

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
  // Trigger mode: 'cron' (default/absent) or 'event'.
  triggerMode?: string
}

// Every event task watches its project's bound repository, so one shared
// binding lookup covers the whole list — the repository is a property of the
// project, not of each row. The shared store coalesces this with the lookups
// the detail/form views and the dock icon perform.
const { slug: boundRepoLabel, resolved: bindingResolved, refresh: refreshBinding } = useForgeBinding()

// Before the binding resolves, the row must not claim the project is unbound:
// "" is a real answer (confirmed unbound), a pending lookup is not.
const repoRowLabel = computed(() => {
  if (boundRepoLabel.value) return boundRepoLabel.value
  return bindingResolved.value ? t('task.form.eventRepoUnbound') : t('common.loading')
})

const tasks = computed(() => store.state.tasks as unknown as TaskItem[])
const hasUnread = computed(() => store.state.taskUnreadCount > 0)
const loading = ref(false)

defineEmits<{
  create: []
  select: [taskId: number]
}>()

async function refresh() {
  if (loading.value) return
  loading.value = true
  try {
    // Minimum spin duration so the refresh animation is always visible,
    // even when the API responds almost instantly.
    await Promise.all([
      Promise.all([loadTasks(), loadAgents(), refreshBinding()]),
      new Promise(resolve => setTimeout(resolve, 600)),
    ])
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

/* Compact header — unified with detail/form/history/settings/proxy headers */
.list-header {
  display: flex;
  align-items: center;
  height: var(--header-height);
  padding:0 var(--space-2) 0 var(--space-6);
  flex-shrink: 0;
  background: var(--bg-primary);
  border-bottom: 1px solid var(--border-color, #e5e5e5);
  gap: var(--space-3);
}

/* Create button in header toolbar */
.create-btn {
  width: 28px;
  height: 28px;
  border: none;
  border-radius: var(--radius-lg);
  background: var(--accent-color, #0066cc);
  color: #fff;
  cursor: pointer;
  display: flex;
  align-items: center;
  justify-content: center;
  flex-shrink: 0;
  transition: all var(--duration-slow) ease;
}

/* Header icon button (refresh, etc.) */
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

/* Clear-unread button accent when unread messages exist */
.clear-unread-btn.active {
  color: var(--accent-color, #0066cc);
  background: color-mix(in srgb, var(--accent-color, #0066cc) 10%, var(--bg-secondary, #f1f3f5));
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
  padding: var(--space-4);
}

.task-items-container {
  display: flex;
  flex-direction: column;
  gap: var(--space-4);
}

.task-loading,
.task-empty {
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: var(--space-6);
  height: 100%;
  color: var(--text-muted, #999);
  font-size: var(--font-size-lg);
}

.empty-icon {
  opacity: var(--opacity-muted);
}

.task-item {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: var(--space-5);
  background: var(--bg-secondary, #f8f9fa);
  border: 1px solid var(--border-color, #e5e5e5);
  border-radius: 0;
  cursor: pointer;
  transition: all var(--duration-slow) ease;
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
  opacity: var(--opacity-soft);
  background: var(--bg-tertiary, #f1f3f5);
}

.task-item-main {
  flex: 1;
  display: flex;
  flex-direction: column;
  gap: var(--space-3);
  min-width: 0;
}

.task-item-header {
  display: flex;
  align-items: center;
  gap: var(--space-3);
  min-width: 0;
}

.task-item-icon {
  flex-shrink: 0;
  vertical-align: middle;
}

/* Trigger-type badge. Kept visually distinct from the status pill on the right
   (which reports lifecycle) and from the meta icons (which are grey) — this is
   the one element that answers "what starts this task?" at a glance.

   The label carries the meaning; colour is a secondary cue. Text uses
   --text-primary rather than the hue token because several themes define
   --color-info/--color-purple as soft pastels (everforest-light's #7fbbb3 on
   #f2e9d0 measures 1.7:1), which would make this badge unreadable exactly
   where it matters. Tinting the background and border instead keeps the two
   modes distinguishable without depending on low-contrast text. */
.task-trigger-badge {
  font-size: var(--font-size-2xs);
  font-weight: var(--font-weight-semibold);
  padding:1px var(--space-3);
  border-radius: var(--radius-full);
  border: 1px solid transparent;
  flex-shrink: 0;
  line-height: var(--line-height-normal);
  white-space: nowrap;
}

.task-trigger-badge.is-cron {
  color: var(--text-primary, #1a1a1a);
  background: color-mix(in srgb, var(--color-info, #1f6feb) 18%, transparent);
  border-color: color-mix(in srgb, var(--color-info, #1f6feb) 45%, transparent);
}

.task-trigger-badge.is-event {
  color: var(--text-primary, #1a1a1a);
  background: color-mix(in srgb, var(--color-purple, #7c3aed) 18%, transparent);
  border-color: color-mix(in srgb, var(--color-purple, #7c3aed) 45%, transparent);
}

.task-item-name {
  font-size: var(--font-size-lg);
  font-weight: var(--font-weight-semibold);
  color: var(--text-primary, #1a1a1a);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  flex: 1;
  min-width: 0;
}

.task-item-unread {
  font-weight: var(--font-weight-semibold);
  background: var(--accent-color, #0066cc);
  color: #fff;
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
  font-size: var(--font-size-2xs);
  padding:3px var(--space-3);
  border-radius: var(--radius-xs);
  font-weight: var(--font-weight-semibold);
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
  background: var(--color-green, #22c55e);
  flex-shrink: 0;
  animation: task-running-pulse 0.8s ease-in-out infinite;
}

@keyframes task-running-pulse {
  0%, 100% { opacity: 1; box-shadow: 0 0 0 0 rgba(34, 197, 94, 0.5); }
  50% { opacity: var(--opacity-soft); box-shadow: 0 0 10px 4px rgba(34, 197, 94, 0.3); }
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
  gap: var(--space-4);
  font-size: var(--font-size-sm);
  color: var(--text-secondary, #666);
  min-width: 0;
  flex-wrap: wrap;
}

.meta-item {
  display: flex;
  align-items: center;
  gap: var(--space-2);
}

.meta-icon {
  color: var(--text-muted, #999);
}

/* The cron label is either "Daily HH:MM" or a raw 5-field expression — at most
   ~12 characters, so it never needs truncating. The max-width cap that used to
   live here only ever clipped the event subscription line, which the row no
   longer renders. */

.task-progress {
  color: var(--accent-color, #0066cc);
  font-weight: var(--font-weight-medium);
  margin-left: 2px;
}

.task-item-next {
  display: flex;
  align-items: center;
  gap: 4px;
  font-size: var(--font-size-xs);
  color: var(--text-muted, #999);
  background: var(--bg-primary, #fff);
  padding: 4px 8px;
  border-radius: 4px;
  border: 1px solid var(--border-color, #e5e5e5);
  width: fit-content;
  max-width: 100%;
}

/* An owner/repo can be long; truncate rather than widen the row. The full
   value stays available via the title attribute. */
.task-item-repo {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  min-width: 0;
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
</style>
