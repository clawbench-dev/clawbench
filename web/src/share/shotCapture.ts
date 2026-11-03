/**
 * Key-frame capture: hashing, de-duplication, quota and the upload queue.
 *
 * No Vue and no DOM. The caller injects a frame grabber (a canvas is not
 * available in every engine the app runs in, and it is not available at all in
 * the test runner), so the decisions that actually matter — "is this frame the
 * same as the last one", "how many have we kept", "what happens when an upload
 * fails" — are testable without a browser.
 *
 * The rules live here rather than in the composable because they are the part
 * that goes wrong quietly: a broken de-duplicator fills a meeting with hundreds
 * of identical slides, and a queue that retries a 400 turns one malformed frame
 * into a request storm.
 */

/** Why a frame was taken. Mirrors internal/service/live_notes_shots.go. */
export type ShotReason = 'manual' | 'file' | 'sharer' | 'switch' | 'speech' | 'change'

/** Lower runs first when the quota forces a choice; 1 is the user asking. */
export const SHOT_REASON_PRIORITY: Record<ShotReason, number> = {
  manual: 1,
  file: 2,
  sharer: 3,
  switch: 3,
  speech: 5,
  change: 6,
}

/** Two frames closer than this (out of 64 bits) are the same picture. */
export const SHOT_HASH_DISTANCE = 8

/** Non-manual triggers are rate-limited: a slide deck must not produce a shot
 *  every time the cursor moves. */
export const SHOT_MIN_INTERVAL_MS = 8_000

/** Client-side ceilings. The server has its own (100) as a safety net. */
export const SHOT_MAX_PER_HOUR = 40
export const SHOT_MAX_PER_MEETING = 80

/** How many uploads may wait behind the one in flight before the oldest
 *  low-value one is dropped. Manual frames are never dropped. */
export const SHOT_MAX_PENDING = 8

export interface ShotJob {
  clientShotId: string
  at: number
  reason: ShotReason
  ref: string
  width: number
  height: number
  blob: Blob
}

/** What the transport reported. `status: 0` means "never reached the server". */
export type ShotSendResult = { ok: true } | { ok: false; status: number }

/**
 * Difference hash over a 9×8 grey sample.
 *
 * Each pixel is compared with the one to its right, which makes the result
 * insensitive to overall brightness (a slide dimmed by a projector still
 * matches) but sensitive to layout — exactly what "the picture changed" means
 * for a screen share.
 */
export function dhash(gray: ArrayLike<number>, width = 9, height = 8): string {
  let out = ''
  for (let y = 0; y < height; y++) {
    for (let x = 0; x < width - 1; x++) {
      const i = y * width + x
      out += gray[i] > gray[i + 1] ? '1' : '0'
    }
  }
  return out
}

/** Bit distance between two equal-length binary hashes. */
export function hashDistance(a: string, b: string): number {
  if (!a || !b) return Number.MAX_SAFE_INTEGER
  const n = Math.min(a.length, b.length)
  let d = Math.abs(a.length - b.length)
  for (let i = 0; i < n; i++) if (a[i] !== b[i]) d++
  return d
}

export type ShotRejectReason = 'interval' | 'duplicate' | 'quota'

export interface ShotDecision {
  keep: boolean
  reject?: ShotRejectReason
}

/**
 * Decides whether a frame is worth keeping.
 *
 * State is the last KEPT frame, not the last one seen: comparing against a
 * rejected frame lets a slowly drifting picture (a video, a scrolling page)
 * walk away from the anchor one small step at a time.
 */
export class ShotScheduler {
  private lastHash = ''
  private lastKeptAt = 0
  private keptAt: number[] = []

  constructor(private readonly maxPerHour = SHOT_MAX_PER_HOUR, private readonly maxPerMeeting = SHOT_MAX_PER_MEETING) {}

  shouldKeep(reason: ShotReason, hash: string, at: number): ShotDecision {
    // The user pressed the button: the frame is wanted even if it looks like
    // the previous one, and even past the soft quota.
    if (reason === 'manual') return { keep: true }
    if (this.keptAt.length >= this.maxPerMeeting) return { keep: false, reject: 'quota' }
    if (this.lastKeptAt > 0 && at - this.lastKeptAt < SHOT_MIN_INTERVAL_MS) {
      return { keep: false, reject: 'interval' }
    }
    if (hash && this.lastHash && hashDistance(hash, this.lastHash) <= SHOT_HASH_DISTANCE) {
      return { keep: false, reject: 'duplicate' }
    }
    const lastHour = this.keptAt.filter((t) => at - t < 3_600_000).length
    if (lastHour >= this.maxPerHour && SHOT_REASON_PRIORITY[reason] > 3) {
      return { keep: false, reject: 'quota' }
    }
    return { keep: true }
  }

