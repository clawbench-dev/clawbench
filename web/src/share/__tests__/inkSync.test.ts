import { describe, expect, it } from 'vitest'
import { INK_SYNC_FRAME_CHARS, parseControlMessage, type InkStroke } from '../liveControl'
import { inkSyncFrame } from '../inkSync'
import { MAX_POINTS_PER_STROKE } from '../useInkBoard'

function stroke(n: number, points = 2): InkStroke {
  return {
    color: '#ff3b30',
    width: 3,
    points: Array.from({ length: points }, (_, i) => ({ x: (i + 1) / 100, y: n / 100 })),
  }
}

describe('inkSyncFrame', () => {
  it('carries nothing when the board is empty', () => {
    // An empty snapshot has no meaning: the receiver can only be non-empty if
    // it saw strokes the publisher has since dropped.
    expect(inkSyncFrame([])).toBeNull()
  })

  it('replays the board in drawing order', () => {
    const frame = inkSyncFrame([stroke(1), stroke(2)])
    expect(frame?.t).toBe('ink-sync')
    expect(frame?.t === 'ink-sync' && frame.strokes).toHaveLength(2)
    // The board draws them in order, so the replay must keep it.
    expect(frame?.t === 'ink-sync' && frame.strokes[0].points[0]).toEqual({ x: 0.01, y: 0.01 })
    expect(frame?.t === 'ink-sync' && frame.strokes[1].points[0]).toEqual({ x: 0.01, y: 0.02 })
  })

  it('rounds coordinates the receiver cannot see anyway', () => {
    const frame = inkSyncFrame([
      { color: '#fff', width: 2, points: [{ x: 0.123456789, y: 0.987654321 }] },
    ])
    expect(frame?.t === 'ink-sync' && frame.strokes[0].points[0]).toEqual({ x: 0.1235, y: 0.9877 })
  })

  it('keeps the newest strokes when the board is too large for one frame', () => {
    const many = Array.from({ length: 600 }, (_, i) => stroke(i, 40))
    const frame = inkSyncFrame(many)
    expect(frame?.t).toBe('ink-sync')
    const strokes = frame?.t === 'ink-sync' ? frame.strokes : []
    // Recent annotations are what the presenter is pointing at; the oldest are
    // a long meeting's history.
    expect(strokes.length).toBeLessThan(many.length)
    expect(strokes.at(-1)).toEqual(many.at(-1))
    // Still drawing order, not the reverse the loop walks in.
    const ys = strokes.map((s) => s.points[0].y)
    expect(ys).toEqual([...ys].sort((a, b) => a - b))
  })

  it('never builds a frame the receiver would reject for size', () => {
    // One stroke that alone overflows: a fixed stroke count would produce a
    // frame past the parser's 64 KiB ceiling and it would be dropped silently.
    const huge = stroke(1, MAX_POINTS_PER_STROKE)
    const frame = inkSyncFrame([stroke(0), huge])
    expect(frame).not.toBeNull()
    const wire = JSON.stringify(frame)
    expect(wire.length).toBeLessThan(64 * 1024)
    expect(wire.length).toBeLessThanOrEqual(INK_SYNC_FRAME_CHARS + 64)
  })

  it('keeps the pen tip of a stroke that alone overflows', () => {
    const huge = stroke(1, MAX_POINTS_PER_STROKE)
    const frame = inkSyncFrame([huge])
    const strokes = frame?.t === 'ink-sync' ? frame.strokes : []
    expect(strokes).toHaveLength(1)
    expect(strokes[0].points.length).toBeLessThan(MAX_POINTS_PER_STROKE)
    // The end of the line is the part the presenter just drew.
    expect(strokes[0].points.at(-1)).toEqual(huge.points.at(-1))
  })

  it('produces a frame the viewer parser accepts unchanged', () => {
    const frame = inkSyncFrame([stroke(1), stroke(2, 5)])
    const parsed = parseControlMessage(JSON.stringify(frame))
    expect(parsed?.t).toBe('ink-sync')
    expect(parsed?.t === 'ink-sync' && parsed.strokes).toHaveLength(2)
    expect(parsed?.t === 'ink-sync' && parsed.strokes[1].points).toHaveLength(5)
  })
})
