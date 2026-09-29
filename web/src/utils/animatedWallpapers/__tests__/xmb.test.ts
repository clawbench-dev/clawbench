import { describe, it, expect, vi, beforeEach } from 'vitest'
import { getAnimatedStyle, resolveStyleParams } from '../index'
import type { FrameContext, ParamValue } from '../types'

/**
 * The XMB style's draw() is a pure function, so it can be exercised against a
 * stub 2D context even though jsdom has no canvas.
 *
 * This is the mirror of `silk.test.ts`. The XMB style was extracted verbatim
 * from the old `WaveBackground.vue`, whose test only covered the component
 * lifecycle — the drawing itself was never asserted. Extracting it into a pure
 * `draw()` makes it testable, and the failure modes it must be held to are the
 * same silent ones silk guards:
 *   - NaN coordinates: canvas discards the whole path, no throw, no warning, so
 *     the wave "vanishes" with a clean console.
 *   - a gradient created with a non-finite stop: this one DOES throw and aborts
 *     the frame, which is why draw() must bail when cssW/cssH are 0.
 * Plus the parameter extremes: a 0 or max slider must not produce either.
 */

/** Records the coordinates passed to path/fill calls so NaN can be detected. */
function makeStubCtx() {
  const points: number[] = []
  const compositeOps: string[] = []
  let composite = 'source-over'
  const record = (...nums: number[]) => { points.push(...nums) }
  const gradient = {
    addColorStop: vi.fn((offset: number, colour: string) => {
      // createLinearGradient throws on a non-finite stop in real browsers, so a
      // NaN offset here is a genuine defect rather than a stub artefact.
      if (!Number.isFinite(offset)) throw new Error(`non-finite gradient stop: ${offset}`)
      if (typeof colour !== 'string' || colour.includes('NaN')) throw new Error(`bad colour: ${colour}`)
    }),
  }
  const ctx = {
    canvas: { width: 800, height: 600 },
    setTransform: vi.fn(),
    save: vi.fn(),
    restore: vi.fn(),
    beginPath: vi.fn(),
    closePath: vi.fn(),
    moveTo: vi.fn((x: number, y: number) => record(x, y)),
    lineTo: vi.fn((x: number, y: number) => record(x, y)),
    fill: vi.fn(),
    stroke: vi.fn(),
    fillRect: vi.fn((x: number, y: number, w: number, h: number) => record(x, y, w, h)),
    createLinearGradient: vi.fn(() => gradient),
    get globalCompositeOperation() { return composite },
    set globalCompositeOperation(v: string) { composite = v; compositeOps.push(v) },
    fillStyle: '',
    strokeStyle: '',
    lineWidth: 1,
    lineJoin: 'round',
    lineCap: 'round',
  }
  return { ctx: ctx as unknown as CanvasRenderingContext2D, points, compositeOps, raw: ctx }
}

function frameFor(ctx: CanvasRenderingContext2D, params: Record<string, ParamValue>, over: Partial<FrameContext> = {}): FrameContext {
  const style = getAnimatedStyle('xmb')
  return {
    ctx,
    cssW: 800,
    cssH: 600,
    scale: 1.5,
    time: 3,
    speed: 1,
    params: resolveStyleParams(style, params),
    ...over,
  }
}

// getComputedStyle returns real values in jsdom, but the theme variables are
// absent; the palette falls back to gruvbox. Stub it to keep the test hermetic.
beforeEach(() => {
  vi.stubGlobal('getComputedStyle', vi.fn(() => ({
    getPropertyValue: (name: string) =>
      name === '--accent-color' ? '#4678f5' : name === '--bg-primary' ? '#0a1e6e' : '',
  })))
})

