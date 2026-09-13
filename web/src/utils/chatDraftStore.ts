// ── Per-session chat input text draft ──
//
// Module-level (NOT component-local) on purpose: an SPA project switch changes
// the root `:key="projectKey"` binding, which makes Vue destroy and rebuild the
// whole `.app-container` subtree — including ChatInputBar. A component-local
// Map would be garbage-collected with it, silently dropping the user's unsent
// text. Hoisting the store to module scope lets the draft survive that remount
// so switching project A → B → A restores the text typed in A.
//
// Mirrors useChatContext's `attachmentDrafts` (also module-level). Keyed by
// session id only: session ids are UUIDs, so they are globally unique and need
// no project scoping. In-memory only — a page reload starts with no drafts,
// matching the existing session-switch draft behavior.

const textDrafts = new Map<string, string>()

/** Store the draft for a session. An empty string is treated as "no draft". */
export function setChatDraft(sessionId: string, text: string): void {
  if (!sessionId) return
  if (text) textDrafts.set(sessionId, text)
  else textDrafts.delete(sessionId)
}

/** Read a session's draft; returns '' when there is none. */
export function getChatDraft(sessionId: string): string {
  if (!sessionId) return ''
  return textDrafts.get(sessionId) ?? ''
}

/** Whether a non-empty draft exists for the session. */
export function hasChatDraft(sessionId: string): boolean {
  return !!sessionId && textDrafts.has(sessionId)
}

/** Drop a session's draft (e.g. the session was archived/destroyed). */
export function deleteChatDraft(sessionId: string): void {
  if (sessionId) textDrafts.delete(sessionId)
}

/** @internal Reset all drafts — for tests only. */
export function _resetChatDraftsForTesting(): void {
  textDrafts.clear()
}
