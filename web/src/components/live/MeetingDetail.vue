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
    <div v-else-if="!record" class="meeting-body">
      <MeetingEmpty
        :icon="FileQuestion"
        :title="t('meeting.detail.notFoundTitle')"
        :desc="t('meeting.detail.notFoundDesc')"
      />
    </div>
    <template v-else>
      <div class="meeting-meta">
        <span>{{ formatWhen(record.startedAt) }}</span>
        <span v-if="durationLabel">· {{ durationLabel }}</span>
        <span>· {{ record.chatCount > 0 ? t('meeting.detail.chatCount', { n: record.chatCount }) : t('meeting.detail.noChat') }}</span>
      </div>

      <!-- The bar sits ABOVE the tabs so it is visible from every tab: the
           record opens on 资源, and a regeneration started elsewhere must not
           be invisible (or unreachable) just because the reader is not on a
           text tab. -->
      <MeetingArtifactBar
        :kind="barKind"
        :state="barState"
        :generation="record?.generation ?? null"
        :has-audio="Boolean(record?.hasAudio)"
        :show-kind="!activeArtifactKind"
        :blocked="barBlockedReason"
        @regenerate="openRegenerate(barKind)"
        @cancel="cancelRegenerate"
      />

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
          <span v-if="tab.count" class="meeting-tab__count">
            {{ tab.count > 99 ? '99+' : tab.count }}
          </span>
        </button>
      </div>


      <div
        class="meeting-body"
        :data-quote-source="quoteSource"
        :data-quote-language="activeTab === 'chat' ? 'chat' : 'meeting'"
        :data-quote-meeting-token="record?.token || ''"
      >
        <template v-if="activeTab === 'minutes'">
          <!-- The minutes are rendered by the SAME pipeline/interactions as the
               file preview. Do NOT go back to v-html + renderMarkdownHtml:
               that drops detectedPaths, never verifies the path chips, and
               resolves relative links against the project root. -->
          <MeetingMarkdown
            v-if="minutesText"
            :content="minutesText"
            :path="minutesPath"
            @copy="copy"
          />
          <div v-else-if="isArtifactRunning('minutes')" class="meeting-generating">
            <div class="meeting-generating__row"><span class="meeting-spinner" />{{ t('liveShare.notes.generating') }}</div>
          </div>
          <MeetingEmpty
            v-else
            :icon="FileText"
            :title="t('meeting.detail.emptyMinutesTitle')"
            :desc="canRegenerate('minutes') ? t('meeting.detail.emptyMinutesDesc') : t('meeting.detail.artifact.noSource')"
          >
            <template v-if="canRegenerate('minutes')" #action>
              <button
                class="fbtn fbtn-primary"
                type="button"
                data-action="meeting-generate-minutes"
                @click="openRegenerate('minutes')"
              >
                {{ t('meeting.detail.artifact.generate') }}
              </button>
            </template>
          </MeetingEmpty>
        </template>

        <template v-else-if="activeTab === 'transcript'">
          <!-- The same rows the live 转写 tab drew, from the stored artifact:
               the cue's voice span gives the speaker back. Do NOT write the
               timing arrow as its two-dash form in this comment: an HTML
               comment ends at the first close marker, so the rest would leak
               into the pane as visible text. -->
          <TranscriptList
            v-if="transcriptLines.length"
            :blocks="transcriptLines"
            data-role="transcript"
            @shot-open="openShot"
            @shot-delete="removeShot"
          />
          <div v-else-if="isArtifactRunning('transcript')" class="meeting-generating">
            <div class="meeting-generating__row"><span class="meeting-spinner" />{{ t('meeting.detail.artifact.runningAsr') }}</div>
          </div>
          <!-- An empty transcript has three different causes and the old single
               sentence ("听记没有开启") was only right for one of them: it also
               claimed that while the file was still being uploaded right after
               a meeting, and when the file exists but could not be read. -->
          <MeetingEmpty
            v-else-if="transcriptPending"
            :icon="Captions"
            :title="t('meeting.detail.transcriptPendingTitle')"
            :desc="t('meeting.detail.transcriptPendingDesc')"
            data-role="transcript-pending"
          />
          <MeetingEmpty
            v-else-if="transcriptUnreadable"
            :icon="Captions"
            :title="t('meeting.detail.transcriptUnreadableTitle')"
            :desc="t('meeting.detail.transcriptUnreadableDesc')"
            data-role="transcript-unreadable"
          />
          <MeetingEmpty
            v-else
            :icon="Captions"
            :title="t('meeting.detail.emptyTranscriptTitle')"
            :desc="canRegenerate('transcript') ? t('meeting.detail.emptyTranscriptDesc') : t('meeting.detail.artifact.noSource')"
          >
            <template v-if="canRegenerate('transcript')" #action>
              <button
                class="fbtn fbtn-primary"
                type="button"
                data-action="meeting-generate-transcript"
                @click="openRegenerate('transcript')"
              >
                {{ record?.hasAudio ? t('meeting.detail.artifact.reasr') : t('meeting.detail.artifact.generate') }}
              </button>
            </template>
          </MeetingEmpty>
        </template>

        <template v-else-if="activeTab === 'revised'">
          <!-- The same rows the 转写 tab draws, from the proofread file: the
               model only fixes words, so the speakers and the timeline are the
               same conversation. Not Markdown — one line per utterance has no
               paragraph breaks and would render as a single block. -->
          <TranscriptList
            v-if="revisedLines.length"
            :blocks="revisedLines"
            data-role="revised"
          />
          <div v-else-if="isArtifactRunning('revised')" class="meeting-generating">
            <div class="meeting-generating__row"><span class="meeting-spinner" />{{ t('liveShare.notes.proofreading') }}</div>
          </div>
          <!-- Proofreading is allowed to fail without failing the minutes: the
               summary then comes from the raw transcript, and this tab stays
               empty. Say so instead of leaving a blank pane. -->
          <MeetingEmpty
            v-else
            :icon="SpellCheck"
            :title="t('meeting.detail.emptyRevisedTitle')"
            :desc="canRegenerate('revised') ? t('meeting.detail.emptyRevisedDesc') : t('meeting.detail.artifact.noSource')"
            data-role="revised-empty"
          >
            <template v-if="canRegenerate('revised')" #action>
              <button
                class="fbtn fbtn-primary"
                type="button"
                data-action="meeting-generate-revised"
                @click="openRegenerate('revised')"
              >
                {{ t('meeting.detail.artifact.generate') }}
              </button>
            </template>
          </MeetingEmpty>
        </template>

        <template v-else-if="activeTab === 'chat'">
          <MeetingEmpty
            v-if="chat.length === 0"
            :icon="MessagesSquare"
            :title="t('meeting.detail.emptyChatTitle')"
          />
          <!-- The same conversation the live panel showed, drawn by the same
               row component: same bubble, same avatar, same timestamp. -->
          <div v-else class="meeting-chat">
            <ChatMessageRow
              v-for="m in chat"
              :key="m.id"
              :name="m.speaker || t('meeting.detail.anonymous')"
              :at="m.at"
              :text="m.text"
              :self="isMine(m)"
            />
          </div>
        </template>

        <template v-else>
          <MeetingResources
            :groups="resourceGroups"
            :loading="resourcesLoading"
            :error="resourcesError"
            :total="resourceTotal"
            :folder-path="resourceDir"
            :links="shareLinks"
            :share-token="record?.token || ''"
            @reload="reloadResources"
          />
        </template>
      </div>

      <div class="meeting-footer">
        <button class="fbtn fbtn-danger" type="button" data-action="meeting-delete" @click="askDelete">
          <Trash2 :size="14" />{{ t('meeting.detail.delete') }}
        </button>
        <span class="meeting-header__spacer" />
        <button v-if="activeTab !== 'chat'" class="fbtn" type="button" :disabled="!canOpen" @click="openFile">
          <ExternalLink :size="14" />{{ openLabel }}
        </button>
        <button v-if="activeTab !== 'resources'" class="fbtn" type="button" :disabled="!copyText" @click="copy(copyText)">
          <Copy :size="14" />{{ copyLabel }}
        </button>
      </div>
    </template>

    <MeetingRegenerateSheet
      :open="regenOpen"
      :target="regenTarget"
      :plan="regenPlan"
      :loading="regenLoading"
      :error="regenError"
      v-model:include-downstream="regenDownstream"
      @close="regenOpen = false"
      @confirm="confirmRegenerate"
    />
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
import {
  ArrowLeft,
  Captions,
  Copy,
  ExternalLink,
  FileQuestion,
  FileText,
  MessageSquare,
  MessagesSquare,
  SpellCheck,
  Trash2,
} from 'lucide-vue-next'
import LoadingIndicator from '@/components/common/LoadingIndicator.vue'
import ChatMessageRow from './ChatMessageRow.vue'
import TranscriptList from './TranscriptList.vue'
import MeetingMarkdown from './MeetingMarkdown.vue'
import MeetingEmpty from './MeetingEmpty.vue'
import MeetingResources from './MeetingResources.vue'
import MeetingArtifactBar from './MeetingArtifactBar.vue'
import MeetingRegenerateSheet from './MeetingRegenerateSheet.vue'
import { useDialog } from '@/composables/useDialog'
import { useSettingsConfig } from '@/composables/useSettingsConfig'
import { useToast } from '@/composables/useToast'
import { useFilePathAnnotation } from '@/composables/useFilePathAnnotation'
import { onAppResume } from '@/composables/useAppForeground'
import {
  artifactStateOf,
  meetingQuoteLabel,
  useMeetingHistory,
  type MeetingArtifactKind,
  type MeetingChatMessage,
  type MeetingGeneration,
  type MeetingRecord,
  type MeetingShot,
  type RegenPlan,
} from '@/composables/useMeetingHistory'
import { useMeetingResources } from '@/composables/useMeetingResources'
import { buildShareLinks, type ShareLink } from '@/composables/useShareLinks'
import {
  setMeetingView,
  clearMeetingView,
  type MeetingDetailTab,
} from '@/composables/useMeetingViewState'
import { cleanupDragGhost } from '@/utils/attachDrag'
import { formatClockTime } from '@/utils/format'
import { resolveUserNickname } from '@/utils/userNickname'
import { meetingDragPayload, startQuoteDrag } from '@/utils/quoteDrag'
import { parseVttCues, vttToPlainText, type VttCue } from '@/utils/vtt'
import { groupNoteSegments, type LiveNoteSegment, type NoteBlock } from '@/composables/liveNotes'
import { parseRevisedTranscript, revisedToPlainText } from '@/utils/revisedTranscript'
import { normalizeSlashes, toProjectRelative } from '@/utils/path'
import { store } from '@/stores/app'
import { appLog } from '@/utils/appLog'
import '@/assets/modal-footer-btn.css'

