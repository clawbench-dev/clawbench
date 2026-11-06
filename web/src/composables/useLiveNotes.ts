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
import { computed, ref } from 'vue'
import { appLog } from '@/utils/appLog'
import {
  LiveNotesCapture,
  groupNoteSegments,
  mergeNoteSegments,
  notesRecordingSupported,
  withMeetingFileEvents,
  withMeetingShots,
  type LiveNoteSegment,
  type NoteBlock,
  type NoteFileEvent,
  type NotesUploadArgs,
  type NotesUploader,
  type NoteTimelineRow,
  type NoteShotEvent,
  type VoiceSensitivity,
} from './liveNotes'

const TAG = 'LiveNotes'

/** How long the final upload drain may take before the minutes are generated
 *  from whatever landed. Generous on purpose: a shorter cut would drop the tail
 *  of every long meeting. */
const FINISH_UPLOAD_TIMEOUT_MS = 180_000

/**
 * A channel that has carried no speech for this long is called out.
 *
 * Two minutes is long enough that a normal pause, a question being typed or a
 * screen-sharing demo does not raise a false alarm, and short enough that a
 * dead microphone is noticed while the meeting still matters.
 */
export const NOTES_SILENCE_ALERT_MS = 120_000

/**
 * Backlog (in segments) at which the panel says transcription is behind.
 *
 * Segments are 4-20s, so three of them is roughly a minute of audio waiting —
 * the point where "why is nothing appearing" starts being a fair question.
 */
export const NOTES_BACKLOG_ALERT = 3

/** How often the live status is recomputed. */
const HEALTH_TICK_MS = 1_000

export type NotesPhase = 'idle' | 'recording' | 'finalizing'
export type MinutesPhase = 'idle' | 'generating' | 'ready' | 'failed'

/**
 * One answer to "what is happening right now", in priority order.
 *
 * The order is not cosmetic: a dead microphone also produces no speech and no
 * pending work, so reporting "silent" would be technically true and completely
 * useless. Every cause that explains the others comes first.
 */
export type NotesHealthKind =
  | 'ok'
  | 'queued'
  | 'silent'
  | 'micDead'
  | 'suspended'
  | 'reconnecting'
  | 'offline'

/** Files a meeting leaves in the project. Empty until the server reports them. */
export interface NotesArtifacts {
  dir?: string
  minutes?: string
  transcript?: string
  revised?: string
  audio?: string
  /** The merged whole-meeting recording (`audio/meeting.webm`). */
  audioMeeting?: string
  audioFiles?: number
}

/** One stage report from the server: minutes or the proofreading pass. */
interface NotesStagePayload {
  status?: MinutesPhase
  markdown?: string
  error?: string
  filePath?: string
  revise?: { status?: MinutesPhase; error?: string }
  artifacts?: NotesArtifacts
}

export type { LiveNoteSegment, NoteBlock }
export { groupNoteSegments, mergeNoteSegments, withMeetingFileEvents, withMeetingShots }
export type { NoteFileEvent, NoteTimelineRow, NoteShotEvent }

const phase = ref<NotesPhase>('idle')
const segments = ref<LiveNoteSegment[]>([])
const minutes = ref('')
/** Path of the Markdown copy written inside the project (empty until saved). */
const minutesFile = ref('')
const minutesPhase = ref<MinutesPhase>('idle')
/**
 * The proofreading stage that runs before the summary. Tracked separately
 * because it fails independently: a proofread failure still leaves usable
 * minutes, and the dialog has to say which one went wrong.
 */
const revisePhase = ref<MinutesPhase>('idle')
const reviseError = ref('')
/** Where the recording, both transcripts and the minutes were written. */
const artifacts = ref<NotesArtifacts>({})
const error = ref('')
/** Segments recorded but not yet transcribed (shown while finalizing). */
const pending = ref(0)
/** Segments lost after retries / dropped by the memory guard. */
const lost = ref(0)
/** True when the final drain timed out: the transcript is missing its tail. */
const truncated = ref(false)

// ── Live status ──────────────────────────────────────────────────────────────
//
// Everything below answers "is transcription actually working", which is a
// different question from "did it ever produce a line". Before this existed the
// panel showed the same empty card for "nobody has spoken yet" and "the
// microphone died twenty minutes ago".

