import type { GitStatsRow, GitUsageTotals, GitEfficiencyPerK } from '@/composables/useGitCodeStats'
import { formatCompactCount } from '@/utils/usageFormat'

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

/**
 * Spend per 1K net added lines for the same window, or null when the ratio is
 * undefined.
 *
 * `net <= 0` (deletions met or exceeded additions) makes "per 1K net lines"
 * meaningless — a negative or zero denominator would render as a huge negative
 * number or Infinity, which reads as real data. The panel shows a hint instead.
 */
export function efficiencyPerK(net: number, usage: GitUsageTotals | null): GitEfficiencyPerK | null {
  if (!usage || net <= 0) return null
  const units = net / 1000
  return {
    tokens: usage.total / units,
    credit: usage.credit / units,
    costUsd: usage.costUsd / units,
  }
}

/** Tokens per 1K lines — same K/M/B tiers as the other token surfaces. */
export function formatPerKTokens(v: number): string {
  return formatCompactCount(v)
}

/** Credit per 1K lines: up to two decimals, no trailing zeros beyond that. */
export function formatPerKCredit(v: number): string {
  return v.toLocaleString('en-US', { maximumFractionDigits: 2 })
}

/**
 * USD per 1K lines. Sub-cent values would round to "$0.00", which reads as
 * "free" — report the bound instead so a cheap model never looks costless.
 */
export function formatPerKCost(v: number): string {
  if (v > 0 && v < 0.01) return '<$0.01'
  return `$${v.toFixed(2)}`
}
