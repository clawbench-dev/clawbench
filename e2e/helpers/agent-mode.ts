import { projectApiFetch } from './auth'
import { getServerURL } from './server'

/**
 * The mode the mock agent must be in for a turn to complete without blocking.
 *
 * In any other mode `cmd/acp-mock` calls `RequestPermission` and waits for an
 * answer that no test provides, so the turn hangs until its timeout.
 */
export const NON_BLOCKING_MODE = 'bypass-permissions'

/**
 * Reset the mock agent's mode so later specs are not affected by this one.
 *
 * Why this is needed: the whole E2E run shares ONE server and ONE project
 * (`workers: 1`). Switching the mode persists it on the agent
 * (`preferred_mode`) and on the session, and both survive into the next spec
 * file. Playwright runs files alphabetically, so the `acp-*` specs (which
 * deliberately switch to Code/Plan) run before `chat`/`slash-commands`, which
 * then inherit a mode where the mock blocks on a permission request.
 *
 * Specs that switch modes must call this from `afterAll`. It is deliberately
 * NOT in the shared auth fixture: the ACP serial specs depend on the mode
 * persisting *within* their own file.
 */
export async function resetMockAgentMode(): Promise<void> {
  await projectApiFetch('/api/agents', {
    method: 'PATCH',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ id: 'acp-mock', preferred_mode: NON_BLOCKING_MODE }),
  })
}

/**
 * Reset every session's mode for the current project back to the non-blocking
 * default. The frontend sends the session's mode on each chat POST, so the
 * agent default alone is not sufficient.
 */
export async function resetSessionModes(): Promise<void> {
  const resp = await projectApiFetch('/api/ai/sessions')
  if (!resp.ok) return
  const data = (await resp.json()) as { sessions?: Array<{ id: string }> }
  for (const s of data.sessions || []) {
    await projectApiFetch('/api/ai/session/update', {
      method: 'PATCH',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ sessionId: s.id, modeId: NON_BLOCKING_MODE }),
    })
  }
}

/** Reset both the agent default and every session's mode. */
export async function resetMockModeEverywhere(): Promise<void> {
  await resetMockAgentMode()
  await resetSessionModes()
}

/**
 * Cancel every in-flight turn.
 *
 * Needed before switching back to the non-blocking mode: a turn that already
 * reached the mock's permission request is parked inside the subprocess, and
 * resetting the mode does not unblock it. Cancelling tears the turn down so the
 * session leaves "running" and the next spec can send normally.
 */
export async function cancelAllTurns(): Promise<void> {
  const resp = await projectApiFetch('/api/ai/sessions')
  if (!resp.ok) return
  const data = (await resp.json()) as { sessions?: Array<{ id: string }> }
  for (const s of data.sessions || []) {
    await projectApiFetch(`/api/ai/chat/cancel?session_id=${encodeURIComponent(s.id)}`, { method: 'POST' })
  }
}

/**
 * Full cleanup for specs that deliberately enter a permission-blocking mode:
 * cancel any parked turn, then restore the non-blocking mode everywhere.
 */
export async function restoreNonBlockingMode(): Promise<void> {
  await cancelAllTurns()
  await resetMockModeEverywhere()
}

/** Exported for tests that need the base URL directly. */
export { getServerURL }
