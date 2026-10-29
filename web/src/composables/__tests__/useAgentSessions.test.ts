import { describe, it, expect, vi, beforeEach } from 'vitest'

/**
 * Paged loader for "conversations using this agent".
 *
 * The behaviour that matters is what the page shows when something goes wrong:
 * the first page failing must surface an error, but a FAILED APPEND must keep
 * the rows already on screen (and keep the cursor) so the user does not lose a
 * list they had scrolled through. A stale page that lands after the agent was
 * switched must be dropped rather than appended to the new list.
 */

const { mockApiGet, mockAppLogE } = vi.hoisted(() => ({
  mockApiGet: vi.fn(),
  mockAppLogE: vi.fn(),
}))

vi.mock('@/utils/api', () => ({
  apiGet: (...args: unknown[]) => mockApiGet(...args),
}))
vi.mock('@/utils/appLog', () => ({
  appLog: { e: (...args: unknown[]) => mockAppLogE(...args), d: vi.fn(), i: vi.fn(), w: vi.fn() },
}))

import { useAgentSessions } from '../useAgentSessions'

function entry(id: string) {
  return {
    entryId: id,
    sessionId: id,
    title: id,
    projectPath: '/p',
    sessionType: 'chat',
    role: 'chat' as const,
    memberName: '',
    archived: false,
    updatedAt: '2026-01-01 10:00:00',
  }
}

function page(ids: string[], hasMore: boolean, cursor = '', cursorId = '', total = 99) {
  return {
    sessions: ids.map(entry),
    hasMore,
    nextCursor: cursor,
    nextCursorId: cursorId,
    total,
  }
}

beforeEach(() => {
  mockApiGet.mockReset()
  mockAppLogE.mockReset()
})

describe('useAgentSessions', () => {
  it('loads the first page and takes `total` from the server', async () => {
    mockApiGet.mockResolvedValue(page(['a', 'b'], false))
    const s = useAgentSessions()

    await s.load('codebuddy')

    expect(s.sessions.value.map((x) => x.entryId)).toEqual(['a', 'b'])
    // The header count is the server's number, not the loaded row count: the
    // list is paged, so deriving it locally would print 2 for 99 conversations.
    expect(s.total.value).toBe(99)
    expect(s.hasMore.value).toBe(false)
    expect(s.loading.value).toBe(false)
  })

  it('requests the first page with the page size and no cursor', async () => {
    mockApiGet.mockResolvedValue(page([], false))
    const s = useAgentSessions()

    await s.load('codebuddy')

    expect(mockApiGet).toHaveBeenCalledTimes(1)
    const url = mockApiGet.mock.calls[0][0] as string
    expect(url).toContain('/api/agents/codebuddy/sessions?')
    expect(url).toContain('limit=50')
    expect(url).not.toContain('cursor=')
  })

  it('appends the next page using the cursor returned verbatim', async () => {
    mockApiGet.mockResolvedValueOnce(page(['a'], true, '2026-01-01 10:00:00', 'a'))
    const s = useAgentSessions()
    await s.load('codebuddy')

    mockApiGet.mockResolvedValueOnce(page(['b'], false))
    await s.loadMore()

    const url = mockApiGet.mock.calls[1][0] as string
    // The cursor is a timestamp containing a space. URLSearchParams serialises
    // it as `+`, which Go's query decoder turns back into a space — asserting
    // on the raw encoding would pin `+` vs `%20`, which does not matter; the
    // decoded value does.
    const q = new URLSearchParams(url.split('?')[1])
    expect(q.get('cursor')).toBe('2026-01-01 10:00:00')
    expect(q.get('cursor_id')).toBe('a')
    expect(s.sessions.value.map((x) => x.entryId)).toEqual(['a', 'b'])
  })

  it('does not request another page when the list is exhausted', async () => {
    mockApiGet.mockResolvedValue(page(['a'], false))
    const s = useAgentSessions()
    await s.load('codebuddy')

    await s.loadMore()

    expect(mockApiGet).toHaveBeenCalledTimes(1)
  })

  it('surfaces a first-page failure as `error` and stops claiming more', async () => {
    mockApiGet.mockRejectedValue(new Error('boom'))
    const s = useAgentSessions()

    await s.load('codebuddy')

    expect(s.error.value).toContain('boom')
    expect(s.hasMore.value).toBe(false)
    expect(s.loading.value).toBe(false)
  })

  it('keeps the loaded rows and the cursor when an APPEND fails', async () => {
    mockApiGet.mockResolvedValueOnce(page(['a'], true, 'c1', 'a'))
    const s = useAgentSessions()
    await s.load('codebuddy')

    mockApiGet.mockRejectedValueOnce(new Error('blip'))
    await s.loadMore()

    // The rows survive: replacing a scrolled list with "failed to load" on a
    // transient blip would throw away everything the user was reading.
    expect(s.sessions.value.map((x) => x.entryId)).toEqual(['a'])
    expect(s.error.value).toBe('')
    expect(s.loadMoreError.value).toContain('blip')
    // hasMore must stay true, otherwise retry can never fire again.
    expect(s.hasMore.value).toBe(true)
    expect(s.loadingMore.value).toBe(false)
  })

  it('retryLoadMore retries the SAME page and clears the footer error', async () => {
    mockApiGet.mockResolvedValueOnce(page(['a'], true, 'c1', 'a'))
    const s = useAgentSessions()
    await s.load('codebuddy')

    mockApiGet.mockRejectedValueOnce(new Error('blip'))
    await s.loadMore()
    expect(s.loadMoreError.value).toContain('blip')

    mockApiGet.mockResolvedValueOnce(page(['b'], false))
    await s.retryLoadMore()

    expect(s.loadMoreError.value).toBe('')
    expect(s.sessions.value.map((x) => x.entryId)).toEqual(['a', 'b'])
    const retryURL = mockApiGet.mock.calls[2][0] as string
    expect(retryURL).toContain('cursor=c1')
  })

  it('drops a stale page that lands after the agent was switched', async () => {
    // The first request for agent A stays in flight while the user opens B.
    let resolveA: (v: unknown) => void = () => {}
    mockApiGet.mockImplementationOnce(
      () => new Promise((res) => { resolveA = res }),
    )
    const s = useAgentSessions()
    const loadA = s.load('agentA')

    // Distinct totals so "A's page overwrote B's" is detectable at all: two
    // equal values would make the assertion pass either way.
    mockApiGet.mockResolvedValueOnce(page(['b1'], false, '', '', 7))
    await s.load('agentB')

    resolveA(page(['a1'], false, '', '', 42))
    await loadA

    // A's rows must not be appended to B's list, and A's `total` must not
    // overwrite B's — the generation guard exists for exactly this.
    expect(s.sessions.value.map((x) => x.entryId)).toEqual(['b1'])
    expect(s.total.value).toBe(7)
  })

  it('reset clears rows, cursor and both error slots', async () => {
    mockApiGet.mockRejectedValueOnce(new Error('boom'))
    const s = useAgentSessions()
    await s.load('codebuddy')
    expect(s.error.value).toContain('boom')

    mockApiGet.mockResolvedValueOnce(page(['a'], false))
    await s.load('other')

    expect(s.error.value).toBe('')
    expect(s.loadMoreError.value).toBe('')
    expect(s.sessions.value.map((x) => x.entryId)).toEqual(['a'])
  })
})
