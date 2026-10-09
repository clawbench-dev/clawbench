import { apiFetch } from './auth'

/**
 * The member-speech cap (`chat.group_max_speeches`) is a GLOBAL server setting,
 * not a per-group one. Group e2e specs use it to bound a turn deterministically
 * (the acp-mock host routes to the same member forever, so without a low cap the
 * turn never ends).
 *
 * Because it is global and the E2E server is shared across specs (1 worker,
 * files run one at a time), any spec that lowers it MUST restore it afterwards
 * — otherwise a later spec inherits the low cap and terminates too early.
 *
 * Uses `apiFetch` (session cookie only): `/api/config` is NOT project-scoped,
 * so it works from the Node context in `afterAll` without a page.
 */
export const DEFAULT_GROUP_MAX_SPEECHES = 100

/** setGroupMaxSpeeches PATCHes the global member-speech cap via /api/config. */
export async function setGroupMaxSpeeches(n: number): Promise<void> {
  const resp = await apiFetch('/api/config', {
    method: 'PATCH',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ chat: { group_max_speeches: n } }),
  })
  if (!resp.ok) {
    throw new Error(`set group_max_speeches failed: ${resp.status}`)
  }
}

/** restoreGroupMaxSpeeches resets the cap to the default so it cannot leak. */
export async function restoreGroupMaxSpeeches(): Promise<void> {
  await setGroupMaxSpeeches(DEFAULT_GROUP_MAX_SPEECHES)
}
