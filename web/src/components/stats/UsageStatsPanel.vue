<template>
  <div ref="panelEl" class="usage-stats-panel" v-show="active">
    <!-- Compact header — matches settings/task panel header style -->
    <header class="stats-header">
      <BarChart3 :size="18" class="stats-header-icon" />
      <span class="stats-header-title">{{ t('nav.stats') }}</span>
      <div class="stats-header-actions">
        <RefreshButton class="stats-refresh" :loading="loading" :disabled="loading" :title="t('nav.refresh')" @click="onRefresh" />
      </div>
    </header>

    <div class="stats-body">
      <!-- Range card -->
      <section class="stats-card-panel">
        <div class="stats-card-title">
          <Clock :size="13" class="stats-card-title-icon" />
          <span>{{ t('stats.rangeTitle') }}</span>
        </div>
        <div class="stats-chip-scroll">
          <button
            v-for="r in rangePresets"
            :key="r.key"
            class="stats-chip"
            :class="{ active: filter.range.rangeKey === r.key }"
            @click="selectRangePreset(r.key)"
          >
            {{ t(r.labelKey) }}
          </button>
          <button class="stats-chip" :class="{ active: filter.range.rangeKey === 'custom' }" @click="selectRangePreset('custom')">
            {{ t('stats.custom') }}
          </button>
          <template v-if="filter.range.rangeKey === 'custom'">
            <span class="stats-date-sep">·</span>
            <input v-model="customStart" type="date" class="stats-date-input" @change="applyCustomRange" />
            <span class="stats-date-sep">→</span>
            <input v-model="customEnd" type="date" class="stats-date-input" @change="applyCustomRange" />
          </template>
        </div>
      </section>

      <!-- Totals overview: reflects the time-range totals only — independent of
           the dimension filter below, so it sits above the filter card. -->
      <section v-if="totalsPresent" class="stats-card-panel">
        <div class="stats-card-title">
          <Gauge :size="13" class="stats-card-title-icon" />
          <span>{{ t('stats.summaryTitle') }}</span>
        </div>
        <div class="stats-summary" :class="{ 'is-stacked': summaryStacked }" ref="summaryEl">
          <div class="stats-donut-col">
            <div class="stats-donut-title" v-if="!overviewDrill">{{ t('stats.summaryInOut') }}</div>
            <div class="stats-donut-title" v-else>
              {{ t('stats.summaryInputCache') }}
              <button class="stats-donut-back" @click="overviewDrill = null">{{ t('stats.back') }}</button>
            </div>
            <UsageChart
              :option="overviewDrill === 'input' ? cacheDonutOption : overviewDonutOption"
              class="stats-donut"
              @chart-click="onOverviewSliceClick"
            />
          </div>
          <div class="stats-totals">
            <div v-for="card in visibleTotals" :key="card.metric" class="stats-total">
              <span class="stats-total-label">{{ t(metricLabelKey(card.metric)) }}</span>
              <span class="stats-total-value">{{ formatMetricCardValue(card.metric, card.value) }}</span>
            </div>
            <!-- Cache hit rate is an overview-only derived metric (it is not an
                 additive column) — shown here whenever the range has cache data. -->
            <div v-if="overviewCachePresent" class="stats-total">
              <span class="stats-total-label">{{ t('stats.colHitRate') }}</span>
              <span class="stats-total-value">{{ overviewHitRateText }}</span>
            </div>
          </div>
        </div>
      </section>

      <!-- Filters card -->
      <section class="stats-card-panel">
        <div class="stats-card-title">
          <SlidersHorizontal :size="13" class="stats-card-title-icon" />
          <span>{{ t('stats.filterTitle') }}</span>
        </div>
        <div class="stats-filter-group">
          <span class="stats-filter-label">{{ t('stats.dimTitle') }}</span>
          <div class="stats-chip-scroll">
            <button
              v-for="d in dimOptions"
              :key="d.id"
              class="stats-chip"
              :class="{ active: filter.dims.includes(d.id) }"
              @click="toggleDim(d.id)"
            >
              {{ t(d.labelKey) }}
            </button>
          </div>
        </div>
        <div class="stats-filter-group">
          <span class="stats-filter-label">{{ t('stats.metricTitle') }}</span>
          <div class="stats-chip-scroll">
            <button
              v-for="m in metricOptions"
              :key="m.id"
              class="stats-chip"
              :class="{ active: filter.metrics.includes(m.id) }"
              @click="toggleMetric(m.id)"
            >
              {{ t(m.labelKey) }}
            </button>
          </div>
        </div>
        <div class="stats-filter-group">
          <span class="stats-filter-label">{{ t('stats.chartTitle') }}</span>
          <div class="stats-chip-scroll">
            <button
              v-for="c in chartTypeOptions"
              :key="c.id"
              class="stats-chip"
              :class="{ active: filter.chartType === c.id }"
              @click="selectChartType(c.id)"
            >
              {{ t(c.labelKey) }}
            </button>
          </div>
        </div>
      </section>

      <!-- Error banner -->
      <div v-if="error" class="stats-error">{{ errorText }}</div>

      <!-- Loading -->
      <div v-if="loading && !tableRows.length" class="stats-loading">
        <LoadingIndicator size="sm" inline />
        <span>{{ t('common.loading') }}</span>
      </div>

      <template v-else>
        <!-- Empty state -->
        <div
          v-if="!hasContent && !error"
          class="stats-empty"
        >
          <BarChart3 :size="34" class="stats-empty-icon" />
          <span>{{ t('stats.noData') }}</span>
        </div>

        <template v-else-if="hasContent">
          <!-- Detail table (only in non-trend grouping; trend responses carry no rows) -->
          <section v-if="filteredRows.length > 0" class="stats-card-panel">
            <div class="stats-card-title">
              <Table :size="13" class="stats-card-title-icon" />
              <span>{{ t('stats.detailTitle') }}</span>
              <span class="stats-count-chip">{{ filteredRows.length }}</span>
            </div>
            <div class="stats-table-wrap">
              <table class="stats-table">
                <thead>
                  <tr>
                    <th v-for="d in filter.dims" :key="d" class="stats-th-dim">
                      {{ t(dimLabelKey(d)) }}
                    </th>
                    <th
                      v-for="m in filter.metrics"
                      :key="m"
                      class="stats-th-num"
                      :class="{ 'sorted': filter.sortBy === m }"
                      @click="sortByMetric(m)"
                    >
                      <span class="stats-th-inner">
                        {{ t(metricLabelKey(m)) }}
                        <span v-if="filter.sortBy === m" class="stats-sort-arrow">{{ filter.sortDesc ? '↓' : '↑' }}</span>
                      </span>
                    </th>
                  </tr>
                </thead>
                <tbody>
                  <tr v-for="(row, i) in filteredRows" :key="i">
                    <td v-for="d in filter.dims" :key="d" class="stats-td-dim">
                      {{ row.key[d] === EMPTY_GROUP_LABEL ? t('stats.emptyLabel') : (row.key[d] || t('stats.emptyLabel')) }}
                    </td>
                    <td v-for="m in filter.metrics" :key="m" class="stats-td-num">
                      {{ formatMetricCellValue(m, row) }}
                    </td>
                  </tr>
                </tbody>
              </table>
            </div>
          </section>

          <!-- Charts: one card per selected metric (bar/pie from rows, trend from trend) -->
          <section v-for="m in filter.metrics" :key="m" class="stats-card-panel stats-chart-panel">
            <div class="stats-card-title">
              <ChartPie :size="13" class="stats-card-title-icon" />
              <span>{{ t(metricLabelKey(m)) }}</span>
            </div>
            <UsageChart :option="chartOptionFor(m)" class="stats-chart" />
          </section>
        </template>
      </template>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, watch, onMounted, onBeforeUnmount } from 'vue'
