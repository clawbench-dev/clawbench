import { describe, expect, it, vi, beforeEach } from 'vitest'

// The composable resolves through the shared forge API client; mock it so these
// tests assert request shaping and state handling rather than HTTP.
const mockFetchForgeUnreadItems = vi.fn()
const mockMarkForgeRead = vi.fn()

vi.mock('@/utils/forgeApi', async () => {
  const actual = await vi.importActual<typeof import('@/utils/forgeApi')>('@/utils/forgeApi')
  return {
    ...actual,
    fetchForgeUnreadItems: (...a: unknown[]) => mockFetchForgeUnreadItems(...a),
    markForgeRead: (...a: unknown[]) => mockMarkForgeRead(...a),
  }
})

// Clearing re-derives the dock badge from the same server response; the badge
// itself is covered by useForgeUnread.test.ts, so forward the write here.
vi.mock('@/composables/useForgeUnread', () => ({
  useForgeUnread: () => ({
    forgeUnreadCount: { value: 0 },
    refresh: vi.fn(),
    onForgeEvent: vi.fn(),
    markAllRead: () => mockMarkForgeRead(),
  }),
}))

vi.mock('@/utils/appLog', () => ({
  appLog: { d: vi.fn(), i: vi.fn(), w: vi.fn(), e: vi.fn() },
}))

import { useForgeUnreadItems } from '@/composables/useForge'
import { ForgeApiError } from '@/utils/forgeApi'

function row(itemKey: string, overrides: Record<string, unknown> = {}) {
  return {
    itemKey,
    type: 'pr',
    number: 1,
    runId: 0,
    eventType: 'commented',
    eventCount: 1,
    url: 'https://example.com/x',
    slug: 'acme/widgets',
    updatedAt: '2026-09-14T12:00:00Z',
    ...overrides,
  }
}

describe('useForgeUnreadItems', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('loads the rows', async () => {
    mockFetchForgeUnreadItems.mockResolvedValue({
      count: 2,
      items: [row('pr/455'), row('pipeline/run:555', { type: 'pipeline', number: 0, runId: 555 })],
    })
    const u = useForgeUnreadItems(() => '/proj')

    expect(u.loaded.value).toBe(false)
    await u.load()

    expect(u.loading.value).toBe(false)
    expect(u.loaded.value).toBe(true)
    expect(u.error.value).toBeNull()
    expect(u.items.value).toHaveLength(2)
    expect(u.items.value[1].runId).toBe(555)
  })

  it('does not request anything without a project', async () => {
    const u = useForgeUnreadItems(() => '')
    await u.load()

    expect(mockFetchForgeUnreadItems).not.toHaveBeenCalled()
    expect(u.items.value).toEqual([])
  })

  it('surfaces an error and does NOT claim the list is empty', async () => {
    // A failed load rendering as "nothing unread" would be a lie the user acts on.
    mockFetchForgeUnreadItems.mockRejectedValue(new ForgeApiError('boom', 'ForgeNetworkError'))
    const u = useForgeUnreadItems(() => '/proj')

    await u.load()

    expect(u.error.value).toEqual({ message: 'boom', code: 'ForgeNetworkError' })
    expect(u.items.value).toEqual([])
    expect(u.loaded.value).toBe(false)
  })

  it('markAllRead clears the list through the shared badge write', async () => {
    mockFetchForgeUnreadItems.mockResolvedValue({ count: 1, items: [row('pr/1')] })
    mockMarkForgeRead.mockResolvedValue({ count: 0 })
    const u = useForgeUnreadItems(() => '/proj')
    await u.load()
    expect(u.items.value).toHaveLength(1)

    await u.markAllRead()

    expect(mockMarkForgeRead).toHaveBeenCalledTimes(1)
    expect(u.items.value).toEqual([])
  })

  it('markRowRead flags the row without removing it', async () => {
    // Removing it would shift every row below the cursor as the user clicks.
    mockFetchForgeUnreadItems.mockResolvedValue({
      count: 2,
      items: [row('pr/1'), row('pr/2')],
    })
    const u = useForgeUnreadItems(() => '/proj')
    await u.load()

    await u.markRowRead('pr/1')

    expect(u.items.value).toHaveLength(2)
    expect(u.items.value[0].read).toBe(true)
    expect(u.items.value[1].read).toBeUndefined()
  })

  it('markRowRead persists the read server-side with the key verbatim', async () => {
    // A local-only flag would leave the badge wrong and the item unread on
    // reload. The key must be passed through untouched: a pipeline's number is
    // 0, so a rebuilt key would be "pipeline/0" and mark nothing.
    mockFetchForgeUnreadItems.mockResolvedValue({
      count: 1,
      items: [row('pipeline/run:555', { type: 'pipeline', number: 0, runId: 555 })],
    })
    mockMarkForgeRead.mockResolvedValue({ count: 0 })
    const u = useForgeUnreadItems(() => '/proj')
    await u.load()

    await u.markRowRead('pipeline/run:555')

    expect(mockMarkForgeRead).toHaveBeenCalledWith('pipeline/run:555')
  })

  it('markRowRead restores the row when the write fails', async () => {
    mockFetchForgeUnreadItems.mockResolvedValue({ count: 1, items: [row('pr/1')] })
    mockMarkForgeRead.mockRejectedValue(new Error('offline'))
    const u = useForgeUnreadItems(() => '/proj')
    await u.load()

    await u.markRowRead('pr/1')

    expect(u.items.value[0].read).toBe(false,
      'a failed mark must not leave the row claiming it was seen')
  })

  it('ignores an unknown item key', async () => {
    mockFetchForgeUnreadItems.mockResolvedValue({ count: 1, items: [row('pr/1')] })
    const u = useForgeUnreadItems(() => '/proj')
    await u.load()

    await u.markRowRead('pr/999')

    expect(u.items.value).toHaveLength(1)
    expect(u.items.value[0].read).toBeUndefined()
  })
})
