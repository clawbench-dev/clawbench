import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest'

// ────────────────────────────────────────────────────────────
// authExpiry.ts wraps window.fetch and redirects to /login when
// an authenticated API call returns the middleware.Auth 401 body
// { error: "unauthorized" } — the expired-session-cookie signal.
// Business 401s (wrong password, etc.) use other bodies and must
// NOT trigger a redirect.
// ────────────────────────────────────────────────────────────

vi.mock('@/utils/appLog', () => ({
  appLog: { d: vi.fn(), i: vi.fn(), w: vi.fn(), e: vi.fn() },
}))

// Mock location so handleUnauthorized can assert the redirect target.
const originalLocation = window.location
let hrefSetter: string | null = null

function mockLocation() {
  const location = { ...originalLocation, origin: 'http://test.local' }
  Object.defineProperty(location, 'href', {
    get: () => hrefSetter,
    set: (v: string) => { hrefSetter = v },
    configurable: true,
  })
  Object.defineProperty(window, 'location', { value: location, configurable: true })
}

function jsonResponse(status: number, body: unknown): Response {
  return new Response(JSON.stringify(body), {
    status,
    statusText: status === 401 ? 'Unauthorized' : 'Internal Server Error',
    headers: { 'Content-Type': 'application/json' },
  })
}

beforeEach(() => {
  hrefSetter = null
  mockLocation()
  vi.resetModules()
})

afterEach(() => {
  Object.defineProperty(window, 'location', { value: originalLocation, configurable: true })
  delete (window as unknown as { __cbAuthExpiryInstalled?: boolean }).__cbAuthExpiryInstalled
  vi.unstubAllGlobals()
})

// Fresh module import per test so module-level flags reset.
async function freshAuthExpiry() {
  return await import('@/utils/authExpiry')
}

describe('handleUnauthorized (body-gated)', () => {
  it('does not redirect when disabled (default)', async () => {
    const { handleUnauthorized, isAuthRedirectEnabled } = await freshAuthExpiry()
    expect(isAuthRedirectEnabled()).toBe(false)

    const fired = handleUnauthorized('/api/chat/sessions', { error: 'unauthorized' })
    expect(fired).toBe(false)
    expect(hrefSetter).toBeNull()
  })

  it('redirects to /login on the middleware.Auth 401 body once enabled', async () => {
    const { handleUnauthorized, setAuthRedirectEnabled } = await freshAuthExpiry()
    setAuthRedirectEnabled(true)

    const fired = handleUnauthorized('/api/chat/sessions', { error: 'unauthorized' })
    expect(fired).toBe(true)
    expect(hrefSetter).toBe('/login')
  })

  it('does NOT redirect for the login POST wrong-password body', async () => {
    const { handleUnauthorized, setAuthRedirectEnabled } = await freshAuthExpiry()
    setAuthRedirectEnabled(true)

    // handler/auth.go:236 — POST /login returns { ok: false } on bad password
    const fired = handleUnauthorized('/login', { ok: false })
    expect(fired).toBe(false)
    expect(hrefSetter).toBeNull()
  })

  it('does NOT redirect for the config-password wrong-current-password body', async () => {
    const { handleUnauthorized, setAuthRedirectEnabled } = await freshAuthExpiry()
    setAuthRedirectEnabled(true)

    // handler/settings.go:1582 — wrong current password, own error UI
    const fired = handleUnauthorized('/api/config/password', {
      error: 'wrong_password',
      message: 'current password is incorrect',
    })
    expect(fired).toBe(false)
    expect(hrefSetter).toBeNull()
  })

  it('does NOT redirect on non-401-status or non-auth error bodies', async () => {
    const { handleUnauthorized, setAuthRedirectEnabled } = await freshAuthExpiry()
    setAuthRedirectEnabled(true)

    expect(handleUnauthorized('/api/chat/sessions', null)).toBe(false)
    expect(handleUnauthorized('/api/chat/sessions', 'unauthorized')).toBe(false)
    expect(handleUnauthorized('/api/chat/sessions', { error: 'Session not found' })).toBe(false)
    expect(handleUnauthorized('/api/chat/sessions', undefined)).toBe(false)
    expect(hrefSetter).toBeNull()
  })

  it('redirects even for trailing-slash / encoded-path variants (no URL whitelist to bypass)', async () => {
    const { handleUnauthorized, setAuthRedirectEnabled } = await freshAuthExpiry()
    setAuthRedirectEnabled(true)

    // The middleware.Auth body is the sole signal — path shape is irrelevant.
    const fired = handleUnauthorized('http://test.local/api//config/password', { error: 'unauthorized' })
    expect(fired).toBe(true)
    expect(hrefSetter).toBe('/login')
  })

  it('does not latch — a blocked navigation retries on the next 401', async () => {
    const { handleUnauthorized, setAuthRedirectEnabled } = await freshAuthExpiry()
    setAuthRedirectEnabled(true)

    expect(handleUnauthorized('/api/chat/sessions', { error: 'unauthorized' })).toBe(true)
    expect(hrefSetter).toBe('/login')
    // No sticky latch: a second 401 (e.g. navigation was interrupted) fires again.
    expect(handleUnauthorized('/api/tasks', { error: 'unauthorized' })).toBe(true)
    expect(hrefSetter).toBe('/login')
  })

  it('re-arms after being disabled (can redirect again on next session)', async () => {
    const { handleUnauthorized, setAuthRedirectEnabled } = await freshAuthExpiry()
    setAuthRedirectEnabled(true)
    expect(handleUnauthorized('/api/chat/sessions', { error: 'unauthorized' })).toBe(true)
    expect(hrefSetter).toBe('/login')

    // Simulate logout → login again
    hrefSetter = null
    setAuthRedirectEnabled(false)
    expect(handleUnauthorized('/api/tasks', { error: 'unauthorized' })).toBe(false)

    setAuthRedirectEnabled(true)
    expect(handleUnauthorized('/api/tasks', { error: 'unauthorized' })).toBe(true)
    expect(hrefSetter).toBe('/login')
  })
})

