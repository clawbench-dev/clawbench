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
 * Minimum distance from the top (px) at which older history starts loading.
 *
 * Loading exactly AT the top means the user always reaches the edge, sees the
 * list stop, waits for the request, and then gets prepended content — the page
 * feels like it "hangs" at the top. Prefetching a bit early hides the round
 * trip behind the scroll: by the time the reader arrives, the older messages
 * are already in the DOM.
 */
export const LOAD_MORE_AHEAD_PX = 400

/**
 * Whether the viewport is close enough to the top that older history should be
 * fetched now. The band is at least LOAD_MORE_AHEAD_PX, and grows to one
 * viewport height on tall panes — a fixed 400px is barely a third of a desktop
 * chat pane, so the prefetch would still finish after the user got there.
 *
 * Deliberately a pure function: the two call sites (manual scroll and the
 * programmatic scroll-to-top) must use the same rule, and the decision is
 * otherwise untestable without faking a whole layout.
 */
export function shouldLoadOlderHistory(scrollTop: number, clientHeight: number): boolean {
  return scrollTop <= Math.max(LOAD_MORE_AHEAD_PX, clientHeight)
}

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
 * How long the "still streaming" pill lingers after the turn ends while the
 * reader is still up in the history. It hands over to the direction FABs on
 * the same 3s beat they use; vanishing on the exact frame the turn finished
 * reads as "the UI broke", not as "the reply is done".
 */
export const STREAM_PILL_FAREWELL_MS = 3000

export interface StreamPillInput {
  /** The viewport is farther than NEAR_BOTTOM_PX from the newest line. */
  awayFromBottom: boolean
  /** A turn is in flight — the reply at the bottom is still growing. */
  streaming: boolean
  /** Inside the farewell window (see STREAM_PILL_FAREWELL_MS). */
  farewell: boolean
}

/**
 * Whether the streaming pill belongs on screen.
 *
 * It is a STATUS, not a navigation shortcut: while a reply is being written
 * below the fold the reader needs to know content is still arriving, and the
 * direction FABs are deliberately transient (they disappear 3s after the last
 * scroll). Hence the pill stays for the whole turn, and only the farewell
 * window extends its life past the end of the turn.
 *
 * `awayFromBottom` is the same threshold the FABs use — two separate cut-offs
 * would let one appear without the other at the same scroll position.
 */
export function shouldShowStreamPill(s: StreamPillInput): boolean {
  if (!s.awayFromBottom) return false
  return s.streaming || s.farewell
}

/**
 * Whether a "pin to bottom" request may execute right now.
 *
 * - A finger on the screen always wins: never move the viewport out from under
 *   a held touch, not even for a force pin.
 * - A live wheel gesture or scrollbar drag also wins for NON-force pins. Both
 *   move the viewport over several frames (WebKit animates a wheel notch), and
 *   a pin written in between cancels the gesture mid-flight — the viewport
 *   snaps back to the bottom before the gesture has moved far enough to trip
 *   the "left the bottom" latch, so every notch bounces. Force pins still run:
 *   they are explicit user actions (send / answer card / session switch) and
 *   must not be deferred to a signal that may never arrive.
 * - A non-force pin is suppressed while the user is away from the bottom; a
 *   force pin overrides that — the user took an action and expects the bottom.
 * - Everything else pins unconditionally: if the user never left, content
 *   growing at any time keeps the view glued to the bottom.
 */
export function shouldPin(s: ScrollStateInput, force: boolean): boolean {
  if (s.userTouching) return false
  if (!force && (s.wheelActive || s.mouseDownActive)) return false
  if (s.userLeftBottom && !force) return false
  return true
}
