<template>
  <div
    class="meeting-panel"
    :class="{ 'is-drop-target': dropActive }"
    @dragenter="onDragEnter"
    @dragover="onDragOver"
    @dragleave="onDragLeave"
    @drop="onDrop"
  >
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

    <HttpsNotice v-if="showHttpsNotice" variant="banner" />

    <div class="meeting-body">
      <!-- First load: the record rows are cards, so the placeholder draws the
           same row shape instead of a lone ring in an empty pane. -->
      <PanelSkeleton
        v-if="loading && sessions.length === 0"
        class="meeting-list-skeleton"
        variant="list"
        :rows="6"
        :leading="false"
        :meta="false"
        :label="t('common.loading')"
      />
      <div v-else-if="error" class="meeting-error">{{ t('meeting.list.loadFailed') }}</div>
      <!-- The empty state speaks the card language the other dock tabs use:
           a muted glyph, a title, and one line about what lands here. The
           footer's two buttons are the action, so the card stays text-only. -->
      <MeetingEmpty
        v-else-if="sessions.length === 0"
        :icon="MEETING_ICON"
        :title="t('meeting.list.empty')"
        :desc="t('meeting.list.emptyDesc')"
      />
      <div v-else class="meeting-list">
        <div
          v-for="r in rows"
          :key="r.record.token"
          class="meeting-row"
          :data-token="r.record.token"
          :data-state="r.state.kind"
          :draggable="isWideScreen"
          @click="emit('open', r.record.token)"
          @dragstart="onDragStart($event, r.record)"
          @dragend="cleanupDragGhost()"
        >
          <!-- Three tiers, three lines. Name first (largest), the numbers it was
               measured with second, and what is inside it last. Everything used
               to share one line, so the eye had to read the whole card to find
               any single fact. -->
          <div class="meeting-row__main">
            <div class="meeting-row__head">
              <span class="meeting-row__title">{{ r.record.title || t('meeting.list.untitled') }}</span>
              <span v-if="r.state.label" class="meeting-pill" :class="r.state.cls">
                <Check v-if="r.state.icon === 'check'" :size="10" />
                <AlertTriangle v-else-if="r.state.icon === 'alert'" :size="10" />
                <span v-if="r.state.dot" class="meeting-pill__dot" />
                {{ r.state.label }}
              </span>
            </div>

            <div class="meeting-row__meta">
              <span
                class="meeting-row__m"
                :title="formatFullTimestamp(new Date(r.record.startedAt))"
              >{{ formatWhen(r.record.startedAt) }}</span>
              <template v-if="r.duration">
                <span class="meeting-row__sep">·</span>
                <span class="meeting-row__m" :class="{ 'is-accent': r.state.kind === 'running' }">{{ r.duration }}</span>
              </template>
              <template v-if="r.record.hasMeetingAudio">
                <span class="meeting-row__sep">·</span>
                <span class="meeting-row__m">{{ t('meeting.list.audioMerged') }}</span>
              </template>
              <template v-else-if="r.record.audioFiles > 0">
                <span class="meeting-row__sep">·</span>
                <span class="meeting-row__m">{{ t('meeting.list.audioShort', { n: r.record.audioFiles }) }}</span>
              </template>
              <template v-if="r.record.chatCount > 0">
                <span class="meeting-row__sep">·</span>
                <span class="meeting-row__m">{{ t('meeting.list.chatShort', { n: r.record.chatCount }) }}</span>
              </template>
              <template v-if="(r.record.shotCount ?? 0) > 0">
                <span class="meeting-row__sep">·</span>
                <span class="meeting-row__m">{{ t('meeting.list.shotShort', { n: r.record.shotCount }) }}</span>
              </template>
              <template v-if="r.speakers > 0">
                <span class="meeting-row__sep">·</span>
                <span class="meeting-row__m">{{ t('meeting.list.speakers', { n: r.speakers }) }}</span>
              </template>
            </div>

            <!-- What it produced. Hidden entirely when there is nothing to
                 say yet (a meeting that is still running), rather than leaving
                 an empty third line on the card. -->
            <div v-if="r.artifacts.length || r.showEmpty" class="meeting-row__tags">
              <span v-for="label in r.artifacts" :key="label" class="meeting-pill is-soft">{{ label }}</span>
              <span v-if="r.showEmpty" class="meeting-pill is-empty">{{ t('meeting.list.noArtifacts') }}</span>
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
    </div>

    <!-- Import progress sits above the footer rather than over the list: the
         record it belongs to is already in the list (the server answers as soon
         as the file is stored), so a blocking overlay would hide the very row
         the user is waiting on. -->
    <div v-if="importing" class="meeting-import-bar">
      <LoadingIndicator size="sm" inline />
      <span class="meeting-import-bar__text">{{ importLabel }}</span>
      <span class="meeting-import-bar__pct">{{ importing.percent }}%</span>
    </div>

    <!-- Footer actions use the shared .fbtn pills, the same language the task
         tab's action bar uses — see design-guide §按钮.

         - THIS client is publishing → the single action is the way back;
           starting or joining a second meeting would strand the running one.
         - A row is 进行中 but this client is not the publisher (another
           device hosts it) → the primary action joins that meeting. Creating
           is not offered: the room is per project, so a create here reuses
           the live room and then fails its publisher attach with 409.
         - Nothing is running → the three entry points. -->
    <div class="meeting-footer">
      <button
        v-if="liveToken"
        class="fbtn fbtn-primary"
        type="button"
        data-action="meeting-resume"
        @click="emit('resume')"
      >
        <ArrowLeft :size="14" />{{ t('meeting.list.liveResume') }}
      </button>
      <button
        v-else-if="runningToken"
        class="fbtn fbtn-primary"
        type="button"
        data-action="meeting-join-live"
        @click="emit('joinLive', runningToken)"
      >
        <LinkIcon :size="14" />{{ t('meeting.list.joinLive') }}
      </button>
      <template v-else>
        <button
          class="fbtn"
          type="button"
          data-action="meeting-import"
          :disabled="importing !== null"
          :title="importAvailable ? t('meeting.list.importHint') : t('meeting.list.importUnavailable')"
          @click="pickFiles"
        >
          <Upload :size="14" />{{ t('meeting.list.import') }}
        </button>
        <button class="fbtn" type="button" data-action="meeting-join" @click="emit('join')">
          <LinkIcon :size="14" />{{ t('meeting.list.join') }}
        </button>
        <button class="fbtn fbtn-primary" type="button" data-action="meeting-create" @click="emit('create')">
          <Plus :size="14" />{{ t('meeting.list.create') }}
        </button>
      </template>
    </div>

    <!-- The drop zone covers the whole panel, but a phone has no drag gesture,
         so the same code path is reachable from the footer button. -->
    <input
      ref="fileInput"
      class="meeting-file-input"
      type="file"
      multiple
      :accept="IMPORT_ACCEPT"
      @change="onPickFiles"
    />

    <div v-if="dropActive" class="meeting-drop-hint">
      <Upload :size="14" />{{ t('meeting.list.importDrop') }}
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
  ArrowLeft,
  Check,
  Link as LinkIcon,
  Plus,
  Trash2,
  Upload,
} from 'lucide-vue-next'
import { MEETING_ICON } from '@/utils/meetingIcon'
import { formatFullTimestamp, formatRelativeTime, startOfLocalDay } from '@/utils/format'
import LoadingIndicator from '@/components/common/LoadingIndicator.vue'
import PanelSkeleton from '@/components/common/PanelSkeleton.vue'
import RefreshButton from '@/components/common/RefreshButton.vue'
import MeetingEmpty from '@/components/live/MeetingEmpty.vue'
import HttpsNotice from '@/components/live/HttpsNotice.vue'
import { getNative } from '@/utils/clawbenchNative'
import { isHttpOrigin } from '@/utils/screenCaptureSupport'
import { useDialog } from '@/composables/useDialog'
import { useToast } from '@/composables/useToast'
import {
  isLiveRecord,
  meetingQuoteLabel,
  useMeetingHistory,
  type MeetingRecord,
} from '@/composables/useMeetingHistory'
import { meetingDragPayload, startQuoteDrag } from '@/utils/quoteDrag'
import { useWideScreenLayout } from '@/composables/useWideScreenLayout'
import { cleanupDragGhost } from '@/utils/attachDrag'
import { appLog } from '@/utils/appLog'
import '@/assets/modal-footer-btn.css'

