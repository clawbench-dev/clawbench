<template>
  <div class="overview-card">
    <h3 class="card-title">
      <Zap class="card-icon" :size="14" />
      {{ t('task.overview.eventTrigger') }}
    </h3>

    <!-- A paused event task is silently inert: the trigger requires
         status=active, so nothing fires and no error is surfaced anywhere.
         Say so explicitly instead of leaving the user to wonder. -->
    <div v-if="paused" class="event-paused-note">
      <AlertTriangle :size="13" />
      <span>{{ t('task.overview.eventPausedNote') }}</span>
    </div>

    <!-- Subscribed transitions, grouped by item kind so "a new issue" and
         "a new PR" read as the distinct triggers they are. -->
    <div class="event-chips">
      <template v-for="group in groupedChips" :key="group.kind">
        <div class="event-chip-group">
          <span class="event-chip-kind">{{ group.kindLabel }}</span>
          <span class="event-chips-row">
            <span
              v-for="chip in group.chips"
              :key="chip.key"
              class="event-chip"
              :class="`kind-${group.kind}`"
            >{{ chip.label }}</span>
          </span>
        </div>
      </template>
      <div v-if="!groupedChips.length" class="overview-value muted">{{ t('task.form.eventTypesNone') }}</div>
    </div>

    <div class="overview-divider"></div>

    <!-- Repository scope: an explicit repo, or whichever repo the project is
         bound to (resolved so the user sees the concrete repo, not "any"). -->
    <div class="overview-row">
      <span class="overview-label">{{ t('task.overview.eventRepo') }}</span>
      <span class="overview-value">{{ repoLabel }}</span>
    </div>
    <div v-if="runCount > 0" class="overview-row">
      <span class="overview-label">{{ t('chat.contentBlocks.statusExecutions', { count: runCount }) }}</span>
    </div>
    <div v-if="lastRunAt" class="overview-row">
      <span class="overview-label">{{ t('chat.contentBlocks.lastRun') }}</span>
      <span class="overview-value">{{ formatDateTimeWithYear(lastRunAt) }}</span>
    </div>

    <!-- The prompt is written against the context injected at trigger time, so
         running it by hand would leave those fields unsubstituted. The detail
         page therefore hides the Run button — say why, rather than letting the
         user hunt for an action that is deliberately absent. -->
    <div class="form-hint">{{ t('task.overview.eventNoManualRun') }}</div>
  </div>

  <!-- Read-only rendering of the block the backend prepends to the prompt.
       Values are illustrative — the real ones come from the triggering event —
       so they are tinted and marked as samples rather than presented as data. -->
  <div class="overview-card">
    <h3 class="card-title">
      <Braces class="card-icon" :size="14" />
      <span class="prompt-title-text">{{ t('task.overview.eventContext') }}</span>
      <span class="sample-badge">{{ t('task.overview.eventContextSample') }}</span>
    </h3>
    <div class="event-context-preview">
      <div class="event-context-heading">{{ t('task.form.eventContextHeader') }}</div>
      <div v-for="row in contextRows" :key="row.placeholder" class="event-context-row">
        <span class="event-context-label">{{ row.label }}</span>
        <span class="event-context-value">{{ row.value }}</span>
      </div>
    </div>
    <div class="form-hint">{{ t('task.overview.eventContextHint') }}</div>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted } from 'vue'
import { useI18n } from 'vue-i18n'
import { AlertTriangle, Braces, Zap } from 'lucide-vue-next'
import { eventChips, eventKindLabel, type EventChip } from '@/utils/forgeEventLabels'
import { eventContextSampleRows, isRepoTargetedOnly } from '@/utils/forgeEventContextVars'
import { useForgeBinding } from '@/composables/useForgeBinding'
import { formatDateTimeWithYear } from '@/utils/format'

const { t } = useI18n()

const props = defineProps<{
  task: Record<string, unknown>
}>()

const eventTypes = computed(() => (props.task.eventTypes as string) || '')
const runCount = computed(() => (props.task.runCount as number) || 0)
const lastRunAt = computed(() => props.task.lastRunAt as string | undefined)
const paused = computed(() => (props.task.status as string) === 'paused')

// ── Trigger chips ──
// Grouped by item kind; a bare legacy key has no kind and lands in its own
// "ungrouped" bucket rendered without a prefix.
const groupedChips = computed(() => {
    const order: string[] = []
    const byKind = new Map<string, EventChip[]>()
    for (const chip of eventChips(eventTypes.value)) {
        const kind = chip.kind || ''
        if (!byKind.has(kind)) {
            byKind.set(kind, [])
            order.push(kind)
        }
        byKind.get(kind)!.push(chip)
    }
    return order.map(kind => ({
        kind,
        kindLabel: kind ? eventKindLabel(kind) : '',
        chips: byKind.get(kind)!,
    }))
})

