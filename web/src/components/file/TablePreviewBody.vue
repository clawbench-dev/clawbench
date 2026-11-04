<template>
  <div class="table-preview">
    <!-- Toolbar: global search / per-column search toggle / hidden columns / count -->
    <div class="table-toolbar">
      <div class="table-search">
        <Search :size="14" class="table-search-icon" />
        <input
          ref="searchInputRef"
          v-model="query"
          class="table-search-input"
          type="text"
          :placeholder="t('file.table.searchPlaceholder')"
          :aria-label="t('file.table.searchPlaceholder')"
          @keydown.esc.prevent="closeSearch"
        />
        <button
          v-if="query"
          class="table-search-clear"
          type="button"
          :title="t('file.table.clearSearch')"
          :aria-label="t('file.table.clearSearch')"
          @click="query = ''"
        >
          <X :size="12" />
        </button>
      </div>

      <button
        class="table-filter-btn"
        type="button"
        :class="{ active: filterRowOpen || activeFilterCount > 0 }"
        :title="t('file.table.filter')"
        @click="toggleFilterRow"
      >
        <Filter :size="13" />
        <span>{{ t('file.table.filter') }}</span>
        <span v-if="activeFilterCount" class="table-filter-count">{{ activeFilterCount }}</span>
      </button>

      <button
        v-if="hiddenColumns.size"
        ref="hiddenBtnRef"
        class="table-filter-btn active"
        type="button"
        :title="t('file.table.hiddenColumns')"
        @click="hiddenMenuOpen = !hiddenMenuOpen"
      >
        <EyeOff :size="13" />
        <span>{{ t('file.table.hiddenColumnsCount', { n: hiddenColumns.size }) }}</span>
      </button>

      <span class="table-summary">{{ summaryText }}</span>
    </div>

    <div v-if="notices.length" class="table-notices">
      <div v-for="notice in notices" :key="notice" class="table-notice">
        <AlertTriangle :size="13" />
        <span>{{ notice }}</span>
      </div>
    </div>

    <!-- Grid -->
    <div ref="scrollRef" class="table-scroll">
      <table ref="tableRef" class="data-table">
        <thead>
          <tr ref="headRowRef" class="head-row">
            <th
              v-for="col in visibleColumns"
              :key="col"
              class="data-th"
              :class="{
                'is-numeric': numericColumns[col],
                'is-sorted': sort?.column === col,
                'is-frozen': frozenColumn === col,
              }"
              :style="frozenStyle(col)"
              :aria-sort="ariaSort(col)"
              @click="cycleSort(col)"
            >
              <span class="th-inner">
                <span class="th-label" :title="`${headerLabel(col)} · ${t('file.table.sortHint')}`">{{ headerLabel(col) }}</span>
                <span class="th-sort" aria-hidden="true">{{ sortGlyph(col) }}</span>
                <button
                  class="th-menu-btn"
                  type="button"
                  :title="t('file.table.menu.title')"
                  :aria-label="t('file.table.menu.title')"
                  @click.stop="openColumnMenu($event, col)"
                >
                  <MoreVertical :size="13" />
                </button>
              </span>
            </th>
          </tr>
          <tr v-if="filterRowOpen" class="filter-row">
            <th
              v-for="col in visibleColumns"
              :key="col"
              class="filter-cell"
              :class="{ 'is-frozen': frozenColumn === col }"
              :style="frozenStyle(col)"
            >
              <input
                v-model="columnFilters[col]"
                class="filter-input"
                type="text"
                size="1"
                :placeholder="t('file.table.columnSearch')"
                :aria-label="`${t('file.table.columnSearch')} ${headerLabel(col)}`"
              />
            </th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="(row, ri) in pageRows" :key="pageStart + ri" class="data-row">
            <td
              v-for="col in visibleColumns"
              :key="col"
              class="data-td"
              :class="{
                'is-numeric': numericColumns[col],
                'is-frozen': frozenColumn === col,
              }"
              :style="frozenStyle(col)"
            >
              <span
                v-if="hasDataBar(col)"
                class="data-bar"
                :class="{ 'is-negative': isNegative(row[col]) }"
                :style="dataBarStyle(col, row[col])"
                aria-hidden="true"
              />
              <span class="cell-text" :title="row[col]">{{ row[col] }}</span>
            </td>
          </tr>
          <tr v-if="pageRows.length === 0">
            <td class="table-empty" :colspan="Math.max(visibleColumns.length, 1)">{{ emptyText }}</td>
          </tr>
        </tbody>
      </table>
    </div>

    <!-- Pagination -->
    <div v-if="pageCount > 1" class="table-footer">
      <button
        class="table-page-btn"
        type="button"
        :disabled="page <= 1"
        :title="t('file.table.prevPage')"
        :aria-label="t('file.table.prevPage')"
        @click="page -= 1"
      >
        <ChevronLeft :size="14" />
      </button>
      <span class="table-page-info">{{ t('file.table.pageInfo', { page, total: pageCount }) }}</span>
      <button
        class="table-page-btn"
        type="button"
        :disabled="page >= pageCount"
        :title="t('file.table.nextPage')"
        :aria-label="t('file.table.nextPage')"
        @click="page += 1"
      >
        <ChevronRight :size="14" />
      </button>
    </div>

    <!-- Column menu -->
    <TableColumnMenu
      v-if="menuColumn !== null"
      v-model:show="columnMenuOpen"
      :target-element="menuTarget"
      :numeric="numericColumns[menuColumn]"
      :sorted="sort?.column === menuColumn ? sort.direction : null"
      :frozen="frozenColumn === menuColumn"
      :data-bar="dataBars.has(menuColumn)"
      :filter-count="valueFilters.get(menuColumn)?.size ?? 0"
      :can-hide="visibleColumns.length > 1"
      @sort="applySortFromMenu"
      @open-values="openValueSheet"
      @hide="hideColumn"
      @show-only="showOnlyColumn"
      @toggle-freeze="toggleFreeze"
      @toggle-data-bar="toggleDataBar"
      @copy-column="copyColumn"
      @copy-header="copyHeader"
      @clear-filter="clearValueFilter"
      @open-stats="statsSheetOpen = true"
    />

    <!-- Restore hidden columns -->
    <PopupMenu
      v-model:show="hiddenMenuOpen"
      :target-element="hiddenBtnRef"
      :max-width="220"
      :menu-items-count="hiddenColumns.size"
    >
      <button
        v-for="col in hiddenColumnList"
        :key="col"
        class="restore-item"
        type="button"
        @click="restoreColumn(col)"
      >
        <Eye :size="14" class="restore-icon" />
        <span class="restore-label">{{ headerLabel(col) }}</span>
      </button>
      <button class="restore-item" type="button" @click="restoreAllColumns">
        <RotateCcw :size="14" class="restore-icon" />
        <span class="restore-label">{{ t('file.table.restoreAllColumns') }}</span>
      </button>
    </PopupMenu>

    <!-- Column stats -->
    <TableColumnStatsSheet
      v-if="menuColumn !== null"
      v-model:open="statsSheetOpen"
      :column="headerLabel(menuColumn)"
      :numeric="numericColumns[menuColumn]"
      :stats="menuStats"
    />

    <!-- Value filter -->
    <TableValueFilterSheet
      v-if="valueSheetColumn !== null"
      v-model:open="valueSheetOpen"
      :title="headerLabel(valueSheetColumn)"
      :values="valueSheetValues"
      :selected="valueSheetSelected"
      @update:selected="applyValueFilter"
    />
  </div>
