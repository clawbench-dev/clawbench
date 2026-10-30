/**
 * Reactive state for AI 听记 on the publisher side.
 *
 * Mirrors the shape of useLiveShare (module-level singleton, one session per
 * app instance) so the dialog and the header observe the same transcript even
 * when the dialog is closed and reopened mid-share.
 *
 * The transcript arrives through the upload responses — no polling while the
 * share is live. Only the minutes are polled, because they are generated
 * asynchronously on the server.
 */
import { ref } from 'vue'
import { appLog } from '@/utils/appLog'
import {
  LiveNotesCapture,
  groupNoteSegments,
  mergeNoteSegments,
  type LiveNoteSegment,
  type NoteBlock,
  type NotesUploader,
} from './liveNotes'

const TAG = 'LiveNotes'

/** How long the final upload drain may take before the minutes are generated
 *  from whatever landed. Generous on purpose: a shorter cut would drop the tail
 *  of every long meeting. */
const FINISH_UPLOAD_TIMEOUT_MS = 180_000

export type NotesPhase = 'idle' | 'recording' | 'finalizing'
export type MinutesPhase = 'idle' | 'generating' | 'ready' | 'failed'

export type { LiveNoteSegment, NoteBlock }
export { groupNoteSegments, mergeNoteSegments }

const phase = ref<NotesPhase>('idle')
const segments = ref<LiveNoteSegment[]>([])
const minutes = ref('')
const minutesPhase = ref<MinutesPhase>('idle')
const error = ref('')
/** Segments recorded but not yet transcribed (shown while finalizing). */
const pending = ref(0)
/** Segments lost after retries / dropped by the memory guard. */
const lost = ref(0)
/** True when the final drain timed out: the transcript is missing its tail. */
const truncated = ref(false)

let capture: LiveNotesCapture | null = null
let sessionToken = ''
let sessionTitle = ''
let pollTimer: ReturnType<typeof setTimeout> | null = null
let labelFor: (channel: string) => string = (channel) => channel

function uploadSegment(token: string): NotesUploader {
  return async (args) => {
    const form = new FormData()
    form.append('token', token)
    form.append('channel', args.channel)
    form.append('speaker', args.speaker)
    form.append('startedAt', String(args.startedAt))
    form.append('durationMs', String(args.durationMs))
    form.append('file', args.blob, 'segment.webm')
    const resp = await fetch('/api/live-share/notes/segment', { method: 'POST', body: form })
    if (!resp.ok) throw new Error(`segment upload failed: ${resp.status}`)
    const data = (await resp.json()) as LiveNoteSegment & { id: number }
    if (!data.id || !data.text) return null
    const seg: LiveNoteSegment = {
      id: data.id,
      channel: data.channel ?? args.channel,
      speaker: data.speaker ?? args.speaker,
      startedAt: data.startedAt ?? args.startedAt,
      durationMs: data.durationMs ?? args.durationMs,
      text: data.text,
    }
    segments.value = mergeNoteSegments(segments.value, [seg])
    return seg
  }
}

