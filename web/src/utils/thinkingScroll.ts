/**
 * Auto-follow for the deep-thinking inline content box while the AI is
 * streaming. The box is a fixed-height scrollable (`max-height` + `overflow-y`)
 * container; each streaming render batch rewrites its innerHTML, and the browser
 * preserves the old `scrollTop` across the rewrite — so new lines accumulate
 * below the viewport unless we re-pin the box to the bottom.
 *
 * The follow rule mirrors the outer chat list (scrollState.ts), simplified for
 * a single small element: the box follows the live stream while the user has
 * not deliberately scrolled away. "Away" is decided purely by distance from the
 * bottom, sampled only on real user input.
 *
 * The same reasoning as the outer list applies to why there is no direction
 * test: content growth moves `scrollTop` by a pixel in either direction, and
 * reading that as "the user scrolled up" would latch follow off while the user
 * sits at the bottom. Distance from the bottom is the only question that
 * distinguishes "reading earlier reasoning" from "at the bottom".
 *
 * All decision logic is pure (no Vue/DOM) so it is unit-testable.
 */

/** Unlock band width: how close to the bottom the user must be to keep following. */
export const RESUME_FOLLOW_PX = 50

/**
 * Whether the box at the given distance from the bottom counts as "the user
 * has left the bottom". Sampled on user input only — content-driven scroll
 * events must not call this.
 */
export function isThinkingUserAwayFromBottom(
  distFromBottom: number,
  resumePx: number = RESUME_FOLLOW_PX,
): boolean {
  return distFromBottom > resumePx
}
