import { describe, it, expect, afterEach } from 'vitest'
import {
  computeAutoUIScale,
  resolveUIScale,
  currentScreenHeight,
  UI_SCALE_BASE_HEIGHT,
  UI_SCALE_STEP,
} from '@/utils/uiScale'

describe('computeAutoUIScale', () => {
  it('is a no-op at the 1080p reference height', () => {
    expect(computeAutoUIScale(1080)).toBe(1)
  })

  it('scales a 1440p screen by 1.35 (snapped to the slider grid)', () => {
    // 1440 / 1080 = 1.333… which is NOT on the 0.05 grid. It is snapped to
    // 1.35 so the settings slider can represent it exactly — otherwise the
    // thumb renders on the 1.35 step while the label would read "133%".
    expect(computeAutoUIScale(1440)).toBe(1.35)
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

  it('scales a 1200p screen by 1.1 (snapped to the slider grid)', () => {
    // 1200 / 1080 = 1.111… → snapped to 1.1
    expect(computeAutoUIScale(1200)).toBe(1.1)
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
  it('ignores the manual value entirely when auto is on', () => {
    // The manual slider is disabled in the UI while auto is on, so a stale
    // manual value must not leak into the applied factor.
    expect(resolveUIScale(true, 1.5, 2160)).toBe(2)
    expect(resolveUIScale(true, 0.8, 1080)).toBe(1)
  })

  it('uses the manual value when auto is off', () => {
    expect(resolveUIScale(false, 1.25, 2160)).toBe(1.25)
    expect(resolveUIScale(false, 0.8, 1080)).toBe(0.8)
  })

  it('clamps a manual value to the supported range', () => {
    // Auto is off, so the screen height is irrelevant — but a corrupt stored
    // value still must not escape the bounds the CSS-zoom applier accepts.
    expect(resolveUIScale(false, 99, 1080)).toBe(2)
    expect(resolveUIScale(false, 0.01, 1080)).toBe(0.5)
  })

  it('snaps an off-grid manual value so the slider can represent it', () => {
    // A legacy/hand-edited value that is not a whole step would render its
    // thumb on the nearest step while the label showed the raw number, so both
    // paths must snap. 0.82 → 0.8, 1.33 → 1.35.
    expect(resolveUIScale(false, 0.82, 1080)).toBe(0.8)
    expect(resolveUIScale(false, 1.33, 1080)).toBe(1.35)
    expect(resolveUIScale(false, 1.02, 1080)).toBe(1)
  })

  it('always returns a manual value representable on the slider step grid', () => {
    for (let m = 0.5; m <= 2; m += 0.01) {
      const f = resolveUIScale(false, m, 1080)
      const steps = f / UI_SCALE_STEP
      expect(Math.abs(steps - Math.round(steps))).toBeLessThan(1e-9)
    }
  })

  it('falls back to 1 for an unusable manual value', () => {
    expect(resolveUIScale(false, NaN, 1080)).toBe(1)
    expect(resolveUIScale(false, 0, 1080)).toBe(1)
    expect(resolveUIScale(false, -2, 1080)).toBe(1)
  })
})

describe('currentScreenHeight', () => {
  const original = window.screen

  afterEach(() => {
    Object.defineProperty(window, 'screen', { value: original, configurable: true })
  })

  it('reads window.screen.height', () => {
    Object.defineProperty(window, 'screen', { value: { height: 1440 }, configurable: true })
    expect(currentScreenHeight()).toBe(1440)
  })

  it('returns 0 rather than NaN when screen is unavailable', () => {
    Object.defineProperty(window, 'screen', { value: undefined, configurable: true })
    expect(currentScreenHeight()).toBe(0)
  })
})
