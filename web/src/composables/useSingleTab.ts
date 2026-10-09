/**
 * Single-tab guard.
 *
 * Why this exists
 * ---------------
 * The server keys a client's WebSocket subscription by `client_id`, which the
 * frontend keeps in `localStorage` — so every tab of the same origin presents
 * the SAME id. `Manager.Subscribe` allows one live connection per id and closes
 * the previous one on a new subscribe, so two tabs fight over the slot: each
 * tab's socket is repeatedly replaced, its `onclose` schedules a reconnect, and
 * the reconnect replaces the other tab's socket in turn. Measured: one client
 * id produced 1,402 subscribes in 17 minutes, with connections lasting ~275ms /
 * ~1750ms alternately. Every reconnect re-runs the app's full state sync
 * (project, files, git branch, sessions, tasks, terminal status) plus
 * fetchPendingEvents, so the storm produced ~2,900 requests/minute and made
 * every page slow to open.
 *
 * Rather than give each tab its own identity — which would mean two live
 * subscriptions, two event streams, and duplicate notifications for the same
 * events — only one tab is allowed to run the app at a time. A second tab shows
 * a blocking screen and never mounts the app, so it opens no WebSocket and
 * issues no API requests.
 *
 * How ownership is decided
 * ------------------------
 * Two tiers, in order of preference:
 *
 *   1. **Web Locks** (`navigator.locks`) — the primary mechanism. A single
 *      exclusive lock named after the channel is the resource; whichever tab
 *      holds it owns the app. The lock is released automatically when the
 *      holding context dies (tab closed, navigated, crashed, or the bfcache
 *      entry evicted), so a tab can never leave a "ghost owner" behind that
 *      blocks everyone else forever — which is exactly the failure the
 *      BroadcastChannel-only protocol was prone to (a `taken` verdict was
 *      never re-checked, so a lost `release` pinned the blocked tab until the
 *      user reloaded by hand). A blocked tab also issues a queued lock request
 *      that the browser grants the instant the owner goes away, which is the
 *      hand-over signal.
 *
 *   2. **BroadcastChannel** — the fallback for environments without Web Locks
 *      (jsdom in tests, and older browsers / WebViews). The protocol is
 *      deliberately simple and self-healing:
 *        a. A tab starts by claiming ownership and broadcasts `claim`.
 *        b. Any other tab holding ownership answers `taken`, and the claimant
 *           stands down (shows the blocked screen).
 *        c. An owner that hears `claim` re-announces `taken`, so a claim racing
 *           a closing owner still resolves to whoever actually holds it.
 *        d. On unload an owner broadcasts `release`; a blocked tab then reloads
 *           to take over, so closing the primary tab hands ownership to the
 *           waiting one.
 *        e. A tab restored from the back/forward cache re-runs acquire(),
 *           because a frozen tab cannot observe `release`/`claim` messages and
 *           may have lost (or gained) ownership while it was suspended.
 *      Its claim window is short (CLAIM_TIMEOUT_MS): if no owner answers, the
 *      claimant assumes it is the only tab and proceeds — a lost race then
 *      resolves as "both tabs think they own it", which is no worse than the
 *      status quo and is corrected on the next reload.
 *
 * Both tiers share the same public contract, so `main.ts` is agnostic to which
 * one is active.
 */

/** Channel / lock name. Versioned so a future protocol change cannot collide. */
const CHANNEL_NAME = 'clawbench-single-tab-v1'
const LOCK_NAME = 'clawbench-single-tab-v1'

/**
 * How long a claiming tab waits for an existing owner to object.
 *
 * This applies only to the BroadcastChannel fallback; Web Locks reports
 * availability directly. It is paid on EVERY page load that uses the fallback,
 * including the common single-tab case, so it is kept small. A BroadcastChannel
 * round-trip is sub-millisecond on the same machine; the margin is for a busy
 * main thread, not for network latency. Measured: a lone tab's acquire() cost
 * ~405ms at 400ms and ~155ms at 150ms. Being too aggressive is self-correcting
 * — a missed objection means two tabs briefly both think they own it, which the
 * next reload resolves.
 */
export const CLAIM_TIMEOUT_MS = 150

type GuardMessage = { type: 'claim' | 'taken' | 'release'; tabId: string }

export interface SingleTabGuardOptions {
  /** Override for tests. Defaults to a per-load random id. */
  tabId?: string
  /** Override for tests. */
  timeoutMs?: number
}

export interface SingleTabGuard {
  /**
   * Resolves true when this tab may run the app, false when another tab already
   * owns it. Never rejects.
   *
   * Safe to call again on the same instance: a tab restored from the
   * back/forward cache re-runs this because it was frozen and may have missed
   * ownership changes. Re-entry re-evaluates from scratch and updates the
   * internal ownership flag.
   */
  acquire(): Promise<boolean>
  /** Release ownership so a waiting tab can take over. Idempotent. */
  release(): void
  /** Stop listening without releasing ownership (page is going away anyway). */
  dispose(): void
  /**
   * Register a callback fired when the current owner releases. A blocked tab
   * uses this to reload and take over, so no manual refresh is needed.
   */
  whenOwnerReleases(cb: () => void): void
  /** Test-only: the live BroadcastChannel, to assert it is reused on re-entry. */
  __channelForTesting(): unknown
}

