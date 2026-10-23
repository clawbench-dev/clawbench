import { describe, it, expect } from 'vitest'
import { terminalSelectionAnchor } from '../terminalSelectionAnchor'

/**
 * A 10×4 grid rendered at (left=100, top=50) with 8×20 px cells.
 *
 * The numbers are chosen so a wrong cell size or a missing viewport offset is
 * visible: cell width 8, cell height 20, and a viewport scrolled to row 100.
 */
const GRID = {
  screenRect: { left: 100, top: 50, width: 80, height: 80 },
  cols: 10,
  rows: 4,
  viewportY: 100,
}

describe('terminalSelectionAnchor', () => {
  it('anchors to the last line of a multi-line selection', () => {
    // Buffer rows 101..102 → viewport rows 1..2. The bar must sit on row 2 (the
    // bottom line, where the pointer left), not on the first row.
    const rect = terminalSelectionAnchor({
      ...GRID,
      position: { start: { x: 2, y: 101 }, end: { x: 5, y: 102 } },
    })

    expect(rect).not.toBeNull()
    expect(rect!.top).toBe(50 + 2 * 20)
    expect(rect!.height).toBe(20)
    // Multi-line: the last line runs from column 0 to the end column.
    expect(rect!.left).toBe(100)
    expect(rect!.width).toBe(5 * 8)
  })

  it('spans only the selected columns on a single line', () => {
    const rect = terminalSelectionAnchor({
      ...GRID,
      position: { start: { x: 3, y: 101 }, end: { x: 7, y: 101 } },
    })

    expect(rect!.top).toBe(50 + 1 * 20)
    expect(rect!.left).toBe(100 + 3 * 8)
    expect(rect!.width).toBe(4 * 8)
  })

  it('normalizes a selection dragged backwards (end left of start)', () => {
    // xterm reports the drag's direction verbatim; the rect must still be the
    // left-to-right span, or the bar would be centred outside the text.
    const forward = terminalSelectionAnchor({
      ...GRID,
      position: { start: { x: 3, y: 101 }, end: { x: 7, y: 101 } },
    })
    const backward = terminalSelectionAnchor({
      ...GRID,
      position: { start: { x: 7, y: 101 }, end: { x: 3, y: 101 } },
    })

    expect(backward!.left).toBe(forward!.left)
    expect(backward!.width).toBe(forward!.width)
  })

  it('clamps a selection scrolled into the scrollback to the visible grid', () => {
    // Rows 10..11 are far above the viewport (viewportY=100): without clamping
    // the rect would be hundreds of pixels above the terminal, dragging the bar
    // off-screen.
    const rect = terminalSelectionAnchor({
      ...GRID,
      position: { start: { x: 0, y: 10 }, end: { x: 4, y: 11 } },
    })

    expect(rect!.top).toBe(50)
    expect(rect!.top + rect!.height).toBeLessThanOrEqual(50 + GRID.screenRect.height)
  })

  it('keeps a non-zero width when the selection ends at the anchor column', () => {
    // `select()` is exclusive of the end cell, so a one-cell selection can end
    // at the same column it starts. A zero-width rect would leave the bar
    // nothing to centre on.
    const rect = terminalSelectionAnchor({
      ...GRID,
      position: { start: { x: 4, y: 101 }, end: { x: 4, y: 101 } },
    })

    expect(rect!.width).toBe(8)
  })

  it('returns null without a selection', () => {
    expect(terminalSelectionAnchor({ ...GRID, position: undefined })).toBeNull()
  })

  it('returns null when the grid has no measured size', () => {
    // The terminal can be mounted while hidden (a background tab), where the
    // rect is all zeros — dividing by it would produce NaN positions.
    expect(terminalSelectionAnchor({
      ...GRID,
      screenRect: { left: 0, top: 0, width: 0, height: 0 },
      position: { start: { x: 0, y: 100 }, end: { x: 3, y: 100 } },
    })).toBeNull()
  })
})