import { useI18n } from 'vue-i18n'
import { BarChart3, Clock, SlidersHorizontal, Gauge, Table, ChartPie } from 'lucide-vue-next'
import RefreshButton from '@/components/common/RefreshButton.vue'
import LoadingIndicator from '@/components/common/LoadingIndicator.vue'
import UsageChart from '@/components/stats/UsageChart.vue'
import {
  useUsageStats,
  USAGE_DIM_IDS,
  USAGE_METRIC_IDS,
  type UsageDimId,
  type UsageMetricId,
  type UsageChartType,
  type UsageRangeKey,
} from '@/composables/useUsageStats'
import { store } from '@/stores/app'
import {
  buildBarOption,
  buildPieOption,
  buildTrendOption,
  buildOverviewDonut,
  buildCacheDonut,
  formatMetricValue,
  rowValueOf,
  EMPTY_GROUP_LABEL,
} from '@/components/stats/statsChart'

const props = defineProps<{
  active: boolean
}>()

const { t } = useI18n()
const stats = useUsageStats()
const filter = stats.filter
const { visibleTotals, tableRows, loading, error } = stats

const rangePresets: { key: UsageRangeKey; labelKey: string }[] = [
  { key: '24h', labelKey: 'stats.range24h' },
  { key: '7d', labelKey: 'stats.range7d' },
  { key: '30d', labelKey: 'stats.range30d' },
]

