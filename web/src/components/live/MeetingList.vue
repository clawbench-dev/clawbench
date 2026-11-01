<template>
  <div class="meeting-panel">
    <div class="meeting-header">
      <span class="meeting-header__icon"><component :is="MEETING_ICON" :size="14" /></span>
      <span class="meeting-header__title">{{ t('meeting.title') }}</span>
      <span class="meeting-header__spacer" />
    </div>

    <div class="meeting-toolbar">
      <span class="meeting-toolbar__title">{{ t('meeting.list.title') }}</span>
      <span class="meeting-toolbar__count">{{ t('meeting.list.count', { n: sessions.length }) }}</span>
      <span class="meeting-toolbar__spacer" />
      <button class="meeting-icon-btn" type="button" :title="t('meeting.list.refresh')" :disabled="loading" @click="reload">
        <RefreshCw :size="14" />
      </button>
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
          v-for="s in sessions"
          :key="s.token"
          class="meeting-row"
          :data-token="s.token"
          draggable="true"
          @click="emit('open', s.token)"
          @dragstart="onDragStart($event, s)"
          @dragend="cleanupDragGhost()"
        >
          <div class="meeting-row__main">
            <span class="meeting-row__title">{{ s.title || t('meeting.list.untitled') }}</span>
            <span class="meeting-row__meta">
              <span>{{ formatWhen(s.startedAt) }}</span>
              <span v-if="durationOf(s)">{{ durationOf(s) }}</span>
              <span v-if="s.audioFiles > 0">{{ t('meeting.list.audioCount', { n: s.audioFiles }) }}</span>
              <span v-if="s.chatCount > 0">{{ t('meeting.list.chatCount', { n: s.chatCount }) }}</span>
            </span>
          </div>
          <div class="meeting-row__tags">
            <span v-if="!s.endedAt" class="meeting-chip meeting-chip--accent">{{ t('meeting.list.running') }}</span>
            <span v-else-if="s.status === 'generating'" class="meeting-chip meeting-chip--accent">
              {{ t('meeting.list.generating') }}
            </span>
            <span v-else-if="s.hasMinutes" class="meeting-chip meeting-chip--ok">
              <Check :size="10" />{{ t('meeting.list.hasMinutes') }}
            </span>
            <span v-else-if="s.hasTranscript" class="meeting-chip meeting-chip--warn">
              {{ t('meeting.list.onlyTranscript') }}
            </span>
            <span v-else class="meeting-chip meeting-chip--muted">{{ t('meeting.list.noArtifacts') }}</span>
          </div>
          <button
            class="meeting-icon-btn meeting-row__del"
            type="button"
            :title="t('meeting.list.delete')"
            :data-action="`meeting-delete-${s.token}`"
            @click.stop="askDelete(s)"
          >
            <Trash2 :size="14" />
          </button>
          <ChevronRight :size="14" class="meeting-row__chev" />
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
 * The 会议 tab's landing view: the project's meeting records.
 *
 * A record is a database row keyed by token, created when the publisher
 * attaches — so a meeting that recorded nothing is still listed. Clicking a row
 * opens its detail; dragging one stages a quote in the chat (the same gesture
 * git history and the forge lists use).
 */
import { computed, onMounted, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { Check, ChevronRight, Link as LinkIcon, Plus, RefreshCw, Trash2 } from 'lucide-vue-next'
import { MEETING_ICON } from '@/utils/meetingIcon'
import LoadingIndicator from '@/components/common/LoadingIndicator.vue'
import { useDialog } from '@/composables/useDialog'
import { useToast } from '@/composables/useToast'
import { meetingQuoteLabel, useMeetingHistory, type MeetingRecord } from '@/composables/useMeetingHistory'
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

function reload(): void {
  void history.load()
}

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

/** `48 分钟` — only when the meeting actually ended. */
function durationOf(s: MeetingRecord): string {
  if (!s.endedAt || !s.startedAt || s.endedAt <= s.startedAt) return ''
  const minutes = Math.max(1, Math.round((s.endedAt - s.startedAt) / 60000))
  return t('meeting.list.duration', { n: minutes })
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
.meeting-header__spacer { flex: 1; }

.meeting-toolbar {
  display: flex;
  align-items: center;
  gap: var(--space-3, 6px);
  padding: var(--space-5, 10px) var(--space-6, 12px);
  border-bottom: 1px solid var(--border-color, #e5e5e5);
  flex-shrink: 0;
}
.meeting-toolbar__title { font-size: var(--font-size-sm); font-weight: 600; }
.meeting-toolbar__count { font-size: var(--font-size-xs); color: var(--text-muted); }
.meeting-toolbar__spacer { flex: 1; }

.meeting-body {
  flex: 1;
  min-height: 0;
  overflow-y: auto;
  display: flex;
  flex-direction: column;
  gap: var(--space-5, 10px);
  padding: var(--space-6, 12px) var(--space-7, 16px);
}

.meeting-list { display: flex; flex-direction: column; gap: var(--space-2, 4px); }

.meeting-row {
  position: relative;
  display: flex;
  align-items: center;
  gap: var(--space-4, 8px);
  padding: var(--space-4, 8px) var(--space-5, 10px);
  border: 1px solid transparent;
  border-radius: var(--radius-md, 10px);
  background: var(--bg-secondary, #f8f9fa);
  cursor: pointer;
}
.meeting-row:hover { border-color: var(--accent-color, #4a90d9); }
.meeting-row__main { flex: 1; min-width: 0; display: flex; flex-direction: column; gap: var(--space-1, 2px); }
.meeting-row__title {
  font-size: var(--font-size-sm);
  font-weight: 500;
  color: var(--text-primary);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
.meeting-row__meta {
  display: flex;
  flex-wrap: wrap;
  gap: var(--space-3, 6px);
  font-size: var(--font-size-xs);
  color: var(--text-muted);
}
.meeting-row__tags { display: flex; align-items: center; gap: var(--space-2, 4px); flex-shrink: 0; }
.meeting-row__del { opacity: 0; transition: opacity 0.12s ease; }
.meeting-row:hover .meeting-row__del { opacity: 1; }
.meeting-row__chev { color: var(--text-hint); flex-shrink: 0; }

.meeting-chip {
  display: inline-flex;
  align-items: center;
  gap: var(--space-1, 2px);
  height: calc(18 * var(--dp));
  padding: 0 var(--space-3, 6px);
  border-radius: var(--radius-xs, 3px);
  background: var(--bg-tertiary);
  color: var(--text-secondary);
  font-size: var(--font-size-2xs);
  white-space: nowrap;
}
.meeting-chip--ok { background: color-mix(in srgb, var(--color-green) 12%, transparent); color: var(--color-green); }
.meeting-chip--warn { background: color-mix(in srgb, var(--color-orange) 12%, transparent); color: var(--color-orange); }
.meeting-chip--muted { color: var(--text-muted); }
.meeting-chip--accent { background: color-mix(in srgb, var(--accent-color) 12%, transparent); color: var(--accent-color); }

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
  border-radius: var(--radius-sm, 6px);
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
