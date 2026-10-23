import type { PendingMessageNavigation } from '@/composables/useMessageNavigation'

/**
 * Decide what a pending "open this message" request should do, given the
 * current session state.
 *
 * Extracted from the chat panel's watcher so the decisions can be tested
 * directly: the watcher itself only has an effect on a fully mounted panel with
 * a live session, and the branches below (defer during a switch, page to the
 * target, fall back when the target is gone) are where the bugs were. Every
 * branch is a pure decision here; the caller performs the side effects.
 */
export type PendingNavigationDecision =
  /** Nothing to do — no request, a request for another session, or a switch in flight. */
  | { kind: 'skip'; reason: 'none' | 'other-session' | 'switching' }
  /** Consume the request and try to reach `messageId`. */
  | { kind: 'jump'; messageId: number; source: PendingMessageNavigation['source'] }

/**
 * @param pending   the parked request (module state), may be null
 * @param sessionId the session currently on screen
 * @param switching whether a session switch is in flight
 */
export function decidePendingNavigation(
  pending: PendingMessageNavigation | null,
  sessionId: string | null,
  switching: boolean,
): PendingNavigationDecision {
  if (!pending) return { kind: 'skip', reason: 'none' }
  // The request may target a session that is not on screen yet. Acting earlier
  // would search the wrong session's message list and silently do nothing.
  if (pending.sessionId !== sessionId) return { kind: 'skip', reason: 'other-session' }
  // A switch clears the message array synchronously and refetches. Consuming
  // during that window searches an empty list, concludes the message is gone and
  // shows a spurious failure — so the request must stay parked and be retried
  // when the load finishes (the watcher re-runs on `switching`).
  if (switching) return { kind: 'skip', reason: 'switching' }
  return { kind: 'jump', messageId: pending.messageId, source: pending.source }
}

/**
 * The toast key for a jump that could not reach its target.
 *
 * Quote and notification jumps share this channel, so the copy must not claim a
 * search happened for them — "that search result is gone" would be wrong.
 */
export function jumpFailureMessageKey(source: PendingMessageNavigation['source']): string {
  return source === 'search' ? 'sessionSearch.jumpFailed' : 'sessionSearch.jumpFailedGeneric'
}
