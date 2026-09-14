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
    const { forgeUnreadCount, refresh, markRead, onForgeEvent } = useForgeUnread()
    await refresh()
    expect(forgeUnreadCount.value).toBe(2)

    await markRead()
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

  it('markRead clears optimistically and persists', async () => {
    mockFetchUnread.mockResolvedValue({ count: 4 })
    mockMarkRead.mockResolvedValue({ count: 0 })
    const { forgeUnreadCount, refresh, markRead } = useForgeUnread()
    await refresh()
    expect(forgeUnreadCount.value).toBe(4)

    await markRead()
    expect(forgeUnreadCount.value).toBe(0)
    expect(mockMarkRead).toHaveBeenCalledTimes(1)
  })

  it('markRead is a no-op when already zero', async () => {
    const { markRead } = useForgeUnread()
    await markRead()
    expect(mockMarkRead).not.toHaveBeenCalled()
  })

  it('a failed markRead re-syncs from the server', async () => {
    mockFetchUnread.mockResolvedValue({ count: 2 })
    mockMarkRead.mockRejectedValue(new Error('fail'))
    const { forgeUnreadCount, refresh, markRead } = useForgeUnread()
    await refresh()
    mockFetchUnread.mockResolvedValue({ count: 2 })
    await markRead()
    // After the failed clear, the refresh restores the authoritative count.
    expect(forgeUnreadCount.value).toBe(2)
  })
})

describe('useForgeUnread markRead (mark all in the bound repository)', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    useForgeUnread().forgeUnreadCount.value = 0
  })

  it('marks read with no itemKey, which the server treats as "all"', async () => {
    mockMarkRead.mockResolvedValue({ count: 0 })
    const { forgeUnreadCount, markRead } = useForgeUnread()
    forgeUnreadCount.value = 4

    await markRead()

    // No argument: the whole repository, not one item.
    expect(mockMarkRead).toHaveBeenCalledWith()
    expect(forgeUnreadCount.value).toBe(0)
  })

  it('settles on the server-reported remainder rather than assuming zero', async () => {
    // Activity can arrive while the request is in flight; the response is the
    // authoritative remainder, so the badge must use it.
    mockMarkRead.mockResolvedValue({ count: 2 })
    const { forgeUnreadCount, markRead } = useForgeUnread()
    forgeUnreadCount.value = 4

    await markRead()

    expect(forgeUnreadCount.value).toBe(2)
  })

  it('does nothing when there is nothing unread', async () => {
    const { forgeUnreadCount, markRead } = useForgeUnread()
    forgeUnreadCount.value = 0
    await markRead()
    expect(mockMarkRead).not.toHaveBeenCalled()
  })
})
