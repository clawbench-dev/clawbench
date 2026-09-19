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
 * A BroadcastChannel election, because it is available in every target browser
 * (Web Locks is not, notably in jsdom and older WebViews).
 *
 * The protocol is deliberately simple and self-healing:
 *   1. A tab starts by claiming ownership and broadcasts `claim`.
 *   2. Any other tab holding ownership answers `taken`, and the claimant
 *      stands down (shows the blocked screen).
 *   3. An owner that hears `claim` re-announces `taken`, so a claim racing a
 *      closing owner still resolves to whoever actually holds it.
 *   4. On unload an owner broadcasts `release`; a blocked tab then reloads to
 *      take over, so closing the primary tab hands ownership to the waiting one.
 *   5. A tab restored from the back/forward cache re-runs acquire(), because a
 *      frozen tab cannot observe `release`/`claim` messages and may have lost
 *      (or gained) ownership while it was suspended.
 *
 * The claim window is short (CLAIM_TIMEOUT_MS). If no owner answers, the
 * claimant assumes it is the only tab and proceeds — a lost race then resolves
 * as "both tabs think they own it", which is no worse than today's behaviour
 * and is corrected on the next reload.
 */

/** Channel name. Versioned so a future protocol change cannot collide. */
const CHANNEL_NAME = 'clawbench-single-tab-v1'

/**
 * How long a claiming tab waits for an existing owner to object.
 *
 * This delay is paid on EVERY page load, including the common single-tab case,
 * so it is kept small. A BroadcastChannel round-trip is sub-millisecond on the
 * same machine; the margin is for a busy main thread, not for network latency.
 * Measured: a lone tab's acquire() cost ~405ms at 400ms and ~155ms at 150ms.
 * Being too aggressive is self-correcting — a missed objection means two tabs
 * briefly both think they own it, which the next reload resolves.
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

  function post(msg: GuardMessage) {
    try {
      channel?.postMessage(msg)
    } catch {
      // A closed channel throws; ownership state is unaffected.
    }
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
          onOwnerReleased?.()
        }
      }
      return ch
    } catch {
      return null
    }
  }

  async function acquire(): Promise<boolean> {
    if (guardNotApplicable()) return true

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

  function release() {
    if (!owns) return
    owns = false
    post({ type: 'release', tabId })
  }

  function dispose() {
    disposed = true
    onOwnerReleased = null
    try {
      channel?.close()
    } catch {
      // already closed
    }
    channel = null
  }

  function whenOwnerReleases(cb: () => void) {
    onOwnerReleased = cb
  }

  /** Test-only: expose the channel so tests can assert it is not re-created. */
  function __channelForTesting() {
    return channel
  }

  return { acquire, release, dispose, whenOwnerReleases, __channelForTesting }
}