</template>

<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import {
  AlertTriangle,
  ChevronLeft,
  ChevronRight,
  Eye,
  EyeOff,
  Filter,
  MoreVertical,
  RotateCcw,
  Search,
  X,
} from 'lucide-vue-next'
import {
  MAX_TABLE_COLUMNS,
  columnValueCounts,
  computeColumnStats,
  detectNumericColumns,
  filterAndSortRows,
  parseNumericCell,
  parseTable,
  type SortDirection,
  type SortState,
} from '@/utils/csvTable'
import { copyText } from '@/utils/clipboard'
import { useToast } from '@/composables/useToast'
import PopupMenu from '@/components/common/PopupMenu.vue'
import TableColumnMenu from './TableColumnMenu.vue'
import TableColumnStatsSheet from './TableColumnStatsSheet.vue'
import TableValueFilterSheet from './TableValueFilterSheet.vue'

const props = defineProps<{
  file: { name?: string; path?: string; content?: string | null; truncated?: boolean } | null
}>()

const { t } = useI18n()
const toast = useToast()

/** Rows per page. Large enough to browse, small enough that a 20k-row export
 *  never mounts 20k <tr> at once. */
const PAGE_SIZE = 100

const parsed = computed(() => parseTable(props.file?.content ?? '', props.file?.name ?? ''))
const numericColumns = computed(() => detectNumericColumns(parsed.value.rows, parsed.value.columnCount))

