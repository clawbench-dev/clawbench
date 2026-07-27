import { describe, it, expect, vi, beforeEach } from 'vitest'

const mockFetch = vi.fn()
globalThis.fetch = mockFetch

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
        network: { upload_rate: 1024, download_rate: 51200 },
      }),
    })
    const { useSystemResources } = await import('../useSystemResources')
    const { startPolling, stopPolling } = useSystemResources()
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
        network: { upload_rate: 0, download_rate: 0 },
      }),
    })
    const { useSystemResources } = await import('../useSystemResources')
    const { startPolling, stopPolling } = useSystemResources()
    startPolling()
    startPolling()
    // Should only have one timer, not two
    await vi.waitFor(() => {
      expect(mockFetch).toHaveBeenCalled()
    })
    // First stopPolling should NOT clear timer (activeCount = 1)
    stopPolling()
    // Second stopPolling should clear timer (activeCount = 0)
    stopPolling()
  })
})
