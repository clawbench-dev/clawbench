import { describe, expect, it } from 'vitest'
import { railReadingIndex, type RailReadingGeometry } from '../railReadingPosition'

/**
 * The rail's "you are here" rule. jsdom reports every rect as 0, so these cases
 * can only be pinned with synthetic geometry — an in-component test could not
 * tell "nearest to centre" from "the message under the reading line" apart.
 */
function geometry(over: Partial<RailReadingGeometry> = {}): RailReadingGeometry {
  // Three 100px messages starting at the top of the viewport.
  return {
    tops: [0, 100, 200],
    bottoms: [100, 200, 300],
    readingLine: 90,
    atTop: false,
    atBottom: false,
    ...over,
  }
}

describe('railReadingIndex', () => {
  it('returns null without geometry or rows', () => {
    expect(railReadingIndex(null)).toBeNull()
    expect(railReadingIndex(geometry({ tops: [], bottoms: [] }))).toBeNull()
  })

  it('picks the message whose box contains the reading line', () => {
    expect(railReadingIndex(geometry({ readingLine: 90 }))).toBe(0)
    expect(railReadingIndex(geometry({ readingLine: 150 }))).toBe(1)
    expect(railReadingIndex(geometry({ readingLine: 299 }))).toBe(2)
  })

  it('picks the first message at the top, not whatever the line lands in', () => {
    // The regression this guards: at scrollTop 0 the first bubbles sit above
    // the line, so a pure "line" rule highlighted message 3 of 40.
    expect(railReadingIndex(geometry({ atTop: true, readingLine: 250 }))).toBe(0)
  })

  it('picks the last message at the bottom, even when it sits below the line', () => {
    // A short final reply ends below the reading line; "nearest to the centre"
    // kept the second-to-last node lit at the very bottom of the conversation.
    const g = geometry({
      atBottom: true,
      tops: [0, 100, 200, 300],
      bottoms: [100, 200, 300, 340],
      readingLine: 250,
    })
    expect(railReadingIndex(g)).toBe(3)
  })

  it('falls back to the ends when the line is outside every box', () => {
    expect(railReadingIndex(geometry({ readingLine: -50 }))).toBe(0)
    expect(railReadingIndex(geometry({ readingLine: 900 }))).toBe(2)
  })
})
