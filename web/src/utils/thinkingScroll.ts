/**
 * Auto-follow for the deep-thinking inline content box while the AI is
 * streaming. The box is a fixed-height scrollable (`max-height` + `overflow-y`)
 * container; each streaming render batch rewrites its innerHTML, and the browser
 * preserves the old `scrollTop` across the rewrite — so new lines accumulate
 * below the viewport unless we re-pin the box to the bottom.
 *
 * The follow rules mirror the outer chat list (scrollState.ts), simplified for
 * a single small element:
 *  - While the user has NOT scrolled up to read earlier reasoning, every content
 *    update pins the box to the bottom (auto-follow the live stream).
 *  - Any upward drag latches "left the bottom". While latched the box is never
 *    yanked down — the user is reading past reasoning.
 *  - Scrolling back to the bottom (within RESUME_FOLLOW_PX) unlocks follow.
 *
 * All decision logic is pure (no Vue/DOM) so it is unit-testable.
 */

/** Unlock band width: how close to the bottom the user must return before follow resumes. */
export const RESUME_FOLLOW_PX = 50

export interface ThinkingScrollState {
  /** True when the user has scrolled up to read earlier content. */
  userLeftBottom: boolean
}

export function initialThinkingScrollState(): ThinkingScrollState {
  return { userLeftBottom: false }
}

/**
 * Update the "user left the bottom" latch from a user scroll event on the
 * thinking content box. Same contract as the outer chat list: any upward
 * movement immediately locks follow off; scrolling back within the unlock band
 * restores it; anything else leaves the latch unchanged.
 */
export function updateThinkingUserLeftBottom(
  current: boolean,
  args: {
    /** True when the scroll moved toward the top (scrollTop decreased). */
    scrollingUp: boolean
    /** Distance from the bottom of the box (scrollHeight - scrollTop - clientHeight). */
    distFromBottom: number
    /** Unlock band width; defaults to RESUME_FOLLOW_PX. */
    resumePx?: number
  },
): boolean {
  if (args.scrollingUp) return true
  if (args.distFromBottom <= (args.resumePx ?? RESUME_FOLLOW_PX)) return false
  return current
}

/**
 * Whether a content update should re-pin the thinking box to the bottom.
 * The box follows only when the user has not deliberately scrolled up to read
 * earlier reasoning — the same "never yank a reading user" rule as the chat.
 */
export function shouldFollowThinking(s: ThinkingScrollState): boolean {
  return !s.userLeftBottom
}
