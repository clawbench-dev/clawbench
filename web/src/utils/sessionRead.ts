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
  } catch (err) {
    // Read state is not a precondition for anything the caller does next; a
    // dropped connection must not turn opening a session into an error.
    appLog.w(TAG, `markSessionRead failed for ${sessionId.slice(0, 12)}`, err)
  }
}
