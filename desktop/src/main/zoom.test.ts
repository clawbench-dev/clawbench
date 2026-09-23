import { describe, it, expect, vi } from 'vitest'
import {
  MIN_ZOOM,
  MAX_ZOOM,
  ZOOM_STEP,
  clampZoomFactor,
  nextZoomFactor,
  applyZoomFactor,
} from './zoom'

/**
 * The zoom arithmetic is deliberately trivial, so these tests are about the
 * contracts that are easy to get wrong and invisible in the UI:
 *
 *  - the range must match the in-app "界面缩放" setting, or the two controls
 *    would disagree about what the smallest/largest view is;
 *  - a step must be exactly reversible, or repeated in/out drifts;
 *  - out-of-range and non-finite input must be absorbed, because
 *    `setZoomFactor` throws on a factor that is not > 0.
 */

describe('zoom range', () => {
  it('matches the in-app UI-scale clamp', () => {
    // applyUIScale() clamps to [0.5, 2]; a different range here would make the
    // native zoom and the settings slider disagree at the extremes.
    expect(MIN_ZOOM).toBe(0.5)
    expect(MAX_ZOOM).toBe(2)
  })

  it('steps by a factor greater than 1', () => {
    expect(ZOOM_STEP).toBeGreaterThan(1)
  })
})

describe('clampZoomFactor', () => {
  it('passes through factors inside the range', () => {
    expect(clampZoomFactor(1)).toBe(1)
    expect(clampZoomFactor(1.331)).toBeCloseTo(1.331, 10)
  })

  it('clamps to the bounds', () => {
    expect(clampZoomFactor(0.1)).toBe(MIN_ZOOM)
    expect(clampZoomFactor(10)).toBe(MAX_ZOOM)
    expect(clampZoomFactor(0.5)).toBe(MIN_ZOOM)
    expect(clampZoomFactor(2)).toBe(MAX_ZOOM)
  })

  it('falls back to 1 for non-finite input', () => {
    // NaN/Infinity reaching setZoomFactor throws, so they must not propagate.
    // The fallback is neutral (1) rather than a bound: a non-finite factor is
    // a corrupt read, and recovering to 100% is less surprising than jumping
    // to whichever extreme the sign happened to point at.
    expect(clampZoomFactor(NaN)).toBe(1)
    expect(clampZoomFactor(Infinity)).toBe(1)
    expect(clampZoomFactor(-Infinity)).toBe(1)
  })

  it('clamps a zero or negative factor up to the minimum', () => {
    // setZoomFactor requires > 0; 0 must not survive as 0.
    expect(clampZoomFactor(0)).toBe(MIN_ZOOM)
    expect(clampZoomFactor(-2)).toBe(MIN_ZOOM)
  })
})

describe('nextZoomFactor', () => {
  it('zooms in and out by one step', () => {
    expect(nextZoomFactor(1, 'in')).toBeCloseTo(ZOOM_STEP, 10)
    expect(nextZoomFactor(1, 'out')).toBeCloseTo(1 / ZOOM_STEP, 10)
  })

  it('returns to the starting factor after an in/out round trip', () => {
    // The reason the step is geometric rather than a fixed increment: a fixed
    // +0.1/-0.1 also round-trips, but a fixed multiplier is what makes the
    // gesture feel uniform. Guard the reversibility either way.
    const start = 1.21
    const roundTripped = nextZoomFactor(nextZoomFactor(start, 'in'), 'out')
    expect(roundTripped).toBeCloseTo(start, 10)
  })

  it('resets to 100% from anywhere', () => {
    expect(nextZoomFactor(1.6105, 'reset')).toBe(1)
    expect(nextZoomFactor(0.6209, 'reset')).toBe(1)
    // Reset is absolute — it must not be clamped by MIN/MAX in a way that
    // makes it a no-op from an out-of-range value.
    expect(nextZoomFactor(5, 'reset')).toBe(1)
  })

  it('clamps at the top instead of exceeding MAX', () => {
    let f = 1
    for (let i = 0; i < 40; i++) f = nextZoomFactor(f, 'in')
    expect(f).toBe(MAX_ZOOM)
  })

  it('clamps at the bottom instead of reaching zero', () => {
    // A zoom-out loop must asymptote to MIN, never to 0 (which would throw).
    let f = 1
    for (let i = 0; i < 40; i++) f = nextZoomFactor(f, 'out')
    expect(f).toBe(MIN_ZOOM)
    expect(f).toBeGreaterThan(0)
  })

  it('treats an unusable current factor as 100%', () => {
    // getZoomFactor() is read back from the webContents; if it ever came back
    // as 0/NaN the next step should recover rather than poison the value.
    expect(nextZoomFactor(0, 'in')).toBeCloseTo(ZOOM_STEP, 10)
    expect(nextZoomFactor(NaN, 'in')).toBeCloseTo(ZOOM_STEP, 10)
    expect(nextZoomFactor(-1, 'out')).toBeCloseTo(1 / ZOOM_STEP, 10)
  })
})

describe('applyZoomFactor', () => {
  /** Minimal stand-in for a BrowserWindow + its webContents. */
  function fakeWindow() {
    const setZoomFactor = vi.fn()
    const win = { webContents: { getZoomFactor: () => 1, setZoomFactor } }
    return { win, setZoomFactor }
  }

  it('writes the factor to webContents, not the window', () => {
    // Regression guard: setZoomFactor lives on webContents. Applying it to the
    // BrowserWindow itself is a runtime TypeError, and the two are easy to
    // confuse at an IPC boundary where the argument is a whole window.
    const { win, setZoomFactor } = fakeWindow()
    applyZoomFactor(win, 1.5)
    expect(setZoomFactor).toHaveBeenCalledWith(1.5)
  })

  it('clamps before writing, so a caller cannot escape the range', () => {
    // Both the wheel/keyboard handler and the renderer's auto-scale IPC go
    // through here; clamping at the shared boundary keeps them consistent.
    const { win, setZoomFactor } = fakeWindow()
    applyZoomFactor(win, 10)
    expect(setZoomFactor).toHaveBeenLastCalledWith(MAX_ZOOM)
    applyZoomFactor(win, 0.01)
    expect(setZoomFactor).toHaveBeenLastCalledWith(MIN_ZOOM)
  })

  it('neutralises a non-finite factor instead of throwing', () => {
    const { win, setZoomFactor } = fakeWindow()
    applyZoomFactor(win, NaN)
    expect(setZoomFactor).toHaveBeenCalledWith(1)
  })

  it('is a no-op with no window', () => {
    // The renderer can ask for a zoom before any window exists (or after the
    // last one closed); that must not throw.
    expect(() => applyZoomFactor(null, 1.5)).not.toThrow()
  })
})
