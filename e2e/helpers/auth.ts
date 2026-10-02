import { getServerURL } from './server'

const E2E_PASSWORD = process.env.E2E_PASSWORD || 'e2e-test-password'
/** The password the password-change spec rotates to; see getSessionCookie. */
const NEW_PASSWORD = 'new-e2e-password-123456'

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
 *
 * NOTE: a password change rotates the cookie token server-side and invalidates
 * every existing session — call `resetSessionCookie()` after such a test so the
 * next call logs in again.
 */
export async function getSessionCookie(): Promise<string> {
  if (cachedCookie) return cachedCookie

  // The password-change spec rotates the server password and can leave it as
  // NEW_PASSWORD if it crashes mid-test, so try both known values.
  const candidates = [E2E_PASSWORD, NEW_PASSWORD]
  let lastStatus = 0
  for (const password of candidates) {
    const resp = await fetch(`${getServerURL()}/login`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ password }),
    })
    if (!resp.ok) {
      lastStatus = resp.status
      continue
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
  throw new Error(`E2E login failed: ${lastStatus}`)
}

/**
 * fetch() against the E2E server with the session cookie attached.
 *
 * Use this for every API call made from the Node context (test bodies,
 * seeding helpers, fixtures).
 *
 * NOTE: this carries ONLY the session cookie. Project-scoped endpoints
 * (`/api/ai/sessions`, `/api/ai/session/update`, …) additionally require the
 * project cookie — use `projectApiFetch` for those.
 *
 * A 401 triggers one re-login and retry: a password change rotates the server's
 * cookie token, which invalidates the memoized cookie mid-run.
 */
export async function apiFetch(path: string, init: RequestInit = {}): Promise<Response> {
  const send = async (cookie: string) => {
    const headers = new Headers(init.headers)
    headers.set('Cookie', cookie)
    return fetch(`${getServerURL()}${path}`, { ...init, headers })
  }

  let resp = await send(await getSessionCookie())
  if (resp.status === 401) {
    resetSessionCookie()
    resp = await send(await getSessionCookie())
  }
  return resp
}

let cachedProjectCookie: string | null = null

/**
 * Log in (if needed) and resolve the project cookie by calling GET /api/project,
 * returning a full `Cookie:` header value with BOTH cookies.
 *
 * The project cookie is port-scoped (`cb<port>_clawbench_project`) and is set
 * by the server on GET /api/project — the same call the frontend makes on load.
 * Memoized for the process lifetime.
 */
export async function getProjectCookieHeader(): Promise<string> {
  if (cachedProjectCookie) return cachedProjectCookie
  const session = await getSessionCookie()
  const resp = await fetch(`${getServerURL()}/api/project`, {
    headers: { Cookie: session },
  })
  const setCookies = resp.headers.getSetCookie?.() ?? []
  const projectCookie = setCookies
    .map((c) => c.split(';')[0].trim())
    .filter((c) => c.includes('clawbench_project'))
    .join('; ')
  cachedProjectCookie = projectCookie ? `${session}; ${projectCookie}` : session
  return cachedProjectCookie
}

/**
 * fetch() with BOTH the session and project cookies — required by
 * project-scoped endpoints, which 403 without the project cookie.
 *
 * A 401 triggers one full re-login (session + project cookies) and retry: a
 * password change rotates the server's cookie token and invalidates the
 * memoized cookies mid-run.
 */
export async function projectApiFetch(path: string, init: RequestInit = {}): Promise<Response> {
  const send = async (cookie: string) => {
    const headers = new Headers(init.headers)
    headers.set('Cookie', cookie)
    return fetch(`${getServerURL()}${path}`, { ...init, headers })
  }

  let resp = await send(await getProjectCookieHeader())
  if (resp.status === 401) {
    resetSessionCookie()
    resp = await send(await getProjectCookieHeader())
  }
  return resp
}

/** Reset the memoized cookie. Call from globalSetup/teardown between runs. */
export function resetSessionCookie(): void {
  cachedCookie = null
  cachedProjectCookie = null
}