function randomTabId(): string {
  try {
    if (typeof crypto !== 'undefined' && typeof crypto.randomUUID === 'function') {
      return crypto.randomUUID()
    }
  } catch {
    // fall through to the Math.random fallback
  }
  return `t${Date.now().toString(36)}${Math.random().toString(36).slice(2, 10)}`
}

/**
 * The Web Locks `LockManager`, or null where the API is unavailable.
 *
 * Typed as present in lib.dom, but undefined at runtime in jsdom and in older
 * WebViews, so the probe is a real one, not a formality.
 */
function getLockManager(): LockManager | null {
  try {
    const locks = (navigator as Navigator & { locks?: LockManager }).locks
    return locks && typeof locks.request === 'function' ? locks : null
  } catch {
    return null
  }
}

/**
 * True when the guard should be skipped entirely.
 *
 * - Child frames share the parent's storage and would otherwise fight it for
 *   ownership; they are always allowed through.
 * - The native app hosts a single WebView, so there is nothing to arbitrate.
 *   Its background service uses a separate `native-bg-*` id and never loads
 *   this page.
 */
function guardNotApplicable(): boolean {
  try {
    if (window !== window.top) return true
  } catch {
    // Cross-origin top access throws — a child frame; skip the guard.
    return true
  }
  return false
}

