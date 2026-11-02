/**
 * Screen capture on Android.
 *
 * The Android WebView has no `getDisplayMedia`, so a share cannot start the way
 * the desktop shell does. Instead the page asks the native host for a
 * MediaProjection session and receives the picture as a stream of JPEG frames
 * plus 48 kHz stereo PCM, which this module turns back into a `MediaStream` the
 * rest of the publishing stack already understands (see useLiveShare).
 *
 * Three properties are load-bearing:
 *
 * - **The host drives the clock.** The native side fires a tick when a frame or
 *   a chunk is ready; the page pulls the payload. Polling from JS would die the
 *   moment the app goes to the background — which is the entire point of
 *   sharing your screen — because Chromium throttles timers in hidden pages to
 *   roughly 1 Hz. A tick is a direct script evaluation and is not throttled.
 * - **The newest frame wins.** The host keeps a single slot; a frame the page
 *   never takes is overwritten rather than queued. Pulling also gives natural
 *   backpressure: while a JPEG is being decoded the next tick's frame is simply
 *   dropped.
 * - **Audio is a jitter buffer, not a queue.** PCM arrives in 100 ms chunks and
 *   is played through a ScriptProcessor at 48 kHz; a small prefill absorbs the
 *   network- and bridge-side jitter, and a cap keeps latency from creeping up
 *   over a long meeting.
 */
import i18n from '@/i18n'
import { appLog } from '@/utils/appLog'
import { getNative } from '@/utils/clawbenchNative'

const TAG = 'AndroidCapture'

/** How often the start flow re-reads the native status while the dialog is up. */
const START_POLL_MS = 150
/**
 * Consent dialogs are slow, and the page is hidden while one is open (its
 * timers throttle to ~1 Hz), so this is generous on purpose.
 */
const START_TIMEOUT_MS = 120_000
/** How often the watchdog notices that the session ended on the native side. */
const WATCHDOG_MS = 2000

const SAMPLE_RATE = 48000
const CHANNELS = 2
/** 0.2 s of audio before playback starts, so the first chunks do not stutter. */
const PREFILL_FRAMES = Math.round(SAMPLE_RATE * 0.2)
/** Above this the buffer is cut back to PREFILL_FRAMES: latency must not drift. */
const MAX_BUFFER_FRAMES = Math.round(SAMPLE_RATE * 0.6)

export interface AndroidCaptureOptions {
  fps?: number
  maxWidth?: number
  quality?: number
  systemAudio?: boolean
  /**
   * Called when the session ends outside the page (the user revoked the share
   * from the system UI, or the service died). The publisher must end the whole
   * share at that point — the picture is gone.
   */
  onEnded?: () => void
}

export interface AndroidCaptureHandle {
  /** Video track, plus the system-audio track when one was captured. */
  stream: MediaStream
  width: number
  height: number
  /** Whether system audio is actually flowing (false when it was denied). */
  systemAudio: boolean
  stop(): void
}

interface NativeState {
  state?: string
  error?: string
  width?: number
  height?: number
  systemAudio?: boolean
}

/**
 * Fixed-capacity circular buffer of interleaved-to-planar stereo PCM.
 *
 * Kept as a class (rather than inline in the capture handle) so the overflow and
 * underrun rules can be tested without an AudioContext.
 */
export class PcmJitterBuffer {
  private readonly capacity: number
  private readonly left: Float32Array
  private readonly right: Float32Array
  private writeIndex = 0
  private readIndex = 0
  private available = 0
  private primed = false

  constructor(capacity = Math.round(SAMPLE_RATE * 2)) {
    this.capacity = capacity
    this.left = new Float32Array(capacity)
    this.right = new Float32Array(capacity)
  }

  /** Frames currently buffered (test/diagnostic). */
  get size(): number {
    return this.available
  }

