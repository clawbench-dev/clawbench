/**
 * Billing-multiplier label for a model (issue #657).
 *
 * The agent reports the multiplier as free text — CodeBuddy sends
 * "x0.06 credits" (and an empty string for its auto router). The CLI's own
 * model picker renders the compact form "x0.06" whenever the column is narrow,
 * so the picker here does the same: the two numbers are meant to be compared
 * side by side, and "(x0.06 credits)" would not fit next to a model name.
 *
 * Anything that does not match the multiplier shape is passed through
 * unchanged — inventing a number from an unrecognized string is worse than
 * showing the agent's own words.
 */
export function formatCreditsLabel(raw?: string | null): string {
  const value = (raw ?? '').trim()
  if (!value) return ''
  // "x0.06 credits" / "×1.5 credits" → "x0.06" / "x1.5"
  const match = /^[x×]\s*(\d+(?:\.\d+)?)\s+credits$/i.exec(value)
  if (match) return `x${match[1]}`
  return value
}
