import { describe, expect, it, vi, afterEach } from 'vitest'
import {
  buildBarOption,
  buildTrendOption,
  isNarrowScreen,
  formatMetricValue,
} from '@/components/stats/statsChart'

function setInnerWidth(w: number) {
  Object.defineProperty(window, 'innerWidth', { configurable: true, value: w })
}

describe('statsChart axis labels (mobile vs desktop)', () => {
  const origWidth = window.innerWidth
  afterEach(() => {
    setInnerWidth(origWidth)
    vi.restoreAllMocks()
  })

  it('treats <1024px as narrow', () => {
    setInnerWidth(800)
    expect(isNarrowScreen()).toBe(true)
    setInnerWidth(1440)
    expect(isNarrowScreen()).toBe(false)
  })

  it('bar: value-axis ticks hidden on narrow, shown on desktop', () => {
    const cats = ['glm', 'opus']
    const vals = [5, 3]
    setInnerWidth(800)
    const narrow = buildBarOption(cats, vals, 'total') as { xAxis: { axisLabel: { show?: boolean } } }
    expect((narrow.xAxis.axisLabel as { show: boolean }).show).toBe(false)
    // Category labels (which bar is which) are always kept.
    const wide = buildBarOption(cats, vals, 'total') as {
      yAxis: { data: string[]; axisLabel: { width?: number } }
    }
    expect(wide.yAxis.data).toEqual(cats)
  })

  it('trend: value-axis ticks hidden on narrow, dates kept with hideOverlap', () => {
    setInnerWidth(800)
    const narrow = buildTrendOption(['2026-09-01', '2026-09-02'], [{ label: 'glm', values: [1, 2] }], 'total') as {
      yAxis: { axisLabel: { show?: boolean } }
      xAxis: { axisLabel: { hideOverlap?: boolean } }
    }
    expect((narrow.yAxis.axisLabel as { show: boolean }).show).toBe(false)
    expect(narrow.xAxis.axisLabel.hideOverlap).toBe(true)
  })
})

describe('formatMetricValue token tiers (raw / K / M / B)', () => {
  it('raw below 1K, one K decimal from 1K up, one M decimal from 1M up', () => {
    expect(formatMetricValue('total', 0)).toBe('0')
    expect(formatMetricValue('total', 5)).toBe('5')
    expect(formatMetricValue('total', 999)).toBe('999')
    expect(formatMetricValue('total', 1000)).toBe('1.0K')
    expect(formatMetricValue('total', 1234)).toBe('1.2K')
    expect(formatMetricValue('total', 999900)).toBe('999.9K')
    expect(formatMetricValue('total', 1_000_000)).toBe('1.0M')
    expect(formatMetricValue('total', 1_234_567)).toBe('1.2M')
    expect(formatMetricValue('total', 12_300_000)).toBe('12.3M')
  })

  it('one B decimal from 1B up', () => {
    expect(formatMetricValue('total', 1_000_000_000)).toBe('1.0B')
    expect(formatMetricValue('total', 1_234_567_890)).toBe('1.2B')
    expect(formatMetricValue('total', 12_300_000_000)).toBe('12.3B')
  })

  it('near-boundary values roll into the next tier instead of 1000.0K / 1000', () => {
    expect(formatMetricValue('total', 999_950)).toBe('1.0M')
    expect(formatMetricValue('total', 999_999)).toBe('1.0M')
    expect(formatMetricValue('input', 999.7)).toBe('1.0K')
    expect(formatMetricValue('output', 12_499_999)).toBe('12.5M')
    expect(formatMetricValue('total', 999_500_000)).toBe('1.0B')
    expect(formatMetricValue('total', 999_499_999)).toBe('999.5M')
  })

  it('applies to all token metrics (input/output/total/cacheHit) but not credit/cost', () => {
    expect(formatMetricValue('input', 2_500_000)).toBe('2.5M')
    expect(formatMetricValue('output', 800)).toBe('800')
    expect(formatMetricValue('cacheHit', 1_500_000)).toBe('1.5M')
    // Non-token metrics keep their existing formatting.
    expect(formatMetricValue('cost', 0.05)).toBe('$0.05')
    expect(formatMetricValue('credit', 1234)).toBe('1,234')
  })

  it('formats cost with exactly two decimals', () => {
    // Sub-cent amounts round to $0.00 instead of switching to a
    // higher-precision format (raw precision stays in the metadata modal).
    expect(formatMetricValue('cost', 0.000036)).toBe('$0.00')
    expect(formatMetricValue('cost', 0.001888)).toBe('$0.00')
    expect(formatMetricValue('cost', 0.006671479)).toBe('$0.01')
    expect(formatMetricValue('cost', 0.05126895)).toBe('$0.05')
    expect(formatMetricValue('cost', 1.43)).toBe('$1.43')
    // A thousands separator is kept for large totals.
    expect(formatMetricValue('cost', 8283.125)).toBe('$8,283.13')
    expect(formatMetricValue('cost', 0)).toBe('$0.00')
  })
})