// ── Repository ──
// Every event task watches its project's bound repository, so the binding is
// resolved for display. An unbound project shows the "no repository" label —
// the task can never fire, which the form warns about at creation time.
//
// The shared store coalesces this with the other consumers, and `resolved`
// separates "not fetched yet" from "confirmed unbound": without it the card
// flashes a false "no repository bound" while the lookup is in flight.
const { slug: boundRepoLabel, resolved: bindingResolved, refresh: refreshBinding } = useForgeBinding()
onMounted(() => { void refreshBinding() })

const repoLabel = computed(() => {
    if (boundRepoLabel.value) return boundRepoLabel.value
    return bindingResolved.value ? t('task.form.eventRepoUnbound') : t('common.loading')
})

// ── Event context preview ──
// The variable set and its gating live in the shared registry, so this block,
// the form's placeholder block and the backend render cannot drift apart. Here
// each row is filled with a sample value so the user sees the shape of what
// will be injected.
const subscribedTransitions = computed(
    () => new Set(eventChips(eventTypes.value).map(c => c.transition)),
)

// The sample item is whichever kind the task actually subscribes to, so an
// issue-only task does not show a "pr #123" sample it will never receive.
//
// A repository-targeted subscription (a pipeline) has no item kind at all: its
// chips carry the "repo" pseudo-kind. Defaulting to 'pr' there made every
// pipeline task advertise a "pr #123" sample it can never receive, so the
// absence of a real kind is represented explicitly rather than folded into 'pr'.
const sampleKind = computed<'issue' | 'pr' | ''>(() => {
    const kinds = eventChips(eventTypes.value).map(c => c.kind)
    if (kinds.includes('issue')) return 'issue'
    if (kinds.includes('pr')) return 'pr'
    return ''
})

/**
 * True when every subscribed transition is repository-targeted (a pipeline).
 *
 * Such a task never receives an item, so the sample block must not promise an
 * `ITEM_TYPE #ITEM_NUMBER` row — that would describe a payload the task can
 * never get.
 */
const repoTargetedOnly = computed(() => isRepoTargetedOnly(subscribedTransitions.value))

// Only a real repository identity may appear in a sample URL — the generic
// "any bound repo" label is prose, not a host.
const sampleRepo = computed(() => boundRepoLabel.value || 'owner/repo')

// Only meaningful for an issue/PR subscription; a pipeline has no item URL, so
// the sample falls back to the repository's own URL.
const sampleItemPath = computed(() => (sampleKind.value === 'issue' ? 'issues' : 'pull'))

const sampleState = computed(() => {
    const transitions = subscribedTransitions.value
    if (transitions.has('merged')) return 'merged'
    if (transitions.has('closed')) return 'closed'
    return 'open'
})

/** Sample values that depend on the task's own subscription, keyed by placeholder. */
const sampleOverrides = computed<Record<string, string>>(() => ({
    // The first subscribed transition, so the sample event type is concrete.
    EVENT_TYPE: eventChips(eventTypes.value)[0]?.key ?? 'pr.opened',
    REPO: sampleRepo.value,
    // Keyed by the registry's placeholder token, which is the backend's own
    // spelling ("ITEM_TYPE #ITEM_NUMBER" is ONE variable rendering two values).
    'ITEM_TYPE #ITEM_NUMBER': `${sampleKind.value} #123`,
    // A pipeline task has no item to point at, so the sample is the repository
    // root rather than a fabricated issue/PR link.
    URL: sampleKind.value === ''
        ? `https://${sampleRepo.value}`
        : `https://${sampleRepo.value}/${sampleItemPath.value}/123`,
    STATE: sampleState.value,
    // Free-text samples are localized (and marked as samples) rather than taken
    // from the registry's literal defaults, which are only there so the
    // registry is self-describing.
    TITLE: t('task.overview.eventSampleTitle'),
    COMMENT_BODY: t('task.overview.eventSampleComment'),
}))

const contextRows = computed(() =>
    eventContextSampleRows(
        { transitions: subscribedTransitions.value, repoTargetedOnly: repoTargetedOnly.value },
        sampleOverrides.value,
    ),
)
</script>