const props = defineProps<{ token: string; initialTab?: MeetingDetailTab }>()
const emit = defineEmits<{ back: []; quote: [token: string]; 'tab-change': [tab: MeetingDetailTab] }>()

const { t, locale } = useI18n()
const toast = useToast()
const dialog = useDialog()
const history = useMeetingHistory()
const { serverConfig } = useSettingsConfig()

/**
 * The record has no "self" flag — it stores the speaker NAME the sender used.
 * Matching it against the local nickname is what puts our own lines on the
 * right, the way every other chat does. No nickname configured means we cannot
 * tell, and then nothing is mirrored rather than guessing wrong.
 */
const myName = computed(() => resolveUserNickname(serverConfig.value?.user))

function isMine(m: MeetingChatMessage): boolean {
  return myName.value !== '' && m.speaker === myName.value
}

const { openFilePath } = useFilePathAnnotation()

type Tab = MeetingDetailTab
const activeTab = ref<Tab>(props.initialTab ?? 'resources')
const record = ref<MeetingRecord | null>(null)
const loading = ref(true)
const minutesText = ref('')
const transcriptText = ref('')
/** The proofread transcript (`transcript-revised.md`), Markdown. */
const revisedText = ref('')
/** The stored cues, kept structured so the tab can draw speaker rows. */
const transcriptCues = ref<VttCue[]>([])
const transcriptFile = ref('')
const revisedFile = ref('')
const chat = ref<MeetingChatMessage[]>([])
/** Key frames, already ordered and offset onto the transcript timeline. */
const shots = ref<MeetingShot[]>([])

