/**
 * Scroll decision functions for the chat message list.
 *
 * Contract (what the user expects):
 * - While the user has NOT deliberately scrolled away, content growth at ANY
 *   time (streaming tokens, throttled render flush, lazy-loaded original text)
 *   pins the view to the bottom.
 * - The user counts as "away" when, at the moment they last touched the
 *   scroll surface, the viewport sat farther than RESUME_FOLLOW_PX from the
 *   bottom. Scrolling back within that band restores follow.
 *
 * ## Why the latch is sampled ONLY on real user input
 *
 * The viewport moves for two reasons that look identical from the scroll
 * offset alone:
 *   1. the user dragged/wheeled it, or
 *   2. the content grew, so the browser moved the offset (scroll anchoring,
 *      `scrollTop` clamping) — possibly by 1px, in either direction.
 *
 * Case 2 must NOT change the latch. Sampling it there is what produced the
 * "stuck mid-conversation" bug: a 1px upward drift during streaming was read
 * as a deliberate scroll away, the latch flipped on while the user was sitting
 * at the very bottom, and every subsequent follow pin was rejected while the
 * content kept growing below the viewport.
 *
 * So the latch is recomputed from the geometry ONLY inside a user-input window
 * (touch drag / wheel / mouse drag). Content-driven scroll events never touch
 * it. This is also why there is no direction test: within a real gesture,
 * distance from the bottom is the whole question — a 1px jitter while the
 * finger rests must not flip the latch, and a genuine 300px drag must.
 *
 * All decision logic is pure (no Vue/DOM) so it is unit-testable.
 */

export interface ScrollStateInput {
  /** True while a touch drag is active (touchstart … touchend). */
  userTouching: boolean
  /** True while a wheel gesture is active (decays SCROLL_STOP_MS after the last wheel event). */
  wheelActive: boolean
  /** True while a mouse button is held on the list (mousedown … mouseup). */
  mouseDownActive: boolean
  /**
   * True when the user last left the viewport farther than RESUME_FOLLOW_PX
   * from the bottom. While set, non-force pins are suppressed — a user reading
   * older content must never be yanked back to the bottom.
   */
  userLeftBottom?: boolean
}

/**
 * How long a wheel gesture keeps counting as "the user is scrolling" after its
 * last event. Wheel has no end event, so the flag decays on this window. It is
 * refreshed ONLY by wheel events — never by scroll events, which the content
 * also produces; refreshing it there would keep it alive forever during a
 * stream and starve every force pin (the "sent message but the reply is never
 * followed" bug).
 */
export const SCROLL_STOP_MS = 250

/**
 * Distance from the bottom (px) below which the user counts as "at the bottom".
 * Used by load-more anchoring; the follow latch uses the tighter
 * RESUME_FOLLOW_PX.
 */
export const NEAR_BOTTOM_PX = 200

/**
 * The single follow threshold. Sampled on user input: within this distance of
 * the bottom, follow is ON; beyond it, follow is OFF.
 *
 * Deliberately small (≈2–3 text lines): it is the only band, so a wide value
 * would mean a user resting mid-list keeps getting yanked to the bottom. The
 * trade-off is that letting go inside the band resumes follow.
 */
export const RESUME_FOLLOW_PX = 50

/**
 * Whether the viewport at the given distance from the bottom counts as
 * "the user has left the bottom". Sampled on user input only — see the module
 * docblock for why content-driven scroll events must not call this.
 */
export function isUserAwayFromBottom(
  distFromBottom: number,
  resumePx: number = RESUME_FOLLOW_PX,
): boolean {
  return distFromBottom > resumePx
}

/**
 * Whether the user is currently driving the scroll surface with their hand.
 * True while a touch is held, a wheel gesture is live, or a mouse button is
 * down. Every flag is event-bounded: touch by touchend/touchcancel, mouse by
 * mouseup, wheel by the decay window above. None of them is refreshed by
 * content-growth scroll events, so this can never latch on permanently.
 */
export function isUserScrolling(s: ScrollStateInput): boolean {
  return s.userTouching || s.wheelActive || s.mouseDownActive
}

/**
 * Whether a "pin to bottom" request may execute right now.
 *
 * - A finger on the screen always wins: never move the viewport out from under
 *   a held touch, not even for a force pin. (Wheel and mouse-drag are NOT
 *   gates — a force pin is an explicit user action and the alternative is
 *   deferring it to a signal that may never arrive.)
 * - A non-force pin is suppressed while the user is away from the bottom; a
 *   force pin (send message / session switch / answer card) overrides that —
 *   the user took an action and expects to see the bottom.
 * - Everything else pins unconditionally: if the user never left, content
 *   growing at any time keeps the view glued to the bottom.
 */
export function shouldPin(s: ScrollStateInput, force: boolean): boolean {
  if (s.userTouching) return false
  if (s.userLeftBottom && !force) return false
  return true
}