/** The publisher's socket, as far as the panel cares. */
const connection = ref<'online' | 'reconnecting' | 'offline'>('online')
/** The audio graph was suspended by the system (the recorder hears nothing). */
const audioSuspended = ref(false)
/** Channels whose audio source disappeared. */
const deadChannels = ref<string[]>([])
/** Epoch ms of the last speech heard on any channel (0 = never). */
const lastVoiceAt = ref(0)
/** Epoch ms the current recording started (so "silent since" has a floor). */
const recordingSince = ref(0)
/** Recomputed once a second so "silent for N minutes" is live, not a snapshot. */
const healthClock = ref(Date.now())
let healthTimer: ReturnType<typeof setInterval> | null = null

let capture: LiveNotesCapture | null = null
/**
 * The stream each member's microphone arrives on, kept OUTSIDE the capture.
 *
 * 听记 can be switched off and on again mid-meeting, and a new capture starts
 * with no channels: only a NEW join calls `addViewer`, so every member already
 * in the room would stay silent in the transcript for the rest of the meeting.
 * Remembering the streams is what lets `start()` re-attach them.
 */
const viewerStreams = new Map<string, MediaStream | null>()
/**
 * Bumped by start() and reset(). Every async continuation captures it and
 * bails out when it no longer matches, because the module-level state is
 * shared: without this, a drain that finishes after the user closed the dialog
 * and started ANOTHER meeting would write the finished meeting's phase,
 * segments and minutes into the new one (captions freeze at "idle", and the new
 * meeting gets a minutes request fired mid-recording).
 */
let sessionGen = 0
let sessionToken = ''
let sessionTitle = ''
let pollTimer: ReturnType<typeof setTimeout> | null = null
/** The upload drain started by a pause, so a later finish can wait for it. */
let draining: Promise<void> | null = null
let labelFor: (channel: string) => string = (channel) => channel
/**
 * Whether the end of the meeting should request minutes automatically.
 *
 * Server setting (meeting.auto_minutes), passed in at start because the
 * trigger is client-side. The audio is merged regardless: turning this off must
 * not cost the user the recording needed by "重新识别转写".
 */
let autoMinutesEnabled = true

/**
 * Builds the multipart body every segment upload shares.
 *
 * The title travels with every segment: the server creates the meeting folder
 * on the FIRST one (audio is written as it arrives), and it needs a name before
 * the minutes request ever happens.
 */
function segmentForm(token: string, args: NotesUploadArgs, archive: boolean): FormData {
  const form = new FormData()
  form.append('token', token)
  if (sessionTitle) form.append('title', sessionTitle)
  form.append('channel', args.channel)
  form.append('speaker', args.speaker)
  form.append('startedAt', String(args.startedAt))
  form.append('durationMs', String(args.durationMs))
  if (archive) form.append('archive', '1')
  form.append('file', args.blob, 'segment.webm')
  return form
}