const dimOptions: { id: UsageDimId; labelKey: string }[] = USAGE_DIM_IDS.map(id => ({ id, labelKey: dimLabelKey(id) }))
const metricOptions: { id: UsageMetricId; labelKey: string }[] = USAGE_METRIC_IDS.map(id => ({ id, labelKey: metricLabelKey(id) }))
const chartTypeOptions: { id: UsageChartType; labelKey: string }[] = [
  { id: 'bar', labelKey: 'stats.chartBar' },
  { id: 'pie', labelKey: 'stats.chartPie' },
  { id: 'trend', labelKey: 'stats.chartTrend' },
]

const customStart = ref<string>('')
const customEnd = ref<string>('')

// Theme tick — bumped on every clawbench-theme-change so the chart options
// (built from CSS variables via resolveStatsPalette) are recomputed and pushed
// to UsageChart as a fresh `option` prop. Without this the palette captured at
// build time would stay on the previous theme until a filter/data change.
const themeTick = ref(0)

// Viewport tick — bumped on window resize so options are rebuilt when crossing
// the narrow/mobile boundary (value-axis tick text is hidden on mobile).
const viewportTick = ref(0)

// Stacked overview layout: when the panel is too narrow to fit the donut
// beside the value cards, switch to a vertical (top-to-bottom) arrangement.
// The panel root always exists (unlike the v-if summary section), so it is the
// observed element; its content width is the summary's max width minus padding.
const panelEl = ref<HTMLElement | null>(null)
const summaryStacked = ref(false)
let panelResizeObserver: ResizeObserver | null = null

function updateSummaryStacked() {
  summaryStacked.value = !!panelEl.value && panelEl.value.clientWidth < 460
}

function onThemeChange() {
  themeTick.value++
}
function onWindowResize() {
  viewportTick.value++
  updateSummaryStacked()
}

onMounted(() => {
  window.addEventListener('clawbench-theme-change', onThemeChange)
  window.addEventListener('resize', onWindowResize)
  if (panelEl.value && typeof ResizeObserver !== 'undefined') {
    panelResizeObserver = new ResizeObserver(() => updateSummaryStacked())
    panelResizeObserver.observe(panelEl.value)
  }
  updateSummaryStacked()
})

onBeforeUnmount(() => {
  window.removeEventListener('clawbench-theme-change', onThemeChange)
  window.removeEventListener('resize', onWindowResize)
  if (panelResizeObserver) {
    panelResizeObserver.disconnect()
    panelResizeObserver = null
  }
})