const props = defineProps<{ active?: boolean; liveToken?: string }>()
const emit = defineEmits<{
  create: []
  join: []
  joinLive: [token: string]
  open: [token: string]
  resume: []
}>()

const { t } = useI18n()

const { isWideScreen } = useWideScreenLayout()
const dialog = useDialog()
const toast = useToast()
const history = useMeetingHistory()
const sessions = computed(() => history.sessions.value)
const loading = computed(() => history.loading.value)
const error = computed(() => history.error.value)

/**
 * Extensions the server can decode, used only to filter a drop BEFORE uploading
 * gigabytes. The authority is the server (it probes the actual file); this list
 * just spares the user a long upload of something that is obviously not media.
 *
 * Deliberately broader than what a browser can play: decoding happens on the
 * server with ffmpeg, so mkv/avi/wmv/amr and friends are all fair game.
 */
const IMPORT_EXTENSIONS = new Set([
  // audio
  'mp3', 'wav', 'm4a', 'aac', 'flac', 'ogg', 'oga', 'opus', 'amr', 'wma',
  'aiff', 'aif', 'ape', 'mka', 'weba', 'caf', 'au', 'ra', '3ga', 'm4b',
  // video
  'mp4', 'm4v', 'mov', 'mkv', 'webm', 'avi', 'wmv', 'flv', 'ts', 'mts',
  'm2ts', 'mpg', 'mpeg', '3gp', '3g2', 'ogv', 'rmvb', 'rm', 'asf', 'vob',
  'f4v', 'divx',
])

