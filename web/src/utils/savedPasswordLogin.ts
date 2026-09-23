/**
 * Saved-password auto-login, used when `/api/me` reports 401/403 in APP mode.
 *
 * Extracted from App.vue's mount handler so the outcome can be unit-tested:
 * every failure path used to fall straight to the login page with NO message,
 * which made a stale/rotated password look like the app silently bouncing the
 * user out. The caller maps each outcome to user-visible feedback.
 */

export type SavedPasswordOutcome =
  /** Re-authenticated successfully. */
  | 'authenticated'
  /** No stored password — the login page is the expected destination. */
  | 'no-password'
  /** A password was stored but the server rejected it. */
  | 'auth-failed'
  /** The login request itself failed (server unreachable). */
  | 'network-error'

export interface SavedPasswordLoginDeps {
  /** Read the host's stored password for the active server. */
  getSavedPassword: () => Promise<string | undefined> | string | undefined
  /** POST /login. Throws (or rejects) on a transport failure. */
  login: (password: string) => Promise<{ ok: boolean }>
  /**
   * Mirror the password back into the native host after a successful login.
   * Failures here must not undo a successful authentication.
   */
  persistPassword?: (password: string) => Promise<void> | void
}

export async function attemptSavedPasswordLogin(
  deps: SavedPasswordLoginDeps,
): Promise<SavedPasswordOutcome> {
  let saved: string | undefined
  try {
    saved = await deps.getSavedPassword()
  } catch {
    // A failing native bridge is a transport problem, not a bad password —
    // reporting it as auth-failed would wrongly prompt for a new password.
    return 'network-error'
  }
  if (!saved) return 'no-password'

  let res: { ok: boolean }
  try {
    res = await deps.login(saved)
  } catch {
    return 'network-error'
  }
  if (!res.ok) return 'auth-failed'

  // Best-effort: the user is authenticated either way, so a failed mirror must
  // not be reported as an auth failure.
  try {
    await deps.persistPassword?.(saved)
  } catch {
    /* ignore */
  }
  return 'authenticated'
}
