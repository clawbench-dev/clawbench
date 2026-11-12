import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest'
import type { Mock } from 'vitest'

vi.mock('@/utils/api', () => ({ apiGet: vi.fn() }))
vi.mock('@/utils/appLog', () => ({
  appLog: { d: vi.fn(), i: vi.fn(), w: vi.fn(), e: vi.fn() },
}))

import { apiGet } from '@/utils/api'
import { appLog } from '@/utils/appLog'
import { useAtFileSearch } from '@/composables/useAtFileSearch'

const mockApiGet = apiGet as unknown as Mock
const mockLogWarn = appLog.w as unknown as Mock

beforeEach(() => {
  mockApiGet.mockReset()
  mockLogWarn.mockReset()
  vi.useFakeTimers()
})

afterEach(() => {
  vi.useRealTimers()
})

describe('useAtFileSearch', () => {
  it('debounces: nothing is requested before the delay', async () => {
    mockApiGet.mockResolvedValue({ items: [] })
    const { schedule } = useAtFileSearch()

    schedule('ma')
    await vi.advanceTimersByTimeAsync(249)
    expect(mockApiGet).not.toHaveBeenCalled()

    await vi.advanceTimersByTimeAsync(1)
    expect(mockApiGet).toHaveBeenCalledTimes(1)
  })

  it('requests the ranked top endpoint with an encoded query and a small limit', async () => {
    mockApiGet.mockResolvedValue({ items: [] })
    const { schedule } = useAtFileSearch()

    schedule('  my file  ')
    await vi.advanceTimersByTimeAsync(250)

    expect(mockApiGet).toHaveBeenCalledWith(
      '/api/dir/search/top?q=my%20file&limit=8',
      expect.objectContaining({ signal: expect.anything() }),
    )
  })

  it('maps hits, deriving the display name from the path and flagging dirs', async () => {
    mockApiGet.mockResolvedValue({
      items: [
        { name: 'ChatInputBar.vue', path: 'web/src/components/chat/ChatInputBar.vue', type: 'file' },
        { path: 'web/src/components/chat', type: 'dir' },
        { name: 'ignored-no-path', type: 'file' },
      ],
    })
    const { hits, hitsQuery, schedule } = useAtFileSearch()

    schedule('chat')
    await vi.advanceTimersByTimeAsync(250)

    expect(hitsQuery.value).toBe('chat')
    expect(hits.value).toEqual([
      { name: 'ChatInputBar.vue', path: 'web/src/components/chat/ChatInputBar.vue', isDir: false },
      { name: 'chat', path: 'web/src/components/chat', isDir: true },
    ])
  })

  it('ignores a stale response that resolves after a newer query', async () => {
    let resolveFirst: (v: unknown) => void = () => {}
    mockApiGet
      .mockImplementationOnce(() => new Promise(resolve => { resolveFirst = resolve }))
      .mockResolvedValueOnce({ items: [{ name: 'newer.ts', path: 'src/newer.ts', type: 'file' }] })

    const { hits, hitsQuery, schedule } = useAtFileSearch()

    schedule('old')
    await vi.advanceTimersByTimeAsync(250)
    schedule('newer')
    await vi.advanceTimersByTimeAsync(250)

    expect(hitsQuery.value).toBe('newer')

    // The first request finally answers — it must not overwrite the newer one.
    resolveFirst({ items: [{ name: 'old.ts', path: 'src/old.ts', type: 'file' }] })
    await vi.advanceTimersByTimeAsync(0)

    expect(hitsQuery.value).toBe('newer')
    expect(hits.value.map(h => h.path)).toEqual(['src/newer.ts'])
  })

  it('clear() drops results and cancels a pending search', async () => {
    mockApiGet.mockResolvedValue({ items: [{ name: 'a.ts', path: 'a.ts', type: 'file' }] })
    const { hits, hitsQuery, schedule, clear } = useAtFileSearch()

    schedule('a')
    await vi.advanceTimersByTimeAsync(250)
    expect(hits.value).toHaveLength(1)

    schedule('ab')
    clear()
    await vi.advanceTimersByTimeAsync(500)

    expect(hits.value).toEqual([])
    expect(hitsQuery.value).toBe('')
    expect(mockApiGet).toHaveBeenCalledTimes(1)
  })

  it('an empty query clears instead of searching', async () => {
    const { schedule } = useAtFileSearch()
    schedule('   ')
    await vi.advanceTimersByTimeAsync(500)
    expect(mockApiGet).not.toHaveBeenCalled()
  })

  it('swallows aborted requests but reports real failures', async () => {
    const abort = Object.assign(new Error('aborted'), { name: 'AbortError' })
    mockApiGet.mockRejectedValueOnce(abort)
    const { schedule } = useAtFileSearch()
    schedule('a')
    await vi.advanceTimersByTimeAsync(250)
    expect(mockLogWarn).not.toHaveBeenCalled()

    mockApiGet.mockRejectedValueOnce(new Error('boom'))
    schedule('b')
    await vi.advanceTimersByTimeAsync(250)
    expect(mockLogWarn).toHaveBeenCalledTimes(1)
  })
})
