import { describe, expect, it } from 'vitest'
import { computeVisibleChipCount } from '@/utils/chipLayout'

describe('computeVisibleChipCount', () => {
  it('counts chips that fit including gaps', () => {
    // 3 chips of 100 + gaps 6: 100, 206, 312
    expect(computeVisibleChipCount([100, 100, 100], 312, 6)).toBe(3)
    expect(computeVisibleChipCount([100, 100, 100], 311, 6)).toBe(2)
  })

  it('returns 0 when even the first chip does not fit', () => {
    expect(computeVisibleChipCount([100, 100], 50, 6)).toBe(0)
  })

  it('returns all when the width is not measurable', () => {
    expect(computeVisibleChipCount([100, 100, 100], 0, 6)).toBe(3)
    expect(computeVisibleChipCount([100, 100], -1, 6)).toBe(2)
  })

  it('returns 0 for an empty list', () => {
    expect(computeVisibleChipCount([], 500, 6)).toBe(0)
  })

  it('handles a single chip without counting a leading gap', () => {
    expect(computeVisibleChipCount([120], 120, 6)).toBe(1)
    expect(computeVisibleChipCount([120], 119, 6)).toBe(0)
  })
})
