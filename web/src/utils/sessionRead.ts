// Mark a chat session as read (`POST /api/ai/chat/read`).
//
// Leaf module (no composable state) because two very different callers need the
// exact same request shape:
//   - `useChatSession.switchSession` — the normal "user opened this session" path;
//   - `App.vue`'s cross-project jump — which, after seeding the target session
//     into identity, no longer goes through `switchSession` at all (see
//     hotSwitchProject Phase 7). Without an explicit read there, the
//     cross-project row's unread badge would never clear — the whole point of
//     opening it from that tab.
//
// `projectPath` must be forwarded for cross-project opens: the backend verifies
// session ownership against it, and omitting it falls back to the cookie
// project, which rejects (403) a session belonging to another project. Omit it
// for same-project switches, where the cookie project IS the owner.
import { appLog } from '@/utils/appLog'

const TAG = 'SessionRead'

export async function markSessionRead(sessionId: string, projectPath?: string): Promise<void> {
  if (!sessionId) return
  const params = new URLSearchParams({ session_id: sessionId })
  if (projectPath) params.set('project_path', projectPath)
  try {
    const resp = await fetch(`/api/ai/chat/read?${params.toString()}`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
    })
    if (!resp.ok) {
      appLog.w(TAG, `markSessionRead failed for ${sessionId.slice(0, 12)}: ${resp.status}`)
    }
    // DRAIN THE BODY, even though nothing reads the payload.
    //
    // A `fetch` whose body is never consumed keeps the response stream open, so
    // the browser still counts the request as in flight: `requestfinished` never
    // fires and `waitForLoadState('networkidle')` never settles. That is not
    // theoretical — the cold-start / resume read added with sessionReadGate made
    // every page load hold one open request, which timed out five Playwright
    // specs (acp-mode-switching, acp-session-persistence, acp-thinking-model,
    // chat quick-send, group-member-identity) at `networkidle` after a reload.
    // Cancelling the stream also releases the connection instead of leaking it.
    await resp.body?.cancel().catch(() => {})
  } catch (err) {
    // Read state is not a precondition for anything the caller does next; a
    // dropped connection must not turn opening a session into an error.
    appLog.w(TAG, `markSessionRead failed for ${sessionId.slice(0, 12)}`, err)
  }
}
