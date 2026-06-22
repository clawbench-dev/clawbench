/**
 * Pure functions extracted from ChatMessageList.vue for testability.
 */

/**
 * Compute how many older messages are not yet loaded.
 */
export function computeRemainingCount(hasMore: boolean, totalMessages: number, loadedMessages: number): number {
  if (!hasMore) return 0
  return Math.max(0, totalMessages - loadedMessages)
}
