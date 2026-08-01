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
    vi.mocked(apiGet).mockResolvedValue({
      available: false, mode: 'none', has_fts_data: false, has_vec_data: false,
      embedder_healthy: false, total_messages: 0, indexed_messages: 0,
      embedded_messages: 0,
    })
  })

  afterEach(() => {
    _resetForTesting()
    vi.restoreAllMocks()
  })

  it('fetches status on refresh', async () => {
    const mockStatus = {
      available: true,
      mode: 'hybrid',
      has_fts_data: true,
      has_vec_data: true,
      embedder_healthy: true,
      total_messages: 100,
      indexed_messages: 80,
      embedded_messages: 60,
    }
    vi.mocked(apiGet).mockResolvedValue(mockStatus)

    const { status, refresh } = useRagStatus()
    await refresh()

    expect(apiGet).toHaveBeenCalledWith('/api/rag/status')
    expect(status.value.total_messages).toBe(100)
    expect(status.value.indexed_messages).toBe(80)
    expect(status.value.embedded_messages).toBe(60)
    expect(status.value.embedder_healthy).toBe(true)
    expect(status.value.mode).toBe('hybrid')
  })

  it('has no speed fields in status', () => {
    const { status } = useRagStatus()
    expect('index_speed' in status.value).toBe(false)
    expect('embed_speed' in status.value).toBe(false)
  })

  it('handles fetch error gracefully', async () => {
    vi.mocked(apiGet).mockRejectedValue(new Error('Network error'))

    const { status, refresh } = useRagStatus()
    await refresh()

    // Status should remain at default values
    expect(status.value.available).toBe(false)
    expect(status.value.total_messages).toBe(0)
  })
})