function dimLabelKey(d: UsageDimId): string {
  switch (d) {
    case 'model': return 'stats.dimModel'
    case 'backend': return 'stats.dimBackend'
    case 'agent': return 'stats.dimAgent'
  }
}
function metricLabelKey(m: UsageMetricId): string {
  switch (m) {
    case 'input': return 'stats.colInput'
    case 'output': return 'stats.colOutput'
    case 'total': return 'stats.colTotal'
    case 'cacheHit': return 'stats.colCacheHit'
    case 'credit': return 'stats.colCredit'
    case 'cost': return 'stats.colCost'
  }
}

// --- Range ---
function selectRangePreset(key: UsageRangeKey) {
  if (key === 'custom') {
    filter.value.range.rangeKey = 'custom'
    // Initialize date inputs to sensible defaults if unset.
    if (!customStart.value || !customEnd.value) {
      const end = new Date()
      const start = new Date()
      start.setDate(start.getDate() - 6)
      customStart.value = toDateInput(start)
      customEnd.value = toDateInput(end)
    }
    stats.setRange({ rangeKey: 'custom', customStart: customStart.value, customEnd: customEnd.value })
  } else {
    stats.setRange({ rangeKey: key })
  }
}

function applyCustomRange() {
  if (!customStart.value || !customEnd.value) return
  if (customStart.value > customEnd.value) return
  stats.setRange({ rangeKey: 'custom', customStart: customStart.value, customEnd: customEnd.value })
}

