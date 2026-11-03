/**
 * Reactive wrapper around the key-frame pipeline for the running meeting.
 *
 * Mirrors the shape of `useLiveNotes`: module-level singleton, one session per
 * app instance, and a generation counter so a slow frame that resolves after
 * the meeting ended cannot upload into the next one.
 *
 * The DOM part (a detached <video> plus two canvases) lives here because it is
 * the only piece that needs a browser; every decision about what to keep is in
 * `share/shotCapture.ts`.
 */
import { ref, watch, type WatchStopHandle } from 'vue'
import { appLog } from '@/utils/appLog'
import { isMeetingFileTracking, meetingFileEntries, onMeetingFileOpened } from '@/utils/meetingFileTracker'
import type { InkStroke } from '@/share/liveControl'
import {
  ShotQueue,
  ShotScheduler,
  dhash,
  newClientShotId,
  type ShotJob,
  type ShotReason,
  type ShotSendResult,
} from '@/share/shotCapture'
import { useLiveNotes } from './useLiveNotes'

const TAG = 'MeetingShots'

/** Wait for the preview to render before grabbing: 700ms catches the click,
 *  not the document. `closeCurrent` drops visits under a second, so a shot at
 *  1.2s is the earliest one that can still be on the timeline. */
const FILE_TRIGGER_DELAY_MS = 1200

/** A source change needs a moment before the first frame of the new track is
 *  decoded; grabbing immediately stores the previous picture. */
const SOURCE_TRIGGER_DELAY_MS = 500

/** Safety net for a meeting where nothing was said or opened for a while. */
const PERIODIC_MS = 60_000

/** How long the end-of-meeting flush may take. Frames are small; the meeting
 *  UI must not wait for them. */
const FLUSH_TIMEOUT_MS = 10_000

/** Utterance triggers arrive after the STT round trip; never wait longer than
 *  this for the "end of the sentence" moment. */
const SPEECH_MAX_DELAY_MS = 3_000

/** Lower-case fragments that usually precede "look at this". Deliberately
 *  small and explainable: a miss only costs one frame, and the file/switch
 *  triggers are the main path anyway. */
const SPEECH_CUES = [
  '看这里',
  '看这个',
  '看下',
  '这张图',
  '这个页面',
  '问题是',
  '结论',
  '决定',
  '下一步',
  '重点',
  '大家看',
  '注意一下',
  'look here',
  'as you can see',
  'the problem is',
  'the point is',
  'next step',
]

/** Kept frames (successfully uploaded). */
const count = ref(0)
/** Frames that could not be stored; shown so the loss is not silent. */
const lost = ref(0)
const active = ref(false)
const paused = ref(false)

let sessionGen = 0
let getSource: () => MediaStream | null = () => null
let getStrokes: () => InkStroke[] = () => []
let scheduler: ShotScheduler | null = null
let queue: ShotQueue | null = null
let unsubFile: (() => void) | null = null
let unwatchSegments: WatchStopHandle | null = null
let periodic: ReturnType<typeof setInterval> | null = null
let video: HTMLVideoElement | null = null
/** Reused across captures: a 1920×1200 canvas is ~9 MB, and a meeting grabs
 *  dozens of frames. Reallocating one per capture is pure GC churn. */
let fullCanvas: HTMLCanvasElement | null = null
let fullCtx: CanvasRenderingContext2D | null = null
let sampleCanvas: HTMLCanvasElement | null = null
let sampleCtx: CanvasRenderingContext2D | null = null
/** One capture at a time: the canvases above are shared, so two overlapping
 *  grabs would encode each other's picture. */
let capturing = false
const seenSegments = new Set<number>()

/**
 * A grabbed picture before encoding.
 *
 * Encoding is the expensive half (~30–60 ms of CPU), and most triggers are
 * dropped as "same picture". Returning a lazy `encode` is what lets the
 * scheduler reject a frame without paying for it.
 */
export interface ShotSample {
  hash: string
  width: number
  height: number
  encode: () => Promise<Blob | null>
}

