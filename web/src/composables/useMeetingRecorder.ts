/**
 * Screen recording for the running meeting.
 *
 * Records what the audience is looking at — the shared screen or a member's
 * screen — with the host's annotations composited on top, plus the meeting's
 * audio mix. One switch, start and stop at any time, several recordings per
 * meeting.
 *
 * Why composite here instead of recording the captured stream directly: the
 * publisher draws in a separate window (or a desktop overlay), and on Windows
 * and macOS that overlay is excluded from the capture. The strokes exist only
 * as normalised coordinates in the ink mirror, so the recording has to be drawn
 * onto a canvas frame by frame — exactly like the key frames do.
 *
 * The file is assembled on the server from ordered chunks (see
 * `share/meetingRecording.ts` for why the order is strict), so a long meeting
 * never has to sit in memory as one Blob and a crash keeps everything already
 * uploaded.
 */
import { ref } from 'vue'
import { appLog } from '@/utils/appLog'
import type { InkStroke } from '@/share/liveControl'
import { drawInkStrokes } from '@/share/inkCanvas'
import {
  RecordingChunkQueue,
  pickRecordingMime,
  recordingSupported,
} from '@/share/meetingRecording'

const TAG = 'MeetingRecorder'

/** Two seconds per chunk: frequent enough that a crash loses little, large
 *  enough that a one-hour meeting is not a thousand requests. */
const CHUNK_MS = 2_000

/** Frames per second of the composited canvas. The capture is already capped
 *  at 30fps by the publishing budget; going above it would only re-encode the
 *  same frames. */
const FRAME_FPS = 30

/** Video bitrate. Screen content is mostly static, and 2.5 Mbps keeps text
 *  legible without turning an hour into a gigabyte. */
const VIDEO_BPS = 2_500_000

/** Audio bitrate for the meeting mix. */
const AUDIO_BPS = 96_000

/** How long `stop()` waits for the recorder's final `dataavailable`. */
const STOP_TIMEOUT_MS = 3_000

/** How long the last chunks may take to upload before the switch flips back. */
const FLUSH_TIMEOUT_MS = 15_000

export type RecordingError = '' | 'unsupported' | 'noSource' | 'failed' | 'overflow' | 'gap'

/** What the compositor hands the recorder. Injected in tests: jsdom has no
 *  canvas media pipeline, and the timing/upload rules are what the tests are
 *  about. */
export interface RecordingSurface {
  /** Canvas capture (video only). The audio track is added by the composable. */
  video: MediaStream
  /** Draw the latest source frame plus these strokes. */
  draw(strokes: readonly InkStroke[]): void
  /** Follow a new source (the sharer changed, or the screen was switched). */
  follow(stream: MediaStream): void
  dispose(): void
}

type SurfaceFactory = (stream: MediaStream) => Promise<RecordingSurface | null>

const state = ref<'idle' | 'starting' | 'recording' | 'stopping'>('idle')
const active = ref(false)
const elapsedMs = ref(0)
/** Bytes the server has confirmed for the current recording. */
const bytes = ref(0)
const error = ref<RecordingError>('')
/** Path (relative to the meeting folder) of the last finished recording. */
const savedName = ref('')

let sessionGen = 0
let recorder: MediaRecorder | null = null
let surface: RecordingSurface | null = null
let queue: RecordingChunkQueue | null = null
let drawTimer: ReturnType<typeof setInterval> | null = null
let clockTimer: ReturnType<typeof setInterval> | null = null
let startedAt = 0
let recordingId = ''
let recordingToken = ''
let nextSeq = 0
let getSource: () => MediaStream | null = () => null
let getStrokes: () => InkStroke[] = () => []
/** The stream the compositor is currently decoding; refreshed every frame. */
let currentSource: MediaStream | null = null
/** When the source first went missing during this recording (0 = present). */
let sourceMissingSince = 0

/** How long a recording survives with nobody sharing. A frozen last frame is
 *  not a video worth keeping; a brief null (a source swap) must not stop it. */
const SOURCE_MISSING_GRACE_MS = 5_000

// ── DOM compositor ──────────────────────────────────────────────────────────

/**
 * A 1px video element decoding the source, plus a canvas at the source's size.
 *
 * Not `display:none`: some engines stop decoding frames for a video that is not
 * rendered. The intrinsic size (`videoWidth`) is independent of the CSS size,
 * so a 1px box still yields the full frame.
 */
