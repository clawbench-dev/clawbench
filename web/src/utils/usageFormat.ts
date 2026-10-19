/**
 * Usage-meter value formatting — a zero-dependency leaf module.
 *
 * Deliberately NOT in components/stats/statsChart.ts: that module imports the
 * ECharts option type at module scope, and AppHeader is on the first-paint path.
 * Importing anything from there would pull the chart library into the initial
 * bundle (and `import type` only defers the problem until someone switches to a
 * value import).
 *
 * The K/M/B thresholds mirror the backend's compactCount
 * (internal/service/usage_meter.go) so a notification and the capsule never
 * disagree about the same number.
 */

export type UsageMeterMetric = 'tokens' | 'credit' | 'cost'

/** Compact token count: 999.5K, 1.0M, 1.0B. */
export function formatCompactCount(v: number): string {
  const n = Math.round(v)
  // Tier chosen from the ROUNDED scaled value so near-boundary numbers cannot
  // render as "1000.0K" or "1,000". Same edges as the backend.
  if (n >= 999_500_000) return `${(n / 1e9).toFixed(1)}B`
  if (n >= 999_950) return `${(n / 1e6).toFixed(1)}M`
  if (n >= 999.5) return `${(n / 1e3).toFixed(1)}K`
  return n.toLocaleString('en-US')
}

/**
 * Format a meter value for the header capsule and popover.
 *
 * `compact` is for the capsule (fixed width, no layout shift); the popover uses
 * the full form so the exact amount is readable.
 */
export function formatUsageMeterValue(
  metric: UsageMeterMetric,
  value: number,
  opts: { compact?: boolean } = {},
): string {
  if (value == null || Number.isNaN(value)) return '—'
  switch (metric) {
    case 'cost':
      return `$${value.toLocaleString('en-US', {
        minimumFractionDigits: 2,
        maximumFractionDigits: 2,
      })}`
    case 'credit':
      return opts.compact
        ? formatCompactCount(value)
        : value.toLocaleString('en-US', { maximumFractionDigits: 2 })
    case 'tokens':
    default:
      return formatCompactCount(value)
  }
}
