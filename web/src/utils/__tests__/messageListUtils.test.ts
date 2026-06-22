import { describe, expect, it } from 'vitest'
import { computeRemainingCount } from '@/utils/messageListUtils.ts'

describe('computeRemainingCount', () => {
  it('returns 0 when hasMore is false', () => {
    expect(computeRemainingCount(false, 100, 20)).toBe(0)
  })
  it('returns difference when hasMore is true', () => {
    expect(computeRemainingCount(true, 100, 20)).toBe(80)
  })
  it('returns 0 when total equals loaded', () => {
    expect(computeRemainingCount(true, 20, 20)).toBe(0)
  })
  it('clamps to 0 when loaded exceeds total', () => {
    expect(computeRemainingCount(true, 10, 20)).toBe(0)
  })
  it('handles zero total', () => {
    expect(computeRemainingCount(true, 0, 0)).toBe(0)
  })
})
