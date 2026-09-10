import type { EChartsCoreOption } from 'echarts/core'
import { gt } from '@/composables/useLocale'
import type { UsageMetricId, UsageRow } from '@/composables/useUsageStats'

// Sentinel the backend uses for a missing group label
// (internal/service/usage_stats.go emptyGroupLabel). Keep in sync on both
// sides — a change there silently breaks the empty-cell rendering below.
export const EMPTY_GROUP_LABEL = '(empty)'

/** Read theme colors live from CSS variables so charts match the UI theme. */
export function resolveStatsPalette() {
  const read = (name: string, fallback: string): string => {
    try {
      const v = getComputedStyle(document.documentElement).getPropertyValue(name).trim()
      return v || fallback
    } catch {
      return fallback
    }
  }
  return {
    accent: read('--accent-color', '#4f8cff'),
    text: read('--text-primary', '#1f2328'),
    textSecondary: read('--text-secondary', '#656d76'),
    axisLine: read('--border-color', '#d0d7de'),
  }
}

/** Palette for chart series (distinct enough in both light and dark). */
const SERIES_COLORS = ['#4f8cff', '#34d399', '#fbbf24', '#f472b6', '#a78bfa', '#22d3ee', '#fb7185', '#4ade80']

/**
 * Whether we are on a narrow (mobile) viewport. Mirrors the app's wide-screen
 * threshold (WIDE_SCREEN_MIN_WIDTH = 1024) so charts share the same split.
 * On narrow screens value-axis tick text is dropped — tooltips/labels still
 * show the exact numbers — while category/date labels are kept.
 */
export function isNarrowScreen(): boolean {
  try {
    return typeof window === 'undefined' || window.innerWidth < 1024
  } catch {
    return false
  }
}

/** Build a horizontal bar chart option (one per selected metric column). */
export function buildBarOption(categories: string[], values: number[], metric: UsageMetricId): EChartsCoreOption {
  const p = resolveStatsPalette()
  // On narrow screens a long category list would stretch the card very tall.
  // Cap the visible rows and enable inside scrolling once the list is long.
  const many = categories.length > 8
  const narrow = isNarrowScreen()
  const opt: Record<string, unknown> = {
    tooltip: { trigger: 'axis', axisPointer: { type: 'shadow' }, valueFormatter: (v: unknown) => formatMetricValue(metric, v as number) },
    grid: { left: 8, right: narrow ? 8 : 24, top: 16, bottom: many ? 28 : 8, containLabel: true },
    xAxis: {
      type: 'value',
      // Bar values are already printed on the right of each bar, so the value
      // axis ticks are redundant on narrow screens — keep them on desktop.
      // Token metrics get the same K/M formatting as the bars/labels.
      axisLabel: narrow ? { show: false } : { color: p.textSecondary, formatter: (v: number) => formatMetricValue(metric, v) },
      splitLine: { lineStyle: { color: p.axisLine, opacity: 0.5 } },
    },
    yAxis: {
      type: 'category',
      data: categories,
      axisLabel: { color: p.textSecondary, width: narrow ? 96 : 130, overflow: 'truncate' },
      axisLine: { lineStyle: { color: p.axisLine } },
    },
    series: [{
      type: 'bar',
      data: values,
      itemStyle: { color: p.accent, borderRadius: [0, 3, 3, 0] },
      barMaxWidth: 22,
      label: {
        show: true,
        position: 'right',
        color: p.textSecondary,
        fontSize: 10,
        // Raw token counts (input/output/…) rendered in the same K/M tiers as
        // the table so the chart never disagrees with the detail numbers.
        formatter: (pp: unknown) => formatMetricValue(metric, (pp as { value: number }).value),
      },
    }],
  }
  if (many) {
    // Inside horizontal scroll: start showing the top (first) rows.
    opt.dataZoom = [{
      type: 'inside',
      yAxisIndex: 0,
      start: 0,
      end: Math.max(12, Math.round((8 / categories.length) * 100)),
    }]
  }
  return opt as EChartsCoreOption
}