function headerLabel(column: number): string {
  const header = parsed.value.headers[column] ?? ''
  return header.trim() === '' ? t('file.table.columnFallback', { n: column + 1 }) : header
}

// ── View state ──────────────────────────────────────────────────────────────

const query = ref('')
const filterRowOpen = ref(false)
const columnFilters = ref<string[]>([])
const sort = ref<SortState | null>(null)
const page = ref(1)
const searchInputRef = ref<HTMLInputElement | null>(null)
const scrollRef = ref<HTMLElement | null>(null)
const tableRef = ref<HTMLElement | null>(null)
const headRowRef = ref<HTMLElement | null>(null)
const hiddenBtnRef = ref<HTMLElement | null>(null)

/** Hidden / frozen / data-bar columns all key off the ORIGINAL column index, so
 *  hiding a column never renumbers the state of the others. */
const hiddenColumns = ref<Set<number>>(new Set())
const frozenColumn = ref<number | null>(null)
const dataBars = ref<Set<number>>(new Set())
const valueFilters = ref<Map<number, Set<string>>>(new Map())

// Popup / sheet state. Declared up here because the reset watcher below runs
// immediately and closes them.
const menuColumn = ref<number | null>(null)
const menuTarget = ref<HTMLElement | null>(null)
const columnMenuOpen = ref(false)
const hiddenMenuOpen = ref(false)
const statsSheetOpen = ref(false)
const valueSheetColumn = ref<number | null>(null)
const valueSheetOpen = ref(false)

const visibleColumns = computed(() =>
  Array.from({ length: parsed.value.columnCount }, (_, i) => i).filter(i => !hiddenColumns.value.has(i)),
)
const hiddenColumnList = computed(() =>
  Array.from(hiddenColumns.value).sort((a, b) => a - b),
)

const activeFilterCount = computed(() => columnFilters.value.filter(f => f.trim() !== '').length)

/** Value-filtered rows. Kept separate from `filterAndSortRows` so the value
 *  filter can key off the raw cell text (exact match) while the text filters
 *  stay substring-based. */
const valueFilteredRows = computed(() => {
  if (valueFilters.value.size === 0) return parsed.value.rows
  const filters = [...valueFilters.value.entries()]
  return parsed.value.rows.filter(row =>
    filters.every(([column, values]) => values.size === 0 || values.has(row[column] ?? '')),
  )
})

const filteredRows = computed(() =>
  filterAndSortRows(valueFilteredRows.value, {
    query: query.value,
    columnFilters: columnFilters.value,
    sort: sort.value,
  }),
)

const pageCount = computed(() => Math.max(1, Math.ceil(filteredRows.value.length / PAGE_SIZE)))
const pageStart = computed(() => (page.value - 1) * PAGE_SIZE)
const pageRows = computed(() => filteredRows.value.slice(pageStart.value, pageStart.value + PAGE_SIZE))

const summaryText = computed(() =>
  t('file.table.rowSummary', {
    shown: filteredRows.value.length,
    total: parsed.value.rows.length,
  }),
)

const emptyText = computed(() =>
  parsed.value.rows.length === 0 ? t('file.table.empty') : t('file.table.noMatches'),
)

