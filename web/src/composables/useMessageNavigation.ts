import { ref } from 'vue'

/**
 * Pending "open this chat message" request, consumed by the chat panel.
 *
 * A quote can carry a `sessionId` + `messageId` (see FileEntry), and clicking
 * its jump button must land the user on that exact message. Two problems make
 * a plain function call insufficient:
 *
 *  1. The target session may not be loaded yet. Switching sessions is async, so
 *     the message list cannot be scrolled until its messages arrive.
 *  2. The quote may point at a DIFFERENT session (or project) than the one on
 *     screen, so the request must survive the switch.
 *
 * A module-level pending request solves both: the dispatcher records what it
 * wants, and the chat panel consumes it once the target session's messages are
 * actually rendered. Same pattern as useCommitNavigation's pendingSha.
 */

export interface PendingMessageNavigation {
  sessionId: string
  messageId: number
  /**
   * Why the jump was requested. Only `'search'` changes the failure copy: a
   * session-search hit is a *search* result, so "that message is no longer in
   * the session" is accurate. Quote and notification jumps share this channel
   * and must not be told they came from a search.
   */
  source?: 'search' | 'quote' | 'notification'
}

const pending = ref<PendingMessageNavigation | null>(null)

/** Record a request to open a specific message in a specific session. */
export function setPendingMessageNavigation(
  sessionId: string,
  messageId: number,
  source?: PendingMessageNavigation['source'],
) {
  if (!sessionId || !messageId) return
  pending.value = { sessionId, messageId, source }
}

/**
 * Take the pending request, if any, clearing it.
 *
 * Consuming (rather than reading) is deliberate: a request must fire exactly
 * once, or every later re-render of the chat panel would re-scroll the user
 * away from wherever they scrolled to.
 */
export function consumePendingMessageNavigation(): PendingMessageNavigation | null {
  const req = pending.value
  pending.value = null
  return req
}

/** The reactive ref, for callers that need to watch it. */
export { pending as pendingMessageNavigation }

/**
 * Drop a request that can never be satisfied because its target session no
 * longer exists.
 *
 * The request is only consumed when the CURRENT session id matches it, so a
 * session that fails to open (deleted, archived, 404) leaves the request parked
 * forever — and the user later opening an unrelated session that happens to
 * reuse the id would be scrolled to a stale message and told it "is no longer in
 * the session". Called from the 404 recovery branch, which is the one place that
 * knows the session is gone rather than merely not loaded yet.
 *
 * Deliberately keyed on the session id: a request for a DIFFERENT session must
 * survive one session's failure.
 */
export function discardPendingMessageNavigation(sessionId: string): void {
  if (pending.value?.sessionId === sessionId) pending.value = null
}

/** Reset module-level state for testing. @internal */
export function _resetPendingMessageForTesting() {
  pending.value = null
}
