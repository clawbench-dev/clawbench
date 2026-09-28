import { describe, it, expect } from 'vitest'
import {
  ANIMATED_STYLES,
  DEFAULT_ANIMATED_STYLE,
  getAnimatedStyle,
  isKnownAnimatedStyle,
  resolveStyleParams,
} from '../index'
import type { SliderSpec } from '../types'

/**
 * The style registry contract.
 *
 * A malformed spec is silent at runtime — a duplicate key silently overwrites a
 * sibling's value, an out-of-range default makes the reset button produce a value
 * the slider cannot represent. So the invariants are asserted here rather than
 * discovered by a user.
 */
describe('animated wallpaper registry', () => {
  it('registers at least the two shipped styles', () => {
    expect(ANIMATED_STYLES.length).toBeGreaterThanOrEqual(2)
    expect(ANIMATED_STYLES.map((s) => s.id)).toEqual(expect.arrayContaining(['xmb', 'silk']))
  })

  it('has a unique id per style', () => {
    const ids = ANIMATED_STYLES.map((s) => s.id)
    expect(new Set(ids).size).toBe(ids.length)
  })

  it('names the default style', () => {
    expect(isKnownAnimatedStyle(DEFAULT_ANIMATED_STYLE)).toBe(true)
  })

  it('gives every style a label key and a draw function', () => {
    for (const s of ANIMATED_STYLES) {
      expect(s.labelKey, `${s.id} labelKey`).toMatch(/^settings\.items\./)
      expect(typeof s.draw, `${s.id} draw`).toBe('function')
    }
  })

  it('gives every style a valid speed range', () => {
    for (const s of ANIMATED_STYLES) {
      const [lo, hi] = s.speedRange
      expect(lo, `${s.id} speedRange low`).toBeGreaterThan(0)
      expect(hi, `${s.id} speedRange high`).toBeGreaterThan(lo)
    }
  })

  it('keeps the shipped XMB speed range stable', () => {
    // 0.2–2.0 matches what the wave shipped with. Changing it would change the
    // feel of an existing wallpaper, so pin it.
    expect(getAnimatedStyle('xmb').speedRange).toEqual([0.2, 2.0])
  })

  it('uses the prototype speed range for silk', () => {
    expect(getAnimatedStyle('silk').speedRange).toEqual([0.25, 2.5])
  })

  describe('param specs', () => {
    it('has a unique key per param within a style', () => {
      for (const s of ANIMATED_STYLES) {
        const keys = s.params.map((p) => p.key)
        expect(new Set(keys).size, `${s.id} param keys`).toBe(keys.length)
      }
    })

    it('gives every param a label key', () => {
      for (const s of ANIMATED_STYLES) {
        for (const p of s.params) {
          expect(p.labelKey, `${s.id}.${p.key} labelKey`).toMatch(/^settings\.items\./)
        }
      }
    })

    it('keeps slider defaults inside their own range', () => {
      // The reset button writes `defaultValue` straight to the slider; a default
      // outside [min,max] would produce a value the control cannot represent.
      for (const s of ANIMATED_STYLES) {
        for (const p of s.params) {
          if (p.kind !== 'slider') continue
          const sl = p as SliderSpec
          expect(sl.defaultValue, `${s.id}.${sl.key} default`).toBeGreaterThanOrEqual(sl.min)
          expect(sl.defaultValue, `${s.id}.${sl.key} default`).toBeLessThanOrEqual(sl.max)
          expect(sl.step, `${s.id}.${sl.key} step`).toBeGreaterThan(0)
          expect(sl.min, `${s.id}.${sl.key} min`).toBeLessThan(sl.max)
        }
      }
    })

    it('uses a non-zero step so every slider is movable', () => {
      for (const s of ANIMATED_STYLES) {
        for (const p of s.params) {
          if (p.kind !== 'slider') continue
          expect(p.step, `${s.id}.${p.key}`).toBeGreaterThan(0)
        }
      }
    })

    it('exposes the XMB edge-fade switch under the non-colliding key', () => {
      // `edgeFade` would collide conceptually with the IMAGE wallpaper's
      // `wallpaperEdgeFade` (a CSS mask). The canvas one is `fadeEdges`.
      const xmb = getAnimatedStyle('xmb')
      expect(xmb.params.map((p) => p.key)).toContain('fadeEdges')
      expect(xmb.params.map((p) => p.key)).not.toContain('edgeFade')
    })

    it('does not give silk an edge-fade toggle', () => {
      // The silk prototype has no such control; the fill is already soft-edged.
      expect(getAnimatedStyle('silk').params.map((p) => p.key)).not.toContain('fadeEdges')
    })
  })

  describe('getAnimatedStyle', () => {
    it('returns the requested style', () => {
      expect(getAnimatedStyle('silk').id).toBe('silk')
      expect(getAnimatedStyle('xmb').id).toBe('xmb')
    })

    it('falls back to the default for an unknown or missing id', () => {
      // A stored id can outlive its style (removed, or hand-edited storage).
      for (const bad of ['nope', '', null, undefined]) {
        expect(getAnimatedStyle(bad).id).toBe(DEFAULT_ANIMATED_STYLE)
      }
    })
  })

  describe('isKnownAnimatedStyle', () => {
    it('accepts registered ids only', () => {
      expect(isKnownAnimatedStyle('xmb')).toBe(true)
      expect(isKnownAnimatedStyle('silk')).toBe(true)
      expect(isKnownAnimatedStyle('nope')).toBe(false)
      expect(isKnownAnimatedStyle('')).toBe(false)
      expect(isKnownAnimatedStyle(null)).toBe(false)
    })
  })

  describe('resolveStyleParams', () => {
    const xmb = getAnimatedStyle('xmb')

    it('fills every declared param with its default when nothing is stored', () => {
      const out = resolveStyleParams(xmb, undefined)
      for (const spec of xmb.params) {
        expect(out[spec.key], `${spec.key}`).toBe(spec.defaultValue)
      }
    })

    it('lets stored values win', () => {
      const out = resolveStyleParams(xmb, { lam: 150, contrast: 80, fadeEdges: false })
      expect(out.lam).toBe(150)
      expect(out.contrast).toBe(80)
      expect(out.fadeEdges).toBe(false)
    })

    it('replaces out-of-range numbers with the default', () => {
      const out = resolveStyleParams(xmb, { lam: 9999, amp: -5 })
      expect(out.lam).toBe(100)
      expect(out.amp).toBe(100)
    })

    it('replaces wrong-typed values with the default', () => {
      // Hand-edited storage, or a value written by an older/other shape.
      const out = resolveStyleParams(xmb, { lam: 'fast' as unknown as number, fadeEdges: 1 as unknown as boolean })
      expect(out.lam).toBe(100)
      expect(out.fadeEdges).toBe(true)
    })

    it('ignores keys that are not declared by the style', () => {
      const out = resolveStyleParams(xmb, { ghost: 42 })
      expect(out).not.toHaveProperty('ghost')
    })

    it('accepts a boundary value exactly at min and max', () => {
      const lam = xmb.params.find((p) => p.key === 'lam') as SliderSpec
      expect(resolveStyleParams(xmb, { lam: lam.min }).lam).toBe(lam.min)
      expect(resolveStyleParams(xmb, { lam: lam.max }).lam).toBe(lam.max)
    })
  })
})