/** Join inside a meeting folder without assuming a separator style. */
function meetingPath(dir: string, ...parts: string[]): string {
  const sep = dir.includes('\\') ? '\\' : '/'
  return [dir.replace(/[\\/]+$/, ''), ...parts].join(sep)
}

const minutesPath = computed(() => (record.value?.dirPath ? meetingPath(record.value.dirPath, 'minutes.md') : ''))
const transcriptPath = computed(() => (
  record.value?.dirPath
    ? meetingPath(record.value.dirPath, transcriptFile.value || 'transcript.vtt')
    : ''
))
const revisedPath = computed(() => (
  record.value?.dirPath
    ? meetingPath(record.value.dirPath, revisedFile.value || 'transcript-revised.md')
    : ''
))
/** Project-relative folder, used by the resource header and footer action. */
const resourceDir = computed(() => (
  record.value?.dirPath
    ? normalizeSlashes(toProjectRelative(record.value.dirPath, store.state.projectRoot ?? ''))
    : ''
))

const {
  groups: resourceGroups,
  loading: resourcesLoading,
  error: resourcesError,
  total: resourceTotal,
  reload: reloadResources,
} = useMeetingResources({
  record: () => record.value,
  minutesText: () => minutesText.value,
  minutesPath: () => minutesPath.value,
})

