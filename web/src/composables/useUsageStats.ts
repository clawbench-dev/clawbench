import { ref, computed } from 'vue'
import { apiGet } from '@/utils/api'
import { appLog } from '@/utils/appLog'

/** Aggregation dimension ids. Order = table column order. */
export type UsageDimId = 'model' | 'backend' | 'agent'
export const USAGE_DIM_IDS: UsageDimId[] = ['model', 'backend', 'agent']

/** Numeric metric ids (table columns / chart series). */
export type UsageMetricId = 'input' | 'output' | 'total' | 'cacheHit' | 'hitRate' | 'credit' | 'cost'
export const USAGE_METRIC_IDS: UsageMetricId[] = ['input', 'output', 'total', 'cacheHit', 'hitRate', 'credit', 'cost']

export type UsageRangeKey = '24h' | '7d' | '30d' | 'custom'
export type UsageChartType = 'bar' | 'pie' | 'trend'

export interface UsageRange {
  rangeKey: UsageRangeKey
  customStart?: string // yyyy-mm-dd, when rangeKey === 'custom'
  customEnd?: string
}

export interface UsageTotals {
  input: number
  output: number
  total: number
  cacheHit: number
  cacheMiss: number
  credit: number
  costUsd: number
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
  messageCnt: number
}

export interface UsageStatsResponse {
  totals: UsageTotals
  rows: UsageRow[]
  trend?: UsageRow[]
}

export interface UsageFilter {
  range: UsageRange
  dims: UsageDimId[] // >= 1
  metrics: UsageMetricId[] // >= 1; default ['total']
  sortBy: UsageMetricId
  sortDesc: boolean
  chartType: UsageChartType
}

// --- Module-level singleton state ---

const DEFAULT_FILTER: UsageFilter = {
  range: { rangeKey: '24h' },
  dims: ['model'],
  metrics: ['total'],
  sortBy: 'total',
  sortDesc: true,
  chartType: 'bar',
}

const filter = ref<UsageFilter>({ ...DEFAULT_FILTER, range: { ...DEFAULT_FILTER.range }, dims: [...DEFAULT_FILTER.dims], metrics: [...DEFAULT_FILTER.metrics] })
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
    // custom: inclusive calendar days, treat end as end-of-day.
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
  p.set('start', start)
  p.set('end', end)
  for (const d of filter.value.dims) p.append('dims', d)
  for (const m of filter.value.metrics) p.append('metrics', m)
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
  filter.value = { ...DEFAULT_FILTER, range: { ...DEFAULT_FILTER.range }, dims: [...DEFAULT_FILTER.dims], metrics: [...DEFAULT_FILTER.metrics] }
  await loadStats()
}

// --- Derived getters ---

/** Totals cards — a metric is only shown when its aggregated value is nonzero. */
const visibleTotals = computed(() => {
  const t = raw.value?.totals
  if (!t) return [] as { metric: UsageMetricId; value: number }[]
  const items: { metric: UsageMetricId; value: number }[] = []
  if (t.input > 0) items.push({ metric: 'input', value: t.input })
  if (t.output > 0) items.push({ metric: 'output', value: t.output })
  if (t.total > 0) items.push({ metric: 'total', value: t.total })
  if (t.cacheHit > 0) items.push({ metric: 'cacheHit', value: t.cacheHit })
  if (t.cacheHit + t.cacheMiss > 0) items.push({ metric: 'hitRate', value: hitRateOf(t.cacheHit, t.cacheMiss) })
  if (t.credit > 0) items.push({ metric: 'credit', value: t.credit })
  if (t.costUsd > 0) items.push({ metric: 'cost', value: t.costUsd })
  return items
})

function hitRateOf(hit: number, miss: number): number {
  const den = hit + miss
  if (den <= 0) return 0
  return hit / den
}

/** Sorted table rows (server returns them pre-sorted by the requested sort). */
const tableRows = computed<UsageRow[]>(() => raw.value?.rows ?? [])

export function useUsageStats() {
  function setDims(next: UsageDimId[]) {
    if (next.length === 0) return // at least one dim required
    filter.value.dims = [...next]
    reloadStats()
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
    void loadStats()
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
    setDims,
    setMetrics,
    setSort,
    setRange,
    setChartType,
  }
}
