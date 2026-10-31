<template>
  <ModalDialog :open="open" :title="t('liveShare.solo.title')" :z-index="2500" @close="onClose">
    <div class="solo">
      <!-- Setup. One field, one button: this is a dictaphone, not a meeting. -->
      <template v-if="phase === 'idle'">
        <p class="solo-hint">{{ t('liveShare.solo.hint') }}</p>
        <input
          v-model="title"
          class="solo-input"
          type="text"
          maxlength="80"
          data-role="solo-title"
          :placeholder="t('liveShare.solo.titlePlaceholder')"
          @keydown.enter="start"
        />
        <div v-if="shareActive" class="solo-warn">{{ t('liveShare.solo.busy') }}</div>
        <div v-if="error" class="solo-error">{{ error }}</div>
      </template>

      <!-- Recording. The elapsed time is the only thing that proves it is still
           running: without it a silent microphone looks exactly like a broken
           one. -->
      <template v-else-if="phase === 'recording'">
        <div class="solo-rec">
          <span class="solo-rec__dot" />
          <span>{{ t('liveShare.solo.recording') }}</span>
          <span class="solo-rec__time" data-role="solo-elapsed">{{ elapsed }}</span>
        </div>
        <div v-if="blocks.length === 0" class="solo-hint">{{ t('liveShare.notes.listening') }}</div>
        <div v-else class="solo-lines">
          <div v-for="b in blocks" :key="b.key" class="solo-line">
            <span class="solo-line__time">{{ b.time }}</span>
            <span class="solo-line__text">{{ b.text }}</span>
          </div>
        </div>
        <div v-if="notes.lost.value > 0" class="solo-warn">
          {{ t('liveShare.notes.lostSegments', { n: notes.lost.value }) }}
        </div>
      </template>

      <!-- Report: minutes + the files they live next to. -->
      <template v-else>
        <div v-if="notes.phase.value === 'finalizing'" class="solo-hint">
          <LoadingIndicator size="sm" inline />
          {{
            notes.pending.value > 0
              ? t('liveShare.notes.uploading', { n: notes.pending.value })
              : t('liveShare.notes.finalizing')
          }}
        </div>
        <div v-else-if="notes.minutesPhase.value === 'generating'" class="solo-hint">
          <LoadingIndicator size="sm" inline />
          {{
            notes.revisePhase.value === 'generating'
              ? t('liveShare.notes.proofreading')
              : t('liveShare.notes.generating')
          }}
        </div>
        <div v-else-if="notes.minutesPhase.value === 'failed'" class="solo-error">
          {{ notes.error.value || t('liveShare.notes.generateFailed') }}
        </div>
        <div
          v-else-if="notes.minutes.value"
          class="solo-minutes markdown-body"
          v-html="renderedMinutes"
        ></div>
        <div v-else class="solo-hint">{{ t('liveShare.notes.empty') }}</div>

        <div
          v-if="notes.minutesPhase.value === 'ready' && notes.revisePhase.value === 'failed'"
          class="solo-warn"
          data-role="proofread-failed"
        >
          {{ t('liveShare.notes.proofreadFailed') }}
        </div>

        <div v-if="artifactRows.length" class="solo-artifacts" data-role="artifacts">
          <div class="solo-artifacts__label">{{ t('liveShare.notes.savedTo') }}</div>
          <div v-for="row in artifactRows" :key="row.key" class="solo-file">
            <span class="solo-file__label">{{ row.label }}</span>
            <span class="solo-file__path" :title="row.path">{{ row.path }}</span>
            <button class="solo-btn solo-btn--sm" type="button" @click="copyPath(row.path)">
              {{ t('liveShare.notes.copyPath') }}
            </button>
          </div>
        </div>
      </template>
    </div>

    <template #footer>
      <template v-if="phase === 'report'">
        <button
          v-if="notes.minutesPhase.value === 'failed'"
          class="fbtn"
          type="button"
          @click="notes.generateMinutes(true)"
        >
          {{ t('common.retry') }}
        </button>
        <button class="fbtn" type="button" :disabled="!notes.minutes.value" @click="copyMinutes">
          {{ t('liveShare.notes.copy') }}
        </button>
        <button class="fbtn fbtn-primary" type="button" @click="onClose">
          {{ t('common.close') }}
        </button>
      </template>
      <template v-else>
        <button class="fbtn" type="button" @click="onClose">
          {{ t('common.cancel') }}
        </button>
        <button
          v-if="phase === 'idle'"
          class="fbtn fbtn-primary"
          type="button"
          data-action="solo-start"
          :disabled="starting || shareActive"
          @click="start"
        >
          {{ starting ? t('liveShare.solo.starting') : t('liveShare.solo.start') }}
        </button>
        <button v-else class="fbtn fbtn-danger" type="button" data-action="solo-stop" @click="stop">
          {{ t('liveShare.solo.stop') }}
        </button>
      </template>
    </template>
  </ModalDialog>