const notices = computed(() => {
  const list: string[] = []
  if (props.file?.truncated) list.push(t('file.viewer.truncated'))
  if (parsed.value.columnsTruncated) list.push(t('file.table.columnsTruncated', { n: MAX_TABLE_COLUMNS }))
  return list
})

// A new file (or an edit that changed the shape) resets every view control:
// a stale query, hidden column or sort on a column that no longer exists would
// silently hide data the user cannot explain.
watch(
  () => [props.file?.path, props.file?.content, parsed.value.columnCount],
  () => {
    query.value = ''
    sort.value = null
    page.value = 1
    columnFilters.value = new Array(parsed.value.columnCount).fill('')
    hiddenColumns.value = new Set()
    frozenColumn.value = null
    dataBars.value = new Set()
    valueFilters.value = new Map()
    columnMenuOpen.value = false
    hiddenMenuOpen.value = false
    statsSheetOpen.value = false
    valueSheetOpen.value = false
    if (parsed.value.columnCount === 0) filterRowOpen.value = false
  },
  { immediate: true },
)

// Any narrowing change restarts pagination — staying on page 7 of a now much
// shorter result would show an empty grid.
watch([query, sort, columnFilters, valueFilters], () => { page.value = 1 }, { deep: true })
watch(pageCount, (count) => { if (page.value > count) page.value = count })

function toggleFilterRow() {
  filterRowOpen.value = !filterRowOpen.value
}

function cycleSort(column: number) {
  const current = sort.value
  if (!current || current.column !== column) {
    sort.value = { column, direction: 'asc' }
    return
  }
  const next: Record<SortDirection | 'none', SortState | null> = {
    asc: { column, direction: 'desc' },
    desc: null,
    none: { column, direction: 'asc' },
  }
  sort.value = next[current.direction]
}

function sortGlyph(column: number): string {
  if (sort.value?.column !== column) return '⇅'
  return sort.value.direction === 'asc' ? '↑' : '↓'
}

function ariaSort(column: number): 'ascending' | 'descending' | 'none' {
  if (sort.value?.column !== column) return 'none'
  return sort.value.direction === 'asc' ? 'ascending' : 'descending'
}

// ── Column menu ─────────────────────────────────────────────────────────────

const menuStats = computed(() => {
  const column = menuColumn.value
  if (column === null) return computeColumnStats([], 0, false)
  return computeColumnStats(parsed.value.rows, column, numericColumns.value[column])
})

function openColumnMenu(event: MouseEvent, column: number) {
  menuColumn.value = column
  menuTarget.value = (event.currentTarget as HTMLElement | null) ?? null
  columnMenuOpen.value = true
}

function applySortFromMenu(direction: 'asc' | 'desc' | null) {
  const column = menuColumn.value
  if (column === null) return
  sort.value = direction ? { column, direction } : null
}

function hideColumn() {
  const column = menuColumn.value
  if (column === null || visibleColumns.value.length <= 1) return
  const next = new Set(hiddenColumns.value)
  next.add(column)
  hiddenColumns.value = next
  if (frozenColumn.value === column) frozenColumn.value = null
}

function showOnlyColumn() {
  const column = menuColumn.value
  if (column === null) return
  hiddenColumns.value = new Set(
    Array.from({ length: parsed.value.columnCount }, (_, i) => i).filter(i => i !== column),
  )
  if (frozenColumn.value !== column) frozenColumn.value = null
}

function restoreColumn(column: number) {
  const next = new Set(hiddenColumns.value)
  next.delete(column)
  hiddenColumns.value = next
  hiddenMenuOpen.value = false
}

function restoreAllColumns() {
  // Close first: the trigger button is `v-if="hiddenColumns.size"`, so clearing
  // the set would unmount the menu's anchor while it is still open.
  hiddenMenuOpen.value = false
  hiddenColumns.value = new Set()
}

function toggleFreeze() {
  const column = menuColumn.value
  if (column === null) return
  frozenColumn.value = frozenColumn.value === column ? null : column
}

function toggleDataBar() {
  const column = menuColumn.value
  if (column === null) return
  const next = new Set(dataBars.value)
  if (next.has(column)) next.delete(column)
  else next.add(column)
  dataBars.value = next
}

