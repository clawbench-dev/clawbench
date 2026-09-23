import { apiGet } from '@/utils/api'

/** The subset of `/api/terminal/status?session=` this helper reads. */
interface TerminalStatusCwd {
  cwd?: string
  hasSession?: boolean
}

/**
 * Resolve a terminal session's LIVE working directory.
 *
 * The backend reads it from the PTY's foreground process group, so it follows
 * `cd` (including inside nested shells). This is the same source the drag-drop
 * upload path uses.
 *
 * It must be fetched on demand rather than read from the tab record: a tab's
 * `cwd` is written once from the one-shot WS `status` message sent at connect
 * time, which carries the LAUNCH directory — so after a `cd` it is stale (and
 * for a tab created without an explicit cwd it is the project root).
 *
 * Returns `''` when unavailable (no session id, unknown session, request
 * failure). Callers decide the fallback; this never throws, because failing to
 * resolve a directory must not break the caller's own action.
 */
export async function fetchTerminalCwd(sessionId?: string): Promise<string> {
  // Without a session id the endpoint takes its "all sessions" branch and
  // returns no cwd at all — skip the pointless round-trip.
  if (!sessionId) return ''
  try {
    const data = await apiGet<TerminalStatusCwd>(
      `/api/terminal/status?session=${encodeURIComponent(sessionId)}`,
    )
    return data?.cwd || ''
  } catch {
    return ''
  }
}
