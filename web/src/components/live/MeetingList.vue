<template>
  <div class="meeting-panel">
    <!-- One bar, not two: the tab title and the list's own title said the same
         thing stacked 36px apart. The record count rides next to the title and
         the refresh control closes the row, the way every other panel header
         does it (git history, forge, file manager). -->
    <div class="meeting-header">
      <span class="meeting-header__icon"><component :is="MEETING_ICON" :size="14" /></span>
      <span class="meeting-header__title">{{ t('meeting.list.title') }}</span>
      <span class="meeting-header__count count-badge">{{ t('meeting.list.count', { n: sessions.length }) }}</span>
      <span class="meeting-header__spacer" />
      <RefreshButton
        class="meeting-icon-btn meeting-refresh-btn"
        :loading="loading"
        :disabled="loading"
        :title="t('meeting.list.refresh')"
        data-action="meeting-refresh"
        @click="reload"
      />
    </div>

    <div class="meeting-body">
      <div v-if="loading && sessions.length === 0" class="meeting-hint">
        <LoadingIndicator size="sm" inline />
        {{ t('common.loading') }}
      </div>
      <div v-else-if="error" class="meeting-error">{{ t('meeting.list.loadFailed') }}</div>
      <div v-else-if="sessions.length === 0" class="meeting-empty">
        <component :is="MEETING_ICON" :size="28" />
        <span>{{ t('meeting.list.empty') }}</span>
      </div>
      <div v-else class="meeting-list">
        <div
          v-for="r in rows"
          :key="r.record.token"
          class="meeting-row"
          :data-token="r.record.token"
          :data-state="r.state.kind"
          draggable="true"
          @click="emit('open', r.record.token)"
          @dragstart="onDragStart($event, r.record)"
          @dragend="cleanupDragGhost()"
        >
          <!-- Title and status share the first line; the meta line underneath
               carries the numbers. Two lines, one card, no chevron: the whole
               row is the hit target, an arrow only repeated that fact. -->
          <div class="meeting-row__main">
            <div class="meeting-row__line">
              <span class="meeting-row__title">{{ r.record.title || t('meeting.list.untitled') }}</span>
              <span class="meeting-chip" :class="r.state.cls">
                <Check v-if="r.state.icon === 'check'" :size="10" />
                <AlertTriangle v-else-if="r.state.icon === 'alert'" :size="10" />
                <span v-if="r.state.dot" class="meeting-chip__dot" />
                {{ r.state.label }}
              </span>
            </div>
            <div class="meeting-row__meta">
              <span class="meeting-row__m">{{ formatWhen(r.record.startedAt) }}</span>
              <template v-if="r.duration">
                <span class="meeting-row__sep">·</span>
                <span class="meeting-row__m" :class="{ 'is-accent': !r.record.endedAt }">{{ r.duration }}</span>
              </template>
              <template v-if="r.record.hasMeetingAudio">
                <span class="meeting-row__sep">·</span>
                <span class="meeting-row__m" :title="t('meeting.list.audioMerged')">
                  <Mic :size="11" />{{ t('meeting.list.audioMerged') }}
                </span>
              </template>
              <template v-else-if="r.record.audioFiles > 0">
                <span class="meeting-row__sep">·</span>
                <span class="meeting-row__m" :title="t('meeting.list.audioCount', { n: r.record.audioFiles })">
                  <Mic :size="11" />{{ t('meeting.list.audioShort', { n: r.record.audioFiles }) }}
                </span>
              </template>
              <template v-if="r.record.chatCount > 0">
                <span class="meeting-row__sep">·</span>
                <span class="meeting-row__m" :title="t('meeting.list.chatCount', { n: r.record.chatCount })">
                  <MessageSquare :size="11" />{{ t('meeting.list.chatShort', { n: r.record.chatCount }) }}
                </span>
              </template>
              <template v-if="r.record.hasRevised">
                <span class="meeting-row__sep">·</span>
                <span class="meeting-row__m"><PenLine :size="11" />{{ t('meeting.list.revised') }}</span>
              </template>
            </div>
          </div>
          <!-- Always visible, never hover-only: a phone has no hover, so the
               delete affordance used to be unreachable there. -->
          <button
            class="meeting-row__del"
            type="button"
            :title="t('meeting.list.delete')"
            :data-action="`meeting-delete-${r.record.token}`"
            @click.stop="askDelete(r.record)"
          >
            <Trash2 :size="13" />
          </button>
        </div>
      </div>
      <div class="meeting-hint">{{ t('meeting.list.hint') }}</div>
    </div>

    <!-- Footer actions use the shared .fbtn pills, the same language the task
         tab's action bar uses — see design-guide §按钮. -->
    <div class="meeting-footer">
      <button class="fbtn" type="button" data-action="meeting-join" @click="emit('join')">
        <LinkIcon :size="14" />{{ t('meeting.list.join') }}
      </button>
      <button class="fbtn fbtn-primary" type="button" data-action="meeting-create" @click="emit('create')">
        <Plus :size="14" />{{ t('meeting.list.create') }}
      </button>
    </div>
  </div>
