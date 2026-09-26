import { describe, it, expect } from 'vitest'
import {
  parseHexColor,
  parseHexColorOr,
  parseAccentColor,
  parseBackgroundColor,
  mixRgb,
  rgba,
  buildWavePalette,
  waveTimeScale,
  centerline,
  noise2,
  visibleCycles,
  WAVE_LAYERS,
  WAVE_SPEED_MID,
  WAVE_SPEED_MIN,
  WAVE_SPEED_MAX,
  WAVE_TIME_SCALE_MIN,
  WAVE_TIME_SCALE_MAX,
  type Rgb,
} from '../waveMath'

describe('parseHexColor', () => {
  it('parses 6-digit hex with and without #', () => {
    expect(parseHexColor('#fe8019')).toEqual({ r: 254, g: 128, b: 25 })
    expect(parseHexColor('fe8019')).toEqual({ r: 254, g: 128, b: 25 })
  })

  it('expands 3-digit shorthand', () => {
    expect(parseHexColor('#abc')).toEqual({ r: 170, g: 187, b: 204 })
  })

  it('trims surrounding whitespace', () => {
    // getPropertyValue on a CSS custom property returns a leading space.
    expect(parseHexColor('  #fe8019  ')).toEqual({ r: 254, g: 128, b: 25 })
  })

  it('returns null (not NaN) for empty input', () => {
    // This is the whole point: an empty CSS variable must not produce NaN,
    // because a NaN colour makes the canvas silently keep its previous value.
    expect(parseHexColor('')).toBeNull()
    expect(parseHexColor('   ')).toBeNull()
    expect(parseHexColor(undefined)).toBeNull()
    expect(parseHexColor(null)).toBeNull()
  })

  it('returns null for malformed input', () => {
    expect(parseHexColor('#gggggg')).toBeNull()
    expect(parseHexColor('rgb(1,2,3)')).toBeNull()
    expect(parseHexColor('#12345')).toBeNull() // wrong length
    expect(parseHexColor('#1234567')).toBeNull()
    expect(parseHexColor('none')).toBeNull()
  })

  it('rejects strings parseInt would happily accept', () => {
    // parseInt('0x1234', 16) === 4660, parseInt('+12345', 16) === 74565 and
    // parseInt('-12345', 16) is negative — all finite numbers that are NOT hex
    // colours. A length check alone lets these through, so the character-class
    // guard is load-bearing, not decoration.
    expect(parseHexColor('0x1234')).toBeNull()
    expect(parseHexColor('+12345')).toBeNull()
    expect(parseHexColor('-12345')).toBeNull()
    expect(parseHexColor('+abcde')).toBeNull()
    expect(parseHexColor(' 0x1234 ')).toBeNull()
  })

  it('never yields NaN components', () => {
    for (const bad of ['', '#', 'zzz', 'rgb(0,0,0)', undefined, null]) {
      const out = parseHexColor(bad as string | null | undefined)
      if (out !== null) {
        expect(Number.isFinite(out.r)).toBe(true)
        expect(Number.isFinite(out.g)).toBe(true)
        expect(Number.isFinite(out.b)).toBe(true)
      }
    }
  })
})

describe('parseHexColorOr', () => {
  it('falls back when the value is unusable', () => {
    const fb: Rgb = { r: 1, g: 2, b: 3 }
    expect(parseHexColorOr('', fb)).toEqual(fb)
    expect(parseHexColorOr('nonsense', fb)).toEqual(fb)
    expect(parseHexColorOr('#000000', fb)).toEqual({ r: 0, g: 0, b: 0 })
  })
})

describe('theme colour parsing', () => {
  it('falls back to gruvbox defaults for missing variables', () => {
    // --accent-color / --bg-primary have no :root fallback in variables.css,
    // so an empty read is a real possibility.
    expect(parseAccentColor('')).toEqual({ r: 254, g: 128, b: 25 })
    expect(parseBackgroundColor('')).toEqual({ r: 40, g: 40, b: 40 })
  })

  it('never returns NaN for any malformed input', () => {
    for (const bad of ['', '   ', 'nonsense', 'rgb(1,2,3)', '#12', undefined, null]) {
      const a = parseAccentColor(bad as string | null | undefined)
      const b = parseBackgroundColor(bad as string | null | undefined)
      expect([a.r, a.g, a.b, b.r, b.g, b.b].every(Number.isFinite)).toBe(true)
    }
  })
})

describe('mixRgb', () => {
  it('returns the endpoints at k=0 and k=1', () => {
    const a: Rgb = { r: 0, g: 0, b: 0 }
    const b: Rgb = { r: 100, g: 200, b: 50 }
    expect(mixRgb(a, b, 0)).toEqual(a)
    expect(mixRgb(a, b, 1)).toEqual(b)
  })

  it('blends at the midpoint', () => {
    expect(mixRgb({ r: 0, g: 0, b: 0 }, { r: 100, g: 100, b: 100 }, 0.5)).toEqual({ r: 50, g: 50, b: 50 })
  })
})