/**
 * The meeting link, kept for as long as the record exists.
 *
 * The link used to live only in the running stage's 会议管理 tab, so the moment
 * the host ended the meeting it disappeared from the UI — while the record (and
 * its artifacts) stayed. The resource tab is where a finished meeting's
 * leftovers live, so the link belongs here: it is the one thing a reader wants
 * to send to someone who missed the meeting, and it stays valid until this
 * record is deleted.
 *
 * Rebuilt from the token instead of reusing the stage's `shareLinks`: those are
 * cleared when the share stops, and they were captured for the publisher's own
 * address list at that moment.
 */
const shareLinks = ref<ShareLink[]>([])
let shareLinksGen = 0
watch(
  () => record.value?.token,
  async (token) => {
    const gen = ++shareLinksGen
    if (!token) {
      shareLinks.value = []
      return
    }
    const links = await buildShareLinks(`/share/${encodeURIComponent(token)}`)
    if (gen !== shareLinksGen) return
    shareLinks.value = links
  },
  { immediate: true },
)

const tabs = computed(() => [
  // No count badge on 资源: the tab body's own header already states the
  // total, and a number next to the label just repeats it.
  { key: 'resources' as Tab, label: t('meeting.detail.tabResources'), count: 0 },
  { key: 'minutes' as Tab, label: t('meeting.detail.tabMinutes'), count: 0 },
  { key: 'transcript' as Tab, label: t('meeting.detail.tabTranscript'), count: 0 },
  { key: 'revised' as Tab, label: t('meeting.detail.tabRevised'), count: 0 },
  { key: 'chat' as Tab, label: t('meeting.detail.tabChat'), count: chat.value.length },
])

const regenOpen = ref(false)
const regenTarget = ref<MeetingArtifactKind>('minutes')
const regenPlan = ref<RegenPlan | null>(null)
const regenLoading = ref(false)
const regenError = ref('')
const regenDownstream = ref(true)
let lastRegenTarget: MeetingArtifactKind | null = null
let lastRegenContentAt = 0
let prevGeneration: MeetingGeneration | null = null

/** The artifact the active text tab owns; null on chat/resources. */
const activeArtifactKind = computed<MeetingArtifactKind | null>(() => {
  if (activeTab.value === 'minutes' || activeTab.value === 'transcript' || activeTab.value === 'revised') {
    return activeTab.value
  }
  return null
})
/**
 * The artifact the bar speaks for.
 *
 * On a text tab that is the tab's own artifact. On 资源/聊天 the bar still has
 * to mean something: it follows the running step, so a chain started from
 * another tab stays visible, and otherwise falls back to the minutes — the
 * record's primary output. `showKind` marks that fallback so "还没有生成" never
 * reads as if it were about the tab on screen.
 */
const barKind = computed<MeetingArtifactKind>(() =>
  activeArtifactKind.value ?? record.value?.generation?.step ?? 'minutes')
const barState = computed(() => artifactStateOf(record.value, barKind.value))
const barBlockedReason = computed(() => {
  if (!record.value) return ''
  return canRegenerate(barKind.value) ? '' : t('meeting.detail.artifact.noSource')
})

/** The step is the current one, or the only job left after a refresh. */
function isArtifactRunning(kind: MeetingArtifactKind): boolean {
  const generation = record.value?.generation
  if (generation?.step === kind) return true
  return !generation && artifactStateOf(record.value, kind).jobStatus === 'running'
}

/**
 * Whether a source exists. This is only used for the empty-state CTA and the
 * disabled reason; the server's plan is the authority on what will actually run.
 */
