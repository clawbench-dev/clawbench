<template>
  <div class="notes-panel">
    <!-- Panel header, same shape as the sharing / port-forward panels: the
         state has to be readable from the tab, because a recording keeps
         running while the user is looking at something else. -->
    <div class="notes-panel__header">
      <span class="notes-panel__icon">
        <NotebookPen :size="14" />
      </span>
      <span class="notes-panel__title">{{ t('nav.notes') }}</span>
      <span v-if="phase === 'recording'" class="notes-panel__rec" data-role="notes-recording">
        <span class="notes-panel__dot" />
        {{ elapsed }}
      </span>
      <span v-else-if="phase === 'report'" class="notes-panel__state">
        {{ t('liveShare.notes.minutesTitle') }}
      </span>
      <span class="notes-panel__spacer" />
      <button
        v-if="phase === 'recording'"
        class="notes-panel__stop"
        type="button"
        data-action="notes-stop"
        @click="stop"
      >
        {{ t('liveShare.solo.stop') }}
      </button>
    </div>

    <div class="notes-panel__body">
      <!-- Recording. The clock is the only proof that the microphone is still
           live: without it a muted device looks exactly like a working one. -->
      <template v-if="phase === 'recording'">
        <div class="notes-hint">{{ t('liveShare.solo.hint') }}</div>
        <div v-if="blocks.length === 0" class="notes-hint">{{ t('liveShare.notes.listening') }}</div>
        <div v-else class="notes-lines" data-role="notes-lines">
          <div v-for="b in blocks" :key="b.key" class="notes-line">
            <span class="notes-line__time">{{ b.time }}</span>
            <span class="notes-line__text">{{ b.text }}</span>
          </div>
        </div>
        <div v-if="notes.lost.value > 0" class="notes-warn">
          {{ t('liveShare.notes.lostSegments', { n: notes.lost.value }) }}
        </div>
      </template>

      <!-- Report: the minutes of the session that just ended, plus the files
           they live next to. -->
      <template v-else-if="phase === 'report'">
        <div v-if="notes.phase.value === 'finalizing'" class="notes-hint">
          <LoadingIndicator size="sm" inline />
          {{
            notes.pending.value > 0
              ? t('liveShare.notes.uploading', { n: notes.pending.value })
              : t('liveShare.notes.finalizing')
          }}
        </div>
        <div v-else-if="notes.minutesPhase.value === 'generating'" class="notes-hint">
          <LoadingIndicator size="sm" inline />
          {{
            notes.revisePhase.value === 'generating'
              ? t('liveShare.notes.proofreading')
              : t('liveShare.notes.generating')
          }}
        </div>
        <div v-else-if="notes.minutesPhase.value === 'failed'" class="notes-error">
          {{ notes.error.value || t('liveShare.notes.generateFailed') }}
        </div>
        <div
          v-else-if="notes.minutes.value"
          class="notes-minutes markdown-body"
          v-html="renderedMinutes"
        ></div>
        <div v-else class="notes-hint">{{ t('liveShare.notes.empty') }}</div>

        <div
          v-if="notes.minutesPhase.value === 'ready' && notes.revisePhase.value === 'failed'"
          class="notes-warn"
          data-role="proofread-failed"
        >
          {{ t('liveShare.notes.proofreadFailed') }}
        </div>

        <div v-if="artifactRows.length" class="notes-artifacts" data-role="artifacts">
          <div class="notes-artifacts__label">{{ t('liveShare.notes.savedTo') }}</div>
          <div v-for="row in artifactRows" :key="row.key" class="notes-file">
            <span class="notes-file__label">{{ row.label }}</span>
            <span class="notes-file__path" :title="row.path">{{ row.path }}</span>
            <button class="notes-btn notes-btn--sm" type="button" @click="copy(row.path)">
              {{ t('liveShare.notes.copyPath') }}
            </button>
          </div>
        </div>

        <div class="notes-actions">
          <button
            v-if="notes.minutesPhase.value === 'failed'"
            class="notes-btn"
            type="button"
            @click="notes.generateMinutes(true)"
          >
            {{ t('common.retry') }}
          </button>
          <button
            class="notes-btn"
            type="button"
            :disabled="!notes.minutes.value"
            @click="copy(notes.minutes.value)"
          >
            {{ t('liveShare.notes.copy') }}
          </button>
          <button class="notes-btn notes-btn--primary" type="button" data-action="notes-done" @click="finishReport">
            {{ t('common.close') }}
          </button>
        </div>
      </template>

      <!-- One past session: its minutes (or the transcript when there are no
           minutes yet) with a way to open the file itself. -->
      <template v-else-if="selected">
        <div class="notes-detail__head">
          <button class="notes-link" type="button" data-action="notes-back" @click="closeDetail">
            {{ t('liveShare.history.back') }}
          </button>
          <span class="notes-detail__title">{{ sessionTitle(selected) }}</span>
          <span class="notes-detail__meta">{{ formatWhen(selected.startedAt || selected.updatedAt) }}</span>
        </div>

        <div v-if="availableKinds.length > 1" class="notes-switch">
          <button
            v-for="kind in availableKinds"
            :key="kind"
            class="notes-switch__btn"
            :class="{ active: kind === detailKind }"
            type="button"
            :data-kind="kind"
            @click="selectKind(kind)"
          >
            {{ kind === 'minutes' ? t('liveShare.history.viewMinutes') : t('liveShare.history.viewTranscript') }}
          </button>
        </div>

        <div v-if="detailLoading" class="notes-hint">
          <LoadingIndicator size="sm" inline />
          {{ t('liveShare.history.loading') }}
        </div>
        <div v-else-if="detailError" class="notes-error">{{ detailError }}</div>
        <div
          v-else-if="detailContent"
          class="notes-minutes markdown-body"
          data-role="notes-detail"
          v-html="renderedDetail"
        ></div>
        <div v-else class="notes-hint">{{ emptyDetailText }}</div>

        <div v-if="detailContent" class="notes-actions">
          <button class="notes-btn" type="button" @click="copy(detailContent)">
            {{ t('liveShare.history.copy') }}
          </button>
          <button class="notes-btn" type="button" data-action="notes-open-file" @click="openInEditor">
            {{ t('liveShare.history.openInEditor') }}
          </button>
        </div>
      </template>

      <!-- Setup + history: the dictaphone and the archive it feeds. -->
      <template v-else>
        <div class="notes-setup">
          <div class="notes-hint">{{ t('liveShare.solo.hint') }}</div>
          <input
            v-model="title"
            class="notes-input"
            type="text"
            maxlength="80"
            data-role="notes-title"
            :placeholder="t('liveShare.solo.titlePlaceholder')"
            @keydown.enter="start"
          />
          <div v-if="shareActive" class="notes-warn">{{ t('liveShare.solo.busy') }}</div>
          <div v-if="error" class="notes-error">{{ error }}</div>
          <div class="notes-actions">
            <button
              class="notes-btn notes-btn--primary"
              type="button"
              data-action="notes-start"
              :disabled="starting || shareActive"
              @click="start"
            >
              {{ starting ? t('liveShare.solo.starting') : t('liveShare.solo.start') }}
            </button>
          </div>
        </div>

        <div class="notes-history">
          <div class="notes-history__head">
            <span class="notes-history__title">{{ t('liveShare.history.title') }}</span>
            <button
              class="notes-link"
              type="button"
              data-action="notes-refresh"
              :disabled="loadingSessions"
              @click="loadSessions"
            >
              {{ t('liveShare.history.refresh') }}
            </button>
          </div>

          <div v-if="loadingSessions && sessions.length === 0" class="notes-hint">
            <LoadingIndicator size="sm" inline />
            {{ t('liveShare.history.loading') }}
          </div>
          <div v-else-if="historyError" class="notes-error">{{ historyError }}</div>
          <div v-else-if="sessions.length === 0" class="notes-hint">
            {{ t('liveShare.history.empty') }}
          </div>
          <div v-else class="notes-history__list">
            <button
              v-for="s in sessions"
              :key="s.dir"
              class="notes-row"
              type="button"
              :data-dir="s.dir"
              @click="openSession(s)"
            >
              <span class="notes-row__title">{{ sessionTitle(s) }}</span>
              <span class="notes-row__meta">
                <span>{{ formatWhen(s.startedAt || s.updatedAt) }}</span>
                <span v-if="s.audioFiles > 0">{{ t('liveShare.history.audioCount', { n: s.audioFiles }) }}</span>
                <span v-if="!s.hasMinutes && s.hasTranscript" class="notes-row__tag">
                  {{ t('liveShare.history.viewTranscript') }}
                </span>
                <span v-if="!s.hasMinutes && !s.hasTranscript" class="notes-row__tag notes-row__tag--muted">
                  {{ t('liveShare.history.noMinutes') }}
                </span>
              </span>
            </button>
          </div>
        </div>
      </template>
    </div>
  </div>
