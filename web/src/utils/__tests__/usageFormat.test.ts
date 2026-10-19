import { describe, it, expect } from 'vitest'
import { formatCompactCount, formatUsageMeterValue } from '@/utils/usageFormat'

describe('formatCompactCount', () => {
  it('keeps small counts verbatim', () => {
    expect(formatCompactCount(0)).toBe('0')
    expect(formatCompactCount(999)).toBe('999')
  })

  it('rounds at the tier boundary instead of printing 1000.0K', () => {
    // The tier is chosen from the ROUNDED value: 999.7 must render as 1.0K, and
    // 999,950 as 1.0M, never "1000.0K" or "1,000".
    expect(formatCompactCount(999.7)).toBe('1.0K')
    expect(formatCompactCount(999_950)).toBe('1.0M')
    expect(formatCompactCount(999_500_000)).toBe('1.0B')
  })

  it('uses K/M/B tiers', () => {
    expect(formatCompactCount(1_200)).toBe('1.2K')
    expect(formatCompactCount(5_000_000)).toBe('5.0M')
    expect(formatCompactCount(2_500_000_000)).toBe('2.5B')
  })
})

describe('formatUsageMeterValue', () => {
  it('renders cost with two decimals and a dollar sign', () => {
    expect(formatUsageMeterValue('cost', 9.2)).toBe('$9.20')
    expect(formatUsageMeterValue('cost', 1234.5)).toBe('$1,234.50')
  })

  it('compacts tokens', () => {
    expect(formatUsageMeterValue('tokens', 1_234_567)).toBe('1.2M')
  })

  it('compacts credit only when asked', () => {
    expect(formatUsageMeterValue('credit', 12.345)).toBe('12.35')
    expect(formatUsageMeterValue('credit', 1_500_000, { compact: true })).toBe('1.5M')
  })

  it('returns a placeholder for non-numbers', () => {
    expect(formatUsageMeterValue('cost', Number.NaN)).toBe('—')
  })
})
