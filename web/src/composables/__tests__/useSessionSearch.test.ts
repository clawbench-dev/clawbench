import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest'

// Mock fetch globally
const mockFetch = vi.fn()
vi.stubGlobal('fetch', mockFetch)

// Mock appLog
vi.mock('@/utils/appLog', () => ({
  appLog: { d: vi.fn(), i: vi.fn(), w: vi.fn(), e: vi.fn() },
}))

// Mock onUnmounted (no real Vue lifecycle in tests)
vi.mock('vue', () => ({
  reactive: (obj: Record<string, unknown>) => obj,
  onUnmounted: vi.fn(),
}))

import { useSessionSearch, fetchSessionFirstMessage, resolveTimeRange } from '@/composables/useSessionSearch'

describe('useSessionSearch', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    mockFetch.mockReset()
    vi.useFakeTimers()
  })

  afterEach(() => {
    vi.useRealTimers()
  })

  describe('initial state', () => {
    it('has correct default values', () => {
      const { state } = useSessionSearch()
      expect(state.query).toBe('')
      expect(state.results).toEqual([])
      expect(state.total).toBe(0)
      expect(state.loading).toBe(false)
      expect(state.error).toBeNull()
      expect(state.searchMode).toBe('')
      expect(state.preferMode).toBe('hybrid')
      expect(state.archivedFilter).toBe('all')
      expect(state.sortOrder).toBe('relevance')
      expect(state.hasMore).toBe(false)
      expect(state.loadingMore).toBe(false)
      expect(state.timeRange).toBe('all')
      expect(state.customFrom).toBe('')
      expect(state.customTo).toBe('')
    })
  })

  describe('resolveTimeRange', () => {
    function localDate(offsetDays: number): string {
      const d = new Date()
      d.setDate(d.getDate() + offsetDays)
      const m = String(d.getMonth() + 1).padStart(2, '0')
      const day = String(d.getDate()).padStart(2, '0')
      return `${d.getFullYear()}-${m}-${day}`
    }

    it('returns empty bounds for the "all" preset', () => {
      expect(resolveTimeRange('all', '2024-01-01', '2024-02-01')).toEqual({ from: '', to: '' })
    })

    it('spans today only for the "today" preset', () => {
      const today = localDate(0)
      expect(resolveTimeRange('today', '', '')).toEqual({ from: today, to: today })
    })

    it('spans today plus the previous six days for "7d"', () => {
      expect(resolveTimeRange('7d', '', '')).toEqual({ from: localDate(-6), to: localDate(0) })
    })

    it('spans today plus the previous 29 days for "30d"', () => {
      expect(resolveTimeRange('30d', '', '')).toEqual({ from: localDate(-29), to: localDate(0) })
    })

    it('passes custom bounds through and trims them', () => {
      expect(resolveTimeRange('custom', ' 2024-01-01 ', '2024-02-01 ')).toEqual({
        from: '2024-01-01',
        to: '2024-02-01',
      })
    })

    it('allows a custom range with only one side set', () => {
      expect(resolveTimeRange('custom', '2024-01-01', '')).toEqual({ from: '2024-01-01', to: '' })
      expect(resolveTimeRange('custom', '', '2024-02-01')).toEqual({ from: '', to: '2024-02-01' })
    })

    it('swaps an inverted custom range instead of sending it as-is', () => {
      // from > to would match nothing on the backend and show an unexplained
      // empty list, so the bounds are reordered.
      expect(resolveTimeRange('custom', '2024-03-01', '2024-01-01')).toEqual({
        from: '2024-01-01',
        to: '2024-03-01',
      })
    })
  })

  describe('search', () => {
    it('performs a search and updates state', async () => {
      const mockResult = {
        session_id: 's1',
        session_title: 'Test Session',
        score: 0.95,
        backend: 'claude',
        project_path: '/home/user/project',
        archived: false,
        created_at: '2025-01-01T00:00:00Z',
        match_count: 2,
        chunks: [],
      }
      mockFetch.mockResolvedValue({
        ok: true,
        json: () => Promise.resolve({
          sessions: [mockResult],
          total: 1,
          mode: 'hybrid',
        }),
      })

      const { state, search } = useSessionSearch()
      await search('test query')

      expect(mockFetch).toHaveBeenCalledWith('/api/rag/session-search', expect.objectContaining({
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ q: 'test query', prefer_mode: 'hybrid', archived: 'all', sort: 'relevance' }),
      }))
      expect(state.results).toHaveLength(1)
      expect(state.results[0]).toEqual(mockResult)
      expect(state.total).toBe(1)
      expect(state.searchMode).toBe('hybrid')
      expect(state.loading).toBe(false)
      expect(state.error).toBeNull()
    })

    it('trims whitespace from query', async () => {
      mockFetch.mockResolvedValue({
        ok: true,
        json: () => Promise.resolve({ sessions: [], total: 0, mode: '' }),
      })

      const { search } = useSessionSearch()
      await search('  test  ')

      expect(mockFetch).toHaveBeenCalledWith('/api/rag/session-search', expect.objectContaining({
        body: JSON.stringify({ q: 'test', prefer_mode: 'hybrid', archived: 'all', sort: 'relevance' }),
      }))
    })

    it('browses all sessions for empty query', async () => {
      mockFetch.mockResolvedValue({
        ok: true,
        json: () => Promise.resolve({ sessions: [], total: 0, mode: 'recent' }),
      })

      const { state, search } = useSessionSearch()
      state.results = [{ session_id: 's1' }] as any
      state.total = 1
      state.loading = true

      await search('')

      expect(state.results).toEqual([])
      expect(state.total).toBe(0)
      expect(state.loading).toBe(false)
      expect(state.error).toBeNull()
      expect(state.searchMode).toBe('recent')
      // Empty query fetches the recent-session list instead of a stale result.
      expect(mockFetch).toHaveBeenCalledWith('/api/rag/session-search', expect.objectContaining({
        method: 'POST',
        body: JSON.stringify({ q: '', prefer_mode: 'hybrid', archived: 'all', sort: 'relevance' }),
      }))
    })

    it('parses has_more from a browse response', async () => {
      mockFetch.mockResolvedValue({
        ok: true,
        json: () => Promise.resolve({ sessions: [{ session_id: 's1' }], total: 1, mode: 'recent', has_more: true }),
      })

      const { state, browse } = useSessionSearch()
      await browse()
      expect(state.hasMore).toBe(true)
      expect(state.searchMode).toBe('recent')
    })

    it('handles HTTP errors', async () => {
      mockFetch.mockResolvedValue({
        ok: false,
        status: 500,
        text: () => Promise.resolve('Internal Server Error'),
      })

      const { state, search } = useSessionSearch()
      await search('test')

      expect(state.error).toBe('Internal Server Error')
      expect(state.loading).toBe(false)
    })

    it('uses status text when body is empty', async () => {
      mockFetch.mockResolvedValue({
        ok: false,
        status: 503,
        text: () => Promise.resolve(''),
      })

      const { state, search } = useSessionSearch()
      await search('test')

      expect(state.error).toBe('Search failed: 503')
    })

    it('handles network errors', async () => {
      mockFetch.mockRejectedValue(new Error('Network error'))

      const { state, search } = useSessionSearch()
      await search('test')

      expect(state.error).toBe('Network error')
      expect(state.loading).toBe(false)
    })

    it('ignores AbortError', async () => {
      const abortError = new DOMException('Aborted', 'AbortError')
      mockFetch.mockRejectedValue(abortError)

      const { state, search } = useSessionSearch()
      await search('test')

      // State should remain loading since we returned early without updating
      expect(state.loading).toBe(true)
      expect(state.error).toBeNull()
    })

    it('cancels previous search when new search starts', async () => {
      let firstController: AbortController | null = null
      let secondController: AbortController | null = null

      mockFetch.mockImplementation((_url: string, options: Record<string, unknown>) => {
        const signal = options.signal as AbortSignal
        if (!firstController) {
          firstController = new AbortController()
          // First call - return a promise that will be aborted
          return new Promise((_resolve, reject) => {
            signal.addEventListener('abort', () => reject(new DOMException('Aborted', 'AbortError')))
          })
        }
        secondController = new AbortController()
        return Promise.resolve({
          ok: true,
          json: () => Promise.resolve({ sessions: [], total: 0, mode: '' }),
        })
      })

      const { state, search } = useSessionSearch()
      const firstSearch = search('first')
      // Start second search before first completes
      const secondSearch = search('second')
      await Promise.all([firstSearch.catch(() => {}), secondSearch])

      // Only the second search should have results
      expect(state.loading).toBe(false)
    })
    it('sends prefer_mode=fts when set', async () => {
      mockFetch.mockResolvedValue({
        ok: true,
        json: () => Promise.resolve({ sessions: [], total: 0, mode: 'fts' }),
      })

      const { state, search } = useSessionSearch()
      state.preferMode = 'fts'
      await search('test')

      expect(mockFetch).toHaveBeenCalledWith('/api/rag/session-search', expect.objectContaining({
        body: JSON.stringify({ q: 'test', prefer_mode: 'fts', archived: 'all', sort: 'relevance' }),
      }))
    })
  })

  describe('setQuery (debounce)', () => {
    it('sets query immediately', () => {
      const { state, setQuery } = useSessionSearch()
      setQuery('hello')

      expect(state.query).toBe('hello')
    })

    it('debounces search calls', () => {
      const { setQuery } = useSessionSearch()

      setQuery('a')
      setQuery('ab')
      setQuery('abc')

      // No fetch yet (debounce)
      expect(mockFetch).not.toHaveBeenCalled()

      // Advance past debounce
      vi.advanceTimersByTime(300)

      // Only one search should fire, with the latest query
      expect(mockFetch).toHaveBeenCalledTimes(1)
      expect(mockFetch).toHaveBeenCalledWith('/api/rag/session-search', expect.objectContaining({
        body: JSON.stringify({ q: 'abc', prefer_mode: 'hybrid', archived: 'all', sort: 'relevance' }),
      }))
    })

    it('browses all sessions for empty query without debouncing', () => {
      mockFetch.mockResolvedValue({
        ok: true,
        json: () => Promise.resolve({ sessions: [], total: 0, mode: 'recent' }),
      })

      const { state, setQuery } = useSessionSearch()
      state.results = [{ session_id: 's1' }] as any
      state.total = 1

      setQuery('')

      expect(state.query).toBe('')
      // Browse fires immediately (no debounce) since the list is preloaded.
      expect(mockFetch).toHaveBeenCalledTimes(1)
      expect(mockFetch).toHaveBeenCalledWith('/api/rag/session-search', expect.objectContaining({
        body: JSON.stringify({ q: '', prefer_mode: 'hybrid', archived: 'all', sort: 'relevance' }),
      }))
    })

    it('browses all sessions for whitespace-only query', () => {
      mockFetch.mockResolvedValue({
        ok: true,
        json: () => Promise.resolve({ sessions: [], total: 0, mode: 'recent' }),
      })

      const { setQuery } = useSessionSearch()
      setQuery('   ')

      expect(mockFetch).toHaveBeenCalledTimes(1)
      expect(mockFetch).toHaveBeenCalledWith('/api/rag/session-search', expect.objectContaining({
        body: JSON.stringify({ q: '', prefer_mode: 'hybrid', archived: 'all', sort: 'relevance' }),
      }))
    })
  })

  describe('setFilters', () => {
    it('sends archive filter and sort order and re-runs the query', async () => {
      mockFetch.mockResolvedValue({
        ok: true,
        json: () => Promise.resolve({ sessions: [], total: 0, mode: 'fts' }),
      })

      const { state, setFilters } = useSessionSearch()
      state.query = 'test'
      await setFilters({ archived: 'archived', sort: 'oldest' })

      expect(state.archivedFilter).toBe('archived')
      expect(state.sortOrder).toBe('oldest')
      expect(mockFetch).toHaveBeenCalledWith('/api/rag/session-search', expect.objectContaining({
        body: JSON.stringify({ q: 'test', prefer_mode: 'hybrid', archived: 'archived', sort: 'oldest' }),
      }))
    })

    it('browses when the query is empty', async () => {
      mockFetch.mockResolvedValue({
        ok: true,
        json: () => Promise.resolve({ sessions: [], total: 0, mode: 'recent' }),
      })

      const { state, setFilters } = useSessionSearch()
      await setFilters({ sort: 'newest' })

      expect(state.sortOrder).toBe('newest')
      expect(mockFetch).toHaveBeenCalledWith('/api/rag/session-search', expect.objectContaining({
        body: JSON.stringify({ q: '', prefer_mode: 'hybrid', archived: 'all', sort: 'newest' }),
      }))
    })

    it('only changes the provided field', async () => {
      mockFetch.mockResolvedValue({
        ok: true,
        json: () => Promise.resolve({ sessions: [], total: 0, mode: 'recent' }),
      })

      const { state, setFilters } = useSessionSearch()
      await setFilters({ archived: 'active' })

      expect(state.archivedFilter).toBe('active')
      expect(state.sortOrder).toBe('relevance')
    })

    it('sends from/to when a time preset is selected', async () => {
      mockFetch.mockResolvedValue({
        ok: true,
        json: () => Promise.resolve({ sessions: [], total: 0, mode: 'recent' }),
      })

      const { state, setFilters } = useSessionSearch()
      await setFilters({ timeRange: '7d' })

      expect(state.timeRange).toBe('7d')
      const body = JSON.parse(mockFetch.mock.calls[0][1].body)
      expect(body.from).toMatch(/^\d{4}-\d{2}-\d{2}$/)
      expect(body.to).toMatch(/^\d{4}-\d{2}-\d{2}$/)
      // The preset window is inclusive of today and starts six days back.
      const from = new Date(`${body.from}T00:00:00`)
      const to = new Date(`${body.to}T00:00:00`)
      const days = Math.round((to.getTime() - from.getTime()) / 86400000)
      expect(days).toBe(6)
    })

    it('sends custom from/to bounds', async () => {
      mockFetch.mockResolvedValue({
        ok: true,
        json: () => Promise.resolve({ sessions: [], total: 0, mode: 'recent' }),
      })

      const { state, setFilters } = useSessionSearch()
      state.query = 'hello'
      await setFilters({ timeRange: 'custom', customFrom: '2024-01-01', customTo: '2024-02-01' })

      expect(mockFetch).toHaveBeenCalledWith('/api/rag/session-search', expect.objectContaining({
        body: JSON.stringify({
          q: 'hello',
          prefer_mode: 'hybrid',
          archived: 'all',
          sort: 'relevance',
          from: '2024-01-01',
          to: '2024-02-01',
        }),
      }))
    })

    it('omits from/to for the default "all" range', async () => {
      mockFetch.mockResolvedValue({
        ok: true,
        json: () => Promise.resolve({ sessions: [], total: 0, mode: 'recent' }),
      })

      const { setFilters } = useSessionSearch()
      await setFilters({ archived: 'active' })

      const body = JSON.parse(mockFetch.mock.calls[0][1].body)
      expect(body).not.toHaveProperty('from')
      expect(body).not.toHaveProperty('to')
    })

    it('keeps the time range when other filters change', async () => {
      mockFetch.mockResolvedValue({
        ok: true,
        json: () => Promise.resolve({ sessions: [], total: 0, mode: 'recent' }),
      })

      const { state, setFilters } = useSessionSearch()
      state.timeRange = 'custom'
      state.customFrom = '2024-01-01'
      state.customTo = '2024-02-01'
      await setFilters({ archived: 'archived' })

      expect(state.timeRange).toBe('custom')
      const body = JSON.parse(mockFetch.mock.calls[0][1].body)
      expect(body.from).toBe('2024-01-01')
      expect(body.to).toBe('2024-02-01')
      expect(body.archived).toBe('archived')
    })
  })

  describe('loadMore', () => {
    function recentSession(id: string, createdAt: string) {
      return { session_id: id, session_title: id, score: 0, backend: 'cli', project_path: '/p', archived: false, created_at: createdAt, match_count: 0, chunks: [] }
    }

    it('appends the next page using the last row as cursor', async () => {
      mockFetch
        .mockResolvedValueOnce({ ok: true, json: () => Promise.resolve({ sessions: [recentSession('s1', '2024-03-01 10:00:00')], total: 1, mode: 'recent', has_more: true }) })
        .mockResolvedValueOnce({ ok: true, json: () => Promise.resolve({ sessions: [recentSession('s2', '2024-02-01 10:00:00')], total: 1, mode: 'recent', has_more: false }) })

      const { state, browse, loadMore } = useSessionSearch()
      await browse()
      expect(state.hasMore).toBe(true)

      await loadMore()
      expect(state.results.map(r => r.session_id)).toEqual(['s1', 's2'])
      expect(state.hasMore).toBe(false)
      // Second request carries the last row's created_at + id as cursor.
      expect(mockFetch).toHaveBeenLastCalledWith('/api/rag/session-search', expect.objectContaining({
        body: JSON.stringify({ q: '', prefer_mode: 'hybrid', archived: 'all', sort: 'relevance', cursor: '2024-03-01 10:00:00', cursor_id: 's1' }),
      }))
    })

    it('deduplicates rows already present', async () => {
      mockFetch
        .mockResolvedValueOnce({ ok: true, json: () => Promise.resolve({ sessions: [recentSession('s1', '2024-03-01 10:00:00')], total: 1, mode: 'recent', has_more: true }) })
        .mockResolvedValueOnce({ ok: true, json: () => Promise.resolve({ sessions: [recentSession('s1', '2024-03-01 10:00:00')], total: 1, mode: 'recent', has_more: false }) })

      const { state, browse, loadMore } = useSessionSearch()
      await browse()
      await loadMore()
      expect(state.results.map(r => r.session_id)).toEqual(['s1'])
    })

    it('does nothing when hasMore is false', async () => {
      mockFetch.mockResolvedValue({ ok: true, json: () => Promise.resolve({ sessions: [recentSession('s1', '2024-03-01 10:00:00')], total: 1, mode: 'recent', has_more: false }) })

      const { browse, loadMore } = useSessionSearch()
      await browse()
      mockFetch.mockClear()
      await loadMore()
      expect(mockFetch).not.toHaveBeenCalled()
    })

    it('does nothing in search mode', async () => {
      mockFetch.mockResolvedValue({ ok: true, json: () => Promise.resolve({ sessions: [recentSession('s1', '2024-03-01 10:00:00')], total: 1, mode: 'hybrid', has_more: true }) })

      const { state, search, loadMore } = useSessionSearch()
      await search('test')
      expect(state.hasMore).toBe(true)
      mockFetch.mockClear()
      await loadMore()
      expect(mockFetch).not.toHaveBeenCalled()
    })
  })

  describe('fetchSessionFirstMessage', () => {
    it('maps the API response into a chunk hit', async () => {
      mockFetch.mockResolvedValue({
        ok: true,
        json: () => Promise.resolve({
          message_id: 7,
          role: 'user',
          content: 'hello world',
          created_at: '2025-01-01T00:00:00Z',
        }),
      })

      const chunk = await fetchSessionFirstMessage('s1')
      expect(mockFetch).toHaveBeenCalledWith('/api/rag/session-first-message?session_id=s1')
      expect(chunk).toEqual({
        chunk_id: 7,
        chunk_text: 'hello world',
        match_positions: [],
        score: 0,
        role: 'user',
        message_id: 7,
        created_at: '2025-01-01T00:00:00Z',
      })
    })

    it('encodes the session id', async () => {
      mockFetch.mockResolvedValue({ ok: true, json: () => Promise.resolve({ content: 'x' }) })
      await fetchSessionFirstMessage('a/b c')
      expect(mockFetch).toHaveBeenCalledWith('/api/rag/session-first-message?session_id=a%2Fb%20c')
    })

    it('returns null when the session has no messages', async () => {
      mockFetch.mockResolvedValue({
        ok: true,
        json: () => Promise.resolve({ message_id: 0, role: '', content: '', created_at: null }),
      })
      expect(await fetchSessionFirstMessage('empty')).toBeNull()
    })

    it('returns null on HTTP error', async () => {
      mockFetch.mockResolvedValue({ ok: false, status: 403 })
      expect(await fetchSessionFirstMessage('s1')).toBeNull()
    })

    it('returns null on network error', async () => {
      mockFetch.mockRejectedValue(new Error('network down'))
      expect(await fetchSessionFirstMessage('s1')).toBeNull()
    })
  })

  describe('clear', () => {
    it('resets all state', () => {
      const { state, clear } = useSessionSearch()
      state.query = 'test'
      state.results = [{ session_id: 's1' }] as any
      state.total = 1
      state.loading = true
      state.error = 'some error'
      state.searchMode = 'hybrid'

      clear()

      expect(state.query).toBe('')
      expect(state.results).toEqual([])
      expect(state.total).toBe(0)
      expect(state.loading).toBe(false)
      expect(state.error).toBeNull()
      expect(state.searchMode).toBe('')
    })

    it('cancels pending debounce timer', () => {
      const { setQuery, clear } = useSessionSearch()

      setQuery('test')
      clear()

      vi.advanceTimersByTime(300)
      expect(mockFetch).not.toHaveBeenCalled()
    })
  })
})