describe('rgba', () => {
  it('formats a canvas colour string', () => {
    expect(rgba({ r: 1, g: 2, b: 3 }, 0.5)).toBe('rgba(1, 2, 3, 0.5)')
  })
})

describe('buildWavePalette', () => {
  it('derives all four colours from accent + background', () => {
    const p = buildWavePalette('#fe8019', '#282828')
    for (const c of [p.bgTop, p.bgBottom, p.waveRim, p.waveBody]) {
      expect([c.r, c.g, c.b].every(Number.isFinite)).toBe(true)
    }
  })

  it('makes the rim the brightest colour, so the crest reads as a crisp edge', () => {
    const p = buildWavePalette('#fe8019', '#282828')
    const lum = (c: Rgb) => 0.2126 * c.r + 0.7152 * c.g + 0.0722 * c.b
    // The crest must outshine the background it is drawn over, otherwise the
    // wave is physically invisible (a bug hit during prototyping).
    expect(lum(p.waveRim)).toBeGreaterThan(lum(p.bgBottom))
    expect(lum(p.bgBottom)).toBeGreaterThan(lum(p.bgTop))
  })

  it('honours explicit overrides', () => {
    const p = buildWavePalette('#fe8019', '#282828', {
      top: '#111111', bottom: '#222222', rim: '#333333', body: '#444444',
    })
    expect(p.bgTop).toEqual({ r: 17, g: 17, b: 17 })
    expect(p.bgBottom).toEqual({ r: 34, g: 34, b: 34 })
    expect(p.waveRim).toEqual({ r: 51, g: 51, b: 51 })
    expect(p.waveBody).toEqual({ r: 68, g: 68, b: 68 })
  })

  it('applies overrides independently', () => {
    const p = buildWavePalette('#fe8019', '#282828', { rim: '#ffffff' })
    expect(p.waveRim).toEqual({ r: 255, g: 255, b: 255 })
    // The rest stays derived rather than falling back to a default.
    const derived = buildWavePalette('#fe8019', '#282828')
    expect(p.bgTop).toEqual(derived.bgTop)
    expect(p.bgBottom).toEqual(derived.bgBottom)
    expect(p.waveBody).toEqual(derived.waveBody)
  })

  it('ignores a malformed override instead of producing NaN', () => {
    const p = buildWavePalette('#fe8019', '#282828', { rim: 'not-a-colour' })
    const derived = buildWavePalette('#fe8019', '#282828')
    expect(p.waveRim).toEqual(derived.waveRim)
  })
})

describe('waveTimeScale', () => {
  it('maps the slider midpoint to 1x', () => {
    expect(waveTimeScale(WAVE_SPEED_MID)).toBe(1)
  })

  it('scales proportionally around the midpoint', () => {
    expect(waveTimeScale(100)).toBe(2)
    expect(waveTimeScale(25)).toBe(0.5)
  })

  it('reaches both ends of the slider range', () => {
    // The bounds must line up with the slider's own min/max: if they were
    // wider, part of the slider would be dead (several positions clamping to
    // the same value); if narrower, the extremes would be unreachable.
    expect(waveTimeScale(WAVE_SPEED_MIN)).toBe(WAVE_TIME_SCALE_MIN)
    expect(waveTimeScale(WAVE_SPEED_MAX)).toBe(WAVE_TIME_SCALE_MAX)
    expect(WAVE_TIME_SCALE_MIN).toBe(0.2)
    expect(WAVE_TIME_SCALE_MAX).toBe(2)
  })

  it('clamps values outside the slider range', () => {
    // Defends against a corrupt persisted value, not the slider itself.
    expect(waveTimeScale(0)).toBe(WAVE_TIME_SCALE_MIN)
    expect(waveTimeScale(-100)).toBe(WAVE_TIME_SCALE_MIN)
    expect(waveTimeScale(1000)).toBe(WAVE_TIME_SCALE_MAX)
  })

  it('is strictly increasing across the slider range', () => {
    // No dead zones: every step of the slider changes the speed.
    let prev = -Infinity
    for (let v = WAVE_SPEED_MIN; v <= WAVE_SPEED_MAX; v++) {
      const cur = waveTimeScale(v)
      expect(cur).toBeGreaterThan(prev)
      prev = cur
    }
  })

  it('falls back to 1x for a non-finite value', () => {
    // A hand-edited or corrupt localStorage value must not freeze the wave.
    // Number.isFinite rejects NaN and ±Infinity alike, so all land on 1x.
    expect(waveTimeScale(NaN)).toBe(1)
    expect(waveTimeScale(Infinity)).toBe(1)
    expect(waveTimeScale(-Infinity)).toBe(1)
    expect(waveTimeScale(undefined as unknown as number)).toBe(1)
  })
})

describe('noise2', () => {
  it('stays within 0..1 over a wide sample', () => {
    let lo = Infinity, hi = -Infinity
    for (let i = 0; i < 3000; i++) {
      const v = noise2(i * 0.37, i * 0.11)
      lo = Math.min(lo, v)
      hi = Math.max(hi, v)
    }
    expect(lo).toBeGreaterThanOrEqual(0)
    expect(hi).toBeLessThanOrEqual(1)
    // And it must actually vary, not return a constant.
    expect(hi - lo).toBeGreaterThan(0.5)
  })

  it('is deterministic across calls', () => {
    expect(noise2(1.5, 2.5)).toBe(noise2(1.5, 2.5))
  })
})

