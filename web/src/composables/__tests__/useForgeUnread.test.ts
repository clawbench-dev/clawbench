import { describe, expect, it, vi, beforeEach } from 'vitest'

const mockFetchUnread = vi.fn()
const mockMarkRead = vi.fn()

vi.mock('@/utils/forgeApi', () => ({
  fetchForgeUnread: (...a: unknown[]) => mockFetchUnread(...a),
  markForgeRead: (...a: unknown[]) => mockMarkRead(...a),
}))

import { useForgeUnread } from '@/composables/useForgeUnread'

describe('useForgeUnread', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    // Reset the module singleton between tests.
    useForgeUnread().forgeUnreadCount.value = 0
  })

  it('refresh sets the count from the server', async () => {
    mockFetchUnread.mockResolvedValue({ count: 5 })
    const { forgeUnreadCount, refresh } = useForgeUnread()
    await refresh()
    expect(forgeUnreadCount.value).toBe(5)
  })

  it('a failed refresh keeps the last known count', async () => {
    mockFetchUnread.mockResolvedValue({ count: 3 })
    const { forgeUnreadCount, refresh } = useForgeUnread()
    await refresh()
    expect(forgeUnreadCount.value).toBe(3)

    mockFetchUnread.mockRejectedValue(new Error('network'))
    await refresh()
    // A failed poll must not clear the badge.
    expect(forgeUnreadCount.value).toBe(3)
  })

  it('onForgeEvent refetches the authoritative count (debounced)', async () => {
    vi.useFakeTimers()
    try {
      mockFetchUnread.mockResolvedValue({ count: 7 })
      const { forgeUnreadCount, onForgeEvent } = useForgeUnread()
      onForgeEvent()
      // Not fetched yet — the call is debounced to coalesce bursts.
      expect(mockFetchUnread).not.toHaveBeenCalled()
      await vi.advanceTimersByTimeAsync(250)
      expect(mockFetchUnread).toHaveBeenCalledTimes(1)
      expect(forgeUnreadCount.value).toBe(7)
    } finally {
      vi.useRealTimers()
    }
  })

  it('onForgeEvent coalesces a burst into one fetch', async () => {
    vi.useFakeTimers()
    try {
      mockFetchUnread.mockResolvedValue({ count: 3 })
      const { onForgeEvent } = useForgeUnread()
      onForgeEvent()
      onForgeEvent()
      onForgeEvent()
      await vi.advanceTimersByTimeAsync(250)
      expect(mockFetchUnread).toHaveBeenCalledTimes(1)
    } finally {
      vi.useRealTimers()
    }
  })

  it('a refetch restores the badge after the tab was opened', async () => {
    // The reported bug: opening the tab clears the badge, and nothing brought
    // it back. A live event must re-derive the server count.
    mockFetchUnread.mockResolvedValue({ count: 2 })
    mockMarkRead.mockResolvedValue({ count: 0 })
    const { forgeUnreadCount, refresh, markAllRead, onForgeEvent } = useForgeUnread()
    await refresh()
    expect(forgeUnreadCount.value).toBe(2)

    await markAllRead()
    expect(forgeUnreadCount.value).toBe(0)

    // A new event arrives while the user is on another tab.
    mockFetchUnread.mockResolvedValue({ count: 1 })
    vi.useFakeTimers()
    try {
      onForgeEvent()
      await vi.advanceTimersByTimeAsync(250)
    } finally {
      vi.useRealTimers()
    }
    expect(forgeUnreadCount.value).toBe(1)
  })

  it('markAllRead clears optimistically and persists', async () => {
    mockFetchUnread.mockResolvedValue({ count: 4 })
    mockMarkRead.mockResolvedValue({ count: 0 })
    const { forgeUnreadCount, refresh, markAllRead } = useForgeUnread()
    await refresh()
    expect(forgeUnreadCount.value).toBe(4)

    await markAllRead()
    expect(forgeUnreadCount.value).toBe(0)
    expect(mockMarkRead).toHaveBeenCalledTimes(1)
  })

  it('a failed markAllRead restores the previous count', async () => {
    // The badge must not claim everything was seen when the write failed.
    mockFetchUnread.mockResolvedValue({ count: 2 })
    const { forgeUnreadCount, refresh, markAllRead } = useForgeUnread()
    await refresh()
    expect(forgeUnreadCount.value).toBe(2)

    mockMarkRead.mockRejectedValue(new Error('offline'))
    await expect(markAllRead()).rejects.toThrow()

    expect(forgeUnreadCount.value).toBe(2, 'a failed clear must roll back')
  })

  it('a mark-all already in flight is reused, not re-issued', async () => {
    // The items list and the pipeline list clear the same repo-level state.
    // Normally the second call short-circuits on the "already zero" guard, but
    // if a refresh repopulates the count in between, the in-flight guard is what
    // still prevents a second write.
    mockFetchUnread.mockResolvedValue({ count: 3 })
    const { forgeUnreadCount, refresh, markAllRead } = useForgeUnread()
    await refresh()
    expect(forgeUnreadCount.value).toBe(3)

    let resolveWrite: (v: { count: number }) => void = () => {}
    mockMarkRead.mockReturnValue(new Promise(res => { resolveWrite = res }))

    const first = markAllRead()
    expect(forgeUnreadCount.value).toBe(0, 'the clear is optimistic')

    // Simulate a refresh landing between the two calls and repopulating the
    // count — without the in-flight guard this would fire a second write.
    forgeUnreadCount.value = 7
    const second = markAllRead()

    resolveWrite({ count: 0 })
    await Promise.all([first, second])

    expect(mockMarkRead).toHaveBeenCalledTimes(1,
      'a second caller must reuse the in-flight write')
    expect(forgeUnreadCount.value).toBe(0)
  })

  it('a refresh issued before the clear cannot restore the old count', async () => {
    // The race: a debounced WS refetch is in flight when the user clicks
    // "mark all read". Its response predates the write, so it must not win.
    mockFetchUnread.mockResolvedValue({ count: 5 })
    const { forgeUnreadCount, refresh, markAllRead } = useForgeUnread()
    await refresh()
    expect(forgeUnreadCount.value).toBe(5)

    let resolveStale: (v: { count: number }) => void = () => {}
    mockFetchUnread.mockReturnValue(new Promise(res => { resolveStale = res }))
    const staleRefresh = refresh()          // in flight, still 5 on the server

    mockMarkRead.mockResolvedValue({ count: 0 })
    await markAllRead()
    expect(forgeUnreadCount.value).toBe(0)

    // The stale GET lands after the clear.
    resolveStale({ count: 5 })
    await staleRefresh

    expect(forgeUnreadCount.value).toBe(0,
      'a response that predates the clear must not restore the old count')
  })
})

