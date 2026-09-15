import { getServerURL } from './server'

const E2E_PASSWORD = process.env.E2E_PASSWORD || 'e2e-test-password'

/**
 * Node-context API helpers.
 *
 * Requests issued from the Playwright *Node* process carry no browser cookies,
 * so they cannot rely on the page's session. They must log in explicitly and
 * replay the session cookie themselves.
 *
 * This used to be unnecessary: the server bypassed auth for any request from
 * loopback. That bypass is gone (it also exposed the API through the FRP
 * tunnel, which dials in from 127.0.0.1), so every Node-side call now needs a
 * real session.
 */

let cachedCookie: string | null = null

/**
 * Log in with the E2E password and return a `Cookie:` header value.
 *
 * The result is memoized for the process lifetime: the server persists its
 * cookie token to disk, so one login covers the whole run (including across the
 * server restarts some tests trigger).
 */
export async function getSessionCookie(): Promise<string> {
  if (cachedCookie) return cachedCookie

  const resp = await fetch(`${getServerURL()}/login`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ password: E2E_PASSWORD }),
  })
  if (!resp.ok) {
    throw new Error(`E2E login failed: ${resp.status} ${resp.statusText}`)
  }

  // On a non-default port the cookie is port-scoped (cb20100_clawbench_session),
  // so read whatever Set-Cookie names were actually returned.
  const setCookies = resp.headers.getSetCookie?.() ?? []
  const cookie = setCookies
    .map((c) => c.split(';')[0].trim())
    .filter((c) => c.length > 0)
    .join('; ')

  if (!cookie) {
    throw new Error('E2E login succeeded but returned no session cookie')
  }

  cachedCookie = cookie
  return cookie
}

/**
 * fetch() against the E2E server with the session cookie attached.
 *
 * Use this for every API call made from the Node context (test bodies,
 * seeding helpers, fixtures).
 */
export async function apiFetch(path: string, init: RequestInit = {}): Promise<Response> {
  const cookie = await getSessionCookie()
  const headers = new Headers(init.headers)
  headers.set('Cookie', cookie)
  return fetch(`${getServerURL()}${path}`, { ...init, headers })
}

/** Reset the memoized cookie. Call from globalSetup/teardown between runs. */
export function resetSessionCookie(): void {
  cachedCookie = null
}