/** Build a pie/donut option (one per selected metric column). */
export function buildPieOption(categories: string[], values: number[], metric: UsageMetricId): EChartsCoreOption {
  const p = resolveStatsPalette()
  const total = values.reduce((a, b) => a + b, 0)
  const data = categories
    .map((name, i) => ({ name, value: values[i] }))
    .filter(d => d.value > 0)
  const showLegend = data.length <= 8
  return {
    tooltip: {
      trigger: 'item',
      formatter: (params: unknown) => {
        const { name, value, percent } = params as { name: string; value: number; percent: number }
        return `${name}<br/>${formatMetricValue(metric, value)} (${percent}%)`
      },
    },
    legend: showLegend ? { bottom: 0, textStyle: { color: p.textSecondary }, type: 'scroll' } : { show: false },
    color: SERIES_COLORS,
    title: total <= 0 ? { text: gt('stats.noData'), left: 'center', top: 'middle', textStyle: { color: p.textSecondary, fontSize: 12 } } : undefined,
    series: [{
      type: 'pie',
      radius: ['40%', '68%'],
      center: ['50%', '46%'],
      data,
      label: { show: showLegend, color: p.textSecondary, fontSize: 10, formatter: '{b}' },
    }],
  }
}

/** Build a per-day line/trend option for one metric. */
export function buildTrendOption(
  days: string[],
  seriesList: { label: string; values: number[] }[],
  metric: UsageMetricId,
): EChartsCoreOption {
  const p = resolveStatsPalette()
  const narrow = isNarrowScreen()
  return {
    tooltip: { trigger: 'axis', valueFormatter: (v: unknown) => formatMetricValue(metric, v as number) },
    legend: { type: 'scroll', bottom: 0, textStyle: { color: p.textSecondary } },
    grid: { left: 8, right: 8, top: 24, bottom: 32, containLabel: true },
    xAxis: {
      type: 'category',
      data: days,
      // Keep dates as the reference even on narrow; just avoid overlaps.
      axisLabel: { color: p.textSecondary, hideOverlap: true, fontSize: narrow ? 10 : undefined },
      axisLine: { lineStyle: { color: p.axisLine } },
    },
    // The value axis has no printed per-point labels; its tick text is noise on
    // narrow screens (hover tooltip gives exact values) — hidden on mobile.
    // Token metrics share the K/M formatting with the tooltip.
    yAxis: {
      type: 'value',
      axisLabel: narrow ? { show: false } : { color: p.textSecondary, formatter: (v: number) => formatMetricValue(metric, v) },
      splitLine: { lineStyle: { color: p.axisLine, opacity: 0.5 } },
    },
    color: SERIES_COLORS,
    series: seriesList.map(s => ({
      type: 'line' as const,
      name: s.label,
      data: s.values,
      smooth: true,
      symbolSize: 5,
      lineStyle: { width: 2 },
    })),
  }
}

// ── Overview donut (input vs output) + cache-drilldown donut ──

/**
 * Compact token counts: raw below 1K, K up to 1M, M up to 1B, B above — each
 * scaled tier keeps one decimal. Used for token metrics
 * (input/output/total/cacheHit…) in cards, tables, chart tooltips and donut
 * labels so all views share one format. Rounding to the nearest integer first
 * keeps boundary values honest: 999,950 → 1.0M, 999.7 → 1.0K (never "1000.0K"
 * or "1,000").
 */
function formatTokenCount(v: number): string {
  const n = Math.round(v)
  // Pick the tier from the ROUNDED scaled value so near-boundary numbers can't
  // render as "1000.0K" or "1,000": 999,950 → 1.0M, 999.7 → 1.0K,
  // 999,500,000 → 1.0B. (999,500..999,949 keeps K, 999.5M..999.949M keeps M —
  // both print the accurate 3-decimal value.)
  if (n >= 999_500_000) return `${(n / 1e9).toFixed(1)}B`
  if (n >= 999_950) return `${(n / 1e6).toFixed(1)}M`
  if (n >= 999.5) return `${(n / 1e3).toFixed(1)}K`
  return n.toLocaleString()
}

