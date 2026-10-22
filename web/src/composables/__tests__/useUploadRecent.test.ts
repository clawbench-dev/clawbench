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
    expect(result.deleteRecentUpload).toBeDefined()
    expect(typeof result.deleteRecentUpload).toBe('function')
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

  it('fetchRecentUploads normalizes a null body to an empty array', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValue({
      ok: true,
      json: () => Promise.resolve(null),
    } as Response)

    await fetchRecentUploads()

    expect(recentUploads.value).toEqual([])
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

  it('deleteRecentUpload success: sends DELETE and removes from list', async () => {
    recentUploads.value = [
      { name: 'a.txt', path: '.clawbench/uploads/a.txt', size: 10, modTime: '' },
      { name: 'b.txt', path: '.clawbench/uploads/b.txt', size: 20, modTime: '' },
    ]
    const { deleteRecentUpload } = useUploadRecent()
    vi.spyOn(globalThis, 'fetch').mockResolvedValue({
      ok: true,
      json: () => Promise.resolve({ ok: true }),
    } as Response)

    const result = await deleteRecentUpload('.clawbench/uploads/a.txt')

    expect(result).toBe(true)
    expect(globalThis.fetch).toHaveBeenCalledWith('/api/upload/recent', {
      method: 'DELETE',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ path: '.clawbench/uploads/a.txt' }),
    })
    expect(recentUploads.value.map(f => f.path)).toEqual(['.clawbench/uploads/b.txt'])
  })

  it('deleteRecentUpload with stale-server array response: keeps list and returns false', async () => {
    recentUploads.value = [{ name: 'a.txt', path: '.clawbench/uploads/a.txt', size: 10, modTime: '' }]
    const { deleteRecentUpload } = useUploadRecent()
    vi.spyOn(globalThis, 'fetch').mockResolvedValue({
      ok: true,
      json: () => Promise.resolve([{ name: 'a.txt', path: '.clawbench/uploads/a.txt', size: 10, modTime: '' }]),
    } as Response)

    const result = await deleteRecentUpload('.clawbench/uploads/a.txt')

    expect(result).toBe(false)
    expect(recentUploads.value.length).toBe(1)
  })

  it('deleteRecentUpload non-ok: keeps list and returns false', async () => {
    recentUploads.value = [{ name: 'a.txt', path: '.clawbench/uploads/a.txt', size: 10, modTime: '' }]
    const { deleteRecentUpload } = useUploadRecent()
    vi.spyOn(globalThis, 'fetch').mockResolvedValue({ ok: false, status: 403 } as Response)

    const result = await deleteRecentUpload('.clawbench/uploads/a.txt')

    expect(result).toBe(false)
    expect(recentUploads.value.length).toBe(1)
  })

  it('deleteRecentUpload error: catches exception and returns false', async () => {
    const { deleteRecentUpload } = useUploadRecent()
    vi.spyOn(globalThis, 'fetch').mockRejectedValue(new TypeError('Network error'))

    const result = await deleteRecentUpload('.clawbench/uploads/a.txt')

    expect(result).toBe(false)
  })
})

describe('useUploadRecent clearRecentUploads', () => {
  let { recentUploads, clearRecentUploads } = useUploadRecent()

  beforeEach(() => {
    recentUploads.value = []
    vi.restoreAllMocks()
  })

  it('sends {all:true} and empties the list', async () => {
    recentUploads.value = [{ name: 'a.png', path: '.clawbench/uploads/a.png', size: 1, modTime: '' }]
    const spy = vi.spyOn(globalThis, 'fetch').mockResolvedValue({
      ok: true,
      json: () => Promise.resolve({ ok: true, deleted: 4, failed: 0 }),
    } as Response)

    const result = await clearRecentUploads()

    // `all` is what tells the server to sweep the directory rather than one
    // file; sending `path` here would delete nothing.
    expect(spy).toHaveBeenCalledWith('/api/upload/recent', expect.objectContaining({
      method: 'DELETE',
      body: JSON.stringify({ all: true }),
    }))
    expect(result).toEqual({ deleted: 4, failed: 0 })
    expect(recentUploads.value).toEqual([])
  })

  it('surfaces the server failure count', async () => {
    const { clearRecentUploads: clear } = useUploadRecent()
    vi.spyOn(globalThis, 'fetch').mockResolvedValue({
      ok: true,
      json: () => Promise.resolve({ ok: true, deleted: 2, failed: 1 }),
    } as Response)

    expect(await clear()).toEqual({ deleted: 2, failed: 1 })
  })

  it('re-fetches instead of blanking the list on a partial sweep', async () => {
    // `failed > 0` means rows are still on the server (an unwritable data
    // directory, an undeletable file). Blanking the local list would show an
    // empty tab that repopulates on the next open, with no explanation.
    const surviving = [{ name: 'p.png', path: 'p.png', size: 1, modTime: '', origin: 'project' }]
    recentUploads.value = [{ name: 'p.png', path: 'p.png', size: 1, modTime: '', origin: 'project' }]
    const spy = vi.spyOn(globalThis, 'fetch')
      .mockResolvedValueOnce({
        ok: true,
        json: () => Promise.resolve({ ok: true, deleted: 1, failed: 1 }),
      } as Response)
      .mockResolvedValueOnce({
        ok: true,
        json: () => Promise.resolve(surviving),
      } as Response)

    const result = await clearRecentUploads()

    expect(result).toEqual({ deleted: 1, failed: 1 })
    expect(spy).toHaveBeenCalledTimes(2)
    expect(spy.mock.calls[1][0]).toBe('/api/upload/recent')
    expect(spy.mock.calls[1][1]).toBeUndefined()
    expect(recentUploads.value).toEqual(surviving)
  })

  it('keeps the list and returns null when the server answers an old-format body', async () => {
    // An older server ignores `all`, answers 400 (or 200 + an array). Treating
    // that as success would hide files that are still on disk.
    recentUploads.value = [{ name: 'a.png', path: '.clawbench/uploads/a.png', size: 1, modTime: '' }]
    vi.spyOn(globalThis, 'fetch').mockResolvedValue({
      ok: true,
      json: () => Promise.resolve([{ name: 'a.png', path: '.clawbench/uploads/a.png' }]),
    } as Response)

    expect(await clearRecentUploads()).toBeNull()
    expect(recentUploads.value).toHaveLength(1)
  })

  it('keeps the list and returns null on a non-ok response', async () => {
    recentUploads.value = [{ name: 'a.png', path: '.clawbench/uploads/a.png', size: 1, modTime: '' }]
    vi.spyOn(globalThis, 'fetch').mockResolvedValue({ ok: false, status: 400 } as Response)

    expect(await clearRecentUploads()).toBeNull()
    expect(recentUploads.value).toHaveLength(1)
  })

  it('returns null on a network error instead of throwing', async () => {
    vi.spyOn(globalThis, 'fetch').mockRejectedValue(new TypeError('Network error'))
    expect(await clearRecentUploads()).toBeNull()
  })
})
