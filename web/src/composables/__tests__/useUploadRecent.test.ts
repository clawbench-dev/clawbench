import { describe, expect, it, vi, beforeEach } from 'vitest'

vi.mock('@/utils/appLog', () => ({
  appLog: { w: vi.fn() },
}))

import { useUploadRecent } from '@/composables/useUploadRecent'

describe('useUploadRecent', () => {
  let { recentUploads, fetchRecentUploads } = useUploadRecent()

  beforeEach(() => {
    recentUploads.value = []
    vi.restoreAllMocks()
  })

  it('recentUploads initial value is empty array', () => {
    expect(recentUploads.value).toEqual([])
  })

  it('useUploadRecent returns the expected interface', () => {
    const result = useUploadRecent()
    expect(result.recentUploads).toBeDefined()
    expect(result.fetchRecentUploads).toBeDefined()
    expect(typeof result.fetchRecentUploads).toBe('function')
  })

  it('fetchRecentUploads success: populates recentUploads', async () => {
    const data = [
      { name: 'file1.txt', path: '/tmp/file1.txt', size: 100, modTime: '2025-01-01' },
      { name: 'file2.txt', path: '/tmp/file2.txt', size: 200, modTime: '2025-01-02' },
    ]
    vi.spyOn(globalThis, 'fetch').mockResolvedValue({
      ok: true,
      json: () => Promise.resolve(data),
    } as Response)

    await fetchRecentUploads()

    expect(recentUploads.value).toEqual(data)
    expect(globalThis.fetch).toHaveBeenCalledWith('/api/upload/recent')
  })

  it('fetchRecentUploads non-ok: does not update recentUploads', async () => {
    recentUploads.value = [{ name: 'existing.txt', path: '/a', size: 1, modTime: '' }]
    vi.spyOn(globalThis, 'fetch').mockResolvedValue({
      ok: false,
      status: 500,
      json: () => Promise.resolve({ error: 'fail' }),
    } as Response)

    await fetchRecentUploads()

    expect(recentUploads.value).toEqual([{ name: 'existing.txt', path: '/a', size: 1, modTime: '' }])
  })

  it('fetchRecentUploads error: catches exception and logs warning', async () => {
    const { appLog } = await import('@/utils/appLog')
    const error = new TypeError('Network error')
    vi.spyOn(globalThis, 'fetch').mockRejectedValue(error)

    await fetchRecentUploads()

    expect(recentUploads.value).toEqual([])
    expect(appLog.w).toHaveBeenCalledWith('UploadRecent', 'Failed to fetch recent uploads', error)
  })
})
