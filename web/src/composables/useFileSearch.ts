import { reactive, computed } from 'vue'
import { appLog } from '@/utils/appLog'
import { useSettingsConfig } from '@/composables/useSettingsConfig'

export interface FileSearchResult {
  name: string
  path: string
  type: 'dir' | 'file' | 'image'
  size?: number
  modified?: string
  matchedIndices: number[]
}

export type SearchScope = 'current' | 'global'

export interface FileSearchState {
  query: string
  recursive: boolean
  scope: SearchScope
  exact: boolean
  results: FileSearchResult[]
  searching: boolean
  total: number
  truncated: boolean
  searchBasePath: string
}

const DEBOUNCE_MS = 300

export function useFileSearch() {
  const { getServerValueWithDefault } = useSettingsConfig()

  function getDisplayLimit(): number {
    const val = getServerValueWithDefault('file_search.display_limit')
    return typeof val === 'number' && val >= 10 && val <= 500 ? val : 100
  }

  const state = reactive<FileSearchState>({
    query: '',
    recursive: false,
    scope: 'current',
    exact: false,
    results: [],
    searching: false,
    total: 0,
    truncated: false,
    searchBasePath: '',
  })

  const effectiveDir = computed(() => state.scope === 'global' ? '' : state.searchBasePath)

  /**
   * Global scope always searches recursively — a non-recursive project-root
   * search only ever matches top-level entries, which defeats the point of a
   * global search. The user's explicit `recursive` preference is preserved so
   * it is restored when global scope is switched off.
   */
  const effectiveRecursive = computed(() => state.scope === 'global' || state.recursive)

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

    // When scope is 'global', always search from project root (empty string)
    const searchDir = state.scope === 'global' ? '' : dir

    if (immediate) {
      openSSE(searchDir)
    } else {
      debounceTimer = setTimeout(() => {
        openSSE(searchDir)
      }, DEBOUNCE_MS)
    }
  }

  function openSSE(dir: string) {
    const displayLimit = getDisplayLimit()
    const params = new URLSearchParams()
    params.set('path', dir || '')
    params.set('q', state.query.trim())
    params.set('recursive', effectiveRecursive.value ? 'true' : 'false')
    params.set('exact', state.exact ? 'true' : 'false')
    params.set('limit', String(displayLimit + 1))

    const url = `/api/dir/search?${params.toString()}`

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

  return { state, effectiveDir, effectiveRecursive, startSearch, cancelSearch, reset, getDisplayLimit }
}
