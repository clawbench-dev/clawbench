<template>
  <div class="share-meeting">
    <!-- The same topbar chrome every public share page uses: brand mark, the
         name of what is open, and the theme picker at the far edge. A member
         who followed a link lands here instead of the app, so the page has to
         say whose page it is — the record body alone reads like a stray
         document. -->
    <div class="share-topbar">
      <img class="share-logo" src="/logo-64.png" alt="ClawBench" />
      <h1 class="share-file-name share-meeting__title" data-role="record-title">
        {{ record?.title || title || t('meeting.list.untitled') }}
      </h1>
      <span class="share-spacer" />
      <ShareFitButton />
      <ShareThemeButton />
    </div>

    <div class="share-meeting__meta">
      <span class="share-meeting__badge" data-role="meeting-ended">
        {{ t('meeting.share.ended') }}
      </span>
      <span v-if="metaLabel" class="share-meeting__when">{{ metaLabel }}</span>
    </div>

    <div v-if="loading" class="share-meeting__center">
      <LoadingIndicator size="sm" inline />
      {{ t('common.loading') }}
    </div>

    <EmptyState
      v-else-if="!record"
      :icon="FileQuestion"
      :title="t('meeting.detail.notFoundTitle')"
      :desc="t('meeting.detail.notFoundDesc')"
      data-role="meeting-missing"
    />

    <template v-else>
      <div class="share-meeting__tabs" role="tablist">
        <button
          v-for="tab in tabs"
          :key="tab.key"
          class="share-meeting__tab"
          :class="{ 'is-on': activeTab === tab.key }"
          type="button"
          role="tab"
          :aria-selected="activeTab === tab.key"
          :data-tab="tab.key"
          @click="activeTab = tab.key"
        >
          {{ tab.label }}
          <span v-if="tab.count" class="share-meeting__count">
            {{ tab.count > 99 ? '99+' : tab.count }}
          </span>
        </button>
      </div>

      <div class="share-meeting__body">
        <template v-if="activeTab === 'minutes'">
          <div v-if="generating" class="share-meeting__hint-row">
            <span class="share-meeting__spinner" />{{ t('liveShare.notes.generating') }}
          </div>
          <div v-else-if="staleGenerating" class="share-meeting__warn">
            {{ t('meeting.detail.generatingStale') }}
          </div>
          <div v-else-if="minutesFailed" class="share-meeting__error">
            {{ t('liveShare.notes.generateFailed') }}
          </div>
          <MeetingMarkdown
            v-else-if="minutesText"
            :content="minutesText"
            path=""
            @copy="copy"
          />
          <EmptyState
            v-else
            :icon="FileText"
            :title="t('meeting.detail.emptyMinutesTitle')"
            :desc="t('meeting.detail.emptyMinutesDesc')"
            data-role="minutes-empty"
          />
        </template>

        <template v-else-if="activeTab === 'transcript'">
          <TranscriptList
            v-if="transcriptLines.length"
            :blocks="transcriptLines"
            readonly
            data-role="transcript"
          />
          <EmptyState
            v-else
            :icon="Captions"
            :title="t('meeting.detail.emptyTranscriptTitle')"
            :desc="t('meeting.detail.emptyTranscriptDesc')"
            data-role="transcript-empty"
          />
        </template>

        <template v-else-if="activeTab === 'revised'">
          <TranscriptList
            v-if="revisedLines.length"
            :blocks="revisedLines"
            readonly
            data-role="revised"
          />
          <div v-else-if="generating" class="share-meeting__hint-row">
            <span class="share-meeting__spinner" />{{ t('liveShare.notes.proofreading') }}
          </div>
          <EmptyState
            v-else
            :icon="SpellCheck"
            :title="t('meeting.detail.emptyRevisedTitle')"
            :desc="t('meeting.detail.emptyRevisedDesc')"
            data-role="revised-empty"
          />
        </template>

        <template v-else>
          <EmptyState
            v-if="chat.length === 0"
            :icon="MessagesSquare"
            :title="t('meeting.detail.emptyChatTitle')"
            :desc="t('meeting.detail.emptyChatDesc')"
            data-role="chat-empty"
          />
          <!-- The same row component the live chat and the host's record use:
               the archived conversation must not be a second drawing of it. -->
          <div v-else class="share-meeting__chat">
            <ChatMessageRow
              v-for="(m, i) in chat"
              :key="`${m.at}-${i}`"
              :name="m.speaker || t('meeting.detail.anonymous')"
              :at="m.at"
              :text="m.text"
              :self="isMine(m)"
            />
          </div>
        </template>
      </div>

      <div v-if="copyText" class="share-meeting__footer">
        <button class="share-meeting__copy" type="button" data-action="meeting-copy" @click="copy(copyText)">
          <Copy :size="14" />{{ copyLabel }}
        </button>
      </div>
    </template>
  </div>
