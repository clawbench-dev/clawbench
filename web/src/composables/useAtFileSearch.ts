/**
 * Project-wide filename search for the chat input's `@` file reference menu.
 *
 * The menu's local sources (recent files, the current directory, recent
 * uploads/shares) only cover files the user has already touched or is looking
 * at. This adds "type a few characters and find any file in the project",
 * backed by `/api/dir/search/top` — a ranked, truncated JSON endpoint rather
 * than the file manager's SSE stream, because the popup only ever shows a
 * handful of best matches.
 *
 * Kept out of ChatInputBar so the debounce/stale-response handling can be
 * tested without mounting the (very large) input component.
 */

import { ref } from 'vue'
import { apiGet } from '@/utils/api'
import { appLog } from '@/utils/appLog'

export interface AtFileHit {
  /** Project-relative path (the value attached to the message). */
  path: string
  /** Basename, used for display and client-side scoring. */
  name: string
  isDir: boolean
}

interface TopSearchItem {
  name?: string
  path?: string
  type?: string
}

interface TopSearchResponse {
  items?: TopSearchItem[]
}

/** Keystroke debounce. Long enough to skip intermediate queries, short enough
 *  that the popup feels live. */
const DEBOUNCE_MS = 250

/** How many project-wide hits to request. The point is "the best few", not a
 *  browsable list — the local sources already cover the nearby files. */
const TOP_LIMIT = 8

export function useAtFileSearch() {
  /** Ranked hits. Only meaningful together with `hitsQuery`. */
  const hits = ref<AtFileHit[]>([])
  /** The query `hits` were produced for. The menu compares this to the live
   *  query so results never leak into a different (or empty) trigger. */
  const hitsQuery = ref('')

  let timer: ReturnType<typeof setTimeout> | null = null
  let controller: AbortController | null = null
  /** Request generation. A response is applied only when it belongs to the
   *  newest request, so a slow search cannot overwrite a newer one. */
  let generation = 0

  function cancelInFlight() {
    if (timer !== null) {
      clearTimeout(timer)
      timer = null
    }
    if (controller !== null) {
      controller.abort()
      controller = null
    }
    generation++
  }

  /** Drop results and stop any pending/in-flight search. */
  function clear() {
    cancelInFlight()
    if (hits.value.length > 0) hits.value = []
    if (hitsQuery.value !== '') hitsQuery.value = ''
  }

  /** Debounced search. Calling again before the timer fires replaces the query. */
  function schedule(query: string) {
    const q = query.trim()
    if (!q) {
      clear()
      return
    }
    cancelInFlight()
    const mine = generation
    timer = setTimeout(() => {
      timer = null
      void run(q, mine)
    }, DEBOUNCE_MS)
  }

  async function run(q: string, mine: number) {
    const ac = new AbortController()
    controller = ac
    try {
      const res = await apiGet<TopSearchResponse>(
        `/api/dir/search/top?q=${encodeURIComponent(q)}&limit=${TOP_LIMIT}`,
        { signal: ac.signal },
      )
      if (mine !== generation) return
      const items = Array.isArray(res?.items) ? res.items : []
      hitsQuery.value = q
      hits.value = items.flatMap(i => {
        const path = typeof i.path === 'string' ? i.path : ''
        if (!path) return []
        return [{
          path,
          name: i.name || path.split('/').pop() || '',
          isDir: i.type === 'dir',
        }]
      })
    } catch (e) {
      // A superseded request is expected (the user kept typing) — not a failure.
      if (mine !== generation || (e as Error)?.name === 'AbortError') return
      appLog.w('AtFileSearch', 'project file search failed', e)
      hitsQuery.value = ''
      hits.value = []
    } finally {
      if (controller === ac) controller = null
    }
  }

  return { hits, hitsQuery, schedule, clear }
}