/** Injected in tests: a canvas is not available there, and the pipeline's
 *  behaviour is what the tests are about. */
type Grabber = (stream: MediaStream, strokes: InkStroke[]) => Promise<ShotSample | null>
let grabber: Grabber = domSample

// ── Frame grabbing ──────────────────────────────────────────────────────────

/**
 * A 1px, invisible video element attached to the body.
 *
 * Not `display:none`: some engines stop decoding frames for a video that is not
 * rendered. The intrinsic size (`videoWidth`) is independent of the CSS size,
 * so a 1px box still yields the full frame.
 */
function ensureVideo(): HTMLVideoElement {
  if (video) return video
  const el = document.createElement('video')
  el.muted = true
  el.playsInline = true
  el.autoplay = true
  el.setAttribute('aria-hidden', 'true')
  el.style.position = 'fixed'
  el.style.left = '-1px'
  el.style.top = '-1px'
  el.style.width = '1px'
  el.style.height = '1px'
  el.style.opacity = '0'
  el.style.pointerEvents = 'none'
  document.body.appendChild(el)
  video = el
  return el
}

function releaseVideo(): void {
  if (!video) return
  try {
    video.srcObject = null
    video.remove()
  } catch {
    /* already detached */
  }
  video = null
}

/** Point the element at `stream`, waiting for metadata when it changed. */
async function attach(el: HTMLVideoElement, stream: MediaStream): Promise<void> {
  if (el.srcObject === stream) return
  el.srcObject = stream
  await new Promise<void>((resolve) => {
    let done = false
    const finish = (): void => {
      if (done) return
      done = true
      el.removeEventListener('loadeddata', finish)
      resolve()
    }
    el.addEventListener('loadeddata', finish)
    setTimeout(finish, 2_000)
  })
  try {
    await el.play()
  } catch {
    /* autoplay of a muted video is allowed; a rejection means no frame yet */
  }
}

function drawStrokes(ctx: CanvasRenderingContext2D, strokes: InkStroke[], w: number, h: number): void {
  // Same normalisation and width scaling as LiveInkLayer: the strokes are
  // relative to the picture, so the same numbers land in the same place.
  const scale = Math.max(1, w / 800)
  for (const stroke of strokes) {
    if (stroke.points.length === 0) continue
    ctx.beginPath()
    ctx.lineCap = 'round'
    ctx.lineJoin = 'round'
    ctx.strokeStyle = stroke.color
    ctx.lineWidth = Math.max(1, stroke.width * scale)
    const [first, ...rest] = stroke.points
    ctx.moveTo(first.x * w, first.y * h)
    if (rest.length === 0) {
      ctx.lineTo(first.x * w + 0.01, first.y * h)
    } else {
      for (const p of rest) ctx.lineTo(p.x * w, p.y * h)
    }
    ctx.stroke()
  }
}

function hashOf(source: CanvasImageSource): string {
  if (!sampleCanvas) {
    sampleCanvas = document.createElement('canvas')
    sampleCanvas.width = 9
    sampleCanvas.height = 8
    sampleCtx = sampleCanvas.getContext('2d', { willReadFrequently: true })
  }
  if (!sampleCtx) return ''
  sampleCtx.clearRect(0, 0, 9, 8)
  sampleCtx.drawImage(source, 0, 0, 9, 8)
  let data: Uint8ClampedArray
  try {
    data = sampleCtx.getImageData(0, 0, 9, 8).data
  } catch {
    return ''
  }
  const gray = new Array<number>(72)
  for (let i = 0; i < 72; i++) {
    const o = i * 4
    gray[i] = 0.299 * data[o] + 0.587 * data[o + 1] + 0.114 * data[o + 2]
  }
  return dhash(gray, 9, 8)
}

