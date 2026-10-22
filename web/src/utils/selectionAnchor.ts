/**
 * Where a text selection sits on screen, as a rect the quote bar can anchor to.
 *
 * Shared by the two selection sources that have their own `getClientRects()`:
 *
 *   - the HOST document (useQuoteQuestion's `selectionAnchorRect`);
 *   - a same-origin preview iframe (iframeSelectionQuote's
 *     `iframeSelectionAnchor`), whose rects are in the FRAME's viewport and get
 *     mapped to the host's afterwards.
 *
 * It lives in its own leaf module precisely because those two must agree. They
 * are mirrors of the same rule, and they did drift: issue #572 fixed the host
 * path to use the UNION of the selection's line boxes while the iframe copy
 * kept the last box, so a multi-line selection in a preview still had the bar
 * placed on top of the text that was just selected. Two copies of a rule is
 * what let that happen; one function is what stops it happening again.
 */

/**
 * The parts of a DOMRect this rule reads.
 *
 * Deliberately the top-left + size form rather than `bottom`/`right`: a real
 * `DOMRect` carries all four, but a plain object built by a caller (or a test
 * double) commonly carries only these, and reading `bottom` off such a box
 * yields `undefined` → `-Infinity` in the union, i.e. a silently broken anchor.
 * Deriving the far edges keeps one definition of "where the box ends".
 */
export interface AnchorBox {
  top: number
  left: number
  width: number
  height: number
}

/** Axis-aligned rect in the same coordinate space as the boxes it came from. */
export interface AnchorUnion {
  top: number
  left: number
  bottom: number
  right: number
}

/**
 * Union of a selection's line boxes, or null when there are none.
 *
 * The union is what makes the bar's placement direction-independent. It used to
 * be the LAST box, on the theory that it is "the point the pointer just left".
 * It is not: `Range.getClientRects()` returns boxes in DOCUMENT order, so the
 * last one is the bottom line no matter which direction the user dragged. The
 * bar prefers to sit ABOVE its anchor, so anchoring on the bottom line put it
 * over the lines above — i.e. over the text the user had just selected. With
 * the union, "above" means above the WHOLE selection.
 *
 * Zero-WIDTH boxes are dropped: a range can end on a caret box at the start of
 * the following line, whose height is a full line. It contributes no width, but
 * its edges would still stretch the union a line down (and its `left` across to
 * column 0).
 */
export function unionRectOfRects(rects: ArrayLike<AnchorBox> | null | undefined): AnchorUnion | null {
  if (!rects || rects.length === 0) return null

  let top = Infinity
  let left = Infinity
  let bottom = -Infinity
  let right = -Infinity
  for (let i = 0; i < rects.length; i++) {
    const r = rects[i]
    if (r.width === 0) continue
    const rBottom = r.top + r.height
    const rRight = r.left + r.width
    if (r.top < top) top = r.top
    if (r.left < left) left = r.left
    if (rBottom > bottom) bottom = rBottom
    if (rRight > right) right = rRight
  }

  // Every box was a caret (a collapsed-looking range): fall back to the raw
  // last box so the caller still gets a usable anchor rather than null.
  if (top === Infinity) {
    const last = rects[rects.length - 1]
    return { top: last.top, left: last.left, bottom: last.top + last.height, right: last.left + last.width }
  }

  return { top, left, bottom, right }
}
