/**
 * The third tool verdict.
 *
 * A tool call normally ends in `success` or `error` (the agent reported a
 * result). When the turn ends while the call is still running — user cancel,
 * agent crash, or a reply split at a mid-turn injection — no result will ever
 * arrive: nothing failed, the turn simply stopped waiting. That is
 * `interrupted`, and it must not be drawn as either a success (nobody saw one)
 * or an error (nothing reported one).
 *
 * Mirrors the backend string `ai.ToolStatusInterrupted` — keep them in step.
 */
export const TOOL_STATUS_INTERRUPTED = 'interrupted'

/** True for a settled tool whose result never arrived. */
export function isToolInterrupted(block: { status?: string } | null | undefined): boolean {
  return block?.status === TOOL_STATUS_INTERRUPTED
}
