import { describe, expect, it, vi, beforeEach } from 'vitest'

const mockFetchNotifications = vi.fn()
const mockClearNotifications = vi.fn()

vi.mock('@/utils/notificationsApi', () => ({
  fetchNotifications: (...a: unknown[]) => mockFetchNotifications(...a),
  clearNotifications: (...a: unknown[]) => mockClearNotifications(...a),
}))

import { useNotifications, ALL_PROJECTS } from '@/composables/useNotifications'

const n = (id: number, over: Record<string, unknown> = {}) => ({
  id,
  kind: 'session',
  eventType: 'completed',
  title: `t${id}`,
  body: '',
  projectId: 1,
  projectPath: '/proj',
  createdAt: `2026-10-15T12:00:0${id}.000Z`,
  ...over,
})

describe('useNotifications', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    // Reset the module singletons between tests.
    const c = useNotifications()
    c.items.value = []
    c.total.value = 0
    c.hasMore.value = false
    c.error.value = ''
    c.filterKind.value = ''
    c.filterRange.value = ''
    c.filterProject.value = ALL_PROJECTS
  })

  it('load populates the list, total and hasMore', async () => {
    mockFetchNotifications.mockResolvedValue({
      items: [n(2), n(1)],
      total: 7,
      hasMore: true,
    })
    const c = useNotifications()
    await c.load()

    expect(c.items.value.map(i => i.id)).toEqual([2, 1])
    expect(c.total.value).toBe(7)
    expect(c.hasMore.value).toBe(true)
    expect(c.error.value).toBe('')
  })

  it('defaults to GLOBAL scope (no project filter sent)', async () => {
    // The notification center spans all projects; the active project is an
    // option, not the implicit default.
    mockFetchNotifications.mockResolvedValue({ items: [], total: 0, hasMore: false })
    const c = useNotifications()
    await c.load()

    expect(c.filterProject.value).toBe(ALL_PROJECTS)
    expect(mockFetchNotifications.mock.calls.at(-1)?.[0].project).toBeUndefined()
  })

  it('setProject narrows the query to that project', async () => {
    mockFetchNotifications.mockResolvedValue({ items: [], total: 0, hasMore: false })
    const c = useNotifications()
    await c.setProject('/proj2')

    expect(c.filterProject.value).toBe('/proj2')
    expect(mockFetchNotifications.mock.calls.at(-1)?.[0].project).toBe('/proj2')
  })

  it('a failed load surfaces the error and clears the list', async () => {
    mockFetchNotifications.mockRejectedValue(new Error('boom'))
    const c = useNotifications()
    await c.load()

    expect(c.error.value).toBe('boom')
    expect(c.items.value).toEqual([])
  })

  it('setKind passes the kind through to the API and reloads', async () => {
    mockFetchNotifications.mockResolvedValue({ items: [], total: 0, hasMore: false })
    const c = useNotifications()
    await c.setKind('task')

    expect(c.filterKind.value).toBe('task')
    expect(mockFetchNotifications.mock.calls.at(-1)?.[0].kind).toBe('task')
  })

  it('setRange passes the range through to the API', async () => {
    mockFetchNotifications.mockResolvedValue({ items: [], total: 0, hasMore: false })
    const c = useNotifications()
    await c.setRange('7d')

    expect(c.filterRange.value).toBe('7d')
    expect(mockFetchNotifications.mock.calls.at(-1)?.[0].range).toBe('7d')
  })

  it('loadMore appends the next page using the last row as a cursor', async () => {
    mockFetchNotifications.mockResolvedValueOnce({
      items: [n(3), n(2)],
      total: 3,
      hasMore: true,
    })
    const c = useNotifications()
    await c.load()

    mockFetchNotifications.mockResolvedValueOnce({
      items: [n(1)],
      total: 3,
      hasMore: false,
    })
    await c.loadMore()

    expect(c.items.value.map(i => i.id)).toEqual([3, 2, 1])
    expect(c.hasMore.value).toBe(false)

    // The cursor is the last row of the FIRST page (id 2, its createdAt).
    const cursorCall = mockFetchNotifications.mock.calls.at(-1)?.[0]
    expect(cursorCall.beforeId).toBe(2)
    expect(cursorCall.beforeAt).toBe('2026-10-15T12:00:02.000Z')
  })

  it('loadMore is a no-op when there is nothing more', async () => {
    mockFetchNotifications.mockResolvedValue({ items: [n(1)], total: 1, hasMore: false })
    const c = useNotifications()
    await c.load()
    const callsAfterLoad = mockFetchNotifications.mock.calls.length

    await c.loadMore()
    expect(mockFetchNotifications.mock.calls.length).toBe(callsAfterLoad)
  })

  it('a filter change invalidates an in-flight loadMore', async () => {
    // The seq guard: a slow loadMore must not append stale rows after the user
    // changed the filter and reloaded.
    mockFetchNotifications.mockResolvedValueOnce({ items: [n(2)], total: 2, hasMore: true })
    const c = useNotifications()
    await c.load()

    let resolveSlow: (v: unknown) => void = () => {}
    mockFetchNotifications.mockReturnValueOnce(new Promise((res) => { resolveSlow = res }))
    const slow = c.loadMore()

    // A reload happens while loadMore is in flight.
    mockFetchNotifications.mockResolvedValueOnce({ items: [n(9)], total: 1, hasMore: false })
    await c.load()

    // Now let the slow loadMore resolve; its rows must be dropped.
    resolveSlow({ items: [n(1)], total: 2, hasMore: true })
    await slow

    expect(c.items.value.map(i => i.id)).toEqual([9])
  })

  it('clear deletes then reloads', async () => {
    mockClearNotifications.mockResolvedValue({ deleted: 5 })
    mockFetchNotifications.mockResolvedValue({ items: [], total: 0, hasMore: false })
    const c = useNotifications()

    await c.clear()

    expect(mockClearNotifications).toHaveBeenCalled()
    expect(mockFetchNotifications).toHaveBeenCalled()
  })

  it('clear rejects when the delete fails', async () => {
    mockClearNotifications.mockRejectedValue(new Error('nope'))
    const c = useNotifications()
    await expect(c.clear()).rejects.toThrow('nope')
  })
})