</template>

<script setup lang="ts">
/**
 * The meeting record, as a VIEWER reads it after the meeting ended.
 *
 * A share link used to die with the room: the moment the host pressed 结束会议
 * the token stopped resolving, so a member who came back to the link saw
 * "会议已结束" and nothing else. The record is permanent, so the same link now
 * opens this view — the same four surfaces the host sees (minutes, transcript,
 * proofread, chat), minus everything that only makes sense on the publisher's
 * disk (file paths, delete, quote into a chat that lives in the app).
 *
 * Data comes from the public `/notes?full=1` endpoint, which the live transcript
 * poll already uses: one token-scoped read, no new credential.
 */
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import {
  Captions,
  Copy,
  FileQuestion,
  FileText,
  MessagesSquare,
  SpellCheck,
} from 'lucide-vue-next'
import LoadingIndicator from '@/components/common/LoadingIndicator.vue'
import ChatMessageRow from '@/components/live/ChatMessageRow.vue'
import TranscriptList from '@/components/live/TranscriptList.vue'
import MeetingMarkdown from '@/components/live/MeetingMarkdown.vue'
import EmptyState from '@/components/common/EmptyState.vue'
import { useToast } from '@/composables/useToast'
import {
  groupNoteSegments,
  withMeetingFileEvents,
  withMeetingShots,
  type NoteBlock,
  type NoteTimelineRow,
} from '@/composables/liveNotes'
import { shotFullUrl, shotThumbUrl } from '@/utils/meetingShots'
import { parseRevisedTranscript, revisedToPlainText } from '@/utils/revisedTranscript'
import { appLog } from '@/utils/appLog'
import { readMeetingNick } from './meetingNickname'
import { getShareToken } from './shareMode'
import ShareThemeButton from './ShareThemeButton.vue'
import ShareFitButton from './ShareFitButton.vue'
import {
  fetchMeetingRecord,
  type ShareChatMessage,
  type ShareMeetingRecord,
} from './liveNotesClient'

withDefaults(defineProps<{ title?: string }>(), { title: '' })

const { t, locale } = useI18n()
const toast = useToast()

type Tab = 'minutes' | 'transcript' | 'revised' | 'chat'
const activeTab = ref<Tab>('minutes')

const record = ref<ShareMeetingRecord | null>(null)
const loading = ref(true)

const tabs = computed(() => [
  { key: 'minutes' as Tab, label: t('meeting.detail.tabMinutes'), count: 0 },
  { key: 'transcript' as Tab, label: t('meeting.detail.tabTranscript'), count: 0 },
  { key: 'revised' as Tab, label: t('meeting.detail.tabRevised'), count: 0 },
  { key: 'chat' as Tab, label: t('meeting.detail.tabChat'), count: record.value?.chat.length ?? 0 },
])

const minutesText = computed(() => record.value?.minutes.markdown ?? '')
const chat = computed<ShareChatMessage[]>(() => record.value?.chat ?? [])

/**
 * The transcript rows: the same merge the live panel does, so a file open or a
 * screen share appears as the row it appeared as during the meeting.
 */
const transcriptLines = computed<NoteBlock[]>(() => {
  const r = record.value
  if (!r) return []
  // The public payload carries no paths (the server blanks them), so the file
  // rows are identified by name — exactly what the live panel shows a viewer.
  const rows: NoteTimelineRow[] = withMeetingFileEvents(
    r.segments,
    r.files.map((f) => ({ path: '', name: f.name, openedAt: f.openedAt, closedAt: f.closedAt })),
  )
  // Frames are part of the record, not a publisher-only extra: a member reads
  // the same timeline the host does, pictures in the same place.
  const token = getShareToken() ?? ''
  return groupNoteSegments(
    withMeetingShots(
      rows,
      r.shots.map((s) => ({
        id: s.id,
        at: s.at,
        reason: s.reason,
        ref: s.ref,
        thumbUrl: shotThumbUrl(token, s.id),
        fullUrl: shotFullUrl(token, s.id),
      })),
    ),
  )
})