  noteKept(reason: ShotReason, hash: string, at: number): void {
    this.keptAt.push(at)
    // Bound the bookkeeping: a meeting cannot produce more than the per-meeting
    // cap, so anything older than that is only noise for the hourly window.
    if (this.keptAt.length > this.maxPerMeeting) this.keptAt = this.keptAt.slice(-this.maxPerMeeting)
    if (hash) this.lastHash = hash
    this.lastKeptAt = at
    void reason
  }

  reset(): void {
    this.lastHash = ''
    this.lastKeptAt = 0
    this.keptAt = []
  }

  get count(): number {
    return this.keptAt.length
  }
}

export interface ShotQueueOptions {
  send: (job: ShotJob) => Promise<ShotSendResult>
  /** Called once per frame that could not be delivered. */
  onLost: () => void
  /** Injectable for tests; defaults to a real timer. */
  sleep?: (ms: number) => Promise<void>
  maxPending?: number
}

/** Delays between retries. Four attempts, then the frame is reported lost. */
export const SHOT_RETRY_DELAYS = [1_000, 3_000, 9_000]

/** A 4xx is a verdict about this frame, not a transport failure: retrying it
 *  cannot succeed and only delays the frames behind it. 429 is the exception. */
export function shotRetryable(status: number): boolean {
  return status === 0 || status === 429 || status >= 500
}

/**
 * Serial upload queue.
 *
 * One frame in flight at a time: each upload is a few hundred KiB and the same
 * connection is carrying the meeting's audio segments. A bounded queue keeps a
 * long outage from growing memory without bound; when it overflows the oldest
 * low-priority frame goes first, because the newest frames are the ones the
 * meeting is still about.
 */
export class ShotQueue {
  private pending: ShotJob[] = []
  private running: Promise<void> | null = null
  private idleWaiters: Array<() => void> = []
  private readonly sleep: (ms: number) => Promise<void>

  constructor(private readonly opts: ShotQueueOptions) {
    this.sleep = opts.sleep ?? ((ms) => new Promise((resolve) => setTimeout(resolve, ms)))
  }

  /** Frames waiting or in flight (diagnostics / tests). */
  get size(): number {
    return this.pending.length
  }

  enqueue(job: ShotJob): void {
    const max = this.opts.maxPending ?? SHOT_MAX_PENDING
    if (this.pending.length >= max) {
      const idx = this.pending.findIndex((j) => j.reason !== 'manual')
      if (idx >= 0) this.pending.splice(idx, 1)
      else this.pending.shift()
      this.opts.onLost()
    }
    this.pending.push(job)
    void this.pump()
  }

  /** Resolves when the queue drains, or after `timeoutMs` — whichever is first. */
  async flush(timeoutMs: number): Promise<void> {
    if (!this.running && this.pending.length === 0) return
    await new Promise<void>((resolve) => {
      let settled = false
      let timer: ReturnType<typeof setTimeout> | null = null
      const finish = (): void => {
        if (settled) return
        settled = true
        if (timer !== null) clearTimeout(timer)
        const i = this.idleWaiters.indexOf(finish)
        if (i >= 0) this.idleWaiters.splice(i, 1)
        resolve()
      }
      timer = setTimeout(finish, timeoutMs)
      this.idleWaiters.push(finish)
    })
  }

  /** Stops accepting work and drops what is queued (used on a hard reset). */
  clear(): void {
    this.pending = []
    this.settle()
  }

  private settle(): void {
    if (this.running || this.pending.length) return
    const waiters = this.idleWaiters
    this.idleWaiters = []
    for (const resolve of waiters) resolve()
  }

  private pump(): void {
    if (this.running) return
    const job = this.pending.shift()
    if (!job) {
      this.settle()
      return
    }
    this.running = this.deliver(job)
      .finally(() => {
        this.running = null
        if (this.pending.length) this.pump()
        else this.settle()
      })
  }

  private async deliver(job: ShotJob): Promise<void> {
    for (let attempt = 0; attempt <= SHOT_RETRY_DELAYS.length; attempt++) {
      if (attempt > 0) await this.sleep(SHOT_RETRY_DELAYS[attempt - 1])
      let result: ShotSendResult
      try {
        result = await this.opts.send(job)
      } catch {
        result = { ok: false, status: 0 }
      }
      if (result.ok) return
      if (!shotRetryable(result.status)) {
        // A rejected frame is not a transport problem; report it and move on so
        // the rest of the meeting is not stuck behind it.
        this.opts.onLost()
        return
      }
    }
    this.opts.onLost()
  }
}

/**
 * A unique id for one frame, used for idempotent retries.
 *
 * `crypto.randomUUID` needs a secure context, and the app is normally served
 * over plain http on a LAN — the same fallback as the chat client id.
 */
export function newClientShotId(): string {
  const c = globalThis.crypto as Crypto | undefined
  if (c && typeof c.randomUUID === 'function') return c.randomUUID()
  return `${Date.now().toString(36)}-${Math.random().toString(36).slice(2, 10)}`
}