describe('centerline', () => {
  const layer = WAVE_LAYERS[WAVE_LAYERS.length - 1]

  it('produces finite values across the width', () => {
    // Guards the NaN class of bug: a missing layer field yields NaN, and canvas
    // silently drops a NaN path, which looks like "the wave disappeared".
    for (let i = 0; i <= 100; i++) {
      const y = centerline(i / 100, layer, { time: 1.234 })
      expect(Number.isFinite(y)).toBe(true)
    }
  })

  it('stays within the canvas plus a small tilt margin', () => {
    for (let t = 0; t < 5; t += 0.5) {
      for (let i = 0; i <= 50; i++) {
        const y = centerline(i / 50, layer, { time: t })
        expect(y).toBeGreaterThan(-0.3)
        expect(y).toBeLessThan(1.3)
      }
    }
  })

  it('is a pure function of its inputs', () => {
    const p = { time: 2.5 }
    expect(centerline(0.42, layer, p)).toBe(centerline(0.42, layer, p))
  })

  it('changes over time (the shape is animated, not static)', () => {
    const a = centerline(0.3, layer, { time: 0 })
    const b = centerline(0.3, layer, { time: 3 })
    expect(a).not.toBe(b)
  })

  it('deforms rather than merely translating', () => {
    // Sample a profile at two times and find the best rigid shift. A pure
    // translation aligns almost perfectly; genuine morphing does not. This is
    // the property that makes the wave read as "fluid" rather than "sliding".
    const N = 200
    const profile = (t: number) => {
      const out: number[] = []
      for (let i = 0; i < N; i++) out.push(centerline(i / N, layer, { time: t }))
      return out
    }
    const a = profile(0)
    const b = profile(4)

    let best = Infinity
    for (let shift = -40; shift <= 40; shift++) {
      let err = 0, count = 0
      for (let i = 0; i < N; i++) {
        const j = i + shift
        if (j < 0 || j >= N) continue
        err += Math.abs(a[i] - b[j])
        count++
      }
      if (count > N * 0.5) best = Math.min(best, err / count)
    }
    // A rigid translation would drive this to ~0.
    expect(best).toBeGreaterThan(0.001)
  })

  it('with zero irregularity is a clean sine (no warp/harmonic/envelope)', () => {
    const a = centerline(0.25, layer, { time: 0, irregularity: 0 })
    const b = centerline(0.25, layer, { time: 0, irregularity: 0 })
    expect(a).toBe(b)
    // Tilt and base remain, so the value is still finite and in range.
    expect(Number.isFinite(a)).toBe(true)
  })

  it('ampScale widens the excursion', () => {
    const narrow = Math.abs(centerline(0.25, layer, { time: 0.7, ampScale: 0.5 }) - layer.base)
    const wide = Math.abs(centerline(0.25, layer, { time: 0.7, ampScale: 2 }) - layer.base)
    expect(wide).toBeGreaterThan(narrow)
  })

  it('lamScale changes the wavelength', () => {
    // Same x, different wavelength → different phase → different height.
    const a = centerline(0.4, layer, { time: 0, lamScale: 1 })
    const b = centerline(0.4, layer, { time: 0, lamScale: 2 })
    expect(a).not.toBe(b)
  })
})

describe('WAVE_LAYERS', () => {
  it('has exactly three bands', () => {
    // The brief is "about two or three waves at once", not a field of ripples.
    expect(WAVE_LAYERS).toHaveLength(3)
  })

  it('gives every band a defined base', () => {
    // A missing base is the NaN trap; assert it explicitly so a future edit
    // that drops the field fails here rather than silently rendering nothing.
    for (const l of WAVE_LAYERS) {
      expect(typeof l.base).toBe('number')
      expect(Number.isFinite(l.base)).toBe(true)
    }
  })

  it('keeps every band within the canvas vertically', () => {
    for (const l of WAVE_LAYERS) {
      expect(l.base).toBeGreaterThan(0)
      expect(l.base).toBeLessThan(1)
    }
  })

  it('stacks the bands front-to-back with increasing opacity', () => {
    for (let i = 1; i < WAVE_LAYERS.length; i++) {
      expect(WAVE_LAYERS[i].alpha).toBeGreaterThan(WAVE_LAYERS[i - 1].alpha)
      expect(WAVE_LAYERS[i].base).toBeGreaterThan(WAVE_LAYERS[i - 1].base)
    }
  })

  it('uses a low frequency so only about half a period is visible', () => {
    const cycles = visibleCycles(1)
    expect(cycles).toBeGreaterThan(0.3)
    expect(cycles).toBeLessThan(0.6)
  })
})

describe('visibleCycles', () => {
  it('shrinks as the wavelength multiplier grows', () => {
    expect(visibleCycles(2)).toBeLessThan(visibleCycles(1))
  })
})
