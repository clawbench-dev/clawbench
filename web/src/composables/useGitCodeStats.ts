import { ref, computed } from 'vue'
import { apiGet } from '@/utils/api'
import { appLog } from '@/utils/appLog'
import type { UsageRange } from '@/composables/useUsageStats'

/** Git code-change statistic shape returned by GET /api/git/stats. */
export interface GitStatsTotals {
  added: number
  deleted: number
  net: number
  commitCnt: number
}

export interface GitStatsRow {
  day?: string
  author: string
  added: number
  deleted: number
  net: number
  commitCnt: number
}

export interface GitStatsResponse {
  isGit: boolean
  totals?: GitStatsTotals
  rows: GitStatsRow[]
  trend?: GitStatsRow[]
}

/** Code-inventory (cloc) row returned by GET /api/git/cloc. */
export interface ClocLanguageRow {
  name: string
  files: number
  code: number
  comment: number
  blank: number
}

export interface ClocResponse {
  languages: ClocLanguageRow[]
  total: ClocLanguageRow
  scannedAt: string
}

// --- Module-level singleton state ---
// Mirrors useUsageStats: one shared state per SPA run, reset on project switch.

const range = ref<UsageRange>({ rangeKey: '24h' })
const raw = ref<GitStatsResponse | null>(null)
const loading = ref(false)
const error = ref<{ status?: number; msgKey?: string; message?: string } | null>(null)

// Code-inventory (cloc) snapshot of the current working tree. Independent of
// the git-history range above: loaded once per project (backend caches).
const clocRaw = ref<ClocResponse | null>(null)
const clocLoading = ref(false)
const clocError = ref<{ status?: number; msgKey?: string; message?: string } | null>(null)

// Abort controller for in-flight requests.
let abortController: AbortController | null = null
let clocAbortController: AbortController | null = null

// Debounce timer for rapid range changes.
let debounceTimer: ReturnType<typeof setTimeout> | null = null

/**
 * Convert a range into {start,end} ISO timestamps. Identical semantics to
 * useUsageStats.rangeToISO (UTC instants, custom dates local → UTC) so both
 * stats panels share one timebase.
 */
function rangeToISO(r: UsageRange): { start: string; end: string } {
  const end = new Date()
  const start = new Date()
  if (r.rangeKey === '24h') {
    start.setHours(start.getHours() - 24)
  } else if (r.rangeKey === '7d') {
    start.setDate(start.getDate() - 7)
  } else if (r.rangeKey === '30d') {
    start.setDate(start.getDate() - 30)
  } else {
    const s = r.customStart ? new Date(`${r.customStart}T00:00:00`) : new Date()
    const e = r.customEnd ? new Date(`${r.customEnd}T23:59:59`) : new Date()
    return { start: s.toISOString(), end: e.toISOString() }
  }
  return { start: start.toISOString(), end: end.toISOString() }
}

/** Build the /api/git/stats query string for the current range. */
function buildURL(): string {
  const { start, end } = rangeToISO(range.value)
  const p = new URLSearchParams()
  p.set('start', start)
  p.set('end', end)
  p.set('trend', '1')
  return `/api/git/stats?${p.toString()}`
}

/** Fetch code-change stats. Public for manual refresh. */
async function loadGitStats(): Promise<void> {
  if (abortController) abortController.abort()
  const controller = new AbortController()
  abortController = controller

  loading.value = true
  error.value = null
  try {
    const resp = await apiGet<GitStatsResponse>(buildURL(), { signal: controller.signal })
    if (controller.signal.aborted) return
    raw.value = resp
  } catch (e) {
    if (controller.signal.aborted) return
    if (e instanceof DOMException && e.name === 'AbortError') return
    const err = e as Error & { status?: number; msgKey?: string }
    appLog.w('GitStats', 'load failed', err?.message)
    error.value = { status: err?.status, msgKey: err?.msgKey, message: err?.message }
  } finally {
    if (!controller.signal.aborted) {
      loading.value = false
    }
  }
}

/** Debounced reload — used when range chips change rapidly. */
function reloadGitStats(): void {
  if (debounceTimer) clearTimeout(debounceTimer)
  debounceTimer = setTimeout(() => {
    debounceTimer = null
    void loadGitStats()
  }, 300)
}

/** Fetch the code-inventory (cloc) snapshot. Public for manual refresh. */
async function loadCloc(): Promise<void> {
  if (clocAbortController) clocAbortController.abort()
  const controller = new AbortController()
  clocAbortController = controller

  clocLoading.value = true
  clocError.value = null
  try {
    const resp = await apiGet<ClocResponse>('/api/git/cloc', { signal: controller.signal })
    if (controller.signal.aborted) return
    clocRaw.value = resp
  } catch (e) {
    if (controller.signal.aborted) return
    if (e instanceof DOMException && e.name === 'AbortError') return
    const err = e as Error & { status?: number; msgKey?: string }
    appLog.w('GitStats', 'cloc load failed', err?.message)
    clocError.value = { status: err?.status, msgKey: err?.msgKey, message: err?.message }
  } finally {
    if (!controller.signal.aborted) {
      clocLoading.value = false
    }
  }
}

/**
 * Reset module-level singleton state — called on SPA project switch (App.vue
 * hotSwitchProject). Cancels any in-flight request and pending debounced
 * reload so a stale request for the previous project never fires against the
 * new one.
 */
export function resetGitStats(): void {
  if (debounceTimer) {
    clearTimeout(debounceTimer)
    debounceTimer = null
  }
  if (abortController) {
    abortController.abort()
    abortController = null
  }
  if (clocAbortController) {
    clocAbortController.abort()
    clocAbortController = null
  }
  range.value = { rangeKey: '24h' }
  raw.value = null
  error.value = null
  loading.value = false
  clocRaw.value = null
  clocError.value = null
  clocLoading.value = false
}

// --- Derived getters ---

/** Non-zero overview cards for the range totals (when in a git repo). */
const visibleTotals = computed(() => {
  const r = raw.value
  if (!r?.isGit || !r.totals) return [] as { metric: 'added' | 'deleted' | 'net' | 'commitCnt'; value: number }[]
  const t = r.totals
  const items: { metric: 'added' | 'deleted' | 'net' | 'commitCnt'; value: number }[] = []
  if (t.added > 0 || t.net !== 0 || t.commitCnt > 0) {
    items.push({ metric: 'added', value: t.added })
    items.push({ metric: 'deleted', value: t.deleted })
    items.push({ metric: 'net', value: t.net })
  }
  items.push({ metric: 'commitCnt', value: t.commitCnt })
  return items
})

const tableRows = computed<GitStatsRow[]>(() => raw.value?.rows ?? [])
const trend = computed<GitStatsRow[]>(() => raw.value?.trend ?? [])

export function useGitCodeStats() {
  function setRange(r: UsageRange) {
    range.value = { ...r }
    void loadGitStats()
  }

  return {
    range,
    raw,
    loading,
    error,
    visibleTotals,
    tableRows,
    trend,
    clocRaw,
    clocLoading,
    clocError,
    loadGitStats,
    reloadGitStats,
    loadCloc,
    resetGitStats,
    setRange,
  }
}
