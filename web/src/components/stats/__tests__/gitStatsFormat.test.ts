import { describe, expect, it } from 'vitest'
import { formatLineCount, formatNetDelta, rowLineValueOf } from '@/components/stats/gitStatsFormat'
import type { GitStatsRow } from '@/composables/useGitCodeStats'

describe('formatLineCount line-count tiers (raw / K / M / B)', () => {
  it('raw below 1K, one K decimal from 1K up, one M decimal from 1M up', () => {
    expect(formatLineCount(0)).toBe('0')
    expect(formatLineCount(5)).toBe('5')
    expect(formatLineCount(999)).toBe('999')
    expect(formatLineCount(1000)).toBe('1.0K')
    expect(formatLineCount(1234)).toBe('1.2K')
    expect(formatLineCount(999900)).toBe('999.9K')
    expect(formatLineCount(1_000_000)).toBe('1.0M')
    expect(formatLineCount(1_234_567)).toBe('1.2M')
    expect(formatLineCount(12_300_000)).toBe('12.3M')
  })

  it('near-boundary values roll into the next tier', () => {
    expect(formatLineCount(999_950)).toBe('1.0M')
    expect(formatLineCount(999_999)).toBe('1.0M')
    expect(formatLineCount(999.7)).toBe('1.0K')
  })

  it('preserves a negative sign for net values', () => {
    expect(formatLineCount(-1)).toBe('-1')
    expect(formatLineCount(-1500)).toBe('-1.5K')
    expect(formatLineCount(-2_000_000)).toBe('-2.0M')
  })
})

describe('formatNetDelta explicit plus', () => {
  it('adds + for positive net, keeps - and raw zero', () => {
    expect(formatNetDelta(1200)).toBe('+1.2K')
    expect(formatNetDelta(0)).toBe('0')
    expect(formatNetDelta(-500)).toBe('-500')
  })
})

describe('rowLineValueOf', () => {
  const row: GitStatsRow = { author: 'A', added: 10, deleted: 4, net: 6, commitCnt: 2 }
  it('maps each metric to the row field', () => {
    expect(rowLineValueOf(row, 'added')).toBe(10)
    expect(rowLineValueOf(row, 'deleted')).toBe(4)
    expect(rowLineValueOf(row, 'net')).toBe(6)
    expect(rowLineValueOf(row, 'commitCnt')).toBe(2)
  })
})