function copyColumn() {
  const column = menuColumn.value
  if (column === null) return
  // Copy what the user is looking at: filtered and in the current sort order.
  const text = filteredRows.value.map(row => row[column] ?? '').join('\n')
  copyText(
    text,
    () => toast.show(t('common.copied'), { icon: 'copy', type: 'success', duration: 1500 }),
    () => toast.show(t('common.operationFailed'), { type: 'error', icon: 'warning' }),
  )
}

function copyHeader() {
  const column = menuColumn.value
  if (column === null) return
  copyText(
    parsed.value.headers[column] ?? '',
    () => toast.show(t('common.copied'), { icon: 'copy', type: 'success', duration: 1500 }),
    () => toast.show(t('common.operationFailed'), { type: 'error', icon: 'warning' }),
  )
}

// ── Value filter ────────────────────────────────────────────────────────────

const valueSheetValues = computed(() =>
  valueSheetColumn.value === null ? [] : columnValueCounts(parsed.value.rows, valueSheetColumn.value),
)
const valueSheetSelected = computed(() => {
  const column = valueSheetColumn.value
  if (column === null) return []
  return [...(valueFilters.value.get(column) ?? [])]
})

function openValueSheet() {
  valueSheetColumn.value = menuColumn.value
  valueSheetOpen.value = true
}

function applyValueFilter(values: string[]) {
  const column = valueSheetColumn.value
  if (column === null) return
  const next = new Map(valueFilters.value)
  if (values.length === 0) next.delete(column)
  else next.set(column, new Set(values))
  valueFilters.value = next
}

function clearValueFilter() {
  const column = menuColumn.value
  if (column === null) return
  const next = new Map(valueFilters.value)
  next.delete(column)
  valueFilters.value = next
}

// ── Frozen column offset ────────────────────────────────────────────────────

/** Pixels between the table's left edge and the frozen column, i.e. the summed
 *  width of every visible column before it. Measured from the DOM because
 *  column widths are content-driven (`table-layout: auto`). */
const frozenLeft = ref(0)
let tableObserver: ResizeObserver | null = null

function measureFrozenOffset() {
  const column = frozenColumn.value
  const row = headRowRef.value
  if (column === null || !row) { frozenLeft.value = 0; return }
  const cells = row.querySelectorAll('th.data-th')
  let left = 0
  for (let i = 0; i < visibleColumns.value.length; i++) {
    if (visibleColumns.value[i] === column) break
    left += cells[i]?.getBoundingClientRect().width ?? 0
  }
  frozenLeft.value = left
}

function frozenStyle(column: number) {
  return column === frozenColumn.value ? { left: `${frozenLeft.value}px` } : undefined
}

onMounted(() => {
  if (typeof ResizeObserver === 'undefined' || !tableRef.value) return
  tableObserver = new ResizeObserver(() => measureFrozenOffset())
  tableObserver.observe(tableRef.value)
})

onBeforeUnmount(() => {
  tableObserver?.disconnect()
  tableObserver = null
})

// Re-measure after anything that can change a column's width or position.
watch(
  [frozenColumn, visibleColumns, pageRows, filterRowOpen, sort],
  () => { void nextTick(measureFrozenOffset) },
  { deep: true },
)

// ── Data bars ───────────────────────────────────────────────────────────────

interface Range { min: number; max: number }

const barRanges = computed<Record<number, Range>>(() => {
  const ranges: Record<number, Range> = {}
  for (const column of dataBars.value) {
    let min = Number.POSITIVE_INFINITY
    let max = Number.NEGATIVE_INFINITY
    let seen = 0
    for (const row of parsed.value.rows) {
      const n = parseNumericCell(row[column] ?? '')
      if (n === null) continue
      if (n < min) min = n
      if (n > max) max = n
      seen++
    }
    if (seen > 0) ranges[column] = { min, max }
  }
  return ranges
})

function hasDataBar(column: number): boolean {
  return dataBars.value.has(column) && numericColumns.value[column] === true && barRanges.value[column] !== undefined
}

