import { describe, expect, it } from 'vitest'
import {
  formatLineCount,
  formatNetDelta,
  rowLineValueOf,
  efficiencyPerK,
  formatPerKTokens,
  formatPerKCredit,
  formatPerKCost,
} from '@/components/stats/gitStatsFormat'
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

describe('efficiencyPerK', () => {
  it('divides each spend quantity by net/1000', () => {
    // 2500 net lines → 2.5 units → 1M tokens = 400K per unit.
    const eff = efficiencyPerK(2500, { total: 1_000_000, credit: 250, costUsd: 5 })
    expect(eff).toEqual({ tokens: 400_000, credit: 100, costUsd: 2 })
  })

  it('scales linearly: half the net lines doubles the per-1K spend', () => {
    const a = efficiencyPerK(2000, { total: 400_000, credit: 40, costUsd: 4 })!
    const b = efficiencyPerK(1000, { total: 400_000, credit: 40, costUsd: 4 })!
    expect(b.tokens).toBe(a.tokens * 2)
    expect(b.credit).toBe(a.credit * 2)
    expect(b.costUsd).toBe(a.costUsd * 2)
  })

  it('returns null when net is zero or negative — the ratio is undefined', () => {
    const usage = { total: 1000, credit: 1, costUsd: 0.1 }
    expect(efficiencyPerK(0, usage)).toBeNull()
    expect(efficiencyPerK(-500, usage)).toBeNull()
  })

  it('returns null when no usage totals were loaded', () => {
    expect(efficiencyPerK(5000, null)).toBeNull()
  })
})

describe('per-1K formatters', () => {
  it('tokens use the shared K/M tiers', () => {
    expect(formatPerKTokens(1234)).toBe('1.2K')
    expect(formatPerKTokens(400_000)).toBe('400.0K')
    expect(formatPerKTokens(2_500_000)).toBe('2.5M')
  })

  it('credit keeps at most two decimals', () => {
    expect(formatPerKCredit(100)).toBe('100')
    expect(formatPerKCredit(12.3456)).toBe('12.35')
  })

  it('cost always shows cents, and sub-cent values report the bound', () => {
    expect(formatPerKCost(2)).toBe('$2.00')
    expect(formatPerKCost(0.5)).toBe('$0.50')
    // Rounding to "$0.00" would read as free.
    expect(formatPerKCost(0.004)).toBe('<$0.01')
    expect(formatPerKCost(0)).toBe('$0.00')
  })
})
