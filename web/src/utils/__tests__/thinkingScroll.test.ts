import { describe, expect, it } from 'vitest'
import {
  initialThinkingScrollState,
  shouldFollowThinking,
  updateThinkingUserLeftBottom,
  RESUME_FOLLOW_PX,
} from '../thinkingScroll'

describe('updateThinkingUserLeftBottom', () => {
  it('any upward drag latches follow off, regardless of distance from the bottom', () => {
    expect(updateThinkingUserLeftBottom(false, { scrollingUp: true, distFromBottom: 0 })).toBe(true)
    expect(updateThinkingUserLeftBottom(false, { scrollingUp: true, distFromBottom: 120 })).toBe(true)
    expect(updateThinkingUserLeftBottom(false, { scrollingUp: true, distFromBottom: 900 })).toBe(true)
  })

  it('an upward drag keeps the latch locked when already locked', () => {
    expect(updateThinkingUserLeftBottom(true, { scrollingUp: true, distFromBottom: 700 })).toBe(true)
    expect(updateThinkingUserLeftBottom(true, { scrollingUp: true, distFromBottom: 10 })).toBe(true)
  })

  it('scrolling back within the resume band unlocks follow', () => {
    expect(updateThinkingUserLeftBottom(true, { scrollingUp: false, distFromBottom: 0 })).toBe(false)
    expect(updateThinkingUserLeftBottom(true, { scrollingUp: false, distFromBottom: RESUME_FOLLOW_PX })).toBe(false)
  })

  it('resting above the resume band but not scrolled up stays locked when already locked', () => {
    // Mirrors the chat-list guard: a non-upward scroll that is NOT back at the
    // bottom must not silently resume following (would yank a reading user).
    expect(updateThinkingUserLeftBottom(true, { scrollingUp: false, distFromBottom: 150 })).toBe(true)
  })

  it('downward scrolls elsewhere leave the latch unchanged', () => {
    expect(updateThinkingUserLeftBottom(false, { scrollingUp: false, distFromBottom: 500 })).toBe(false)
    expect(updateThinkingUserLeftBottom(true, { scrollingUp: false, distFromBottom: 500 })).toBe(true)
  })
})

describe('shouldFollowThinking', () => {
  it('follows while the user has not scrolled up', () => {
    expect(shouldFollowThinking(initialThinkingScrollState())).toBe(true)
    expect(shouldFollowThinking({ userLeftBottom: false })).toBe(true)
  })

  it('never follows once the user scrolled up to read earlier reasoning', () => {
    expect(shouldFollowThinking({ userLeftBottom: true })).toBe(false)
  })
})
