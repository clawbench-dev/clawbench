<template>
  <!-- Root visibility is driven by the host (StatsTabHost) through `active`:
       without v-show this absolutely-positioned pane would stay rendered on
       top of the sibling usage panel and block switching back to it. -->
  <div v-show="active" class="git-code-stats-panel">
    <div class="git-body">
      <!-- Range card -->
      <section class="stats-card-panel">
        <div class="stats-card-title">
          <Clock :size="13" class="stats-card-title-icon" />
          <span>{{ t('gitStats.rangeTitle') }}</span>
        </div>
        <div class="stats-chip-scroll">
          <button
            v-for="r in rangePresets"
            :key="r.key"
            class="stats-chip"
            :class="{ active: range.rangeKey === r.key }"
            @click="selectRangePreset(r.key)"
          >
            {{ t(r.labelKey) }}
          </button>
          <button class="stats-chip" :class="{ active: range.rangeKey === 'custom' }" @click="selectRangePreset('custom')">
            {{ t('gitStats.custom') }}
          </button>
          <template v-if="range.rangeKey === 'custom'">
            <span class="stats-date-sep">·</span>
            <input v-model="customStart" type="date" class="stats-date-input" @change="applyCustomRange" />
            <span class="stats-date-sep">→</span>
            <input v-model="customEnd" type="date" class="stats-date-input" @change="applyCustomRange" />
          </template>
        </div>
      </section>

      <!-- Error banner -->
      <div v-if="error" class="stats-error">{{ errorText }}</div>

      <!-- First load spinner (raw not populated yet) -->
      <div v-else-if="!rawLoaded" class="stats-loading">
        <LoadingIndicator size="sm" inline />
        <span>{{ t('common.loading') }}</span>
      </div>

      <!-- Not a git repository -->
      <div v-else-if="!isGit" class="stats-empty">
        <GitBranch :size="34" class="stats-empty-icon" />
        <span>{{ t('gitStats.notGitRepo') }}</span>
      </div>

      <template v-else>
        <!-- Empty state: git repo but no commits in range -->
        <div v-if="!totalsPresent" class="stats-empty">
          <Code2 :size="34" class="stats-empty-icon" />
          <span>{{ t('gitStats.noData') }}</span>
        </div>

        <template v-else>
          <!-- Totals overview -->
          <section class="stats-card-panel">
            <div class="stats-card-title">
              <Gauge :size="13" class="stats-card-title-icon" />
              <span>{{ t('gitStats.summaryTitle') }}</span>
            </div>
            <div class="git-totals">
              <div v-for="card in visibleTotals" :key="card.metric" class="stats-total">
                <span class="stats-total-label">{{ t(gitMetricLabelKey(card.metric)) }}</span>
                <span class="stats-total-value" :class="metricValueClass(card.metric, card.value)">
                  {{ card.metric === 'net' ? formatNetDelta(card.value) : formatMetricCard(card.metric, card.value) }}
                </span>
              </div>
            </div>
          </section>

          <!-- Per-author detail table -->
          <section class="stats-card-panel">
            <div class="stats-card-title">
              <Table :size="13" class="stats-card-title-icon" />
              <span>{{ t('gitStats.authorTitle') }}</span>
              <span class="stats-count-chip">{{ sortedRows.length }}</span>
            </div>
            <div class="stats-table-wrap">
              <table class="stats-table">
                <thead>
                  <tr>
                    <th class="stats-th-dim stats-th-sortable" @click="cycleSort('author')">
                      <span class="stats-th-inner">
                        {{ t('gitStats.colAuthor') }}
                        <span v-if="sortBy === 'author'" class="stats-sort-arrow">{{ sortDir === 'asc' ? '↑' : '↓' }}</span>
                      </span>
                    </th>
                    <th class="stats-th-num stats-th-sortable" @click="cycleSort('added')">
                      <span class="stats-th-inner">
                        {{ t('gitStats.colAdded') }}
                        <span v-if="sortBy === 'added'" class="stats-sort-arrow">{{ sortDir === 'asc' ? '↑' : '↓' }}</span>
                      </span>
                    </th>
                    <th class="stats-th-num stats-th-sortable" @click="cycleSort('deleted')">
                      <span class="stats-th-inner">
                        {{ t('gitStats.colDeleted') }}
                        <span v-if="sortBy === 'deleted'" class="stats-sort-arrow">{{ sortDir === 'asc' ? '↑' : '↓' }}</span>
                      </span>
                    </th>
                    <th class="stats-th-num stats-th-sortable" @click="cycleSort('net')">
                      <span class="stats-th-inner">
                        {{ t('gitStats.colNet') }}
                        <span v-if="sortBy === 'net'" class="stats-sort-arrow">{{ sortDir === 'asc' ? '↑' : '↓' }}</span>
                      </span>
                    </th>
                    <th class="stats-th-num stats-th-sortable" @click="cycleSort('commitCnt')">
                      <span class="stats-th-inner">
                        {{ t('gitStats.colCommits') }}
                        <span v-if="sortBy === 'commitCnt'" class="stats-sort-arrow">{{ sortDir === 'asc' ? '↑' : '↓' }}</span>
                      </span>
                    </th>
                  </tr>
                </thead>
                <tbody>
                  <tr v-for="(row, i) in sortedRows" :key="i">
                    <td class="stats-td-dim">{{ row.author || '—' }}</td>
                    <td class="stats-td-num">{{ formatLineCount(row.added) }}</td>
                    <td class="stats-td-num">{{ formatLineCount(row.deleted) }}</td>
                    <td class="stats-td-num" :class="metricValueClass('net', row.net)">{{ formatNetDelta(row.net) }}</td>
                    <td class="stats-td-num">{{ row.commitCnt.toLocaleString() }}</td>
                  </tr>
                </tbody>
              </table>
            </div>
          </section>

          <!-- Per-day trend chart -->
          <section class="stats-card-panel git-chart-panel">
            <div class="stats-card-title">
              <ChartLine :size="13" class="stats-card-title-icon" />
              <span>{{ t('gitStats.trendTitle') }}</span>
            </div>
            <!-- Author scope: multi-select chips. Empty selection = all
                 authors summed; checking authors narrows to their union. -->
            <div v-if="authorList.length > 1" class="stats-chip-scroll">
              <button
                class="stats-chip"
                :class="{ active: selectedAuthors.length === 0 }"
                @click="clearAuthors"
              >
                {{ t('gitStats.allAuthors') }}
              </button>
              <button
                v-for="a in authorList"
                :key="a"
                class="stats-chip"
                :class="{ active: selectedAuthors.includes(a) }"
                @click="toggleAuthor(a)"
              >
                {{ a }}
              </button>
            </div>
            <UsageChart :option="trendOption" class="git-chart" />
          </section>
        </template>
      </template>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, watch, onMounted, onBeforeUnmount } from 'vue'