function canRegenerate(kind: MeetingArtifactKind): boolean {
  const current = record.value
  if (!current) return false
  if (kind === 'minutes') {
    return current.hasAudio || current.hasSegments || artifactStateOf(current, 'revised').hasContent
  }
  return current.hasAudio || current.hasSegments
}

function kindLabel(kind: MeetingArtifactKind): string {
  if (kind === 'minutes') return t('meeting.detail.tabMinutes')
  if (kind === 'revised') return t('meeting.detail.tabRevised')
  return t('meeting.detail.tabTranscript')
}

/**
 * The stored cues as speaker turns.
 *
 * The cue's voice span is the speaker, so the record shows the same rows the
 * live panel showed. Consecutive lines from one speaker are merged by the same
 * rule the live transcript uses (`groupNoteSegments`) — not by a second one
 * that would split the conversation differently on the two surfaces.
 */
const REASON_KEYS: Record<string, string> = {
  manual: 'meeting.shots.reasonManual',
  file: 'meeting.shots.reasonFile',
  sharer: 'meeting.shots.reasonSharer',
  switch: 'meeting.shots.reasonSwitch',
  speech: 'meeting.shots.reasonSpeech',
  change: 'meeting.shots.reasonChange',
}

function shotReasonLabel(reason: string): string {
  return t(REASON_KEYS[reason] ?? 'meeting.shots.reasonChange')
}

/** Absolute path of a stored frame (the image viewer takes one). */
function shotFullPath(shot: MeetingShot): string {
  const dir = record.value?.dirPath
  if (!dir) return ''
  return meetingPath(dir, ...shot.fileName.split('/'))
}

/**
 * Thumbnail URL.
 *
 * The frame lives inside the meeting folder, which is inside the project, so
 * the existing thumbnail endpoint serves it with the project cookie — no new
 * binary endpoint, and the file-manager cache/revalidation comes for free.
 */
function shotThumbUrl(shot: MeetingShot): string {
  const full = shotFullPath(shot)
  if (!full) return ''
  const rel = normalizeSlashes(toProjectRelative(full, store.state.projectRoot ?? ''))
  return `/api/fs/thumb?target=${encodeURIComponent(rel)}&w=480`
}

/**
 * The stored cues as speaker turns, with the key frames merged in.
 *
 * Frames and utterances share one timeline, so they are put in ONE array and
 * sorted before grouping: `groupNoteSegments` walks its input in order, and
 * appending the frames would put every screenshot at the end of the meeting.
 * The row also carries text — an empty one is dropped by the grouper — which is
 * why the reason label is the row's text and the picture is an extra field.
 */
const transcriptLines = computed<NoteBlock[]>(() => {
  const rows: Array<
    LiveNoteSegment & { kind?: 'file' | 'screen' | 'shot'; shot?: NoteBlock['shot'] }
  > = transcriptCues.value.map((cue, i) => ({
    id: i + 1,
    channel: cue.speaker || 'unknown',
    speaker: cue.speaker,
    startedAt: cue.startMs,
    durationMs: Math.max(0, cue.endMs - cue.startMs),
    text: cue.text,
    ...(cue.kind ? { kind: cue.kind } : {}),
  }))
  for (const shot of shots.value) {
    rows.push({
      // Negative ids keep frames out of the way of cue ids, which are 1-based.
      id: -shot.id,
      channel: 'shot',
      speaker: '',
      startedAt: shot.offsetMs,
      durationMs: 0,
      text: shotReasonLabel(shot.reason),
      kind: 'shot',
      shot: {
        id: shot.id,
        thumbUrl: shotThumbUrl(shot),
        fullPath: shotFullPath(shot),
        reason: shot.reason,
        ref: shot.ref,
      },
    })
  }
  rows.sort((a, b) => a.startedAt - b.startedAt || a.id - b.id)
  return groupNoteSegments(rows)
})

/**
 * The proofread transcript as the same rows.
 *
 * The file carries start times but no durations, so the 4s merge window is
 * measured from the previous line's start rather than from its end: the list
 * stays closer to one row per line, and never merges two distant utterances.
 */