async function domSurface(stream: MediaStream): Promise<RecordingSurface | null> {
  if (typeof document === 'undefined') return null
  const video = document.createElement('video')
  video.muted = true
  video.playsInline = true
  video.autoplay = true
  video.setAttribute('aria-hidden', 'true')
  video.style.position = 'fixed'
  video.style.left = '-1px'
  video.style.top = '-1px'
  video.style.width = '1px'
  video.style.height = '1px'
  video.style.opacity = '0'
  video.style.pointerEvents = 'none'
  document.body.appendChild(video)

  const attach = async (next: MediaStream): Promise<void> => {
    if (video.srcObject === next) return
    video.srcObject = next
    await new Promise<void>((resolve) => {
      let done = false
      const finish = (): void => {
        if (done) return
        done = true
        video.removeEventListener('loadeddata', finish)
        resolve()
      }
      video.addEventListener('loadeddata', finish)
      setTimeout(finish, 2_000)
    })
    try {
      await video.play()
    } catch {
      /* autoplay of a muted video is allowed; a rejection means no frame yet */
    }
  }
  await attach(stream)

  const canvas = document.createElement('canvas')
  const ctx = canvas.getContext('2d')
  if (!ctx || typeof canvas.captureStream !== 'function') {
    video.remove()
    return null
  }
  const capture = canvas.captureStream(FRAME_FPS)

  return {
    video: capture,
    draw(strokes) {
      const w = video.videoWidth
      const h = video.videoHeight
      if (video.readyState < 2 || !w || !h) return
      if (canvas.width !== w || canvas.height !== h) {
        // Assigning either dimension clears the canvas and resets its state.
        canvas.width = w
        canvas.height = h
      }
      ctx.drawImage(video, 0, 0, w, h)
      drawInkStrokes(ctx, strokes, w, h)
    },
    follow(next) {
      void attach(next)
    },
    dispose() {
      try {
        video.srcObject = null
        video.remove()
      } catch {
        /* already detached */
      }
      capture.getTracks().forEach((track) => track.stop())
    },
  }
}

let createSurface: SurfaceFactory = domSurface

function domRecorder(stream: MediaStream, mime: string): MediaRecorder {
  const options: MediaRecorderOptions = {
    videoBitsPerSecond: VIDEO_BPS,
    audioBitsPerSecond: AUDIO_BPS,
  }
  if (mime) options.mimeType = mime
  try {
    return new MediaRecorder(stream, options)
  } catch (e) {
    // Older engines without the requested codec: let the engine choose rather
    // than losing the whole feature.
    appLog.w(TAG, 'requested mime unsupported, using default', e)
    return new MediaRecorder(stream)
  }
}

let createRecorder: (stream: MediaStream, mime: string) => MediaRecorder = domRecorder

// ── Upload ──────────────────────────────────────────────────────────────────

function recordingUrl(seq: number): string {
  const params = new URLSearchParams({
    token: recordingToken,
    recordingId,
    seq: String(seq),
    at: String(startedAt),
  })
  return `/api/live-share/notes/recording?${params.toString()}`
}

/**
 * Send one chunk. `true` = stored; `false` = retry; throw = give up.
 *
 * A 409 (seq gap), 413 (too big) or 404 (meeting gone) cannot be fixed by
 * retrying, and pretending otherwise would only delay the error the host has
 * to see.
 */
async function sendChunk(seq: number, blob: Blob): Promise<boolean> {
  let resp: Response
  try {
    resp = await fetch(recordingUrl(seq), {
      method: 'POST',
      headers: { 'Content-Type': 'application/octet-stream' },
      body: blob,
    })
  } catch (e) {
    appLog.w(TAG, 'recording chunk upload failed', e)
    return false
  }
  if (resp.ok) {
    try {
      const data = (await resp.json()) as { fileName?: string; bytes?: number }
      if (typeof data.bytes === 'number') bytes.value = data.bytes
      if (data.fileName) savedName.value = data.fileName
    } catch {
      /* the chunk is stored; a malformed body only loses the byte counter */
    }
    return true
  }
  if (resp.status === 409) {
    error.value = 'gap'
    throw new Error('recording gap')
  }
  if (resp.status === 413) {
    error.value = 'overflow'
    throw new Error('recording too large')
  }
  if (resp.status === 404 || resp.status === 403) throw new Error('meeting gone')
  appLog.w(TAG, 'recording chunk rejected', resp.status)
  return false
}

async function finishOnServer(token: string, id: string): Promise<void> {
  try {
    const resp = await fetch('/api/live-share/notes/recording/finish', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ token, recordingId: id }),
    })
    if (!resp.ok) return
    const data = (await resp.json()) as { fileName?: string; bytes?: number }
    if (data.fileName) savedName.value = data.fileName
    if (typeof data.bytes === 'number') bytes.value = data.bytes
  } catch (e) {
    // The file is already complete on the server; this call only closes the
    // session. Failing it costs a stale map entry, not the recording.
    appLog.w(TAG, 'recording finish failed', e)
  }
}

