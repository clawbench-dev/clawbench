<template>
  <div class="usage-stats-panel" v-show="active">
    <!-- Header: title + refresh -->
    <div class="stats-header">
      <span class="stats-header-title">{{ t('nav.stats') }}</span>
      <RefreshButton class="stats-refresh" :loading="loading" :disabled="loading" :title="t('nav.refresh')" @click="onRefresh" />
    </div>

    <div class="stats-scroll">
      <!-- Time range -->
      <div class="stats-section stats-range-row">
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
          <input v-model="customStart" type="date" class="stats-date-input" @change="applyCustomRange" />
          <span class="stats-date-sep">→</span>
          <input v-model="customEnd" type="date" class="stats-date-input" @change="applyCustomRange" />
        </template>
      </div>

      <!-- Totals cards -->
      <div v-if="visibleTotals.length > 0" class="stats-section stats-cards">
        <div v-for="card in visibleTotals" :key="card.metric" class="stats-card">
          <span class="stats-card-label">{{ t(metricLabelKey(card.metric)) }}</span>
          <span class="stats-card-value">{{ formatMetricCardValue(card.metric, card.value) }}</span>
        </div>
      </div>

      <!-- Dims -->
      <div class="stats-section stats-filter-row">
        <span class="stats-filter-label">{{ t('stats.dimTitle') }}</span>
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

      <!-- Metrics (columns) -->
      <div class="stats-section stats-filter-row">
        <span class="stats-filter-label">{{ t('stats.metricTitle') }}</span>
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

      <!-- Chart type -->
      <div class="stats-section stats-filter-row">
        <span class="stats-filter-label">{{ t('stats.chartTitle') }}</span>
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

      <!-- Error -->
      <div v-if="error" class="stats-error">{{ errorText }}</div>

      <!-- Loading -->
      <div v-if="loading && !tableRows.length" class="stats-loading">
        <LoadingIndicator size="sm" inline />
        <span>{{ t('common.loading') }}</span>
      </div>

      <template v-else>
        <!-- In trend mode the backend returns trend series (no grouped rows), so
             "has content" = rows exist OR trend data exists. -->
        <div
          v-if="tableRows.length === 0 && rawTrend.length === 0 && !error"
          class="stats-empty"
        >{{ t('stats.noData') }}</div>

        <template v-else-if="tableRows.length > 0 || rawTrend.length > 0">
          <!-- Table: only in non-trend grouping (rows present) -->
          <div v-if="tableRows.length > 0" class="stats-section stats-table-wrap">
            <table class="stats-table">
              <thead>
                <tr>
                  <th
                    v-for="d in filter.dims"
                    :key="d"
                    class="stats-th-dim"
                  >
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
                <tr v-for="(row, i) in tableRows" :key="i">
                  <td v-for="d in filter.dims" :key="d" class="stats-td-dim">
                    {{ row.key[d] === '(empty)' ? t('stats.emptyLabel') : (row.key[d] || t('stats.emptyLabel')) }}
                  </td>
                  <td
                    v-for="m in filter.metrics"
                    :key="m"
                    class="stats-td-num"
                  >
                    {{ formatMetricCellValue(m, row) }}
                  </td>
                </tr>
              </tbody>
            </table>
          </div>

          <!-- Charts: one per selected metric (bar/pie from rows, trend from trend) -->
          <div class="stats-section stats-charts">
            <div v-for="m in filter.metrics" :key="m" class="stats-chart-block">
              <div class="stats-chart-title">{{ t(metricLabelKey(m)) }}</div>
              <UsageChart :option="chartOptionFor(m)" class="stats-chart" />
            </div>
          </div>
        </template>
      </template>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, watch, onMounted } from 'vue'
import { useI18n } from 'vue-i18n'
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
  formatMetricValue,
  rowValueOf,
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
    case 'hitRate': return 'stats.colHitRate'
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
  const dims = filter.value.dims
  const rows = tableRows.value
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
}
.stats-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 10px 12px 6px;
  flex-shrink: 0;
}
.stats-header-title {
  font-size: 15px;
  font-weight: 600;
  color: var(--text-primary);
}
.stats-refresh {
  color: var(--text-secondary);
}
.stats-scroll {
  flex: 1;
  overflow-y: auto;
  padding: 0 12px 24px;
}
.stats-section {
  margin-top: 12px;
}
.stats-range-row {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 6px;
}
.stats-chip {
  border: 1px solid var(--border-color);
  background: var(--bg-elevated, var(--bg-secondary));
  color: var(--text-secondary);
  border-radius: 999px;
  padding: 4px 12px;
  font-size: 12px;
  cursor: pointer;
  transition: background 0.15s, color 0.15s;
}
.stats-chip.active {
  background: var(--accent-color, #4f8cff);
  border-color: var(--accent-color, #4f8cff);
  color: #fff;
}
.stats-date-input {
  background: var(--bg-elevated, var(--bg-secondary));
  color: var(--text-primary);
  border: 1px solid var(--border-color);
  border-radius: 6px;
  padding: 3px 6px;
  font-size: 12px;
}
.stats-date-sep {
  color: var(--text-secondary);
}
.stats-cards {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
}
.stats-card {
  flex: 1 1 auto;
  min-width: 110px;
  display: flex;
  flex-direction: column;
  gap: 2px;
  padding: 8px 10px;
  border: 1px solid var(--border-color);
  border-radius: 8px;
  background: var(--bg-elevated, var(--bg-secondary));
}
.stats-card-label {
  font-size: 11px;
  color: var(--text-secondary);
}
.stats-card-value {
  font-size: 16px;
  font-weight: 600;
  color: var(--text-primary);
  word-break: break-all;
}
.stats-filter-row {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 6px;
}
.stats-filter-label {
  font-size: 12px;
  color: var(--text-secondary);
  margin-right: 2px;
  flex-shrink: 0;
}
.stats-error {
  margin-top: 12px;
  padding: 10px 12px;
  border-radius: 8px;
  background: color-mix(in srgb, var(--color-red, #ef4444) 12%, transparent);
  color: var(--color-red, #ef4444);
  font-size: 13px;
}
.stats-loading,
.stats-empty {
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 8px;
  padding: 40px 0;
  color: var(--text-secondary);
  font-size: 13px;
}
.stats-table-wrap {
  overflow-x: auto;
}
.stats-table {
  width: 100%;
  border-collapse: collapse;
  font-size: 12px;
}
.stats-table th,
.stats-table td {
  padding: 6px 8px;
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
  position: sticky;
  top: 0;
  background: var(--bg-secondary, var(--bg-primary));
  cursor: pointer;
  user-select: none;
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
.stats-charts {
  display: flex;
  flex-direction: column;
  gap: 16px;
}
.stats-chart-block {
  border: 1px solid var(--border-color);
  border-radius: 10px;
  padding: 8px 8px 4px;
  background: var(--bg-elevated, var(--bg-secondary));
}
.stats-chart-title {
  font-size: 12px;
  color: var(--text-secondary);
  padding: 2px 6px 6px;
}
.stats-chart {
  width: 100%;
  height: 260px;
}
</style>
