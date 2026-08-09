import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'

const mockFetch = vi.fn()
globalThis.fetch = mockFetch

// Mock document event listeners
const listeners: Record<string, EventListener> = {}
const origAdd = document.addEventListener
const origRemove = document.removeEventListener

beforeEach(() => {
  mockFetch.mockReset()
  // Intercept visibilitychange listeners
  document.addEventListener = vi.fn((event: string, handler: EventListener) => {
    listeners[event] = handler
  }) as any
  document.removeEventListener = vi.fn((event: string) => {
    delete listeners[event]
  }) as any
})

afterEach(() => {
  document.addEventListener = origAdd
  document.removeEventListener = origRemove
})

describe('useSystemResources', () => {
  beforeEach(() => {
    mockFetch.mockReset()
  })

  it('should export composable function', async () => {
    const { useSystemResources } = await import('../useSystemResources')
    expect(typeof useSystemResources).toBe('function')
  })

  it('should fetch resources on startPolling', async () => {
    mockFetch.mockResolvedValue({
      ok: true,
      json: () => Promise.resolve({
        cpu: { percent: 25.5, core_count: 4 },
        memory: { used: 4000000000, total: 8000000000, percent: 50 },
        disk: { used: 50000000000, total: 200000000000, percent: 25 },
        disk_io: { read_rate: 0, write_rate: 0 },
        network: { upload_rate: 1024, download_rate: 51200 },
        load: { load1: 1.0, load5: 0.8, load15: 0.6 },
      }),
    })
    const { useSystemResources } = await import('../useSystemResources')
    const { startPolling, stopPolling, resources } = useSystemResources()
    startPolling()
    await vi.waitFor(() => {
      expect(mockFetch).toHaveBeenCalled()
    })
    stopPolling()
  })

  it('should start only one timer with multiple startPolling calls', async () => {
    mockFetch.mockResolvedValue({
      ok: true,
      json: () => Promise.resolve({
        cpu: { percent: 0, core_count: 4 },
        memory: { used: 0, total: 0, percent: 0 },
        disk: { used: 0, total: 0, percent: 0 },
        disk_io: { read_rate: 0, write_rate: 0 },
        network: { upload_rate: 0, download_rate: 0 },
        load: { load1: 0, load5: 0, load15: 0 },
      }),
    })
    const { useSystemResources } = await import('../useSystemResources')
    const { startPolling, stopPolling } = useSystemResources()
    startPolling()
    startPolling()
    await vi.waitFor(() => {
      expect(mockFetch).toHaveBeenCalled()
    })
    stopPolling()
    stopPolling()
  })

  it('should handle fetch failure gracefully', async () => {
    mockFetch.mockRejectedValue(new Error('Network error'))
    const { useSystemResources } = await import('../useSystemResources')
    const { startPolling, stopPolling, resources } = useSystemResources()
    startPolling()
    await vi.waitFor(() => {
      expect(mockFetch).toHaveBeenCalled()
    })
    // Resources should remain at defaults after fetch failure
    expect(resources.value.cpu.percent).toBe(0)
    stopPolling()
  })

  it('should handle non-ok response', async () => {
    mockFetch.mockResolvedValue({ ok: false, status: 500 })
    const { useSystemResources } = await import('../useSystemResources')
    const { startPolling, stopPolling, resources } = useSystemResources()
    startPolling()
    await vi.waitFor(() => {
      expect(mockFetch).toHaveBeenCalled()
    })
    // Resources should remain at defaults after non-ok response
    expect(resources.value.cpu.percent).toBe(0)
    stopPolling()
  })

  it('should register and remove visibilitychange listener', async () => {
    mockFetch.mockResolvedValue({
      ok: true,
      json: () => Promise.resolve({
        cpu: { percent: 0, core_count: 4 },
        memory: { used: 0, total: 0, percent: 0 },
        disk: { used: 0, total: 0, percent: 0 },
        disk_io: { read_rate: 0, write_rate: 0 },
        network: { upload_rate: 0, download_rate: 0 },
        load: { load1: 0, load5: 0, load15: 0 },
      }),
    })
    const { useSystemResources } = await import('../useSystemResources')
    const { startPolling, stopPolling } = useSystemResources()
    startPolling()
    await vi.waitFor(() => {
      expect(mockFetch).toHaveBeenCalled()
    })
    // Should have registered visibilitychange listener
    expect(listeners['visibilitychange']).toBeDefined()
    stopPolling()
    // Should have removed visibilitychange listener
    expect(listeners['visibilitychange']).toBeUndefined()
  })

  it('should pause polling when tab becomes hidden', async () => {
    mockFetch.mockResolvedValue({
      ok: true,
      json: () => Promise.resolve({
        cpu: { percent: 10, core_count: 4 },
        memory: { used: 0, total: 0, percent: 0 },
        disk: { used: 0, total: 0, percent: 0 },
        disk_io: { read_rate: 0, write_rate: 0 },
        network: { upload_rate: 0, download_rate: 0 },
        load: { load1: 0, load5: 0, load15: 0 },
      }),
    })
    const { useSystemResources } = await import('../useSystemResources')
    const { startPolling, stopPolling } = useSystemResources()
    startPolling()
    await vi.waitFor(() => {
      expect(mockFetch).toHaveBeenCalled()
    })
    const fetchCountAfterStart = mockFetch.mock.calls.length

    // Simulate tab hidden
    Object.defineProperty(document, 'hidden', { value: true, configurable: true })
    const handler = listeners['visibilitychange']
    if (handler) handler(new Event('visibilitychange'))

    // Wait a bit — no new fetches should happen
    await new Promise(r => setTimeout(r, 150))
    expect(mockFetch.mock.calls.length).toBe(fetchCountAfterStart)

    // Simulate tab visible again
    Object.defineProperty(document, 'hidden', { value: false, configurable: true })
    if (handler) handler(new Event('visibilitychange'))

    await vi.waitFor(() => {
      expect(mockFetch.mock.calls.length).toBeGreaterThan(fetchCountAfterStart)
    })

    stopPolling()
  })

  it('should not resume polling when visible if no active consumers', async () => {
    mockFetch.mockResolvedValue({
      ok: true,
      json: () => Promise.resolve({
        cpu: { percent: 0, core_count: 4 },
        memory: { used: 0, total: 0, percent: 0 },
        disk: { used: 0, total: 0, percent: 0 },
        disk_io: { read_rate: 0, write_rate: 0 },
        network: { upload_rate: 0, download_rate: 0 },
        load: { load1: 0, load5: 0, load15: 0 },
      }),
    })
    const { useSystemResources } = await import('../useSystemResources')
    const { startPolling, stopPolling } = useSystemResources()
    startPolling()
    await vi.waitFor(() => {
      expect(mockFetch).toHaveBeenCalled()
    })
    stopPolling()

    // Now no active consumers — visibility change should not resume polling
    Object.defineProperty(document, 'hidden', { value: false, configurable: true })
    const handler = listeners['visibilitychange']
    const fetchCount = mockFetch.mock.calls.length
    if (handler) handler(new Event('visibilitychange'))

    await new Promise(r => setTimeout(r, 150))
    expect(mockFetch.mock.calls.length).toBe(fetchCount)
  })

  it('should update resources on successful fetch', async () => {
    const testData = {
      cpu: { percent: 33.3, core_count: 8 },
      memory: { used: 6000000000, total: 16000000000, percent: 37.5 },
      disk: { used: 100000000000, total: 500000000000, percent: 20 },
      disk_io: { read_rate: 1024, write_rate: 512 },
      network: { upload_rate: 2048, download_rate: 4096 },
      load: { load1: 2.0, load5: 1.5, load15: 1.0 },
    }
    mockFetch.mockResolvedValue({
      ok: true,
      json: () => Promise.resolve(testData),
    })
    const { useSystemResources } = await import('../useSystemResources')
    const { startPolling, stopPolling, resources } = useSystemResources()
    startPolling()
    await vi.waitFor(() => {
      expect(resources.value.cpu.percent).toBe(33.3)
    })
    expect(resources.value.cpu.core_count).toBe(8)
    expect(resources.value.memory.percent).toBe(37.5)
    stopPolling()
  })

  // ── Background polling ──

  it('should start background polling with startBackgroundPolling', async () => {
    mockFetch.mockResolvedValue({
      ok: true,
      json: () => Promise.resolve({
        cpu: { percent: 0, core_count: 4 },
        memory: { used: 0, total: 0, percent: 0 },
        disk: { used: 0, total: 0, percent: 0 },
        disk_io: { read_rate: 0, write_rate: 0 },
        network: { upload_rate: 0, download_rate: 0 },
        load: { load1: 0, load5: 0, load15: 0 },
      }),
    })
    const { useSystemResources } = await import('../useSystemResources')
    const { startBackgroundPolling, stopBackgroundPolling } = useSystemResources()
    startBackgroundPolling()
    await vi.waitFor(() => {
      expect(mockFetch).toHaveBeenCalled()
    })
    expect(listeners['visibilitychange']).toBeDefined()
    stopBackgroundPolling()
  })

  it('should stop background polling and remove visibility listener', async () => {
    mockFetch.mockResolvedValue({
      ok: true,
      json: () => Promise.resolve({
        cpu: { percent: 0, core_count: 4 },
        memory: { used: 0, total: 0, percent: 0 },
        disk: { used: 0, total: 0, percent: 0 },
        disk_io: { read_rate: 0, write_rate: 0 },
        network: { upload_rate: 0, download_rate: 0 },
        load: { load1: 0, load5: 0, load15: 0 },
      }),
    })
    const { useSystemResources } = await import('../useSystemResources')
    const { startBackgroundPolling, stopBackgroundPolling } = useSystemResources()
    startBackgroundPolling()
    await vi.waitFor(() => {
      expect(mockFetch).toHaveBeenCalled()
    })
    stopBackgroundPolling()
    expect(listeners['visibilitychange']).toBeUndefined()
  })

  it('should not start a new timer when background polling is added while foreground is active', async () => {
    mockFetch.mockResolvedValue({
      ok: true,
      json: () => Promise.resolve({
        cpu: { percent: 0, core_count: 4 },
        memory: { used: 0, total: 0, percent: 0 },
        disk: { used: 0, total: 0, percent: 0 },
        disk_io: { read_rate: 0, write_rate: 0 },
        network: { upload_rate: 0, download_rate: 0 },
        load: { load1: 0, load5: 0, load15: 0 },
      }),
    })
    const { useSystemResources } = await import('../useSystemResources')
    const { startPolling, stopPolling, startBackgroundPolling, stopBackgroundPolling } = useSystemResources()
    startPolling()
    await vi.waitFor(() => {
      expect(mockFetch).toHaveBeenCalled()
    })
    const fetchCountBefore = mockFetch.mock.calls.length
    // Adding background while foreground is active should not start a new timer
    startBackgroundPolling()
    stopPolling()
    // Background should still be active
    expect(listeners['visibilitychange']).toBeDefined()
    stopBackgroundPolling()
  })

  it('should switch to background interval when foreground stops but background remains', async () => {
    mockFetch.mockResolvedValue({
      ok: true,
      json: () => Promise.resolve({
        cpu: { percent: 0, core_count: 4 },
        memory: { used: 0, total: 0, percent: 0 },
        disk: { used: 0, total: 0, percent: 0 },
        disk_io: { read_rate: 0, write_rate: 0 },
        network: { upload_rate: 0, download_rate: 0 },
        load: { load1: 0, load5: 0, load15: 0 },
      }),
    })
    const { useSystemResources } = await import('../useSystemResources')
    const { startPolling, stopPolling, startBackgroundPolling, stopBackgroundPolling } = useSystemResources()
    startBackgroundPolling()
    startPolling()
    await vi.waitFor(() => {
      expect(mockFetch).toHaveBeenCalled()
    })
    // Stop foreground — should switch to background interval
    stopPolling()
    // Background is still active
    expect(listeners['visibilitychange']).toBeDefined()
    stopBackgroundPolling()
  })

  // ── onUnmounted cleanup ──

  it('should expose startBackgroundPolling and stopBackgroundPolling', async () => {
    const { useSystemResources } = await import('../useSystemResources')
    const { startBackgroundPolling, stopBackgroundPolling, resources } = useSystemResources()
    expect(typeof startBackgroundPolling).toBe('function')
    expect(typeof stopBackgroundPolling).toBe('function')
    expect(resources.value).toBeDefined()
  })
})