import { useI18n } from 'vue-i18n'
import { Clock, Gauge, Table, ChartLine, GitBranch, Code2 } from 'lucide-vue-next'
import LoadingIndicator from '@/components/common/LoadingIndicator.vue'
import UsageChart from '@/components/stats/UsageChart.vue'
import { useGitCodeStats } from '@/composables/useGitCodeStats'
import type { UsageRangeKey } from '@/composables/useUsageStats'
import { formatLineCount, formatNetDelta } from '@/components/stats/gitStatsFormat'
import { buildGitTrendOption } from '@/components/stats/gitStatsChart'
import { store } from '@/stores/app'

const props = defineProps<{
  active: boolean
}>()

const { t } = useI18n()
const stats = useGitCodeStats()
const range = stats.range
const { raw, error, visibleTotals, tableRows, trend } = stats

const rangePresets: { key: UsageRangeKey; labelKey: string }[] = [
  { key: '24h', labelKey: 'gitStats.range24h' },
  { key: '7d', labelKey: 'gitStats.range7d' },
  { key: '30d', labelKey: 'gitStats.range30d' },
]

const customStart = ref<string>('')
const customEnd = ref<string>('')

// Sorting is client-side over the author rows (rows are few). Every column is
// sortable; clicking a new column picks it (numeric desc, author asc), clicking
// the same column toggles the direction.
type AuthorSortCol = 'author' | 'added' | 'deleted' | 'net' | 'commitCnt'
const sortBy = ref<AuthorSortCol>('added')
const sortDir = ref<'asc' | 'desc'>('desc')

// Trend chart author scope — multi-select. An empty selection means "all
// authors" (the project-wide sum); checking authors narrows the daily lines to
// the union of the selected authors.
const selectedAuthors = ref<string[]>([])

const authorList = computed(() => {
  const authors = [...new Set(tableRows.value.map(r => r.author))]
    .filter(Boolean)
    .sort((a, b) => a.localeCompare(b))
  // Keep a selected author in the list even if the current range no longer has
  // commits from them (so the chip can be deselected to restore the view).
  for (const a of selectedAuthors.value) {
    if (!authors.includes(a)) authors.push(a)
  }
  return authors
})

function toggleAuthor(author: string) {
  selectedAuthors.value = selectedAuthors.value.includes(author)
    ? selectedAuthors.value.filter(a => a !== author)
    : [...selectedAuthors.value, author]
}

function clearAuthors() {
  selectedAuthors.value = []
}

const isGit = computed(() => raw.value?.isGit === true)
const rawLoaded = computed(() => raw.value !== null)
const totalsPresent = computed(() => {
  const t0 = raw.value?.totals
  return !!t0 && (t0.added > 0 || t0.deleted > 0 || t0.commitCnt > 0 || t0.net !== 0)
})

