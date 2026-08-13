import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import {
  manageServiceWorker,
} from '@/utils/serviceWorkerCleanup'

function mockHeaders(ct: string | null): Headers {
  return { get: vi.fn((name: string) => (name === 'content-type' ? ct : null)) } as unknown as Headers
}

function setupServiceWorkerMocks() {
  const unregister = vi.fn().mockResolvedValue(true)
  const reg = { scope: 'https://xulongzhe.top/', unregister }
  const getRegistrations = vi.fn().mockResolvedValue([reg])
  Object.defineProperty(navigator, 'serviceWorker', {
    value: {
      register: vi.fn().mockResolvedValue({ scope: 'https://xulongzhe.top/' }),
      getRegistrations,
    },
    configurable: true,
    writable: true,
  })
  return { unregister, getRegistrations }
}

function setupFetchMock(status: number, ct: string | null) {
  return vi.fn().mockResolvedValue({ ok: status >= 200 && status < 300, status, headers: mockHeaders(ct) })
}

describe('serviceWorkerCleanup', () => {
  const originalSW = navigator.serviceWorker
  const originalFetch = window.fetch
  const originalCaches = window.caches

  beforeEach(() => {
    Object.defineProperty(window, 'fetch', { value: undefined, configurable: true, writable: true })
    Object.defineProperty(window, 'caches', { value: undefined, configurable: true, writable: true })
  })

  afterEach(() => {
    Object.defineProperty(navigator, 'serviceWorker', {
      value: originalSW, configurable: true, writable: true,
    })
    Object.defineProperty(window, 'fetch', { value: originalFetch, configurable: true, writable: true })
    Object.defineProperty(window, 'caches', { value: originalCaches, configurable: true, writable: true })
  })

  it('registers the worker when /sw.js is a valid JS response', async () => {
    setupServiceWorkerMocks()
    Object.defineProperty(window, 'fetch', { value: setupFetchMock(200, 'text/javascript'), configurable: true })
    const register = (navigator.serviceWorker.register as unknown as ReturnType<typeof vi.fn>)
    await manageServiceWorkerWithLoad()
    expect(register).toHaveBeenCalledWith('/sw.js')
  })

  it('unregisters a stale worker and clears caches when /sw.js returns 404', async () => {
    const { unregister, getRegistrations } = setupServiceWorkerMocks()
    Object.defineProperty(window, 'fetch', { value: setupFetchMock(404, 'text/plain'), configurable: true })
    const cacheKeys = vi.fn().mockResolvedValue(['sw-cache', 'v1'])
    const cacheDelete = vi.fn().mockResolvedValue(true)
    Object.defineProperty(window, 'caches', {
      value: { keys: cacheKeys, delete: cacheDelete }, configurable: true,
    })
    await manageServiceWorkerWithLoad()
    expect(getRegistrations).toHaveBeenCalled()
    expect(unregister).toHaveBeenCalled()
    expect(cacheDelete).toHaveBeenCalledWith('sw-cache')
    expect(cacheDelete).toHaveBeenCalledWith('v1')
  })

  it('unregisters even when the fetch check throws (e.g. network error)', async () => {
    const { unregister } = setupServiceWorkerMocks()
    Object.defineProperty(window, 'fetch', {
      value: vi.fn().mockRejectedValue(new TypeError('Failed to fetch')), configurable: true,
    })
    await manageServiceWorkerWithLoad()
    expect(unregister).toHaveBeenCalled()
  })

  it('does nothing when Service Worker is unsupported', async () => {
    const register = vi.fn()
    Object.defineProperty(navigator, 'serviceWorker', { value: undefined, configurable: true, writable: true })
    await manageServiceWorkerWithLoad()
    expect(register).not.toHaveBeenCalled()
  })

  it('does not unregister when the worker is served and registered successfully', async () => {
    const { unregister } = setupServiceWorkerMocks()
    Object.defineProperty(window, 'fetch', { value: setupFetchMock(200, 'application/javascript'), configurable: true })
    await manageServiceWorkerWithLoad()
    expect(unregister).not.toHaveBeenCalled()
  })
})

// manageServiceWorker attaches a 'load' listener (or runs immediately if already
// complete). Force the immediate path so the async flow is awaited deterministically.
async function manageServiceWorkerWithLoad() {
  Object.defineProperty(document, 'readyState', { value: 'complete', configurable: true })
  await manageServiceWorker()
}
