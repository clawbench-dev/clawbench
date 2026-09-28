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
    </div>

    <div v-if="actionable" class="stask-body">
      <!-- ── Disabled event task ──
           An event task fires only while status=active, so disabling one makes
           it silently inert: no error, no notification, and the status dot just
           goes grey. The detail page says so outright (TaskEventCard), and the
           chat card must not leave the reader to infer it. Event-only: a paused
           cron task simply has no next run, which its own rows already show. -->
      <div v-if="eventPaused" class="stask-warn">
        <AlertTriangle :size="13" />
        <span>{{ t('task.overview.eventPausedNote') }}</span>
      </div>

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
        <div v-if="task!.lastRunAt" class="stask-row"><strong>{{ t('chat.contentBlocks.lastRun') }}</strong>{{ formatRelativeTime(task!.lastRunAt as string) }}</div>
      </template>

      <!-- ── Cron schedule ── -->
      <template v-else>
        <div class="stask-row"><strong>{{ t('chat.contentBlocks.frequency') }}</strong>{{ humanizeCron(task!.cronExpr as string) }}</div>
        <div class="stask-row"><strong>{{ t('chat.contentBlocks.executor') }}</strong><AgentIcon :backend="getAgentBackend(task!.agentId as string)" :name="getAgentName(task!.agentId as string)" :size="14" class="stask-agent-icon" /> {{ getAgentName(task!.agentId as string) }}</div>
        <!-- Progress toward the run limit, matching the task list page. Only a
             bounded task has progress to show; an unlimited one never advances
             toward anything, so a count there would be noise. -->
        <div class="stask-row">
          <strong>{{ t('chat.contentBlocks.repeat') }}</strong>
          <span>{{ repeatLabel(task!.repeatMode as string, task!.maxRuns as number) }}<span v-if="isBoundedRepeat" class="stask-progress">({{ (task!.runCount as number) || 0 }}/{{ task!.maxRuns || 1 }})</span></span>
        </div>
        <div class="stask-row"><strong>{{ t('chat.contentBlocks.status') }}</strong><span class="stask-status-dot" :class="statusClassOf(task!)"></span>{{ statusLabelOf(task!) }}</div>
        <div v-if="task!.lastRunAt" class="stask-row"><strong>{{ t('chat.contentBlocks.lastRun') }}</strong>{{ formatRelativeTime(task!.lastRunAt as string) }}</div>
        <div v-if="task!.nextRunAt" class="stask-row"><strong>{{ t('chat.contentBlocks.nextRun') }}</strong>{{ formatRelativeTime(task!.nextRunAt as string) }}</div>
      </template>

      <!-- ── What the task does ──
           Every row above describes mechanics (when it runs, who runs it). The
           prompt is the only place the card says what a run actually does, and
           it is already in the /api/tasks payload, so no extra request is
           needed. Last row for both trigger modes, mirroring the detail page's
           trigger-card-then-prompt-card order.
           Stripped of markdown and truncated by stripMarkdownPreview (counted in
           code points, and it adds its own ellipsis); the CSS clamp is the
           narrow-screen safety net. -->
      <div v-if="promptPreview" class="stask-row stask-row-prompt">
        <strong>{{ t('chat.contentBlocks.prompt') }}</strong>
        <span class="stask-prompt">{{ promptPreview }}</span>
      </div>
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
import { AlertTriangle, Archive, Clock, Zap } from 'lucide-vue-next'
import AgentIcon from '@/components/common/AgentIcon.vue'
import { humanizeCron, repeatLabel, formatRelativeTime, stripMarkdownPreview } from '@/utils/format'
import { statusClass, statusLabel } from '@/utils/contentBlocks.ts'
import { eventChips, eventKindLabel } from '@/utils/forgeEventLabels'

const { t } = useI18n()

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

/**
 * A disabled event task can never fire (the trigger requires status=active),
 * and nothing else in the app reports that — so the card states it. Cron tasks
 * are excluded: pausing one only clears its next run, which its own row shows.
 */
const eventPaused = computed(() => isEventTask.value && props.task?.status === 'paused')

/**
 * The prompt, stripped of markdown and truncated for the preview row. Empty
 * when the task carries no prompt (or has not resolved yet), which hides the
 * row rather than printing an empty label.
 */
const promptPreview = computed(() => stripMarkdownPreview((props.task?.prompt as string) || ''))

/**
 * Whether the repeat mode has a limit to make progress toward. An unlimited
 * task never advances toward anything, so `runCount` there is not progress and
 * printing "12/1" (the maxRuns=0 fallback) would be actively wrong.
 */
const isBoundedRepeat = computed(() => {
  const mode = props.task?.repeatMode as string
  return mode === 'once' || mode === 'limited'
})

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

// The status helpers take `t` explicitly (they are shared with non-component
// callers), so these wrappers bind the component's own i18n context for the
// template. The task record arrives as a loose API payload, so the shape the
// helpers require is asserted here rather than on the prop.
function statusClassOf(task: Record<string, unknown>) { return statusClass(task as { status: string }) }
function statusLabelOf(task: Record<string, unknown>) { return statusLabel(task as { status: string; runCount: number; runningCount: number }, t) }
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

/* ── Prompt preview ──
   The value is the one row whose content is arbitrary user text, so it may
   shrink and clamp (the header gutter `.stask-row strong` stays fixed). Two
   lines is the budget: enough to recognise the task, short of turning the card
   into a prompt viewer. `overflow-wrap: anywhere` keeps an unbroken token (a
   long path) from overflowing the rounded card. */
.stask-row-prompt {
  align-items: baseline;
}

.stask-prompt {
  min-width: 0;
  display: -webkit-box;
  -webkit-line-clamp: 2;
  line-clamp: 2;
  -webkit-box-orient: vertical;
  overflow: hidden;
  overflow-wrap: anywhere;
}

.stask-progress {
  margin-left: var(--space-2);
  color: var(--accent-color, #4a90d9);
  font-weight: var(--font-weight-medium);
}

/* ── Disabled event-task warning ──
   Mirrors .event-paused-note on the task detail page (same amber tint, same
   warning glyph) so the two surfaces agree on what the state means. */
.stask-warn {
  display: flex;
  align-items: center;
  gap: var(--space-3);
  padding: var(--space-3) var(--space-4);
  margin-bottom: var(--space-3);
  background: color-mix(in srgb, var(--color-yellow, #eab308) 12%, transparent);
  border: 1px solid color-mix(in srgb, var(--color-yellow, #eab308) 35%, transparent);
  color: var(--color-yellow, #a16207);
  font-size: var(--font-size-xs);
  line-height: var(--line-height-normal);
}

.stask-warn svg {
  flex-shrink: 0;
}

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