const revisedLines = computed<NoteBlock[]>(() =>
  groupNoteSegments(
    parseRevisedTranscript(record.value?.minutes.revise?.text ?? '').map((line, i) => ({
      id: i + 1,
      channel: line.kind || line.speaker || 'unknown',
      speaker: line.speaker,
      startedAt: line.startMs,
      durationMs: 0,
      text: line.text,
      ...(line.kind ? { kind: line.kind } : {}),
    })),
  ),
)

/**
 * A job that died with the process.
 *
 * The public payload carries no `updated_at`, so the age is measured from the
 * meeting's end. The minutes job is bounded at ten minutes, and it cannot start
 * before the meeting ends — anything still `generating` past that window is a
 * job nobody is running.
 */
const staleGenerating = computed(() => {
  const r = record.value
  if (!r || r.minutes.status !== 'generating') return false
  return r.endedAt > 0 && Date.now() - r.endedAt > 10 * 60 * 1000
})
const generating = computed(() => {
  const r = record.value
  if (!r || r.minutes.status !== 'generating') return false
  return !staleGenerating.value
})
const minutesFailed = computed(() => record.value?.minutes.status === 'failed')

/** The viewer's own lines: same nickname they joined with (remembered locally). */
const myName = readMeetingNick()
function isMine(m: ShareChatMessage): boolean {
  return myName !== '' && m.speaker === myName
}

const metaLabel = computed(() => {
  const r = record.value
  if (!r?.startedAt) return ''
  const when = new Date(r.startedAt).toLocaleString(locale.value, {
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
  })
  if (!r.endedAt || r.endedAt <= r.startedAt) return when
  const minutes = Math.max(1, Math.round((r.endedAt - r.startedAt) / 60000))
  return `${when} · ${t('meeting.list.duration', { n: minutes })}`
})

const copyText = computed(() => {
  if (activeTab.value === 'chat') {
    return chat.value
      .map((m) =>
        t('meeting.detail.chatLine', {
          time: new Date(m.at).toLocaleTimeString(locale.value, { hour: '2-digit', minute: '2-digit' }),
          speaker: m.speaker || t('meeting.detail.anonymous'),
          text: m.text,
        }),
      )
      .join('\n')
  }
  if (activeTab.value === 'revised') return revisedToPlainText(record.value?.minutes.revise?.text ?? '')
  if (activeTab.value === 'transcript') return transcriptLines.value.map((b) => b.text).join('\n')
  return minutesText.value
})
const copyLabel = computed(() =>
  activeTab.value === 'chat' ? t('meeting.detail.copyAll') : t('common.copy'),
)

async function copy(text: string): Promise<void> {
  if (!text) return
  try {
    await navigator.clipboard.writeText(text)
    toast.show(t('common.copied'), { type: 'success', icon: 'copy' })
  } catch (e) {
    appLog.w('MeetingShareDetail', 'copy failed', e)
  }
}

/**
 * The minutes job starts only after the publisher has drained its upload queue,
 * so right after a meeting the record legitimately has no minutes yet. Keep
 * polling for that window and while a job is running; stop once the record has
 * settled, so an old record costs nothing.
 */
function shouldPoll(r: ShareMeetingRecord | null): boolean {
  if (!r) return false
  if (r.minutes.status === 'generating') return true
  return r.minutes.status === 'idle' && r.endedAt > 0 && Date.now() - r.endedAt < 10 * 60 * 1000
}

let timer: ReturnType<typeof setTimeout> | null = null
let disposed = false

async function load(silent = false): Promise<void> {
  if (!silent) loading.value = true
  try {
    const next = await fetchMeetingRecord()
    if (disposed) return
    record.value = next
    if (shouldPoll(next)) schedulePoll()
  } finally {
    if (!silent) loading.value = false
  }
}

function schedulePoll(): void {
  if (disposed) return
  if (timer) clearTimeout(timer)
  timer = setTimeout(() => {
    void (async () => {
      await load(true)
      if (disposed) return
      if (!shouldPoll(record.value) && timer) {
        clearTimeout(timer)
        timer = null
      }
    })()
  }, 2500)
}

onMounted(() => {
  void load()
})

onBeforeUnmount(() => {
  disposed = true
  if (timer) clearTimeout(timer)
  timer = null
})
</script>