export function createSingleTabGuard(options: SingleTabGuardOptions = {}): SingleTabGuard {
  const tabId = options.tabId ?? randomTabId()
  const timeoutMs = options.timeoutMs ?? CLAIM_TIMEOUT_MS

  let owns = false
  let disposed = false
  let channel: BroadcastChannel | null = null
  // Set when another tab answers our claim with `taken`.
  let contestedBy: string | null = null
  // Fired when the owner released ownership and this tab may take over.
  let onOwnerReleased: (() => void) | null = null
  // True once we have observed the owner releasing (either tier). Latched so a
  // release that lands BEFORE whenOwnerReleases() is registered is not lost —
  // the owner can go away in the window between acquire() resolving false and
  // main.ts wiring up the reload callback.
  let ownerReleased = false

  // ── Web Locks state ──────────────────────────────────────────────────────
  // True while this tab holds the exclusive lock. Re-entry (bfcache restore)
  // short-circuits on it: a fresh `ifAvailable` request would report null
  // because we hold the lock ourselves.
  let holdsLock = false
  // Resolving this releases the lock the browser is holding on our behalf.
  let releaseHold: (() => void) | null = null
  // A blocked tab registers one queued lock request to learn when the owner
  // goes away; the browser grants it the moment the lock frees up.
  let releaseWatchStarted = false

  function post(msg: GuardMessage) {
    try {
      channel?.postMessage(msg)
    } catch {
      // A closed channel throws; ownership state is unaffected.
    }
  }

  /**
   * Record that the owner went away and fire the reload callback if one is
   * registered. Latches the event so a release observed before the callback is
   * wired up still triggers a reload once whenOwnerReleases() runs.
   */
  function notifyOwnerReleased() {
    if (disposed) return
    ownerReleased = true
    onOwnerReleased?.()
  }

  function openChannel(): BroadcastChannel | null {
    if (typeof BroadcastChannel === 'undefined') return null
    try {
      const ch = new BroadcastChannel(CHANNEL_NAME)
      ch.onmessage = (e: MessageEvent<GuardMessage>) => {
        const msg = e.data
        if (!msg || msg.tabId === tabId) return
        if (msg.type === 'claim') {
          // Someone is claiming. If we hold it, say so — this is what stops a
          // second tab. If we don't, stay silent: the claimant will proceed on
          // timeout, and two non-owners must not deadlock each other.
          if (owns) post({ type: 'taken', tabId })
          return
        }
        if (msg.type === 'taken') {
          contestedBy = msg.tabId
          return
        }
        if (msg.type === 'release') {
          notifyOwnerReleased()
        }
      }
      return ch
    } catch {
      return null
    }
  }

  /**
   * Claim the app through Web Locks.
   *
   * `ifAvailable: true` never queues: the callback runs immediately with the
   * lock when it is free, or with null when another tab holds it. On success we
   * keep the callback pending (via `releaseHold`) so the browser holds the lock
   * for as long as this tab lives — releasing it on teardown, crash, or
   * navigation without any cooperation from us.
   *
   * Returns true when granted, false when another tab holds the lock, and null
   * when Web Locks itself failed — the caller then falls back to the
   * BroadcastChannel tier rather than blocking on an API error.
   */
  async function acquireViaLocks(locks: LockManager): Promise<boolean | null> {
    // Re-entry while we still hold the lock means we are the owner (a
    // pageshow re-validation). A new request would see our own lock as taken
    // and report null, so short-circuit rather than misread it as contested.
    if (holdsLock) {
      owns = true
      return true
    }

    const granted = await new Promise<boolean | null>((resolve) => {
      let settled = false
      const finish = (value: boolean | null) => {
        if (!settled) {
          settled = true
          resolve(value)
        }
      }
      try {
        const req = locks.request(LOCK_NAME, { ifAvailable: true }, (lock) => {
          if (!lock) {
            // Another tab holds it. We are blocked.
            finish(false)
            return undefined
          }
          // Keep the callback pending until release()/dispose(): the browser
          // holds the lock while this promise is unsettled.
          const hold = new Promise<void>((res) => { releaseHold = res })
          finish(true)
          return hold
        })
        // `ifAvailable` is not expected to reject, but if it does we must not
        // read that as "another tab owns it" — report the failure so the
        // caller can fall back.
        void Promise.resolve(req).catch(() => finish(null))
      } catch {
        finish(null)
      }
    })

    if (granted === null) return null
    if (!granted) {
      owns = false
      watchForLockRelease(locks)
      return false
    }

    holdsLock = true
    owns = true
    return true
  }

  /**
   * Ask the browser to tell us when the lock frees up.
   *
   * A queued (non-`ifAvailable`) request is granted the instant the current
   * holder releases it — including when that holder is a tab that closed,
   * navigated, or crashed without running any cleanup. This is the Web Locks
   * replacement for the BroadcastChannel `release` message, and it cannot get
   * stuck the way a lost message can.
   */
  function watchForLockRelease(locks: LockManager) {
    if (releaseWatchStarted || disposed) return
    releaseWatchStarted = true
    try {
      void Promise.resolve(
        locks.request(LOCK_NAME, () => {
          // Granted: the previous owner is gone. Return immediately, which
          // frees the lock again for whoever reloads next.
          notifyOwnerReleased()
        })
      ).catch(() => {
        // A rejected watch is harmless; the tab simply never auto-reloads.
      })
    } catch {
      // Synchronous throw: no watch, no auto-reload.
    }
  }

  /** Claim the app through the BroadcastChannel protocol (fallback). */
  async function acquireViaChannel(): Promise<boolean> {
    // Reuse an existing channel on re-entry (a tab restored from the
    // back/forward cache re-acquires). Opening a second one would leave the
    // first subscribed, so `taken`/`release` could arrive twice.
    if (!channel) channel = openChannel()
    if (!channel) {
      // No BroadcastChannel: cannot arbitrate. Allow the app rather than
      // blocking every user of an old browser.
      return true
    }

    // Re-entry must start from a clean verdict, otherwise a previous `taken`
    // would keep this tab blocked forever even after the owner went away.
    contestedBy = null
    owns = false
    post({ type: 'claim', tabId })

    // Give an existing owner a chance to object before claiming ownership.
    const deadline = Date.now() + timeoutMs
    while (Date.now() < deadline && contestedBy === null && !disposed) {
      await new Promise(r => setTimeout(r, 20))
    }

    if (contestedBy !== null) {
      owns = false
      return false
    }

    owns = true
    return true
  }

  async function acquire(): Promise<boolean> {
    if (guardNotApplicable()) return true

    const locks = getLockManager()
    if (locks) {
      const viaLocks = await acquireViaLocks(locks)
      if (viaLocks !== null) return viaLocks
      // Web Locks errored — fall through to the BroadcastChannel tier rather
      // than leaving the tab blocked on an API failure.
    }
    return acquireViaChannel()
  }

  function release() {
    if (!owns) return
    owns = false
    holdsLock = false
    // Releasing the browser-held lock promotes a waiting tab immediately.
    releaseHold?.()
    releaseHold = null
    post({ type: 'release', tabId })
  }

  function dispose() {
    disposed = true
    onOwnerReleased = null
    // Drop the lock now rather than waiting for the browser to reclaim it on
    // teardown, so a waiting tab is promoted without delay.
    releaseHold?.()
    releaseHold = null
    holdsLock = false
    try {
      channel?.close()
    } catch {
      // already closed
    }
    channel = null
  }

  function whenOwnerReleases(cb: () => void) {
    onOwnerReleased = cb
    // The owner may have gone away before this callback was wired up (the
    // window between acquire() resolving false and main.ts registering the
    // reload). Replay the latched release so the tab still takes over.
    if (ownerReleased) cb()
  }

  /** Test-only: expose the channel so tests can assert it is not re-created. */
  function __channelForTesting() {
    return channel
  }

  return { acquire, release, dispose, whenOwnerReleases, __channelForTesting }
}
