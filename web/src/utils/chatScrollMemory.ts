/**
 * Per-session chat scroll position memory.
 *
 * Switching sessions rebuilds the message list DOM (listKey embeds the session
 * id), so the browser's scrollTop is lost. This module remembers where the user
 * was reading in every session they left, so returning can put them back.
 *
 * Contract (what the user expects):
 * - A session left while AT the bottom is NOT remembered — returning shows the
 *   newest content, which is exactly what "I was watching the live end" means.
 * - A session left while scrolled away from the bottom IS remembered, and
 *   returning restores that position (pixels + message anchor + ratio, so a
 *   partially loaded history can still land in the right place).
 * - Only a STILL session is restored. A running one still lands at the bottom:
 *   its output is the point, and restoring mid-list would hide it.
 * - Memory is in-memory only (page lifetime, not across reloads), matching the
 *   file-preview scroll cache (`fileScrollCache.ts`).
 */
export interface ChatScrollEntry {
  /** Raw scroll offset when the user left. */
  scrollTop: number
  /** `data-msg-key` of the top-most visible message; '' for transient bubbles
   *  (pending / local / drain rows carry no key), in which case only pixels and
   *  the ratio are usable. */
  anchorKey: string
  /** The anchor message's top relative to the scroll container's top. */
  anchorOffset: number
  /** scrollTop / maxScroll, for when the restored content is a different
   *  height than what the user left (lazy-loaded history). 0 when maxScroll<=0. */
  ratio: number
}

const memory = new Map<string, ChatScrollEntry>()

/** Maximum entries to keep (oldest evicted first) — prevents unbounded growth. */
const MAX_ENTRIES = 100

export function saveChatScrollPosition(sessionId: string, entry: ChatScrollEntry): void {
  if (!sessionId) return
  memory.set(sessionId, entry)
  if (memory.size > MAX_ENTRIES) {
    const oldest = memory.keys().next().value
    if (oldest !== undefined) memory.delete(oldest)
  }
}

export function clearChatScrollPosition(sessionId: string): void {
  memory.delete(sessionId)
}

export function getChatScrollPosition(sessionId: string): ChatScrollEntry | undefined {
  return memory.get(sessionId)
}

export function hasChatScrollPosition(sessionId: string): boolean {
  return !!sessionId && memory.has(sessionId)
}

/**
 * Whether a session switch should restore the remembered position instead of
 * pinning to the bottom.
 *
 * Pure so the rule has exactly one definition: it is consumed by
 * `useChatSession.switchSession` (to decide `forceScrollBottom`) and pinned by
 * unit tests without any DOM.
 */
export function shouldRestoreChatScroll(hasMemory: boolean, isRunning: boolean): boolean {
  return hasMemory && !isRunning
}

/** @internal Reset all state — for tests only. */
export function _resetChatScrollMemory(): void {
  memory.clear()
}