</template>

<script setup lang="ts">
/**
 * AI 听记: the dictaphone and the archive of what it recorded.
 *
 * This used to be a header-button dialog. It became a dock tab for the same
 * reason screen sharing did — a recording is a workspace, not a one-shot
 * action: the transcript keeps arriving while the user looks elsewhere, and the
 * minutes of past sessions are something you come back to read.
 *
 * The recorder itself is `useLiveNotes`, the same module singleton the share
 * panel drives, so only one session can run at a time; starting here is refused
 * while a share is on air, and a share starting mid-recording stops this one.
 *
 * History is read from the project's meeting folders (see
 * `GET /api/live-share/notes/sessions`), not from the database rows: the rows
 * expire after the retention window, the folders do not.
 */
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { NotebookPen } from 'lucide-vue-next'
import LoadingIndicator from '@/components/common/LoadingIndicator.vue'
import { useToast } from '@/composables/useToast'
import { useLiveShare } from '@/composables/useLiveShare'
import { groupNoteSegments, useLiveNotes } from '@/composables/useLiveNotes'
import { renderMarkdownHtml } from '@/composables/useMarkdownRenderer'
import { appLog } from '@/utils/appLog'

const props = defineProps<{ active?: boolean }>()
const emit = defineEmits<{ 'open-file': [path: string] }>()

