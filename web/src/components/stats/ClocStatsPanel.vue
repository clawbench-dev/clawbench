<template>
  <!-- Code-inventory (cloc) panel: a snapshot of the current working tree,
       independent of the git-history range and of whether the project is even
       a git repository. Root visibility driven by StatsTabHost through active. -->
  <div v-show="active" class="cloc-stats-panel">
    <div class="cloc-body">
      <section class="stats-card-panel">
        <div class="stats-card-title">
          <Files :size="13" class="stats-card-title-icon" />
          <span>{{ t('gitStats.clocTitle') }}</span>
        </div>

        <div v-if="clocError" class="stats-error">{{ clocErrorText }}</div>
        <div v-else-if="!clocLoaded" class="stats-loading">
          <LoadingIndicator size="sm" inline />
          <span>{{ t('common.loading') }}</span>
        </div>
        <div v-else-if="clocLanguages.length === 0" class="stats-empty">
          <FileCode2 :size="30" class="stats-empty-icon" />
          <span>{{ t('gitStats.clocNoData') }}</span>
        </div>
        <template v-else>
          <div class="cloc-totals">
            <div class="stats-total">
              <span class="stats-total-label">{{ t('gitStats.clocColCode') }}</span>
              <span class="stats-total-value">{{ formatLineCount(clocTotal.code) }}</span>
            </div>
            <div class="stats-total">
              <span class="stats-total-label">{{ t('gitStats.clocColComment') }}</span>
              <span class="stats-total-value">{{ formatLineCount(clocTotal.comment) }}</span>
            </div>
            <div class="stats-total">
              <span class="stats-total-label">{{ t('gitStats.clocColBlank') }}</span>
              <span class="stats-total-value">{{ formatLineCount(clocTotal.blank) }}</span>
            </div>
            <div class="stats-total">
              <span class="stats-total-label">{{ t('gitStats.clocColFiles') }}</span>
              <span class="stats-total-value">{{ clocTotal.files.toLocaleString() }}</span>
            </div>
          </div>

          <UsageChart :option="clocBarOption" class="cloc-chart" />

          <div class="stats-table-wrap">
            <table class="stats-table">
              <thead>
                <tr>
                  <th class="stats-th-dim stats-th-sortable" @click="cycleSort('name')">
                    <span class="stats-th-inner">
                      {{ t('gitStats.clocColLang') }}
                      <span v-if="sortCol === 'name'" class="stats-sort-arrow">{{ sortDir === 'asc' ? '↑' : '↓' }}</span>
                    </span>
                  </th>
                  <th class="stats-th-num stats-th-sortable" @click="cycleSort('files')">
                    <span class="stats-th-inner">
                      {{ t('gitStats.clocColFiles') }}
                      <span v-if="sortCol === 'files'" class="stats-sort-arrow">{{ sortDir === 'asc' ? '↑' : '↓' }}</span>
                    </span>
                  </th>
                  <th class="stats-th-num stats-th-sortable" @click="cycleSort('code')">
                    <span class="stats-th-inner">
                      {{ t('gitStats.clocColCode') }}
                      <span v-if="sortCol === 'code'" class="stats-sort-arrow">{{ sortDir === 'asc' ? '↑' : '↓' }}</span>
                    </span>
                  </th>
                  <th class="stats-th-num stats-th-sortable" @click="cycleSort('comment')">
                    <span class="stats-th-inner">
                      {{ t('gitStats.clocColComment') }}
                      <span v-if="sortCol === 'comment'" class="stats-sort-arrow">{{ sortDir === 'asc' ? '↑' : '↓' }}</span>
                    </span>
                  </th>
                  <th class="stats-th-num stats-th-sortable" @click="cycleSort('blank')">
                    <span class="stats-th-inner">
                      {{ t('gitStats.clocColBlank') }}
                      <span v-if="sortCol === 'blank'" class="stats-sort-arrow">{{ sortDir === 'asc' ? '↑' : '↓' }}</span>
                    </span>
                  </th>
                </tr>
              </thead>
              <tbody>
                <tr v-for="row in sortedRows" :key="row.name">
                  <td class="stats-td-dim">{{ row.name }}</td>
                  <td class="stats-td-num">{{ row.files.toLocaleString() }}</td>
                  <td class="stats-td-num">{{ formatLineCount(row.code) }}</td>
                  <td class="stats-td-num">{{ formatLineCount(row.comment) }}</td>
                  <td class="stats-td-num">{{ formatLineCount(row.blank) }}</td>
                </tr>
                <tr>
                  <td class="stats-td-dim"><strong>{{ t('gitStats.clocTotalLabel') }}</strong></td>
                  <td class="stats-td-num"><strong>{{ clocTotal.files.toLocaleString() }}</strong></td>
                  <td class="stats-td-num"><strong>{{ formatLineCount(clocTotal.code) }}</strong></td>
                  <td class="stats-td-num"><strong>{{ formatLineCount(clocTotal.comment) }}</strong></td>
                  <td class="stats-td-num"><strong>{{ formatLineCount(clocTotal.blank) }}</strong></td>
                </tr>
              </tbody>
            </table>
          </div>
        </template>
      </section>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, watch, onMounted, onBeforeUnmount } from 'vue'
