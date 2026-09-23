/**
 * Pure helpers for the main window's `did-fail-load` handling.
 *
 * Extracted from `window.ts` so the decision and the injected script can be
 * tested without an Electron runtime — `window.ts` imports `electron` at module
 * scope, which a unit test cannot load.
 */

/** Chromium's ERR_ABORTED: a cancelled navigation, not an unreachable server. */
const ERR_ABORTED = -3

export interface LoadFailure {
  errorCode: number
  failedUrl: string
  /** URL of the built-in login page, so a failure OF the login page is ignored. */
  loginUrl: string
  isMainFrame: boolean
}

/**
 * Whether a failed load means "the configured server is unreachable" and the
 * window should fall back to the login page.
 *
 * Three cases must NOT fall back:
 *  - a subframe failure — the app page itself loaded fine;
 *  - ERR_ABORTED — a cancelled navigation (e.g. a redirect we prevented), which
 *    says nothing about reachability and would otherwise hijack the window;
 *  - a failure of the login page itself — there is nowhere further to go, and
 *    re-loading it would loop.
 */
export function shouldFallBackToLogin(f: LoadFailure): boolean {
  if (!f.isMainFrame) return false
  if (f.errorCode === ERR_ABORTED) return false
  if (!f.failedUrl) return false
  if (f.failedUrl === f.loginUrl) return false
  return true
}

/**
 * Build the script that hands a load failure to the login page's
 * `onConnectError()`.
 *
 * `JSON.stringify` produces a valid, fully-quoted JS string literal, so an
 * error description containing quotes or backslashes cannot break out of the
 * call. Without this the fallback was silent: the user got the login page back
 * with no indication of why the connection failed.
 */
export function buildConnectErrorScript(errorDesc: string): string {
  const arg = JSON.stringify(errorDesc || '')
  return `if (typeof onConnectError === 'function') { onConnectError(${arg}) }`
}
