import { describe, it, expect, afterEach } from 'vitest'
import {
  computeAutoUIScale,
  resolveUIScale,
  currentScreenHeight,
  screenHeightToCssPixels,
  shouldSuggestUIScale,
  UI_SCALE_BASE_HEIGHT,
  UI_SCALE_STEP,
  UI_SCALE_SUGGEST_THRESHOLD,
  applyUIScale,
  designToCss,
} from '@/utils/uiScale'

describe('computeAutoUIScale', () => {
  it('is a no-op at the 1080p reference height', () => {
    expect(computeAutoUIScale(1080)).toBe(1)
  })

  it('scales a 1440p screen by 1.33 (snapped to the slider grid)', () => {
    // 1440 / 1080 = 1.333… → snapped to 1.33 on the 1% grid, so the slider can
    // represent it exactly. (The old 5% grid forced 1.35, which was 1.3% away
    // from the true fit purely to make the thumb land on a step.)
    expect(computeAutoUIScale(1440)).toBe(1.33)
  })

  it('scales a 2160p (4K at 100% OS scaling) screen by 2', () => {
    expect(computeAutoUIScale(2160)).toBe(2)
  })

  it('caps above 4K rather than growing without bound', () => {
    // 4320 / 1080 = 4, but the layout must never exceed the cap.
    expect(computeAutoUIScale(4320)).toBe(2)
  })

  it('never shrinks below 1 on a low-resolution screen', () => {
    // 768 / 1080 = 0.71 — explicitly clamped up to 1 by design.
    expect(computeAutoUIScale(768)).toBe(1)
    expect(computeAutoUIScale(1080 - 1)).toBe(1)
  })

  it('scales a 1200p screen by 1.11 (snapped to the slider grid)', () => {
    // 1200 / 1080 = 1.111… → snapped to 1.11 on the 1% grid. (On the old 5%
    // grid this landed on 1.1, i.e. a 1% error the user could see in the
    // label but could not fix — the reason the grid is finer now.)
    expect(computeAutoUIScale(1200)).toBe(1.11)
  })

  it('always returns a value representable on the slider step grid', () => {
    // The whole point of snapping: every auto factor must be a whole multiple
    // of UI_SCALE_STEP, or the thumb and the label disagree.
    for (let h = 1080; h <= 4320; h += 7) {
      const f = computeAutoUIScale(h)
      const steps = f / UI_SCALE_STEP
      expect(Math.abs(steps - Math.round(steps))).toBeLessThan(1e-9)
    }
  })

  it('is monotonic across common panel heights', () => {
    const heights = [768, 900, 1080, 1200, 1440, 1600, 2160]
    const factors = heights.map(h => computeAutoUIScale(h))
    for (let i = 1; i < factors.length; i++) {
      expect(factors[i]).toBeGreaterThanOrEqual(factors[i - 1])
    }
  })

  it('degrades to 1 for unusable screen heights', () => {
    // A headless / exotic environment must not produce a broken factor.
    expect(computeAutoUIScale(0)).toBe(1)
    expect(computeAutoUIScale(-1080)).toBe(1)
    expect(computeAutoUIScale(NaN)).toBe(1)
    expect(computeAutoUIScale(Infinity)).toBe(1)
  })

  it('degrades to 1 for an unusable base', () => {
    expect(computeAutoUIScale(1440, 0)).toBe(1)
    expect(computeAutoUIScale(1440, NaN)).toBe(1)
  })

  it('honours a custom base and cap', () => {
    expect(computeAutoUIScale(2160, 2160, 2)).toBe(1)
    expect(computeAutoUIScale(4320, 1080, 1.5)).toBe(1.5)
  })

  it('exposes 1080 as the reference height', () => {
    expect(UI_SCALE_BASE_HEIGHT).toBe(1080)
  })
})

