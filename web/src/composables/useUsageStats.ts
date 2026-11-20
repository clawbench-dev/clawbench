import { ref, computed } from 'vue'
import { apiGet } from '@/utils/api'
import { appLog } from '@/utils/appLog'
import { localConfig } from '@/composables/useSettingsConfig'
import { readPanelState, writePanelState, type StatsRangeState } from '@/utils/panelState'

/** Aggregation dimension ids. Order = table column order. */
export type UsageDimId = 'model' | 'backend' | 'agent' | 'project'
/** Dims selectable in every scope. `project` joins only in scope=all — see
 * dimsForScope — because in a single-project query every row carries the same
 * project, so grouping by it adds a constant column and no information. */
export const USAGE_DIM_IDS: UsageDimId[] = ['model', 'backend', 'agent']
/** Dims selectable in cross-project scope; `project` leads because it is the
 * headline grouping when aggregating the whole instance. */
export const USAGE_DIM_IDS_ALL: UsageDimId[] = ['project', 'model', 'backend', 'agent']

/** Which projects a query covers. Mirrors the backend's scope enum. */
export type UsageScope = 'project' | 'all'

/** The dims a scope may group by. */
export function dimsForScope(scope: UsageScope): UsageDimId[] {
  return scope === 'all' ? USAGE_DIM_IDS_ALL : USAGE_DIM_IDS
}

/** Numeric metric ids (table columns / chart series). */
export type UsageMetricId = 'input' | 'output' | 'total' | 'cacheHit' | 'credit' | 'cost' | 'duration'
export const USAGE_METRIC_IDS: UsageMetricId[] = ['input', 'output', 'total', 'cacheHit', 'credit', 'cost', 'duration']

export type UsageRangeKey = '24h' | '7d' | '30d' | 'custom'
export type UsageChartType = 'bar' | 'pie' | 'trend'

export interface UsageRange {
  rangeKey: UsageRangeKey
  // Custom calendar days are picked as local dates then converted to UTC
  // instants (see rangeToISO). The backend filters and day-buckets by
  // date(m.created_at) on UTC text, so for non-UTC users the effective window
  // shifts by the local UTC offset relative to the dates shown in the picker.
  // Kept as-is for consistency with the preset ranges (24h/7d/30d are also
  // now-based UTC instants); the backend timebase is UTC.
  customStart?: string // yyyy-mm-dd, when rangeKey === 'custom'
  customEnd?: string
}

/**
 * The window the stats panels open with (设置 → 统计 → 默认时间范围).
 *
 * Read at call time, not at module load, so a setting changed in the same
 * session is picked up by the next panel activation. `custom` is never a
 * default: it needs two concrete dates the user has not picked yet.
 */
export function configuredStatsRange(): UsageRange {
  const key = localConfig.statsDefaultRange
  return { rangeKey: key === '7d' || key === '30d' ? key : '24h' }
}

/** Whether 记住上次选择 is on. One reader, so both panels and the tab agree. */
export function remembersStatsSelection(): boolean {
  return localConfig.statsRememberLast === true
}

/**
 * The window a freshly loaded panel starts on.
 *
 * With 记住上次选择 ON this is the stored selection, which is what makes the
 * switch survive a page reload (the module singleton alone only survives the
 * SPA session). With it OFF the stored value is never read — the configured
 * default wins.
 *
 * A stored `custom` window without both dates is discarded: `rangeToISO`
 * resolves that to "now → now", an empty range that reads as a bug.
 */
export function initialStatsRange(): UsageRange {
  if (remembersStatsSelection()) {
    const saved = readPanelState<StatsRangeState | null>('statsRange')
    if (saved && (saved.rangeKey === '24h' || saved.rangeKey === '7d' || saved.rangeKey === '30d')) {
      return { rangeKey: saved.rangeKey }
    }
    if (saved?.rangeKey === 'custom' && saved.customStart && saved.customEnd) {
      return { rangeKey: 'custom', customStart: saved.customStart, customEnd: saved.customEnd }
    }
  }
  return configuredStatsRange()
}

/** Persist the chosen window — a no-op while 记住上次选择 is OFF. */
export function rememberStatsRange(range: UsageRange): void {
  if (!remembersStatsSelection()) return
  if (range.rangeKey === 'custom') {
    if (!range.customStart || !range.customEnd) return
    writePanelState<StatsRangeState | null>('statsRange', {
      rangeKey: 'custom',
      customStart: range.customStart,
      customEnd: range.customEnd,
    })
    return
  }
  writePanelState<StatsRangeState | null>('statsRange', { rangeKey: range.rangeKey })
}

export interface UsageTotals {
  input: number
  output: number
  total: number
  cacheHit: number
  cacheMiss: number
  credit: number
  costUsd: number
  /** Summed AI runtime in milliseconds (wall clock of every turn in range). */
  durationMs: number
  messageCnt: number
}

export interface UsageRow {
  day?: string
  key: Partial<Record<UsageDimId, string>>
  input: number
  output: number
  total: number
  cacheHit: number
  cacheMiss: number
  credit: number
  costUsd: number
  durationMs: number
  messageCnt: number
}