const sortedRows = computed(() => {
  const col = sortBy.value
  const dir = sortDir.value
  return [...tableRows.value].sort((a, b) => {
    let cmp: number
    if (col === 'author') {
      cmp = a.author.localeCompare(b.author)
    } else {
      cmp = a[col] - b[col]
    }
    if (dir === 'desc' && col !== 'author') return -cmp
    return cmp
  })
})

function cycleSort(col: AuthorSortCol) {
  if (sortBy.value === col) {
    // Numeric columns default desc, author asc — same column toggles.
    sortDir.value = sortDir.value === 'asc' ? 'desc' : 'asc'
  } else {
    sortBy.value = col
    sortDir.value = col === 'author' ? 'asc' : 'desc'
  }
}

function gitMetricLabelKey(m: string): string {
  switch (m) {
    case 'added': return 'gitStats.colAdded'
    case 'deleted': return 'gitStats.colDeleted'
    case 'net': return 'gitStats.colNet'
    default: return 'gitStats.colCommits'
  }
}

function formatMetricCard(metric: string, value: number): string {
  if (metric === 'commitCnt') return value.toLocaleString()
  return formatLineCount(value)
}

function metricValueClass(metric: string, value: number): string {
  if (metric !== 'net') return ''
  if (value > 0) return 'git-val-pos'
  if (value < 0) return 'git-val-neg'
  return ''
}

// Theme tick — the git trend chart option is rebuilt from CSS palette vars, so
// it must be recomputed on theme change (same pattern as UsageStatsPanel).
const themeTick = ref(0)
function onThemeChange() {
  themeTick.value++
}

onMounted(() => {
  window.addEventListener('clawbench-theme-change', onThemeChange)
})
onBeforeUnmount(() => {
  window.removeEventListener('clawbench-theme-change', onThemeChange)
})

const trendOption = computed(() => {
  void themeTick.value
  // Backend trend rows are (day × author). Empty selection folds across every
  // author per day (project-wide); otherwise only the selected authors count.
  const selected = selectedAuthors.value
  const rows = selected.length === 0
    ? trend.value
    : trend.value.filter(r => selected.includes(r.author))
  const days = [...new Set(rows.map(r => r.day).filter(Boolean))] as string[]
  const added = days.map(day => rows.filter(r => r.day === day).reduce((s, r) => s + r.added, 0))
  const deleted = days.map(day => rows.filter(r => r.day === day).reduce((s, r) => s + r.deleted, 0))
  const net = added.map((a, i) => a - deleted[i])
  return buildGitTrendOption(days, added, deleted, t('gitStats.colAdded'), t('gitStats.colDeleted'), net, t('gitStats.colNet'))
})

// --- Range ---
function selectRangePreset(key: UsageRangeKey) {
  if (key === 'custom') {
    range.value.rangeKey = 'custom'
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

const errorText = computed(() => {
  if (!error.value) return ''
  if (error.value.message) return error.value.message
  return error.value.status ? `${t('gitStats.loadFailed')} (${error.value.status})` : t('gitStats.loadFailed')
})

// --- Load triggers ---
let projectRoot = store.state.projectRoot
watch(() => store.state.projectRoot, (p) => {
  if (p !== projectRoot && props.active) {
    projectRoot = p
    void stats.loadGitStats()
  }
})

watch(() => props.active, (nowActive, was) => {
  if (nowActive && !was) {
    void stats.loadGitStats()
  }
})

onMounted(() => {
  if (props.active) {
    void stats.loadGitStats()
  }
})
</script>

<style scoped>
.git-code-stats-panel {
  height: 100%;
  display: flex;
  flex-direction: column;
  overflow: hidden;
  background: var(--bg-primary, #fff);
}

/* ── Panel body scrolls under the host's tab bar ── */
.git-body {
  flex: 1;
  overflow-y: auto;
  padding: 10px;
  display: flex;
  flex-direction: column;
  gap: 10px;
  min-height: 0;
}

/* ── Shared card/chip/table vocabulary — mirror of UsageStatsPanel's scoped
   rules so both panels render identically. ── */
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
.stats-chip-scroll {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 6px;
  min-width: 0;
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
.git-totals {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(120px, 1fr));
  gap: 8px;
  min-width: 0;
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
.git-val-pos {
  color: var(--color-green, #16a34a) !important;
}
.git-val-neg {
  color: var(--color-red, #ef4444) !important;
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
.stats-th-inner {
  display: inline-flex;
  align-items: center;
  gap: 3px;
}
.stats-sort-arrow {
  font-size: 11px;
}
.stats-td-num {
  color: var(--text-primary);
  font-variant-numeric: tabular-nums;
}

/* ── Chart ── */
.git-chart-panel {
  background: var(--bg-primary);
}
.git-chart {
  width: 100%;
  height: 220px;
}
</style>
