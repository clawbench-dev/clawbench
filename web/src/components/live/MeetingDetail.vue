<template>
  <div class="meeting-panel">
    <div class="meeting-header">
      <button class="meeting-icon-btn" type="button" :title="t('meeting.detail.back')" data-action="meeting-back" @click="emit('back')">
        <ArrowLeft :size="14" />
      </button>
      <span
        class="meeting-header__title"
        :draggable="Boolean(record)"
        :title="t('meeting.detail.dragHint')"
        @dragstart="onDragStart"
        @dragend="cleanupDragGhost()"
      >{{ record?.title || t('meeting.list.untitled') }}</span>
      <span class="meeting-header__spacer" />
      <button
        v-if="record"
        class="meeting-icon-btn"
        type="button"
        :title="t('meeting.detail.quote')"
        data-action="meeting-quote"
        @click="onQuote"
      >
        <MessageSquare :size="14" />
      </button>
    </div>

    <div v-if="loading" class="meeting-body meeting-center">
      <LoadingIndicator size="sm" inline />
      {{ t('common.loading') }}
    </div>
    <div v-else-if="!record" class="meeting-body meeting-center">
      <span class="meeting-hint">{{ t('meeting.detail.notFound') }}</span>
    </div>
    <template v-else>
      <div class="meeting-meta">
        <span>{{ formatWhen(record.startedAt) }}</span>
        <span v-if="durationLabel">· {{ durationLabel }}</span>
        <span>· {{ record.chatCount > 0 ? t('meeting.detail.chatCount', { n: record.chatCount }) : t('meeting.detail.noChat') }}</span>
      </div>

      <div class="meeting-tabs">
        <button
          v-for="tab in tabs"
          :key="tab.key"
          class="meeting-tab"
          :class="{ 'meeting-tab--on': activeTab === tab.key }"
          type="button"
          :data-tab="tab.key"
          @click="selectTab(tab.key)"
        >
          {{ tab.label }}
          <!-- The badge counts what is actually loaded, not the server's
               total: a badge that disagrees with the list below it is worse
               than no badge. -->
          <span v-if="tab.key === 'chat' && chat.length > 0" class="meeting-tab__count">
            {{ chat.length > 99 ? '99+' : chat.length }}
          </span>
        </button>
      </div>

      <div
        class="meeting-body"
        :data-quote-source="quoteSource"
        :data-quote-language="activeTab === 'chat' ? 'chat' : 'meeting'"
        :data-quote-meeting-token="record?.token || ''"
        @click="onBodyClick"
      >
        <template v-if="activeTab === 'minutes'">
          <div v-if="generating" class="meeting-generating">
            <div class="meeting-generating__row"><span class="meeting-spinner" />{{ t('liveShare.notes.generating') }}</div>
          </div>
          <div v-else-if="staleGenerating" class="meeting-warn">
            {{ t('meeting.detail.generatingStale') }}
            <button class="meeting-link" type="button" @click="regenerate">{{ t('common.retry') }}</button>
          </div>
          <div v-else-if="minutesFailed" class="meeting-error">{{ t('liveShare.notes.generateFailed') }}</div>
          <div v-else-if="minutesHtml" class="meeting-md markdown-body" v-html="minutesHtml" />
          <div v-else class="meeting-hint">{{ t('meeting.detail.noMinutes') }}</div>

          <div v-if="artifactRows.length" class="meeting-artifacts">
            <div class="meeting-artifacts__label">{{ t('liveShare.notes.savedTo') }}</div>
            <div v-for="row in artifactRows" :key="row.key" class="meeting-file">
              <span class="meeting-file__label">{{ row.label }}</span>
              <span class="meeting-file__path" :title="row.path">{{ row.path }}</span>
              <button class="meeting-btn" type="button" @click="copy(row.path)">{{ t('liveShare.notes.copyPath') }}</button>
            </div>
          </div>
        </template>

        <template v-else-if="activeTab === 'transcript'">
          <!-- SRT is plain text: running it through the Markdown renderer turns
               cue numbers into headings and swallows the arrow in a timestamp
               line. Do NOT write that arrow as its two-dash form here: an HTML
               comment ends at the first close marker, so the rest of this
               comment would leak into the pane as visible text. -->
          <pre v-if="transcriptText" class="meeting-transcript" data-role="transcript">{{ transcriptText }}</pre>
          <div v-else class="meeting-hint">{{ t('meeting.detail.noTranscript') }}</div>
        </template>

        <template v-else>
          <div v-if="chat.length === 0" class="meeting-hint">{{ t('meeting.detail.noChatMessages') }}</div>
          <div v-else class="meeting-chat">
            <div v-for="m in chat" :key="m.id" class="meeting-chat__row">
              <div class="meeting-chat__head">
                <span class="meeting-chat__who">{{ m.speaker || t('meeting.detail.anonymous') }}</span>
                <span>{{ formatClock(m.at) }}</span>
              </div>
              <div class="meeting-chat__text">{{ m.text }}</div>
            </div>
          </div>
        </template>
      </div>

      <div class="meeting-footer">
        <button class="fbtn fbtn-danger" type="button" data-action="meeting-delete" @click="askDelete">
          <Trash2 :size="14" />{{ t('meeting.detail.delete') }}
        </button>
        <span class="meeting-header__spacer" />
        <button v-if="activeTab !== 'chat'" class="fbtn" type="button" :disabled="!currentText" @click="openFile">
          <ExternalLink :size="14" />{{ t('liveShare.history.openInEditor') }}
        </button>
        <button class="fbtn" type="button" :disabled="!copyText" @click="copy(copyText)">
          <Copy :size="14" />{{ activeTab === 'chat' ? t('meeting.detail.copyAll') : t('liveShare.notes.copy') }}
        </button>
      </div>
    </template>
  </div>