describe('xmb style draw', () => {
  const style = getAnimatedStyle('xmb')

  it('paints a frame without throwing', () => {
    const { ctx } = makeStubCtx()
    expect(() => style.draw(frameFor(ctx, {}))).not.toThrow()
  })

  it('never emits NaN coordinates', () => {
    // The load-bearing assertion: canvas silently drops a NaN path.
    const { ctx, points } = makeStubCtx()
    style.draw(frameFor(ctx, {}))
    expect(points.length).toBeGreaterThan(0)
    expect(points.every(Number.isFinite)).toBe(true)
  })

  it('bails out on a zero-width canvas instead of throwing', () => {
    // cssW = 0 makes u = x/cssW NaN, and createLinearGradient throws on a
    // non-finite stop, which would abort the whole frame.
    const { ctx, raw } = makeStubCtx()
    expect(() => style.draw(frameFor(ctx, {}, { cssW: 0 }))).not.toThrow()
    expect(raw.createLinearGradient).not.toHaveBeenCalled()
  })

  it('bails out on a zero-height canvas', () => {
    const { ctx, raw } = makeStubCtx()
    expect(() => style.draw(frameFor(ctx, {}, { cssH: 0 }))).not.toThrow()
    expect(raw.createLinearGradient).not.toHaveBeenCalled()
  })

  it('survives every parameter at its minimum', () => {
    const { ctx, points } = makeStubCtx()
    const min: Record<string, number> = {}
    for (const p of style.params) if (p.kind === 'slider') min[p.key] = p.min
    expect(() => style.draw(frameFor(ctx, min))).not.toThrow()
    expect(points.every(Number.isFinite)).toBe(true)
  })

  it('survives every parameter at its maximum', () => {
    const { ctx, points } = makeStubCtx()
    const max: Record<string, number> = {}
    for (const p of style.params) if (p.kind === 'slider') max[p.key] = p.max
    expect(() => style.draw(frameFor(ctx, max))).not.toThrow()
    expect(points.every(Number.isFinite)).toBe(true)
  })

  it('draws one crest stroke per band at the default edge setting', () => {
    // The crispness comes from the crest stroke, not from blur — losing it
    // would silently flatten the wave into three soft smears.
    const { ctx, raw } = makeStubCtx()
    style.draw(frameFor(ctx, {}))
    expect(raw.stroke.mock.calls.length).toBe(3)
  })

  it('skips the crest stroke when edge is zero', () => {
    const { ctx, raw } = makeStubCtx()
    style.draw(frameFor(ctx, { edge: 0 }))
    expect(raw.stroke).not.toHaveBeenCalled()
  })

  it('applies the edge fade as a destination-in mask by default', () => {
    // `fadeEdges` defaults to true; the fade is what keeps the bands from
    // hitting the screen edges as hard vertical cuts.
    const { ctx, compositeOps } = makeStubCtx()
    style.draw(frameFor(ctx, {}))
    expect(compositeOps).toContain('destination-in')
  })

  it('does not apply the edge fade when the switch is off', () => {
    const { ctx, compositeOps } = makeStubCtx()
    style.draw(frameFor(ctx, { fadeEdges: false }))
    expect(compositeOps).not.toContain('destination-in')
  })

  it('leaves compositing back at source-over after the fade', () => {
    // The mask runs last; a leaked `destination-in` would erase every later
    // frame drawn on the same context.
    const { ctx, raw } = makeStubCtx()
    style.draw(frameFor(ctx, {}))
    expect(raw.globalCompositeOperation).toBe('source-over')
  })

  it('survives NaN shape parameters without emitting NaN coordinates', () => {
    // `resolveStyleParams` normally rejects non-finite values, but draw() must
    // not depend on its caller for that: a NaN here makes canvas silently drop
    // the whole path, so the wave "vanishes" with a clean console. Feeding NaN
    // straight in is the only way to actually exercise the per-layer guard.
    //
    // `contrast` is deliberately kept finite: it scales a *colour*, and a NaN
    // colour is a different (silent, non-throwing) failure than a NaN path.
    const { ctx, points } = makeStubCtx()
    const style = getAnimatedStyle('xmb')
    const params = { ...resolveStyleParams(style, {}), lam: Number.NaN, amp: Number.NaN, irr: Number.NaN, tilt: Number.NaN, band: Number.NaN }
    expect(() => style.draw({ ctx, cssW: 800, cssH: 600, scale: 1.5, time: 3, speed: 1, params })).not.toThrow()
    expect(points.some((v) => !Number.isFinite(v))).toBe(false)
  })

  it('skips the band fill when its centreline is NaN but still paints the stage', () => {
    // The guard is per-layer (`continue`), not a whole-frame bail: one bad
    // layer must not take the background gradient down with it.
    const { ctx, raw } = makeStubCtx()
    const style = getAnimatedStyle('xmb')
    const params = { ...resolveStyleParams(style, {}), lam: Number.NaN }
    style.draw({ ctx, cssW: 800, cssH: 600, scale: 1.5, time: 3, speed: 1, params })
    // Stage fillRect + the edge-fade fillRect still ran.
    expect(raw.fillRect.mock.calls.length).toBeGreaterThanOrEqual(2)
    // No band path was filled, so no crest stroke either.
    expect(raw.stroke).not.toHaveBeenCalled()
  })

  it('is deterministic: the same frame twice produces identical geometry', () => {
    const a = makeStubCtx()
    const b = makeStubCtx()
    style.draw(frameFor(a.ctx, {}))
    style.draw(frameFor(b.ctx, {}))
    expect(a.points).toEqual(b.points)
  })
})
