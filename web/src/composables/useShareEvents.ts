/**
 * "A share was just created or revoked" — a one-way notification.
 *
 * WHY A SEPARATE MODULE
 * ---------------------
 * The 分享 tab lists all three kinds at once, but they are created from three
 * unrelated places: a file preview toolbar, a conversation's share dialog, and
 * a meeting's result page. None of them knows the tab exists, and the tab is
 * lazily mounted — it may not even be in memory when the share is made.
 *
 * The tab already refreshes when it is re-activated, which covers the common
 * "share something, then go look at the list" path. What it does NOT cover is
 * having the tab open on one side of a wide screen while the share is made on
 * the other — there the list sat stale until the user switched away and back.
 *
 * WHY NOT POLL
 * ------------
 * A share is a rare, user-initiated event. Polling three endpoints every few
 * seconds to catch something that happens a handful of times a day would cost
 * far more than it saves, and would fight the refresh button's own feedback.
 *
 * WHY NOT A WATCHER ON THE EXISTING SHARE SETS
 * ------------------------------
 * Those sets hold "which file/session is shared", and the tab's list contains
 * meeting rows they know nothing about (a meeting's token is minted by the
 * server on join, not by a dialog). Watching them would refresh on the wrong
 * signal and still miss meetings. The emit is placed at the mutation site
 * instead, so it fires for every kind.
 */

type ShareKind = 'file' | 'session' | 'meeting'

type Listener = (kind: ShareKind) => void

const listeners = new Set<Listener>()

/**
 * Announce that a share of `kind` was just created or revoked.
 *
 * Call this AFTER the server has accepted the change — a listener that refetches
 * immediately must not race the write it is reacting to.
 */
export function emitShareChanged(kind: ShareKind): void {
  for (const fn of listeners) {
    try {
      fn(kind)
    } catch {
      // A listener throwing must not stop the others, and must never surface as
      // an error in the component that merely shared something.
    }
  }
}

/** Subscribe to share changes. Returns the unsubscribe function. */
export function onShareChanged(fn: Listener): () => void {
  listeners.add(fn)
  return () => {
    listeners.delete(fn)
  }
}

/** Test seam: drop every listener (module state survives between test files). */
export function _resetShareChangedForTest(): void {
  listeners.clear()
}