</template>

<script setup lang="ts">
/**
 * The meeting record detail — the same view for "a meeting that just ended" and "a record
 * clicked in the list". Ending a meeting navigates here; there is no separate
 * post-meeting screen.
 *
 * Chat is read from the database (permanent). The artifacts are on disk and are
 * listed only when they exist: a meeting with AI notes off legitimately has none.
 */
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { ArrowLeft, Copy, ExternalLink, MessageSquare, Trash2 } from 'lucide-vue-next'
import LoadingIndicator from '@/components/common/LoadingIndicator.vue'
import { useDialog } from '@/composables/useDialog'
import { useToast } from '@/composables/useToast'
import { renderMarkdownHtml } from '@/composables/useMarkdownRenderer'
import {
  isStaleGenerating,
  meetingQuoteLabel,
  useMeetingHistory,
  type MeetingChatMessage,
  type MeetingRecord,
} from '@/composables/useMeetingHistory'
import { cleanupDragGhost } from '@/utils/attachDrag'
import { meetingDragPayload, startQuoteDrag } from '@/utils/quoteDrag'
import { appLog } from '@/utils/appLog'
import '@/assets/modal-footer-btn.css'

const props = defineProps<{ token: string }>()
const emit = defineEmits<{ back: []; quote: [token: string]; 'open-file': [path: string] }>()

const { t, locale } = useI18n()
const toast = useToast()
const dialog = useDialog()
const history = useMeetingHistory()

type Tab = 'minutes' | 'transcript' | 'chat'
const activeTab = ref<Tab>('minutes')
const record = ref<MeetingRecord | null>(null)
const loading = ref(true)
const minutesText = ref('')
const transcriptText = ref('')
const transcriptFile = ref('')
const chat = ref<MeetingChatMessage[]>([])

const tabs = computed(() => [
  { key: 'minutes' as Tab, label: t('meeting.detail.tabMinutes') },
  { key: 'transcript' as Tab, label: t('meeting.detail.tabTranscript') },
  { key: 'chat' as Tab, label: t('meeting.detail.tabChat') },
])

const generating = computed(() => record.value?.status === 'generating' && !isStaleGenerating(record.value))
const staleGenerating = computed(() => (record.value ? isStaleGenerating(record.value) : false))
const minutesFailed = computed(() => record.value?.status === 'failed')

const minutesHtml = computed(() => (minutesText.value ? renderMarkdownHtml(minutesText.value) : ''))


const quoteSource = computed(() => {
  const r = record.value
  if (!r) return ''
  const d = new Date(r.startedAt)
  const stamp = `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')}`
  return t('meeting.quote.source', { title: r.title || t('meeting.list.untitled'), date: stamp })
})

const durationLabel = computed(() => {
  const r = record.value
  if (!r?.endedAt || !r.startedAt || r.endedAt <= r.startedAt) return ''
  const minutes = Math.max(1, Math.round((r.endedAt - r.startedAt) / 60000))
  return t('meeting.list.duration', { n: minutes })
})