export interface UsageStatsResponse {
  totals: UsageTotals
  rows: UsageRow[]
  trend?: UsageRow[]
}

export interface UsageFilter {
  scope: UsageScope // 'project' (current) | 'all' (instance-wide)
  /** Agent ids to include; empty = every agent (the panel's 全部智能体). */
  agents: string[]
  range: UsageRange
  dims: UsageDimId[] // >= 1
  metrics: UsageMetricId[] // >= 1; default ['total']
  sortBy: UsageMetricId
  sortDesc: boolean
  chartType: UsageChartType
}

// --- Module-level singleton state ---

const DEFAULT_FILTER: UsageFilter = {
  scope: 'project',
  agents: [],
  range: initialStatsRange(),
  dims: ['model'],
  metrics: ['total'],
  sortBy: 'total',
  sortDesc: true,
  chartType: 'bar',
}

const filter = ref<UsageFilter>({ ...DEFAULT_FILTER, range: { ...DEFAULT_FILTER.range }, dims: [...DEFAULT_FILTER.dims], metrics: [...DEFAULT_FILTER.metrics], agents: [...DEFAULT_FILTER.agents] })
const raw = ref<UsageStatsResponse | null>(null)
const loading = ref(false)
const error = ref<{ status?: number; msgKey?: string; message?: string } | null>(null)

// Abort controller for in-flight requests.
let abortController: AbortController | null = null

// Debounce timer for rapid filter changes.
let debounceTimer: ReturnType<typeof setTimeout> | null = null

/** Convert a preset range key into {start,end} ISO timestamps (now-based). */
function rangeToISO(range: UsageRange): { start: string; end: string } {
  const end = new Date()
  const start = new Date()
  if (range.rangeKey === '24h') {
    start.setHours(start.getHours() - 24)
  } else if (range.rangeKey === '7d') {
    start.setDate(start.getDate() - 7)
  } else if (range.rangeKey === '30d') {
    start.setDate(start.getDate() - 30)
  } else {
    // custom: inclusive calendar days, treat end as end-of-day. The date-only
    // string parses as LOCAL midnight (no zone suffix) and toISOString() then
    // converts to a UTC instant. The backend filters and day-buckets on UTC
    // text (date(m.created_at)), so this is the established convention — see
    // the UsageRange.customStart comment.
    const s = range.customStart ? new Date(`${range.customStart}T00:00:00`) : new Date()
    const e = range.customEnd ? new Date(`${range.customEnd}T23:59:59`) : new Date()
    return { start: s.toISOString(), end: e.toISOString() }
  }
  return { start: start.toISOString(), end: end.toISOString() }
}

/** Build the /api/usage/stats query string for the current filter. */
function buildURL(includeTrend: boolean): string {
  const { start, end } = rangeToISO(filter.value.range)
  const p = new URLSearchParams()
  p.set('scope', filter.value.scope)
  p.set('start', start)
  p.set('end', end)
  for (const d of filter.value.dims) p.append('dims', d)
  for (const m of filter.value.metrics) p.append('metrics', m)
  for (const a of filter.value.agents) p.append('agent', a)
  p.set('sort', filter.value.sortBy)
  p.set('order', filter.value.sortDesc ? 'desc' : 'asc')
  p.set('limit', '200')
  if (includeTrend) p.set('trend', '1')
  return `/api/usage/stats?${p.toString()}`
}

/** Fetch usage stats (and trend when chart type is trend). Public for manual refresh. */
async function loadStats(): Promise<void> {
  if (abortController) abortController.abort()
  const controller = new AbortController()
  abortController = controller

  loading.value = true
  error.value = null
  try {
    const includeTrend = filter.value.chartType === 'trend'
    const resp = await apiGet<UsageStatsResponse>(buildURL(includeTrend), { signal: controller.signal })
    if (controller.signal.aborted) return
    raw.value = resp
  } catch (e) {
    if (controller.signal.aborted) return
    // AbortError from apiGet (timeout) surfaces as a DOMException.
    if (e instanceof DOMException && e.name === 'AbortError') return
    const err = e as Error & { status?: number; msgKey?: string }
    appLog.w('UsageStats', 'load failed', err?.message)
    error.value = { status: err?.status, msgKey: err?.msgKey, message: err?.message }
  } finally {
    if (!controller.signal.aborted) {
      loading.value = false
    }
  }
}

/** Debounced reload — used when filter chips change rapidly. */
function reloadStats(): void {
  if (debounceTimer) clearTimeout(debounceTimer)
  debounceTimer = setTimeout(() => {
    debounceTimer = null
    void loadStats()
  }, 300)
}

/** Reset filters to defaults and fetch. */
async function resetStatsFilter(): Promise<void> {
  filter.value = { ...DEFAULT_FILTER, range: initialStatsRange(), dims: [...DEFAULT_FILTER.dims], metrics: [...DEFAULT_FILTER.metrics], agents: [...DEFAULT_FILTER.agents] }
  await loadStats()
}