function encode(canvas: HTMLCanvasElement): Promise<Blob | null> {
  return new Promise((resolve) => {
    canvas.toBlob(
      (webp) => {
        if (webp) {
          resolve(webp)
          return
        }
        // Firefox and Safari fall back to PNG for an unsupported type.
        canvas.toBlob((png) => resolve(png), 'image/png')
      },
      'image/webp',
      0.82,
    )
  })
}

/**
 * Grab the current picture and its hash, without encoding.
 *
 * The full-size canvas is reused and only resized when the source dimensions
 * change; the 9×8 one is a fixed scratch surface. The returned `encode`
 * closure is safe to call because `captureNow` runs one capture at a time.
 */
async function domSample(stream: MediaStream, strokes: InkStroke[]): Promise<ShotSample | null> {
  const el = ensureVideo()
  await attach(el, stream)
  if (el.readyState < 2 || !el.videoWidth || !el.videoHeight) {
    await new Promise((resolve) => setTimeout(resolve, 200))
  }
  const w = el.videoWidth
  const h = el.videoHeight
  if (!w || !h) return null
  if (!fullCanvas) {
    fullCanvas = document.createElement('canvas')
    fullCtx = fullCanvas.getContext('2d')
  }
  if (!fullCtx) return null
  if (fullCanvas.width !== w || fullCanvas.height !== h) {
    // Assigning either dimension clears the canvas and resets its state, which
    // is fine here: the frame is drawn immediately afterwards.
    fullCanvas.width = w
    fullCanvas.height = h
  }
  fullCtx.drawImage(el, 0, 0, w, h)
  drawStrokes(fullCtx, strokes, w, h)
  const hash = hashOf(fullCanvas)
  const canvas = fullCanvas
  return { hash, width: w, height: h, encode: () => encode(canvas) }
}

// ── Upload ──────────────────────────────────────────────────────────────────

async function sendShot(tok: string, job: ShotJob): Promise<ShotSendResult> {
  const form = new FormData()
  form.append('token', tok)
  form.append('clientShotId', job.clientShotId)
  form.append('at', String(job.at))
  form.append('reason', job.reason)
  if (job.ref) form.append('ref', job.ref)
  form.append('width', String(job.width))
  form.append('height', String(job.height))
  form.append('file', job.blob, 'shot.webp')
  try {
    const resp = await fetch('/api/live-share/notes/shot', { method: 'POST', body: form })
    return resp.ok ? { ok: true } : { ok: false, status: resp.status }
  } catch (e) {
    appLog.d(TAG, 'shot upload failed', e)
    return { ok: false, status: 0 }
  }
}

// ── Pipeline ────────────────────────────────────────────────────────────────

/** Grab one frame and, if the scheduler agrees, encode and queue it. */
async function captureNow(reason: ShotReason, ref = ''): Promise<void> {
  if (!active.value || paused.value || !scheduler || !queue) return
  if (capturing) return
  capturing = true
  const gen = sessionGen
  try {
    const stream = getSource()
    if (!stream || stream.getVideoTracks().length === 0) return
    let sample: ShotSample | null
    try {
      sample = await grabber(stream, getStrokes())
    } catch (e) {
      appLog.w(TAG, 'frame grab failed', e)
      return
    }
    // A meeting can end while the frame is being decoded; the next one must not
    // receive this meeting's picture.
    if (gen !== sessionGen || !sample) return
    const at = Date.now()
    const decision = scheduler.shouldKeep(reason, sample.hash, at)
    if (!decision.keep) {
      // The expensive half never runs for a frame we are about to throw away.
      appLog.d(TAG, 'shot skipped', decision.reject, reason)
      return
    }
    const blob = await sample.encode()
    if (gen !== sessionGen) return
    if (!blob) {
      lost.value++
      return
    }
    scheduler.noteKept(reason, sample.hash, at)
    queue.enqueue({
      clientShotId: newClientShotId(),
      at,
      reason,
      ref,
      width: sample.width,
      height: sample.height,
      blob,
    })
  } finally {
    capturing = false
  }
}

