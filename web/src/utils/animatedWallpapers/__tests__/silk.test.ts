import { describe, it, expect, vi, beforeEach } from 'vitest'
import { getAnimatedStyle, resolveStyleParams } from '../index'
import type { FrameContext, ParamValue } from '../types'

/**
 * The silk style's draw() is a pure function, so it can be exercised against a
 * stub 2D context even though jsdom has no canvas.
 *
 * What matters here is not the pixels (unverifiable without a real rasteriser)
 * but the two failure modes that are SILENT in a real browser:
 *   - NaN coordinates: canvas discards the whole path, no throw, no warning, so
 *     the style "vanishes" with a clean console.
 *   - a gradient created with a non-finite stop: this one DOES throw and aborts
 *     the frame, which is why draw() must bail when cssW/cssH are 0.
 * Plus the parameter extremes: a 0 or max slider must not produce either.
 */

/** Records the coordinates passed to path/arc calls so NaN can be detected. */
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
    arc: vi.fn((x: number, y: number, r: number) => record(x, y, r)),
    fill: vi.fn(),
    stroke: vi.fn(),
    fillRect: vi.fn((x: number, y: number, w: number, h: number) => record(x, y, w, h)),
    createLinearGradient: vi.fn(() => gradient),
    createRadialGradient: vi.fn(() => gradient),
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
  const style = getAnimatedStyle('silk')
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

describe('silk style draw', () => {
  const style = getAnimatedStyle('silk')

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

  it('draws no stars when the density is zero', () => {
    const { ctx, raw } = makeStubCtx()
    style.draw(frameFor(ctx, { density: 0 }))
    // Stars are the only thing drawn with arc().
    expect(raw.arc).not.toHaveBeenCalled()
  })

  it('draws stars at the default density', () => {
    const { ctx, raw } = makeStubCtx()
    style.draw(frameFor(ctx, {}))
    expect(raw.arc.mock.calls.length).toBeGreaterThan(100)
  })

  it('skips the glow gradients when the glow is zero', () => {
    const { ctx, raw } = makeStubCtx()
    style.draw(frameFor(ctx, { pglow: 0 }))
    expect(raw.createRadialGradient).not.toHaveBeenCalled()
  })

  it('uses additive compositing so overlapping filaments glow', () => {
    // With 'source-over' the last filament would paint over the others and the
    // band would read as a flat smear instead of accumulating silk.
    //
    // The mode is set inside a save()/restore() pair, so the stub only ever sees
    // the 'lighter' assignment — restore() is what returns it to source-over
    // (and the stub cannot model that, hence asserting the bracket instead).
    const { ctx, compositeOps, raw } = makeStubCtx()
    style.draw(frameFor(ctx, {}))
    expect(compositeOps).toContain('lighter')
    expect(raw.save).toHaveBeenCalled()
    expect(raw.restore).toHaveBeenCalled()
  })

  it('survives NaN parameters without emitting NaN coordinates', () => {
    // `resolveStyleParams` normally rejects non-finite values, but draw() must
    // not depend on its caller for that: a NaN here makes canvas silently drop
    // the whole path, so the style "vanishes" with a clean console. Feeding NaN
    // straight in is the only way to actually exercise the guard.
    const { ctx, points } = makeStubCtx()
    const style = getAnimatedStyle('silk')
    const nan: Record<string, ParamValue> = {}
    for (const p of style.params) nan[p.key] = Number.NaN
    expect(() => style.draw({ ctx, cssW: 800, cssH: 600, scale: 1.5, time: 3, speed: 1, params: nan })).not.toThrow()
    expect(points.some((v) => !Number.isFinite(v))).toBe(false)
  })

  it('survives a NaN band parameter while the starfield is still enabled', () => {
    // The all-NaN case above short-circuits: a NaN `density` fails `dens > 0.01`,
    // so the starfield never runs and its own guards are never reached. Here the
    // band geometry is NaN but the stars are on, which is the case where a NaN
    // band path would otherwise hand NaN centres to every follow/cluster star.
    const { ctx, points, raw } = makeStubCtx()
    const style = getAnimatedStyle('silk')
    style.draw({
      ctx, cssW: 800, cssH: 600, scale: 1.5, time: 3, speed: 1,
      params: { ...resolveStyleParams(style, {}), amp: Number.NaN, lam: Number.NaN },
    })
    expect(points.some((v) => !Number.isFinite(v))).toBe(false)
    // The starfield still ran (scatter stars do not depend on the band).
    expect(raw.arc.mock.calls.length).toBeGreaterThan(100)
  })

  it('is deterministic: the same frame twice produces identical geometry', () => {
    const a = makeStubCtx()
    const b = makeStubCtx()
    style.draw(frameFor(a.ctx, {}))
    style.draw(frameFor(b.ctx, {}))
    expect(a.points).toEqual(b.points)
  })
})
