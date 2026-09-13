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
import { computed, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { AlertTriangle, Braces, Zap } from 'lucide-vue-next'
import { eventChips, eventKindLabel, type EventChip } from '@/utils/forgeEventLabels'
import { fetchForgeBinding } from '@/utils/forgeApi'
import { formatDateTimeWithYear } from '@/utils/format'

const { t } = useI18n()

const props = defineProps<{
  task: Record<string, unknown>
}>()

const eventTypes = computed(() => (props.task.eventTypes as string) || '')
const eventRepo = computed(() => (props.task.eventRepo as string) || '')
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

// ── Repository scope ──
// An explicit eventRepo is authoritative. When empty the backend matches any
// repo bound to the project, so resolve the binding to show the concrete repo.
const boundRepoLabel = ref('')
onMounted(async () => {
    if (eventRepo.value) return
    try {
        const res = await fetchForgeBinding()
        const b = res?.binding
        if (b) boundRepoLabel.value = `${b.owner}/${b.repo}`
    } catch {
        // Best-effort: fall back to the generic label below.
    }
})

const repoLabel = computed(() => {
    if (eventRepo.value) return explicitRepo.value || eventRepo.value
    return boundRepoLabel.value || t('task.form.eventRepoAny')
})

/** The bare owner/repo from an explicit scope, or '' when unset. Stored as
 *  "platform|host|owner/repo"; the owner/repo tail is what identifies it. */
const explicitRepo = computed(() => {
    if (!eventRepo.value) return ''
    const parts = eventRepo.value.split('|')
    return parts[parts.length - 1] || ''
})

// ── Event context preview ──
// Mirrors the backend's EventPromptTemplate ordering, but substitutes sample
// values so the user sees the shape of what will be injected. Variables scoped
// to a transition only appear when that transition is subscribed.
const subscribedTransitions = computed(() => new Set(eventChips(eventTypes.value).map(c => c.transition)))

// The sample item is whichever kind the task actually subscribes to, so an
// issue-only task does not show a "pr #123" sample it will never receive.
const sampleKind = computed<'issue' | 'pr'>(() => {
    const kinds = eventChips(eventTypes.value).map(c => c.kind)
    if (kinds.includes('issue') && !kinds.includes('pr')) return 'issue'
    return 'pr'
})

const sampleItemPath = computed(() => (sampleKind.value === 'pr' ? 'pull' : 'issues'))

const sampleState = computed(() => {
    const transitions = subscribedTransitions.value
    if (transitions.has('merged')) return 'merged'
    if (transitions.has('closed')) return 'closed'
    return 'open'
})

interface ContextRow {
    label: string
    placeholder: string
    value: string
}

// Only a real repository identity may appear in a sample URL — the generic
// "any bound repo" label is prose, not a host.
const sampleRepo = computed(() => boundRepoLabel.value || explicitRepo.value || 'owner/repo')

const contextRows = computed<ContextRow[]>(() => {
    const subscribed = subscribedTransitions.value
    const showAll = subscribed.size === 0
    const show = (transition: string) => showAll || subscribed.has(transition)
    const rows: ContextRow[] = [
        { label: t('task.form.varEventType'), placeholder: 'EVENT_TYPE', value: sampleEventType.value },
        { label: t('task.form.varRepo'), placeholder: 'REPO', value: sampleRepo.value },
        { label: t('task.form.varItem'), placeholder: 'ITEM_TYPE #ITEM_NUMBER', value: `${sampleKind.value} #123` },
        { label: t('task.form.varTitle'), placeholder: 'TITLE', value: t('task.overview.eventSampleTitle') },
        { label: t('task.form.varUrl'), placeholder: 'URL', value: `https://${sampleRepo.value}/${sampleItemPath.value}/123` },
        { label: t('task.form.varAuthor'), placeholder: 'AUTHOR', value: 'octocat' },
        { label: t('task.form.varState'), placeholder: 'STATE', value: sampleState.value },
    ]
    if (show('commented')) {
        rows.push({ label: t('task.form.varCommentBody'), placeholder: 'COMMENT_BODY', value: t('task.overview.eventSampleComment') })
    }
    if (show('pipeline_done')) {
        rows.push({ label: t('task.form.varPipelineStatus'), placeholder: 'PIPELINE_STATUS', value: 'success' })
        rows.push({ label: t('task.form.varPipelineUrl'), placeholder: 'PIPELINE_URL', value: 'https://ci.example.com/run/42' })
    }
    return rows
})

/** The first subscribed transition, used to make the sample event type concrete. */
const sampleEventType = computed(() => {
    const first = eventChips(eventTypes.value)[0]
    return first ? first.key : 'pr.opened'
})
</script>

<style scoped>
.overview-card {
  background: var(--bg-secondary, #f8f9fa);
  border: 1px solid var(--border-color, #e5e5e5);
  border-radius: var(--radius-sm, 6px);
  padding: 10px;
  display: flex;
  flex-direction: column;
  gap: 6px;
}
.card-title {
  display: flex;
  align-items: center;
  gap: 8px;
  font-size: var(--font-size-md);
  font-weight: 600;
  color: var(--text-primary, #1a1a1a);
  margin: 0;
}
.card-icon { color: var(--text-muted, #999); }
.prompt-title-text { flex: 1; min-width: 0; }

/* ── Paused warning ── */
.event-paused-note {
  display: flex;
  align-items: center;
  gap: 6px;
  padding: 6px 8px;
  border-radius: var(--radius-sm, 6px);
  background: color-mix(in srgb, var(--color-yellow, #eab308) 12%, transparent);
  border: 1px solid color-mix(in srgb, var(--color-yellow, #eab308) 35%, transparent);
  color: var(--color-yellow, #a16207);
  font-size: var(--font-size-xs);
}

/* ── Trigger chips ── */
.event-chips {
  display: flex;
  flex-direction: column;
  gap: 6px;
}
.event-chip-group {
  display: flex;
  align-items: baseline;
  gap: 6px;
  flex-wrap: wrap;
}
.event-chip-kind {
  font-size: var(--font-size-xs);
  font-weight: 600;
  color: var(--text-muted, #999);
  flex-shrink: 0;
  min-width: 56px;
}
.event-chips-row {
  display: flex;
  flex-wrap: wrap;
  gap: 4px;
}
.event-chip {
  font-size: var(--font-size-xs);
  padding: 1px 8px;
  border-radius: 999px;
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
  margin: 2px 0;
}
.overview-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
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
  font-weight: 500;
  padding: 1px 6px;
  border-radius: 999px;
  color: var(--text-muted, #999);
  background: var(--bg-tertiary, #f3f4f6);
  border: 1px solid var(--border-color, #e5e5e5);
  flex-shrink: 0;
}
.event-context-preview {
  border: 1px dashed var(--border-color, #d1d5db);
  border-radius: var(--radius-sm, 6px);
  background: var(--bg-primary, #fff);
  padding: 8px 10px;
  display: flex;
  flex-direction: column;
  gap: 3px;
}
.event-context-heading {
  font-size: var(--font-size-sm);
  font-weight: 600;
  color: var(--text-secondary, #4b5563);
  margin-bottom: 2px;
}
.event-context-row {
  display: flex;
  gap: 6px;
  font-size: var(--font-size-sm);
  line-height: 1.5;
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
