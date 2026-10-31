/**
 * Which conversation-index row the reader is currently looking at.
 *
 * The chat-area rail is a "you are here" marker, so its highlight must name the
 * message under the reader's eye — not merely the message closest to the
 * geometric centre of the viewport. Those two disagree exactly where the cue
 * matters most:
 *
 *   - At the very top the first bubbles are scrolled past the centre line, so
 *     "nearest to centre" lands on message 3 of 40 while the reader is reading
 *     message 1.
 *   - At the very bottom a short final reply sits below the centre line, so
 *     "nearest to centre" keeps the SECOND-TO-LAST node lit while the reader is
 *     looking at the last one.
 *
 * The rule is therefore: at either end of the scroll the answer is unambiguous
 * (first / last); in between it is the message whose box contains the reading
 * line (a third of the way down the viewport).
 *
 * Kept as a pure function so the geometry can be unit-tested with synthetic
 * rects — jsdom reports every rect as 0, so an in-component test could not tell
 * any of these cases apart.
 */
export interface RailReadingGeometry {
  /** Viewport-relative top edge of each message, in DOM order. */
  tops: number[]
  /** Viewport-relative bottom edge of each message, in DOM order. */
  bottoms: number[]
  /** Viewport-relative y of the line the reader's eye sits on. */
  readingLine: number
  /** The scroller is at (or within a pixel of) its top. */
  atTop: boolean
  /** The scroller is at (or within a pixel of) its bottom. */
  atBottom: boolean
}

/** Index of the message the reader is looking at, or null when there is none. */
export function railReadingIndex(geometry: RailReadingGeometry | null): number | null {
  if (!geometry || geometry.tops.length === 0) return null
  const last = geometry.tops.length - 1
  // Ends first: at either edge the reader is unambiguously on that message, and
  // the reading line can still fall inside a neighbour (a short last bubble).
  if (geometry.atTop) return 0
  if (geometry.atBottom) return last
  for (let i = 0; i <= last; i++) {
    if (geometry.bottoms[i] > geometry.readingLine) return i
  }
  return last
}
