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
  deleted: boolean
  created_at: string
  match_count: number
  chunks: ChunkHit[]
}

interface SessionSearchResponse {
  sessions: SessionSearchResult[]
  total: number
  mode: string
}

const DEBOUNCE_MS = 300

export function useSessionSearch() {
  const state = reactive({
    query: '',
    results: [] as SessionSearchResult[],
    total: 0,
    loading: false,
    error: null as string | null,
    searchMode: '',
    preferMode: 'hybrid' as 'hybrid' | 'fts',
  })

  let debounceTimer: ReturnType<typeof setTimeout> | null = null
  let abortController: AbortController | null = null

  function cancelPending() {
    if (debounceTimer !== null) {
      clearTimeout(debounceTimer)
      debounceTimer = null
    }
    if (abortController !== null) {
      abortController.abort()
      abortController = null
    }
  }

  function clear() {
    cancelPending()
    state.query = ''
    state.results = []
    state.total = 0
    state.loading = false
    state.error = null
    state.searchMode = ''
  }

  async function search(q: string) {
    const trimmed = q.trim()
    if (!trimmed) {
      state.results = []
      state.total = 0
      state.loading = false
      state.error = null
      state.searchMode = ''
      return
    }

    cancelPending()
    state.loading = true
    state.error = null

    abortController = new AbortController()

    try {
      const res = await fetch('/api/rag/session-search', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ q: trimmed, prefer_mode: state.preferMode }),
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
      state.loading = false
    } catch (err: unknown) {
      if (err instanceof DOMException && err.name === 'AbortError') return
      const message = err instanceof Error ? err.message : String(err)
      appLog.e('SessionSearch', 'search error', message)
      state.error = message
      state.loading = false
    }
  }

  function setQuery(q: string) {
    state.query = q
    cancelPending()
    if (!q.trim()) {
      state.results = []
      state.total = 0
      state.loading = false
      state.error = null
      return
    }
    debounceTimer = setTimeout(() => {
      search(q)
    }, DEBOUNCE_MS)
  }

  onUnmounted(() => {
    cancelPending()
  })

  return { state, setQuery, search, clear }
}