  /** Append little-endian 16-bit interleaved stereo. */
  push(bytes: Uint8Array): void {
    if (bytes.length < 4) return
    const frames = Math.floor(bytes.length / 4)
    for (let i = 0; i < frames; i++) {
      const offset = i * 4
      // Little-endian: the platform hands us PCM_16BIT in native byte order,
      // which is little-endian on every Android ABI we ship.
      const l = ((bytes[offset] | (bytes[offset + 1] << 8)) << 16) >> 16
      const r = ((bytes[offset + 2] | (bytes[offset + 3] << 8)) << 16) >> 16
      this.left[this.writeIndex] = l / 32768
      this.right[this.writeIndex] = r / 32768
      this.writeIndex = (this.writeIndex + 1) % this.capacity
      if (this.available === this.capacity) {
        // Full: the oldest frame is what has to go.
        this.readIndex = (this.readIndex + 1) % this.capacity
      } else {
        this.available++
      }
    }
    if (this.available > MAX_BUFFER_FRAMES) {
      const drop = this.available - PREFILL_FRAMES
      this.readIndex = (this.readIndex + drop) % this.capacity
      this.available -= drop
    }
    if (!this.primed && this.available >= PREFILL_FRAMES) this.primed = true
  }

  /**
   * Fill the given output buffers, zero-filling any shortfall.
   *
   * Silence before the prefill is deliberate: starting playback on the first
   * chunk produces a stutter that never recovers, because every later underrun
   * lands mid-word.
   */
  pull(outLeft: Float32Array, outRight: Float32Array): void {
    const count = Math.min(outLeft.length, outRight.length)
    if (!this.primed) {
      outLeft.fill(0)
      outRight.fill(0)
      return
    }
    let i = 0
    for (; i < count && this.available > 0; i++) {
      outLeft[i] = this.left[this.readIndex]
      outRight[i] = this.right[this.readIndex]
      this.readIndex = (this.readIndex + 1) % this.capacity
      this.available--
    }
    if (i < count) {
      outLeft.fill(0, i)
      outRight.fill(0, i)
      // The buffer ran dry: re-arm the prefill so the next chunks arrive as a
      // continuous block instead of trickling in one at a time.
      this.primed = false
    }
  }

  clear(): void {
    this.writeIndex = 0
    this.readIndex = 0
    this.available = 0
    this.primed = false
  }
}

/** Decode one base64 JPEG into an ImageBitmap, or null when it is unusable. */
async function decodeFrame(base64: string): Promise<ImageBitmap | null> {
  try {
    const binary = atob(base64)
    const bytes = new Uint8Array(binary.length)
    for (let i = 0; i < binary.length; i++) bytes[i] = binary.charCodeAt(i)
    return await createImageBitmap(new Blob([bytes], { type: 'image/jpeg' }))
  } catch (e) {
    appLog.w(TAG, 'frame decode failed', e)
    return null
  }
}

function base64ToBytes(base64: string): Uint8Array {
  const binary = atob(base64)
  const bytes = new Uint8Array(binary.length)
  for (let i = 0; i < binary.length; i++) bytes[i] = binary.charCodeAt(i)
  return bytes
}

function sleep(ms: number): Promise<void> {
  return new Promise((resolve) => setTimeout(resolve, ms))
}

/**
 * Ask the host for a capture session and wire the result into a MediaStream.
 *
 * Rejects when the user denies the consent dialog, when the platform refuses the
 * capture, or when the host never answers. The caller is expected to surface the
 * message and end the share.
 */
