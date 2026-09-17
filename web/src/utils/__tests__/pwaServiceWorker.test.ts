import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest'

// ────────────────────────────────────────────────────────────
// pwaServiceWorker.ts registers the installability service worker.
//
// The gates matter more than the happy path: this worker must never be
// registered from a context where it could intercept requests it shouldn't
// (an iframe, the native app) or where registration would fail confusingly
// (dev server answering /sw.js with index.html). Each gate is asserted
// individually — a regression that drops one would otherwise show up only as
// a mysterious 403 in production, which is exactly how the previous worker
// broke the app.
// ────────────────────────────────────────────────────────────

const mockIsNativeApp = vi.fn(() => false)
vi.mock('@/utils/clawbenchNative', () => ({
  isNativeApp: () => mockIsNativeApp(),
}))

vi.mock('@/utils/appLog', () => ({
  appLog: { d: vi.fn(), i: vi.fn(), w: vi.fn(), e: vi.fn() },
}))

import { registerPwaServiceWorker } from '@/utils/pwaServiceWorker'

const SW_URL = '/sw.js'

/** Fake ServiceWorkerContainer recording register() calls. */
function installServiceWorkerMock() {
  const register = vi.fn().mockResolvedValue({ scope: 'http://test.local/' })
  const container = { register }
  Object.defineProperty(navigator, 'serviceWorker', { value: container, configurable: true })
  return register
}

/** Make the HEAD /sw.js probe answer with the given status + content type. */
function stubHeadResponse(status: number, contentType: string | null) {
  const headers = new Headers()
  if (contentType) headers.set('content-type', contentType)
  vi.stubGlobal('fetch', vi.fn().mockResolvedValue(new Response(null, { status, headers })))
}

function setSecureContext(value: boolean) {
  Object.defineProperty(window, 'isSecureContext', { value, configurable: true })
}

beforeEach(() => {
  vi.clearAllMocks()
  mockIsNativeApp.mockReturnValue(false)
  setSecureContext(true)
})

afterEach(() => {
  vi.unstubAllGlobals()
  // Remove the container so a test that asserts absence starts from nothing.
  Reflect.deleteProperty(navigator, 'serviceWorker')
})

describe('registerPwaServiceWorker gates', () => {
  it('registers with scope / and updateViaCache none when every gate passes', async () => {
    const register = installServiceWorkerMock()
    stubHeadResponse(200, 'text/javascript')

    await expect(registerPwaServiceWorker()).resolves.toBe(true)

    expect(register).toHaveBeenCalledTimes(1)
    expect(register).toHaveBeenCalledWith(SW_URL, {
      scope: '/',
      updateViaCache: 'none',
    })
  })

  it('skips registration when the Service Worker API is missing', async () => {
    // No container installed by this test.
    stubHeadResponse(200, 'text/javascript')

    await expect(registerPwaServiceWorker()).resolves.toBe(false)
  })

  it('skips registration outside a secure context (http origin, native WebView)', async () => {
    const register = installServiceWorkerMock()
    stubHeadResponse(200, 'text/javascript')
    setSecureContext(false)

    await expect(registerPwaServiceWorker()).resolves.toBe(false)
    expect(register).not.toHaveBeenCalled()
  })

  it('skips registration inside an iframe', async () => {
    const register = installServiceWorkerMock()
    stubHeadResponse(200, 'text/javascript')

    const realTop = window.top
    Object.defineProperty(window, 'top', { value: {}, configurable: true })
    try {
      await expect(registerPwaServiceWorker()).resolves.toBe(false)
      expect(register).not.toHaveBeenCalled()
    } finally {
      Object.defineProperty(window, 'top', { value: realTop, configurable: true })
    }
  })

  it('skips registration inside the native app', async () => {
    const register = installServiceWorkerMock()
    stubHeadResponse(200, 'text/javascript')
    mockIsNativeApp.mockReturnValue(true)

    await expect(registerPwaServiceWorker()).resolves.toBe(false)
    expect(register).not.toHaveBeenCalled()
  })

  it('skips registration when /sw.js is answered with HTML (dev SPA fallback)', async () => {
    const register = installServiceWorkerMock()
    stubHeadResponse(200, 'text/html; charset=utf-8')

    await expect(registerPwaServiceWorker()).resolves.toBe(false)
    expect(register).not.toHaveBeenCalled()
  })

  it('skips registration when /sw.js is missing', async () => {
    const register = installServiceWorkerMock()
    stubHeadResponse(404, 'text/plain')

    await expect(registerPwaServiceWorker()).resolves.toBe(false)
    expect(register).not.toHaveBeenCalled()
  })

  it('returns false instead of throwing when register() rejects', async () => {
    const register = installServiceWorkerMock()
    register.mockRejectedValue(new Error('registration failed'))
    stubHeadResponse(200, 'text/javascript')

    await expect(registerPwaServiceWorker()).resolves.toBe(false)
  })

  it('returns false instead of throwing when the probe fetch rejects', async () => {
    const register = installServiceWorkerMock()
    vi.stubGlobal('fetch', vi.fn().mockRejectedValue(new Error('offline')))

    await expect(registerPwaServiceWorker()).resolves.toBe(false)
    expect(register).not.toHaveBeenCalled()
  })
})
