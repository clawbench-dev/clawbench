<template>
  <div
    class="scheduled-task-card"
    :class="{ deleted, 'is-event': isEventTask }"
    :data-quote-source="quoteSourceLabel"
    :data-quote-task-id="taskIdAttr"
    data-quote-language="task"
    @click="handleClick"
  >
    <div class="stask-header">
      <Archive v-if="deleted" :size="14" class="stask-icon" />
      <!-- The header icon states WHAT starts the task: a cron task is defined
           by *when* it runs (a clock), an event task by *what* it watches
           (a bolt). A single Clock icon told an event task's user it has a
           schedule, which it never does. -->
      <Zap v-else-if="isEventTask" :size="14" class="stask-icon" />
      <Clock v-else :size="14" class="stask-icon" />
      <template v-if="deleted">{{ t('chat.contentBlocks.taskDeleted') }}</template>
      <template v-else-if="loading">{{ t('chat.contentBlocks.loading') }}</template>
      <template v-else>{{ task?.name || t('chat.contentBlocks.scheduledTaskCreated') }}</template>
      <span v-if="actionable" class="stask-status-badge" :class="statusValue">{{ statusLabelSimpleOf(task!) }}</span>
    </div>

    <div v-if="actionable" class="stask-body">
      <!-- ── Event trigger ──
           An event task has no schedule and its repeat mode is inert: the
           backend never exhausts it (scheduler.go's event-task branch), so
           rendering 频率 / 重复 would print a blank cron line and a fabricated
           "不限次数". The subscription is what actually fires the task, so that
           is what the card shows instead. -->
      <template v-if="isEventTask">
        <div class="stask-row stask-row-chips">
          <strong>{{ t('task.form.eventTypes') }}</strong>
          <span class="stask-chips">
            <span
              v-for="chip in chips"
              :key="chip.key"
              class="stask-chip"
              :class="`kind-${chip.kind || 'other'}`"
            >{{ chip.kind ? `${eventKindLabel(chip.kind)} · ${chip.label}` : chip.label }}</span>
            <span v-if="!chips.length" class="stask-chips-none">{{ t('task.form.eventTypesNone') }}</span>
          </span>
        </div>
        <div class="stask-row"><strong>{{ t('chat.contentBlocks.executor') }}</strong><AgentIcon :backend="getAgentBackend(task!.agentId as string)" :name="getAgentName(task!.agentId as string)" :size="14" class="stask-agent-icon" /> {{ getAgentName(task!.agentId as string) }}</div>
        <div class="stask-row"><strong>{{ t('chat.contentBlocks.status') }}</strong><span class="stask-status-dot" :class="statusClassOf(task!)"></span>{{ statusLabelOf(task!) }}</div>
        <div v-if="task!.lastRunAt" class="stask-row"><strong>{{ t('chat.contentBlocks.lastRun') }}</strong>{{ formatTimeOf(task!.lastRunAt as string) }}</div>
      </template>

      <!-- ── Cron schedule ── -->
      <template v-else>
        <div class="stask-row"><strong>{{ t('chat.contentBlocks.frequency') }}</strong>{{ humanizeCron(task!.cronExpr as string) }}</div>
        <div class="stask-row"><strong>{{ t('chat.contentBlocks.executor') }}</strong><AgentIcon :backend="getAgentBackend(task!.agentId as string)" :name="getAgentName(task!.agentId as string)" :size="14" class="stask-agent-icon" /> {{ getAgentName(task!.agentId as string) }}</div>
        <div class="stask-row"><strong>{{ t('chat.contentBlocks.repeat') }}</strong>{{ repeatLabel(task!.repeatMode as string, task!.maxRuns as number) }}</div>
        <div class="stask-row"><strong>{{ t('chat.contentBlocks.status') }}</strong><span class="stask-status-dot" :class="statusClassOf(task!)"></span>{{ statusLabelOf(task!) }}</div>
        <div v-if="task!.lastRunAt" class="stask-row"><strong>{{ t('chat.contentBlocks.lastRun') }}</strong>{{ formatTimeOf(task!.lastRunAt as string) }}</div>
        <div v-if="task!.nextRunAt" class="stask-row"><strong>{{ t('chat.contentBlocks.nextRun') }}</strong>{{ formatTimeOf(task!.nextRunAt as string) }}</div>
      </template>
    </div>
  </div>
</template>

<script setup lang="ts">
/**
 * The task preview card shown inside a chat message.
 *
 * Extracted from ContentBlocks.vue, which rendered this markup twice — once for
 * the summary view (data fetched from /api/tasks) and once for the block view
 * (data from the shared task block store). Two copies meant every change had to
 * be made in both places, and the event-task adaptation was applied to only one
 * of the surfaces (the task detail page) while this card kept rendering
 * cron-only fields.
 */
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { Archive, Clock, Zap } from 'lucide-vue-next'
import AgentIcon from '@/components/common/AgentIcon.vue'
import { humanizeCron, repeatLabel } from '@/utils/format'
import { statusClass, statusLabel, statusLabelSimple, formatTime } from '@/utils/contentBlocks.ts'
import { eventChips, eventKindLabel } from '@/utils/forgeEventLabels'

const { t, locale } = useI18n()

const props = defineProps<{
  /** Task record from /api/tasks (or the shared block store). */
  task: Record<string, unknown> | null
  loading?: boolean
  deleted?: boolean
  /** Agent lookup helpers, injected down the chat render chain. */
  getAgentBackend: (agentId: string) => string
  getAgentName: (agentId: string) => string
}>()

const emit = defineEmits(['select'])

// An absent triggerMode is the cron default (tasks created before event mode
// existed), matching TaskDetailPage / TaskListPage.
const isEventTask = computed(() => (props.task?.triggerMode as string) === 'event')

const chips = computed(() => eventChips(props.task?.eventTypes as string))

/** The task's raw status, used as the badge's modifier class. */
const statusValue = computed(() => (props.task?.status as string) || '')

/** The card is only interactive once its data resolved and it still exists. */
const actionable = computed(() => !props.deleted && !props.loading && !!props.task)

/**
 * Quote-source identity for this card.
 *
 * A scheduled-task card is quoted by selecting its title, so the quote must
 * carry the task id (a machine key the jump handler and the AI can both use).
 * The name alone is not addressable.
 *
 * No id is exposed for a deleted task: the task no longer exists, so offering
 * its id would produce a quote whose jump goes nowhere. The same holds before
 * the task record resolves.
 */
const taskIdAttr = computed(() => {
  if (props.deleted || props.loading || !props.task) return ''
  const id = props.task.id
  return typeof id === 'number' || typeof id === 'string' ? String(id) : ''
})

const quoteSourceLabel = computed(() => {
  const name = (props.task?.name as string) || ''
  if (!name || !taskIdAttr.value) return ''
  return `${name} (#${taskIdAttr.value})`
})

function handleClick() {
  if (actionable.value) emit('select')
}

// The shared helpers are locale-agnostic (they take `t`/`locale`), so these
// wrappers bind the component's own i18n context for the template. The task
// record arrives as a loose API payload, so the shape the helpers require is
// asserted here rather than on the prop.
function statusClassOf(task: Record<string, unknown>) { return statusClass(task as { status: string }) }
function statusLabelOf(task: Record<string, unknown>) { return statusLabel(task as { status: string; runCount: number; runningCount: number }, t) }
function statusLabelSimpleOf(task: Record<string, unknown>) { return statusLabelSimple(task as { status: string }, t) }
function formatTimeOf(iso: string) { return formatTime(iso, locale.value, t) }
</script>

<style scoped>
.scheduled-task-card {
  margin: var(--space-4) 0;
  border: 1px solid color-mix(in srgb, var(--accent-color, #4a90d9) 30%, var(--border-color, #dee2e6));
  /* Matches the unified inline card (.chat-inline-card) — the approval and
     ask cards — so the chat surfaces share one corner treatment. `overflow:
     hidden` is what lets the header strip's tinted background follow the
     rounded corners instead of poking past them. */
  border-radius: var(--radius-sm);
  overflow: hidden;
  background: color-mix(in srgb, var(--accent-color, #4a90d9) 6%, var(--bg-primary, #fff));
  cursor: pointer;
  transition: box-shadow var(--duration-base), border-color var(--duration-base);
}

@media (hover: hover) {
  .scheduled-task-card:hover {
    border-color: color-mix(in srgb, var(--accent-color, #4a90d9) 50%, var(--border-color, #dee2e6));
    box-shadow: 0 2px 8px color-mix(in srgb, var(--accent-color, #4a90d9) 15%, transparent);
  }
}

/* An event task is triggered by an external event rather than by a schedule,
   so its card carries the event accent (matching the task list's "事件" badge)
   instead of the schedule accent. */
.scheduled-task-card.is-event {
  border-color: color-mix(in srgb, var(--color-purple, #7c3aed) 30%, var(--border-color, #dee2e6));
  background: color-mix(in srgb, var(--color-purple, #7c3aed) 6%, var(--bg-primary, #fff));
}

@media (hover: hover) {
  .scheduled-task-card.is-event:hover {
    border-color: color-mix(in srgb, var(--color-purple, #7c3aed) 50%, var(--border-color, #dee2e6));
    box-shadow: 0 2px 8px color-mix(in srgb, var(--color-purple, #7c3aed) 15%, transparent);
  }
}

.scheduled-task-card.deleted {
  opacity: var(--opacity-muted);
  border-color: var(--border-color, #dee2e6);
  background: var(--bg-secondary);
  cursor: default;
  box-shadow: none;
}

.scheduled-task-card.deleted .stask-header {
  background: var(--bg-tertiary);
  color: var(--text-muted, #999);
  border-bottom-color: var(--border-color, #dee2e6);
}

.stask-header {
  display: flex;
  align-items: center;
  gap: 5px;
  padding: var(--space-2) var(--space-5);
  background: color-mix(in srgb, var(--accent-color, #4a90d9) 12%, transparent);
  color: var(--accent-color, #4a90d9);
  font-weight: var(--font-weight-semibold);
  font-size: var(--font-size-sm);
  border-bottom: 1px solid color-mix(in srgb, var(--accent-color, #4a90d9) 15%, var(--border-color, #dee2e6));
  cursor: pointer;
}

.scheduled-task-card.is-event .stask-header {
  background: color-mix(in srgb, var(--color-purple, #7c3aed) 12%, transparent);
  color: var(--color-purple, #7c3aed);
  border-bottom-color: color-mix(in srgb, var(--color-purple, #7c3aed) 15%, var(--border-color, #dee2e6));
}

.stask-icon {
  flex-shrink: 0;
  margin-right: var(--space-1);
}

.stask-body {
  padding: var(--space-5) var(--space-6);
  font-size: var(--font-size-sm);
  line-height: var(--line-height-relaxed);
}

.stask-row {
  display: flex;
  gap: var(--space-4);
  margin-bottom: var(--space-2);
}

.stask-row strong {
  min-width: 70px;
  color: var(--text-secondary, #495057);
}

/* The body is now the card's last element (the footer button was removed), so
   the final row must not add its own gap on top of the body's bottom padding. */
.stask-row:last-child {
  margin-bottom: 0;
}

/* The subscription list can run long, so its value wraps onto its own lines
   while the label keeps its fixed gutter like every other row. */
.stask-row-chips {
  align-items: baseline;
}

.stask-chips {
  display: flex;
  flex-wrap: wrap;
  gap: var(--space-2);
}

.stask-chip {
  font-size: var(--font-size-xs);
  padding: 1px var(--space-4);
  border-radius: var(--radius-full);
  border: 1px solid transparent;
  white-space: nowrap;
}

/* Same tinting as the task list / detail card: issues and PRs are different
   triggers, so a mixed subscription is scannable at a glance. */
.stask-chip.kind-issue {
  color: var(--color-success, #16a34a);
  background: color-mix(in srgb, var(--color-success, #16a34a) 12%, transparent);
  border-color: color-mix(in srgb, var(--color-success, #16a34a) 35%, transparent);
}

.stask-chip.kind-pr {
  color: var(--color-purple, #8b5cf6);
  background: color-mix(in srgb, var(--color-purple, #8b5cf6) 12%, transparent);
  border-color: color-mix(in srgb, var(--color-purple, #8b5cf6) 35%, transparent);
}

.stask-chip.kind-repo,
.stask-chip.kind-other {
  color: var(--text-secondary, #4b5563);
  background: var(--bg-tertiary, #f3f4f6);
  border-color: var(--border-color, #e5e5e5);
}

.stask-chips-none {
  font-size: var(--font-size-sm);
  color: var(--text-muted, #999);
}

.stask-agent-icon {
  vertical-align: middle;
}

.stask-status-badge {
  font-size: var(--font-size-2xs);
  padding: 1px 5px;
  border-radius: var(--radius-xs);
  font-weight: var(--font-weight-medium);
  margin-left: auto;
}

.stask-status-badge.active { background: rgba(34, 197, 94, 0.12); color: #22c55e; }
.stask-status-badge.paused { background: rgba(234, 179, 8, 0.12); color: #eab308; }
.stask-status-badge.completed { background: var(--bg-tertiary, #e9ecef); color: var(--text-muted, #999); }

.stask-status-dot {
  width: 8px;
  height: 8px;
  border-radius: 50%;
  flex-shrink: 0;
  align-self: center;
  margin-right: var(--space-2);
}

.stask-status-dot.status-active {
  background: #4caf50;
}

.stask-status-dot.status-paused {
  background: #ff9800;
}

.stask-status-dot.status-completed {
  background: #9e9e9e;
}
</style>