function toDateInput(d: Date): string {
  const p = (n: number) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())}`
}

// --- Filter toggles ---
function toggleDim(d: UsageDimId) {
  const next = filter.value.dims.includes(d)
    ? filter.value.dims.filter(x => x !== d)
    : [...filter.value.dims, d]
  stats.setDims(next)
}

function toggleMetric(m: UsageMetricId) {
  const next = filter.value.metrics.includes(m)
    ? filter.value.metrics.filter(x => x !== m)
    : [...filter.value.metrics, m]
  stats.setMetrics(next)
}

function sortByMetric(m: UsageMetricId) {
  stats.setSort(m)
}

function selectChartType(c: UsageChartType) {
  stats.setChartType(c)
}

// --- Cells / cards ---
function formatMetricCardValue(m: UsageMetricId, value: number): string {
  return formatMetricValue(m, value)
}

function formatMetricCellValue(m: UsageMetricId, row: UsageRowLike): string {
  return formatMetricValue(m, rowValueOf(row as never, m))
}

// --- Chart option building ---
type UsageRowLike = { key: Partial<Record<UsageDimId, string>>; input: number; output: number; total: number; cacheHit: number; cacheMiss: number; credit: number; costUsd: number }

function chartOptionFor(m: UsageMetricId) {
  // Read themeTick (palette rebuild on theme change) and viewportTick (axis
  // label visibility when crossing the mobile/desktop boundary) so the option
  // is rebuilt when either changes.
  void themeTick.value
  void viewportTick.value
  const dims = filter.value.dims
  const rows = filteredRows.value
  const labelOf = (r: { key: Partial<Record<UsageDimId, string>> }): string =>
    dims.map(d => r.key[d] ?? '').filter(Boolean).join(' × ') || t('stats.emptyLabel')
  const categories = rows.map(r => labelOf(r))
  if (filter.value.chartType === 'pie') {
    const values = rows.map(r => rowValueOf(r, m))
    return buildPieOption(categories, values, m)
  }
  if (filter.value.chartType === 'trend') {
    const trend = rawTrend.value
    if (!trend.length) {
      return buildTrendOption([], [], m)
    }
    const days = [...new Set(trend.map(r => r.day).filter(Boolean))] as string[]
    const labels = [...new Set(trend.map(r => labelOf(r)))]
    const series = labels.map(label => ({
      label,
      values: days.map(day => {
        const row = trend.find(r => r.day === day && labelOf(r) === label)
        return row ? rowValueOf(row, m) : 0
      }),
    }))
    return buildTrendOption(days, series, m)
  }
  // bar
  const values = rows.map(r => rowValueOf(r, m))
  return buildBarOption(categories, values, m)
}

const rawTrend = computed(() => stats.raw.value?.trend ?? [])

// Rows whose every selected metric value is zero add no information and only
// waste space in the table/charts — drop them.
const filteredRows = computed(() => {
  const ms = filter.value.metrics
  return tableRows.value.filter(r => ms.some(m => rowValueOf(r, m) !== 0))
})

// The overview donut / cards render from the range totals regardless of row
// grouping; "has content" = any totals present OR rows OR trend data.
const totals = computed(() => stats.raw.value?.totals ?? null)
const totalsPresent = computed(() => {
  const t = totals.value
  if (!t) return false
  return t.input > 0 || t.output > 0 || t.total > 0 || t.cacheHit > 0 || t.cacheMiss > 0 || t.costUsd > 0
})
const hasContent = computed(() => totalsPresent.value || filteredRows.value.length > 0 || rawTrend.value.length > 0)

// Overview donut: input vs output share of the range totals. Clicking the
// input slice drills into that input's cache composition (hit vs miss).
const overviewDrill = ref<null | 'input'>(null)

// Cache hit rate, shown as an overview-only derived card (it is not an
// additive metric column). Present whenever the range totals carry cache data.
const overviewCachePresent = computed(() => {
  const t0 = totals.value
  return !!t0 && t0.cacheHit + t0.cacheMiss > 0
})
const overviewHitRateText = computed(() => {
  const t0 = totals.value
  if (!t0 || t0.cacheHit + t0.cacheMiss <= 0) return '—'
  return `${((t0.cacheHit / (t0.cacheHit + t0.cacheMiss)) * 100).toFixed(1)}%`
})

const overviewDonutOption = computed(() => {
  void themeTick.value
  const tt = totals.value
  return buildOverviewDonut(
    tt?.input ?? 0,
    tt?.output ?? 0,
    t('stats.colInput'),
    t('stats.colOutput'),
  )
})
const cacheDonutOption = computed(() => {
  void themeTick.value
  const tt = totals.value
  return buildCacheDonut(
    tt?.cacheHit ?? 0,
    tt?.cacheMiss ?? 0,
    t('stats.cacheHitShort'),
    t('stats.cacheMissShort'),
  )
})
function onOverviewSliceClick(params: Record<string, unknown>) {
  // ECharts pie click params: name is the slice's legend label. Only drill when
  // a series slice (not the legend) is clicked.
  if (overviewDrill.value !== null) return // already drilled
  if (params.componentType && params.componentType !== 'series') return
  if (params.name === t('stats.colInput')) {
    overviewDrill.value = 'input'
  }
}

const errorText = computed(() => {
  if (!error.value) return ''
  // The server sends a localized message in Error.message plus a msgKey as a
  // discriminator. The msgKey only exists in the backend locale YAML, so
  // prefer the already-localized message text.
  if (error.value.message) return error.value.message
  return error.value.status ? `${t('stats.loadFailed')} (${error.value.status})` : t('stats.loadFailed')
})

// --- Load triggers ---
let projectRoot = store.state.projectRoot
watch(() => store.state.projectRoot, (p) => {
  if (p !== projectRoot && props.active) {
    projectRoot = p
    void stats.loadStats()
  }
})

watch(() => props.active, (nowActive, was) => {
  if (nowActive && !was) {
    void stats.loadStats()
  }
})

onMounted(() => {
  // TabPanel mounts this component the first time the stats tab is opened — at
  // that point props.active is already true, so the props.active watch never
  // fires. Fetch on mount so the very first open always loads data.
  if (props.active) {
    void stats.loadStats()
  }
})

function onRefresh() {
  void stats.loadStats()
}
</script>

<style scoped>
.usage-stats-panel {
  height: 100%;
  display: flex;
  flex-direction: column;
  overflow: hidden;
  background: var(--bg-primary, #fff);
}

/* ── Compact header (36px, matches settings/task panels) ── */
.stats-header {
  display: flex;
  align-items: center;
  gap: 6px;
  height: var(--header-height, 36px);
  padding: 0 4px 0 12px;
  border-bottom: 1px solid var(--border-color);
  flex-shrink: 0;
  background: var(--bg-primary);
}
.stats-header-icon {
  color: var(--accent-color);
  flex-shrink: 0;
}
.stats-header-title {
  flex: 1;
  min-width: 0;
  font-size: 15px;
  font-weight: 600;
  color: var(--text-primary);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
.stats-header-actions {
  display: flex;
  align-items: center;
  gap: 4px;
  flex-shrink: 0;
}
/* Round header icon button — same family as .header-btn in other panels */
.stats-refresh {
  width: 28px;
  height: 28px;
  border: none;
  border-radius: 14px;
  background: transparent;
  color: var(--text-secondary);
  cursor: pointer;
  display: flex;
  align-items: center;
  justify-content: center;
  flex-shrink: 0;
}
@media (hover: hover) {
  .stats-refresh:hover {
    background: var(--bg-tertiary);
    color: var(--accent-color);
  }
}

.stats-body {
  flex: 1;
  overflow-y: auto;
  padding: 10px;
  display: flex;
  flex-direction: column;
  gap: 10px;
}

/* ── Outlined grouping card (aligns with overview-card) ── */
.stats-card-panel {
  background: var(--bg-secondary, #f8f9fa);
  border: 1px solid var(--border-color, #e5e5e5);
  border-radius: var(--radius-sm, 6px);
  padding: 10px;
  display: flex;
  flex-direction: column;
  gap: 8px;
}
.stats-card-title {
  display: flex;
  align-items: center;
  gap: 6px;
  font-size: 13px;
  font-weight: 600;
  color: var(--text-primary);
  margin: 0;
}
.stats-card-title-icon {
  color: var(--text-secondary);
  flex-shrink: 0;
}
.stats-count-chip {
  margin-left: auto;
  font-size: 10px;
  font-weight: 500;
  color: var(--text-secondary);
  background: var(--bg-tertiary);
  border-radius: 10px;
  padding: 1px 7px;
  line-height: 16px;
}

/* ── Chips / toggles ──
   Each labelled group keeps a fixed label; the chips wrap onto further lines
   when the row does not fit (no horizontal scrolling). */
.stats-filter-group {
  display: flex;
  align-items: flex-start;
  gap: 8px;
  min-width: 0;
}
.stats-filter-label {
  font-size: 12px;
  color: var(--text-secondary);
  flex-shrink: 0;
  min-width: 4em;
  line-height: 26px;
}
.stats-chip-scroll {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 6px;
  min-width: 0;
  flex: 1;
}
.stats-chip {
  flex-shrink: 0;
  border: 1px solid var(--border-color);
  background: var(--bg-elevated, var(--bg-primary));
  color: var(--text-secondary);
  border-radius: 999px;
  padding: 3px 11px;
  font-size: 12px;
  line-height: 18px;
  cursor: pointer;
  transition: background 0.15s, color 0.15s, border-color 0.15s;
  -webkit-tap-highlight-color: transparent;
}
@media (hover: hover) {
  .stats-chip:hover {
    border-color: var(--accent-color);
    color: var(--text-primary);
  }
}
.stats-chip.active {
  background: var(--accent-color, #4f8cff);
  border-color: var(--accent-color, #4f8cff);
  color: #fff;
}
.stats-date-input {
  flex-shrink: 0;
  background: var(--bg-elevated, var(--bg-primary));
  color: var(--text-primary);
  border: 1px solid var(--border-color);
  border-radius: var(--radius-sm, 6px);
  padding: 2px 6px;
  font-size: 12px;
}
.stats-date-sep {
  flex-shrink: 0;
  color: var(--text-muted);
}

/* ── Totals overview ── */
.stats-summary {
  display: flex;
  align-items: stretch;
  gap: 12px;
  min-width: 0;
}
/* Narrow container: stack vertically — donut on top (full width), value cards
   below. */
.stats-summary.is-stacked {
  flex-direction: column;
}
.stats-donut-col {
  flex: 0 0 190px;
  display: flex;
  flex-direction: column;
  gap: 2px;
  min-width: 0;
}
.stats-summary.is-stacked .stats-donut-col {
  flex: none;
  width: 100%;
}
.stats-summary.is-stacked .stats-donut {
  height: 200px;
}
.stats-summary.is-stacked .stats-totals {
  flex: none;
  width: 100%;
}
.stats-donut-title {
  font-size: 11px;
  color: var(--text-secondary);
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 6px;
  min-height: 20px;
}
.stats-donut-back {
  border: none;
  background: var(--bg-tertiary);
  color: var(--text-secondary);
  border-radius: 10px;
  padding: 0 8px;
  font-size: 11px;
  line-height: 18px;
  cursor: pointer;
  flex-shrink: 0;
}
.stats-donut {
  width: 100%;
  height: 170px;
}
.stats-totals {
  flex: 1;
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(110px, 1fr));
  gap: 8px;
  min-width: 0;
  align-content: start;
}
.stats-total {
  background: var(--bg-elevated, var(--bg-primary));
  border: 1px solid var(--border-color);
  border-radius: var(--radius-sm, 6px);
  padding: 8px 10px;
  display: flex;
  flex-direction: column;
  gap: 2px;
  min-width: 0;
}
.stats-total-label {
  font-size: 11px;
  color: var(--text-secondary);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
.stats-total-value {
  font-size: 16px;
  font-weight: 600;
  color: var(--text-primary);
  font-variant-numeric: tabular-nums;
  word-break: break-all;
}

/* ── Error / loading / empty ── */
.stats-error {
  padding: 9px 12px;
  border-radius: var(--radius-sm, 6px);
  background: color-mix(in srgb, var(--color-red, #ef4444) 12%, transparent);
  border: 1px solid color-mix(in srgb, var(--color-red, #ef4444) 30%, transparent);
  color: var(--color-red, #ef4444);
  font-size: 13px;
}
.stats-loading,
.stats-empty {
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 10px;
  padding: 48px 0;
  color: var(--text-muted, #999);
  font-size: 13px;
}
.stats-empty-icon {
  opacity: 0.5;
}

/* ── Detail table ── */
.stats-table-wrap {
  overflow-x: auto;
  margin: 0 -10px;
  padding: 0 10px;
}
.stats-table {
  width: 100%;
  border-collapse: collapse;
  font-size: 12px;
}
.stats-table th,
.stats-table td {
  padding: 7px 8px;
  border-bottom: 1px solid var(--border-color);
  text-align: right;
  white-space: nowrap;
}
.stats-table .stats-th-dim,
.stats-table .stats-td-dim {
  text-align: left;
}
.stats-table th {
  color: var(--text-secondary);
  font-weight: 500;
  background: var(--bg-secondary, var(--bg-primary));
  cursor: pointer;
  user-select: none;
}
.stats-table tbody tr:last-child td {
  border-bottom: none;
}
@media (hover: hover) {
  .stats-table tbody tr:hover td {
    background: color-mix(in srgb, var(--bg-tertiary) 55%, transparent);
  }
}
.stats-th-inner {
  display: inline-flex;
  align-items: center;
  gap: 3px;
}
.stats-sort-arrow {
  font-size: 11px;
}
.stats-th-num.sorted {
  color: var(--accent-color, #4f8cff);
}
.stats-td-num {
  color: var(--text-primary);
  font-variant-numeric: tabular-nums;
}

/* ── Charts ── */
.stats-chart-panel {
  background: var(--bg-primary);
}
.stats-chart {
  width: 100%;
  height: 260px;
}
</style>