const { t, locale } = useI18n()
const toast = useToast()
const notes = useLiveNotes()
const share = useLiveShare()

type Phase = 'idle' | 'recording' | 'report'
type ArtifactKind = 'minutes' | 'transcript' | 'revised'

/** One meeting folder, as returned by the history endpoint. */
interface NotesSession {
  dir: string
  dirPath: string
  title: string
  startedAt: number
  updatedAt: number
  hasMinutes: boolean
  hasTranscript: boolean
  hasRevised: boolean
  audioFiles: number
}

const phase = ref<Phase>('idle')
const title = ref('')
const error = ref('')
const starting = ref(false)
const elapsedMs = ref(0)

const sessions = ref<NotesSession[]>([])
const loadingSessions = ref(false)
const historyError = ref('')
const selected = ref<NotesSession | null>(null)
const detailKind = ref<ArtifactKind>('minutes')
const detailContent = ref('')
// File name the server actually read for the open artifact. A transcript may
// still be the pre-SRT `transcript.md`, so the name cannot be guessed here.
const detailFile = ref('')
const detailLoading = ref(false)
const detailError = ref('')

let audioCtx: AudioContext | null = null
let micStream: MediaStream | null = null
let timer: ReturnType<typeof setInterval> | null = null
let startedAt = 0

/** A share is on air: the notes state machine is already in use. */
const shareActive = computed(() => share.state.value !== 'idle')

