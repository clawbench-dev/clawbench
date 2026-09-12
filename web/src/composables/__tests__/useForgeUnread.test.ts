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

  it('bump increments locally for a live event', () => {
    const { forgeUnreadCount, bump } = useForgeUnread()
    bump()
    bump()
    expect(forgeUnreadCount.value).toBe(2)
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
