import { afterEach, describe, expect, it, vi } from 'vitest'
import { LASER_IDLE_MS, useInkBoard } from '../useInkBoard'

describe('useInkBoard', () => {
  afterEach(() => {
    vi.useRealTimers()
  })

  it('records a local stroke from begin to end', () => {
    const board = useInkBoard()
    board.beginStroke({ x: 0.1, y: 0.2 }, '#ff3b30', 3)
    board.extendStroke({ x: 0.2, y: 0.3 })
    board.endStroke()

    expect(board.strokes.value).toHaveLength(1)
    expect(board.strokes.value[0].points).toEqual([
      { x: 0.1, y: 0.2 },
      { x: 0.2, y: 0.3 },
    ])
  })

  it('builds a remote stroke from start/move/end frames', () => {
    const board = useInkBoard()
    board.apply({ t: 'ink', phase: 'start', x: 0, y: 0, color: '#fff', width: 2 })
    board.apply({ t: 'ink', phase: 'move', x: 0.5, y: 0.5, color: '#fff', width: 2 })
    board.apply({ t: 'ink', phase: 'end', x: 0, y: 0, color: '#fff', width: 2 })

    expect(board.strokes.value).toHaveLength(1)
    expect(board.strokes.value[0].points).toHaveLength(2)
  })

  it('starts a stroke when a move arrives without a start', () => {
    const board = useInkBoard()
    // A dropped first frame must not leave a hole in the middle of the line.
    board.apply({ t: 'ink', phase: 'move', x: 0.4, y: 0.4, color: '#fff', width: 2 })

    expect(board.strokes.value).toHaveLength(1)
    expect(board.strokes.value[0].points).toHaveLength(1)
  })

  it('undoes and clears', () => {
    const board = useInkBoard()
    board.apply({ t: 'ink', phase: 'start', x: 0, y: 0, color: '#fff', width: 2 })
    board.apply({ t: 'ink', phase: 'end', x: 0, y: 0, color: '#fff', width: 2 })
    board.apply({ t: 'ink', phase: 'start', x: 0.5, y: 0.5, color: '#fff', width: 2 })
    board.apply({ t: 'ink', phase: 'end', x: 0, y: 0, color: '#fff', width: 2 })
    expect(board.strokes.value).toHaveLength(2)

    board.apply({ t: 'ink-undo' })
    expect(board.strokes.value).toHaveLength(1)

    board.apply({ t: 'ink-clear' })
    expect(board.strokes.value).toHaveLength(0)
  })

  it('shows the laser and hides it after the idle window', () => {
    vi.useFakeTimers()
    const board = useInkBoard()

    board.apply({ t: 'laser', x: 0.3, y: 0.7, on: true })
    expect(board.laser.value).toEqual({ x: 0.3, y: 0.7, on: true })

    // A frozen pointer after the presenter stopped moving reads as a hung
    // picture, so it fades on its own.
    vi.advanceTimersByTime(LASER_IDLE_MS + 10)
    expect(board.laser.value?.on).toBe(false)

    board.apply({ t: 'laser', x: 0.3, y: 0.7, on: false })
    expect(board.laser.value?.on).toBe(false)
    board.dispose()
  })

  it('ignores frames it does not own', () => {
    const board = useInkBoard()
    expect(board.apply({ t: 'chat', id: '1', from: 'v', name: 'v', text: 'hi', at: 0 })).toBe(false)
    expect(board.strokes.value).toHaveLength(0)
  })
})
