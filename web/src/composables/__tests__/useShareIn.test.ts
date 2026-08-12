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

  it('fetchRecentShares normalizes a null body to an empty array', async () => {
    // The backend encodes a nil slice as `null` when the (existing but empty)
    // share-in dir has no files. The frontend must treat it as an empty array,
    // otherwise the AttachDrawer Shares tab crashes on `recentShares.length`.
    mockFetch.mockResolvedValue({ ok: true, json: async () => null })

    const { useShareIn } = await import('@/composables/useShareIn.ts')
    const { recentShares, fetchRecentShares } = useShareIn()
    await fetchRecentShares()

    expect(recentShares.value).toEqual([])
  })

  it('deleteRecentShare success: sends DELETE and removes from list', async () => {
    const files = [
      { name: 'a.txt', path: '.clawbench/share-in/a.txt', size: 10, modTime: 't' },
      { name: 'b.txt', path: '.clawbench/share-in/b.txt', size: 20, modTime: 't' },
    ]
    mockFetch.mockResolvedValue({ ok: true, json: async () => ({ ok: true }) })

    const { useShareIn } = await import('@/composables/useShareIn.ts')
    const { recentShares, deleteRecentShare } = useShareIn()
    recentShares.value = files

    const result = await deleteRecentShare('.clawbench/share-in/a.txt')

    expect(result).toBe(true)
    expect(mockFetch).toHaveBeenCalledWith('/api/share-in/recent', {
      method: 'DELETE',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ path: '.clawbench/share-in/a.txt' }),
    })
    expect(recentShares.value.map(f => f.path)).toEqual(['.clawbench/share-in/b.txt'])
  })

  it('deleteRecentShare with stale-server array response: keeps list and returns false', async () => {
    const files = [{ name: 'a.txt', path: '.clawbench/share-in/a.txt', size: 10, modTime: 't' }]
    mockFetch.mockResolvedValue({ ok: true, json: async () => files })

    const { useShareIn } = await import('@/composables/useShareIn.ts')
    const { recentShares, deleteRecentShare } = useShareIn()
    recentShares.value = files

    const result = await deleteRecentShare('.clawbench/share-in/a.txt')

    expect(result).toBe(false)
    expect(recentShares.value.length).toBe(1)
  })

  it('deleteRecentShare non-ok: keeps list and returns false', async () => {
    const files = [{ name: 'a.txt', path: '.clawbench/share-in/a.txt', size: 10, modTime: 't' }]
    mockFetch.mockResolvedValue({ ok: false, status: 403 })

    const { useShareIn } = await import('@/composables/useShareIn.ts')
    const { recentShares, deleteRecentShare } = useShareIn()
    recentShares.value = files

    const result = await deleteRecentShare('.clawbench/share-in/a.txt')

    expect(result).toBe(false)
    expect(recentShares.value.length).toBe(1)
  })
})
