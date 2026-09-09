import type { GitStatsRow } from '@/composables/useGitCodeStats'

/** Metric id for git code-change columns / chart series. */
export type GitStatsMetricId = 'added' | 'deleted' | 'net' | 'commitCnt'

/**
 * Compact line counts: raw below 1K, K up to 1M, M above — each scaled tier
 * keeps one decimal. Sign is preserved so net (negative possible) renders
 * correctly in axis ticks and tooltips. Boundary rounding mirrors the token
 * formatter in statsChart.ts: 999,950 → 1.0M, 999.7 → 1.0K.
 */
export function formatLineCount(v: number): string {
  if (v == null || Number.isNaN(v)) return '—'
  const sign = v < 0 ? '-' : ''
  const n = Math.abs(v)
  const r = Math.round(n)
  if (r >= 999_950) return `${sign}${(n / 1e6).toFixed(1)}M`
  if (r >= 999.5) return `${sign}${(n / 1e3).toFixed(1)}K`
  return sign + r.toLocaleString()
}

/** Net delta card value: positive values get an explicit "+". */
export function formatNetDelta(v: number): string {
  if (v > 0) return `+${formatLineCount(v)}`
  return formatLineCount(v)
}

/** Raw numeric value of a row for a metric (for chart series / table cells). */
export function rowLineValueOf(row: GitStatsRow, metric: GitStatsMetricId): number {
  switch (metric) {
    case 'added': return row.added
    case 'deleted': return row.deleted
    case 'net': return row.net
    case 'commitCnt': return row.commitCnt
  }
}
