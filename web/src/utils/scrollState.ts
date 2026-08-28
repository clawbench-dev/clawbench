/**
 * Scroll ownership state machine + pure decision functions for the chat message list.
 *
 * Root-cause context: the chat list's auto-follow logic relied on scattered
 * `userTouching` / `isAtBottom` flags, a fixed 150ms post-touchend window, and
 * unconditional force pins. On touch devices (Android WebView) a fling keeps
 * emitting scroll events for hundreds of ms, and force pins (send message,
 * session switch) could yank the view back to the bottom while the user is
 * still scrolling — the "弹回" (snap-back) bug.
 *
 * Follow contract (what the user expects):
 * - While the viewport sits at the bottom, new streamed content follows.
 * - The moment the user scrolls away from the bottom (even a little), follow
 *   stops entirely and never yanks them back — regardless of how much content
 *   arrives, until they scroll back to the bottom.
 * - A "grace band" exists ONLY to cover the case where the viewport is pinned
 *   to the bottom but the DOM hasn't grown there yet (throttled render flush /
 *   async markdown). It never applies once the user has scrolled away.
 *
 * These functions are pure (no Vue/DOM) so the decision logic is unit-testable.
 */

export type ScrollOwner = 'user' | 'programmatic' | 'idle'

export interface ScrollStateInput {
  /** Who owns the scroll viewport right now. */
  owner: ScrollOwner
  /** True while a touch drag is active (touchstart … touchend). */
  userTouching: boolean
  /** Date.now() of the most recent scroll event. */
  lastScrollAt: number
  /** The current time (Date.now()) to compare against lastScrollAt. */
  now: number
  /** scrollHeight - scrollTop - clientHeight of the container, read live. */
  nearBottomDist: number
  /**
   * True when the scroll request comes from a streaming context (new content is
   * arriving — assistant message streaming, placeholder creation, content render
   * flush). The container height lags the incoming tokens, so nearBottomDist may
   * exceed NEAR_BOTTOM_PX even though the viewport IS pinned to the bottom.
   */
  streaming?: boolean
  /**
   * True when the user has scrolled away from the bottom (past NEAR_BOTTOM_PX)
   * during a stream. While set, ALL stream follow is suppressed — a user
   * reading older content must never be yanked back to the bottom. Cleared only
   * when the user scrolls back to the bottom. Force pins (send message, session
   * switch) still override this so intentional content-growth pins work.
   */
  userLeftBottom?: boolean
}

/** Silent window after the last scroll event before we consider scrolling "stopped". */
export const SCROLL_STOP_MS = 250
/** Distance from the bottom (px) that still counts as "at the bottom". */
export const NEAR_BOTTOM_PX = 100
/**
 * When streaming, how far above the bottom the viewport may sit while we still
 * follow. The DOM height lags the streaming token stream (throttled render
 * flush, async markdown), so a freshly created placeholder or a just-flushed
 * block can leave the viewport up to a few hundred px above the true bottom.
 * We follow within this grace band so the user never sees the viewport drift
 * mid-stream — but ONLY while the user has not scrolled away (userLeftBottom).
 */
export const STREAM_FOLLOW_GRACE_PX = 1000

/**
 * Whether the user is currently scrolling or in a fling. True while the touch
 * is held, or while scroll events keep arriving (owner === 'user' and the last
 * event was within SCROLL_STOP_MS). A fling keeps firing scroll events, so the
 * window auto-extends for the whole fling duration.
 */
export function isUserScrolling(s: ScrollStateInput): boolean {
  if (s.userTouching) return true
  return s.owner === 'user' && s.now - s.lastScrollAt < SCROLL_STOP_MS
}

/**
 * Whether a "content grew, pin to bottom" request may execute right now.
 *
 * Decision order:
 * - Never while the user is actively scrolling/flinging — that is what caused
 *   the snap-back. (Force pins are deferred, not applied.)
 * - Follow when the viewport is at/near the bottom (nearBottomDist <=
 *   NEAR_BOTTOM_PX) — this is the "at the bottom → follow" contract.
 * - If the user scrolled away (userLeftBottom), do NOT follow — they are
 *   reading older content and must never be yanked back. Only force pins
 *   (send message / session switch) still pin.
 * - Streaming + user still at/near the bottom (userLeftBottom false): follow
 *   within the grace band — the DOM lags the token stream, so the viewport may
 *   sit a bit above the true bottom even though it IS pinned. This fixes the
 *   probabilistic "assistant message appears but the list doesn't auto-scroll"
 *   bug without ever dragging back a user who scrolled away.
 * - Force pins follow even when the user scrolled away and is stationary.
 * - A programmatic jump (message index) only pins when the target is near the
 *   bottom; a jump into the middle must not be overridden by stream growth.
 */
export function shouldFollowStream(s: ScrollStateInput, force: boolean): boolean {
  if (isUserScrolling(s)) return false
  if (s.nearBottomDist <= NEAR_BOTTOM_PX) return true
  // The user deliberately left the bottom — never yank them back. This gates
  // ALL streaming branches below (grace band included). Only force pins apply.
  if (s.userLeftBottom && !force) return false
  // Streaming + user never left the bottom: the DOM lags the token stream, so
  // follow within the grace band to absorb render-flush height jumps.
  if (s.streaming && s.nearBottomDist <= STREAM_FOLLOW_GRACE_PX) {
    return true
  }
  if (force) return true
  if (s.owner === 'programmatic' && s.nearBottomDist <= NEAR_BOTTOM_PX) return true
  return false
}
