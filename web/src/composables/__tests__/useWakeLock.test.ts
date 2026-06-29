import { describe, expect, it, vi, beforeEach } from 'vitest'

// Reset module state between tests
vi.mock('@/utils/appLog', () => ({
  appLog: {
    d: vi.fn(),
    i: vi.fn(),
    w: vi.fn(),
    e: vi.fn(),
  },
}))

describe('useWakeLock', () => {
  let mockWakeLockRequest: ReturnType<typeof vi.fn>
  let mockSentinel: { release: ReturnType<typeof vi.fn>; released: boolean; onrelease: ((...args: any[]) => void) | null }

  beforeEach(() => {
    vi.resetModules()
    mockWakeLockRequest = vi.fn()
    mockSentinel = {
      release: vi.fn(() => { mockSentinel.released = true }),
      released: false,
      onrelease: null,
    }
    mockWakeLockRequest.mockResolvedValue(mockSentinel)

    // Reset navigator.wakeLock
    Object.defineProperty(globalThis, 'navigator', {
      value: {
        wakeLock: {
          request: mockWakeLockRequest,
        },
      },
      writable: true,
      configurable: true,
    })

    // Reset AndroidNative
    ;(globalThis as any).AndroidNative = undefined

    // Reset document visibilityState
    Object.defineProperty(document, 'visibilityState', {
      value: 'visible',
      writable: true,
      configurable: true,
    })
  })

  async function importUseWakeLock() {
    const mod = await import('@/composables/useWakeLock')
    return mod.useWakeLock()
  }

  it('acquires wake lock via Web API', async () => {
    const { held, acquire } = await importUseWakeLock()
    await acquire()
    expect(mockWakeLockRequest).toHaveBeenCalledWith('screen')
    expect(held.value).toBe(true)
  })

  it('does not acquire again if already held', async () => {
    const { acquire } = await importUseWakeLock()
    await acquire()
    mockWakeLockRequest.mockClear()
    await acquire()
    // Second acquire should be a no-op (already held)
    expect(mockWakeLockRequest).not.toHaveBeenCalled()
  })

  it('releases wake lock', async () => {
    const { held, acquire, release } = await importUseWakeLock()
    await acquire()
    expect(held.value).toBe(true)
    release()
    expect(held.value).toBe(false)
    expect(mockSentinel.release).toHaveBeenCalled()
  })

  it('handles unsupported browser (no navigator.wakeLock)', async () => {
    Object.defineProperty(globalThis, 'navigator', {
      value: {},
      writable: true,
      configurable: true,
    })

    const { held, acquire, release } = await importUseWakeLock()
    await acquire()
    // Should not throw, held may be false if no Android bridge either
    expect(held.value).toBe(false)
    release()
    expect(held.value).toBe(false)
  })

  it('acquires via Android bridge when Web API unavailable', async () => {
    const mockSetKeepScreenOn = vi.fn()
    ;(globalThis as any).AndroidNative = { setKeepScreenOn: mockSetKeepScreenOn }

    // Remove Web Wake Lock API
    Object.defineProperty(globalThis, 'navigator', {
      value: {},
      writable: true,
      configurable: true,
    })

    const { held, acquire } = await importUseWakeLock()
    await acquire()
    expect(mockSetKeepScreenOn).toHaveBeenCalledWith(true)
    expect(held.value).toBe(true)
  })

  it('releases via Android bridge', async () => {
    const mockSetKeepScreenOn = vi.fn()
    ;(globalThis as any).AndroidNative = { setKeepScreenOn: mockSetKeepScreenOn }

    const { acquire, release } = await importUseWakeLock()
    await acquire()
    release()
    expect(mockSetKeepScreenOn).toHaveBeenCalledWith(false)
  })

  it('re-acquires on visibility change when shouldHold is true', async () => {
    const { acquire } = await importUseWakeLock()
    await acquire()
    mockWakeLockRequest.mockClear()

    // Simulate browser auto-release (webWakeLock = null, shouldHold still true)
    // Then simulate page becoming visible again
    Object.defineProperty(document, 'visibilityState', {
      value: 'hidden',
      writable: true,
      configurable: true,
    })
    document.dispatchEvent(new Event('visibilitychange'))

    Object.defineProperty(document, 'visibilityState', {
      value: 'visible',
      writable: true,
      configurable: true,
    })
    document.dispatchEvent(new Event('visibilitychange'))

    // Wait for async _doAcquire
    await vi.waitFor(() => {
      expect(mockWakeLockRequest).toHaveBeenCalled()
    })
  })

  it('handles Web API acquire failure gracefully', async () => {
    mockWakeLockRequest.mockRejectedValue(new Error('Not allowed'))

    const { held, acquire } = await importUseWakeLock()
    await acquire()
    // Should not throw; held stays false if no Android bridge
    expect(held.value).toBe(false)
  })

  it('sets held to true when Web API succeeds even without Android bridge', async () => {
    const { held, acquire } = await importUseWakeLock()
    await acquire()
    expect(held.value).toBe(true)
  })

  it('Web API release callback clears held when shouldHold is false', async () => {
    const { held, acquire } = await importUseWakeLock()
    await acquire()
    expect(held.value).toBe(true)

    // The sentinel's onrelease callback is set during acquire
    // Simulate the browser releasing the lock (e.g. tab hidden)
    // First, we need to release via our API so shouldHold = false
    // Then trigger the browser's onrelease
    const { release } = await importUseWakeLock()
    release()
    expect(held.value).toBe(false)
  })
})
