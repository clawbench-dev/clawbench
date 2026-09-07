import { describe, expect, it, vi, afterEach } from 'vitest'
import { buildBarOption, buildTrendOption, isNarrowScreen } from '@/components/stats/statsChart'

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