// ── Session ─────────────────────────────────────────────────────────────────

function clearTimers(): void {
  if (drawTimer !== null) {
    clearInterval(drawTimer)
    drawTimer = null
  }
  if (clockTimer !== null) {
    clearInterval(clockTimer)
    clockTimer = null
  }
}

/** Drop everything without uploading or reporting: used after a fatal error. */
function teardown(): void {
  clearTimers()
  // Detach the queue BEFORE stopping: the recorder's final `dataavailable`
  // would otherwise enqueue into a session that no longer exists.
  queue = null
  if (recorder) {
    try {
      if (recorder.state !== 'inactive') recorder.stop()
    } catch {
      /* already stopped */
    }
    recorder = null
  }
  surface?.dispose()
  surface = null
}

/**
 * The queue could not keep up or hit a non-retryable answer: stop now.
 *
 * `error` is set by the queue's caller; this only makes sure the recorder is
 * not left running with nowhere to put its output.
 */
function onQueueFatal(reason: 'failed' | 'overflow'): void {
  appLog.e(TAG, `recording stopped: ${reason}`)
  if (reason === 'overflow' && !error.value) error.value = 'overflow'
  else if (!error.value) error.value = 'failed'
  const token = recordingToken
  const id = recordingId
  teardown()
  state.value = 'idle'
  active.value = false
  if (token && id) void finishOnServer(token, id)
}

