/**
 * Card summary for a tool call: the borrowed background-task label when there
 * is one, otherwise whatever the generic summarizer produced.
 *
 * Issue #682: a TaskOutput / TaskStop card knows only a random background-task
 * id, so its summary read "oAVlC6" while the Bash card that started the same
 * task right above it read "跑全量测试…". `labels` (see
 * utils/backgroundTaskLabels.ts) carries that borrowed label, keyed by the
 * lookup block's tool-call id, and wins over the id-only summary.
 *
 * Kept in a leaf module because `toolCallSummary` lives in chatBlocks.ts, which
 * many specs mock wholesale — a new export there would be `undefined` in every
 * such mock.
 */
export function toolCardSummary(
  block: { id?: string; input?: Record<string, unknown>; name?: string; summary?: string },
  summarize: (b: { id?: string; input?: Record<string, unknown>; name?: string; summary?: string }) => string,
  labels?: Map<string, string>,
): string {
  const label = block?.id ? labels?.get(block.id) : undefined
  return label || summarize(block)
}