<style scoped>
.share-meeting {
  display: flex;
  flex-direction: column;
  flex: 1;
  min-height: 0;
  overflow: hidden;
  background: var(--bg-primary);
}

.share-meeting__title {
  margin: 0;
  min-width: 0;
}
/* Meta line under the topbar: the record's own facts, not chrome. */
.share-meeting__meta {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: var(--space-3);
  flex-shrink: 0;
  padding: var(--space-3) var(--space-7);
  border-bottom: 1px solid var(--border-color);
  font-size: var(--font-size-xs);
  color: var(--text-muted);
}
.share-meeting__badge {
  height: calc(18 * var(--dp));
  padding: 0 var(--space-3);
  border-radius: var(--radius-full);
  background: var(--bg-tertiary);
  color: var(--text-muted);
  font-size: var(--font-size-2xs);
  line-height: calc(18 * var(--dp));
}

.share-meeting__center {
  display: flex;
  flex: 1;
  align-items: center;
  justify-content: center;
  gap: var(--space-2);
  color: var(--text-muted);
  font-size: var(--font-size-sm);
}

.share-meeting__tabs {
  display: flex;
  flex-shrink: 0;
  border-bottom: 1px solid var(--border-color);
  background: var(--bg-secondary);
}
.share-meeting__tab {
  display: inline-flex;
  flex: 1 1 0;
  align-items: center;
  justify-content: center;
  gap: var(--space-2);
  height: calc(34 * var(--dp));
  padding: 0 var(--space-5);
  border: none;
  border-bottom: 2px solid transparent;
  background: transparent;
  color: var(--text-secondary);
  font-size: var(--font-size-sm);
  cursor: pointer;
}
.share-meeting__tab:hover { color: var(--text-primary); }
.share-meeting__tab.is-on {
  color: var(--accent-color);
  border-bottom-color: var(--accent-color);
  font-weight: 600;
}
.share-meeting__count {
  min-width: calc(14 * var(--dp));
  height: calc(14 * var(--dp));
  padding: 0 var(--space-1);
  border-radius: var(--radius-full);
  background: var(--accent-color);
  color: #fff;
  font-size: var(--font-size-2xs);
  line-height: calc(14 * var(--dp));
  text-align: center;
}

.share-meeting__body {
  flex: 1;
  min-height: 0;
  overflow-y: auto;
  display: flex;
  flex-direction: column;
  gap: var(--space-5);
  padding: var(--space-6) var(--space-7);
}

.share-meeting__chat { display: flex; flex-direction: column; gap: var(--space-3); }

.share-meeting__hint-row {
  display: flex;
  align-items: center;
  gap: var(--space-3);
  padding: var(--space-5);
  border-radius: var(--radius-md);
  background: var(--bg-secondary);
  color: var(--text-secondary);
  font-size: var(--font-size-sm);
}
.share-meeting__spinner {
  width: calc(13 * var(--dp));
  height: calc(13 * var(--dp));
  border: 2px solid color-mix(in srgb, var(--accent-color) 25%, transparent);
  border-top-color: var(--accent-color);
  border-radius: 50%;
  animation: share-meeting-spin 0.8s linear infinite;
  flex-shrink: 0;
}
@keyframes share-meeting-spin { to { transform: rotate(360deg); } }

.share-meeting__warn {
  padding: var(--space-4) var(--space-5);
  border-radius: var(--radius-md);
  background: color-mix(in srgb, var(--color-orange) 10%, transparent);
  color: var(--color-orange);
  font-size: var(--font-size-xs);
  line-height: 1.6;
}
.share-meeting__error {
  padding: var(--space-4) var(--space-5);
  border-radius: var(--radius-md);
  background: color-mix(in srgb, var(--color-red) 10%, transparent);
  color: var(--color-red);
  font-size: var(--font-size-sm);
}

.share-meeting__footer {
  flex-shrink: 0;
  display: flex;
  justify-content: flex-end;
  padding: var(--space-3) var(--space-4);
  border-top: 1px solid var(--border-color);
}
.share-meeting__copy {
  display: inline-flex;
  align-items: center;
  gap: var(--space-2);
  height: calc(30 * var(--dp));
  padding: 0 var(--space-5);
  border: 1px solid var(--border-color);
  border-radius: var(--radius-sm);
  background: var(--bg-secondary);
  color: var(--text-primary);
  font-size: var(--font-size-sm);
  cursor: pointer;
}
.share-meeting__copy:hover { background: var(--bg-hover); }
</style>