function offsetLabel(ms: number): string {
  const total = Math.max(0, Math.floor(ms / 1000))
  return `${String(Math.floor(total / 60)).padStart(2, '0')}:${String(total % 60).padStart(2, '0')}`
}

const blocks = computed(() => {
  const list = groupNoteSegments(notes.segments.value).slice(-40)
  const base = list.find((b) => b.startedAt > 0)?.startedAt ?? 0
  return list.map((b) => ({
    key: b.key,
    // One speaker means a speaker label on every line is pure noise; the
    // timestamp is what actually helps while scrolling back.
    time: base > 0 && b.startedAt > 0 ? offsetLabel(b.startedAt - base) : '--:--',
    text: b.text,
  }))
})

const elapsed = computed(() => {
  const total = Math.floor(elapsedMs.value / 1000)
  return `${String(Math.floor(total / 60)).padStart(2, '0')}:${String(total % 60).padStart(2, '0')}`
})

const renderedMinutes = computed(() =>
  notes.minutes.value ? renderMarkdownHtml(notes.minutes.value) : '',
)

const renderedDetail = computed(() =>
  detailContent.value ? renderMarkdownHtml(detailContent.value) : '',
)

const artifactRows = computed(() => {
  const a = notes.artifacts.value
  const rows: Array<{ key: string; label: string; path: string }> = []
  if (a.minutes) {
    rows.push({ key: 'minutes', label: t('liveShare.notes.artifactMinutes'), path: a.minutes })
  }
  if (a.transcript) {
    rows.push({
      key: 'transcript',
      label: t('liveShare.notes.artifactTranscript'),
      path: a.transcript,
    })
  }
  if (a.revised) {
    rows.push({ key: 'revised', label: t('liveShare.notes.artifactRevised'), path: a.revised })
  }
  if (a.audio) {
    rows.push({
      key: 'audio',
      label: t('liveShare.notes.artifactAudio', { n: a.audioFiles ?? 0 }),
      path: a.audio,
    })
  }
  return rows
})

/** The artifacts a session actually has, in reading order. */
const availableKinds = computed<ArtifactKind[]>(() => {
  const s = selected.value
  if (!s) return []
  const kinds: ArtifactKind[] = []
  if (s.hasMinutes) kinds.push('minutes')
  if (s.hasTranscript) kinds.push('transcript')
  return kinds
})

const emptyDetailText = computed(() => {
  const s = selected.value
  if (!s) return ''
  return s.hasTranscript ? t('liveShare.history.noMinutes') : t('liveShare.history.noTranscript')
})

function sessionTitle(s: NotesSession): string {
  return s.title || t('liveShare.history.untitled')
}

/** Short, locale-aware timestamp for a session row. */
function formatWhen(ms: number): string {
  if (!ms) return ''
  const date = new Date(ms)
  if (Number.isNaN(date.getTime())) return ''
  return date.toLocaleString(locale.value, {
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
  })
}

/** Asks the server for a session token, then starts capturing. */
async function start(): Promise<void> {
  if (starting.value || shareActive.value || phase.value === 'recording') return
  starting.value = true
  error.value = ''
  selected.value = null
  try {
    const resp = await fetch('/api/live-share/notes/session', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ title: title.value.trim() }),
    })
    if (!resp.ok) throw new Error(`session request failed: ${resp.status}`)
    const data = (await resp.json()) as { token?: string }
    if (!data.token) throw new Error('session request returned no token')

    micStream = await navigator.mediaDevices.getUserMedia({ audio: true })
    audioCtx = new AudioContext()
    notes.start({
      token: data.token,
      ctx: audioCtx,
      micStream,
      title: title.value.trim(),
      label: () => t('liveShare.notes.speakerMe'),
    })
    startedAt = Date.now()
    elapsedMs.value = 0
    timer = setInterval(() => {
      elapsedMs.value = Date.now() - startedAt
    }, 1000)
    phase.value = 'recording'
  } catch (e) {
    appLog.w('NotesPanel', 'start failed', e)
    error.value =
      e instanceof DOMException && e.name === 'NotAllowedError'
        ? t('liveShare.solo.micDenied')
        : e instanceof Error
          ? e.message
          : String(e)
    await teardown()
  } finally {
    starting.value = false
  }
}

