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

  it('bar: mobile moves category axis into legend, desktop keeps axes', () => {
    const cats = ['glm', 'opus']
    const vals = [5, 3]
    setInnerWidth(800)
    const narrow = buildBarOption(cats, vals, 'total') as {
      legend?: { data?: string[] }
      xAxis: { show?: boolean }
      yAxis: { show?: boolean }
      series: { name: string; data: (number | null)[] }[]
    }
    // Mobile: one coloured series per category + bottom legend carrying the
    // category names; the category (left) axis is hidden so bars get full width.
    expect(narrow.series).toHaveLength(2)
    expect(narrow.series.map(s => s.name)).toEqual(cats)
    expect(narrow.yAxis.show).toBe(false)
    // Each series paints exactly one row (its own value, null elsewhere).
    expect(narrow.series[0].data).toEqual([5, null])
    expect(narrow.series[1].data).toEqual([null, 3])
    expect(narrow.legend?.data).toEqual(cats)

    setInnerWidth(1440)
    const wide = buildBarOption(cats, vals, 'total') as {
      xAxis: { show?: boolean }
      yAxis: { data: string[] }
      series: { data: number[] }[]
    }
    // Desktop: single-series bar with the value axis as ruler.
    expect(wide.xAxis.show).toBe(true)
    expect(wide.yAxis.data).toEqual(cats)
    expect(wide.series).toHaveLength(1)
  })

  it('bar: mobile keeps the labelled category axis when rows exceed legend capacity', () => {
    const manyCats = Array.from({ length: 12 }, (_, i) => `m${i}`)
    const vals = manyCats.map((_, i) => i)
    setInnerWidth(800)
    const narrow = buildBarOption(manyCats, vals, 'total') as {
      xAxis: { show?: boolean }
      yAxis: { show?: boolean; data: string[] }
      series: { data: number[] }[]
    }
    // Too many categories for colour legend → stay on the labelled single bar.
    expect(narrow.series).toHaveLength(1)
    expect(narrow.yAxis.data).toEqual(manyCats)
    expect(narrow.xAxis.show).toBe(false)
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
})