/** `accept` for the file picker: the MIME wildcards plus every extension. */
const IMPORT_ACCEPT = [
  'audio/*',
  'video/*',
  ...Array.from(IMPORT_EXTENSIONS, (ext) => `.${ext}`),
].join(',')

const importAvailable = computed(() => history.importAvailability.value.available)
const fileInput = ref<HTMLInputElement | null>(null)
const dropActive = ref(false)
const importing = ref<{ name: string; percent: number; index: number; total: number } | null>(null)
/** dragenter/dragleave fire per element crossed; a depth counter is the only
 *  way to know when the pointer actually left the panel. */
let dropDepth = 0

const importLabel = computed(() => {
  const state = importing.value
  if (!state) return ''
  if (state.total > 1) {
    return t('meeting.list.importProgressN', {
      name: state.name, index: state.index, total: state.total, pct: state.percent,
    })
  }
  return t('meeting.list.importProgress', { name: state.name, pct: state.percent })
})

/** True for an OS file drag. Internal drags (a meeting row being quoted) carry
 *  our own MIME types and must keep working untouched. */
function hasFiles(e: DragEvent): boolean {
  const types = e.dataTransfer?.types
  return !!types && Array.from(types).includes('Files')
}

function isMediaFile(file: File): boolean {
  if (file.type.startsWith('audio/') || file.type.startsWith('video/')) return true
  const dot = file.name.lastIndexOf('.')
  return dot >= 0 && IMPORT_EXTENSIONS.has(file.name.slice(dot + 1).toLowerCase())
}

function onDragEnter(e: DragEvent): void {
  if (!hasFiles(e)) return
  e.preventDefault()
  dropDepth++
  dropActive.value = true
}

function onDragOver(e: DragEvent): void {
  if (!hasFiles(e)) return
  e.preventDefault()
  if (e.dataTransfer) e.dataTransfer.dropEffect = 'copy'
}

function onDragLeave(): void {
  dropDepth = Math.max(0, dropDepth - 1)
  if (dropDepth === 0) dropActive.value = false
}

function onDrop(e: DragEvent): void {
  dropDepth = 0
  dropActive.value = false
  if (!hasFiles(e)) return
  // The chat column listens for OS drops too; a recording dropped on the
  // meeting list must not also become a chat attachment.
  e.preventDefault()
  e.stopPropagation()
  void importFiles(Array.from(e.dataTransfer?.files ?? []))
}

function pickFiles(): void {
  if (!importAvailable.value) {
    toast.show(t('meeting.list.importUnavailable'), { type: 'error', icon: 'warning' })
    return
  }
  fileInput.value?.click()
}

function onPickFiles(e: Event): void {
  const input = e.target as HTMLInputElement
  const files = Array.from(input.files ?? [])
  // Clearing lets the same file be picked twice in a row.
  input.value = ''
  void importFiles(files)
}

/** Map a server message key onto this panel's wording. */
function importFailText(reason: string): string {
  switch (reason) {
    case 'MeetingImportUnavailable':
      return t('meeting.list.importUnavailable')
    case 'MeetingImportTooLarge':
      return t('meeting.list.importTooLarge')
    case 'network':
      return t('meeting.list.importNetwork')
    default:
      return reason
  }
}

/**
 * Import a batch sequentially. One at a time on purpose: each file becomes its
 * own meeting and runs a recognition chain, and the server's transcription
 * slots are shared with live meetings.
 */