function isNegative(value: string | undefined): boolean {
  const n = parseNumericCell(value ?? '')
  return n !== null && n < 0
}

/** Bar length for one cell.
 *  All-positive / all-negative columns map min→0% and max→100% (the Excel
 *  behaviour: differences inside the column stay visible). A column that
 *  crosses zero is measured from 0 so the sign is comparable. */
function dataBarStyle(column: number, value: string | undefined): Record<string, string> {
  const range = barRanges.value[column]
  const n = parseNumericCell(value ?? '')
  if (!range || n === null) return { width: '0%' }

  // Every branch below assigns, so no initial value is needed (and a `= 0`
  // would be flagged as a useless assignment).
  let ratio: number
  if (range.min >= 0) {
    ratio = range.max === range.min ? 1 : (n - range.min) / (range.max - range.min)
  } else if (range.max <= 0) {
    ratio = range.max === range.min ? 1 : (range.max - n) / (range.max - range.min)
  } else {
    const maxAbs = Math.max(Math.abs(range.min), Math.abs(range.max)) || 1
    ratio = Math.abs(n) / maxAbs
  }
  const percent = Math.max(0, Math.min(1, ratio)) * 100
  return { width: `${percent}%` }
}

// ── Search routing ──────────────────────────────────────────────────────────

/** Focus the search box (header button / Ctrl+F route through here). */
function focusSearch() {
  void nextTick(() => {
    searchInputRef.value?.focus()
    searchInputRef.value?.select()
  })
}

/** Dismiss the search UI: clear the free-text query and drop focus. Column
 *  filters stay — they are a separate, still-visible control. */
function closeSearch() {
  query.value = ''
  searchInputRef.value?.blur()
}

defineExpose({ focusSearch, closeSearch })
</script>

<style scoped>
.table-preview {
  display: flex;
  flex: 1;
  flex-direction: column;
  min-height: 0;
  min-width: 0;
  background: var(--bg-primary);
}

/* ── Toolbar ─────────────────────────────────────────────────────────────── */

.table-toolbar {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: var(--space-3);
  padding: var(--space-4) var(--space-6);
  border-bottom: 1px solid var(--border-color);
  background: var(--bg-primary);
}

.table-search {
  position: relative;
  display: flex;
  align-items: center;
  flex: 1;
  min-width: calc(150 * var(--dp));
  max-width: calc(340 * var(--dp));
}

.table-search-icon {
  position: absolute;
  left: var(--space-4);
  color: var(--text-muted);
  pointer-events: none;
}

.table-search-input {
  width: 100%;
  box-sizing: border-box;
  padding: calc(5 * var(--dp)) calc(24 * var(--dp)) calc(5 * var(--dp)) calc(26 * var(--dp));
  border: 1px solid var(--border-color);
  border-radius: var(--radius-lg);
  background: var(--bg-secondary);
  color: var(--text-primary);
  font-family: inherit;
  font-size: var(--font-size-md);
  line-height: var(--input-line-height, normal);
  outline: none;
}

.table-search-input:focus {
  border-color: var(--accent-color);
}

.table-search-clear {
  position: absolute;
  right: var(--space-2);
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: calc(18 * var(--dp));
  height: calc(18 * var(--dp));
  padding: 0;
  border: none;
  border-radius: var(--radius-full);
  background: transparent;
  color: var(--text-secondary);
  cursor: pointer;
}

.table-filter-btn {
  display: inline-flex;
  align-items: center;
  gap: var(--space-2);
  padding: calc(5 * var(--dp)) var(--space-6);
  border: 1px solid var(--border-color);
  border-radius: var(--radius-lg);
  background: transparent;
  color: var(--text-secondary);
  font-family: inherit;
  font-size: var(--font-size-sm);
  line-height: 1;
  cursor: pointer;
  transition: border-color var(--duration-base), color var(--duration-base);
}

.table-filter-btn.active {
  border-color: var(--accent-color);
  color: var(--accent-color);
}

