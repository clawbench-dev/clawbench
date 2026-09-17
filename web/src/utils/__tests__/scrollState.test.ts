import { describe, expect, it } from 'vitest'
import { isUserScrolling, shouldPin, isUserAwayFromBottom, SCROLL_STOP_MS, RESUME_FOLLOW_PX, NEAR_BOTTOM_PX, type ScrollStateInput } from '../scrollState'

function baseInput(overrides: Partial<ScrollStateInput> = {}): ScrollStateInput {
  return {
    userTouching: false,
    wheelActive: false,
    mouseDownActive: false,
    ...overrides,
  }
}

describe('isUserAwayFromBottom', () => {
  it('is false at or inside the resume band', () => {
    expect(isUserAwayFromBottom(0)).toBe(false)
    expect(isUserAwayFromBottom(RESUME_FOLLOW_PX)).toBe(false)
  })

  it('is true only beyond the band', () => {
    expect(isUserAwayFromBottom(RESUME_FOLLOW_PX + 1)).toBe(true)
    expect(isUserAwayFromBottom(500)).toBe(true)
  })

  it('has no direction input — a 1px offset at the bottom is not "away"', () => {
    // The regression this guards: a layout nudge moves scrollTop by ~1px while
    // the user sits at the very bottom. Direction-based latching read that as
    // "scrolled up" and killed follow for the rest of the turn.
    expect(isUserAwayFromBottom(1)).toBe(false)
    expect(isUserAwayFromBottom(-0)).toBe(false)
  })

  it('a custom resumePx overrides the default band', () => {
    expect(isUserAwayFromBottom(40, 50)).toBe(false)
    expect(isUserAwayFromBottom(60, 50)).toBe(true)
  })
})

describe('isUserScrolling', () => {
  it('is true while a touch is held', () => {
    expect(isUserScrolling(baseInput({ userTouching: true }))).toBe(true)
  })

  it('is true during a live wheel gesture and while the mouse is down', () => {
    expect(isUserScrolling(baseInput({ wheelActive: true }))).toBe(true)
    expect(isUserScrolling(baseInput({ mouseDownActive: true }))).toBe(true)
  })

  it('is false when no input flag is set', () => {
    expect(isUserScrolling(baseInput({}))).toBe(false)
  })

  it('cannot be latched on by content-driven scroll events', () => {
    // Every flag is event-bounded; there is no timestamp/owner channel that a
    // stream's continuous scroll events could keep refreshing. This is what
    // previously left the flag true for an entire turn and starved every pin.
    const s = baseInput({})
    expect(isUserScrolling(s)).toBe(false)
    expect(isUserScrolling({ ...s })).toBe(false)
  })
})

describe('shouldPin', () => {
  it('a held touch blocks even a force pin (hand always wins)', () => {
    expect(shouldPin(baseInput({ userTouching: true }), true)).toBe(false)
    expect(shouldPin(baseInput({ userTouching: true }), false)).toBe(false)
  })

  it('wheel/mouse input does NOT block a force pin', () => {
    // Regression guard: a force pin (send / answer card) must not be deferred
    // behind a wheel or mouse-drag flag — the deferral depended on a "scroll
    // stopped" signal that a streaming turn never emits, so the pin was lost.
    expect(shouldPin(baseInput({ wheelActive: true }), true)).toBe(true)
    expect(shouldPin(baseInput({ mouseDownActive: true }), true)).toBe(true)
  })

  it('pins unconditionally when the user never left the bottom', () => {
    expect(shouldPin(baseInput({}), false)).toBe(true)
    expect(shouldPin(baseInput({ userLeftBottom: false }), false)).toBe(true)
  })

  it('a non-force pin is suppressed while the user is away', () => {
    expect(shouldPin(baseInput({ userLeftBottom: true }), false)).toBe(false)
  })

  it('a force pin overrides the away latch', () => {
    expect(shouldPin(baseInput({ userLeftBottom: true }), true)).toBe(true)
  })
})

describe('constants', () => {
  it('keeps the resume band tighter than the near-bottom band', () => {
    expect(RESUME_FOLLOW_PX).toBeLessThan(NEAR_BOTTOM_PX)
  })

  it('uses a short wheel-decay window', () => {
    expect(SCROLL_STOP_MS).toBeGreaterThan(0)
    expect(SCROLL_STOP_MS).toBeLessThanOrEqual(500)
  })
})
