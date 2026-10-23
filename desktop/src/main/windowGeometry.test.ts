import { describe, it, expect } from 'vitest'
import {
  DEFAULT_WINDOW_BOUNDS,
  MIN_VISIBLE_PX,
  MIN_WINDOW_HEIGHT,
  MIN_WINDOW_WIDTH,
  boundsToPersist,
  isValidBounds,
  resolveStartupBounds,
  type DisplayLike,
  type WindowBounds,
} from './windowGeometry'

const PRIMARY: DisplayLike = {
  bounds: { x: 0, y: 0, width: 1920, height: 1080 },
  workArea: { x: 0, y: 0, width: 1920, height: 1040 },
}

/** A second monitor to the right of the primary. */
const SECOND: DisplayLike = {
  bounds: { x: 1920, y: 0, width: 1920, height: 1080 },
  workArea: { x: 1920, y: 0, width: 1920, height: 1040 },
}

function rect(x: number, y: number, width = 1280, height = 800): WindowBounds {
  return { x, y, width, height }
}

describe('isValidBounds', () => {
  it('accepts a normal rect and rejects non-objects', () => {
    expect(isValidBounds(rect(10, 10))).toBe(true)
    expect(isValidBounds(null)).toBe(false)
    expect(isValidBounds(undefined)).toBe(false)
    expect(isValidBounds('1280x800')).toBe(false)
  })

  it('rejects non-finite coordinates', () => {
    expect(isValidBounds({ x: NaN, y: 0, width: 1280, height: 800 })).toBe(false)
    expect(isValidBounds({ x: 0, y: Infinity, width: 1280, height: 800 })).toBe(false)
  })

  it('rejects rects smaller than the minimum window size', () => {
    expect(isValidBounds({ x: 0, y: 0, width: MIN_WINDOW_WIDTH - 1, height: 800 })).toBe(false)
    expect(isValidBounds({ x: 0, y: 0, width: 1280, height: MIN_WINDOW_HEIGHT - 1 })).toBe(false)
    // Exactly at the floor is valid — the boundary must not be off by one.
    expect(isValidBounds({ x: 0, y: 0, width: MIN_WINDOW_WIDTH, height: MIN_WINDOW_HEIGHT })).toBe(true)
  })
})

describe('resolveStartupBounds', () => {
  it('restores a saved rect that is still on a connected display', () => {
    const saved = rect(100, 120, 1000, 700)
    expect(resolveStartupBounds(saved, [PRIMARY], PRIMARY)).toEqual(saved)
  })

  it('restores a saved rect on a secondary display', () => {
    const saved = rect(2000, 100, 1000, 700)
    expect(resolveStartupBounds(saved, [PRIMARY, SECOND], PRIMARY)).toEqual(saved)
  })

  it('falls back to a centred default when the saved monitor is gone', () => {
    // Saved on the second monitor, which is no longer connected. Restoring it
    // verbatim would put the window entirely off-screen with no way to reach it.
    const saved = rect(2000, 100, 1000, 700)
    const result = resolveStartupBounds(saved, [PRIMARY], PRIMARY)
    expect(result).not.toEqual(saved)
    expect(result).toEqual({
      x: Math.round((1920 - 1280) / 2),
      y: Math.round((1040 - 800) / 2),
      width: 1280,
      height: 800,
    })
  })

  it('centres within the work area, not the full display', () => {
    // Work area is 40px shorter (taskbar), so the vertical centre must shift.
    const result = resolveStartupBounds(null, [PRIMARY], PRIMARY)
    expect(result.y).toBe(Math.round((1040 - 800) / 2))
    expect(result.y).not.toBe(Math.round((1080 - 800) / 2))
  })

  it('keeps a partially off-screen rect when enough of it is visible', () => {
    // Hanging off the right edge by 200px, but still draggable.
    const saved = rect(1920 - 200, 100, 1000, 700)
    expect(resolveStartupBounds(saved, [PRIMARY], PRIMARY)).toEqual(saved)
  })

  it('rejects a rect with less than the minimum visible sliver', () => {
    // Only MIN_VISIBLE_PX - 1 px inside the right edge.
    const saved = rect(1920 - (MIN_VISIBLE_PX - 1), 100, 1000, 700)
    expect(resolveStartupBounds(saved, [PRIMARY], PRIMARY)).not.toEqual(saved)
  })

  it('shrinks the default to fit a small work area', () => {
    const small: DisplayLike = {
      bounds: { x: 0, y: 0, width: 1024, height: 600 },
      workArea: { x: 0, y: 0, width: 1024, height: 560 },
    }
    const result = resolveStartupBounds(null, [small], small)
    expect(result.width).toBe(1024)
    expect(result.height).toBe(560)
    expect(result.x).toBe(0)
    expect(result.y).toBe(0)
  })

  it('returns the bare default when there is no display information', () => {
    expect(resolveStartupBounds(null, [], null)).toEqual(DEFAULT_WINDOW_BOUNDS)
    expect(resolveStartupBounds(rect(10, 10), [], null)).toEqual(DEFAULT_WINDOW_BOUNDS)
  })

  it('does not hand back the caller\'s object', () => {
    // resolveStartupBounds returns the saved rect directly when it is usable;
    // assert it is a copy so a later mutation by the caller cannot corrupt the
    // store's in-memory defaults.
    const saved = rect(100, 120, 1000, 700)
    const result = resolveStartupBounds(saved, [PRIMARY], PRIMARY)
    result.x = 999
    expect(saved.x).toBe(100)
  })
})

describe('boundsToPersist', () => {
  it('persists the restored rect while maximized', () => {
    // The whole point: saving the maximized rect would make un-maximize after a
    // restart restore to a screen-sized window.
    const normal = rect(100, 120, 1000, 700)
    expect(boundsToPersist({ isMaximized: true, isFullScreen: false, normalBounds: normal })).toEqual(normal)
  })

  it('persists nothing while fullscreen', () => {
    expect(
      boundsToPersist({ isMaximized: false, isFullScreen: true, normalBounds: rect(100, 120) }),
    ).toBeNull()
  })

  it('persists the normal rect when neither maximized nor fullscreen', () => {
    const normal = rect(100, 120, 1000, 700)
    expect(boundsToPersist({ isMaximized: false, isFullScreen: false, normalBounds: normal })).toEqual(normal)
  })

  it('rejects a degenerate restored rect', () => {
    expect(
      boundsToPersist({ isMaximized: false, isFullScreen: false, normalBounds: rect(0, 0, 10, 10) }),
    ).toBeNull()
  })

  it('returns a copy, not the caller\'s object', () => {
    const normal = rect(100, 120, 1000, 700)
    const result = boundsToPersist({ isMaximized: false, isFullScreen: false, normalBounds: normal })
    expect(result).not.toBe(normal)
  })
})