/** Stops the recorder, waits for the backlog, then shows the minutes. */
async function stop(): Promise<void> {
  if (phase.value !== 'recording') return
  phase.value = 'report'
  await notes.stopCapture()
  await teardown()
  await notes.finishUploads()
  // The meeting folder exists from the first uploaded segment, so the archive
  // can be refreshed right away — its minutes appear once the job finishes.
  await loadSessions()
}

/** Releases the microphone and the audio graph. Idempotent. */
async function teardown(): Promise<void> {
  if (timer !== null) {
    clearInterval(timer)
    timer = null
  }
  micStream?.getTracks().forEach((track) => track.stop())
  micStream = null
  if (audioCtx) {
    const ctx = audioCtx
    audioCtx = null
    await ctx.close().catch(() => undefined)
  }
}

/** Leaves the report view for the setup + history view. */
function finishReport(): void {
  notes.reset()
  phase.value = 'idle'
  title.value = ''
  error.value = ''
}

async function loadSessions(): Promise<void> {
  if (loadingSessions.value) return
  loadingSessions.value = true
  historyError.value = ''
  try {
    const resp = await fetch('/api/live-share/notes/sessions')
    if (!resp.ok) throw new Error(`history request failed: ${resp.status}`)
    const data = (await resp.json()) as { sessions?: NotesSession[] }
    sessions.value = Array.isArray(data.sessions) ? data.sessions : []
  } catch (e) {
    appLog.w('NotesPanel', 'load history failed', e)
    historyError.value = t('liveShare.history.loadFailed')
  } finally {
    loadingSessions.value = false
  }
}

async function openSession(s: NotesSession): Promise<void> {
  selected.value = s
  detailKind.value = s.hasMinutes ? 'minutes' : 'transcript'
  detailContent.value = ''
  detailError.value = ''
  await loadDetail()
}

async function selectKind(kind: ArtifactKind): Promise<void> {
  if (detailKind.value === kind) return
  detailKind.value = kind
  detailContent.value = ''
  detailError.value = ''
  await loadDetail()
}

function closeDetail(): void {
  selected.value = null
  detailContent.value = ''
  detailError.value = ''
}

async function loadDetail(): Promise<void> {
  const s = selected.value
  if (!s) return
  const kind = detailKind.value
  if (kind === 'minutes' ? !s.hasMinutes : !s.hasTranscript) {
    detailContent.value = ''
    return
  }
  detailLoading.value = true
  detailError.value = ''
  try {
    const url = `/api/live-share/notes/sessions/artifact?dir=${encodeURIComponent(s.dir)}&kind=${kind}`
    const resp = await fetch(url)
    if (!resp.ok) throw new Error(`artifact request failed: ${resp.status}`)
    const data = (await resp.json()) as { content?: string; file?: string }
    detailContent.value = data.content ?? ''
    detailFile.value = data.file ?? ''
  } catch (e) {
    appLog.w('NotesPanel', 'load artifact failed', e)
    detailError.value = t('liveShare.history.loadFailed')
  } finally {
    detailLoading.value = false
  }
}

/** Opens the artifact in the file viewer (same path the row points at). */
function openInEditor(): void {
  const s = selected.value
  if (!s) return
  const file = detailFile.value || (detailKind.value === 'minutes' ? 'minutes.md' : 'transcript.srt')
  // The server's path uses the host's separator; joining with "/" would break
  // on Windows, where dirPath ends in a backslash.
  const sep = s.dirPath.includes('\\') ? '\\' : '/'
  emit('open-file', `${s.dirPath}${sep}${file}`)
}