async function importFiles(files: File[]): Promise<void> {
  const media = files.filter(isMediaFile)
  if (media.length === 0) {
    if (files.length > 0) {
      toast.show(t('meeting.list.importUnsupported'), { type: 'error', icon: 'warning' })
    }
    return
  }
  if (!importAvailable.value) {
    toast.show(t('meeting.list.importUnavailable'), { type: 'error', icon: 'warning' })
    return
  }
  if (importing.value) return

  let imported = 0
  let firstToken = ''
  let lastReason = ''
  for (let i = 0; i < media.length; i++) {
    const file = media[i]
    importing.value = { name: file.name, percent: 0, index: i + 1, total: media.length }
    const result = await history.importFile(file, (percent) => {
      if (importing.value) importing.value = { ...importing.value, percent }
    })
    if (result.ok) {
      imported++
      if (!firstToken) firstToken = result.token
    } else {
      lastReason = result.reason
      appLog.w('MeetingList', 'import failed', result.reason)
    }
  }
  importing.value = null

  if (imported === 0) {
    toast.show(t('meeting.list.importFailed', { reason: importFailText(lastReason) }), {
      type: 'error', icon: 'warning', duration: 6000,
    })
    return
  }
  // The record already exists server-side; this reload is what puts it on
  // screen, and schedulePoll keeps the row's state fresh while it transcodes.
  await history.load()
  schedulePoll()
  toast.show(
    imported > 1 ? t('meeting.list.importDoneN', { n: imported }) : t('meeting.list.importDone'),
    { type: 'success', icon: 'success' },
  )
  // One file is a single, obvious destination — open it so the user watches
  // the transcript appear. A batch stays on the list.
  if (imported === 1 && firstToken) emit('open', firstToken)
}

/**
 * The meeting a row above advertises as 进行中 — the same `isLiveRecord` fact
 * the pill uses, so the footer can never offer a second meeting while the
 * list itself says one is running.
 */
const runningToken = computed(() => sessions.value.find(isLiveRecord)?.token ?? '')

/**
 * The desktop shell promotes its own http origin to a secure context, so
 * capture works there — but the link it hands out gets no such treatment: a
 * guest opening it in a browser cannot talk or share a screen without HTTPS.
 * Nothing else in the UI says so, and the fix is one click away.
 *
 * Scoped to the desktop shell on purpose: a plain browser on an insecure origin
 * cannot capture at all, and the create page already blocks with the full
 * notice. Showing both would say the same thing twice.
 *
 * Gated on the protocol, not on `isSecureContext`: the promotion above makes
 * that flag true here, so asking it would hide the banner on every desktop
 * shell that actually needs it.
 */
const showHttpsNotice = computed(
  () => getNative()?.isDesktopApp?.() === true && isHttpOrigin(),
)

/** `3小时前` / `昨天` / `2天前` — the list's own stamp; the detail spells the
 *  exact date and clock out in full, and the tooltip keeps it reachable here. */
function formatWhen(ms: number): string {
  if (!ms) return ''
  const d = new Date(ms)
  if (!Number.isFinite(d.getTime())) return ''
  // Past a day the shared formatter says "1天前", which is a worse word for
  // something the reader can name exactly. Only that bucket is special-cased;
  // newer records keep the relative wording, older ones "N天前" then the date.
  if (Date.now() - ms >= 86_400_000) {
    // Round, not floor: a DST day is 23 or 25 hours long.
    const days = Math.round((startOfLocalDay(new Date()) - startOfLocalDay(d)) / 86_400_000)
    if (days === 1) return t('time.yesterday')
  }
  return formatRelativeTime(d)
}

/**
 * A live meeting's clock ticks, so the row has to be able to re-render without
 * a reload. Only running rows read it, and the timer only exists while at least
 * one row is running.
 */
const now = ref(Date.now())
let clock: ReturnType<typeof setInterval> | null = null
const anyRunning = computed(() => sessions.value.some(isLiveRecord))

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
  if (pollTimer) clearTimeout(pollTimer)
  pollTimer = null
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
  /** Empty for a meeting that simply ended: there is no lifecycle left to
   *  report, and the artifacts on the third line already say what it holds. */
  label: string
  icon?: 'check' | 'alert'
  dot?: boolean
}

/**
 * The lifecycle of a record, and nothing else.
 *
 * This used to also carry "有纪要 / 仅转写 / 无产物", which forced a choice
 * between two independent facts (is the job healthy? what did it produce?) and
 * put the artifacts where the reader looked for status. The artifacts are now
 * their own line; this one only speaks up when the meeting is still running,
 * still generating, or broken.
 */