/** A different picture is on the wire: capture once it has settled. */
function noteSourceChanged(reason: Extract<ShotReason, 'sharer' | 'switch'> = 'switch'): void {
  if (!active.value || paused.value) return
  setTimeout(() => {
    void captureNow(reason)
  }, SOURCE_TRIGGER_DELAY_MS)
}

function matchesCue(text: string): boolean {
  const lower = text.toLowerCase()
  return SPEECH_CUES.some((cue) => lower.includes(cue))
}

function scanSegments(list: ReadonlyArray<{ id: number; text: string; startedAt: number; durationMs: number }>): void {
  for (const seg of list) {
    if (!seg.id || seenSegments.has(seg.id)) continue
    seenSegments.add(seg.id)
    if (!matchesCue(seg.text)) continue
    const end = seg.startedAt + Math.max(0, seg.durationMs)
    const delay = Math.min(SPEECH_MAX_DELAY_MS, Math.max(0, end - Date.now()))
    setTimeout(() => {
      void captureNow('speech', String(seg.id))
    }, delay)
  }
}

function start(opts: {
  token: string
  getSource: () => MediaStream | null
  getStrokes: () => InkStroke[]
}): void {
  stop()
  sessionGen++
  getSource = opts.getSource
  getStrokes = opts.getStrokes
  seenSegments.clear()
  // Frames begin at the toggle. Lines transcribed before it were never
  // presented while this was on, and re-scanning them would fire a burst of
  // retroactive shots for keywords the user already heard.
  const notes = useLiveNotes()
  for (const seg of notes.segments.value) seenSegments.add(seg.id)
  scheduler = new ShotScheduler()
  count.value = 0
  lost.value = 0
  paused.value = false
  active.value = true

  const tok = opts.token
  queue = new ShotQueue({
    send: async (job) => {
      const result = await sendShot(tok, job)
      if (result.ok) count.value++
      return result
    },
    onLost: () => {
      lost.value++
    },
  })

  unsubFile = onMeetingFileOpened((path) => {
    setTimeout(() => {
      if (!active.value || !isMeetingFileTracking()) return
      // A visit shorter than a second is dropped from the timeline; capturing
      // it would leave a frame with nothing to sit next to.
      const stillOpen = meetingFileEntries().some((e) => e.path === path && e.closedAt === 0)
      if (stillOpen) void captureNow('file', path)
    }, FILE_TRIGGER_DELAY_MS)
  })

  unwatchSegments = watch(
    notes.segments,
    (list) => scanSegments(list),
    { deep: false },
  )

  periodic = setInterval(() => {
    void captureNow('change')
  }, PERIODIC_MS)
}

function stop(): void {
  sessionGen++
  active.value = false
  paused.value = false
  if (unsubFile) {
    unsubFile()
    unsubFile = null
  }
  if (unwatchSegments) {
    unwatchSegments()
    unwatchSegments = null
  }
  if (periodic !== null) {
    clearInterval(periodic)
    periodic = null
  }
  releaseVideo()
  // Free the 9 MB frame buffer while no meeting is running; it is recreated on
  // the next capture.
  fullCanvas = null
  fullCtx = null
  sampleCanvas = null
  sampleCtx = null
  const pending = queue
  queue = null
  scheduler = null
  // Fire and forget: the queue already carries its own token, and the meeting
  // UI must not wait for a few hundred KiB on the way back to the list.
  if (pending) void pending.flush(FLUSH_TIMEOUT_MS)
}

function pause(): void {
  paused.value = true
}

function resume(): void {
  paused.value = false
}

export function useMeetingShots() {
  return {
    active,
    paused,
    count,
    lost,
    start,
    stop,
    pause,
    resume,
    captureNow,
    noteSourceChanged,
  }
}

/** @internal Test seam: replace the DOM grabber. */
export function _setShotGrabberForTest(fn: Grabber | null): void {
  grabber = fn ?? domSample
}

/** @internal Test seam: drop all state without touching the network. */
export function _resetMeetingShotsForTest(): void {
  stop()
  count.value = 0
  lost.value = 0
  seenSegments.clear()
}
