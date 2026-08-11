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

import { useSessionSearch } from '@/composables/useSessionSearch'

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
        body: JSON.stringify({ q: 'test query', prefer_mode: 'hybrid' }),
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
        body: JSON.stringify({ q: 'test', prefer_mode: 'hybrid' }),
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
        body: JSON.stringify({ q: '', prefer_mode: 'hybrid' }),
      }))
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
        body: JSON.stringify({ q: 'test', prefer_mode: 'fts' }),
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
        body: JSON.stringify({ q: 'abc', prefer_mode: 'hybrid' }),
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
        body: JSON.stringify({ q: '', prefer_mode: 'hybrid' }),
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
        body: JSON.stringify({ q: '', prefer_mode: 'hybrid' }),
      }))
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