/** Files this meeting actually left on disk, in reading order. */
const artifactRows = computed(() => {
  const r = record.value
  if (!r?.dirPath) return []
  const sep = r.dirPath.includes('\\') ? '\\' : '/'
  const rows: Array<{ key: string; label: string; path: string }> = []
  if (r.hasMinutes) rows.push({ key: 'minutes', label: t('liveShare.notes.artifactMinutes'), path: `${r.dirPath}${sep}minutes.md` })
  if (r.hasTranscript) {
    rows.push({
      key: 'transcript',
      label: t('liveShare.notes.artifactTranscript'),
      path: `${r.dirPath}${sep}${transcriptFile.value || 'transcript.srt'}`,
    })
  }
  if (r.hasRevised) rows.push({ key: 'revised', label: t('liveShare.notes.artifactRevised'), path: `${r.dirPath}${sep}transcript-revised.md` })
  if (r.audioFiles > 0) rows.push({ key: 'audio', label: t('liveShare.notes.artifactAudio', { n: r.audioFiles }), path: `${r.dirPath}${sep}audio` })
  return rows
})

const currentText = computed(() => (activeTab.value === 'transcript' ? transcriptText.value : minutesText.value))
const copyText = computed(() => {
  if (activeTab.value === 'chat') {
    return chat.value
      .map((m) =>
        t('meeting.detail.chatLine', {
          time: formatClock(m.at),
          speaker: m.speaker || t('meeting.detail.anonymous'),
          text: m.text,
        }),
      )
      .join('\n')
  }
  return currentText.value
})

function formatWhen(ms: number): string {
  if (!ms) return ''
  return new Date(ms).toLocaleString(locale.value, {
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
  })
}

function formatClock(ms: number): string {
  if (!ms) return ''
  const d = new Date(ms)
  return `${String(d.getHours()).padStart(2, '0')}:${String(d.getMinutes()).padStart(2, '0')}`
}

async function load(): Promise<void> {
  if (!props.token) return
  loading.value = true
  try {
    record.value = await history.get(props.token)
    if (!record.value) return
    const [minutes, transcript] = await Promise.all([
      history.artifact(props.token, 'minutes'),
      history.artifact(props.token, 'transcript'),
    ])
    minutesText.value = minutes?.content ?? ''
    transcriptText.value = transcript?.content ?? ''
    transcriptFile.value = transcript?.file ?? ''
    const messages = await history.chat(props.token)
    chat.value = messages.messages
    // The minutes job starts AFTER stop() resolves, i.e. after this view was
    // opened. Without polling, a meeting that just ended would say "no minutes"
    // until the user navigated away and back.
    if (shouldPoll(record.value)) pollMinutes()
  } finally {
    loading.value = false
  }
}

/**
 * Whether the minutes may still arrive.
 *
 * `generating` is obvious. The second case is the just-ended meeting: the
 * publisher drains its upload queue and only then asks for the minutes, so for
 * a while the record is still `idle` although a job is about to start. The
 * window is bounded so an old meeting that never got minutes does not poll
 * forever.
 */
function shouldPoll(r: MeetingRecord | null): boolean {
  if (!r) return false
  if (r.status === 'generating') return true
  return r.status === 'idle' && r.endedAt > 0 && Date.now() - r.endedAt < 10 * 60 * 1000
}

function selectTab(tab: Tab): void {
  activeTab.value = tab
}

function onDragStart(e: DragEvent): void {
  const r = record.value
  if (!r) return
  const payload = meetingDragPayload({ token: r.token, label: meetingQuoteLabel(r) })
  if (!startQuoteDrag(e, payload)) e.preventDefault()
}

function onQuote(): void {
  if (record.value) emit('quote', record.value.token)
}

/** Double-click a block copies it — same affordance as the markdown preview. */
function onBodyClick(e: MouseEvent): void {
  if (e.detail !== 2) return
  const target = e.target as HTMLElement | null
  const text = target?.innerText?.trim()
  if (text) void copy(text)
}

async function copy(text: string): Promise<void> {
  if (!text) return
  try {
    await navigator.clipboard.writeText(text)
    toast.show(t('common.copied'), { type: 'success', icon: 'copy' })
  } catch (e) {
    appLog.w('MeetingDetail', 'copy failed', e)
  }
}

function openFile(): void {
  const r = record.value
  if (!r) return
  if (activeTab.value === 'minutes' && r.hasMinutes) {
    const sep = r.dirPath?.includes('\\') ? '\\' : '/'
    emit('open-file', `${r.dirPath}${sep}minutes.md`)
    return
  }
  if (activeTab.value === 'transcript' && r.hasTranscript) {
    const sep = r.dirPath?.includes('\\') ? '\\' : '/'
    emit('open-file', `${r.dirPath}${sep}${transcriptFile.value || 'transcript.srt'}`)
  }
}

