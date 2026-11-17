/**
 * Screen recording: format choice and the ordered upload queue.
 *
 * Kept out of the composable so the two things that can silently corrupt a
 * recording — picking a codec the engine does not have, and writing chunks out
 * of order — are testable without a browser media stack.
 *
 * A recording is one file assembled on the server from sequential chunks. The
 * first chunk carries the container header; a gap or a duplicate in the middle
 * makes every later frame undecodable. That is why the queue is strict: it
 * never drops, never reorders, and reports a failure instead of skipping.
 */

/** Codecs to try, best first. VP9 gives the sharpest text at a given bitrate. */
export const RECORDING_MIME_CANDIDATES = [
  'video/webm;codecs=vp9,opus',
  'video/webm;codecs=vp8,opus',
  'video/webm;codecs=vp8',
  'video/webm',
] as const

/** Whether this engine can record video at all. */
export function recordingSupported(): boolean {
  return typeof MediaRecorder !== 'undefined'
}

/**
 * The first mime type the engine accepts, or '' to let it choose its default.
 *
 * `isTypeSupported` is missing on a few embedded engines; those get '' rather
 * than a constructor that throws.
 */
export function pickRecordingMime(
  isSupported: ((type: string) => boolean) | undefined = typeof MediaRecorder !== 'undefined'
    ? MediaRecorder.isTypeSupported?.bind(MediaRecorder)
    : undefined,
): string {
  if (!isSupported) return ''
  for (const type of RECORDING_MIME_CANDIDATES) {
    if (isSupported(type)) return type
  }
  return ''
}

/** One chunk as the recorder produced it. `seq` starts at 0 for every recording. */
export interface RecordingChunk {
  seq: number
  blob: Blob
}

export interface RecordingQueueOptions {
  /**
   * Upload one chunk. Resolve true when it is stored, false for a retryable
   * failure (network hiccup, 5xx); throw when retrying cannot help (a 409 gap,
   * 413 too big, 404 meeting gone) — that stops the recording immediately.
   */
  send: (chunk: RecordingChunk) => Promise<boolean>
  /** Called once when a chunk can no longer be uploaded. The recording stops. */
  onFatal: (reason: 'failed' | 'overflow') => void
  /** Backoff between attempts, in ms. Tests shrink this. */
  retryDelays?: readonly number[]
  /** Hard cap on bytes waiting for the network; protects memory, not bandwidth. */
  maxPendingBytes?: number
}

/** Default retry schedule: ~15s of tolerance for a tunnel hiccup. */
const DEFAULT_RETRY_DELAYS = [500, 1_000, 2_000, 4_000, 8_000] as const

/**
 * The default memory ceiling.
 *
 * At the recorder's ~2.5 Mbps this is roughly seven minutes of video. A queue
 * that deep means the connection is gone, not slow — holding more would only
 * trade a lost recording for an out-of-memory crash that also loses it.
 */
const DEFAULT_MAX_PENDING_BYTES = 160 * 1024 * 1024

export class RecordingChunkQueue {
  private readonly pending: RecordingChunk[] = []
  private pendingBytes = 0
  private pumping = false
  private fatal = false
  private finishing = false
  private readonly waiters: Array<() => void> = []
  private readonly retryDelays: readonly number[]
  private readonly maxPendingBytes: number

  constructor(private readonly options: RecordingQueueOptions) {
    this.retryDelays = options.retryDelays ?? DEFAULT_RETRY_DELAYS
    this.maxPendingBytes = options.maxPendingBytes ?? DEFAULT_MAX_PENDING_BYTES
  }

  /** Chunks waiting for the network (diagnostics and tests). */
  get depth(): number {
    return this.pending.length
  }

  get bytes(): number {
    return this.pendingBytes
  }

  get stopped(): boolean {
    return this.fatal
  }

  enqueue(chunk: RecordingChunk): void {
    if (this.fatal) return
    this.pending.push(chunk)
    this.pendingBytes += chunk.blob.size
    if (this.pendingBytes > this.maxPendingBytes) {
      // Everything already queued is useless without this chunk, so the whole
      // recording is lost either way; fail loudly instead of growing forever.
      this.fail('overflow')
      return
    }
    void this.pump()
  }

  /** No more chunks are coming; resolves when the queue has drained or failed. */
  async finish(): Promise<void> {
    this.finishing = true
    if (!this.pumping && this.pending.length === 0) return
    await new Promise<void>((resolve) => {
      this.waiters.push(resolve)
    })
  }

  private fail(reason: 'failed' | 'overflow'): void {
    if (this.fatal) return
    this.fatal = true
    this.pending.length = 0
    this.pendingBytes = 0
    this.options.onFatal(reason)
    this.release()
  }

  private release(): void {
    while (this.waiters.length > 0) {
      const resolve = this.waiters.shift()
      resolve?.()
    }
  }

  private async pump(): Promise<void> {
    if (this.pumping || this.fatal) return
    this.pumping = true
    try {
      while (this.pending.length > 0 && !this.fatal) {
        const chunk = this.pending[0]
        const ok = await this.uploadWithRetry(chunk)
        if (!ok) {
          this.fail('failed')
          break
        }
        this.pending.shift()
        this.pendingBytes -= chunk.blob.size
      }
    } finally {
      this.pumping = false
      // A chunk enqueued between the last loop check and here would otherwise
      // sit forever: `enqueue` only pumps when it is not already pumping.
      const more = !this.fatal && this.pending.length > 0
      if (more) void this.pump()
      else if (this.finishing || this.fatal) this.release()
    }
  }

  private async uploadWithRetry(chunk: RecordingChunk): Promise<boolean> {
    for (let attempt = 0; ; attempt++) {
      try {
        if (await this.options.send(chunk)) return true
      } catch {
        // Non-retryable (gap, too large, meeting gone): stop the recording now.
        return false
      }
      // A recording already declared dead must not hold a timer open while it
      // waits to discover that.
      if (this.fatal) return false
      if (attempt >= this.retryDelays.length) return false
      await new Promise<void>((resolve) => setTimeout(resolve, this.retryDelays[attempt]))
      if (this.fatal) return false
    }
  }
}