export function useMeetingRecorder() {
  async function start(opts: {
    token: string
    getSource: () => MediaStream | null
    getStrokes: () => InkStroke[]
    getAudio: () => MediaStream | null
  }): Promise<boolean> {
    if (state.value !== 'idle') return false
    error.value = ''
    savedName.value = ''
    bytes.value = 0
    elapsedMs.value = 0
    if (!recordingSupported()) {
      error.value = 'unsupported'
      return false
    }
    const source = opts.getSource()
    if (!source || source.getVideoTracks().length === 0) {
      error.value = 'noSource'
      return false
    }
    state.value = 'starting'
    const gen = ++sessionGen
    recordingToken = opts.token
    getSource = opts.getSource
    getStrokes = opts.getStrokes
    startedAt = Date.now()
    recordingId = newRecordingId()
    nextSeq = 0

    let created: RecordingSurface | null = null
    try {
      created = await createSurface(source)
    } catch (e) {
      appLog.w(TAG, 'compositor failed', e)
    }
    if (gen !== sessionGen || state.value !== 'starting') {
      // The switch was turned off (or the meeting ended) while the compositor
      // was being built: this session belongs to nobody.
      created?.dispose()
      return false
    }
    if (!created) {
      state.value = 'idle'
      error.value = 'unsupported'
      return false
    }
    surface = created

    const tracks = [...created.video.getVideoTracks()]
    const audio = opts.getAudio()
    if (audio) tracks.push(...audio.getAudioTracks())
    const stream = new MediaStream(tracks)
    try {
      recorder = createRecorder(stream, pickRecordingMime())
    } catch (e) {
      appLog.w(TAG, 'MediaRecorder construction failed', e)
      created.dispose()
      surface = null
      state.value = 'idle'
      error.value = 'unsupported'
      return false
    }

    queue = new RecordingChunkQueue({
      send: (chunk) => sendChunk(chunk.seq, chunk.blob),
      onFatal: (reason) => {
        if (gen === sessionGen) onQueueFatal(reason)
      },
    })

    recorder.ondataavailable = (ev: BlobEvent) => {
      if (gen !== sessionGen) return
      if (!ev.data || ev.data.size === 0) return
      queue?.enqueue({ seq: nextSeq++, blob: ev.data })
    }
    recorder.onerror = (ev: Event) => {
      appLog.e(TAG, 'recorder error', (ev as unknown as { error?: unknown }).error)
      if (gen !== sessionGen) return
      error.value = 'failed'
      void stopInternal()
    }
    if (gen !== sessionGen || state.value !== 'starting') {
      try {
        if (recorder.state !== 'inactive') recorder.stop()
      } catch {
        /* already stopped */
      }
      recorder = null
      created.dispose()
      surface = null
      queue = null
      return false
    }
    recorder.start(CHUNK_MS)

    // The picture can change mid-recording (switch source, member takes over),
    // so every frame asks which stream is on the wire instead of caching one.
    drawTimer = setInterval(() => {
      if (gen !== sessionGen) return
      const next = getSource()
      if (!next) {
        if (sourceMissingSince === 0) sourceMissingSince = Date.now()
        if (Date.now() - sourceMissingSince >= SOURCE_MISSING_GRACE_MS) {
          error.value = 'noSource'
          void stopInternal()
          return
        }
      } else {
        sourceMissingSince = 0
        if (next !== currentSource) {
          currentSource = next
          surface?.follow(next)
        }
      }
      surface?.draw(getStrokes())
    }, Math.round(1000 / FRAME_FPS))

    clockTimer = setInterval(() => {
      if (gen !== sessionGen) return
      elapsedMs.value = Date.now() - startedAt
    }, 1_000)

    currentSource = source
    sourceMissingSince = 0
    state.value = 'recording'
    active.value = true
    return true
  }

  async function stopInternal(): Promise<void> {
    if (state.value !== 'recording' && state.value !== 'starting') return
    state.value = 'stopping'
    const gen = sessionGen
    const token = recordingToken
    const id = recordingId
    clearTimers()
    elapsedMs.value = startedAt > 0 ? Date.now() - startedAt : elapsedMs.value

    const rec = recorder
    if (rec && rec.state !== 'inactive') {
      await new Promise<void>((resolve) => {
        let done = false
        const timer = setTimeout(finish, STOP_TIMEOUT_MS)
        function finish(): void {
          if (done) return
          done = true
          clearTimeout(timer)
          resolve()
        }
        const prev = rec.onstop
        rec.onstop = (ev) => {
          prev?.call(rec, ev)
          finish()
        }
        try {
          rec.stop()
        } catch {
          finish()
        }
      })
    }
    if (gen === sessionGen) {
      surface?.dispose()
      surface = null
      recorder = null
      currentSource = null
    }

    const q = queue
    queue = null
    if (q) {
      const timeout: { resolve: (() => void) | null; timer: ReturnType<typeof setTimeout> | null } = {
        resolve: null,
        timer: null,
      }
      await Promise.race([
        q.finish(),
        new Promise<void>((resolve) => {
          timeout.resolve = resolve
          timeout.timer = setTimeout(resolve, FLUSH_TIMEOUT_MS)
        }),
      ])
      if (timeout.timer !== null) clearTimeout(timeout.timer)
      // Resolve the loser too: an abandoned pending promise is a test leak and,
      // in a long-lived page, a retained closure for every recording.
      timeout.resolve?.()
    }
    if (gen === sessionGen && token && id) await finishOnServer(token, id)
    if (gen === sessionGen) {
      state.value = 'idle'
      active.value = false
      recordingId = ''
    }
  }

  let stopPromise: Promise<void> | null = null

  /**
   * Stop and finalise.
   *
   * Concurrent calls share one finalisation: a second click must not report
   * "saved" while the first is still flushing the last chunks.
   */
  async function stop(): Promise<void> {
    if (stopPromise) return stopPromise
    stopPromise = stopInternal().finally(() => {
      stopPromise = null
    })
    return stopPromise
  }

  /** Abandon the session without uploading anything more (meeting ended). */
  function cancel(): void {
    sessionGen++
    teardown()
    state.value = 'idle'
    active.value = false
    currentSource = null
    sourceMissingSince = 0
    recordingId = ''
  }

  return {
    state,
    active,
    elapsedMs,
    bytes,
    error,
    savedName,
    start,
    stop,
    cancel,
  }
}

/** A short, file-name-safe id: the server validates the same alphabet. */
function newRecordingId(): string {
  const bytes = new Uint8Array(8)
  if (typeof crypto !== 'undefined' && crypto.getRandomValues) {
    crypto.getRandomValues(bytes)
  } else {
    for (let i = 0; i < bytes.length; i++) bytes[i] = Math.floor(Math.random() * 256)
  }
  return Array.from(bytes, (b) => b.toString(16).padStart(2, '0')).join('')
}

/** @internal Test seam: replace the DOM compositor. */
export function _setRecordingSurfaceForTest(fn: SurfaceFactory | null): void {
  createSurface = fn ?? domSurface
}

/** @internal Test seam: replace the MediaRecorder constructor. */
export function _setRecordingRecorderForTest(
  fn: ((stream: MediaStream, mime: string) => MediaRecorder) | null,
): void {
  createRecorder = fn ?? domRecorder
}

/** @internal Test seam: drop all state without touching the network. */
export function _resetMeetingRecorderForTest(): void {
  sessionGen++
  teardown()
  state.value = 'idle'
  elapsedMs.value = 0
  bytes.value = 0
  error.value = ''
  savedName.value = ''
}
