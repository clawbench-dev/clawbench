import type { EChartsCoreOption } from 'echarts/core'
import { gt } from '@/composables/useLocale'
import { resolveStatsPalette, isNarrowScreen } from '@/components/stats/statsChart'
import { formatLineCount } from '@/components/stats/gitStatsFormat'

/**
 * Palette for git-stat chart series:
 *   added (green), deleted (red), net (accent blue)
 */
const GIT_SERIES_COLORS = ['#34d399', '#fb7185', '#4f8cff']

/**
 * Per-day multi-series line option for added/deleted/net line counts.
 * Series must be in the same order as the tooltip labels — see
 * GIT_SERIES_COLORS. `netValues` is optional; when omitted the net line is
 * not drawn.
 */
export function buildGitTrendOption(
  days: string[],
  addedValues: number[],
  deletedValues: number[],
  addedLabel: string,
  deletedLabel: string,
  netValues?: number[],
  netLabel?: string,
): EChartsCoreOption {
  const p = resolveStatsPalette()
  const narrow = isNarrowScreen()
  const series = [
    { type: 'line' as const, name: addedLabel, data: addedValues, smooth: true, symbolSize: 5, lineStyle: { width: 2 } },
    { type: 'line' as const, name: deletedLabel, data: deletedValues, smooth: true, symbolSize: 5, lineStyle: { width: 2 } },
  ]
  if (netValues && netLabel) {
    series.push({ type: 'line' as const, name: netLabel, data: netValues, smooth: true, symbolSize: 5, lineStyle: { width: 2 } })
  }
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
    series,
  }
}

/**
 * Horizontal bar chart of code-line inventory by language (cloc). The code
 * column is the primary axis; tooltips show the full code/comment/blank/file
 * breakdown.
 */
export function buildClocBarOption(
  languages: { name: string; files: number; code: number; comment: number; blank: number }[],
): EChartsCoreOption {
  const p = resolveStatsPalette()
  const narrow = isNarrowScreen()
  const categories = languages.map(l => l.name)
  const values = languages.map(l => l.code)
  const many = categories.length > 8
  const opt: Record<string, unknown> = {
    tooltip: {
      trigger: 'axis',
      axisPointer: { type: 'shadow' },
      formatter: (params: unknown) => {
        const list = params as { name: string; dataIndex: number }[]
        if (!list.length) return ''
        const idx = list[0].dataIndex
        const l = languages[idx]
        return `${l.name}<br/>${l.code.toLocaleString()} code · ${l.comment.toLocaleString()} comment · ${l.blank.toLocaleString()} blank · ${l.files.toLocaleString()} files`
      },
    },
    grid: { left: 8, right: narrow ? 8 : 24, top: 16, bottom: many ? 28 : 8, containLabel: true },
    xAxis: {
      type: 'value',
      axisLabel: narrow ? { show: false } : { color: p.textSecondary, formatter: (v: number) => formatLineCount(v) },
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
      barMaxWidth: 18,
      label: {
        show: true,
        position: 'right',
        color: p.textSecondary,
        fontSize: 10,
        formatter: (pp: unknown) => formatLineCount((pp as { value: number }).value),
      },
    }],
  }
  if (many) {
    opt.dataZoom = [{
      type: 'inside',
      yAxisIndex: 0,
      start: 0,
      end: Math.max(12, Math.round((8 / categories.length) * 100)),
    }]
  }
  return opt as EChartsCoreOption
}
