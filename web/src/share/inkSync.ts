/**
 * Catching a (re)joining viewer up on the ink already on the board.
 *
 * Strokes only ever travel as live frames, so a viewer that joins after the
 * presenter has drawn starts with an empty overlay: the ink it missed was sent
 * before it was on the channel. The publisher therefore hands over its board
 * when the viewer's control channel opens.
 *
 * One frame, not a stream of fake `ink` frames: the receiver replaces its board
 * wholesale, so a half-delivered replay can never leave a line that stops in
 * the middle. The frame is bounded to fit the control channel — a snapshot the
 * receiver would reject for size is the same as no snapshot at all.
 *
 * Kept free of Vue so the framing rules can be tested without a peer.
 */
import { INK_SYNC_FRAME_CHARS, type ControlMessage, type InkStroke } from './liveControl'

/**
 * Coordinates are rounded to 1/10000 of the picture.
 *
 * On a 4K picture that is still finer than a pixel, while full float precision
 * would double or triple the size of a snapshot for nothing anybody can see.
 */
function roundCoord(n: number): number {
  return Math.round(n * 1e4) / 1e4
}

/**
 * The strokes to replay, newest first, cut to fit one frame.
 *
 * Newest first on purpose: the annotations still on the presenter's screen are
 * the recent ones, and a board that has grown past the budget is a long
 * meeting's worth of history, not something anybody is pointing at. A single
 * stroke that alone overflows keeps its newest points (the pen tip), so the
 * frame stays legal instead of being dropped by the receiver.
 */
export function inkSyncFrame(strokes: readonly InkStroke[]): ControlMessage | null {
  if (strokes.length === 0) return null

  const kept: InkStroke[] = []
  // `[]` — every element adds one comma on top of its own JSON.
  let chars = 2

  for (let i = strokes.length - 1; i >= 0; i--) {
    const stroke = strokes[i]
    const points = stroke.points.map((p) => ({ x: roundCoord(p.x), y: roundCoord(p.y) }))
    let size = JSON.stringify({ color: stroke.color, width: stroke.width, points: [] }).length + 1
    const fitting: Array<{ x: number; y: number }> = []
    for (let j = points.length - 1; j >= 0; j--) {
      const pointChars = JSON.stringify(points[j]).length + 1
      if (chars + size + pointChars > INK_SYNC_FRAME_CHARS) break
      fitting.push(points[j])
      size += pointChars
    }
    // Nothing of this stroke fits: it is the newest one left, so no older
    // stroke can fit either.
    if (fitting.length === 0) break
    kept.push({ color: stroke.color, width: stroke.width, points: fitting.reverse() })
    chars += size
  }

  if (kept.length === 0) return null
  return { t: 'ink-sync', strokes: kept.reverse() }
}