export function useLiveNotes() {
  /**
   * Begin capturing. `systemStream` / `micStream` may be null (the user turned
   * that source off); viewers are added later as they join.
   */
  function start(opts: {
    token: string
    ctx: AudioContext
    systemStream: MediaStream | null
    micStream: MediaStream | null
    title?: string
    label?: (channel: string) => string
  }): void {
    reset()
    sessionToken = opts.token
    sessionTitle = opts.title ?? ''
    labelFor = opts.label ?? ((channel) => channel)
    capture = new LiveNotesCapture(opts.ctx, uploadSegment(opts.token), {
      // Losing a segment is a permanent hole in the transcript, so the count is
      // surfaced instead of being logged and forgotten.
      onSegmentLost: () => {
        lost.value += 1
      },
      onOverflow: () => {
        lost.value += 1
      },
    })
    capture.addChannel('system', labelFor('system'), opts.systemStream)
    capture.addChannel('mic', labelFor('mic'), opts.micStream)
    phase.value = 'recording'
  }

  /** Adds one viewer's microphone as its own channel. */
  function addViewer(viewerId: string, stream: MediaStream | null): void {
    if (!capture || !viewerId) return
    capture.addChannel(`viewer:${viewerId}`, labelFor(`viewer:${viewerId}`), stream)
  }

  function removeViewer(viewerId: string): void {
    capture?.removeChannel(`viewer:${viewerId}`)
  }

  /**
   * Stops the recorders and waits for the final blobs to be produced.
   *
   * Split from finishUploads() because the caller must tear the AudioContext
   * down as soon as this resolves, while the uploads (and the minutes) keep
   * running in the background.
   */
  async function stopCapture(): Promise<void> {
    if (!capture) return
    await capture.stopAndFlush(2000)
  }

  /** Flushes queued uploads, then asks for the minutes. */
  async function finishUploads(): Promise<void> {
    const cap = capture
    if (!cap) return
    capture = null
    phase.value = 'finalizing'
    // The backlog may legitimately be minutes long (the STT backend is slower
    // than real time during a burst), so wait for it rather than cutting the
    // meeting short. The UI shows the remaining count while this runs.
    const progress = setInterval(() => {
      pending.value = cap.pending
    }, 500)
    try {
      pending.value = cap.pending
      const drained = await cap.drainUploads(FINISH_UPLOAD_TIMEOUT_MS)
      if (!drained) truncated.value = true
    } finally {
      clearInterval(progress)
      pending.value = 0
      phase.value = 'idle'
    }
    if (segments.value.length > 0) await generateMinutes()
  }

  /** Convenience for callers with nothing else to tear down. */
  async function finish(): Promise<void> {
    await stopCapture()
    await finishUploads()
  }

  /** Requests minutes generation; resolves once the server accepted the job. */
  async function generateMinutes(regenerate = false): Promise<void> {
    if (!sessionToken) return
    stopPolling()
    minutesPhase.value = 'generating'
    error.value = ''
    try {
      const resp = await fetch('/api/live-share/notes/minutes', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ token: sessionToken, title: sessionTitle, regenerate }),
      })
      if (!resp.ok) throw new Error(`minutes request failed: ${resp.status}`)
      const data = (await resp.json()) as { status?: MinutesPhase; markdown?: string; error?: string }
      if (data.status === 'ready' && data.markdown) {
        minutes.value = data.markdown
        minutesPhase.value = 'ready'
        return
      }
      if (data.status === 'idle') {
        // Nothing transcribed: there is no meeting to summarise.
        minutesPhase.value = 'idle'
        return
      }
      pollMinutes()
    } catch (e) {
      appLog.w(TAG, 'generate minutes failed', e)
      minutesPhase.value = 'failed'
      error.value = e instanceof Error ? e.message : String(e)
    }
  }

  /** Polls the server until the minutes are ready (or the share has no text). */
  function pollMinutes(attempt = 0): void {
    stopPolling()
    if (!sessionToken) return
    if (attempt > 90) {
      minutesPhase.value = 'failed'
      error.value = 'minutes generation timed out'
      return
    }
    pollTimer = setTimeout(async () => {
      pollTimer = null
      try {
        const resp = await fetch(
          `/api/live-share/notes?token=${encodeURIComponent(sessionToken)}&since=${lastSegmentID()}`,
        )
        if (!resp.ok) throw new Error(`notes poll failed: ${resp.status}`)
        const data = (await resp.json()) as {
          segments?: LiveNoteSegment[]
          minutes?: { status?: MinutesPhase; markdown?: string; error?: string }
        }
        if (data.segments?.length) {
          segments.value = mergeNoteSegments(segments.value, data.segments)
        }
        const st = data.minutes?.status ?? 'idle'
        minutesPhase.value = st
        if (st === 'ready') {
          minutes.value = data.minutes?.markdown ?? ''
          return
        }
        if (st === 'failed') {
          error.value = data.minutes?.error ?? ''
          return
        }
        if (st === 'idle') return
        pollMinutes(attempt + 1)
      } catch (e) {
        appLog.w(TAG, 'minutes poll failed', e)
        pollMinutes(attempt + 1)
      }
    }, 2000)
  }

  /** Pulls anything the local state missed (dialog reopened mid-share). */
  async function refresh(): Promise<void> {
    if (!sessionToken) return
    try {
      const resp = await fetch(
        `/api/live-share/notes?token=${encodeURIComponent(sessionToken)}&since=${lastSegmentID()}`,
      )
      if (!resp.ok) return
      const data = (await resp.json()) as {
        segments?: LiveNoteSegment[]
        minutes?: { status?: MinutesPhase; markdown?: string; error?: string }
      }
      if (data.segments?.length) {
        segments.value = mergeNoteSegments(segments.value, data.segments)
      }
      if (data.minutes?.status) {
        minutesPhase.value = data.minutes.status
        minutes.value = data.minutes.markdown ?? minutes.value
      }
    } catch (e) {
      appLog.d(TAG, 'refresh failed', e)
    }
  }

  function stopPolling(): void {
    if (pollTimer !== null) {
      clearTimeout(pollTimer)
      pollTimer = null
    }
  }

  function lastSegmentID(): number {
    // The cursor is the highest id, NOT the last element: the list is ordered
    // by speaking time, and a late transcription can carry an earlier
    // startedAt with a larger id.
    let max = 0
    for (const s of segments.value) if (s.id > max) max = s.id
    return max
  }

  function reset(): void {
    stopPolling()
    capture = null
    sessionToken = ''
    sessionTitle = ''
    phase.value = 'idle'
    segments.value = []
    minutes.value = ''
    minutesPhase.value = 'idle'
    error.value = ''
    pending.value = 0
    lost.value = 0
    truncated.value = false
  }

  return {
    phase,
    segments,
    minutes,
    minutesPhase,
    error,
    pending,
    lost,
    truncated,
    start,
    addViewer,
    removeViewer,
    stopCapture,
    finishUploads,
    finish,
    generateMinutes,
    refresh,
    reset,
  }
}

/** Test seam. */
export function _resetLiveNotesForTest(): void {
  useLiveNotes().reset()
}
