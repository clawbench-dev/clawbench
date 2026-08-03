import { describe, expect, it } from 'vitest'
import { normalizeRatio, clampRatio, MIN_PANEL_WIDTH, DEFAULT_RATIO } from '@/utils/splitRatio'

describe('normalizeRatio', () => {
  it('returns DEFAULT_RATIO for non-finite / non-number input', () => {
    expect(normalizeRatio(Number.NaN)).toBe(DEFAULT_RATIO)
    expect(normalizeRatio('0.3' as unknown as number)).toBe(DEFAULT_RATIO)
    expect(normalizeRatio(undefined as unknown as number)).toBe(DEFAULT_RATIO)
    expect(normalizeRatio(Number.POSITIVE_INFINITY)).toBe(DEFAULT_RATIO)
  })

  it('clamps to [0, 1]', () => {
    expect(normalizeRatio(-0.5)).toBe(0)
    expect(normalizeRatio(1.7)).toBe(1)
    expect(normalizeRatio(0.4)).toBe(0.4)
  })
})

describe('clampRatio', () => {
  it('respects symmetric min widths on both sides', () => {
    // container 1000, minLeft=320, minRight=320 → left ∈ [320, 680]
    expect(clampRatio(0.1, 1000)).toBeCloseTo(0.32)   // 320/1000
    expect(clampRatio(0.9, 1000)).toBeCloseTo(0.68)   // 680/1000
    expect(clampRatio(0.5, 1000)).toBeCloseTo(0.5)
  })

  it('returns DEFAULT_RATIO when container is too small for two panels', () => {
    expect(clampRatio(0.3, MIN_PANEL_WIDTH * 2 - 10)).toBe(DEFAULT_RATIO)
  })

  it('returns DEFAULT_RATIO for invalid container width', () => {
    expect(clampRatio(0.3, 0)).toBe(DEFAULT_RATIO)
    expect(clampRatio(0.3, Number.NaN)).toBe(DEFAULT_RATIO)
  })
})
