import { app } from 'electron'
import { getStore } from './store'
import { record } from './clientLog'

/**
 * Secure-context promotion for the server the shell loads.
 *
 * The desktop shell loads the server over plain HTTP on a LAN address, which
 * Chromium does NOT treat as a secure context. Without one `navigator.mediaDevices`
 * is undefined, so microphone capture, screen capture and WebCodecs are all
 * unreachable — voice input and meetings are dead, not merely degraded.
 *
 * `unsafely-treat-insecure-origin-as-secure` promotes origins to secure-context
 * treatment. Two properties shape everything here:
 *
 * - It takes a comma-separated LIST, so every saved server can be covered at
 *   once. Promoting only the active one meant that switching servers left
 *   capture broken until the next cold start, which is invisible until somebody
 *   opens a meeting.
 * - Chromium reads the switch ONCE, at startup. An origin added later cannot be
 *   promoted in this process, so connecting to a brand-new server has to
 *   relaunch the app (see `needsSecureContextRelaunch`).
 *
 * Only `http://` origins are promoted: `https://` and `file://` are already
 * secure, and a wildcard is not supported by the switch (verified: `http://*`
 * leaves the page insecure).
 */

/** Origins this process started with. Never grows — see the module comment. */
let promoted: string[] = []

/** `http://` origin of `url`, or '' when it is not a promotable one. */
function httpOrigin(url: unknown): string {
  if (typeof url !== 'string' || !url) return ''
  try {
    const origin = new URL(url).origin
    return origin.startsWith('http://') ? origin : ''
  } catch {
    // A malformed stored URL is reported by the connect flow; nothing to do.
    return ''
  }
}

/**
 * Every `http://` origin worth promoting: the active server plus everything in
 * the saved list.
 *
 * The saved list matters because the user can switch to any of them at runtime
 * and a switch cannot add to the switch list. A brand-new server is handled by
 * the relaunch path instead.
 */
export function promotableOrigins(): string[] {
  const urls: unknown[] = [getStore().get('serverUrl')]
  const servers = getStore().get('servers')
  if (Array.isArray(servers)) {
    for (const entry of servers) urls.push((entry as { url?: unknown } | null)?.url)
  }
  const out: string[] = []
  for (const url of urls) {
    const origin = httpOrigin(url)
    if (origin && !out.includes(origin)) out.push(origin)
  }
  return out
}

/**
 * Register the switch. Must run before `whenReady` — Chromium reads command-line
 * switches during startup, and appending one afterwards is a no-op.
 */
export function applySecureContextSwitch(): void {
  promoted = promotableOrigins()
  if (promoted.length === 0) return
  app.commandLine.appendSwitch('unsafely-treat-insecure-origin-as-secure', promoted.join(','))
  record('I', 'SecureContext', `promoted ${promoted.join(',')}`)
}

/**
 * Whether connecting to `url` requires a restart before capture works.
 *
 * True only for an `http://` origin this process did not promote. Callers
 * persist the new server URL first: the next process reads it from the store
 * and promotes it, so the relaunch happens at most once per server.
 */
export function needsSecureContextRelaunch(url: string): boolean {
  const origin = httpOrigin(url)
  return origin !== '' && !promoted.includes(origin)
}

/** Test seam: pretend this process was started with `origins`. */
export function _setPromotedForTests(origins: string[]): void {
  promoted = origins
}
