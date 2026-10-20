import { describe, expect, it } from 'vitest'
import {
  cacheHitRate,
  formatUsageCost,
  hasTokenDetail,
  usageCategoryRows,
  usageColor,
  usagePct,
} from '../usageDisplay'

/**
 * usageDisplay is the single source of the context panel's numeric rules. It is
 * shared by the single-chat popup and the group member panel, so a regression
 * here silently changes BOTH surfaces — which is exactly why the thresholds and
 * the "no data" cases are pinned individually.
 */
describe('usagePct', () => {
  it('rounds the ratio to a whole percent', () => {
    expect(usagePct({ used: 50, size: 200 })).toBe(25)
    expect(usagePct({ used: 1, size: 3 })).toBe(33)
  })

  it('returns 0 for an unknown window instead of dividing by zero', () => {
    expect(usagePct({ used: 100, size: 0 })).toBe(0)
    expect(usagePct(null)).toBe(0)
    expect(usagePct(undefined)).toBe(0)
  })

  it('can exceed 100 when the agent reports used > size (rendered clamped)', () => {
    expect(usagePct({ used: 300, size: 200 })).toBe(150)
  })
})

describe('usageColor', () => {
  // Boundaries matter: the four steps are what tells a user "you are close to
  // the limit", so an off-by-one here changes the warning they see.
  it.each([
    [0, '#22c55e'],
    [74, '#22c55e'],
    [75, '#eab308'],
    [89, '#eab308'],
    [90, '#f97316'],
    [94, '#f97316'],
    [95, '#ef4444'],
    [100, '#ef4444'],
    [150, '#ef4444'],
  ])('%i%% → %s', (pct, expected) => {
    expect(usageColor(pct)).toBe(expected)
  })
})

describe('cacheHitRate', () => {
  it('computes hit / (hit + miss) when no full-prompt input is known', () => {
    expect(cacheHitRate({ cacheHitTokens: 326400, cacheMissTokens: 2984 })).toBeCloseTo(99.09, 1)
  })

  // The agent's own definition is cacheRead / FULL PROMPT: prompt_tokens
  // already includes both the cached and the cache-write portion. Dividing by
  // (hit + miss) alone drops the write side and reports a HIGHER rate than the
  // agent does for the same numbers.
  it('divides by the full prompt when inputTokens covers the split', () => {
    // hit 800, miss 100, write 100 → prompt = 1000.
    const usage = { cacheHitTokens: 800, cacheMissTokens: 100, inputTokens: 1000 }
    expect(cacheHitRate(usage)).toBeCloseTo(80, 5)
    // The old (hit+miss) formula would have said 88.9% here.
    expect(cacheHitRate({ cacheHitTokens: 800, cacheMissTokens: 100 })).toBeCloseTo(88.89, 1)
  })

  it('ignores an input that does not cover the split', () => {
    // A per-call input with turn-level cache counters: using it would divide by
    // a number smaller than the split and exceed 100%.
    expect(cacheHitRate({ cacheHitTokens: 900, cacheMissTokens: 300, inputTokens: 500 })).toBeCloseTo(75, 5)
  })

  it('is null when neither side is reported (row hidden, not shown as 0%)', () => {
    expect(cacheHitRate({})).toBeNull()
    expect(cacheHitRate(null)).toBeNull()
  })

  it('is 0 when only misses are reported', () => {
    expect(cacheHitRate({ cacheMissTokens: 10 })).toBe(0)
  })
})

describe('hasTokenDetail', () => {
  it('is false for a usage with only used/size', () => {
    expect(hasTokenDetail({ used: 1, size: 100 })).toBe(false)
    expect(hasTokenDetail(null)).toBe(false)
  })

  it('is true for ANY token/cost/credit field', () => {
    expect(hasTokenDetail({ used: 1, size: 100, inputTokens: 5 })).toBe(true)
    expect(hasTokenDetail({ used: 1, size: 100, thoughtTokens: 5 })).toBe(true)
    expect(hasTokenDetail({ used: 1, size: 100, credit: 0.5 })).toBe(true)
    expect(hasTokenDetail({ used: 1, size: 100, cost: 0.01 })).toBe(true)
  })
})

describe('formatUsageCost', () => {
  it('uses the currency symbol when the code is known', () => {
    expect(formatUsageCost(0.05126895, 'USD')).toBe('$0.05')
    expect(formatUsageCost(1.5, 'CNY')).toBe('¥1.50')
  })

  it('rounds sub-cent amounts to 0.00 instead of switching precision', () => {
    expect(formatUsageCost(0.000036, 'USD')).toBe('$0.00')
  })

  it('falls back to the bare code for an unknown currency', () => {
    expect(formatUsageCost(12.34, 'XYZ')).toBe('12.34 XYZ')
  })

  it('shows the bare number when no currency is reported (no fabricated $)', () => {
    // CodeBuddy ships cost.amount == credit with an empty currency.
    expect(formatUsageCost(1.43, '')).toBe('1.43')
    expect(formatUsageCost(1.43, undefined)).toBe('1.43')
  })

  it('is empty for zero / negative so the row can be hidden', () => {
    expect(formatUsageCost(0, 'USD')).toBe('')
    expect(formatUsageCost(-1, 'USD')).toBe('')
  })
})

describe('usageCategoryRows', () => {
  const t = (key: string) => key

  it('drops zero-valued components (agents emit explicit zeros)', () => {
    const rows = usageCategoryRows({ usageByCategory: { tools: 100, conversation: 0 } }, t)
    expect(rows).toEqual([{ key: 'tools', value: 100, label: 'chat.sessionInfo.catTools' }])
  })

  it('keeps the agent-reported order', () => {
    const rows = usageCategoryRows({ usageByCategory: { conversation: 1, tools: 2 } }, t)
    expect(rows.map(r => r.key)).toEqual(['conversation', 'tools'])
  })

  it('falls back to the raw key for an unknown component', () => {
    const rows = usageCategoryRows({ usageByCategory: { mystery: 7 } }, t)
    expect(rows).toEqual([{ key: 'mystery', value: 7, label: 'mystery' }])
  })

  it('is empty without a breakdown', () => {
    expect(usageCategoryRows({}, t)).toEqual([])
    expect(usageCategoryRows(null, t)).toEqual([])
  })
})