<style scoped>
.overview-card {
  background: var(--bg-secondary, #f8f9fa);
  border: 1px solid var(--border-color, #e5e5e5);
  border-radius: 0;
  padding: var(--space-5);
  display: flex;
  flex-direction: column;
  gap: var(--space-3);
}
.card-title {
  display: flex;
  align-items: center;
  gap: var(--space-4);
  font-size: var(--font-size-md);
  font-weight: var(--font-weight-semibold);
  color: var(--text-primary, #1a1a1a);
  margin: 0;
}
.card-icon { color: var(--text-muted, #999); }
.prompt-title-text { flex: 1; min-width: 0; }

/* ── Paused warning ── */
.event-paused-note {
  display: flex;
  align-items: center;
  gap: var(--space-3);
  padding: var(--space-3) var(--space-4);
  border-radius: 0;
  background: color-mix(in srgb, var(--color-yellow, #eab308) 12%, transparent);
  border: 1px solid color-mix(in srgb, var(--color-yellow, #eab308) 35%, transparent);
  color: var(--color-yellow, #a16207);
  font-size: var(--font-size-xs);
}

/* ── Trigger chips ── */
.event-chips {
  display: flex;
  flex-direction: column;
  gap: var(--space-3);
}
.event-chip-group {
  display: flex;
  align-items: baseline;
  gap: var(--space-3);
  flex-wrap: wrap;
}
.event-chip-kind {
  font-size: var(--font-size-xs);
  font-weight: var(--font-weight-semibold);
  color: var(--text-muted, #999);
  flex-shrink: 0;
  min-width: 56px;
}
.event-chips-row {
  display: flex;
  flex-wrap: wrap;
  gap: var(--space-2);
}
.event-chip {
  font-size: var(--font-size-xs);
  padding:1px var(--space-4);
  border-radius: var(--radius-full);
  border: 1px solid transparent;
  white-space: nowrap;
}
/* Issues and PRs are different triggers — tint them differently so a mixed
   subscription is scannable at a glance. */
.event-chip.kind-issue {
  color: var(--color-success, #16a34a);
  background: color-mix(in srgb, var(--color-success, #16a34a) 12%, transparent);
  border-color: color-mix(in srgb, var(--color-success, #16a34a) 35%, transparent);
}
.event-chip.kind-pr {
  color: var(--color-purple, #8b5cf6);
  background: color-mix(in srgb, var(--color-purple, #8b5cf6) 12%, transparent);
  border-color: color-mix(in srgb, var(--color-purple, #8b5cf6) 35%, transparent);
}
.event-chip:not(.kind-issue):not(.kind-pr) {
  color: var(--text-secondary, #4b5563);
  background: var(--bg-tertiary, #f3f4f6);
  border-color: var(--border-color, #e5e5e5);
}

.overview-divider {
  height: 1px;
  background: var(--border-color, #e5e5e5);
  margin: var(--space-1) 0;
}
.overview-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: var(--space-4);
}
.overview-label {
  font-size: var(--font-size-sm);
  color: var(--text-secondary, #666);
  flex-shrink: 0;
}
.overview-value {
  font-size: var(--font-size-md);
  color: var(--text-primary, #1a1a1a);
  text-align: right;
  word-break: break-word;
}
.overview-value.muted { color: var(--text-muted, #999); }

/* ── Context preview ── */
.sample-badge {
  font-size: var(--font-size-2xs);
  font-weight: var(--font-weight-medium);
  padding:1px var(--space-3);
  border-radius: var(--radius-full);
  color: var(--text-muted, #999);
  background: var(--bg-tertiary, #f3f4f6);
  border: 1px solid var(--border-color, #e5e5e5);
  flex-shrink: 0;
}
.event-context-preview {
  border: 1px dashed var(--border-color, #d1d5db);
  border-radius: 0;
  background: var(--bg-primary, #fff);
  padding: var(--space-4) var(--space-5);
  display: flex;
  flex-direction: column;
  gap: 3px;
}
.event-context-heading {
  font-size: var(--font-size-sm);
  font-weight: var(--font-weight-semibold);
  color: var(--text-secondary, #4b5563);
  margin-bottom: var(--space-1);
}
.event-context-row {
  display: flex;
  gap: var(--space-3);
  font-size: var(--font-size-sm);
  line-height: var(--line-height-normal);
}
.event-context-label {
  color: var(--text-muted, #999);
  flex-shrink: 0;
}
.event-context-label::after { content: '：'; }
.event-context-value {
  color: var(--text-secondary, #4b5563);
  font-style: italic;
  word-break: break-word;
}
.form-hint {
  font-size: var(--font-size-xs);
  color: var(--text-muted, #6b7280);
}
</style>