describe('useForgeUnread markAllRead (mark all in the bound repository)', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    useForgeUnread().forgeUnreadCount.value = 0
  })

  it('marks read with no itemKey, which the server treats as "all"', async () => {
    mockMarkRead.mockResolvedValue({ count: 0 })
    const { forgeUnreadCount, markAllRead } = useForgeUnread()
    forgeUnreadCount.value = 4

    await markAllRead()

    // No argument: the whole repository, not one item.
    expect(mockMarkRead).toHaveBeenCalledWith()
    expect(forgeUnreadCount.value).toBe(0)
  })

  it('settles on the server-reported remainder rather than assuming zero', async () => {
    // Activity can arrive while the request is in flight; the response is the
    // authoritative remainder, so the badge must use it.
    mockMarkRead.mockResolvedValue({ count: 2 })
    const { forgeUnreadCount, markAllRead } = useForgeUnread()
    forgeUnreadCount.value = 4

    await markAllRead()

    expect(forgeUnreadCount.value).toBe(2)
  })

  it('is a no-op when there is nothing unread', async () => {
    // Clearing an already-zero repo is a wasted write.
    mockMarkRead.mockResolvedValue({ count: 0 })
    const { forgeUnreadCount, markAllRead } = useForgeUnread()
    forgeUnreadCount.value = 0
    await markAllRead()
    expect(mockMarkRead).not.toHaveBeenCalled()
  })
})
