import { describe, expect, it } from 'vitest'

/**
 * Tests for the auto-follow scroll logic in TaskExecDetail.vue.
 *
 * During live streaming output, the content area should stay pinned to the
 * bottom (like the chat UI). If the user scrolls up, auto-follow pauses;
 * scrolling back near the bottom resumes it.
 */

const NEAR_BOTTOM_THRESHOLD = 100

/** Pure-function equivalent of handleScroll's isAtBottom decision */
function isNearBottom(scrollHeight: number, scrollTop: number, clientHeight: number): boolean {
  const distFromBottom = scrollHeight - scrollTop - clientHeight
  return distFromBottom < NEAR_BOTTOM_THRESHOLD
}

/** Pure-function equivalent of scrollToBottom's guard: only scroll when at bottom */
function shouldAutoScroll(isAtBottom: boolean, userTouching: boolean): boolean {
  return isAtBottom && !userTouching
}

describe('TaskExecDetail auto-follow scroll', () => {
  describe('isNearBottom', () => {
    it('returns true when within threshold of the bottom', () => {
      expect(isNearBottom(1000, 910, 90)).toBe(true) // 0px from bottom
      expect(isNearBottom(1000, 815, 90)).toBe(true) // 95px from bottom (< 100)
    })

    it('returns false when scrolled away from the bottom', () => {
      expect(isNearBottom(1000, 700, 90)).toBe(false) // 210px from bottom
      expect(isNearBottom(1000, 0, 90)).toBe(false)   // at top
    })
  })

  describe('shouldAutoScroll', () => {
    it('scrolls when at bottom and not touching', () => {
      expect(shouldAutoScroll(true, false)).toBe(true)
    })

    it('does not scroll when user scrolled away from bottom', () => {
      expect(shouldAutoScroll(false, false)).toBe(false)
    })

    it('does not scroll while user is touching/dragging', () => {
      expect(shouldAutoScroll(true, true)).toBe(false)
    })
  })
})
