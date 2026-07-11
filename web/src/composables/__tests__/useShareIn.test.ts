import { describe, expect, it, vi, beforeEach } from 'vitest'

// Mock fetch for testing
const mockFetch = vi.fn()
global.fetch = mockFetch

// Reset module state between tests by re-importing
describe('useShareIn', () => {
  beforeEach(() => {
    mockFetch.mockReset()
  })

  it('returns empty array initially', async () => {
    mockFetch.mockResolvedValue({
      ok: true,
      json: async () => [{ name: 'test.txt', path: '.clawbench/share-in/test.txt', size: 100, modTime: '2026-07-11T00:00:00Z' }],
    })

    // Dynamic import to get fresh module state
    const { useShareIn } = await import('@/composables/useShareIn.ts')
    const { recentShares } = useShareIn()
    // recentShares starts empty before fetch
    expect(recentShares.value).toEqual([])
  })

  it('fetchRecentShares populates recentShares', async () => {
    const files = [
      { name: 'photo.jpg', path: '.clawbench/share-in/photo.jpg', size: 2048, modTime: '2026-07-11T12:00:00Z' },
      { name: 'doc.pdf', path: '.clawbench/share-in/doc.pdf', size: 1024, modTime: '2026-07-11T11:00:00Z' },
    ]
    mockFetch.mockResolvedValue({
      ok: true,
      json: async () => files,
    })

    const { useShareIn } = await import('@/composables/useShareIn.ts')
    const { recentShares, fetchRecentShares } = useShareIn()
    await fetchRecentShares()

    expect(mockFetch).toHaveBeenCalledWith('/api/share-in/recent')
    expect(recentShares.value).toEqual(files)
  })
})