</template>

<script setup lang="ts">
/**
 * The 视频会议 tab's landing view: the project's meeting records.
 *
 * A record is a database row keyed by token, created when the publisher
 * attaches — so a meeting that recorded nothing is still listed. Clicking a row
 * opens its detail; dragging one stages a quote in the chat (the same gesture
 * git history and the forge lists use).
 */
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import {
  AlertTriangle,
  Check,
  Link as LinkIcon,
  MessageSquare,
  Mic,
  PenLine,
  Plus,
  Trash2,
} from 'lucide-vue-next'
import { MEETING_ICON } from '@/utils/meetingIcon'
import LoadingIndicator from '@/components/common/LoadingIndicator.vue'
import RefreshButton from '@/components/common/RefreshButton.vue'
import { useDialog } from '@/composables/useDialog'
import { useToast } from '@/composables/useToast'
import {
  isStaleGenerating,
  meetingQuoteLabel,
  useMeetingHistory,
  type MeetingRecord,
} from '@/composables/useMeetingHistory'
import { meetingDragPayload, startQuoteDrag } from '@/utils/quoteDrag'
import { cleanupDragGhost } from '@/utils/attachDrag'
import { appLog } from '@/utils/appLog'
import '@/assets/modal-footer-btn.css'

const props = defineProps<{ active?: boolean }>()
const emit = defineEmits<{
  create: []
  join: []
  open: [token: string]
}>()

const { t, locale } = useI18n()
const dialog = useDialog()
const toast = useToast()
const history = useMeetingHistory()
const sessions = computed(() => history.sessions.value)
const loading = computed(() => history.loading.value)
const error = computed(() => history.error.value)

/** `11/01 17:35` — the list's own stamp; the detail spells it out in full. */
function formatWhen(ms: number): string {
  if (!ms) return ''
  const d = new Date(ms)
  if (Number.isNaN(d.getTime())) return ''
  return d.toLocaleString(locale.value, {
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
  })
}

/**
 * A live meeting's clock ticks, so the row has to be able to re-render without
 * a reload. Only running rows read it, and the timer only exists while at least
 * one row is running.
 */
const now = ref(Date.now())
let clock: ReturnType<typeof setInterval> | null = null
const anyRunning = computed(() => sessions.value.some((s) => !s.endedAt && s.startedAt > 0))

watch(
  anyRunning,
  (running) => {
    if (running && !clock) {
      clock = setInterval(() => { now.value = Date.now() }, 30_000)
    } else if (!running && clock) {
      clearInterval(clock)
      clock = null
    }
  },
  { immediate: true },
)
onBeforeUnmount(() => {
  if (clock) clearInterval(clock)
  clock = null
})

/**
 * `48 分钟` — measured to the end for a finished meeting, to the clock for a
 * running one. A record still marked live a day later is a leftover from a
 * crash, not a meeting: showing "1440 分钟" would invent a fact.
 */