function uploadSegment(token: string): NotesUploader {
  return async (args) => {
    const resp = await fetch('/api/live-share/notes/segment', {
      method: 'POST',
      body: segmentForm(token, args, false),
    })
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

/**
 * Uploads one chunk of the archive-only mix track.
 *
 * Archive, not transcription: the mix has no single speaker, so the server
 * stores it without calling the STT provider. It never yields a transcript line.
 */
function uploadArchiveSegment(token: string): NotesUploader {
  return async (args) => {
    const resp = await fetch('/api/live-share/notes/segment', {
      method: 'POST',
      body: segmentForm(token, args, true),
    })
    if (!resp.ok) throw new Error(`archive upload failed: ${resp.status}`)
    return null
  }
}

export function useLiveNotes() {
  /**
   * The one answer to "is transcription working right now".
   *
   * Kept here rather than in the panel because three producers feed it (the
   * recorder, the audio graph, the publisher socket) and the priority between
   * them is the whole design: a dead microphone also means "no speech" and "no
   * backlog", so the specific causes must win over the general ones.
   */
  const health = computed<NotesHealthKind>(() => {
    if (phase.value !== 'recording') return 'ok'
    if (deadChannels.value.length > 0) return 'micDead'
    if (audioSuspended.value) return 'suspended'
    if (connection.value === 'offline') return 'offline'
    if (connection.value === 'reconnecting') return 'reconnecting'
    if (pending.value >= NOTES_BACKLOG_ALERT) return 'queued'
    const since = Math.max(lastVoiceAt.value, recordingSince.value)
    if (since > 0 && healthClock.value - since >= NOTES_SILENCE_ALERT_MS) return 'silent'
    return 'ok'
  })

  /** How long the meeting has been without speech (0 when unknown). */
  const silentForMs = computed(() => {
    const since = Math.max(lastVoiceAt.value, recordingSince.value)
    return since > 0 ? Math.max(0, healthClock.value - since) : 0
  })

  function startHealthTimer(): void {
    stopHealthTimer()
    healthClock.value = Date.now()
    healthTimer = setInterval(() => {
      healthClock.value = Date.now()
      const cap = capture
      if (!cap) return
      pending.value = cap.pending
      deadChannels.value = cap.deadChannels()
      lastVoiceAt.value = cap.lastVoiceAt
    }, HEALTH_TICK_MS)
  }

  function stopHealthTimer(): void {
    if (healthTimer !== null) {
      clearInterval(healthTimer)
      healthTimer = null
    }
  }

  /** Reported by the share's socket: it is what carries every upload. */
  function setConnectionState(next: 'online' | 'reconnecting' | 'offline'): void {
    connection.value = next
  }

  /** Reported by the audio graph: a suspended context records nothing. */
  function setAudioSuspended(next: boolean): void {
    audioSuspended.value = next
  }

  /**
   * Begin capturing. Only microphones are transcribed: the system-audio loop
   * would capture whatever else the machine is playing, which is not what a
   * meeting transcript is for, and it duplicated every remote voice into a
   * second channel. Viewers are added later as they join.
   *
   * `resume` restarts the recorder of the SAME meeting (听记 was switched off for
   * a moment). It keeps the segments, the minutes and the member channels — a
   * plain start would wipe the transcript and make the meeting's minutes cover
   * only the part recorded after the toggle. A different token always starts
   * clean: the module is a singleton and would otherwise show the previous
   * meeting's transcript.
   */
  function start(opts: {
    token: string
    ctx: AudioContext
    micStream: MediaStream | null
    title?: string
    label?: (channel: string) => string
    resume?: boolean
    autoMinutes?: boolean
    /** Shared voice-gate switch (`stt.voice_gate`); defaults to on. */
    voiceGate?: boolean
    /** Shared sensitivity (`stt.voice_gate_sensitivity`); defaults to standard. */
    sensitivity?: VoiceSensitivity
  }): void {
    // No recorder on this engine: leave the module idle and say so once. The
    // meeting continues (picture / microphone / chat), it just has no 听记.
    if (!notesRecordingSupported()) {
      appLog.w(TAG, 'MediaRecorder unavailable: notes stay off for this meeting')
      return
    }
    if (opts.resume === true && sessionToken === opts.token) {
      // Same meeting: keep everything, but invalidate the previous capture's
      // continuations — its drain must not report "finalizing" over this one.
      sessionGen += 1
      stopPolling()
    } else {
      reset()
    }
    sessionToken = opts.token
    autoMinutesEnabled = opts.autoMinutes !== false
    if (opts.title !== undefined) sessionTitle = opts.title
    if (opts.label) labelFor = opts.label
    capture = new LiveNotesCapture(opts.ctx, uploadSegment(opts.token), {
      voiceGate: opts.voiceGate,
      sensitivity: opts.sensitivity,
      // The mix track is the file the user actually listens to: one recording
      // of the whole meeting instead of one per participant.
      archiveUpload: uploadArchiveSegment(opts.token),
      // Losing a segment is a permanent hole in the transcript, so the count is
      // surfaced instead of being logged and forgotten.
      onSegmentLost: () => {
        lost.value += 1
      },
      onOverflow: () => {
        lost.value += 1
      },
    })
    capture.addChannel('mic', () => labelFor('mic'), opts.micStream)
    // Re-attach every member already in the room (a resumed recorder would
    // otherwise only hear the host and whoever joins next).
    for (const [viewerId, stream] of viewerStreams) {
      capture.addChannel(`viewer:${viewerId}`, () => labelFor(`viewer:${viewerId}`), stream)
    }
    phase.value = 'recording'
    // A fresh recorder has heard nothing yet: "silent since" starts now, not at
    // whatever the previous session last heard.
    recordingSince.value = Date.now()
    lastVoiceAt.value = 0
    deadChannels.value = []
    startHealthTimer()
  }

  /**
   * Adds one viewer's microphone as its own channel.
   *
   * The stream is remembered even while no recorder runs: 听记 may be switched
   * on later, and `start()` re-attaches everything it knows about.
   */
  function addViewer(viewerId: string, stream: MediaStream | null): void {
    if (!viewerId) return
    viewerStreams.set(viewerId, stream)
    if (!capture) return
    capture.addChannel(`viewer:${viewerId}`, () => labelFor(`viewer:${viewerId}`), stream)
  }

  /**
   * Point the publisher's channel at a different input device.
   *
   * The host can switch microphones mid-meeting (headset put on). `addChannel`
   * replaces an existing channel, so the transcript keeps one "mic" stream of
   * segments instead of a second one — the segment in flight is flushed under
   * the same speaker label.
   */
  function setMicStream(stream: MediaStream | null): void {
    if (!capture) return
    capture.addChannel('mic', () => labelFor('mic'), stream)
  }

  function removeViewer(viewerId: string): void {
    viewerStreams.delete(viewerId)
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
    stopHealthTimer()
    // The recorder is gone: a dead channel or a suspended graph is no longer a
    // statement about anything, and leaving them set would keep the status bar
    // red between here and the finalize that hides it.
    deadChannels.value = []
    audioSuspended.value = false
    if (!capture) return
    await capture.stopAndFlush(2000)
  }

  /**
   * Flushes queued uploads, then asks for the minutes.
   *
   * `merge` is false when the recording is only being PAUSED: turning 听记 off
   * mid-meeting drains the queue, but folding the audio now would leave the
   * rest of the meeting (recorded after the toggle) as loose segments beside a
   * merged file that no longer covers it. The merge AND the minutes wait for
   * the meeting to end, when nothing more is coming — generating them here
   * summarized only the first half and nothing ever regenerated them.
   *
   * With no live capture there is nothing to drain: either 听记 was never on, or
   * it was switched off earlier in the meeting. `merge` (i.e. the meeting is
   * really over) still folds the recording and summarizes every segment.
   */
  async function finishUploads(merge = true): Promise<void> {
    const cap = capture
    if (!cap) {
      if (!merge) return
      // A pause may still be draining its queue (the panel does not await the
      // toggle). Summarizing now would present a transcript that is missing its
      // tail as the meeting's end.
      if (draining) await draining.catch(() => undefined)
      await finishPaused()
      return
    }
    const gen = sessionGen
    capture = null
    phase.value = 'finalizing'
    // The backlog may legitimately be minutes long (the STT backend is slower
    // than real time during a burst), so wait for it rather than cutting the
    // meeting short. The UI shows the remaining count while this runs.
    const progress = setInterval(() => {
      if (gen === sessionGen) pending.value = cap.pending
    }, 500)
    const run = (async () => {
      try {
        pending.value = cap.pending
        const drained = await cap.drainUploads(FINISH_UPLOAD_TIMEOUT_MS)
        if (gen !== sessionGen) return
        if (!drained) truncated.value = true
      } finally {
        clearInterval(progress)
        if (gen === sessionGen) {
          pending.value = 0
          phase.value = 'idle'
        }
      }
    })()
    draining = run
    try {
      await run
    } finally {
      if (draining === run) draining = null
    }
    // The dialog may have been closed and another meeting started while the
    // backlog drained; its state is not ours to touch.
    if (gen !== sessionGen) return
    // Merge BEFORE the minutes: only now is "no more audio is coming" true, and
    // the minutes link to the recording.
    if (merge) await finishPaused()
  }

  /**
   * Fold the recording and summarize the whole meeting.
   *
   * The tail of `finishUploads`, split out because it is also what a meeting
   * whose recorder was paused earlier needs: there is no queue to drain, but
   * the segments it did produce are the transcript.
   */
  async function finishPaused(): Promise<void> {
    const gen = sessionGen
    await mergeAudio()
    if (gen !== sessionGen) return
    if (autoMinutesEnabled && segments.value.length > 0) await generateMinutes()
  }

  /**
   * Folds the meeting's audio segments into one file per channel plus the
   * whole-meeting mix.
   *
   * Failure is logged, not surfaced: the segments stay on disk and the minutes
   * are still worth generating.
   */
  async function mergeAudio(): Promise<void> {
    if (!sessionToken) return
    try {
      const resp = await fetch('/api/live-share/notes/audio/merge', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ token: sessionToken }),
      })
      if (!resp.ok) throw new Error(`audio merge failed: ${resp.status}`)
      const data = (await resp.json()) as { artifacts?: NotesArtifacts }
      if (data.artifacts) artifacts.value = data.artifacts
    } catch (e) {
      appLog.w(TAG, 'merge meeting audio failed', e)
    }
  }

  /** Convenience for callers with nothing else to tear down. */
  async function finish(): Promise<void> {
    await stopCapture()
    await finishUploads()
  }

  /** Requests minutes generation; resolves once the server accepted the job. */
  async function generateMinutes(regenerate = false): Promise<void> {
    if (!sessionToken) return
    const gen = sessionGen
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
      const data = (await resp.json()) as NotesStagePayload
      if (gen !== sessionGen) return
      applyStage(data)
      if (data.status === 'ready' && data.markdown) return
      if (data.status === 'idle') return
      pollMinutes()
    } catch (e) {
      appLog.w(TAG, 'generate minutes failed', e)
      if (gen !== sessionGen) return
      minutesPhase.value = 'failed'
      error.value = e instanceof Error ? e.message : String(e)
    }
  }

  /** Polls the server until the minutes are ready (or the share has no text). */
  function pollMinutes(attempt = 0): void {
    stopPolling()
    if (!sessionToken) return
    const gen = sessionGen
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
          minutes?: NotesStagePayload
        }
        if (gen !== sessionGen) return
        if (data.segments?.length) {
          segments.value = mergeNoteSegments(segments.value, data.segments)
        }
        applyStage(data.minutes ?? {})
        if (minutesPhase.value === 'ready') return
        if (minutesPhase.value === 'failed') {
          error.value = data.minutes?.error ?? ''
          return
        }
        if (minutesPhase.value === 'idle') return
        pollMinutes(attempt + 1)
      } catch (e) {
        appLog.w(TAG, 'minutes poll failed', e)
        if (gen !== sessionGen) return
        pollMinutes(attempt + 1)
      }
    }, 2000)
  }

  /**
   * Applies one server payload.
   *
   * Three call sites (initial POST, poll, refresh) used to parse the same shape
   * by hand; they now share this so a new field cannot be wired into one and
   * forgotten in the others.
   */
  function applyStage(data: NotesStagePayload): void {
    const st = data.status ?? 'idle'
    minutesPhase.value = st
    if (data.markdown !== undefined) minutes.value = data.markdown
    if (data.filePath) minutesFile.value = data.filePath
    if (data.artifacts) artifacts.value = data.artifacts
    if (data.revise?.status) revisePhase.value = data.revise.status
    reviseError.value = data.revise?.error ?? ''
  }

  /** Pulls anything the local state missed (dialog reopened mid-share). */
  async function refresh(): Promise<void> {
    if (!sessionToken) return
    const gen = sessionGen
    try {
      const resp = await fetch(
        `/api/live-share/notes?token=${encodeURIComponent(sessionToken)}&since=${lastSegmentID()}`,
      )
      if (!resp.ok) return
      const data = (await resp.json()) as {
        segments?: LiveNoteSegment[]
        minutes?: NotesStagePayload
      }
      if (gen !== sessionGen) return
      if (data.segments?.length) {
        segments.value = mergeNoteSegments(segments.value, data.segments)
      }
      if (data.minutes?.status) applyStage(data.minutes)
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
    // Invalidate every in-flight continuation before clearing the state they
    // would otherwise write into.
    sessionGen += 1
    stopPolling()
    stopHealthTimer()
    capture = null
    draining = null
    viewerStreams.clear()
    labelFor = (channel) => channel
    autoMinutesEnabled = true
    sessionToken = ''
    sessionTitle = ''
    phase.value = 'idle'
    segments.value = []
    minutes.value = ''
    minutesFile.value = ''
    minutesPhase.value = 'idle'
    revisePhase.value = 'idle'
    reviseError.value = ''
    artifacts.value = {}
    error.value = ''
    pending.value = 0
    lost.value = 0
    truncated.value = false
    connection.value = 'online'
    audioSuspended.value = false
    deadChannels.value = []
    lastVoiceAt.value = 0
    recordingSince.value = 0
  }

  return {
    phase,
    segments,
    minutes,
    minutesFile,
    minutesPhase,
    revisePhase,
    reviseError,
    artifacts,
    error,
    pending,
    lost,
    truncated,
    health,
    silentForMs,
    deadChannels,
    connection,
    audioSuspended,
    setConnectionState,
    setAudioSuspended,
    start,
    addViewer,
    removeViewer,
    setMicStream,
    stopCapture,
    finishUploads,
    finish,
    generateMinutes,
    mergeAudio,
    refresh,
    reset,
  }
}

/** Test seam. */
export function _resetLiveNotesForTest(): void {
  useLiveNotes().reset()
}