describe('installAuthRedirectInterceptor', () => {
  it('redirects on the auth-expiry body and preserves the body for the caller', async () => {
    const { installAuthRedirectInterceptor, setAuthRedirectEnabled } = await freshAuthExpiry()
    setAuthRedirectEnabled(true)
    vi.stubGlobal('fetch', () => Promise.resolve(jsonResponse(401, { error: 'unauthorized' })))

    installAuthRedirectInterceptor()

    const resp = await window.fetch('http://test.local/api/chat/sessions')
    expect(resp.status).toBe(401)
    expect(await resp.json()).toEqual({ error: 'unauthorized' }) // caller still reads the body
    expect(hrefSetter).toBe('/login') // redirect side effect fired
  })

  it('does not redirect on business-401 bodies (wrong_password) and passes through untouched', async () => {
    const { installAuthRedirectInterceptor, setAuthRedirectEnabled } = await freshAuthExpiry()
    setAuthRedirectEnabled(true)
    vi.stubGlobal('fetch', () =>
      Promise.resolve(jsonResponse(401, { error: 'wrong_password', message: 'current password is incorrect' })))

    installAuthRedirectInterceptor()

    const resp = await window.fetch('http://test.local/api/config/password')
    expect(resp.status).toBe(401)
    expect(await resp.json()).toEqual({ error: 'wrong_password', message: 'current password is incorrect' })
    expect(hrefSetter).toBeNull()
  })

  it('does not redirect on non-401 responses', async () => {
    const { installAuthRedirectInterceptor, setAuthRedirectEnabled } = await freshAuthExpiry()
    setAuthRedirectEnabled(true)
    vi.stubGlobal('fetch', () => Promise.resolve(jsonResponse(500, { error: 'boom' })))

    installAuthRedirectInterceptor()

    const resp = await window.fetch('http://test.local/api/config')
    expect(resp.status).toBe(500)
    expect(hrefSetter).toBeNull()
  })

  it('does not redirect when disabled (e.g. mount-time /api/me check)', async () => {
    const { installAuthRedirectInterceptor } = await freshAuthExpiry()
    vi.stubGlobal('fetch', () => Promise.resolve(jsonResponse(401, { error: 'unauthorized' })))

    installAuthRedirectInterceptor()

    const resp = await window.fetch('http://test.local/api/me')
    expect(resp.status).toBe(401)
    expect(await resp.json()).toEqual({ error: 'unauthorized' })
    expect(hrefSetter).toBeNull()
  })

  it('does not redirect on a cross-origin 401 even with the auth body', async () => {
    const { installAuthRedirectInterceptor, setAuthRedirectEnabled } = await freshAuthExpiry()
    setAuthRedirectEnabled(true)
    // resp.url points at a foreign host → must be ignored
    vi.stubGlobal('fetch', () =>
      Promise.resolve(new Response(JSON.stringify({ error: 'unauthorized' }), {
        status: 401,
        headers: { 'Content-Type': 'application/json' },
        // jsdom lets us set an absolute response URL
        ...({ url: 'http://foreign.example/api/thing' } as Record<string, unknown>),
      })))

    installAuthRedirectInterceptor()

    const resp = await window.fetch('http://foreign.example/api/thing')
    expect(resp.status).toBe(401)
    expect(hrefSetter).toBeNull()
  })

  it('passes through an abort/network rejection untouched (no redirect)', async () => {
    const { installAuthRedirectInterceptor, setAuthRedirectEnabled } = await freshAuthExpiry()
    setAuthRedirectEnabled(true)

    const origFetch = vi.fn().mockRejectedValue(new DOMException('The operation was aborted', 'AbortError'))
    vi.stubGlobal('fetch', origFetch)

    installAuthRedirectInterceptor()

    await expect(window.fetch('http://test.local/api/chat/history')).rejects.toMatchObject({ name: 'AbortError' })
    expect(hrefSetter).toBeNull()
  })

  it('is idempotent — installing twice wraps fetch only once', async () => {
    const { installAuthRedirectInterceptor, setAuthRedirectEnabled } = await freshAuthExpiry()
    setAuthRedirectEnabled(true)

    const calls = vi.fn()
    // Real Response-based fetch so a 401 body passes through the json() probe.
    vi.stubGlobal('fetch', (url: string) => {
      calls(url)
      return Promise.resolve(jsonResponse(401, { error: 'unauthorized' }))
    })

    installAuthRedirectInterceptor()
    installAuthRedirectInterceptor()

    await window.fetch('/api/me')
    expect(calls).toHaveBeenCalledTimes(1)
  })
})