function durationOf(s: MeetingRecord): string {
  if (!s.startedAt) return ''
  const end = s.endedAt || now.value
  if (end <= s.startedAt) return ''
  if (!s.endedAt && end - s.startedAt > 86_400_000) return ''
  const minutes = Math.max(1, Math.round((end - s.startedAt) / 60000))
  return t('meeting.list.duration', { n: minutes })
}

interface RowState {
  /** Drives the 2px edge stripe — only states that need attention get one. */
  kind: '' | 'running' | 'generating' | 'stale' | 'failed'
  cls: string
  label: string
  icon?: 'check' | 'alert'
  dot?: boolean
}

/**
 * One chip, not four stacked tags: at a glance the user needs to know whether
 * this record is worth opening (minutes? only a transcript? nothing?) and
 * whether it needs attention (still running, generating, or broken).
 *
 * The failed/interrupted branches sit above the artifact branches on purpose:
 * a job that died leaves no minutes, so without them it renders as a calm
 * "no artifacts" — the one reading that is actively wrong.
 */
function rowState(s: MeetingRecord): RowState {
  if (!s.endedAt) {
    return { kind: 'running', cls: 'is-accent', label: t('meeting.list.running'), dot: true }
  }
  if (s.status === 'failed') {
    return { kind: 'failed', cls: 'is-danger', label: t('meeting.list.failed'), icon: 'alert' }
  }
  if (isStaleGenerating(s)) {
    return { kind: 'stale', cls: 'is-warn', label: t('meeting.list.stale'), icon: 'alert' }
  }
  if (s.status === 'generating') {
    return { kind: 'generating', cls: 'is-accent', label: t('meeting.list.generating') }
  }
  if (s.hasMinutes) {
    return { kind: '', cls: 'is-ok', label: t('meeting.list.hasMinutes'), icon: 'check' }
  }
  if (s.hasTranscript) {
    return { kind: '', cls: 'is-warn', label: t('meeting.list.onlyTranscript') }
  }
  return { kind: '', cls: 'is-muted', label: t('meeting.list.noArtifacts') }
}

const rows = computed(() =>
  sessions.value.map((record) => ({
    record,
    state: rowState(record),
    duration: durationOf(record),
  })),
)

function reload(): void {
  void history.load()
}

function onDragStart(e: DragEvent, s: MeetingRecord): void {
  const payload = meetingDragPayload({ token: s.token, label: meetingQuoteLabel(s) })
  // The drop handler (App) reads this payload and fills the text in; nothing is
  // staged until the user actually drops it on the chat column.
  if (!startQuoteDrag(e, payload)) e.preventDefault()
}

async function askDelete(s: MeetingRecord): Promise<void> {
  const ok = await dialog.confirm(
    t('meeting.list.deleteConfirm', { title: s.title || t('meeting.list.untitled') }),
    { dangerous: true },
  )
  if (!ok) return
  const res = await history.remove(s.token)
  if (res.ok) return
  appLog.w('MeetingList', 'delete failed', res.reason)
  // A 409 (still running / minutes generating) would otherwise look like the
  // click did nothing.
  toast.show(
    res.reason === 'busy' ? t('meeting.detail.deleteBusy') : t('meeting.detail.deleteFailed'),
    { type: 'error', icon: 'warning' },
  )
}

// Switching back to the tab re-reads the list: a meeting that finished while
// the user was elsewhere must appear without a manual refresh.
watch(
  () => props.active,
  (active) => {
    if (active) reload()
  },
)

onMounted(() => {
  if (props.active !== false) reload()
})
</script>

