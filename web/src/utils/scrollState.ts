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
}

/** Silent window after the last scroll event before we consider scrolling "stopped". */
export const SCROLL_STOP_MS = 150
/** Distance from the bottom (px) that still counts as "at the bottom". */
export const NEAR_BOTTOM_PX = 100

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
 * - Never while the user is scrolling/flinging — that is what caused the
 *   snap-back. (This applies to force pins too; they only override "user has
 *   scrolled away and is stationary", never "user is actively scrolling".)
 * - Follow when already near the bottom.
 * - force pins follow even when the user scrolled away and is stationary
 *   (send message / session switch need to pin).
 * - A programmatic jump (message index) only pins when the target is near the
 *   bottom; a jump into the middle must not be overridden by stream growth.
 */
export function shouldFollowStream(s: ScrollStateInput, force: boolean): boolean {
  if (isUserScrolling(s)) return false
  if (s.nearBottomDist <= NEAR_BOTTOM_PX) return true
  if (force) return true
  if (s.owner === 'programmatic' && s.nearBottomDist <= NEAR_BOTTOM_PX) return true
  return false
}
