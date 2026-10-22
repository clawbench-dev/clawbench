/**
 * Where a terminal selection sits on screen, for the floating quote bar.
 *
 * xterm owns its selection: `.xterm` carries `user-select: none`, so the host
 * document's `Selection` is always collapsed there and `Range.getClientRects()`
 * reports nothing. The bar therefore cannot measure the selection the way it
 * does for markdown/chat text, and without an explicit anchor it falls back to
 * its pinned position at the top of the page — far away from the text the user
 * just selected (the reported defect).
 *
 * This converts xterm's own buffer-space selection into a rect in
 * `getBoundingClientRect()` space, which is what the bar expects (it converts to
 * pre-zoom `position: fixed` values itself, see selectionToolbarPosition.ts).
 *
 * Coordinate spaces, and why nothing here needs the UI scale:
 *   - `getSelectionPosition()` returns BUFFER cells (0-based, scrollback
 *     included), so the viewport row is `y - viewportY`.
 *   - `dimensions.css.cell` is in PRE-zoom px while `screenRect` is zoom-scaled.
 *     Dividing the rendered grid size by the row/column count yields the cell
 *     size in the SAME space as the rect, so the two never need reconciling.
 */

export interface TerminalSelectionPosition {
  start: { x: number; y: number }
  end: { x: number; y: number }
}

export interface TerminalSelectionAnchorInput {
  /** `term.getSelectionPosition()` — undefined when nothing is selected. */
  position: TerminalSelectionPosition | undefined
  /** `screenElement.getBoundingClientRect()` — the character grid. */
  screenRect: { left: number; top: number; width: number; height: number }
  cols: number
  rows: number
  /** `term.buffer.active.viewportY` — first buffer row currently on screen. */
  viewportY: number
}

function clamp(v: number, min: number, max: number): number {
  return Math.max(min, Math.min(max, v))
}

/**
 * The rect of the selection's LAST line — the bottom line, where the pointer
 * just left — matching the host path (`selectionAnchorRect`). Returns null when
 * there is no selection or the grid has no measurable size.
 */
export function terminalSelectionAnchor(input: TerminalSelectionAnchorInput): DOMRect | null {
  const { position, screenRect, cols, rows, viewportY } = input
  if (!position) return null
  if (cols <= 0 || rows <= 0) return null
  if (screenRect.width <= 0 || screenRect.height <= 0) return null

  const cellW = screenRect.width / cols
  const cellH = screenRect.height / rows
  if (!(cellW > 0) || !(cellH > 0)) return null

  // Clamp into the visible rows: a selection that starts in the scrollback (or
  // extends past the bottom) must still anchor to something on screen rather
  // than dragging the bar off the viewport.
  const startRow = clamp(position.start.y - viewportY, 0, rows - 1)
  const endRow = clamp(position.end.y - viewportY, 0, rows - 1)
  const startCol = clamp(position.start.x, 0, cols)
  const endCol = clamp(position.end.x, 0, cols)

  // Single line: the line box spans the selected columns. Multi-line: the last
  // line starts at column 0 and ends at the selection's end column.
  const singleLine = startRow === endRow
  const leftCol = singleLine ? Math.min(startCol, endCol) : 0
  const rightCol = singleLine ? Math.max(startCol, endCol) : endCol

  const left = screenRect.left + leftCol * cellW
  const top = screenRect.top + endRow * cellH
  // Never zero-width: a collapsed end column would give the bar nothing to
  // centre on, and `computeSelectionToolbarStyle` centres on the rect's midpoint.
  const width = Math.max(cellW, (rightCol - leftCol) * cellW)

  return new DOMRect(left, top, width, cellH)
}