<style scoped>
.meeting-panel {
  display: flex;
  flex-direction: column;
  flex: 1;
  min-height: 0;
  overflow: hidden;
  background: var(--bg-primary, #ffffff);
}

.meeting-header {
  display: flex;
  align-items: center;
  gap: var(--space-3, 6px);
  height: var(--header-height);
  padding: 0 var(--space-5, 10px) 0 var(--space-6, 12px);
  border-bottom: 1px solid var(--border-color, #e5e5e5);
  flex-shrink: 0;
}

.meeting-header__icon { display: flex; color: var(--text-secondary, #666); }
.meeting-header__title {
  font-size: var(--font-size-md);
  font-weight: var(--font-weight-semibold);
  color: var(--text-primary, #1a1a1a);
}
/* Shape and geometry come from the shared .count-badge; only the colour is
   local so the pill matches this tab's muted header. */
.meeting-header__count {
  background: var(--bg-tertiary);
  color: var(--text-muted);
  font-weight: var(--font-weight-bold);
}
.meeting-header__spacer { flex: 1; }
/* Press feedback matches the other panel headers' refresh buttons (the shared
   .drilldown-refresh-btn / .forge-header-btn scale). The spin + green check
   come from RefreshButton itself. */
.meeting-refresh-btn {
  transition: transform var(--duration-slow) ease, background var(--duration-slow) ease,
    color var(--duration-slow) ease;
}
.meeting-refresh-btn:active:not(:disabled) { transform: scale(0.9); }

.meeting-body {
  flex: 1;
  min-height: 0;
  overflow-y: auto;
  display: flex;
  flex-direction: column;
  gap: var(--space-5, 10px);
  padding: var(--space-4, 8px) var(--space-5, 10px) var(--space-6, 12px);
}

.meeting-list { display: flex; flex-direction: column; gap: var(--space-4, 8px); }

/* Square card, hairline border, 8px gap — the task tab's row language. */
.meeting-row {
  position: relative;
  display: flex;
  align-items: center;
  gap: var(--space-4, 8px);
  padding: var(--space-3, 6px) var(--space-4, 8px) var(--space-3, 6px) var(--space-5, 10px);
  border: 1px solid var(--border-color, #e5e5e5);
  border-radius: 0;
  background: var(--bg-secondary, #f8f9fa);
  cursor: pointer;
  transition: background var(--duration-base) ease, border-color var(--duration-base) ease,
    box-shadow var(--duration-base) ease;
}
@media (hover: hover) {
  .meeting-row:hover {
    background: var(--bg-hover);
    border-color: var(--accent-color, #4a90d9);
    box-shadow: 0 calc(2 * var(--dp)) calc(8 * var(--dp)) rgba(0, 0, 0, 0.06);
  }
}
.meeting-row:active { background: var(--bg-tertiary); }

/* The stripe overlays the card's own left border so it costs no layout shift. */
.meeting-row::before {
  content: "";
  position: absolute;
  left: -1px;
  top: -1px;
  bottom: -1px;
  width: 2px;
  background: transparent;
}
.meeting-row[data-state="running"]::before { background: var(--accent-color); }
.meeting-row[data-state="generating"]::before { background: var(--color-info); }
.meeting-row[data-state="stale"]::before { background: var(--color-orange); }
.meeting-row[data-state="failed"]::before { background: var(--color-red); }

.meeting-row__main {
  flex: 1;
  min-width: 0;
  display: flex;
  flex-direction: column;
  gap: 1px;
}
.meeting-row__line {
  display: flex;
  align-items: center;
  gap: var(--space-3, 6px);
  min-width: 0;
}
.meeting-row__title {
  flex: 1;
  min-width: 0;
  font-size: var(--font-size-sm);
  font-weight: var(--font-weight-medium);
  line-height: calc(16 * var(--dp));
  color: var(--text-primary);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
.meeting-row__meta {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  row-gap: 1px;
  font-size: var(--font-size-xs);
  line-height: calc(15 * var(--dp));
  color: var(--text-muted);
  /* Times and counts line up column-to-column instead of jittering. */
  font-variant-numeric: tabular-nums;
  min-width: 0;
}
.meeting-row__m {
  display: inline-flex;
  align-items: center;
  gap: calc(3 * var(--dp));
  white-space: nowrap;
}
.meeting-row__m svg { flex-shrink: 0; opacity: 0.85; }
.meeting-row__m.is-accent { color: var(--accent-color); }
.meeting-row__sep { padding: 0 var(--space-2, 4px); color: var(--text-hint); }

/* Square, uppercase-ish, one per row. Colour is a secondary cue: the label
   carries the meaning. */
.meeting-chip {
  display: inline-flex;
  align-items: center;
  gap: calc(3 * var(--dp));
  height: calc(17 * var(--dp));
  padding: 0 var(--space-3, 6px);
  border: 1px solid transparent;
  border-radius: 0;
  font-size: var(--font-size-2xs);
  font-weight: var(--font-weight-semibold);
  letter-spacing: 0.02em;
  white-space: nowrap;
  flex-shrink: 0;
}
.meeting-chip.is-ok {
  color: var(--color-green);
  background: color-mix(in srgb, var(--color-green) 13%, transparent);
  border-color: color-mix(in srgb, var(--color-green) 38%, transparent);
}
.meeting-chip.is-warn {
  color: var(--color-orange);
  background: color-mix(in srgb, var(--color-orange) 13%, transparent);
  border-color: color-mix(in srgb, var(--color-orange) 38%, transparent);
}
.meeting-chip.is-danger {
  color: var(--color-red);
  background: color-mix(in srgb, var(--color-red) 13%, transparent);
  border-color: color-mix(in srgb, var(--color-red) 38%, transparent);
}
.meeting-chip.is-accent {
  color: var(--accent-color);
  background: color-mix(in srgb, var(--accent-color) 13%, transparent);
  border-color: color-mix(in srgb, var(--accent-color) 40%, transparent);
}
.meeting-chip.is-muted {
  color: var(--text-muted);
  border-color: var(--border-color);
}
.meeting-chip__dot {
  width: calc(5 * var(--dp));
  height: calc(5 * var(--dp));
  border-radius: 50%;
  background: currentColor;
  animation: meeting-chip-pulse 1.4s ease-in-out infinite;
}
@keyframes meeting-chip-pulse {
  0%, 100% { opacity: 1; }
  50% { opacity: 0.25; }
}

/* Always visible (a phone has no hover); only the colour waits for hover. */
.meeting-row__del {
  width: calc(26 * var(--dp));
  height: calc(26 * var(--dp));
  display: inline-flex;
  align-items: center;
  justify-content: center;
  padding: 0;
  border: 1px solid transparent;
  border-radius: 0;
  background: transparent;
  color: var(--text-muted);
  cursor: pointer;
  flex-shrink: 0;
  transition: color var(--duration-base) ease, background var(--duration-base) ease,
    border-color var(--duration-base) ease;
}
@media (hover: hover) {
  .meeting-row:hover .meeting-row__del { color: var(--text-secondary); }
  .meeting-row__del:hover {
    color: var(--color-red);
    background: color-mix(in srgb, var(--color-red) 12%, transparent);
    border-color: color-mix(in srgb, var(--color-red) 35%, transparent);
  }
}
.meeting-row__del:active { transform: scale(0.92); }

.meeting-hint {
  display: flex;
  align-items: center;
  gap: var(--space-2, 4px);
  font-size: var(--font-size-xs);
  line-height: 1.6;
  color: var(--text-muted);
}
.meeting-error {
  padding: var(--space-4, 8px) var(--space-5, 10px);
  border-radius: var(--radius-md);
  background: color-mix(in srgb, var(--color-red) 10%, transparent);
  color: var(--color-red);
  font-size: var(--font-size-sm);
}
.meeting-empty {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: var(--space-3, 6px);
  padding: var(--space-8, 20px) 0;
  color: var(--text-muted);
  font-size: var(--font-size-sm);
}

.meeting-icon-btn {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: calc(26 * var(--dp));
  height: calc(26 * var(--dp));
  border: none;
  border-radius: 0;
  background: transparent;
  color: var(--text-muted);
  cursor: pointer;
  flex-shrink: 0;
}
.meeting-icon-btn:hover { background: var(--bg-hover); color: var(--text-primary); }
.meeting-icon-btn:disabled { opacity: 0.5; cursor: default; }

.meeting-footer {
  display: flex;
  flex-wrap: nowrap;
  justify-content: flex-end;
  justify-content: safe flex-end;
  gap: var(--space-3, 6px);
  padding: var(--space-3, 6px) var(--space-4, 8px);
  border-top: 1px solid var(--border-color);
  background: var(--bg-primary);
  flex-shrink: 0;
  overflow-x: auto;
  overflow-y: hidden;
}
</style>
