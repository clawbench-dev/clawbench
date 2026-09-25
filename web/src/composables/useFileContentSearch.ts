import { reactive, computed } from 'vue'
import { appLog } from '@/utils/appLog'
import { useSettingsConfig } from '@/composables/useSettingsConfig'

/** A rune-offset span inside a match line, used for highlighting. */
export interface ContentMatchRange {
  start: number
  end: number
}

/** One matching line inside a file. */
export interface ContentMatch {
  line: number
  text: string
  ranges: ContentMatchRange[]
}

/** One file with at least one matching line. */
export interface ContentSearchFileResult {
  name: string
  path: string
  matches: ContentMatch[]
  /** Total matches in the file, which may exceed `matches.length`. */
  total: number
  /** The per-file match cap was hit. */
  truncated?: boolean
  /** Git would not track this file (dimmed like a browsed entry). */
  ignored?: boolean
}

export type SearchScope = 'current' | 'global'

export interface ContentSearchState {
  query: string
  recursive: boolean
  /** Interpret the query as a regular expression. */
  regex: boolean
  /** Match whole words only. */
  wholeWord: boolean
  /** Case-sensitive matching (default off, like VSCode). */
  caseSensitive: boolean
  /** Comma-separated include globs (gitignore syntax). */
  include: string
  /** Comma-separated exclude globs. */
  exclude: string
  scope: SearchScope
  results: ContentSearchFileResult[]
  searching: boolean
  /** Number of files with at least one match. */
  files: number
  /** Total matching lines across reported files. */
  matches: number
  /** How many files were actually read. */
  searched: number
  truncated: boolean
  /**
   * The user stopped an in-flight search. The results present are a partial
   * prefix of the tree walk, so the counts must not be presented as final —
   * same reason `truncated` exists.
   */
  stopped: boolean
  /** Non-empty when the backend rejected the pattern (e.g. invalid regex). */
  error: string
  searchBasePath: string
}

const DEBOUNCE_MS = 300

/**
 * Content (grep-style) search over the project tree, streamed over SSE.
 *
 * One `result` event carries every match for a single file, so results are
 * grouped as they arrive and the list never has to be reassembled. Mirrors
 * `useFileSearch`'s transport and cancellation model; the difference is that
 * the unit of a result is a file with many matches rather than a single entry.
 */
export function useFileContentSearch() {
  const { getServerValueWithDefault } = useSettingsConfig()

  function getDisplayLimit(): number {
    const val = getServerValueWithDefault('file_search.display_limit')
    return typeof val === 'number' && val >= 10 && val <= 500 ? val : 100
  }

  const state = reactive<ContentSearchState>({
    query: '',
    recursive: false,
    regex: false,
    wholeWord: false,
    caseSensitive: false,
    include: '',
    exclude: '',
    scope: 'current',
    results: [],
    searching: false,
    files: 0,
    matches: 0,
    searched: 0,
    truncated: false,
    stopped: false,
    error: '',
    searchBasePath: '',
  })

  const effectiveDir = computed(() => (state.scope === 'global' ? '' : state.searchBasePath))

  /**
   * Global scope always searches recursively — a non-recursive project-root
   * content search only reads top-level files, which defeats the point. The
   * user's explicit `recursive` preference is preserved so it returns when
   * global scope is switched off.
   */
  const effectiveRecursive = computed(() => state.scope === 'global' || state.recursive)

  /** Total match lines currently loaded (sum of the reported per-file counts). */
  const loadedMatches = computed(() =>
    state.results.reduce((sum, f) => sum + f.matches.length, 0),
  )

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

  /**
   * User-initiated stop. Distinct from `cancelSearch`, which is the silent
   * teardown used when a new search replaces the old one (typing, include /
   * exclude edits, scope change) — marking those as "stopped" would flash a
   * spurious partial-results notice on every keystroke.
   *
   * Closing the EventSource aborts the request, which cancels the backend walk
   * through the request context; the files already reported stay on screen.
   */
  function stopSearch() {
    if (!state.searching) return
    cancelSearch()
    state.stopped = true
  }

  function reset() {
    cancelSearch()
    state.query = ''
    state.results = []
    state.files = 0
    state.matches = 0
    state.searched = 0
    state.truncated = false
    state.stopped = false
    state.error = ''
    state.searchBasePath = ''
  }

  function startSearch(dir: string, immediate = false) {
    cancelSearch()

    const q = state.query.trim()
    if (!q) {
      state.results = []
      state.files = 0
      state.matches = 0
      state.searched = 0
      state.truncated = false
      state.stopped = false
      state.error = ''
      return
    }

    state.searchBasePath = dir
    state.searching = true
    state.results = []
    state.files = 0
    state.matches = 0
    state.searched = 0
    state.truncated = false
    state.stopped = false
    state.error = ''

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
    params.set('regex', state.regex ? 'true' : 'false')
    params.set('wholeWord', state.wholeWord ? 'true' : 'false')
    params.set('caseSensitive', state.caseSensitive ? 'true' : 'false')
    if (state.include.trim()) params.set('include', state.include.trim())
    if (state.exclude.trim()) params.set('exclude', state.exclude.trim())
    params.set('limit', String(displayLimit + 1))

    const url = `/api/file/content-search?${params.toString()}`

    eventSource = new EventSource(url)

    eventSource.addEventListener('result', (e: MessageEvent) => {
      try {
        const data: ContentSearchFileResult = JSON.parse(e.data)
        state.results.push(data)
        state.files = state.results.length
      } catch {
        appLog.w('ContentSearch', 'failed to parse result event')
      }
    })

    eventSource.addEventListener('done', (e: MessageEvent) => {
      try {
        const data = JSON.parse(e.data)
        state.files = data.files
        state.matches = data.matches
        state.searched = data.searched
        state.truncated = data.truncated
      } catch {
        appLog.w('ContentSearch', 'failed to parse done event')
      }
      state.searching = false
      cleanupSSE()
    })

    eventSource.addEventListener('error', (e: MessageEvent) => {
      // Business-level error event (e.g. an invalid regular expression). An
      // EventSource cannot read the body of a non-2xx response, which is why
      // the backend reports this in-band.
      if (e.data) {
        try {
          const data = JSON.parse(e.data)
          state.error = data.message || ''
        } catch {
          appLog.w('ContentSearch', 'SSE error event with invalid data')
        }
      }
      state.searching = false
      cleanupSSE()
    })

    eventSource.onerror = () => {
      // Connection-level error. Fires after a normal close too, so only log
      // while a search is still considered in flight.
      if (state.searching) {
        appLog.w('ContentSearch', 'SSE connection error')
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

  return {
    state,
    effectiveDir,
    effectiveRecursive,
    loadedMatches,
    startSearch,
    cancelSearch,
    stopSearch,
    reset,
    getDisplayLimit,
  }
}