describe('resolveUIScale', () => {
  it('returns the stored value unchanged', () => {
    // The scale is a plain stored setting now — there is no screen input at
    // all, which is the whole point: nothing can re-derive it behind the user.
    expect(resolveUIScale(1.25)).toBe(1.25)
    expect(resolveUIScale(0.8)).toBe(0.8)
    expect(resolveUIScale(2)).toBe(2)
    expect(resolveUIScale(1)).toBe(1)
  })

  it('clamps a stored value to the supported range', () => {
    // A corrupt/legacy stored value must not escape the bounds the CSS-zoom
    // applier accepts.
    expect(resolveUIScale(99)).toBe(2)
    expect(resolveUIScale(0.01)).toBe(0.5)
  })

  it('snaps an off-grid stored value so the slider can represent it', () => {
    // A legacy/hand-edited value that is not a whole step would render its
    // thumb on the nearest step while the label showed the raw number.
    // On the 1% grid only sub-percent noise gets rounded away.
    expect(resolveUIScale(0.824)).toBe(0.82)
    expect(resolveUIScale(1.334)).toBe(1.33)
    expect(resolveUIScale(1.024)).toBe(1.02)
  })

  it('always returns a value representable on the slider step grid', () => {
    for (let m = 0.5; m <= 2; m += 0.01) {
      const f = resolveUIScale(m)
      const steps = f / UI_SCALE_STEP
      expect(Math.abs(steps - Math.round(steps))).toBeLessThan(1e-9)
    }
  })

  it('falls back to 1 for an unusable stored value', () => {
    expect(resolveUIScale(NaN)).toBe(1)
    expect(resolveUIScale(0)).toBe(1)
    expect(resolveUIScale(-2)).toBe(1)
  })
})

describe('shouldSuggestUIScale', () => {
  it('suggests when the screen wants a noticeably larger factor', () => {
    // 4K at 100% (2160 → 2.0) against a fresh install's 1.0.
    expect(shouldSuggestUIScale(1, 2)).toBe(true)
    // 1440p: 1.33 vs 1.0 is 0.33, well past the 0.15 bar.
    expect(shouldSuggestUIScale(1, computeAutoUIScale(1440))).toBe(true)
  })

  it('stays quiet for a difference the user would not notice', () => {
    // 1200p fits 1.11 — an 11% bump is noise on a screen that already reads
    // fine, so it must not interrupt startup.
    expect(shouldSuggestUIScale(1, computeAutoUIScale(1200))).toBe(false)
    // 1080p and below derive exactly 1, i.e. "already correct".
    expect(shouldSuggestUIScale(1, computeAutoUIScale(1080))).toBe(false)
    expect(shouldSuggestUIScale(1, computeAutoUIScale(768))).toBe(false)
  })

  it('never suggests shrinking, however far off the stored value is', () => {
    // A user who deliberately set 200% on a 1080p laptop must not be asked to
    // undo it on every launch — the failure being fixed is "too small", not
    // "too big".
    expect(shouldSuggestUIScale(2, 1)).toBe(false)
    expect(shouldSuggestUIScale(1.5, 1.33)).toBe(false)
  })

  it('is quiet when the two factors already agree', () => {
    expect(shouldSuggestUIScale(1.33, 1.33)).toBe(false)
    expect(shouldSuggestUIScale(2, 2)).toBe(false)
  })

  it('treats the threshold as a lower bound on the difference', () => {
    // Boundary: exactly at the bar suggests, just under it does not. Pins the
    // comparison as >= rather than > so a screen sitting on the boundary is
    // still offered (it is the case the user complained about).
    expect(shouldSuggestUIScale(1, 1 + UI_SCALE_SUGGEST_THRESHOLD)).toBe(true)
    expect(shouldSuggestUIScale(1, 1 + UI_SCALE_SUGGEST_THRESHOLD - 0.01)).toBe(false)
    // Explicit threshold wins over the default.
    expect(shouldSuggestUIScale(1, 1.1, 0.05)).toBe(true)
    expect(shouldSuggestUIScale(1, 1.1, 0.5)).toBe(false)
  })

  it('refuses to suggest on unusable input', () => {
    // Startup must degrade to "no prompt", never to a broken factor.
    expect(shouldSuggestUIScale(NaN, 2)).toBe(false)
    expect(shouldSuggestUIScale(1, NaN)).toBe(false)
    expect(shouldSuggestUIScale(0, 2)).toBe(false)
    expect(shouldSuggestUIScale(-1, 2)).toBe(false)
    expect(shouldSuggestUIScale(1, 2, NaN)).toBe(false)
    expect(shouldSuggestUIScale(1, 2, 0)).toBe(false)
  })

  it('has a threshold well below the smallest screen that derives a fit', () => {
    // 1440p (the common "everything is tiny" laptop panel) must clear the bar;
    // this pins the relationship so retuning one constant cannot silently
    // disable the prompt on the screens it exists for.
    expect(computeAutoUIScale(1440) - 1).toBeGreaterThanOrEqual(UI_SCALE_SUGGEST_THRESHOLD)
  })
})

