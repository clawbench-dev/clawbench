import type { EChartsCoreOption } from 'echarts/core'
import { gt } from '@/composables/useLocale'
import { resolveStatsPalette, isNarrowScreen } from '@/components/stats/statsChart'
import { formatLineCount } from '@/components/stats/gitStatsFormat'

/** Palette for git-stat chart series. */
const GIT_SERIES_COLORS = ['#34d399', '#fb7185']

/**
 * Per-day multi-series line option for added/deleted line counts.
 * Series must be in the same order as the tooltip labels (added first, then
 * deleted) — see GIT_SERIES_COLORS.
 */
export function buildGitTrendOption(
  days: string[],
  addedValues: number[],
  deletedValues: number[],
  addedLabel: string,
  deletedLabel: string,
): EChartsCoreOption {
  const p = resolveStatsPalette()
  const narrow = isNarrowScreen()
  return {
    tooltip: {
      trigger: 'axis',
      valueFormatter: (v: unknown) => formatLineCount(v as number),
    },
    legend: { type: 'scroll', bottom: 0, textStyle: { color: p.textSecondary } },
    grid: { left: 8, right: 8, top: 24, bottom: 32, containLabel: true },
    xAxis: {
      type: 'category',
      data: days,
      axisLabel: { color: p.textSecondary, hideOverlap: true, fontSize: narrow ? 10 : undefined },
      axisLine: { lineStyle: { color: p.axisLine } },
    },
    yAxis: {
      type: 'value',
      axisLabel: narrow ? { show: false } : { color: p.textSecondary, formatter: (v: number) => formatLineCount(v) },
      splitLine: { lineStyle: { color: p.axisLine, opacity: 0.5 } },
    },
    color: GIT_SERIES_COLORS,
    title: days.length === 0
      ? { text: gt('stats.noData'), left: 'center', top: 'middle', textStyle: { color: p.textSecondary, fontSize: 12 } }
      : undefined,
    series: [
      { type: 'line' as const, name: addedLabel, data: addedValues, smooth: true, symbolSize: 5, lineStyle: { width: 2 } },
      { type: 'line' as const, name: deletedLabel, data: deletedValues, smooth: true, symbolSize: 5, lineStyle: { width: 2 } },
    ],
  }
}
