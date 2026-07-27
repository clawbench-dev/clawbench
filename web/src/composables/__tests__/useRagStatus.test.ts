import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { useRagStatus, _resetForTesting } from '../useRagStatus'
import { apiGet } from '@/utils/api'

vi.mock('@/utils/api', () => ({
  apiGet: vi.fn(),
}))

vi.mock('@/utils/appLog', () => ({
  appLog: { d: vi.fn(), i: vi.fn(), w: vi.fn(), e: vi.fn() },
}))

describe('useRagStatus', () => {
  beforeEach(() => {
    vi.useFakeTimers()
    vi.mocked(apiGet).mockResolvedValue({
      available: false, mode: 'none', has_fts_data: false, has_vec_data: false,
      embedder_healthy: false, total_messages: 0, indexed_messages: 0,
      total_chunks: 0, embedded_chunks: 0,
    })
  })

  afterEach(() => {
    _resetForTesting()
    vi.useRealTimers()
    vi.restoreAllMocks()
  })

  it('fetches status on startPolling', async () => {
    const mockStatus = {
      available: true,
      mode: 'hybrid',
      has_fts_data: true,
      has_vec_data: true,
      embedder_healthy: true,
      total_messages: 100,
      indexed_messages: 80,
      total_chunks: 200,
      embedded_chunks: 150,
    }
    vi.mocked(apiGet).mockResolvedValue(mockStatus)

    const { status, startPolling, stopPolling } = useRagStatus()
    startPolling()

    await vi.waitFor(() => {
      expect(apiGet).toHaveBeenCalledWith('/api/rag/status')
    })

    await vi.waitFor(() => {
      expect(status.value.total_messages).toBe(100)
      expect(status.value.embedder_healthy).toBe(true)
      expect(status.value.mode).toBe('hybrid')
    })

    stopPolling()
  })

  it('polls at interval', async () => {
    const { startPolling, stopPolling } = useRagStatus()
    startPolling()

    // Wait for initial fetch
    await vi.waitFor(() => {
      expect(apiGet).toHaveBeenCalled()
    })

    const callCountBefore = vi.mocked(apiGet).mock.calls.length

    vi.advanceTimersByTime(10_000)
    await vi.waitFor(() => {
      expect(vi.mocked(apiGet).mock.calls.length).toBeGreaterThan(callCountBefore)
    })

    stopPolling()
  })

  it('stops polling when stopPolling is called', async () => {
    const { startPolling, stopPolling } = useRagStatus()
    startPolling()

    await vi.waitFor(() => {
      expect(apiGet).toHaveBeenCalled()
    })

    stopPolling()
    const callCount = vi.mocked(apiGet).mock.calls.length

    vi.advanceTimersByTime(30_000)
    // No additional calls after stopping
    expect(vi.mocked(apiGet).mock.calls.length).toBe(callCount)
  })
})