</template>

<script setup lang="ts">
/**
 * Voice notes: microphone-only dictation, no room and no link.
 *
 * It reuses the whole 听记 pipeline — segment upload, transcription, the
 * context-aware proofread, the minutes, the permanent files — because none of
 * that ever needed a meeting. The only thing this dialog adds is a session
 * token: the server mints one and records it, which is what makes the upload
 * endpoints authorize without a room behind them.
 *
 * The recorder is `useLiveNotes`, the same module singleton the share panel
 * uses. Two sessions cannot run at once (one shared state machine), so starting
 * here is refused while a share is on air, and the share panel refuses to start
 * while this is recording.
 */
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import ModalDialog from '@/components/common/ModalDialog.vue'
import LoadingIndicator from '@/components/common/LoadingIndicator.vue'
import { useToast } from '@/composables/useToast'
import { useLiveShare } from '@/composables/useLiveShare'
import { groupNoteSegments, useLiveNotes } from '@/composables/useLiveNotes'
import { renderMarkdownHtml } from '@/composables/useMarkdownRenderer'
import { appLog } from '@/utils/appLog'
import '@/assets/modal-footer-btn.css'

const props = defineProps<{ open: boolean }>()
const emit = defineEmits<{ close: [] }>()

const { t } = useI18n()
const toast = useToast()
const notes = useLiveNotes()
const share = useLiveShare()

const phase = ref<'idle' | 'recording' | 'report'>('idle')
const title = ref('')
const error = ref('')
const starting = ref(false)
const elapsedMs = ref(0)

let audioCtx: AudioContext | null = null
let micStream: MediaStream | null = null
let timer: ReturnType<typeof setInterval> | null = null
let startedAt = 0

/** A share is on air: the notes state machine is already in use. */
const shareActive = computed(() => share.state.value !== 'idle')

/** mm:ss since the first transcribed line, matching the transcript file. */
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