.table-filter-count {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  min-width: calc(14 * var(--dp));
  height: calc(14 * var(--dp));
  padding: 0 calc(3 * var(--dp));
  border-radius: var(--radius-full);
  background: var(--accent-color);
  color: #fff;
  font-size: var(--font-size-2xs);
  line-height: 1;
}

.table-summary {
  margin-left: auto;
  color: var(--text-secondary);
  font-size: var(--font-size-sm);
  font-variant-numeric: tabular-nums;
  white-space: nowrap;
}

/* ── Notices (truncated file / too many columns) ─────────────────────────── */

.table-notices {
  flex-shrink: 0;
}

.table-notice {
  display: flex;
  align-items: center;
  gap: var(--space-3);
  padding: var(--space-2) var(--space-6);
  background: rgba(245, 158, 11, 0.1);
  border-bottom: 1px solid rgba(245, 158, 11, 0.2);
  color: var(--color-orange);
  font-size: var(--font-size-sm);
}

/* ── Grid ────────────────────────────────────────────────────────────────── */

.table-scroll {
  flex: 1;
  min-height: 0;
  overflow: auto;
  /* The grid scrolls on its own; keep the page behind it still. */
  overscroll-behavior: contain;
}

.data-table {
  width: max-content;
  min-width: 100%;
  border-collapse: separate;
  border-spacing: 0;
  font-size: var(--font-size-sm);
}

.data-th,
.data-td {
  padding: calc(6 * var(--dp)) var(--space-4);
  border-bottom: 1px solid var(--border-color);
  text-align: left;
  vertical-align: top;
}

.data-th {
  position: sticky;
  top: 0;
  z-index: 2;
  background: var(--bg-secondary);
  color: var(--text-secondary);
  font-weight: var(--font-weight-medium);
  white-space: nowrap;
  cursor: pointer;
  user-select: none;
}

/* Second header row: one search box per column. Styled as part of the grid —
   square corners, no box of its own, just the cell's own bottom border. A
   rounded input floating over a square table reads as a foreign control.
   `size="1"` on the element collapses the input's intrinsic ~20-character
   width; without it every column would be widened to fit a search box. */
.filter-row .filter-cell {
  position: sticky;
  top: calc(30 * var(--dp));
  z-index: 2;
  height: calc(30 * var(--dp));
  padding: 0;
  border-bottom: 1px solid var(--border-color);
  background: var(--bg-secondary);
}

.head-row .data-th {
  height: calc(30 * var(--dp));
  box-sizing: border-box;
}

.th-inner {
  display: inline-flex;
  align-items: center;
  gap: var(--space-2);
}

.th-label {
  max-width: calc(220 * var(--dp));
  overflow: hidden;
  text-overflow: ellipsis;
}

.th-sort {
  color: var(--text-muted);
  font-size: var(--font-size-2xs);
}

.data-th.is-sorted {
  color: var(--text-primary);
}

.data-th.is-sorted .th-sort {
  color: var(--accent-color);
}

/* Per-column menu trigger. Always visible: a touch screen has no hover, so a
   hover-only affordance would be unreachable on Android. */
.th-menu-btn {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: calc(18 * var(--dp));
  height: calc(18 * var(--dp));
  padding: 0;
  border: none;
  border-radius: var(--radius-sm);
  background: transparent;
  color: var(--text-muted);
  cursor: pointer;
}

/* Fills its cell edge to edge: same padding as a data cell, no border of its
   own, square corners. Focus is shown with the accent underline rather than a
   ring, so the row keeps reading as a table row. */
.filter-input {
  display: block;
  width: 100%;
  min-width: 0;
  box-sizing: border-box;
  padding: 0 var(--space-4);
  border: none;
  border-radius: 0;
  background: transparent;
  color: var(--text-primary);
  font-family: inherit;
  font-size: var(--font-size-sm);
  line-height: var(--input-line-height, normal);
  outline: none;
  box-shadow: inset 0 calc(-2 * var(--dp)) 0 transparent;
  transition: box-shadow var(--duration-base);
}

.filter-input::placeholder {
  color: var(--text-muted);
}

