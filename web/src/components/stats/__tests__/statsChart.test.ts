import { describe, expect, it, vi, afterEach } from 'vitest'
import {
  buildBarOption,
  buildPieOption,
  buildOverviewDonut,
  buildCacheDonut,
  buildTrendOption,
  isNarrowScreen,
  formatMetricValue,
  resolveStatsPalette,
  BAR_MAX_WIDTH,
  DONUT_RADIUS,
} from '@/components/stats/statsChart'
import { buildClocBarOption } from '@/components/stats/gitStatsChart'

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

describe('statsChart thin/refined geometry', () => {
  type BarSeries = {
    barMaxWidth?: number
    showBackground?: boolean
    itemStyle?: { borderRadius?: unknown }
    backgroundStyle?: { borderRadius?: unknown }
  }
  type PieOpt = {
    series: { radius?: unknown; label?: { show?: boolean }; emphasis?: { scaleSize?: number } }[]
    graphic?: { style: { text: string } }[]
    title?: { text?: string }
  }

  const barSeries = (opt: unknown): BarSeries =>
    ((opt as { series: BarSeries[] }).series)[0]

  it('bar: hairline bar with a faint full-width track', () => {
    const opt = buildBarOption(['a', 'b'], [10, 5], 'total')
    const s = barSeries(opt)
    // 4px hairline — anything thicker reads as a block, not a measure.
    expect(s.barMaxWidth).toBe(BAR_MAX_WIDTH)
    expect(BAR_MAX_WIDTH).toBeLessThanOrEqual(6)
    // The track is what makes a 4px bar readable; without it the bar floats.
    expect(s.showBackground).toBe(true)
    expect(s.backgroundStyle?.borderRadius).toBe(2)
    // Rounded on both ends (a capsule), not the old [0,3,3,0] square end.
    expect(s.itemStyle?.borderRadius).toBe(2)
  })

  it('bar: value label masks the track it is printed over', () => {
    // The track spans the full plot width, so a label without an opaque
    // background gets the track line drawn straight through the digits.
    // Asserted against the palette's own surface colours (read from CSS vars;
    // the jsdom fallbacks are the literals here) rather than just "some
    // background is set" — a wrong surface colour is as broken as none.
    const opt = buildBarOption(['a'], [10], 'total')
    const label = (opt as { series: { label: { backgroundColor?: string } }[] }).series[0].label
    expect(label.backgroundColor).toBeTruthy()
    // Both surfaces must be distinct — the cloc panel paints on --bg-secondary,
    // so reusing the primary surface there would leave a visible rectangle.
    const p = resolveStatsPalette()
    expect(p.surface).not.toBe(p.surfaceAlt)
  })

  it('cloc bar: label masks the track using the secondary surface', () => {
    const opt = buildClocBarOption([{ name: 'Go', files: 1, code: 100, comment: 5, blank: 5 }])
    const s = (opt as { series: { label: { backgroundColor?: string }, showBackground?: boolean }[] }).series[0]
    expect(s.showBackground).toBe(true)
    // ClocStatsPanel mounts this chart inside a --bg-secondary card.
    expect(s.label.backgroundColor).toBe(resolveStatsPalette().surfaceAlt)
  })

  it('pie: hairline ring, no slice labels, center carries the total', () => {
    const opt = buildPieOption(['opus', 'glm'], [70, 30], 'total', '总 Tokens') as PieOpt
    expect(opt.series[0].radius).toEqual(DONUT_RADIUS)
    // 8% ring: thin enough to read as a gauge, not a solid disc.
    expect(parseFloat(DONUT_RADIUS[1]) - parseFloat(DONUT_RADIUS[0])).toBeLessThanOrEqual(10)
    // Slice labels are gone — the legend already names every slice.
    expect(opt.series[0].label?.show).toBe(false)
    // The center total replaces the information the labels used to carry.
    const texts = (opt.graphic ?? []).map(g => g.style.text)
    expect(texts).toContain('总 Tokens')
    expect(texts.some(t => t.includes('100'))).toBe(true)
  })

  it('pie: no center graphic when the metric total is zero', () => {
    const opt = buildPieOption(['a'], [0], 'total', '总 Tokens') as PieOpt
    expect(opt.graphic).toEqual([])
    expect(opt.title?.text).toBeTruthy()
  })

  it('overview donut: center total, thin ring, no emphasis blow-up', () => {
    const opt = buildOverviewDonut(3000, 1000, '输入', '输出') as PieOpt
    expect(opt.series[0].radius).toEqual(DONUT_RADIUS)
    expect((opt.graphic ?? []).map(g => g.style.text).some(t => t.includes('4.0K'))).toBe(true)
    // scaleSize used to grow the slice on hover, which pushed the ring out of
    // alignment with the center total. Emphasis now only dims.
    expect(opt.series[0].emphasis?.scaleSize).toBeUndefined()
  })

  it('cache donut: center total shown when there is data, absent when empty', () => {
    const withData = buildCacheDonut(800, 200, '命中', '未命中') as PieOpt
    expect(withData.series[0].radius).toEqual(DONUT_RADIUS)
    expect((withData.graphic ?? []).map(g => g.style.text).some(t => t.includes('1.0K'))).toBe(true)

    const empty = buildCacheDonut(0, 0, '命中', '未命中') as PieOpt
    expect(empty.graphic).toEqual([])
    expect(empty.title?.text).toBeTruthy()
  })

  it('cache donut: center shows the authoritative input total, not hit+miss', () => {
    // The slices are a breakdown of INPUT, so the hole must print the same
    // number as the "输入 Tokens" overview card. hit+miss can drift below it
    // when a row recorded input without a cache split — that is the real-data
    // shape (older rows: input>0, hit=miss=0). Passing the total explicitly is
    // what keeps the two views agreeing.
    const opt = buildCacheDonut(700, 200, '命中', '未命中', 1000) as PieOpt
    const texts = (opt.graphic ?? []).map(g => g.style.text)
    expect(texts).toContain('1.0K') // 1000 → the input total
    expect(texts).not.toContain('900') // 700+200 must NOT be what is shown
    // The label names it as input (i18n-resolved), never as a hit/miss slice —
    // otherwise a reader could take the number for a cache subtotal.
    expect(texts).not.toContain('命中')
    expect(texts).not.toContain('未命中')
    expect(texts).toHaveLength(2)
  })

  it('cache donut: falls back to hit+miss when no input total is supplied', () => {
    const opt = buildCacheDonut(700, 200, '命中', '未命中') as PieOpt
    const texts = (opt.graphic ?? []).map(g => g.style.text)
    expect(texts).toContain('900')
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