function artifactLabel(kind: 'minutes' | 'transcript' | 'revised'): string {
  if (kind === 'minutes') return t('meeting.detail.tabMinutes')
  if (kind === 'revised') return t('meeting.detail.tabRevised')
  return t('meeting.detail.tabTranscript')
}

function rowState(s: MeetingRecord): RowState {
  if (isLiveRecord(s)) {
    return { kind: 'running', cls: 'is-accent', label: t('meeting.list.running'), dot: true }
  }
  if (s.generation) {
    return {
      kind: 'generating',
      cls: 'is-accent',
      label: t('meeting.list.regenerating', { kind: artifactLabel(s.generation.step) }),
    }
  }
  const states = Object.values(s.artifacts ?? {})
  if (states.some((state) => state.jobStatus === 'failed')) {
    return { kind: 'failed', cls: 'is-danger', label: t('meeting.list.failed'), icon: 'alert' }
  }
  if (states.some((state) => state.stale)) {
    return { kind: 'stale', cls: 'is-warn', label: t('meeting.list.updating'), icon: 'alert' }
  }
  return { kind: '', cls: '', label: '' }
}

/** What a meeting produced, as pills, in reading order. */
function artifactsOf(s: MeetingRecord): string[] {
  const out: string[] = []
  if (s.hasMinutes) out.push(t('meeting.list.pillMinutes'))
  if (s.hasTranscript) out.push(t('meeting.list.pillTranscript'))
  if (s.hasRevised) out.push(t('meeting.list.pillRevised'))
  return out
}

const rows = computed(() =>
  sessions.value.map((record) => {
    const artifacts = artifactsOf(record)
    return {
      record,
      state: rowState(record),
      duration: durationOf(record),
      speakers: record.speakerCount ?? 0,
      artifacts,
      // Only an ENDED meeting can be "empty"; a running one simply has not
      // produced anything yet.
      showEmpty: artifacts.length === 0 && record.endedAt > 0,
    }
  }),
)

let pollTimer: ReturnType<typeof setTimeout> | null = null
const anyGenerating = computed(() => sessions.value.some((s) => Boolean(s.generation)))

/**
 * Poll the list only while a row is generating, and stop the moment none is.
 * A user who parks on the list still sees the completion; an idle list makes no
 * requests.
 */
function schedulePoll(): void {
  if (pollTimer) clearTimeout(pollTimer)
  pollTimer = null
  if (!anyGenerating.value || props.active === false) return
  pollTimer = setTimeout(() => {
    void history.load().then(() => schedulePoll())
  }, 5000)
}

function reload(): void {
  void history.load().then(() => schedulePoll())
}

// A row that settles between polls must stop the timer immediately; otherwise
// one more request fires before schedulePoll notices.
watch(anyGenerating, () => schedulePoll())

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
    if (active) {
      reload()
      return
    }
    if (pollTimer) clearTimeout(pollTimer)
    pollTimer = null
  },
)

onMounted(() => {
  if (props.active !== false) reload()
})
</script>