export async function startAndroidScreenCapture(
  opts: AndroidCaptureOptions = {},
): Promise<AndroidCaptureHandle> {
  const bridge = getNative()
  if (!bridge || !bridge.startScreenCapture) {
    throw new Error(i18n.global.t('liveShare.unsupported'))
  }

  // Non-optional alias: the guard above narrows `bridge`, but the closures below
  // (tick / watchdog / stop) are defined after it and TypeScript drops the
  // narrowing for them.
  const native = bridge
  native.startScreenCapture!(
    JSON.stringify({
      fps: opts.fps ?? 10,
      maxWidth: opts.maxWidth ?? 1280,
      quality: opts.quality ?? 55,
      systemAudio: Boolean(opts.systemAudio),
    }),
  )

  const state = await waitForRunning()
  const width = state.width ?? 0
  const height = state.height ?? 0
  if (width <= 0 || height <= 0) {
    // The projection is live but unusable. Leaving it running would keep the
    // foreground service (and the system "screen is shared" badge) alive with
    // nobody reading it, so end it before reporting the failure.
    try {
      void native.stopScreenCapture?.()
    } catch {
      /* best effort */
    }
    throw new Error(i18n.global.t('liveShare.androidFailed'))
  }

  // ── Video sink ───────────────────────────────────────────────────────────
  const videoSink = createVideoSink(width, height)
  const jitter = new PcmJitterBuffer()
  const audioCtx = state.systemAudio ? createAudioSink(jitter) : null

  const stream = new MediaStream([videoSink.track])
  const audioTrack = audioCtx?.destination.stream.getAudioTracks()[0] ?? null
  if (audioTrack) stream.addTrack(audioTrack)

  let stopped = false
  let busy = false

  const tick = async (): Promise<void> => {
    if (stopped || busy) return
    busy = true
    try {
      // Both queues are drained on the same tick: the host coalesces nothing, so
      // a tick means "something is ready", and asking for both is cheaper than
      // tracking which one fired.
      const frame = await native.takeScreenFrame?.()
      if (frame) await videoSink.write(await decodeFrame(frame))
      const pcm = await native.takeSystemAudio?.()
      if (pcm) jitter.push(base64ToBytes(pcm))
    } catch (e) {
      appLog.w(TAG, 'tick failed', e)
    } finally {
      busy = false
    }
  }

  const globalWindow = window as unknown as Record<string, unknown>
  globalWindow.__clawbenchScreenTick = () => {
    void tick()
  }

  const watchdog = setInterval(() => {
    if (stopped) return
    void Promise.resolve(native.getScreenCaptureState?.() ?? '').then((raw) => {
      if (stopped) return
      try {
        const next = JSON.parse(String(raw)) as NativeState
        if (next.state === 'running') return
      } catch {
        // A garbled status is not evidence that the share ended; the tick will
        // tell us soon enough either way.
        return
      }
      appLog.i(TAG, 'capture ended on the native side')
      stop()
      opts.onEnded?.()
    })
  }, WATCHDOG_MS)

  function stop(): void {
    if (stopped) return
    stopped = true
    clearInterval(watchdog)
    delete globalWindow.__clawbenchScreenTick
    try {
      void native.stopScreenCapture?.()
    } catch (e) {
      appLog.w(TAG, 'stopScreenCapture failed', e)
    }
    for (const track of stream.getTracks()) {
      try {
        track.stop()
      } catch {
        /* already ended */
      }
    }
    videoSink.close()
    audioCtx?.close()
    jitter.clear()
  }

  return {
    stream,
    width,
    height,
    systemAudio: Boolean(audioCtx),
    stop,
  }
}

/** Poll the native status until the projection is live. */
async function waitForRunning(): Promise<NativeState> {
  const bridge = getNative()
  const deadline = Date.now() + START_TIMEOUT_MS
  while (Date.now() < deadline) {
    await sleep(START_POLL_MS)
    let parsed: NativeState
    try {
      parsed = JSON.parse(String(await bridge?.getScreenCaptureState?.())) as NativeState
    } catch {
      continue
    }
    if (parsed.state === 'running') return parsed
    if (parsed.state === 'denied') throw new Error(i18n.global.t('liveShare.androidDenied'))
    if (parsed.state === 'error') {
      throw new Error(parsed.error || i18n.global.t('liveShare.androidFailed'))
    }
  }
  throw new Error(i18n.global.t('liveShare.androidFailed'))
}