.filter-input:focus {
  box-shadow: inset 0 calc(-2 * var(--dp)) 0 var(--accent-color);
}

.data-td {
  position: relative;
  color: var(--text-primary);
}

.data-row:hover .data-td {
  background: var(--bg-secondary);
}

/* Data bar: painted behind the text, never over it. */
.data-bar {
  position: absolute;
  top: calc(3 * var(--dp));
  bottom: calc(3 * var(--dp));
  left: 0;
  min-width: calc(2 * var(--dp));
  border-radius: 0;
  background: color-mix(in srgb, var(--accent-color) 22%, transparent);
  pointer-events: none;
}

.data-bar.is-negative {
  left: auto;
  right: 0;
  background: color-mix(in srgb, var(--color-red) 24%, transparent);
}

.cell-text {
  position: relative;
  display: block;
  max-width: calc(360 * var(--dp));
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.is-numeric {
  text-align: right;
}

.data-td.is-numeric .cell-text {
  font-variant-numeric: tabular-nums;
}

/* ── Frozen column ───────────────────────────────────────────────────────── */

.data-th.is-frozen,
.filter-cell.is-frozen {
  left: 0; /* overridden by the measured inline offset */
  z-index: 4;
  background: var(--bg-secondary);
}

.data-td.is-frozen {
  /* `position: sticky` is the whole mechanism — without it `left` is just a
     relative offset, and the cell scrolls away with everything else. The
     header cell gets sticky from `.data-th`; body cells must opt in here. */
  position: sticky;
  left: 0; /* overridden by the measured inline offset */
  z-index: 3;
  background: var(--bg-primary);
  box-shadow: 1px 0 0 var(--border-color);
}

.table-empty {
  padding: calc(40 * var(--dp)) var(--space-6);
  color: var(--text-secondary);
  font-size: var(--font-size-sm);
  text-align: center;
}

/* ── Restore-hidden-columns menu ─────────────────────────────────────────── */

.restore-item {
  display: flex;
  align-items: center;
  gap: var(--space-4);
  width: 100%;
  padding: calc(6 * var(--dp)) var(--space-5);
  border: none;
  background: transparent;
  color: var(--text-primary);
  font-family: inherit;
  font-size: var(--font-size-md);
  text-align: left;
  cursor: pointer;
  white-space: nowrap;
}

.restore-icon {
  flex-shrink: 0;
  color: var(--text-secondary);
}

.restore-label {
  overflow: hidden;
  text-overflow: ellipsis;
}

/* ── Pagination ──────────────────────────────────────────────────────────── */

.table-footer {
  display: flex;
  align-items: center;
  justify-content: center;
  gap: var(--space-4);
  flex-shrink: 0;
  padding: var(--space-3) var(--space-6);
  border-top: 1px solid var(--border-color);
  background: var(--bg-primary);
}

.table-page-btn {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: calc(24 * var(--dp));
  height: calc(24 * var(--dp));
  padding: 0;
  border: 1px solid var(--border-color);
  border-radius: var(--radius-md);
  background: transparent;
  color: var(--text-secondary);
  cursor: pointer;
}

.table-page-btn:disabled {
  opacity: 0.4;
  cursor: default;
}

.table-page-info {
  color: var(--text-secondary);
  font-size: var(--font-size-sm);
  font-variant-numeric: tabular-nums;
}

@media (hover: hover) {
  .table-filter-btn:hover:not(.active) {
    border-color: var(--text-secondary);
    color: var(--text-primary);
  }

  .table-search-clear:hover {
    background: var(--bg-tertiary);
    color: var(--text-primary);
  }

  .table-page-btn:not(:disabled):hover {
    border-color: var(--accent-color);
    color: var(--accent-color);
  }

  .th-menu-btn:hover {
    background: var(--bg-tertiary);
    color: var(--text-primary);
  }

  .restore-item:hover {
    background: var(--bg-hover);
  }
}
</style>

<style>
[data-theme-base="dark"] .table-notice {
  background: rgba(245, 158, 11, 0.15);
  color: #fbbf24;
  border-bottom-color: rgba(245, 158, 11, 0.3);
}
</style>