const labelFormatter = (name: string, value: number): string =>
  `${name}\n${formatTokenCount(value)}`

/**
 * Build the totals-overview donut: input vs output slices. Clicking a slice
 * drills into the input slice's cache composition (see buildCacheDonut).
 * Show a title when only one side is non-zero.
 */
export function buildOverviewDonut(input: number, output: number, inputLabel: string, outputLabel: string): EChartsCoreOption {
  const p = resolveStatsPalette()
  const data = [
    { name: inputLabel, value: input },
    { name: outputLabel, value: output },
  ].filter(d => d.value > 0)
  const total = input + output
  const oneSideOnly = data.length <= 1
  return {
    tooltip: {
      trigger: 'item',
      formatter: (params: unknown) => {
        const { name, value } = params as { name: string; value: number }
        return labelFormatter(name, value)
      },
    },
    color: [p.accent, SERIES_COLORS[1]],
    title: oneSideOnly && total > 0
      ? { text: gt('stats.onlyOneSide'), left: 'center', top: 4, textStyle: { color: p.textSecondary, fontSize: 11 } }
      : undefined,
    legend: { bottom: 0, textStyle: { color: p.textSecondary }, icon: 'circle', itemWidth: 8, itemHeight: 8 },
    series: [{
      type: 'pie',
      radius: ['45%', '72%'],
      center: ['50%', '44%'],
      data,
      label: { show: false },
      emphasis: { scaleSize: 6 },
    }],
  }
}

/**
 * Cache composition donut for the input slice: cache hit vs miss. The parent
 * shows this when the user clicks the input slice of the overview donut.
 */
export function buildCacheDonut(hit: number, miss: number, hitLabel: string, missLabel: string): EChartsCoreOption {
  const p = resolveStatsPalette()
  const data = [
    { name: hitLabel, value: hit },
    { name: missLabel, value: miss },
  ].filter(d => d.value > 0)
  const noData = data.length === 0
  return {
    tooltip: {
      trigger: 'item',
      formatter: (params: unknown) => {
        const { name, value } = params as { name: string; value: number }
        return labelFormatter(name, value)
      },
    },
    color: [SERIES_COLORS[2], SERIES_COLORS[5]],
    title: noData ? { text: gt('stats.noData'), left: 'center', top: 'middle', textStyle: { color: p.textSecondary, fontSize: 12 } } : undefined,
    legend: noData ? undefined : { bottom: 0, textStyle: { color: p.textSecondary }, icon: 'circle', itemWidth: 8, itemHeight: 8 },
    series: [{
      type: 'pie',
      radius: ['45%', '72%'],
      center: ['50%', '44%'],
      data,
      label: { show: false },
      emphasis: { scaleSize: 6 },
    }],
  }
}

/** Human readable value for tooltips/labels, consistent with the table. */
export function formatMetricValue(metric: UsageMetricId, v: number): string {
  if (v == null || Number.isNaN(v)) return '—'
  switch (metric) {
    case 'credit': return `${v.toLocaleString('en-US', { maximumFractionDigits: 4 })}`
    case 'cost':
      // Cost is always shown with exactly two decimals (thousands separator
      // kept for large totals). Sub-cent amounts round to $0.00 — the raw
      // precision is still available in the per-message metadata modal.
      return `$${v.toLocaleString('en-US', { minimumFractionDigits: 2, maximumFractionDigits: 2 })}`
    default:
      // input / output / total / cacheHit — raw token counts compacted to
      // K/M tiers (unit shown on the labels, e.g. "输入 Tokens").
      return formatTokenCount(v)
  }
}

/** Compute an ECharts row-series value for a metric. */
export function rowValueOf(row: UsageRow, metric: UsageMetricId): number {
  switch (metric) {
    case 'input': return row.input
    case 'output': return row.output
    case 'total': return row.total
    case 'cacheHit': return row.cacheHit
    case 'credit': return row.credit
    case 'cost': return row.costUsd
  }
}