async function copy(text: string): Promise<void> {
  if (!text) return
  try {
    await navigator.clipboard.writeText(text)
    toast.show(t('common.copied'), { type: 'success', icon: 'copy' })
  } catch (e) {
    appLog.w('NotesPanel', 'copy failed', e)
  }
}

// A share started from the dock takes over the shared notes state machine;
// this panel cannot survive that, so it stops its own recording rather than
// showing another session's transcript.
watch(shareActive, (active) => {
  if (active && phase.value === 'recording') void stop()
})

// Switching to the tab re-reads the archive: a meeting that finished while the
// user was elsewhere must show up without a manual refresh.
watch(
  () => props.active,
  (active) => {
    if (active && phase.value !== 'recording') void loadSessions()
  },
)

onMounted(() => {
  if (props.active !== false) void loadSessions()
})

onBeforeUnmount(() => {
  void teardown()
})
</script>

<style scoped>
.notes-panel {
  display: flex;
  flex-direction: column;
  flex: 1;
  min-height: 0;
  overflow: hidden;
  background: var(--bg-primary, #ffffff);
}

.notes-panel__header {
  display: flex;
  align-items: center;
  height: var(--header-height);
  padding: 0 var(--space-5, 10px) 0 var(--space-6, 12px);
  gap: var(--space-3, 6px);
  flex-shrink: 0;
  background: var(--bg-primary);
  border-bottom: 1px solid var(--border-color, #e5e5e5);
}

.notes-panel__icon {
  display: flex;
  align-items: center;
  color: var(--text-secondary, #666);
  flex-shrink: 0;
}

.notes-panel__title {
  font-size: var(--font-size-md);
  font-weight: var(--font-weight-semibold);
  color: var(--text-primary, #1a1a1a);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.notes-panel__spacer {
  flex: 1;
}

/* "Recording" marker. Red dot plus the clock: the dock is scanned, not read,
   and a running dictaphone must be obvious without opening the tab. */
.notes-panel__rec {
  display: inline-flex;
  align-items: center;
  gap: var(--space-2, 4px);
  height: calc(18 * var(--dp));
  padding: 0 var(--space-3, 6px);
  border-radius: var(--radius-full, 999px);
  background: color-mix(in srgb, var(--color-red, #cf222e) 14%, transparent);
  color: var(--color-red, #cf222e);
  font-size: var(--font-size-xs, 11px);
  font-weight: var(--font-weight-semibold);
  font-variant-numeric: tabular-nums;
  flex-shrink: 0;
}

.notes-panel__dot {
  width: 6px;
  height: 6px;
  border-radius: 50%;
  background: currentColor;
  animation: notes-panel-pulse 1.4s ease-in-out infinite;
}

@keyframes notes-panel-pulse {
  0%,
  100% {
    opacity: 0.35;
  }
  50% {
    opacity: 1;
  }
}

.notes-panel__state {
  font-size: var(--font-size-xs, 11px);
  color: var(--text-secondary, #666);
  flex-shrink: 0;
}

.notes-panel__stop {
  height: calc(26 * var(--dp));
  padding: 0 var(--space-5, 10px);
  border: 1px solid color-mix(in srgb, var(--color-red, #cf222e) 40%, transparent);
  border-radius: var(--radius-md, 6px);
  background: color-mix(in srgb, var(--color-red, #cf222e) 10%, transparent);
  color: var(--color-red, #cf222e);
  font-size: var(--font-size-sm, 12px);
  cursor: pointer;
  flex-shrink: 0;
}

.notes-panel__stop:hover {
  background: color-mix(in srgb, var(--color-red, #cf222e) 18%, transparent);
}

.notes-panel__body {
  flex: 1;
  min-height: 0;
  overflow-y: auto;
  display: flex;
  flex-direction: column;
  gap: var(--space-5, 10px);
  padding: var(--space-5, 10px) var(--space-6, 12px) var(--space-7, 16px);
}

.notes-hint {
  display: flex;
  align-items: center;
  gap: var(--space-3, 6px);
  margin: 0;
  font-size: var(--font-size-sm, 12px);
  line-height: 1.6;
  color: var(--text-secondary, #666);
}

.notes-warn,
.notes-error {
  padding: var(--space-4, 8px) var(--space-5, 10px);
  border-radius: var(--radius-md, 6px);
  font-size: var(--font-size-xs, 11px);
  line-height: 1.5;
}

.notes-warn {
  background: color-mix(in srgb, var(--color-orange, #bc4c00) 10%, transparent);
  color: var(--color-orange, #bc4c00);
}

.notes-error {
  background: color-mix(in srgb, var(--color-red, #cf222e) 10%, transparent);
  color: var(--color-red, #cf222e);
}

.notes-setup {
  display: flex;
  flex-direction: column;
  gap: var(--space-4, 8px);
  padding-bottom: var(--space-5, 10px);
  border-bottom: 1px solid var(--border-color, #e5e5e5);
}

.notes-input {
  width: 100%;
  height: calc(32 * var(--dp));
  padding: 0 var(--space-5, 10px);
  border: 1px solid var(--border-color, #e5e5e5);
  border-radius: var(--radius-md, 6px);
  background: var(--bg-secondary, #f6f8fa);
  color: var(--text-primary, #1a1a1a);
  font-size: var(--font-size-sm, 12px);
}

.notes-input:focus {
  outline: none;
  border-color: var(--accent-color, #0066cc);
}

.notes-lines {
  display: flex;
  flex-direction: column;
  gap: var(--space-3, 6px);
  padding: var(--space-4, 8px) var(--space-5, 10px);
  border-radius: var(--radius-md, 6px);
  background: var(--bg-secondary, #f6f8fa);
}

.notes-line {
  display: flex;
  gap: var(--space-3, 6px);
  font-size: var(--font-size-sm, 12px);
  line-height: 1.5;
}

.notes-line__time {
  flex: 0 0 auto;
  color: var(--text-muted, #8b949e);
  font-family: var(--font-mono, ui-monospace, monospace);
  font-variant-numeric: tabular-nums;
}

.notes-line__text {
  flex: 1 1 auto;
  min-width: 0;
  color: var(--text-primary, #1a1a1a);
  overflow-wrap: break-word;
}

/* The panel body is the only scroll container. `.markdown-body` is
   `flex: 1; overflow-y: auto` for the chat reading column, which here would
   create a second scrollbar and push the actions to the bottom of the panel. */
.notes-minutes {
  flex: 0 0 auto;
  overflow: visible;
  padding: 0;
  font-size: var(--font-size-sm, 12px);
  line-height: 1.7;
  color: var(--text-primary, #1a1a1a);
}

.notes-artifacts {
  display: flex;
  flex-direction: column;
  gap: var(--space-2, 4px);
}

.notes-artifacts__label {
  font-size: var(--font-size-xs, 11px);
  color: var(--text-muted, #656d76);
}

.notes-file {
  display: flex;
  align-items: center;
  gap: var(--space-3, 6px);
  min-width: 0;
  padding: var(--space-3, 6px) var(--space-5, 10px);
  border-radius: var(--radius-md, 6px);
  background: var(--bg-tertiary, #f6f8fa);
  font-size: var(--font-size-xs, 11px);
}

.notes-file__label {
  flex: 0 0 auto;
  color: var(--text-muted, #656d76);
}

.notes-file__path {
  flex: 1 1 auto;
  min-width: 0;
  font-family: var(--font-mono, ui-monospace, monospace);
  color: var(--text-primary, #1f2328);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.notes-actions {
  display: flex;
  gap: var(--space-4, 8px);
  justify-content: flex-end;
}

.notes-btn {
  height: calc(28 * var(--dp));
  padding: 0 var(--space-6, 12px);
  border: 1px solid var(--border-color, #e5e5e5);
  border-radius: var(--radius-md, 6px);
  background: var(--bg-primary, #fff);
  color: var(--text-primary, #1a1a1a);
  font-size: var(--font-size-sm, 12px);
  cursor: pointer;
  flex-shrink: 0;
}

.notes-btn:hover:not(:disabled) {
  border-color: var(--accent-color, #0066cc);
  color: var(--accent-color, #0066cc);
}

.notes-btn:disabled {
  opacity: 0.5;
  cursor: not-allowed;
}

.notes-btn--sm {
  height: calc(24 * var(--dp));
  padding: 0 var(--space-4, 8px);
  font-size: var(--font-size-xs, 11px);
}

.notes-btn--primary {
  border-color: var(--accent-color, #0066cc);
  background: var(--accent-color, #0066cc);
  color: var(--bg-primary, #fff);
}

.notes-btn--primary:hover:not(:disabled) {
  color: var(--bg-primary, #fff);
  filter: brightness(1.08);
}

.notes-history {
  display: flex;
  flex-direction: column;
  gap: var(--space-3, 6px);
}

.notes-history__head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: var(--space-4, 8px);
}

.notes-history__title {
  font-size: var(--font-size-sm, 12px);
  font-weight: var(--font-weight-semibold, 600);
  color: var(--text-primary, #1a1a1a);
}

.notes-link {
  padding: 0;
  border: none;
  background: none;
  color: var(--accent-color, #0066cc);
  font-size: var(--font-size-xs, 11px);
  cursor: pointer;
}

.notes-link:disabled {
  color: var(--text-muted, #8b949e);
  cursor: not-allowed;
}

.notes-history__list {
  display: flex;
  flex-direction: column;
  gap: var(--space-2, 4px);
}

.notes-row {
  display: flex;
  flex-direction: column;
  gap: var(--space-1, 2px);
  width: 100%;
  padding: var(--space-4, 8px) var(--space-5, 10px);
  border: 1px solid transparent;
  border-radius: var(--radius-md, 6px);
  background: var(--bg-secondary, #f6f8fa);
  text-align: left;
  cursor: pointer;
}

.notes-row:hover {
  border-color: var(--accent-color, #0066cc);
}

.notes-row__title {
  font-size: var(--font-size-sm, 12px);
  font-weight: var(--font-weight-medium, 500);
  color: var(--text-primary, #1a1a1a);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.notes-row__meta {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: var(--space-3, 6px);
  font-size: var(--font-size-xs, 11px);
  color: var(--text-muted, #656d76);
}

.notes-row__tag {
  padding: 0 var(--space-3, 6px);
  border-radius: var(--radius-sm, 4px);
  background: var(--bg-tertiary, #eaeef2);
  color: var(--text-secondary, #666);
}

.notes-row__tag--muted {
  color: var(--text-muted, #8b949e);
}

.notes-detail__head {
  display: flex;
  flex-wrap: wrap;
  align-items: baseline;
  gap: var(--space-3, 6px);
}

.notes-detail__title {
  flex: 1 1 auto;
  min-width: 0;
  font-size: var(--font-size-md, 13px);
  font-weight: var(--font-weight-semibold, 600);
  color: var(--text-primary, #1a1a1a);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.notes-detail__meta {
  font-size: var(--font-size-xs, 11px);
  color: var(--text-muted, #656d76);
}

.notes-switch {
  display: inline-flex;
  align-self: flex-start;
  gap: var(--space-1, 2px);
  padding: var(--space-1, 2px);
  border-radius: var(--radius-md, 6px);
  background: var(--bg-secondary, #f6f8fa);
}

.notes-switch__btn {
  height: calc(24 * var(--dp));
  padding: 0 var(--space-5, 10px);
  border: none;
  border-radius: var(--radius-sm, 4px);
  background: transparent;
  color: var(--text-secondary, #666);
  font-size: var(--font-size-xs, 11px);
  cursor: pointer;
}

.notes-switch__btn.active {
  background: var(--bg-primary, #fff);
  color: var(--text-primary, #1a1a1a);
  box-shadow: var(--shadow-sm);
}
</style>