interface VideoSink {
  track: MediaStreamTrack
  write(bitmap: ImageBitmap | null): Promise<void>
  close(): void
}

/**
 * Build the video track.
 *
 * `VideoTrackGenerator` is preferred: frames written into it never touch the
 * compositor, so they keep flowing while the app is in the background — which is
 * exactly when a screen share runs. Older WebViews fall back to a canvas, where
 * the picture is produced by the compositor and may stall while hidden.
 */
function createVideoSink(width: number, height: number): VideoSink {
  const globalWindow = window as unknown as Record<string, unknown>
  const Generator =
    (globalWindow.VideoTrackGenerator as new (init: { kind: 'video' }) => MediaStreamTrack & {
      writable: WritableStream<VideoFrame>
    }) ??
    (globalWindow.MediaStreamTrackGenerator as new (init: { kind: 'video' }) => MediaStreamTrack & {
      writable: WritableStream<VideoFrame>
    })

  if (Generator && typeof globalWindow.VideoFrame === 'function') {
    const generator = new Generator({ kind: 'video' })
    const writer = generator.writable.getWriter()
    return {
      track: generator,
      async write(bitmap) {
        if (!bitmap) return
        // The frame holds a GPU texture and must be released whether or not the
        // write succeeded: once the generator is closed every tick throws, and
        // a 10 fps leak of VideoFrames is what makes a long share crawl.
        let frame: VideoFrame | null = null
        try {
          frame = new VideoFrame(bitmap, { timestamp: performance.now() * 1000 })
          await writer.write(frame)
        } catch (e) {
          appLog.w(TAG, 'frame write failed', e)
        } finally {
          frame?.close()
          bitmap.close()
        }
      },
      close() {
        void writer.close().catch(() => undefined)
      },
    }
  }

  const canvas = document.createElement('canvas')
  canvas.width = width
  canvas.height = height
  const ctx = canvas.getContext('2d')
  const stream = canvas.captureStream(0)
  const track = stream.getVideoTracks()[0] as CanvasCaptureMediaStreamTrack
  return {
    track,
    async write(bitmap) {
      if (!bitmap) return
      try {
        ctx?.drawImage(bitmap, 0, 0, canvas.width, canvas.height)
        track.requestFrame()
      } finally {
        bitmap.close()
      }
    },
    close() {
      track.stop()
    },
  }
}

interface AudioSink {
  destination: MediaStreamAudioDestinationNode
  close(): void
}

/**
 * Play the PCM stream into a MediaStreamDestination.
 *
 * A ScriptProcessor (rather than an AudioWorklet) because it needs no module
 * load: a failed `addModule` on some WebView builds would take the whole audio
 * track down, and the extra ~85 ms of block latency is irrelevant next to the
 * 200 ms jitter buffer. The zero-valued ConstantSource only exists to keep the
 * processor "active" — a ScriptProcessor with no connected input is not
 * guaranteed to be pulled by every implementation.
 */
function createAudioSink(jitter: PcmJitterBuffer): AudioSink {
  const ctx = new AudioContext({ sampleRate: SAMPLE_RATE })
  void ctx.resume().catch(() => undefined)
  const destination = ctx.createMediaStreamDestination()
  const processor = ctx.createScriptProcessor(4096, CHANNELS, CHANNELS)
  processor.onaudioprocess = (event) => {
    const left = event.outputBuffer.getChannelData(0)
    const right = event.outputBuffer.getChannelData(1)
    jitter.pull(left, right)
  }
  const keepAlive = ctx.createConstantSource()
  keepAlive.offset.value = 0
  keepAlive.connect(processor)
  processor.connect(destination)
  keepAlive.start()

  return {
    destination,
    close() {
      try {
        processor.onaudioprocess = null
        keepAlive.stop()
        keepAlive.disconnect()
        processor.disconnect()
        destination.disconnect()
      } catch {
        /* already torn down */
      }
      void ctx.close().catch(() => undefined)
    },
  }
}