async function regenerate(): Promise<void> {
  if (!record.value) return
  try {
    const resp = await fetch('/api/live-share/notes/minutes', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ token: record.value.token, title: record.value.title, regenerate: true }),
    })
    if (!resp.ok) throw new Error(`minutes failed: ${resp.status}`)
    record.value.status = 'generating'
    record.value.updatedAt = Date.now()
    pollMinutes()
  } catch (e) {
    appLog.w('MeetingDetail', 'regenerate failed', e)
  }
}

let pollTimer: ReturnType<typeof setTimeout> | null = null
function pollMinutes(): void {
  if (pollTimer) clearTimeout(pollTimer)
  pollTimer = setTimeout(() => {
    void (async () => {
      await load()
      if (!shouldPoll(record.value)) {
        if (pollTimer) clearTimeout(pollTimer)
        pollTimer = null
      }
    })()
  }, 2500)
}

onBeforeUnmount(() => {
  if (pollTimer) clearTimeout(pollTimer)
  pollTimer = null
})

async function askDelete(): Promise<void> {
  const r = record.value
  if (!r) return
  const ok = await dialog.confirm(
    t('meeting.detail.deleteConfirm', { title: r.title || t('meeting.list.untitled') }),
    { dangerous: true },
  )
  if (!ok) return
  const res = await history.remove(r.token)
  if (res.ok) {
    emit('back')
    return
  }
  toast.show(
    res.reason === 'busy' ? t('meeting.detail.deleteBusy') : t('meeting.detail.deleteFailed'),
    { type: 'error', icon: 'warning' },
  )
}

watch(
  () => props.token,
  () => {
    activeTab.value = 'minutes'
    void load()
  },
)

onMounted(() => {
  void load()
})
</script>

<style scoped>
.meeting-panel { display: flex; flex-direction: column; flex: 1; min-height: 0; overflow: hidden; background: var(--bg-primary); }
.meeting-header {
  display: flex; align-items: center; gap: var(--space-3);
  height: var(--header-height); padding: 0 var(--space-5) 0 var(--space-6);
  border-bottom: 1px solid var(--border-color); flex-shrink: 0;
}
.meeting-header__title {
  font-size: var(--font-size-md); font-weight: 600; color: var(--text-primary);
  white-space: nowrap; overflow: hidden; text-overflow: ellipsis;
}
.meeting-header__spacer { flex: 1; }
.meeting-icon-btn {
  display: inline-flex; align-items: center; justify-content: center;
  width: calc(26 * var(--dp)); height: calc(26 * var(--dp));
  border: none; border-radius: var(--radius-sm); background: transparent;
  color: var(--text-muted); cursor: pointer; flex-shrink: 0;
}
.meeting-icon-btn:hover { background: var(--bg-hover); color: var(--text-primary); }

.meeting-meta {
  display: flex; flex-wrap: wrap; gap: var(--space-2);
  padding: var(--space-3) var(--space-7);
  font-size: var(--font-size-xs); color: var(--text-muted);
  border-bottom: 1px solid var(--border-color);
  flex-shrink: 0;
}

.meeting-tabs {
  display: flex; flex-shrink: 0;
  border-bottom: 1px solid var(--border-color);
  background: var(--bg-secondary);
}
.meeting-tab {
  display: inline-flex; flex: 1 1 0; align-items: center; justify-content: center;
  gap: var(--space-2);
  height: calc(34 * var(--dp)); padding: 0 var(--space-5);
  border: none; border-bottom: 2px solid transparent; background: transparent;
  color: var(--text-secondary); font-size: var(--font-size-sm); cursor: pointer;
}
.meeting-tab:hover { color: var(--text-primary); }
.meeting-tab--on { color: var(--accent-color); border-bottom-color: var(--accent-color); font-weight: 600; }
.meeting-tab__count {
  min-width: calc(14 * var(--dp)); height: calc(14 * var(--dp));
  padding: 0 var(--space-1); border-radius: var(--radius-full);
  background: var(--accent-color); color: #fff;
  font-size: var(--font-size-2xs); line-height: calc(14 * var(--dp)); text-align: center;
}

.meeting-body {
  flex: 1; min-height: 0; overflow-y: auto;
  display: flex; flex-direction: column; gap: var(--space-5);
  padding: var(--space-6) var(--space-7);
}
.meeting-center { align-items: center; justify-content: center; color: var(--text-muted); font-size: var(--font-size-sm); }

.meeting-md {
  font-size: var(--font-size-sm); line-height: 1.75; color: var(--text-primary);
  flex: 0 0 auto; overflow: visible; padding: 0;
}
.meeting-md :deep(h3) { margin: var(--space-5) 0 var(--space-2); font-size: var(--font-size-sm); }