<style scoped>
.meeting-panel {
  position: relative;
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

/* Square card, hairline border, 8px gap — the task tab's row language. The
   card stays sharp while its labels are pills: the container is structure, the
   pills are metadata. */
.meeting-row {
  position: relative;
  display: flex;
  align-items: center;
  gap: var(--space-4, 8px);
  padding: var(--space-4, 8px) var(--space-4, 8px) var(--space-4, 8px) var(--space-5, 10px);
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

/* Three tiers. The gaps are what make the tiers readable: 6px between name and
   numbers, 6px between numbers and tags, and nothing tighter than that. */
.meeting-row__main {
  flex: 1;
  min-width: 0;
  display: flex;
  flex-direction: column;
  gap: var(--space-3, 6px);
}
.meeting-row__head {
  display: flex;
  align-items: center;
  gap: var(--space-3, 6px);
  min-width: 0;
}
.meeting-row__title {
  flex: 1;
  min-width: 0;
  font-size: var(--font-size-md);
  font-weight: var(--font-weight-semibold);
  line-height: calc(18 * var(--dp));
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
.meeting-row__m { white-space: nowrap; }
.meeting-row__m.is-accent { color: var(--accent-color); }
.meeting-row__sep { padding: 0 var(--space-2, 4px); color: var(--text-hint); }

.meeting-row__tags {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: var(--space-2, 4px);
  min-width: 0;
}

/* Pills: the app's badge language (radius-full, tinted fill, matching border).
   Colour is a secondary cue — the label always carries the meaning. */
.meeting-pill {
  display: inline-flex;
  align-items: center;
  gap: calc(3 * var(--dp));
  height: calc(18 * var(--dp));
  padding: 0 var(--space-3, 6px) 0 var(--space-3, 6px);
  border: 1px solid transparent;
  border-radius: var(--radius-full);
  font-size: var(--font-size-2xs);
  font-weight: var(--font-weight-medium);
  line-height: 1;
  white-space: nowrap;
  flex-shrink: 0;
}
.meeting-pill svg { flex-shrink: 0; opacity: 0.9; }

/* What is inside the meeting: neutral, because it is an attribute and not a
   state. The type pill is the one that gets a hue. */
.meeting-pill.is-soft {
  background: var(--bg-tertiary);
  color: var(--text-secondary);
  border-color: var(--border-color);
}
.meeting-pill.is-empty {
  background: transparent;
  border: 1px dashed var(--border-color);
  color: var(--text-muted);
}
.meeting-pill.is-accent {
  color: var(--accent-color);
  background: color-mix(in srgb, var(--accent-color) 13%, transparent);
  border-color: color-mix(in srgb, var(--accent-color) 40%, transparent);
}
.meeting-pill.is-warn {
  color: var(--color-orange);
  background: color-mix(in srgb, var(--color-orange) 13%, transparent);
  border-color: color-mix(in srgb, var(--color-orange) 38%, transparent);
}
.meeting-pill.is-danger {
  color: var(--color-red);
  background: color-mix(in srgb, var(--color-red) 13%, transparent);
  border-color: color-mix(in srgb, var(--color-red) 38%, transparent);
}
.meeting-pill__dot {
  width: calc(5 * var(--dp));
  height: calc(5 * var(--dp));
  border-radius: 50%;
  background: currentColor;
  animation: meeting-pill-pulse 1.4s ease-in-out infinite;
}
@keyframes meeting-pill-pulse {
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

/* The record rows are cards inset by their own horizontal padding inside
   `.meeting-body`; the placeholder sits in that same body, so its row inset has
   to be the card's, or the whole list jumps sideways when the data lands. */
.meeting-list-skeleton { --panel-skeleton-inset: calc(10 * var(--dp)); }

.meeting-error {
  padding: var(--space-4, 8px) var(--space-5, 10px);
  border-radius: var(--radius-md);
  background: color-mix(in srgb, var(--color-red) 10%, transparent);
  color: var(--color-red);
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

/* ── Import a dropped recording ─────────────────────────────────────────── */

/* The picker is only ever driven from the footer button, never shown. */
.meeting-file-input { display: none; }

/* Same dashed accent frame the chat column uses for a file drop, so "drop a
   file here" reads identically in both places. */
.is-drop-target::after {
  content: "";
  position: absolute;
  inset: 0;
  border: 2px dashed var(--accent-color, #0066cc);
  background: color-mix(in srgb, var(--accent-color, #0066cc) 6%, transparent);
  pointer-events: none;
  z-index: 10;
}

.meeting-drop-hint {
  position: absolute;
  left: 50%;
  top: 50%;
  transform: translate(-50%, -50%);
  display: flex;
  align-items: center;
  gap: var(--space-3, 6px);
  padding: var(--space-5, 10px) calc(18 * var(--dp));
  border-radius: var(--radius-full);
  background: var(--bg-primary);
  border: 1px solid var(--accent-color, #0066cc);
  box-shadow: 0 calc(4 * var(--dp)) calc(16 * var(--dp)) rgba(0, 0, 0, 0.15);
  color: var(--accent-color, #0066cc);
  font-size: var(--font-size-md);
  font-weight: var(--font-weight-semibold);
  pointer-events: none;
  z-index: 11;
  white-space: nowrap;
}

/* Sits between the list and the footer: it is a status line, not a modal. */
.meeting-import-bar {
  display: flex;
  align-items: center;
  gap: var(--space-3, 6px);
  padding: var(--space-3, 6px) var(--space-5, 10px);
  border-top: 1px solid var(--border-color);
  background: var(--bg-secondary);
  color: var(--text-secondary);
  font-size: var(--font-size-xs);
  flex-shrink: 0;
  min-width: 0;
}
.meeting-import-bar__text {
  flex: 1;
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.meeting-import-bar__pct {
  color: var(--text-primary);
  font-variant-numeric: tabular-nums;
  flex-shrink: 0;
}
</style>
