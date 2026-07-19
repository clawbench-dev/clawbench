import { reactive } from 'vue'
import { appLog } from '@/utils/appLog'

export interface FileSearchResult {
  name: string
  path: string
  type: 'dir' | 'file' | 'image'
  matchedIndices: number[]
}

export interface FileSearchState {
  query: string
  recursive: boolean
  results: FileSearchResult[]
  searching: boolean
  total: number
  truncated: boolean
  searchBasePath: string
}

const DEBOUNCE_MS = 300
const DEFAULT_LIMIT = 100

export function useFileSearch() {
  const state = reactive<FileSearchState>({
    query: '',
    recursive: true,
    results: [],
    searching: false,
    total: 0,
    truncated: false,
    searchBasePath: '',
  })

  let eventSource: EventSource | null = null
  let debounceTimer: ReturnType<typeof setTimeout> | null = null

  function cancelSearch() {
    if (debounceTimer !== null) {
      clearTimeout(debounceTimer)
      debounceTimer = null
    }
    if (eventSource !== null) {
      eventSource.close()
      eventSource = null
    }
    state.searching = false
  }

  function reset() {
    cancelSearch()
    state.query = ''
    state.results = []
    state.total = 0
    state.truncated = false
    state.searchBasePath = ''
  }

  function startSearch(dir: string, immediate = false) {
    cancelSearch()

    const q = state.query.trim()
    if (!q) {
      state.results = []
      state.total = 0
      state.truncated = false
      return
    }

    state.searchBasePath = dir
    state.searching = true
    state.results = []
    state.total = 0
    state.truncated = false

    if (immediate) {
      openSSE(dir)
    } else {
      debounceTimer = setTimeout(() => {
        openSSE(dir)
      }, DEBOUNCE_MS)
    }
  }

  function openSSE(dir: string) {
    const params = new URLSearchParams()
    params.set('path', dir || '')
    params.set('q', state.query.trim())
    params.set('recursive', state.recursive ? 'true' : 'false')
    params.set('limit', String(DEFAULT_LIMIT))

    const url = `/api/dir/search?${params.toString()}`
    appLog.d('FileSearch', 'opening SSE', url)

    eventSource = new EventSource(url)

    eventSource.addEventListener('result', (e: MessageEvent) => {
      try {
        const data: FileSearchResult = JSON.parse(e.data)
        state.results.push(data)
      } catch {
        appLog.w('FileSearch', 'failed to parse result event')
      }
    })

    eventSource.addEventListener('done', (e: MessageEvent) => {
      try {
        const data = JSON.parse(e.data)
        state.total = data.total
        state.truncated = data.truncated
      } catch {
        appLog.w('FileSearch', 'failed to parse done event')
      }
      state.searching = false
      cleanupSSE()
    })

    eventSource.addEventListener('error', (e: MessageEvent) => {
      // Business-level error event from SSE (not EventSource connection error)
      try {
        const data = JSON.parse(e.data)
        appLog.w('FileSearch', 'search error', data.message)
      } catch {
        appLog.w('FileSearch', 'SSE error event with invalid data')
      }
      state.searching = false
      cleanupSSE()
    })

    eventSource.onerror = () => {
      // EventSource connection-level error
      if (state.searching) {
        appLog.w('FileSearch', 'SSE connection error')
        state.searching = false
      }
      cleanupSSE()
    }
  }

  function cleanupSSE() {
    if (eventSource !== null) {
      eventSource.close()
      eventSource = null
    }
  }

  return { state, startSearch, cancelSearch, reset }
}
