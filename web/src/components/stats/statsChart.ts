import type { EChartsCoreOption } from 'echarts/core'
import { gt } from '@/composables/useLocale'
import type { UsageMetricId, UsageRow } from '@/composables/useUsageStats'

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

/** Build a horizontal bar chart option (one per selected metric column). */
export function buildBarOption(categories: string[], values: number[], metric: UsageMetricId): EChartsCoreOption {
  const p = resolveStatsPalette()
  return {
    tooltip: { trigger: 'axis', axisPointer: { type: 'shadow' }, valueFormatter: (v: unknown) => formatMetricValue(metric, v as number) },
    grid: { left: 8, right: 24, top: 16, bottom: 8, containLabel: true },
    xAxis: { type: 'value', axisLabel: { color: p.textSecondary }, splitLine: { lineStyle: { color: p.axisLine, opacity: 0.5 } } },
    yAxis: {
      type: 'category',
      data: categories,
      axisLabel: { color: p.textSecondary, width: 130, overflow: 'truncate' },
      axisLine: { lineStyle: { color: p.axisLine } },
    },
    series: [{
      type: 'bar',
      data: values,
      itemStyle: { color: p.accent, borderRadius: [0, 3, 3, 0] },
      barMaxWidth: 22,
      label: { show: true, position: 'right', color: p.textSecondary, fontSize: 10 },
    }],
  }
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
  return {
    tooltip: { trigger: 'axis', valueFormatter: (v: unknown) => formatMetricValue(metric, v as number) },
    legend: { type: 'scroll', bottom: 0, textStyle: { color: p.textSecondary } },
    grid: { left: 8, right: 16, top: 24, bottom: 32, containLabel: true },
    xAxis: { type: 'category', data: days, axisLabel: { color: p.textSecondary }, axisLine: { lineStyle: { color: p.axisLine } } },
    yAxis: { type: 'value', axisLabel: { color: p.textSecondary }, splitLine: { lineStyle: { color: p.axisLine, opacity: 0.5 } } },
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

/** Human readable value for tooltips/labels, consistent with the table. */
export function formatMetricValue(metric: UsageMetricId, v: number): string {
  if (v == null || Number.isNaN(v)) return '—'
  switch (metric) {
    case 'hitRate': return `${(v * 100).toFixed(1)}%`
    case 'credit': return `${v.toLocaleString('en-US', { maximumFractionDigits: 4 })}`
    case 'cost': {
      const abs = Math.abs(v)
      if (abs > 0 && abs < 0.0001) return '$0.0001'
      return `$${v.toLocaleString('en-US', { minimumFractionDigits: 2, maximumFractionDigits: 6 })}`
    }
    default:
      return Math.round(v).toLocaleString()
  }
}

/** Compute an ECharts row-series value for a metric (hitRate derived). */
export function rowValueOf(row: UsageRow, metric: UsageMetricId): number {
  switch (metric) {
    case 'input': return row.input
    case 'output': return row.output
    case 'total': return row.total
    case 'cacheHit': return row.cacheHit
    case 'hitRate': {
      const den = row.cacheHit + row.cacheMiss
      return den > 0 ? row.cacheHit / den : 0
    }
    case 'credit': return row.credit
    case 'cost': return row.costUsd
  }
}
