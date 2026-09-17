import { describe, expect, it } from 'vitest'
import { isThinkingUserAwayFromBottom, RESUME_FOLLOW_PX } from '../thinkingScroll'

describe('isThinkingUserAwayFromBottom', () => {
  it('is false at or inside the resume band', () => {
    expect(isThinkingUserAwayFromBottom(0)).toBe(false)
    expect(isThinkingUserAwayFromBottom(RESUME_FOLLOW_PX)).toBe(false)
  })

  it('is true only beyond the band', () => {
    expect(isThinkingUserAwayFromBottom(RESUME_FOLLOW_PX + 1)).toBe(true)
    expect(isThinkingUserAwayFromBottom(900)).toBe(true)
  })

  it('does not latch on a 1px layout nudge while resting at the bottom', () => {
    // Same regression as the outer chat list: the box is rewritten on every
    // streaming batch, the browser keeps the old scrollTop, and a small
    // downward-then-upward correction must not read as "user scrolled up".
    expect(isThinkingUserAwayFromBottom(1)).toBe(false)
    expect(isThinkingUserAwayFromBottom(-0)).toBe(false)
  })

  it('has no direction input at all', () => {
    expect(isThinkingUserAwayFromBottom.length).toBeLessThanOrEqual(2)
  })

  it('a custom resumePx overrides the default band', () => {
    expect(isThinkingUserAwayFromBottom(10, 50)).toBe(false)
    expect(isThinkingUserAwayFromBottom(700, 50)).toBe(true)
  })
})