.meeting-artifacts { display: flex; flex-direction: column; gap: var(--space-2); }
.meeting-artifacts__label { font-size: var(--font-size-xs); color: var(--text-muted); }
.meeting-file {
  display: flex; align-items: center; gap: var(--space-3); min-width: 0;
  padding: var(--space-3) var(--space-5); border-radius: var(--radius-sm);
  background: var(--bg-tertiary); font-size: var(--font-size-xs);
}
.meeting-file__label { flex: 0 0 calc(76 * var(--dp)); color: var(--text-muted); white-space: nowrap; }
.meeting-file__path {
  flex: 1; min-width: 0; font-family: var(--font-mono);
  color: var(--text-primary); white-space: nowrap; overflow: hidden; text-overflow: ellipsis;
}

.meeting-transcript {
  margin: 0;
  font-family: var(--font-mono, ui-monospace, monospace);
  font-size: var(--font-size-xs);
  line-height: 1.7;
  color: var(--text-primary);
  white-space: pre-wrap;
  overflow-wrap: anywhere;
}

/* The archived transcript is the SAME conversation the live panel showed, so
   it is drawn as the same bubbles. Bare text lines here read as "the styling
   was lost" — the padding/radius/background are what makes a line a message.
   `LiveChatPanel.vue`'s `.live-chat__text` is the other half of this pair;
   `meetingChatStyleParity.test.ts` keeps the two from drifting. */
.meeting-chat { display: flex; flex-direction: column; gap: var(--space-4); }
.meeting-chat__row { display: flex; flex-direction: column; gap: var(--space-1); max-width: 100%; }
.meeting-chat__head { display: flex; gap: var(--space-3); font-size: var(--font-size-xs); color: var(--text-muted); }
.meeting-chat__who { color: var(--text-secondary); font-weight: 500; }
.meeting-chat__text {
  padding: var(--space-3) var(--space-4);
  border-radius: var(--radius-md);
  background: var(--bg-tertiary);
  color: var(--text-primary);
  font-size: var(--font-size-sm);
  line-height: 1.6;
  overflow-wrap: anywhere;
  white-space: pre-wrap;
}

.meeting-generating {
  display: flex; flex-direction: column; gap: var(--space-3);
  padding: var(--space-5); border-radius: var(--radius-md); background: var(--bg-secondary);
}
.meeting-generating__row { display: flex; align-items: center; gap: var(--space-3); font-size: var(--font-size-sm); color: var(--text-secondary); }
.meeting-spinner {
  width: calc(13 * var(--dp)); height: calc(13 * var(--dp));
  border: 2px solid color-mix(in srgb, var(--accent-color) 25%, transparent);
  border-top-color: var(--accent-color); border-radius: 50%;
  animation: meeting-spin 0.8s linear infinite; flex-shrink: 0;
}
@keyframes meeting-spin { to { transform: rotate(360deg); } }

.meeting-hint { display: flex; align-items: center; gap: var(--space-2); font-size: var(--font-size-xs); line-height: 1.6; color: var(--text-muted); }
.meeting-warn {
  padding: var(--space-4) var(--space-5); border-radius: var(--radius-md);
  background: color-mix(in srgb, var(--color-orange) 10%, transparent);
  color: var(--color-orange); font-size: var(--font-size-xs); line-height: 1.6;
}
.meeting-error {
  padding: var(--space-4) var(--space-5); border-radius: var(--radius-md);
  background: color-mix(in srgb, var(--color-red) 10%, transparent);
  color: var(--color-red); font-size: var(--font-size-sm);
}
.meeting-link { border: none; background: none; color: var(--accent-color); cursor: pointer; font-size: inherit; padding: 0; }

.meeting-footer {
  display: flex; flex-wrap: wrap; align-items: center; justify-content: flex-end; gap: var(--space-3);
  padding: var(--space-3) var(--space-4);
  border-top: 1px solid var(--border-color); background: var(--bg-primary); flex-shrink: 0;
}
/* In-body small action only (the artifact row's 复制路径). Footer actions use
   the shared .fbtn pills — see design-guide §按钮. */
.meeting-btn {
  display: inline-flex; align-items: center; gap: var(--space-2);
  height: calc(24 * var(--dp)); padding: 0 var(--space-4);
  border: 1px solid var(--border-color); border-radius: var(--radius-sm);
  background: var(--bg-primary); color: var(--text-primary);
  font-size: var(--font-size-xs); cursor: pointer;
}
.meeting-btn:hover:not(:disabled) { border-color: var(--accent-color); color: var(--accent-color); }
.meeting-btn:disabled { opacity: 0.45; cursor: not-allowed; }
</style>
