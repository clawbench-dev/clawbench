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

/**
 * TokenRateStat is the measured output speed of ONE turn.
 *
 * Source: the backend's message metadata (`tokensPerSecond` + `genMs`) or a
 * session's `tokenRate` object (the settings endpoint, used for group members).
 *
 * The denominator is NOT wall time: it is the time the model actually spent
 * emitting text/thinking. A turn that ran a long build therefore still reports
 * the model's own speed instead of the build's.
 */
export interface TokenRateStat {
  tokensPerSecond: number
  genMs?: number
}

/**
 * Coerces a wire value into a usable TokenRateStat, or null.
 *
 * Partial/older payloads are common (a field missing, a zero from `omitempty`,
 * a numeric string from JSON), so every read is validated here rather than in
 * each component. A zero rate means "not measured", never "0 tok/s".
 */
export function normalizeTokenRate(raw: unknown): TokenRateStat | null {
  if (!raw || typeof raw !== 'object') return null
  const rec = raw as Record<string, unknown>
  const rate = Number(rec.tokensPerSecond)
  if (!Number.isFinite(rate) || rate <= 0) return null
  const genMs = Number(rec.genMs)
  return {
    tokensPerSecond: rate,
    genMs: Number.isFinite(genMs) && genMs > 0 ? genMs : undefined,
  }
}

/**
 * The latest COMPLETED assistant turn's measured speed.
 *
 * The newest completed turn decides: if its backend could not measure (a CLI
 * that emits one finished text blob), the row is hidden rather than showing an
 * older turn's number under a "latest reply" label. A turn still streaming is
 * skipped so the previous completed reply stays visible while the next one is
 * being written.
 */
export function latestTokenRate(messages: readonly unknown[] | null | undefined): TokenRateStat | null {
  if (!Array.isArray(messages)) return null
  for (let i = messages.length - 1; i >= 0; i--) {
    const msg = messages[i] as { role?: unknown; metadata?: unknown; streaming?: unknown } | null | undefined
    if (!msg || msg.role !== 'assistant' || msg.streaming) continue
    return normalizeTokenRate(msg.metadata)
  }
  return null
}

/**
 * Display form of a rate: one decimal below 100 (the range where the digit
 * matters), a whole grouped number above it. Empty when unmeasured — callers
 * use that to hide the row instead of rendering "0 tok/s".
 */
export function formatTokenRate(stat: TokenRateStat | null | undefined): string {
  const rate = stat?.tokensPerSecond ?? 0
  if (!Number.isFinite(rate) || rate <= 0) return ''
  const value = rate >= 100 ? Math.round(rate).toLocaleString('en-US') : rate.toFixed(1)
  return `${value} tok/s`
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
 * When an ACP backend reports standard `cachedReadTokens` / `cachedWriteTokens`
 * (e.g. Antigravity `PromptResponse.Usage`) without explicit `cacheHitTokens` /
 * `cacheMissTokens`, derive the hit rate from `cachedReadTokens` and the prompt
 * denominator (detecting uncached-exclusive `inputTokens` via `totalTokens` so
 * we never double-count or exceed 100%).
 *
 * The exclusive/inclusive decision ASSUMES both counters describe the same
 * request. A snapshot mixing a per-call input with turn-level cache counters
 * cannot be detected here; the `cachedRead > denominator` guard only catches
 * the blatant case, so a moderately inflated rate is possible for such a
 * mismatched producer.
 *
 * When `totalTokens` is absent the prompt is treated as INCLUSIVE (denominator
 * = inputTokens). That is the conservative direction: an exclusive prompt then
 * divides by the uncached part only, which can only understate the rate or hit
 * the guard above — never fabricate a higher one.
 *
 * Returns null when nothing is reported or the ratio cannot be safely
 * determined — the row is hidden entirely rather than showing a fabricated 0%.
 */
export function cacheHitRate(
  usage: Pick<
    UsageSnapshot,
    | 'cacheHitTokens'
    | 'cacheMissTokens'
    | 'cachedReadTokens'
    | 'cachedWriteTokens'
    | 'cacheCreationTokens'
    | 'inputTokens'
    | 'outputTokens'
    | 'totalTokens'
    | 'thoughtTokens'
  > | null | undefined,
): number | null {
  const hit = usage?.cacheHitTokens ?? 0
  const miss = usage?.cacheMissTokens ?? 0
  const input = usage?.inputTokens ?? 0
  if (hit > 0 || miss > 0) {
    const split = hit + miss
    const denominator = input > 0 && input >= split ? input : split
    if (denominator <= 0) return 0
    return (hit / denominator) * 100
  }

  const cachedRead = usage?.cachedReadTokens ?? 0
  // The two write spellings describe the same counter, so take the larger
  // rather than summing them. (CodeBuddy reports prompt_cache_write_tokens and
  // cache_creation_input_tokens as separate fields, but it always carries
  // explicit hit/miss counters and therefore never reaches this branch.)
  const cachedWrite = Math.max(usage?.cachedWriteTokens ?? 0, usage?.cacheCreationTokens ?? 0)
  if ((cachedRead <= 0 && cachedWrite <= 0) || input <= 0) return null

  const output = usage?.outputTokens ?? 0
  const total = usage?.totalTokens ?? 0
  const thought = usage?.thoughtTokens ?? 0
  // Some agents fold thinking into output, others report it as a fourth term,
  // and a few add small extra counters to `total`. Accept any of those sums
  // within a 1% band: a near-miss must not silently fall back to the inclusive
  // reading, which would shrink the denominator to the uncached part only.
  // Misclassifying an inclusive prompt is harmless here — its difference is
  // cachedRead + cachedWrite, which is large whenever the rate is non-trivial.
  const exclusiveNoThought = input + output + cachedRead + cachedWrite
  const slack = Math.max(1, Math.round(total * 0.01))
  const near = (a: number, b: number) => Math.abs(a - b) <= slack
  const isExclusivePrompt =
    total > 0 &&
    (near(total, exclusiveNoThought) || (thought > 0 && near(total, exclusiveNoThought + thought)))
  const denominator = isExclusivePrompt ? input + cachedRead + cachedWrite : input
  if (denominator <= 0 || cachedRead > denominator) return null
  return (cachedRead / denominator) * 100
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
