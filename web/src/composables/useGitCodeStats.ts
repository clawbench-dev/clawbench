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

/**
 * Interval usage totals needed for the efficiency ratios. Only the three
 * additive quantities the panel divides by net lines are kept — the full
 * usage response (per-dimension rows, cache split) belongs to the usage tab.
 */
export interface GitUsageTotals {
  total: number
  credit: number
  costUsd: number
}

/** Spend per 1K net added lines, for one interval. */
export interface GitEfficiencyPerK {
  tokens: number
  credit: number
  costUsd: number
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

// Interval usage totals for the efficiency ratios (tokens/credit/cost per 1K
// net lines). Loaded alongside the git stats for the same window so both
// halves of a ratio always describe the same period.
const usageTotals = ref<GitUsageTotals | null>(null)
const usageLoading = ref(false)
const usageError = ref<{ status?: number; msgKey?: string; message?: string } | null>(null)

// Code-inventory (cloc) snapshot of the current working tree. Independent of
// the git-history range above: loaded once per project (backend caches).
const clocRaw = ref<ClocResponse | null>(null)
const clocLoading = ref(false)
const clocError = ref<{ status?: number; msgKey?: string; message?: string } | null>(null)

// Abort controller for in-flight requests.
let abortController: AbortController | null = null
let clocAbortController: AbortController | null = null
let usageAbortController: AbortController | null = null

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

/**
 * Build the /api/git/stats and /api/usage/stats query strings for the current
 * range.
 *
 * Both URLs are derived from ONE rangeToISO call on purpose: the preset ranges
 * are computed from `new Date()` at call time, so building them separately
 * yields windows a few milliseconds apart. That is invisible to the numbers
 * today, but it would make the two halves of an efficiency ratio describe
 * slightly different windows — and a test could not pin "same window" at all.
 */
function buildURLs(): { git: string; usage: string } {
  const { start, end } = rangeToISO(range.value)

  const g = new URLSearchParams()
  g.set('start', start)
  g.set('end', end)
  g.set('trend', '1')

  // Only the range totals are needed for the ratios, so the usage request asks
  // for a single dimension with limit 1 — the backend always returns `totals`
  // regardless of the grouping, and this keeps the payload to one row instead
  // of the whole per-model breakdown the usage tab fetches.
  const u = new URLSearchParams()
  u.set('scope', 'project')
  u.set('start', start)
  u.set('end', end)
  u.append('dims', 'model')
  u.append('metrics', 'total')
  u.set('limit', '1')

  return { git: `/api/git/stats?${g.toString()}`, usage: `/api/usage/stats?${u.toString()}` }
}

/** Fetch the interval usage totals that pair with the git stats window.
 *
 * `url` comes from buildURLs so the two requests of one refresh always carry
 * byte-identical start/end — see the note there. */
async function loadUsageTotals(url: string): Promise<void> {
  if (usageAbortController) usageAbortController.abort()
  const controller = new AbortController()
  usageAbortController = controller

  usageLoading.value = true
  usageError.value = null
  // Clear the previous window's totals before the request: the panel must
  // never pair the NEW net lines with the OLD spend. The two requests finish
  // independently, so without this a fast git response would immediately
  // render a ratio whose numerator is stale — a plausible-looking number that
  // describes no window at all. The panel shows "loading" until both land.
  usageTotals.value = null
  try {
    const resp = await apiGet<{ totals?: { total?: number; credit?: number; costUsd?: number } }>(
      url,
      { signal: controller.signal },
    )
    if (controller.signal.aborted) return
    const t = resp?.totals
    // All-zero means no usage rows fell in the window (the aggregate returns
    // zeros, not an absent object). Keep it null so the panel reports "no
    // usage in range" instead of claiming zero spend per 1K lines, which
    // would read as a free run.
    const hasAny = !!t && ((t.total ?? 0) !== 0 || (t.credit ?? 0) !== 0 || (t.costUsd ?? 0) !== 0)
    usageTotals.value = hasAny
      ? { total: t!.total ?? 0, credit: t!.credit ?? 0, costUsd: t!.costUsd ?? 0 }
      : null
  } catch (e) {
    if (controller.signal.aborted) return
    if (e instanceof DOMException && e.name === 'AbortError') return
    const err = e as Error & { status?: number; msgKey?: string }
    appLog.w('GitStats', 'usage totals load failed', err?.message)
    usageTotals.value = null
    usageError.value = { status: err?.status, msgKey: err?.msgKey, message: err?.message }
  } finally {
    if (!controller.signal.aborted) {
      usageLoading.value = false
    }
  }
}

/** Fetch code-change stats. Public for manual refresh. */
async function loadGitStats(): Promise<void> {
  if (abortController) abortController.abort()
  const controller = new AbortController()
  abortController = controller

  loading.value = true
  error.value = null
  const urls = buildURLs()
  // The efficiency ratios divide spend by net lines, so both halves must
  // describe the same window — refresh them together on every entry point
  // (mount, tab activation, project switch, range change, manual refresh).
  void loadUsageTotals(urls.usage)
  try {
    const resp = await apiGet<GitStatsResponse>(urls.git, { signal: controller.signal })
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
  if (usageAbortController) {
    usageAbortController.abort()
    usageAbortController = null
  }
  range.value = { rangeKey: '24h' }
  raw.value = null
  error.value = null
  loading.value = false
  usageTotals.value = null
  usageError.value = null
  usageLoading.value = false
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
    usageTotals,
    usageLoading,
    usageError,
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
