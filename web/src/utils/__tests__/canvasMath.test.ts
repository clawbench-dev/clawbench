import { describe, it, expect } from 'vitest'
import {
  BLACK,
  FALLBACK_ACCENT,
  FALLBACK_BG,
  TAU,
  WHITE,
  luminance,
  makeRng,
  mixRgb,
  noise2,
  parseAccentColor,
  parseBackgroundColor,
  parseHexColor,
  parseHexColorOr,
  rgba,
} from '../canvasMath'

/**
 * The shared primitives every animated wallpaper style builds on.
 *
 * These were extracted from waveMath.ts so a second style could reuse them
 * instead of shipping its own copies (the ps3 prototype did exactly that). The
 * NaN / fallback guarantees are the load-bearing part: canvas silently ignores an
 * invalid fillStyle and keeps the previous one, so a NaN colour presents as "the
 * background never updates" with a clean console.
 */
describe('canvasMath', () => {
  describe('parseHexColor', () => {
    it('parses 6-digit hex with and without the hash', () => {
      expect(parseHexColor('#ff8000')).toEqual({ r: 255, g: 128, b: 0 })
      expect(parseHexColor('ff8000')).toEqual({ r: 255, g: 128, b: 0 })
    })

    it('expands 3-digit shorthand', () => {
      expect(parseHexColor('#f80')).toEqual({ r: 255, g: 136, b: 0 })
    })

    it('trims surrounding whitespace', () => {
      // getComputedStyle can hand back a padded value; a leading space used to
      // fail the character test and silently fall back.
      expect(parseHexColor('  #ffffff  ')).toEqual({ r: 255, g: 255, b: 255 })
    })

    it('returns null (never NaN) for malformed input', () => {
      for (const bad of ['', '   ', 'nope', '#12345', '#1234567', 'rgb(1,2,3)', '#gggggg', null, undefined]) {
        expect(parseHexColor(bad as string | null | undefined), `input ${String(bad)}`).toBeNull()
      }
    })

    it('never yields NaN components for any accepted input', () => {
      const c = parseHexColor('#0a1e6e')!
      expect([c.r, c.g, c.b].every(Number.isFinite)).toBe(true)
    })
  })

  describe('parseHexColorOr', () => {
    it('falls back only when parsing fails', () => {
      expect(parseHexColorOr('#000000', WHITE)).toEqual({ r: 0, g: 0, b: 0 })
      // Note: 'bad' is NOT malformed — it is valid 3-digit hex (#bbaadd). Use a
      // genuinely unparseable value to exercise the fallback.
      expect(parseHexColorOr('not-a-colour', WHITE)).toEqual(WHITE)
      expect(parseHexColorOr('#12345', WHITE)).toEqual(WHITE)
      expect(parseHexColorOr(undefined, BLACK)).toEqual(BLACK)
    })
  })

  describe('theme variable parsers', () => {
    it('uses the gruvbox fallbacks when the variable is absent', () => {
      // --accent-color / --bg-primary have no :root default, so an empty read is
      // the normal cold-start case rather than an error.
      expect(parseAccentColor('')).toEqual(FALLBACK_ACCENT)
      expect(parseBackgroundColor('')).toEqual(FALLBACK_BG)
    })

    it('prefers a real value when one is present', () => {
      expect(parseAccentColor('#4678f5')).toEqual({ r: 70, g: 120, b: 245 })
      expect(parseBackgroundColor('#0a1e6e')).toEqual({ r: 10, g: 30, b: 110 })
    })
  })

  describe('luminance', () => {
    it('is 0 for black and 1 for white', () => {
      expect(luminance(BLACK)).toBeCloseTo(0, 5)
      expect(luminance(WHITE)).toBeCloseTo(1, 5)
    })

    it('increases monotonically with brightness', () => {
      const l = (v: number) => luminance({ r: v, g: v, b: v })
      expect(l(0)).toBeLessThan(l(64))
      expect(l(64)).toBeLessThan(l(128))
      expect(l(128)).toBeLessThan(l(255))
    })

    it('weights green above red above blue', () => {
      expect(luminance({ r: 255, g: 0, b: 0 })).toBeLessThan(luminance({ r: 0, g: 255, b: 0 }))
      expect(luminance({ r: 0, g: 0, b: 255 })).toBeLessThan(luminance({ r: 255, g: 0, b: 0 }))
    })
  })

  describe('mixRgb', () => {
    it('returns the endpoints at k=0 and k=1', () => {
      expect(mixRgb(BLACK, WHITE, 0)).toEqual(BLACK)
      expect(mixRgb(BLACK, WHITE, 1)).toEqual(WHITE)
    })

    it('interpolates per channel and rounds', () => {
      expect(mixRgb({ r: 0, g: 0, b: 0 }, { r: 255, g: 128, b: 1 }, 0.5)).toEqual({ r: 128, g: 64, b: 1 })
    })

    it('extrapolates outside 0..1 without clamping', () => {
      // Callers rely on this: the palette pushes a colour past its base toward
      // white, and clamping here would flatten the crest.
      expect(mixRgb(BLACK, WHITE, 2)).toEqual({ r: 510, g: 510, b: 510 })
    })
  })

  describe('rgba', () => {
    it('formats a canvas colour string', () => {
      expect(rgba({ r: 1, g: 2, b: 3 }, 0.5)).toBe('rgba(1, 2, 3, 0.5)')
      expect(rgba(WHITE, 0)).toBe('rgba(255, 255, 255, 0)')
    })
  })

  describe('noise2', () => {
    it('stays within 0..1', () => {
      for (let i = 0; i < 200; i++) {
        const v = noise2(i * 0.37, i * 0.11)
        expect(v).toBeGreaterThanOrEqual(0)
        expect(v).toBeLessThanOrEqual(1)
      }
    })

    it('is deterministic across calls', () => {
      // The wave must look identical on every load; a non-deterministic noise
      // field would reshuffle the whole shape.
      expect(noise2(1.5, 2.5)).toBe(noise2(1.5, 2.5))
      expect(noise2(12.25, 0.75)).toBe(noise2(12.25, 0.75))
    })

    it('is continuous across integer boundaries', () => {
      // Value noise interpolates between lattice points; a discontinuity at the
      // boundary would show up as a visible seam in the wave.
      const justBefore = noise2(3 - 1e-6, 0.5)
      const justAfter = noise2(3 + 1e-6, 0.5)
      expect(Math.abs(justAfter - justBefore)).toBeLessThan(1e-4)
    })

    it('actually varies with position', () => {
      const samples = new Set([noise2(0.1, 0.2), noise2(5.3, 1.7), noise2(9.9, 4.4)])
      expect(samples.size).toBeGreaterThan(1)
    })
  })

  describe('makeRng', () => {
    it('is reproducible for the same seed', () => {
      const a = makeRng(12345)
      const b = makeRng(12345)
      const seqA = [a(), a(), a(), a()]
      const seqB = [b(), b(), b(), b()]
      expect(seqA).toEqual(seqB)
    })

    it('differs across seeds', () => {
      expect(makeRng(1)()).not.toBe(makeRng(2)())
    })

    it('stays within 0..1', () => {
      const r = makeRng(987654321)
      for (let i = 0; i < 500; i++) {
        const v = r()
        expect(v).toBeGreaterThanOrEqual(0)
        expect(v).toBeLessThan(1)
      }
    })
  })

  it('exports a full turn as TAU', () => {
    expect(TAU).toBeCloseTo(Math.PI * 2, 10)
  })
})
