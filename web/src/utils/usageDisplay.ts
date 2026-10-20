// usageDisplay.ts — pure helpers behind the context-usage detail panel.
//
// Extracted from ChatInputBar's inline popup so the single-chat popup and the
// group member panel compute EXACTLY the same numbers, colours and labels. A
// second copy is what would let the two drift (e.g. one showing a cache-hit
// rate the other hides), so this module is the single source for the rules.
//
// Everything here is pure: no Vue reactivity, no i18n singleton. The label
// lookup takes the caller's `t` so the module stays testable without a plugin.

/**
 * UsageSnapshot is the wire shape shared by every usage producer:
 * `ai.UsageState` (REST `usageState` / group member `usage`), the
 * `usage_update` event payload, and the per-session frontend cache.
 *
 * All extension fields are optional because ACP agents distribute them across
 * several partial notifications within one turn — an absent field means "this
 * notification carried no new value", never "the value is zero".
 */
export interface UsageSnapshot {
  used: number
  size: number
  inputTokens?: number
  outputTokens?: number
  totalTokens?: number
  cachedReadTokens?: number
  cachedWriteTokens?: number
  thoughtTokens?: number
  cost?: number
  currency?: string
  cacheCreationTokens?: number
  cacheHitTokens?: number
  cacheMissTokens?: number
  credit?: number
  usageByCategory?: Record<string, number>
}

/** Percent of the context window in use, rounded. 0 when no window is known. */
export function usagePct(usage: Pick<UsageSnapshot, 'used' | 'size'> | null | undefined): number {
  if (!usage || usage.size <= 0) return 0
  return Math.round((usage.used / usage.size) * 100)
}

/**
 * Traffic-light colour for a usage percentage.
 *
 * Deliberately fixed hues (not theme tokens): the thresholds mean the same
 * thing in every theme, and the accent colour is orange in ~3/36 themes which
 * would collide with the warning step.
 */
export function usageColor(pct: number): string {
  if (pct >= 95) return '#ef4444'
  if (pct >= 90) return '#f97316'
  if (pct >= 75) return '#eab308'
  return '#22c55e'
}

/**
 * Cache hit rate as a percentage, matching the agent's own accounting.
 *
 * CodeBuddy (and the WebUI stats page) define the denominator as the FULL
 * prompt — `cacheReadInputTokens / inputTokens` — because its prompt_tokens
 * already include the cached portion AND the cache-write portion. Dividing by
 * (hit + miss) alone drops the write side from the denominator and reports a
 * systematically HIGHER hit rate than the agent does; on a mixed DB the two
 * figures differ by several points.
 *
 * `inputTokens` is the normalized full prompt for every backend (Claude/Codex
 * are rewritten to the same shape at ingestion, see
 * tokenUsageFromClaudeTokenCount), so it is the preferred denominator. It is
 * only trusted when it actually covers the split — a snapshot carrying a
 * per-call input with turn-level cache counters would otherwise divide by too
 * small a number. Falling back to hit+miss keeps older/partial snapshots
 * renderable rather than hiding the row.
 *
 * Returns null when nothing is reported — the row is hidden entirely rather
 * than showing a fabricated 0%.
 */
export function cacheHitRate(usage: Pick<UsageSnapshot, 'cacheHitTokens' | 'cacheMissTokens' | 'inputTokens'> | null | undefined): number | null {
  const hit = usage?.cacheHitTokens ?? 0
  const miss = usage?.cacheMissTokens ?? 0
  const input = usage?.inputTokens ?? 0
  if (hit <= 0 && miss <= 0) return null
  const split = hit + miss
  const denominator = input > 0 && input >= split ? input : split
  if (denominator <= 0) return 0
  return (hit / denominator) * 100
}

/**
 * True when any token/cost row inside the "Token detail" section has a value.
 * The section header only renders then — otherwise the panel would show an
 * empty header between used/size/remaining and the context breakdown.
 */
export function hasTokenDetail(usage: UsageSnapshot | null | undefined): boolean {
  if (!usage) return false
  return (
    (usage.inputTokens ?? 0) > 0 ||
    (usage.outputTokens ?? 0) > 0 ||
    (usage.totalTokens ?? 0) > 0 ||
    (usage.cachedReadTokens ?? 0) > 0 ||
    (usage.cachedWriteTokens ?? 0) > 0 ||
    (usage.cacheCreationTokens ?? 0) > 0 ||
    (usage.cacheHitTokens ?? 0) > 0 ||
    (usage.cacheMissTokens ?? 0) > 0 ||
    (usage.thoughtTokens ?? 0) > 0 ||
    (usage.credit ?? 0) > 0 ||
    (usage.cost ?? 0) > 0
  )
}

/** Currency symbols for the codes agents actually report. */
export const CURRENCY_SYMBOLS: Record<string, string> = {
  USD: '$',
  CNY: '¥',
  EUR: '€',
  GBP: '£',
  JPY: '¥',
  HKD: 'HK$',
  KRW: '₩',
  AUD: 'A$',
  CAD: 'C$',
}

/**
 * Format a reported cost for display.
 *
 * - Always two decimals: sub-cent amounts (pi/opencode report ~$0.000036 per
 *   turn) round to 0.00 instead of switching to a higher-precision format.
 * - An unknown currency code falls back to the bare code ("12.34 XYZ").
 * - An EMPTY currency shows the bare number. CodeBuddy reports cost.amount ==
 *   credit with an empty currency; inventing "$" or a placeholder would
 *   fabricate a unit the agent never claimed.
 * - 0 or negative returns '' (the row is hidden).
 */
export function formatUsageCost(amount: number, currency?: string): string {
  if (!amount || amount <= 0) return ''
  const formatted = amount.toFixed(2)
  if (!currency) return formatted
  const sym = CURRENCY_SYMBOLS[currency] ?? ''
  return sym ? `${sym}${formatted}` : `${formatted} ${currency}`
}

/** i18n keys for the known `usageByCategory` components. */
export const USAGE_CATEGORY_LABEL_KEYS: Record<string, string> = {
  conversation: 'chat.sessionInfo.catConversation',
  tools: 'chat.sessionInfo.catTools',
  systemPrompt: 'chat.sessionInfo.catSystemPrompt',
  skills: 'chat.sessionInfo.catSkills',
  mcp: 'chat.sessionInfo.catMCP',
}

export interface UsageCategoryRow {
  key: string
  value: number
  label: string
}

/**
 * Context breakdown rows from an agent's `usageByCategory` (currently only
 * CodeBuddy reports it), ordered as the agent sent them. Zero-valued entries
 * are dropped (agents emit explicit zeros on the first notification of a turn,
 * which would render a list of "0" rows). An unknown component key keeps its raw
 * key as the label rather than disappearing.
 */
export function usageCategoryRows(
  usage: Pick<UsageSnapshot, 'usageByCategory'> | null | undefined,
  t: (key: string) => string,
): UsageCategoryRow[] {
  const cat = usage?.usageByCategory
  if (!cat) return []
  return Object.entries(cat)
    .filter(([, v]) => (v ?? 0) > 0)
    .map(([key, value]) => ({
      key,
      value: value ?? 0,
      label: USAGE_CATEGORY_LABEL_KEYS[key] ? t(USAGE_CATEGORY_LABEL_KEYS[key]) : key,
    }))
}