const revisedLines = computed<NoteBlock[]>(() =>
  groupNoteSegments(
    parseRevisedTranscript(revisedText.value).map((line, i) => ({
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
 * How long after the end of a meeting an absent transcript is still "on its
 * way" rather than "never existed".
 *
 * The publisher writes `transcript.vtt` only after it has drained its upload
 * queue, and that drain may take up to 3 minutes (`FINISH_UPLOAD_TIMEOUT_MS`);
 * the merge and the write follow. Without this grace the tab told the reader
 * "听记没有开启" for the first minutes after every meeting — the one moment they
 * are most likely to open it.
 *
 * `Date.now()` is not reactive on its own, but the record is re-fetched every
 * 2.5s while the meeting is still settling (`shouldPoll`), so the sentence
 * flips on the next tick after the window closes.
 */
const TRANSCRIPT_GRACE_MS = 4 * 60 * 1000

const transcriptPending = computed(() => {
  const r = record.value
  if (!r || r.hasTranscript || !r.endedAt) return false
  return Date.now() - r.endedAt < TRANSCRIPT_GRACE_MS
})

/**
 * The transcript FILE exists but this list could not draw anything from it —
 * the artifact request failed, or the file carries no cue we can show. Saying
 * "听记没有开启" here sends the reader looking for a setting that was on.
 */
const transcriptUnreadable = computed(
  () => Boolean(record.value?.hasTranscript) && transcriptLines.value.length === 0,
)

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

const currentText = computed(() => {
  if (activeTab.value === 'transcript') return transcriptText.value
  if (activeTab.value === 'revised') return revisedToPlainText(revisedText.value)
  return minutesText.value
})
const copyText = computed(() => {
  // The resource tab has no single text body; its copy affordances live on
  // each row (path) and in the header (folder path).
  if (activeTab.value === 'resources') return ''
  if (activeTab.value === 'chat') {
    return chat.value
      .map((m) =>
        t('meeting.detail.chatLine', {
          time: formatClockTime(new Date(m.at)),
          speaker: m.speaker || t('meeting.detail.anonymous'),
          text: m.text,
        }),
      )
      .join('\n')
  }
  return currentText.value
})

const canOpen = computed(() => {
  if (activeTab.value === 'minutes') return Boolean(record.value?.hasMinutes && minutesPath.value)
  if (activeTab.value === 'transcript') return Boolean(record.value?.hasTranscript && transcriptPath.value)
  if (activeTab.value === 'revised') return Boolean(record.value?.hasRevised && revisedPath.value)
  if (activeTab.value === 'resources') return Boolean(resourceDir.value)
  return false
})

/** The footer's copy action names the text it copies, not just "copy". */
const copyLabel = computed(() => {
  if (activeTab.value === 'chat') return t('meeting.detail.copyAll')
  if (activeTab.value === 'minutes') return t('liveShare.notes.copy')
  return t('common.copy')
})

const openLabel = computed(() => (
  activeTab.value === 'resources'
    ? t('meeting.detail.openResources')
    : t('liveShare.history.open')
))

function formatWhen(ms: number): string {
  if (!ms) return ''
  return new Date(ms).toLocaleString(locale.value, {
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
  })
}

const loadedContentAt: Record<MeetingArtifactKind, number> = {
  minutes: -1,
  transcript: -1,
  revised: -1,
}

/**
 * Fetch the record and its artifacts.
 *
 * `silent` is for the poll. It must not toggle `loading` (that swaps the whole
 * panel below the header for a spinner) and it only re-reads an artifact whose
 * `contentAt` moved: a long regeneration would otherwise re-download three
 * unchanged files every 2.5 seconds.
 */
async function load(silent = false): Promise<void> {
  if (!props.token) return
  if (!silent) loading.value = true
  try {
    const rec = await history.get(props.token)
    record.value = rec
    if (!rec) return
    await loadArtifacts(rec, silent)
    if (!silent) {
      const messages = await history.chat(props.token)
      chat.value = messages.messages
      // Frames are final by the time the record is opened; the poll below only
      // waits for the text artifacts, so there is no reason to re-read them
      // every 2.5s.
      shots.value = await history.shots(props.token)
    }
    // The initial job starts AFTER stop() resolves, i.e. after this view was
    // opened. Without polling, a meeting that just ended would say "no minutes"
    // until the user navigated away and back.
    if (shouldPoll(rec)) pollRecord()
  } finally {
    if (!silent) loading.value = false
  }
}

async function loadArtifacts(rec: MeetingRecord, silent: boolean): Promise<void> {
  for (const kind of ['minutes', 'transcript', 'revised'] as const) {
    const contentAt = artifactStateOf(rec, kind).contentAt
    if (silent && loadedContentAt[kind] === contentAt) continue
    const art = await history.artifact(props.token, kind)
    if (kind === 'minutes') {
      minutesText.value = art?.content ?? ''
    } else if (kind === 'transcript') {
      // The artifact is WebVTT; the tab is a reading surface, so the markup is
      // stripped here rather than in the file.
      transcriptText.value = vttToPlainText(art?.content ?? '')
      transcriptCues.value = parseVttCues(art?.content ?? '')
      transcriptFile.value = art?.file ?? ''
    } else {
      revisedText.value = art?.content ?? ''
      revisedFile.value = art?.file ?? ''
    }
    loadedContentAt[kind] = contentAt
  }
}

/**
 * Whether a job may still change the record.
 *
 * `generation` is obvious. The second case is the just-ended meeting: the
 * publisher drains its upload queue and only then asks for the minutes, so for
 * a while the record has no job although one is about to start. The window is
 * bounded so an old meeting that never got minutes does not poll forever.
 */
function shouldPoll(r: MeetingRecord | null): boolean {
  if (!r) return false
  if (r.generation) return true
  return r.endedAt > 0 && !artifactStateOf(r, 'minutes').hasContent
    && Date.now() - r.endedAt < 10 * 60 * 1000
}

function selectTab(tab: Tab): void {
  activeTab.value = tab
  emit('tab-change', tab)
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

async function copy(text: string): Promise<void> {
  if (!text) return
  try {
    await navigator.clipboard.writeText(text)
    toast.show(t('common.copied'), { type: 'success', icon: 'copy' })
  } catch (e) {
    appLog.w('MeetingDetail', 'copy failed', e)
  }
}

/**
 * Open the current tab's file through the shared pipeline.
 *
 * `'live'` is the navigation source: it records the meeting + tab as the
 * return target, so Back from the viewer lands back here instead of the
 * meeting list (or, before this change, the task tab).
 */
function openFile(): void {
  const target = activeTab.value === 'resources'
    ? resourceDir.value
    : activeTab.value === 'transcript'
      ? transcriptPath.value
      : activeTab.value === 'revised'
        ? revisedPath.value
        : minutesPath.value
  if (target) void openFilePath(target, undefined, undefined, 'live')
}

async function openRegenerate(kind: MeetingArtifactKind): Promise<void> {
  if (!record.value) return
  regenTarget.value = kind
  regenDownstream.value = true
  regenOpen.value = true
  regenPlan.value = null
  await fetchRegenPlan()
}

/** Open one frame in the image viewer (the same one the file manager uses). */
function openShot(block: NoteBlock): void {
  const target = block.shot?.fullPath
  if (target) void openFilePath(target, undefined, undefined, 'live')
}

/**
 * Delete one frame. Confirmed, because it removes a file from the meeting
 * folder and cannot be undone; the row disappears only after the server says
 * it is gone.
 */
async function removeShot(block: NoteBlock): Promise<void> {
  const shot = block.shot
  if (!shot) return
  const ok = await dialog.confirm(t('meeting.shots.deleteConfirm'), { dangerous: true })
  if (!ok) return
  if (!(await history.deleteShot(props.token, shot.id))) {
    toast.show(t('meeting.shots.deleteFailed'), { type: 'error', icon: 'warning' })
    return
  }
  shots.value = shots.value.filter((s) => s.id !== shot.id)
  if (record.value && typeof record.value.shotCount === 'number') {
    record.value.shotCount = Math.max(0, record.value.shotCount - 1)
  }
}

async function fetchRegenPlan(): Promise<void> {
  if (!record.value) return
  regenLoading.value = true
  regenError.value = ''
  try {
    const plan = await history.regenPlan(record.value.token, regenTarget.value, regenDownstream.value)
    if (!plan) {
      regenError.value = t('meeting.detail.regen.planFailed')
      return
    }
    regenPlan.value = plan
  } finally {
    regenLoading.value = false
  }
}

async function confirmRegenerate(): Promise<void> {
  const current = record.value
  if (!current || !regenPlan.value || regenPlan.value.blocked) return
  const kind = regenTarget.value
  lastRegenTarget = kind
  lastRegenContentAt = artifactStateOf(current, kind).contentAt
  const result = await history.regenerate(current.token, kind, regenDownstream.value)
  if (!result.ok) {
    if (result.plan) {
      regenPlan.value = result.plan
      return
    }
    toast.show(t('meeting.detail.regen.startFailed'), { type: 'error', icon: 'warning' })
    return
  }
  regenOpen.value = false
  if (result.session) {
    record.value = result.session
    prevGeneration = result.session.generation
  }
  pollRecord()
}

async function cancelRegenerate(): Promise<void> {
  if (!record.value) return
  const ok = await history.cancelRegeneration(record.value.token)
  if (!ok) {
    toast.show(t('meeting.detail.regen.cancelFailed'), { type: 'error', icon: 'warning' })
    return
  }
  toast.show(t('meeting.detail.artifact.cancelled'), { type: 'info', icon: 'info' })
  pollRecord()
}

watch(regenDownstream, () => {
  if (regenOpen.value) void fetchRegenPlan()
})

// Completion toast: only for a job this view started. A job started on another
// device already arrives as a live_event toast/notification.
watch(
  () => record.value?.generation,
  (generation) => {
    const previous = prevGeneration
    prevGeneration = generation ?? null
    if (!previous || generation) return
    const target = lastRegenTarget
    if (!target) return
    const state = artifactStateOf(record.value, target)
    if (state.jobStatus !== 'failed') {
      if (state.contentAt !== lastRegenContentAt) {
        toast.show(t('meeting.detail.artifact.updated', { kind: kindLabel(target) }), { type: 'success', icon: 'success' })
      } else {
        toast.show(t('meeting.detail.artifact.unchanged'), { type: 'info', icon: 'info' })
      }
    }
    lastRegenTarget = null
  },
)

let pollTimer: ReturnType<typeof setTimeout> | null = null
/**
 * Set once the component is gone.
 *
 * `onBeforeUnmount` only cancels the timer that is already scheduled; a `load()`
 * that is in flight at that moment still finishes and re-arms the poll, so a
 * closed view kept hitting four endpoints every 2.5 s until the process ended.
 */
let disposed = false
function pollRecord(): void {
  if (disposed) return
  if (pollTimer) clearTimeout(pollTimer)
  pollTimer = setTimeout(() => {
    void (async () => {
      await load(true)
      if (disposed) return
      if (!shouldPoll(record.value)) {
        if (pollTimer) clearTimeout(pollTimer)
        pollTimer = null
      }
    })()
  }, 2500)
}

const removeResumeListener = onAppResume(() => {
  // A backgrounded WebView throttles timers; on return the record may be many
  // seconds stale even though the job finished. Refresh once immediately.
  void load(true)
})

onBeforeUnmount(() => {
  disposed = true
  removeResumeListener()
  if (pollTimer) clearTimeout(pollTimer)
  pollTimer = null
  // The panel is unmounted on project switch / teardown; a stale view would
  // otherwise be used as a return target by a later file jump.
  clearMeetingView()
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
    activeTab.value = props.initialTab ?? 'resources'
    loadedContentAt.minutes = -1
    loadedContentAt.transcript = -1
    loadedContentAt.revised = -1
    prevGeneration = null
    lastRegenTarget = null
    void load()
  },
)

// Returning from a file restores the tab the user was on; the panel is kept
// alive, so the prop (not a remount) is what changes.
watch(
  () => props.initialTab,
  (tab) => {
    if (tab) activeTab.value = tab
  },
)

// Hand the current view to the navigation coordinator. It builds the return
// target when a file/resource is opened from this panel.
watch(
  [() => record.value?.token, activeTab],
  () => {
    const token = record.value?.token
    if (token) setMeetingView(token, activeTab.value)
  },
  { immediate: true },
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

/* The archived chat is the SAME conversation the live panel showed, so the row
   itself is the same component (`ChatMessageRow.vue`) — bubble, avatar and
   timestamp have exactly one implementation. This container only owns the
   scroll column and the gap between messages. */
.meeting-chat { display: flex; flex-direction: column; gap: var(--space-3); }

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
  display: flex; flex-wrap: nowrap; align-items: center; justify-content: flex-end;
  justify-content: safe flex-end; gap: var(--space-3);
  padding: var(--space-3) var(--space-4);
  border-top: 1px solid var(--border-color); background: var(--bg-primary); flex-shrink: 0;
  overflow-x: auto; overflow-y: hidden;
}
</style>