describe('screenHeightToCssPixels', () => {
  it('leaves the value untouched off Linux', () => {
    // macOS/Windows report CSS pixels, and their dpr is panel density — a
    // Retina 5K reports 1440 at dpr 2 and must keep factor 1.33, not 0.67.
    expect(screenHeightToCssPixels(1440, 2, 2880, false)).toBe(1440)
    expect(screenHeightToCssPixels(2160, 2, 4320, false)).toBe(2160)
  })

  it('leaves the value untouched when dpr is 1 (Linux at 100%)', () => {
    // 4K at 100% OS scaling: screen.height is already 2160 CSS pixels and the
    // window fits (2160 * 1 == 2160), so factor 2 is correct.
    expect(screenHeightToCssPixels(2160, 1, 2160, true)).toBe(2160)
  })

  it('divides by dpr when the reported height is device pixels (native Wayland)', () => {
    // The reported 2160 cannot be CSS pixels: the window alone is 1048 CSS
    // tall (2096 device), which still fits, but a 200%-scaled 4K panel is
    // 1080 logical. Dividing gives the height the OS-scaled UI lives in.
    expect(screenHeightToCssPixels(2160, 2, 2096, true)).toBe(1080)
  })

  it('keeps the value when the window is already taller than the report', () => {
    // X11/XWayland: 1080 CSS pixels at dpr 2. A maximized window is 1048 CSS
    // tall = 2096 device, which EXCEEDS the reported 1080 — so 1080 cannot be
    // device pixels, and halving it would under-scale.
    expect(screenHeightToCssPixels(1080, 2, 2096, true)).toBe(1080)
  })

  it('prefers dividing when the window cannot be measured', () => {
    // 0/NaN window metrics are ambiguous; the device-pixel reading is the one
    // that would otherwise produce the 4x UI this exists to prevent.
    expect(screenHeightToCssPixels(2160, 2, 0, true)).toBe(1080)
    expect(screenHeightToCssPixels(2160, 2, NaN, true)).toBe(1080)
  })

  it('passes through unusable input unchanged', () => {
    expect(screenHeightToCssPixels(0, 2, 100, true)).toBe(0)
    expect(screenHeightToCssPixels(NaN, 2, 100, true)).toBeNaN()
    expect(screenHeightToCssPixels(2160, NaN, 100, true)).toBe(2160)
    expect(screenHeightToCssPixels(2160, 0, 100, true)).toBe(2160)
  })

  it('is a no-op for a fractional dpr at or below 1', () => {
    expect(screenHeightToCssPixels(1080, 0.9, 100, true)).toBe(1080)
  })
})

