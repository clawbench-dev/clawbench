import { describe, expect, it } from 'vitest'
import { isUserScrolling, shouldFollowStream, SCROLL_STOP_MS, NEAR_BOTTOM_PX, type ScrollStateInput } from '../scrollState'

function baseInput(overrides: Partial<ScrollStateInput> = {}): ScrollStateInput {
  return {
    owner: 'idle',
    userTouching: false,
    lastScrollAt: 0,
    now: 0,
    nearBottomDist: 500,
    ...overrides,
  }
}

describe('isUserScrolling', () => {
  it('returns true while the user is touching/dragging', () => {
    expect(isUserScrolling(baseInput({ userTouching: true }))).toBe(true)
  })

  it('returns true when owner=user and last scroll event was within the stop window', () => {
    const now = 10000
    expect(isUserScrolling(baseInput({ owner: 'user', lastScrollAt: now - 100, now }))).toBe(true)
    // Fling keeps firing scroll events — window auto-extends
    expect(isUserScrolling(baseInput({ owner: 'user', lastScrollAt: now - 149, now }))).toBe(true)
  })

  it('returns false when the last scroll event is older than the stop window', () => {
    const now = 10000
    expect(isUserScrolling(baseInput({ owner: 'user', lastScrollAt: now - SCROLL_STOP_MS - 1, now }))).toBe(false)
  })

  it('returns false when owner is not user (idle/programmatic) and no touch', () => {
    const now = 10000
    expect(isUserScrolling(baseInput({ owner: 'idle', lastScrollAt: now - 10, now }))).toBe(false)
    expect(isUserScrolling(baseInput({ owner: 'programmatic', lastScrollAt: now - 10, now }))).toBe(false)
  })

  it('returns true for userTouching regardless of owner (touch takes priority)', () => {
    expect(isUserScrolling(baseInput({ owner: 'programmatic', userTouching: true }))).toBe(true)
    expect(isUserScrolling(baseInput({ owner: 'idle', userTouching: true }))).toBe(true)
  })

  it('boundary: exactly SCROLL_STOP_MS since the last scroll is NOT scrolling (strict <)', () => {
    const now = 10000
    expect(isUserScrolling(baseInput({ owner: 'user', lastScrollAt: now - SCROLL_STOP_MS, now }))).toBe(false)
  })
})

describe('shouldFollowStream', () => {
  it('returns false while the user is scrolling even with force=true (force never overrides active scrolling)', () => {
    const now = 10000
    const input = baseInput({ owner: 'user', userTouching: false, lastScrollAt: now - 50, now, nearBottomDist: 0 })
    expect(shouldFollowStream(input, true)).toBe(false)
    // Touch held — the root cause of the snap-back bug
    expect(shouldFollowStream(baseInput({ userTouching: true, nearBottomDist: 0 }), true)).toBe(false)
  })

  it('returns true when stationary and already near the bottom', () => {
    expect(shouldFollowStream(baseInput({ nearBottomDist: NEAR_BOTTOM_PX }), false)).toBe(true)
    expect(shouldFollowStream(baseInput({ nearBottomDist: 0 }), false)).toBe(true)
  })

  it('returns false when stationary but scrolled away (no force)', () => {
    expect(shouldFollowStream(baseInput({ nearBottomDist: 500 }), false)).toBe(false)
  })

  it('returns true for force pins when stationary even if scrolled away', () => {
    expect(shouldFollowStream(baseInput({ nearBottomDist: 500 }), true)).toBe(true)
  })

  it('returns false for programmatic owner when jumped to the middle', () => {
    expect(shouldFollowStream(baseInput({ owner: 'programmatic', nearBottomDist: 500 }), false)).toBe(false)
  })

  it('returns true for programmatic owner when target is near the bottom', () => {
    expect(shouldFollowStream(baseInput({ owner: 'programmatic', nearBottomDist: 50 }), false)).toBe(true)
  })

  it('boundary: exactly NEAR_BOTTOM_PX follows; NEAR_BOTTOM_PX+1 does not (non-force)', () => {
    expect(shouldFollowStream(baseInput({ nearBottomDist: NEAR_BOTTOM_PX }), false)).toBe(true)
    expect(shouldFollowStream(baseInput({ nearBottomDist: NEAR_BOTTOM_PX + 1 }), false)).toBe(false)
  })

  it('returns true for programmatic owner at exactly NEAR_BOTTOM_PX, false just beyond', () => {
    expect(shouldFollowStream(baseInput({ owner: 'programmatic', nearBottomDist: NEAR_BOTTOM_PX }), false)).toBe(true)
    expect(shouldFollowStream(baseInput({ owner: 'programmatic', nearBottomDist: NEAR_BOTTOM_PX + 1 }), false)).toBe(false)
  })

  it('force + userTouching never follows even when near the bottom', () => {
    expect(shouldFollowStream(baseInput({ userTouching: true, nearBottomDist: 0 }), true)).toBe(false)
  })
})
