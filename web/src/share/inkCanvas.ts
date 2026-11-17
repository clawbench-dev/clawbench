/**
 * Draw the meeting's annotation strokes onto a 2D canvas.
 *
 * The publisher draws in a separate window (or a desktop overlay), so the
 * captured picture never contains the ink: anything that produces a still or a
 * video from that picture has to composite the strokes itself. Key frames and
 * screen recordings both come through here, which is what keeps a frame and a
 * recording of the same moment identical.
 *
 * Strokes are normalised to the picture (0..1), so the same numbers land in the
 * same place at any canvas size. Width follows the same `width / 800` scale the
 * annotation layer uses; the strokes are already in device pixels, so DPR is
 * deliberately not applied again.
 */
import type { InkStroke } from './liveControl'

export function drawInkStrokes(
  ctx: CanvasRenderingContext2D,
  strokes: readonly InkStroke[],
  width: number,
  height: number,
): void {
  if (strokes.length === 0) return
  const scale = Math.max(1, width / 800)
  for (const stroke of strokes) {
    if (stroke.points.length === 0) continue
    ctx.beginPath()
    ctx.lineCap = 'round'
    ctx.lineJoin = 'round'
    ctx.strokeStyle = stroke.color
    ctx.lineWidth = Math.max(1, stroke.width * scale)
    const [first, ...rest] = stroke.points
    ctx.moveTo(first.x * width, first.y * height)
    if (rest.length === 0) {
      // A tap is a dot: a zero-length line would draw nothing.
      ctx.lineTo(first.x * width + 0.01, first.y * height)
    } else {
      for (const point of rest) ctx.lineTo(point.x * width, point.y * height)
    }
    ctx.stroke()
  }
}