describe('currentScreenHeight', () => {
  const originalScreen = window.screen
  const originalOuter = window.outerHeight
  const originalInner = window.innerHeight
  const originalDpr = window.devicePixelRatio
  const originalUa = navigator.userAgent

  function setEnv(opts: { screenH: number; dpr?: number; outerH?: number; innerH?: number; ua?: string }) {
    Object.defineProperty(window, 'screen', { value: { height: opts.screenH }, configurable: true })
    Object.defineProperty(window, 'devicePixelRatio', { value: opts.dpr ?? 1, configurable: true })
    Object.defineProperty(window, 'outerHeight', { value: opts.outerH ?? 800, configurable: true })
    Object.defineProperty(window, 'innerHeight', { value: opts.innerH ?? 700, configurable: true })
    Object.defineProperty(navigator, 'userAgent', { value: opts.ua ?? 'Mozilla/5.0 (X11; Linux x86_64) Chrome/154', configurable: true })
  }

  afterEach(() => {
    Object.defineProperty(window, 'screen', { value: originalScreen, configurable: true })
    Object.defineProperty(window, 'outerHeight', { value: originalOuter, configurable: true })
    Object.defineProperty(window, 'innerHeight', { value: originalInner, configurable: true })
    Object.defineProperty(window, 'devicePixelRatio', { value: originalDpr, configurable: true })
    Object.defineProperty(navigator, 'userAgent', { value: originalUa, configurable: true })
  })

  it('reads window.screen.height', () => {
    setEnv({ screenH: 1440, dpr: 1, outerH: 800 })
    expect(currentScreenHeight()).toBe(1440)
  })

  it('returns 0 rather than NaN when screen is unavailable', () => {
    Object.defineProperty(window, 'screen', { value: undefined, configurable: true })
    expect(currentScreenHeight()).toBe(0)
  })

  it('normalizes native Wayland device pixels to CSS pixels', () => {
    // Live GNOME 46 Wayland, 4K at scale 2: screen.height 2160, dpr 2, a
    // maximized window 1048 CSS tall. Must report 1080 so auto scale is 1.
    setEnv({ screenH: 2160, dpr: 2, outerH: 1048, innerH: 961 })
    expect(currentScreenHeight()).toBe(1080)
  })

  it('keeps XWayland CSS pixels as-is', () => {
    // Same monitor, XWayland backend: screen.height 1080, dpr 2, window 1048
    // CSS (2096 device) — already CSS pixels, must stay 1080.
    setEnv({ screenH: 1080, dpr: 2, outerH: 1048, innerH: 961 })
    expect(currentScreenHeight()).toBe(1080)
  })

  it('does not touch Android, whose UA also contains Linux', () => {
    // Android is excluded from auto scale by its own check, but the height
    // must not be silently halved either.
    setEnv({ screenH: 2160, dpr: 2, outerH: 800, ua: 'Mozilla/5.0 (Linux; Android 14; Pixel 8) Chrome/154' })
    expect(currentScreenHeight()).toBe(2160)
  })
})

describe('designToCss', () => {
  afterEach(() => {
    applyUIScale(1)
  })

  it('is the identity at scale 1', () => {
    // The whole point of the conversion is that it changes nothing at 100%:
    // every migrated threshold must keep its exact previous behaviour there.
    applyUIScale(1)
    expect(designToCss(460)).toBe(460)
    expect(designToCss(0)).toBe(0)
  })

  it('multiplies by the applied scale', () => {
    applyUIScale(1.5)
    expect(designToCss(460)).toBeCloseTo(690, 5)
    applyUIScale(2)
    expect(designToCss(320)).toBe(640)
  })

  it('scales fractional design values without rounding', () => {
    // Letter-spacing and similar sub-pixel lengths are passed through; rounding
    // here would reintroduce the very drift this replaced.
    applyUIScale(1.5)
    expect(designToCss(0.5)).toBeCloseTo(0.75, 6)
  })

  it('falls back to 1 when the applied scale is unreadable', () => {
    // First paint before the applier runs, or a standalone document: the value
    // must degrade to "no scaling", not to 0 or NaN.
    document.documentElement.style.removeProperty('--ui-scale')
    expect(designToCss(460)).toBe(460)
  })
})
