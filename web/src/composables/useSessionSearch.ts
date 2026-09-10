import { reactive, onUnmounted } from 'vue'
import { appLog } from '@/utils/appLog'

export interface MatchRange {
  start: number
  end: number
}

export interface ChunkHit {
  chunk_id: number
  chunk_text: string
  match_positions: MatchRange[]
  score: number
  role: string
  message_id: number
  created_at: string
}

export interface SessionSearchResult {
  session_id: string
  session_title: string
  score: number
  backend: string
  project_path: string
  archived: boolean
  created_at: string
  match_count: number
  chunks: ChunkHit[]
}

interface SessionSearchResponse {
  sessions: SessionSearchResult[]
  total: number
  mode: string
  has_more?: boolean
}

const DEBOUNCE_MS = 300

export type SessionArchiveFilter = 'all' | 'active' | 'archived'
export type SessionSortOrder = 'relevance' | 'newest' | 'oldest'

// fetchSessionFirstMessage lazily loads a session's earliest message for the
// browse-mode detail preview. The browse list omits message content for
// performance, so this is called only when a session's detail view is opened.
export async function fetchSessionFirstMessage(sessionId: string): Promise<ChunkHit | null> {
  try {
    const res = await fetch(`/api/rag/session-first-message?session_id=${encodeURIComponent(sessionId)}`)
    if (!res.ok) return null
    const data = await res.json()
    if (!data || !data.content) return null
    return {
      chunk_id: data.message_id ?? 0,
      chunk_text: data.content,
      match_positions: [],
      score: 0,
      role: data.role ?? '',
      message_id: data.message_id ?? 0,
      created_at: data.created_at ?? '',
    }
  } catch (err: unknown) {
    appLog.e('SessionSearch', 'first message fetch error', err instanceof Error ? err.message : String(err))
    return null
  }
}

export function useSessionSearch() {
  const state = reactive({
    query: '',
    results: [] as SessionSearchResult[],
    total: 0,
    loading: false,
    loadingMore: false,
    hasMore: false,
    error: null as string | null,
    searchMode: '',
    preferMode: 'hybrid' as 'hybrid' | 'fts',
    archivedFilter: 'all' as SessionArchiveFilter,
    sortOrder: 'relevance' as SessionSortOrder,
  })

  let debounceTimer: ReturnType<typeof setTimeout> | null = null
  let abortController: AbortController | null = null
  let loadMoreController: AbortController | null = null

  function cancelPending() {
    if (debounceTimer !== null) {
      clearTimeout(debounceTimer)
      debounceTimer = null
    }
    if (abortController !== null) {
      abortController.abort()
      abortController = null
    }
    if (loadMoreController !== null) {
      loadMoreController.abort()
      loadMoreController = null
    }
  }

  function clear() {
    cancelPending()
    state.query = ''
    state.results = []
    state.total = 0
    state.loading = false
    state.loadingMore = false
    state.hasMore = false
    state.error = null
    state.searchMode = ''
  }

  function requestBody(q: string, cursor?: string, cursorId?: string) {
    const body: Record<string, string> = {
      q,
      prefer_mode: state.preferMode,
      archived: state.archivedFilter,
      sort: state.sortOrder,
    }
    if (cursor && cursorId) {
      body.cursor = cursor
      body.cursor_id = cursorId
    }
    return body
  }

  async function doFetch(q: string) {
    cancelPending()
    state.loading = true
    state.loadingMore = false
    state.error = null

    abortController = new AbortController()

    try {
      const res = await fetch('/api/rag/session-search', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(requestBody(q)),
        signal: abortController.signal,
      })

      if (!res.ok) {
        const text = await res.text().catch(() => '')
        state.error = text || `Search failed: ${res.status}`
        state.loading = false
        return
      }

      const data: SessionSearchResponse = await res.json()
      state.results = data.sessions
      state.total = data.total
      state.searchMode = data.mode
      state.hasMore = !!data.has_more
      state.loading = false
    } catch (err: unknown) {
      if (err instanceof DOMException && err.name === 'AbortError') return
      const message = err instanceof Error ? err.message : String(err)
      appLog.e('SessionSearch', 'search error', message)
      state.error = message
      state.loading = false
    }
  }

  // Browse all sessions when there is no query to search for. In browse mode
  // the list is paginated: loadMore appends the next page on scroll.
  function browse() {
    return doFetch('')
  }

  // Append the next page of browse results. Only meaningful in browse mode —
  // search results are ranked and not paginated.
  async function loadMore() {
    if (state.loading || state.loadingMore || !state.hasMore) return
    if (state.searchMode !== 'recent') return
    const last = state.results[state.results.length - 1]
    if (!last) return

    loadMoreController = new AbortController()
    state.loadingMore = true
    try {
      const res = await fetch('/api/rag/session-search', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(requestBody('', last.created_at, last.session_id)),
        signal: loadMoreController.signal,
      })
      if (!res.ok) {
        state.loadingMore = false
        return
      }
      const data: SessionSearchResponse = await res.json()
      // Guard against duplicates if the same page is somehow returned twice.
      const seen = new Set(state.results.map(r => r.session_id))
      const fresh = (data.sessions || []).filter(s => !seen.has(s.session_id))
      state.results = [...state.results, ...fresh]
      state.hasMore = !!data.has_more
      state.loadingMore = false
    } catch (err: unknown) {
      if (err instanceof DOMException && err.name === 'AbortError') return
      const message = err instanceof Error ? err.message : String(err)
      appLog.e('SessionSearch', 'load more error', message)
      state.loadingMore = false
    }
  }

  function search(q: string) {
    return doFetch(q.trim())
  }

  function setQuery(q: string) {
    state.query = q
    cancelPending()
    if (!q.trim()) {
      // Empty query → browse all sessions newest-first instead of a stale list.
      browse()
      return
    }
    debounceTimer = setTimeout(() => {
      search(q)
    }, DEBOUNCE_MS)
  }

  // Apply an archive filter and/or sort order, then re-run the current query
  // (or browse) so the visible list reflects the new selection immediately.
  function setFilters(filters: { archived?: SessionArchiveFilter; sort?: SessionSortOrder }) {
    if (filters.archived !== undefined) state.archivedFilter = filters.archived
    if (filters.sort !== undefined) state.sortOrder = filters.sort
    if (state.query.trim()) {
      search(state.query)
    } else {
      browse()
    }
  }

  onUnmounted(() => {
    cancelPending()
  })

  return { state, setQuery, search, browse, clear, setFilters, loadMore }
}