import { useI18n } from 'vue-i18n'
import { Files, FileCode2 } from 'lucide-vue-next'
import LoadingIndicator from '@/components/common/LoadingIndicator.vue'
import UsageChart from '@/components/stats/UsageChart.vue'
import { useGitCodeStats } from '@/composables/useGitCodeStats'
import type { ClocLanguageRow } from '@/composables/useGitCodeStats'
import { formatLineCount } from '@/components/stats/gitStatsFormat'
import { buildClocBarOption } from '@/components/stats/gitStatsChart'
import { store } from '@/stores/app'

const props = defineProps<{
  active: boolean
}>()

const { t } = useI18n()
const stats = useGitCodeStats()

const clocRaw = stats.clocRaw
const clocError = stats.clocError

const clocLoaded = computed(() => clocRaw.value !== null)
const clocLanguages = computed<ClocLanguageRow[]>(() => clocRaw.value?.languages ?? [])
const clocTotal = computed<ClocLanguageRow>(() => clocRaw.value?.total ?? { name: '', files: 0, code: 0, comment: 0, blank: 0 })

const clocErrorText = computed(() => {
  if (!clocError.value) return ''
  if (clocError.value.message) return clocError.value.message
  return clocError.value.status ? `${t('gitStats.clocLoadFailed')} (${clocError.value.status})` : t('gitStats.clocLoadFailed')
})

// Theme tick — chart option is rebuilt from CSS palette vars on theme change.
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

const clocBarOption = computed(() => {
  void themeTick.value
  return buildClocBarOption(clocLanguages.value)
})

// --- Table sorting ---
type ClocSortCol = 'name' | 'files' | 'code' | 'comment' | 'blank'
const sortCol = ref<ClocSortCol>('code')
const sortDir = ref<'asc' | 'desc'>('desc')

const sortedRows = computed<ClocLanguageRow[]>(() => {
  const rows = [...clocLanguages.value]
  const col = sortCol.value
  rows.sort((a, b) => {
    const cmp = col === 'name'
      ? a.name.localeCompare(b.name)
      : a[col] - b[col]
    return sortDir.value === 'asc' ? cmp : -cmp
  })
  return rows
})

function cycleSort(col: ClocSortCol) {
  if (sortCol.value === col) {
    sortDir.value = sortDir.value === 'asc' ? 'desc' : 'asc'
  } else {
    sortCol.value = col
    sortDir.value = col === 'name' ? 'asc' : 'desc'
  }
}

// --- Load triggers ---
let projectRoot = store.state.projectRoot
watch(() => store.state.projectRoot, (p) => {
  if (p !== projectRoot && props.active) {
    projectRoot = p
    void stats.loadCloc()
  }
})

watch(() => props.active, (nowActive, was) => {
  if (nowActive && !was) {
    void stats.loadCloc()
  }
})

onMounted(() => {
  if (props.active) {
    void stats.loadCloc()
  }
})
</script>

<style scoped>
.cloc-stats-panel {
  height: 100%;
  display: flex;
  flex-direction: column;
  overflow: hidden;
  background: var(--bg-primary, #fff);
}

/* ── Panel body scrolls under the host's tab bar ── */
.cloc-body {
  flex: 1;
  overflow-y: auto;
  padding: 10px;
  display: flex;
  flex-direction: column;
  gap: 10px;
  min-height: 0;
}

/* ── Shared card/chip/table vocabulary — mirror of the git panel ── */
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

.cloc-totals {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(100px, 1fr));
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

.cloc-chart {
  width: 100%;
  height: 260px;
}

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
}
.stats-table tbody tr:last-child td {
  border-bottom: none;
}
.stats-th-inner {
  display: inline-flex;
  align-items: center;
  gap: 3px;
}
.stats-th-sortable {
  cursor: pointer;
  user-select: none;
}
.stats-sort-arrow {
  font-size: 11px;
}
.stats-td-num {
  color: var(--text-primary);
  font-variant-numeric: tabular-nums;
}
</style>