const renderedMinutes = computed(() =>
  notes.minutes.value ? renderMarkdownHtml(notes.minutes.value) : '',
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

const elapsed = computed(() => {
  const total = Math.floor(elapsedMs.value / 1000)
  const m = Math.floor(total / 60)
  const s = total % 60
  return `${String(m).padStart(2, '0')}:${String(s).padStart(2, '0')}`
})

/** Asks the server for a session token, then starts capturing. */
async function start(): Promise<void> {
  if (starting.value || shareActive.value) return
  starting.value = true
  error.value = ''
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
    appLog.w('SoloNotes', 'start failed', e)
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

/** Closing mid-recording must stop the microphone: a hidden recorder is a bug. */
async function onClose(): Promise<void> {
  if (phase.value === 'recording') {
    await notes.stopCapture()
    await teardown()
  }
  notes.reset()
  phase.value = 'idle'
  title.value = ''
  error.value = ''
  emit('close')
}

async function copyMinutes(): Promise<void> {
  try {
    await navigator.clipboard.writeText(notes.minutes.value)
    toast.show(t('common.copied'), { type: 'success', icon: 'copy' })
  } catch (e) {
    appLog.w('SoloNotes', 'copy minutes failed', e)
  }
}

async function copyPath(path: string): Promise<void> {
  try {
    await navigator.clipboard.writeText(path)
    toast.show(t('common.copied'), { type: 'success', icon: 'copy' })
  } catch (e) {
    appLog.w('SoloNotes', 'copy path failed', e)
  }
}

// A share started from the dock takes over the shared notes state machine;
// this dialog cannot survive that, so it closes itself rather than showing
// another session's transcript.
watch(shareActive, (active) => {
  if (active && phase.value !== 'idle') void onClose()
})

watch(
  () => props.open,
  (open) => {
    if (!open) return
    // Reopening always lands on the setup view: a stale report from a previous
    // session is more confusing than an empty one.
    if (phase.value === 'report') {
      notes.reset()
      phase.value = 'idle'
    }
  },
)

onBeforeUnmount(() => {
  void teardown()
})
</script>

<style scoped>
.solo {
  display: flex;
  flex-direction: column;
  gap: var(--space-5, 10px);
  min-width: 0;
  min-height: calc(160 * var(--dp));
}

.solo-hint {
  margin: 0;
  font-size: var(--font-size-sm, 12px);
  line-height: 1.6;
  color: var(--text-secondary, #666);
}

.solo-input {
  width: 100%;
  height: calc(32 * var(--dp));
  padding: 0 var(--space-5, 10px);
  border: 1px solid var(--border-color, #e5e5e5);
  border-radius: var(--radius-md, 6px);
  background: var(--bg-secondary, #f6f8fa);
  color: var(--text-primary, #1a1a1a);
  font-size: var(--font-size-sm, 12px);
}

.solo-input:focus {
  outline: none;
  border-color: var(--accent-color, #0066cc);
}

.solo-warn,
.solo-error {
  padding: var(--space-4, 8px) var(--space-5, 10px);
  border-radius: var(--radius-md, 6px);
  font-size: var(--font-size-xs, 11px);
  line-height: 1.5;
}

.solo-warn {
  background: color-mix(in srgb, var(--color-orange, #bc4c00) 10%, transparent);
  color: var(--color-orange, #bc4c00);
}

.solo-error {
  background: color-mix(in srgb, var(--color-red, #cf222e) 10%, transparent);
  color: var(--color-red, #cf222e);
}

/* Recording banner: dot + state + clock, in one row so the eye can check it at
   a glance while talking. */
.solo-rec {
  display: flex;
  align-items: center;
  gap: var(--space-3, 6px);
  padding: var(--space-4, 8px) var(--space-5, 10px);
  border-radius: var(--radius-md, 6px);
  background: color-mix(in srgb, var(--color-red, #cf222e) 8%, transparent);
  color: var(--color-red, #cf222e);
  font-size: var(--font-size-sm, 12px);
  font-weight: var(--font-weight-medium, 500);
}

.solo-rec__dot {
  width: 8px;
  height: 8px;
  border-radius: 50%;
  background: currentColor;
  animation: solo-pulse 1.4s ease-in-out infinite;
}

.solo-rec__time {
  margin-left: auto;
  font-family: var(--font-mono, ui-monospace, monospace);
  font-variant-numeric: tabular-nums;
}

@keyframes solo-pulse {
  0%,
  100% {
    opacity: 0.3;
  }
  50% {
    opacity: 1;
  }
}

.solo-lines {
  display: flex;
  flex-direction: column;
  gap: var(--space-3, 6px);
  max-height: calc(260 * var(--dp));
  overflow-y: auto;
  padding: var(--space-4, 8px) var(--space-5, 10px);
  border-radius: var(--radius-md, 6px);
  background: var(--bg-secondary, #f6f8fa);
}

.solo-line {
  display: flex;
  gap: var(--space-3, 6px);
  font-size: var(--font-size-sm, 12px);
  line-height: 1.5;
}

.solo-line__time {
  flex: 0 0 auto;
  color: var(--text-muted, #8b949e);
  font-family: var(--font-mono, ui-monospace, monospace);
  font-variant-numeric: tabular-nums;
}

.solo-line__text {
  flex: 1 1 auto;
  min-width: 0;
  color: var(--text-primary, #1a1a1a);
  word-break: break-word;
}

.solo-minutes {
  font-size: var(--font-size-sm, 12px);
  line-height: 1.7;
  color: var(--text-primary, #1a1a1a);
}

.solo-artifacts {
  display: flex;
  flex-direction: column;
  gap: var(--space-2, 4px);
}

.solo-artifacts__label {
  font-size: var(--font-size-xs, 11px);
  color: var(--text-muted, #656d76);
}

.solo-file {
  display: flex;
  align-items: center;
  gap: var(--space-3, 6px);
  min-width: 0;
  padding: var(--space-3, 6px) var(--space-5, 10px);
  border-radius: var(--radius-md, 6px);
  background: var(--bg-tertiary, #f6f8fa);
  font-size: var(--font-size-xs, 11px);
}

.solo-file__label {
  flex: 0 0 auto;
  color: var(--text-muted, #656d76);
}

.solo-file__path {
  flex: 1 1 auto;
  min-width: 0;
  font-family: var(--font-mono, ui-monospace, monospace);
  color: var(--text-primary, #1f2328);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.solo-btn {
  height: calc(24 * var(--dp));
  padding: 0 var(--space-4, 8px);
  border: 1px solid var(--border-color, #e5e5e5);
  border-radius: var(--radius-md, 6px);
  background: var(--bg-primary, #fff);
  color: var(--text-primary, #1a1a1a);
  font-size: var(--font-size-xs, 11px);
  cursor: pointer;
  flex-shrink: 0;
}

.solo-btn:hover {
  border-color: var(--accent-color, #0066cc);
  color: var(--accent-color, #0066cc);
}
</style>