/**
 * Reset module-level singleton state — called on SPA project switch (App.vue
 * hotSwitchProject). Cancels any in-flight request and pending debounced reload
 * so a stale request for the previous project never fires against the new one,
 * and clears cached data/filter so the stats panel for the new project starts
 * fresh (its project-root watch reloads on activation).
 */
export function resetUsageStats(): void {
  if (debounceTimer) {
    clearTimeout(debounceTimer)
    debounceTimer = null
  }
  if (abortController) {
    abortController.abort()
    abortController = null
  }
  filter.value = { ...DEFAULT_FILTER, range: initialStatsRange(), dims: [...DEFAULT_FILTER.dims], metrics: [...DEFAULT_FILTER.metrics], agents: [...DEFAULT_FILTER.agents] }
  raw.value = null
  error.value = null
  loading.value = false
}

// --- Derived getters ---

/** Totals overview cards — additive quantities over the range, shown when
 * nonzero. The overall token total leads the list (it is the headline number);
 * the input/output split follows. Cache hit (tokens served from cache) is kept
 * here so the overview shows how much cache traffic contributed; the hit-rate
 * percentage and the hit/miss split are derived in the panel. Runtime comes
 * last: it is the only card measured in time, not tokens, so it must not break
 * up the token flow above it. */
const visibleTotals = computed(() => {
  const t = raw.value?.totals
  if (!t) return [] as { metric: UsageMetricId; value: number }[]
  const items: { metric: UsageMetricId; value: number }[] = []
  if (t.total > 0) items.push({ metric: 'total', value: t.total })
  if (t.input > 0) items.push({ metric: 'input', value: t.input })
  if (t.output > 0) items.push({ metric: 'output', value: t.output })
  if (t.cacheHit > 0) items.push({ metric: 'cacheHit', value: t.cacheHit })
  if (t.credit > 0) items.push({ metric: 'credit', value: t.credit })
  if (t.costUsd > 0) items.push({ metric: 'cost', value: t.costUsd })
  if (t.durationMs > 0) items.push({ metric: 'duration', value: t.durationMs })
  return items
})

/** Sorted table rows (server returns them pre-sorted by the requested sort). */
const tableRows = computed<UsageRow[]>(() => raw.value?.rows ?? [])

export function useUsageStats() {
  function setDims(next: UsageDimId[]) {
    if (next.length === 0) return // at least one dim required
    filter.value.dims = [...next]
    reloadStats()
  }

  /**
   * Switch between the current project and all projects. Any dim that is not
   * selectable in the new scope is dropped (the `project` dim only exists in
   * scope=all), so the request can never carry an invalid combination. If the
   * prune empties the selection, fall back to the scope's default dims.
   */
  function setScope(next: UsageScope) {
    if (filter.value.scope === next) return
    filter.value.scope = next
    const allowed = dimsForScope(next)
    const kept = filter.value.dims.filter(d => allowed.includes(d))
    filter.value.dims = kept.length > 0 ? kept : [allowed[0]]
    void loadStats()
  }

  /**
   * Restrict the aggregate to the given agents. An empty list means every
   * agent — the panel sends it when 全部智能体 is selected.
   */
  function setAgents(next: string[]) {
    filter.value.agents = [...next]
    void loadStats()
  }

  function setMetrics(next: UsageMetricId[]) {
    if (next.length === 0) return // at least one metric required
    filter.value.metrics = [...next]
    // Keep the sort column meaningful: default to first selected metric if the
    // current sort metric was deselected.
    if (!next.includes(filter.value.sortBy)) {
      filter.value.sortBy = next[0]
    }
    reloadStats()
  }

  function setSort(metric: UsageMetricId) {
    if (filter.value.sortBy === metric) {
      filter.value.sortDesc = !filter.value.sortDesc
    } else {
      filter.value.sortBy = metric
      filter.value.sortDesc = true
    }
    void loadStats()
  }

  function setRange(range: UsageRange) {
    filter.value.range = { ...range }
    // Only writes while 记住上次选择 is on — the check lives in rememberStatsRange.
    rememberStatsRange(filter.value.range)
    void loadStats()
  }

  /**
   * Put the window back to the configured default (设置 → 统计).
   *
   * Called when the panel is (re)activated with 记住上次选择 off, so the panel
   * opens where the settings say instead of where the user left it.
   */
  function resetRangeToDefault() {
    filter.value.range = configuredStatsRange()
  }

  function setChartType(type: UsageChartType) {
    if (filter.value.chartType === type) return
    filter.value.chartType = type
    void loadStats() // trend needs a different backend payload
  }

  return {
    filter,
    raw,
    loading,
    error,
    visibleTotals,
    tableRows,
    loadStats,
    reloadStats,
    resetStatsFilter,
    resetUsageStats,
    resetRangeToDefault,
    setScope,
    setDims,
    setAgents,
    setMetrics,
    setSort,
    setRange,
    setChartType,
  }
}
