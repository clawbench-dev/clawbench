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

/**
 * i18n key naming the measured quantity, for every surface that puts a word in
 * front of the number (header capsule, phone row).
 *
 * The capsule used to say "Usage" regardless of the configured metric, so a
 * budget of "$20" and one of "20 tokens" were labelled identically — the word
 * carried no information the number did not already carry, and the unit was
 * only visible from the currency sign.
 *
 * Falls back to the generic feature label when the metric is unknown (a rule
 * that is off, or an older payload that omits it).
 */
export function usageMeterMetricLabelKey(metric: UsageMeterMetric | undefined): string {
  switch (metric) {
    case 'tokens':
      return 'usageMeter.metricTokens'
    case 'credit':
      return 'usageMeter.metricCredit'
    case 'cost':
      return 'usageMeter.metricCost'
    default:
      return 'usageMeter.label'
  }
}

/**
 * i18n key for the popover's heading: the measured quantity followed by the
 * feature noun ("Cost usage", "费用用量").
 *
 * A fixed "Usage" heading sat directly above a figure that could be $20 or 20
 * tokens, so the panel never said which of the two it was showing. The metric
 * is the only thing that distinguishes those rules, so it belongs in the title.
 *
 * Falls back to the plain feature title when the metric is unknown (a rule that
 * is off, or an older payload that omits it).
 */
export function usageMeterTitleKey(metric: UsageMeterMetric | undefined): string {
  switch (metric) {
    case 'tokens':
      return 'usageMeter.titleTokens'
    case 'credit':
      return 'usageMeter.titleCredit'
    case 'cost':
      return 'usageMeter.titleCost'
    default:
      return 'usageMeter.title'
  }
}

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
 * Compact value for space-constrained surfaces (header capsule, phone row):
 * K/M/B tiers with exactly one decimal, and no trailing ".0" below the K tier.
 *
 * One decimal is kept on the tiers even when it is zero ("5.0M") so the
 * capsule does not change width as the number moves.
 */
function formatCompactDecimal(v: number): string {
  const abs = Math.abs(v)
  if (abs >= 999_500_000) return `${(v / 1e9).toFixed(1)}B`
  if (abs >= 999_950) return `${(v / 1e6).toFixed(1)}M`
  if (abs >= 999.5) return `${(v / 1e3).toFixed(1)}K`
  const one = v.toFixed(1)
  return one.endsWith('.0') ? one.slice(0, -2) : one
}

/**
 * Format a meter value for the header capsule and popover.
 *
 * `compact` is for the space-constrained surfaces (the header capsule and the
 * phone row inside the system-resources popover); the popover body uses the
 * full form so exact amounts are readable next to the comparison bars.
 *
 * Tokens are always compacted: an exact token count is noise at any size, and
 * the K/M/B thresholds mirror the backend's compactCount
 * (internal/service/usage_meter.go) so a notification and the UI never
 * disagree about the same number.
 */
export function formatUsageMeterValue(
  metric: UsageMeterMetric,
  value: number,
  opts: { compact?: boolean } = {},
): string {
  if (value == null || Number.isNaN(value)) return '—'
  if (metric === 'tokens') return formatCompactCount(value)
  if (opts.compact) {
    const prefix = metric === 'cost' ? '$' : ''
    return prefix + formatCompactDecimal(value)
  }
  switch (metric) {
    case 'cost':
      return `$${value.toLocaleString('en-US', {
        minimumFractionDigits: 2,
        maximumFractionDigits: 2,
      })}`
    case 'credit':
    default:
      return value.toLocaleString('en-US', { maximumFractionDigits: 2 })
  }
}
