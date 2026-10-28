// Mark the session the app just LANDED on as read — the project switch and the
// cold start both end with a conversation on screen that nobody "clicked".
//
// Why this is not covered by switchSession: `360241f44` decoupled "load history"
// from "mark read" so that automatic reloads (WS reconnect, completion refresh,
// pagination) stop clearing badges the user never saw. `switchSession` was given
// an explicit read to compensate, and every later path (completion, cancel,
// foreground resume) got its own. The landing paths did not: a project switch
// resolves identity in `initSessionFromAPI` and the remounted panel's first
// `loadHistory` fetches the conversation directly, so neither `switchSession`
// nor any completion event runs. Result: the user lands on a session, its
// messages are on screen, and its unread dot stays lit until they switch away
// and back. Cold start has the same shape (it used to be marked read as a side
// effect of the old GET handler).
//
// Two guards, and both are load-bearing:
//
//   - `pendingSessionId`: a cross-project jump is a landing too, but Phase 7
//     marks it read itself (adopt path or `switchSession`), WITH `project_path`
//     so the backend can prove ownership. Marking here as well would fire the
//     same POST without it — the cookie project is not the owner yet in spirit
//     and the call would just 403 and log noise.
//   - panel visibility: a badge exists to say "there is a reply you have not
//     looked at". Landing is only evidence of looking when the chat panel is
//     actually on screen — on a narrow layout the restored panel may be the
//     file manager, and on a wide one the chat column may be collapsed. Same
//     predicate the completion notification uses, for the same reason.
import { isChatPanelVisible } from '@/composables/useWideScreenLayout'
import { markSessionRead } from '@/utils/sessionRead'

export interface LandedSessionReadOptions {
  /** Session the landing resolved to (`currentSessionId` after the switch). */
  sessionId: string
  /** The cross-project target, when this landing IS that jump. */
  pendingSessionId?: string | null
  isWideScreen: boolean
  chatCollapsed: boolean
  activeTab: string
  /**
   * Called once the read has SETTLED, so the list re-reads the cleared badge.
   *
   * Ordering matters: `POST /api/ai/chat/read` is what moves `last_read_at`, and
   * the list response carries the per-session unread count. Refreshing first
   * makes the badge's disappearance a race the landing can lose.
   */
  onMarked: () => void
}

/**
 * Mark the landed-on session read when the chat panel shows it. Never throws —
 * a failed read must not affect the switch that just completed.
 *
 * No `projectPath` is forwarded: a landing happens AFTER `store.setProject()`
 * flipped the project cookie, so the cookie project already is the session's
 * owner and the backend's fallback verifies it correctly.
 */
export async function markLandedSessionRead(opts: LandedSessionReadOptions): Promise<void> {
  const { sessionId, pendingSessionId, onMarked } = opts
  if (!sessionId) return
  // A pending jump owns its own read (with project_path) — see the header note.
  if (pendingSessionId) return
  if (!isChatPanelVisible({
    isWideScreen: opts.isWideScreen,
    chatCollapsed: opts.chatCollapsed,
    activeTab: opts.activeTab,
  })) return
  await markSessionRead(sessionId)
  onMarked()
}
