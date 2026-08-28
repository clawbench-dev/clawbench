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
  /**
   * True when the scroll request comes from a streaming context (new content is
   * arriving — assistant message streaming, placeholder creation, content render
   * flush). The container height lags the incoming tokens, so nearBottomDist may
   * exceed NEAR_BOTTOM_PX even though the viewport IS pinned to the bottom. When
   * streaming, follow as long as the gap is within what the incoming content
   * could plausibly grow (see STREAM_FOLLOW_GRACE_PX), instead of requiring the
   * viewport to already sit at the bottom.
   */
  streaming?: boolean
  /**
   * Timestamp (Date.now()) when the current stream-follow window began. A
   * stream-follow window opens when a streaming context starts (assistant
   * placeholder created, stream connected) and re-opens when the user scrolls
   * back to the bottom during streaming.
   *
   * Rationale: a single throttled render flush can grow scrollHeight far beyond
   * STREAM_FOLLOW_GRACE_PX in one frame (e.g. a burst of tokens arriving at
   * once). Judging follow by a static distance alone would then permanently
   * lose the bottom (scrollTop lags further with every subsequent flush).
   * Within this window, a stationary user is assumed to be following the stream,
   * so follow regardless of the gap — until they actively scroll away.
   */
  streamStartAt?: number
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
 * mid-stream, while a genuinely scrolled-away user (gap far beyond the band)
 * is still left alone.
 */
export const STREAM_FOLLOW_GRACE_PX = 1000

/**
 * How long after a stream-follow window opens (or re-opens via a scroll-back
 * to the bottom) we follow without a distance limit. A throttled render flush
 * (ContentBlocks.vue) can grow scrollHeight by far more than the grace band in
 * a single frame — long bursts, code blocks, slow markdown. The window must
 * outlive the longest plausible flush pause. 2s covers a slow network / heavy
 * render while still abandoning follow shortly after the user stops scrolling.
 */
export const STREAM_FOLLOW_LATENCY_MS = 2000

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
 * - Streaming context (streaming=true) follows within STREAM_FOLLOW_GRACE_PX —
 *   the DOM height lags the token stream, so the viewport may sit slightly
 *   above the bottom while content is still arriving.
 * - force pins follow even when the user scrolled away and is stationary
 *   (send message / session switch need to pin).
 * - A programmatic jump (message index) only pins when the target is near the
 *   bottom; a jump into the middle must not be overridden by stream growth.
 */
export function shouldFollowStream(s: ScrollStateInput, force: boolean): boolean {
  if (isUserScrolling(s)) return false
  if (s.nearBottomDist <= NEAR_BOTTOM_PX) return true
  // Streaming context: content is still arriving, so the container height lags
  // the token stream. Follow within the grace band — the viewport is pinned to
  // the bottom even though the DOM hasn't grown there yet. This fixes the
  // probabilistic "assistant message appears but the list doesn't auto-scroll"
  // bug caused by requiring nearBottomDist <= NEAR_BOTTOM_PX at the exact
  // moment the placeholder/block is created. A user who scrolled far away
  // (gap beyond the band) is still left alone.
  if (s.streaming && s.nearBottomDist <= STREAM_FOLLOW_GRACE_PX) {
    return true
  }
  // Time-windowed follow: within a fresh stream-follow window a stationary
  // user is assumed to be following — a single throttled render flush can grow
  // scrollHeight beyond STREAM_FOLLOW_GRACE_PX in one frame, and the static
  // distance check above would then permanently lose the bottom (every later
  // flush re-reads a gap that never shrinks back below the band). The window
  // is re-opened whenever the user scrolls back to the bottom during streaming,
  // so follow resumes the moment they return. This ONLY applies to a stationary
  // user — isUserScrolling() already rejected active scrolls above.
  if (s.streaming && s.streamStartAt !== undefined && s.now - s.streamStartAt < STREAM_FOLLOW_LATENCY_MS) {
    return true
  }
  if (force) return true
  if (s.owner === 'programmatic' && s.nearBottomDist <= NEAR_BOTTOM_PX) return true
  return false
}
