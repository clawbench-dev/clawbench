import { describe, it, expect } from 'vitest'
import { unionRectOfRects } from '../selectionAnchor.ts'

/**
 * The one geometry rule both selection sources use: the host document
 * (useQuoteQuestion) and a preview iframe (iframeSelectionQuote).
 *
 * It is pinned here rather than only through its two consumers because those
 * two are what drifted: the host path was fixed to use the union while the
 * iframe copy kept the last box, so a multi-line selection in a preview still
 * had the quote bar placed on top of the text that had just been selected
 * (issue #572). A single implementation plus this test is what makes that
 * impossible to reintroduce by editing one side.
 */
describe('unionRectOfRects', () => {
  it('unions every line box', () => {
    expect(unionRectOfRects([
      { top: 10, left: 20, width: 200, height: 18 },
      { top: 40, left: 20, width: 120, height: 18 },
    ])).toEqual({ top: 10, left: 20, bottom: 58, right: 220 })
  })

  it('is direction-independent (document order does not matter)', () => {
    // A drag can go bottom-up; `getClientRects()` still reports document order,
    // which is exactly why "the last box" was the wrong choice.
    const forward = unionRectOfRects([
      { top: 10, left: 20, width: 200, height: 18 },
      { top: 40, left: 20, width: 120, height: 18 },
    ])
    const reversed = unionRectOfRects([
      { top: 40, left: 20, width: 120, height: 18 },
      { top: 10, left: 20, width: 200, height: 18 },
    ])
    expect(reversed).toEqual(forward)
  })

  it('drops zero-width caret boxes', () => {
    // A range can end on a caret at the start of the following line: full line
    // height, no width. Including it would stretch the union a line down and pull
    // `left` across to column 0.
    expect(unionRectOfRects([
      { top: 10, left: 20, width: 200, height: 18 },
      { top: 28, left: 0, width: 0, height: 18 },
    ])).toEqual({ top: 10, left: 20, bottom: 28, right: 220 })
  })

  it('falls back to the last box when every box is a caret', () => {
    // A collapsed-looking range still needs SOME anchor; null would send the bar
    // to its pinned top position.
    expect(unionRectOfRects([
      { top: 10, left: 20, width: 0, height: 18 },
      { top: 40, left: 30, width: 0, height: 18 },
    ])).toEqual({ top: 40, left: 30, bottom: 58, right: 30 })
  })

  it('returns null for no boxes at all', () => {
    expect(unionRectOfRects([])).toBeNull()
    expect(unionRectOfRects(null)).toBeNull()
    expect(unionRectOfRects(undefined)).toBeNull()
  })
})
