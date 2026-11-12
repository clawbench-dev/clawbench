<template>
  <div ref="rootRef" class="dt" :class="{ 'is-data': isData, 'is-fill': fill, 'no-wrap': !wrap }">
    <!-- 表头栏：标题本身就是「简单表 / 数据表」的切换器 -->
    <div class="dt-head">
      <div class="dt-seg" role="group" :aria-label="t('dataTable.mode')">
        <button type="button" :class="{ 'is-on': !isData }" @click="setMode('normal')">{{ t('dataTable.normal') }}</button>
        <button type="button" :class="{ 'is-on': isData }" @click="setMode('data')">{{ t('dataTable.data') }}</button>
      </div>
      <span class="dt-head-spacer" />
      <div class="dt-actions">
        <button
          class="dt-act dt-copy-btn"
          type="button"
          :title="t('tableBlock.copy')"
          :aria-label="t('tableBlock.copy')"
          @click="openCopyMenu($event)"
        >
          <Copy :size="12" /><ChevronDown :size="9" class="dt-caret" />
        </button>
        <button
          v-if="!isData"
          class="dt-act dt-wrap-btn"
          type="button"
          :class="{ 'is-on': wrap }"
          :title="wrap ? t('tableBlock.wrapOn') : t('tableBlock.wrapOff')"
          :aria-label="wrap ? t('tableBlock.wrapOn') : t('tableBlock.wrapOff')"
          @click="wrap = !wrap"
        >
          <WrapText :size="12" />
        </button>
        <button
          class="table-block-landscape-btn"
          type="button"
          :title="t('tableBlock.landscape')"
          :aria-label="t('tableBlock.landscape')"
          @click="openLandscape"
        >
          <RotateCw :size="12" />
        </button>
      </div>
    </div>

    <!-- 工具条：只有数据表才有 -->
    <div v-if="isData" class="dt-bar">
      <div class="dt-search" :class="{ 'has-value': query }">
        <Search :size="13" class="dt-search-icon" />
        <input
          ref="searchRef"
          v-model="query"
          type="search"
          :placeholder="t('dataTable.search')"
          :aria-label="t('dataTable.search')"
          @keydown.esc="query = ''"
        />
        <button v-if="query" class="dt-search-clear" type="button" :aria-label="t('common.close')" @click="query = ''">
          <X :size="11" />
        </button>
      </div>
      <button
        class="dt-tool"
        :class="{ 'is-on': filterRowOpen || activeFilterCount > 0 }"
        type="button"
        :title="t('dataTable.filterRowHint')"
        @click="filterRowOpen = !filterRowOpen"
      >
        {{ t('dataTable.filterRow') }}<span v-if="activeFilterCount" class="dt-badge">{{ activeFilterCount }}</span>
      </button>
      <button class="dt-tool" :class="{ 'is-on': hidden.size > 0 }" type="button" @click="openColumns($event)">
        {{ t('dataTable.columns') }}<span v-if="hidden.size" class="dt-badge">{{ hidden.size }}</span>
      </button>
      <span class="dt-count">{{ countLabel }}</span>
    </div>

    <!-- 网格 -->
    <div ref="scrollRef" class="dt-scroll" :style="scrollStyle">
      <table ref="tableRef" class="dt-grid">
        <thead>
          <tr class="dt-head-row">
            <th
              v-for="col in visibleColumns"
              :key="col"
              :data-col="col"
              scope="col"
              :class="{
                'is-numeric': isNumCol(col),
                'is-sorted': sort?.column === col,
                'is-frozen': frozen === col,
              }"
              :style="frozenStyle(col)"
              :aria-sort="ariaSort(col)"
              @click="onHeaderClick(col)"
            >
              <span class="dt-th-label">{{ headerLabel(col) }}</span>
              <span v-if="isData" class="dt-arrow" aria-hidden="true">{{ sortGlyph(col) }}</span>
              <button
                v-if="isData"
                class="dt-funnel"
                type="button"
                :class="{ 'is-on': hasColumnFilter(col) }"
                :title="t('dataTable.columnMenu')"
                :aria-label="t('dataTable.columnMenu')"
                @click.stop="openColumnMenu($event, col)"
              >
                <Filter :size="11" />
              </button>
            </th>
          </tr>
          <tr v-if="isData && filterRowOpen" class="dt-filter-row">
            <th
              v-for="col in visibleColumns"
              :key="col"
              class="dt-filter-cell"
              :class="{ 'is-frozen': frozen === col }"
              :style="frozenStyle(col)"
            >
              <input
                v-model="columnFilters[col]"
                class="dt-filter-input"
                type="text"
                size="1"
                :placeholder="t('dataTable.columnFilter')"
                :aria-label="`${t('dataTable.columnFilter')} ${headerLabel(col)}`"
              />
            </th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="(row, ri) in pageRows" :key="pageStart + ri" class="dt-row">
            <td
              v-for="col in visibleColumns"
              :key="col"
              :class="{ 'is-numeric': isNumCol(col), 'is-frozen': frozen === col }"
              :style="frozenStyle(col)"
            >
              <span v-if="barWidth(col, row[col])" class="dt-cell-bar" :style="{ width: barWidth(col, row[col]) }" aria-hidden="true" />
              <span class="dt-cell">{{ displayCell(col, row[col]) }}</span>
            </td>
          </tr>
          <tr v-if="pageRows.length === 0">
            <td class="dt-empty" :colspan="Math.max(visibleColumns.length, 1)">{{ emptyText }}</td>
          </tr>
        </tbody>
        <tfoot v-if="isData && pageRows.length > 0 && hasNumericColumn">
          <tr>
            <td
              v-for="col in visibleColumns"
              :key="col"
              :class="{ 'is-numeric': isNumCol(col), 'is-frozen': frozen === col }"
              :style="frozenStyle(col)"
            >
              <template v-if="col === visibleColumns[0]">{{ t('dataTable.total') }}</template>
              <template v-else>{{ footerValue(col) }}</template>
            </td>
          </tr>
        </tfoot>
      </table>
    </div>

    <!-- 分页（文件预览用；聊天里不分页） -->
    <div v-if="pageCount > 1" class="dt-pager">
      <button class="dt-page-btn" type="button" :disabled="page <= 1" :aria-label="t('dataTable.prevPage')" @click="page -= 1">
        <ChevronLeft :size="14" />
      </button>
      <span class="dt-page-info">{{ t('dataTable.pageInfo', { page, total: pageCount }) }}</span>
      <button class="dt-page-btn" type="button" :disabled="page >= pageCount" :aria-label="t('dataTable.nextPage')" @click="page += 1">
        <ChevronRight :size="14" />
      </button>
    </div>

    <!-- 浮层：列菜单 / 按值筛选 / 列统计 / 显示列 -->
    <Teleport to="body">
      <div
        v-if="pop"
        ref="popEl"
        class="dt-pop"
        :style="{ left: `${pop.x}px`, top: `${pop.y}px` }"
        @mousedown.stop
      >
        <template v-if="pop.kind === 'menu'">
          <div class="dt-pop-head">{{ headerLabel(pop.col) }}</div>
          <div class="dt-pop-filter">
            <input
              type="text"
              :value="columnFilters[pop.col]"
              :placeholder="t('dataTable.filterColumn')"
              :aria-label="t('dataTable.filterColumn')"
              @input="setColumnFilter(pop.col, ($event.target as HTMLInputElement).value)"
            />
          </div>
          <div class="dt-pop-sep" />
          <button class="dt-pop-item" type="button" @click="applySort('asc')">
            <ArrowUp :size="13" class="dt-pop-icon" />{{ t('dataTable.sortAsc') }}
          </button>
          <button class="dt-pop-item" type="button" @click="applySort('desc')">
            <ArrowDown :size="13" class="dt-pop-icon" />{{ t('dataTable.sortDesc') }}
          </button>
          <button v-if="sort" class="dt-pop-item" type="button" @click="applySort(null)">
            <X :size="13" class="dt-pop-icon" />{{ t('dataTable.sortClear') }}
          </button>
          <div class="dt-pop-sep" />
          <button class="dt-pop-item" type="button" @click="openValues(pop.col)">
            <ListFilter :size="13" class="dt-pop-icon" />{{ t('dataTable.filterByValue') }}
            <span v-if="valueFilterCount(pop.col)" class="dt-pop-mini">{{ valueFilterCount(pop.col) }}</span>
          </button>
          <button v-if="hasColumnFilter(pop.col)" class="dt-pop-item" type="button" @click="clearColumnFilter(pop.col)">
            <FilterX :size="13" class="dt-pop-icon" />{{ t('dataTable.clearFilter') }}
          </button>
          <div class="dt-pop-sep" />
          <button class="dt-pop-item" type="button" :disabled="visibleColumns.length <= 1" @click="hideColumn(pop.col)">
            <EyeOff :size="13" class="dt-pop-icon" />{{ t('dataTable.hideColumn') }}
          </button>
          <button class="dt-pop-item" type="button" :disabled="visibleColumns.length <= 1" @click="onlyColumn(pop.col)">
            <Eye :size="13" class="dt-pop-icon" />{{ t('dataTable.onlyColumn') }}
          </button>
          <button class="dt-pop-item" type="button" @click="toggleFreeze(pop.col)">
            <Pin :size="13" class="dt-pop-icon" />{{ frozen === pop.col ? t('dataTable.unfreeze') : t('dataTable.freeze') }}
          </button>
          <template v-if="isNumCol(pop.col)">
            <div class="dt-pop-sep" />
            <button class="dt-pop-item" type="button" @click="toggleBars(pop.col)">
              <BarChartHorizontal :size="13" class="dt-pop-icon" />{{ barsOff.has(pop.col) ? t('dataTable.showBars') : t('dataTable.hideBars') }}
            </button>
          </template>
          <div class="dt-pop-sep" />
          <button class="dt-pop-item" type="button" @click="copyColumn(pop.col)">
            <Copy :size="13" class="dt-pop-icon" />{{ t('dataTable.copyColumn') }}
          </button>
          <button class="dt-pop-item" type="button" @click="openStats(pop.col)">
            <BarChart3 :size="13" class="dt-pop-icon" />{{ t('dataTable.stats') }}
          </button>
        </template>

        <template v-else-if="pop.kind === 'values'">
          <div class="dt-pop-head">{{ t('dataTable.filterByValue') }} · {{ headerLabel(pop.col) }}</div>
          <div class="dt-pop-search">
            <input v-model="valueQuery" type="text" :placeholder="t('dataTable.searchValues')" :aria-label="t('dataTable.searchValues')" />
          </div>
          <div class="dt-pop-list">
            <label v-for="item in visibleValues" :key="item.value" class="dt-pop-check">
              <input
                type="checkbox"
                :checked="isValueChecked(item.value)"
                @change="toggleValue(item.value)"
              />
              <span class="dt-pop-value">{{ item.value === '' ? t('dataTable.emptyValue') : item.value }}</span>
              <span class="dt-pop-count">{{ item.count }}</span>
            </label>
            <div v-if="visibleValues.length === 0" class="dt-pop-empty">{{ t('dataTable.noMatchingValues') }}</div>
          </div>
          <div class="dt-pop-foot">
            <button class="dt-pop-btn" type="button" :disabled="!valueFilterCount(pop.col)" @click="clearValues(pop.col)">
              {{ t('dataTable.showAllValues') }}
            </button>
            <button class="dt-pop-btn is-primary" type="button" @click="closePop">{{ t('common.ok') }}</button>
          </div>
        </template>

        <template v-else-if="pop.kind === 'stats'">
          <div class="dt-pop-head">{{ headerLabel(pop.col) }} · {{ t('dataTable.stats') }}</div>
          <div class="dt-stat-sec">{{ t('dataTable.statsBasic') }}</div>
          <dl class="dt-stat-list">
            <div class="dt-stat-row"><dt>{{ t('dataTable.statRows') }}</dt><dd>{{ popStats.rows.toLocaleString() }}</dd></div>
            <div class="dt-stat-row"><dt>{{ t('dataTable.statEmpty') }}</dt><dd>{{ popStats.empty.toLocaleString() }}</dd></div>
            <div class="dt-stat-row"><dt>{{ t('dataTable.statDistinct') }}</dt><dd>{{ popStats.distinct.toLocaleString() }}</dd></div>
          </dl>
          <template v-if="popStats.sum !== null">
            <div class="dt-stat-sec">{{ t('dataTable.statsNumeric') }}</div>
            <dl class="dt-stat-list">
              <div class="dt-stat-row"><dt>{{ t('dataTable.statSum') }}</dt><dd>{{ formatNumber(popStats.sum) }}</dd></div>
              <div class="dt-stat-row"><dt>{{ t('dataTable.statAvg') }}</dt><dd>{{ formatNumber(popStats.avg) }}</dd></div>
              <div class="dt-stat-row"><dt>{{ t('dataTable.statMin') }}</dt><dd>{{ formatNumber(popStats.min) }}</dd></div>
              <div class="dt-stat-row"><dt>{{ t('dataTable.statMax') }}</dt><dd>{{ formatNumber(popStats.max) }}</dd></div>
            </dl>
          </template>
          <p v-else class="dt-stat-hint">{{ t('dataTable.statsTextOnly') }}</p>
        </template>

        <template v-else-if="pop.kind === 'copy'">
          <button class="dt-pop-item" type="button" @click="copyTable('markdown')">{{ t('tableBlock.copyMarkdown') }}</button>
          <button class="dt-pop-item" type="button" @click="copyTable('html')">{{ t('tableBlock.copyHtml') }}</button>
          <button class="dt-pop-item" type="button" @click="copyTable('tsv')">{{ t('tableBlock.copyTsv') }}</button>
          <button class="dt-pop-item" type="button" @click="copyTable('csv')">{{ t('dataTable.copyCsv') }}</button>
        </template>

        <template v-else-if="pop.kind === 'columns'">
          <div class="dt-pop-head">{{ t('dataTable.columns') }}</div>
          <div class="dt-pop-list">
            <label v-for="option in headerOptions" :key="option.index" class="dt-pop-check">
              <input
                type="checkbox"
                :checked="!hidden.has(option.index)"
                @change="toggleColumn(option.index, ($event.target as HTMLInputElement).checked)"
              />
              <span class="dt-pop-value">{{ option.label }}</span>
            </label>
          </div>
          <div class="dt-pop-foot">
            <button class="dt-pop-btn" type="button" @click="showAllColumns">{{ t('dataTable.showAllColumns') }}</button>
          </div>
        </template>
      </div>
    </Teleport>
  </div>
</template>

<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import {
  ArrowDown,
  ArrowUp,
  BarChart3,
  BarChartHorizontal,
  ChevronDown,
  ChevronLeft,
  ChevronRight,
  Copy,
  Eye,
  EyeOff,
  Filter,
  FilterX,
  ListFilter,
  Pin,
  RotateCw,
  Search,
  WrapText,
  X,
} from 'lucide-vue-next'
import {
  columnValueCounts,
  computeColumnStats,
  detectNumericColumns,
  filterAndSortRows,
  isNumericTable,
  parseNumericCell,
  type SortDirection,
  type SortState,
} from '@/utils/csvTable'
import { copyText } from '@/utils/clipboard'
import { getDefaultBlockWrap } from '@/utils/blockWrapRead'
import { useToast } from '@/composables/useToast'

const props = defineProps<{
  headers: string[]
  rows: string[][]
  /** auto = 按内容判定（默认）；normal / data = 强制 */
  initialMode?: 'auto' | 'normal' | 'data'
  /** 每页行数；0 表示不分页 */
  pageSize?: number
  /** 不分页时滚动区的最大高度 */
  maxHeight?: string
  /** 撑满父容器（文件预览用） */
  fill?: boolean
}>()

const { t } = useI18n()
const toast = useToast()

const mode = ref<'auto' | 'normal' | 'data'>(props.initialMode ?? 'auto')
const query = ref('')
const filterRowOpen = ref(false)
const columnFilters = ref<string[]>([])
const sort = ref<SortState | null>(null)
const page = ref(1)
const hidden = ref<Set<number>>(new Set())
const frozen = ref<number | null>(0)
const barsOff = ref<Set<number>>(new Set())
const valueFilters = ref<Map<number, Set<string>>>(new Map())

/** 浮层状态要在复位 watcher 之前声明：那个 watcher 是 immediate 的，会先调 closePop。 */
const pop = ref<{ kind: 'menu' | 'values' | 'stats' | 'columns' | 'copy'; col: number; x: number; y: number } | null>(null)
const popEl = ref<HTMLElement | null>(null)
let popAnchor: HTMLElement | null = null

const rootRef = ref<HTMLElement | null>(null)
const wrap = ref(getDefaultBlockWrap())
const searchRef = ref<HTMLInputElement | null>(null)
const scrollRef = ref<HTMLElement | null>(null)
const tableRef = ref<HTMLElement | null>(null)
const frozenLeft = ref(0)

const pageSize = computed(() => props.pageSize ?? 0)

/** 内容变了（换文件 / 换消息里的表）就把视图状态全部复位，否则会拿旧筛选条件去筛新表。 */
watch(
  () => [props.headers, props.rows],
  () => {
    mode.value = props.initialMode ?? 'auto'
    query.value = ''
    filterRowOpen.value = false
    columnFilters.value = props.headers.map(() => '')
    sort.value = null
    page.value = 1
    hidden.value = new Set()
    frozen.value = 0
    barsOff.value = new Set()
    valueFilters.value = new Map()
    closePop()
    void nextTick(measureFrozen)
  },
  { immediate: true },
)

// ── 视图数据 ───────────────────────────────────────────────────────────

const columnCount = computed(() => props.headers.length)
const detected = computed(() => isNumericTable(props.headers, props.rows))
const isData = computed(() => (mode.value === 'auto' ? detected.value : mode.value === 'data'))
const numeric = computed(() => detectNumericColumns(props.rows, columnCount.value))
const visibleColumns = computed(() =>
  Array.from({ length: columnCount.value }, (_, i) => i).filter(i => !hidden.value.has(i)),
)

const activeFilterCount = computed(() => {
  const columns = columnFilters.value.filter(v => v.trim() !== '').length
  const values = [...valueFilters.value.values()].filter(set => set.size > 0).length
  return columns + values
})

/** 值筛选先按原值精确匹配，再交给共用的「关键词 + 每列关键词 + 排序」流程。 */
const filteredRows = computed(() => {
  if (!isData.value) return props.rows
  let rows = props.rows
  if (valueFilters.value.size > 0) {
    const filters = [...valueFilters.value.entries()]
    rows = rows.filter(row =>
      filters.every(([col, values]) => values.size === 0 || values.has(row[col] ?? '')),
    )
  }
  return filterAndSortRows(rows, {
    query: query.value,
    columnFilters: columnFilters.value,
    sort: sort.value,
  })
})

const pageCount = computed(() =>
  pageSize.value > 0 ? Math.max(1, Math.ceil(filteredRows.value.length / pageSize.value)) : 1,
)
const pageStart = computed(() => (pageSize.value > 0 ? (page.value - 1) * pageSize.value : 0))
const pageRows = computed(() =>
  pageSize.value > 0
    ? filteredRows.value.slice(pageStart.value, pageStart.value + pageSize.value)
    : filteredRows.value,
)

watch(pageCount, count => { if (page.value > count) page.value = count })
watch([query, sort, columnFilters, valueFilters], () => { page.value = 1 }, { deep: true })

const countLabel = computed(() => {
  const total = props.rows.length
  const shown = filteredRows.value.length
  const rows = isData.value && shown !== total
    ? t('dataTable.rowCountFiltered', { shown, total })
    : t('dataTable.rowCount', { n: total })
  return `${rows} · ${t('dataTable.columnCount', { n: visibleColumns.value.length })}`
})

/** 显示列浮层用的选项（空表头也带位置兜底名）。 */
const headerOptions = computed(() =>
  props.headers.map((_, index) => ({ index, label: headerLabel(index) })),
)

const emptyText = computed(() =>
  props.rows.length === 0 ? t('dataTable.empty') : t('dataTable.noMatches'),
)

const scrollStyle = computed(() => {
  if (props.fill) return undefined
  return { maxHeight: props.maxHeight ?? '420px' }
})

function isNumCol(col: number): boolean {
  return isData.value && col > 0 && Boolean(numeric.value[col])
}

function headerLabel(col: number): string {
  const header = props.headers[col] ?? ''
  return header.trim() === '' ? t('dataTable.columnFallback', { n: col + 1 }) : header
}

/** 只给「本来就是规范写法」的数字加千分位，`007`、`42.10` 这类原样显示。 */
function displayCell(col: number, value: string | undefined): string {
  const raw = value ?? ''
  if (!isNumCol(col)) return raw
  const n = parseNumericCell(raw)
  if (n === null || String(n) !== raw.trim()) return raw
  return n.toLocaleString('en-US', { maximumFractionDigits: 20 })
}

function formatNumber(value: number | null): string {
  if (value === null || !Number.isFinite(value)) return '—'
  const rounded = Math.round(value * 100) / 100
  return rounded.toLocaleString('en-US', { maximumFractionDigits: 2 })
}

// ── 排序 ───────────────────────────────────────────────────────────────

function onHeaderClick(col: number): void {
  if (!isData.value) return
  const current = sort.value
  if (!current || current.column !== col) {
    sort.value = { column: col, direction: 'asc' }
    return
  }
  const next: Record<SortDirection | 'none', SortState | null> = {
    asc: { column: col, direction: 'desc' },
    desc: null,
    none: { column: col, direction: 'asc' },
  }
  sort.value = next[current.direction]
}

function applySort(direction: SortDirection | null): void {
  const col = pop.value?.col
  if (col === undefined) return
  sort.value = direction ? { column: col, direction } : null
  closePop()
}

function sortGlyph(col: number): string {
  if (sort.value?.column !== col) return '⇅'
  return sort.value.direction === 'asc' ? '↑' : '↓'
}

function ariaSort(col: number): 'ascending' | 'descending' | 'none' {
  if (sort.value?.column !== col) return 'none'
  return sort.value.direction === 'asc' ? 'ascending' : 'descending'
}

// ── 数据条 / 合计 ──────────────────────────────────────────────────────

const barRanges = computed<Record<number, { min: number; max: number }>>(() => {
  const ranges: Record<number, { min: number; max: number }> = {}
  if (!isData.value) return ranges
  for (const col of visibleColumns.value) {
    if (!isNumCol(col)) continue
    let min = Number.POSITIVE_INFINITY
    let max = Number.NEGATIVE_INFINITY
    let seen = 0
    for (const row of filteredRows.value) {
      const n = parseNumericCell(row[col] ?? '')
      if (n === null) continue
      if (n < min) min = n
      if (n > max) max = n
      seen++
    }
    if (seen > 0) ranges[col] = { min, max }
  }
  return ranges
})

function barWidth(col: number, value: string | undefined): string {
  if (!isNumCol(col) || barsOff.value.has(col)) return ''
  const range = barRanges.value[col]
  const n = parseNumericCell(value ?? '')
  if (!range || n === null) return ''
  const ratio = range.max === range.min ? 1 : (n - range.min) / (range.max - range.min)
  return `${Math.max(2, Math.round(Math.max(0, Math.min(1, ratio)) * 100))}%`
}

function footerValue(col: number): string {
  if (!isNumCol(col)) return ''
  let sum = 0
  let seen = 0
  for (const row of filteredRows.value) {
    const n = parseNumericCell(row[col] ?? '')
    if (n === null) continue
    sum += n
    seen++
  }
  return seen > 0 ? formatNumber(sum) : ''
}

// ── 冻结列 ─────────────────────────────────────────────────────────────

function frozenStyle(col: number): Record<string, string> | undefined {
  return frozen.value === col ? { left: `${frozenLeft.value}px` } : undefined
}

let observer: ResizeObserver | null = null

function measureFrozen(): void {
  const col = frozen.value
  const row = tableRef.value?.querySelector('thead tr')
  if (col === null || !row) { frozenLeft.value = 0; return }
  let left = 0
  for (const cell of Array.from(row.querySelectorAll('th'))) {
    if (Number((cell as HTMLElement).dataset.col) === col) break
    left += cell.getBoundingClientRect().width
  }
  frozenLeft.value = left
}

watch([frozen, visibleColumns, pageRows, filterRowOpen, sort], () => {
  void nextTick(measureFrozen)
}, { deep: true })

onMounted(() => {
  if (typeof ResizeObserver === 'undefined' || !tableRef.value) return
  observer = new ResizeObserver(() => measureFrozen())
  observer.observe(tableRef.value)
  void nextTick(measureFrozen)
})

onBeforeUnmount(() => {
  observer?.disconnect()
  observer = null
  document.removeEventListener('mousedown', onDocMouseDown, true)
  window.removeEventListener('scroll', onWindowScroll, true)
})

// ── 显示列 / 冻结 / 数据条 ─────────────────────────────────────────────

function setMode(next: 'normal' | 'data'): void {
  mode.value = next
  closePop()
}

function toggleColumn(col: number, visible: boolean): void {
  const next = new Set(hidden.value)
  if (visible) {
    next.delete(col)
  } else {
    if (columnCount.value - next.size <= 1) return
    next.add(col)
    if (frozen.value === col) frozen.value = null
    if (sort.value?.column === col) sort.value = null
  }
  hidden.value = next
}

function showAllColumns(): void {
  hidden.value = new Set()
}

function hideColumn(col: number): void {
  toggleColumn(col, false)
  closePop()
}

function onlyColumn(col: number): void {
  hidden.value = new Set(
    Array.from({ length: columnCount.value }, (_, i) => i).filter(i => i !== col),
  )
  frozen.value = col
  closePop()
}

function toggleFreeze(col: number): void {
  frozen.value = frozen.value === col ? null : col
  closePop()
}

function toggleBars(col: number): void {
  const next = new Set(barsOff.value)
  if (next.has(col)) next.delete(col)
  else next.add(col)
  barsOff.value = next
  closePop()
}

// ── 列筛选 / 值筛选 ────────────────────────────────────────────────────

/** 列菜单顶部的关键词输入：输入即筛，浮层和焦点都不受影响。 */
function setColumnFilter(col: number, value: string): void {
  columnFilters.value[col] = value
}

function hasColumnFilter(col: number): boolean {
  return Boolean(columnFilters.value[col]?.trim()) || valueFilterCount(col) > 0
}

function valueFilterCount(col: number): number {
  return valueFilters.value.get(col)?.size ?? 0
}

function clearColumnFilter(col: number): void {
  columnFilters.value[col] = ''
  const next = new Map(valueFilters.value)
  next.delete(col)
  valueFilters.value = next
  closePop()
}

const valueQuery = ref('')
const popValues = computed(() => {
  const col = pop.value?.col
  return col === undefined ? [] : columnValueCounts(props.rows, col)
})
const visibleValues = computed(() => {
  const needle = valueQuery.value.trim().toLowerCase()
  if (!needle) return popValues.value
  return popValues.value.filter(item => item.value.toLowerCase().includes(needle))
})

function isValueChecked(value: string): boolean {
  const col = pop.value?.col
  if (col === undefined) return true
  const selected = valueFilters.value.get(col)
  return !selected || selected.size === 0 || selected.has(value)
}

function toggleValue(value: string): void {
  const col = pop.value?.col
  if (col === undefined) return
  const all = popValues.value.map(item => item.value)
  let selected = valueFilters.value.get(col)
  if (!selected || selected.size === 0) {
    // 从「全选」开始取消一个，等于「除了它都要」
    selected = new Set(all.filter(item => item !== value))
  } else {
    selected = new Set(selected)
    if (selected.has(value)) selected.delete(value)
    else selected.add(value)
  }
  const next = new Map(valueFilters.value)
  if (selected.size === all.length) next.delete(col)
  else next.set(col, selected)
  valueFilters.value = next
}

function clearValues(col: number): void {
  const next = new Map(valueFilters.value)
  next.delete(col)
  valueFilters.value = next
}

const popStats = computed(() => {
  const col = pop.value?.col ?? 0
  return computeColumnStats(props.rows, col, Boolean(numeric.value[col]))
})

/** 有数值列才显示合计行；纯文字表切到数据表也没有合计可算。 */
const hasNumericColumn = computed(() =>
  visibleColumns.value.some(col => isNumCol(col)),
)

async function openLandscape(event: MouseEvent): Promise<void> {
  const root = rootRef.value
  if (!root) return
  // 动态引入：这个模块带着 app 的 i18n 单例，静态引入会让所有只 mock 了
  // vue-i18n 一部分导出的组件测试在加载阶段就炸。
  const { openBlockLandscape } = await import('@/composables/useBlockLandscape')
  openBlockLandscape(root, event.currentTarget as HTMLElement)
}

function openCopyMenu(event: MouseEvent): void {
  openPop('copy', 0, event.currentTarget as HTMLElement)
}

/** 复制的是「当前看到的行与列」，数字保留原始写法。 */
function copyTable(format: 'markdown' | 'html' | 'tsv' | 'csv'): void {
  const cols = visibleColumns.value
  const headers = cols.map(col => headerLabel(col))
  const rows = filteredRows.value.map(row => cols.map(col => row[col] ?? ''))

  let text: string
  if (format === 'markdown') {
    const cell = (value: string) => value.replace(/\|/g, '\\|').replace(/\r?\n/g, '<br>')
    text = [
      `| ${headers.map(cell).join(' | ')} |`,
      `| ${headers.map(() => '---').join(' | ')} |`,
      ...rows.map(row => `| ${row.map(cell).join(' | ')} |`),
    ].join('\n')
  } else if (format === 'tsv') {
    text = [headers.join('\t'), ...rows.map(row => row.map(v => v.replace(/[\t\n]/g, ' ')).join('\t'))].join('\n')
  } else if (format === 'csv') {
    const cell = (value: string) => (/[",\n]/.test(value) ? `"${value.replace(/"/g, '""')}"` : value)
    text = [headers.map(cell).join(','), ...rows.map(row => row.map(cell).join(','))].join('\n')
  } else {
    const cell = (value: string) => value
      .replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;').replace(/"/g, '&quot;')
    text = `<table>\n<thead><tr>${headers.map(h => `<th>${cell(h)}</th>`).join('')}</tr></thead>\n`
      + `<tbody>${rows.map(row => `<tr>${row.map(v => `<td>${cell(v)}</td>`).join('')}</tr>`).join('')}</tbody>\n</table>`
  }

  closePop()
  copyText(
    text,
    () => toast.show(t('common.copied'), { icon: 'copy', type: 'success', duration: 1500 }),
    () => toast.show(t('common.operationFailed'), { type: 'error', icon: 'warning' }),
  )
}

// ── 复制单列 ───────────────────────────────────────────────────────────

function copyColumn(col: number): void {
  const text = filteredRows.value.map(row => row[col] ?? '').join('\n')
  closePop()
  copyText(
    text,
    () => toast.show(t('common.copied'), { icon: 'copy', type: 'success', duration: 1500 }),
    () => toast.show(t('common.operationFailed'), { type: 'error', icon: 'warning' }),
  )
}

// ── 浮层 ───────────────────────────────────────────────────────────────

function placePop(state: { x: number; y: number }): void {
  const el = popEl.value
  if (!el || !popAnchor) return
  const rect = popAnchor.getBoundingClientRect()
  const width = el.offsetWidth
  const height = el.offsetHeight
  const left = Math.max(8, Math.min(rect.left, window.innerWidth - width - 8))
  const below = rect.bottom + 4
  const top = below + height > window.innerHeight - 8
    ? Math.max(8, rect.top - height - 4)
    : below
  state.x = left
  state.y = top
}

function openPop(kind: 'menu' | 'values' | 'stats' | 'columns' | 'copy', col: number, anchor: HTMLElement): void {
  closePop()
  popAnchor = anchor
  const rect = anchor.getBoundingClientRect()
  const state = { kind, col, x: rect.left, y: rect.bottom + 4 }
  pop.value = state
  if (kind === 'values') valueQuery.value = ''
  void nextTick(() => { if (pop.value) placePop(pop.value) })
}

function closePop(): void {
  pop.value = null
  popAnchor = null
}

function openColumnMenu(event: MouseEvent, col: number): void {
  openPop('menu', col, event.currentTarget as HTMLElement)
}

function openValues(col: number): void {
  const anchor = popAnchor
  if (!anchor) return
  openPop('values', col, anchor)
}

function openStats(col: number): void {
  const anchor = popAnchor
  if (!anchor) return
  openPop('stats', col, anchor)
}

function openColumns(event: MouseEvent): void {
  openPop('columns', 0, event.currentTarget as HTMLElement)
}

function onDocMouseDown(event: MouseEvent): void {
  if (!pop.value) return
  const target = event.target as Node | null
  if (target && popEl.value?.contains(target)) return
  if (target && popAnchor?.contains(target)) return
  closePop()
}

function onWindowScroll(): void {
  if (!pop.value || !popAnchor) return
  const rect = popAnchor.getBoundingClientRect()
  const gone = rect.bottom < 0 || rect.top > window.innerHeight
    || rect.right < 0 || rect.left > window.innerWidth
  if (gone) closePop()
  else placePop(pop.value)
}

onMounted(() => {
  document.addEventListener('mousedown', onDocMouseDown, true)
  window.addEventListener('scroll', onWindowScroll, true)
})

// ── 对外 ───────────────────────────────────────────────────────────────

function focusSearch(): void {
  // 普通表模式没有搜索框（那是数据表的能力）。文件查看器的搜索按钮 / Ctrl+F
  // 是明确的搜索意图，切到数据表再聚焦，比静默无反应好。
  if (!isData.value) mode.value = 'data'
  void nextTick(() => {
    searchRef.value?.focus()
    searchRef.value?.select()
  })
}

function closeSearch(): void {
  query.value = ''
  searchRef.value?.blur()
}

defineExpose({ focusSearch, closeSearch })
</script>

<style scoped>
.dt {
  display: flex;
  flex-direction: column;
  min-width: 0;
  min-height: 0;
  background: var(--bg-primary);
}

.dt.is-fill {
  flex: 1;
}

/* ── 工具条 ─────────────────────────────────────────────────────────── */

.dt-bar {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: var(--space-3);
  flex-shrink: 0;
  padding: var(--space-3) var(--space-4);
  border-bottom: 1px solid var(--border-color);
  background: var(--bg-primary);
}

/* ── 表头栏：标题位置就是「简单表 / 数据表」的切换器 ─────────────── */

.dt-head {
  display: flex;
  align-items: center;
  gap: var(--space-2);
  flex-shrink: 0;
  min-height: calc(24 * var(--dp));
  padding: 0 var(--space-3);
  background: var(--code-bg);
  border-bottom: 1px solid var(--border-color);
  user-select: none;
}

.dt-head-spacer {
  flex: 1 1 auto;
}

/* 尺寸跟原来的「表格」标题一致：小字号、无边框、无大背景。 */
.dt-seg {
  display: inline-flex;
  align-items: center;
  gap: 1px;
  padding: 1px;
  border: 0;
  border-radius: var(--radius-xs);
  background: transparent;
}

.dt-seg button {
  padding: 0 calc(4 * var(--dp));
  border: 0;
  border-radius: var(--radius-xs);
  background: transparent;
  color: var(--text-muted);
  font-family: inherit;
  font-size: var(--font-size-xs);
  line-height: 1.6;
  cursor: pointer;
  white-space: nowrap;
}

.dt-seg button:hover {
  color: var(--text-secondary);
}

.dt-seg button.is-on {
  background: var(--bg-tertiary);
  color: var(--text-primary);
}

.dt-actions {
  display: flex;
  align-items: center;
  gap: 1px;
}

.dt-act {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  gap: 1px;
  height: calc(20 * var(--dp));
  min-width: calc(20 * var(--dp));
  padding: 0 calc(3 * var(--dp));
  border: 0;
  border-radius: var(--radius-xs);
  background: transparent;
  color: var(--text-muted);
  cursor: pointer;
}

.dt-act:hover {
  background: var(--bg-tertiary);
  color: var(--text-secondary);
}

.dt-act.is-on {
  color: var(--accent-color);
}

.dt-caret {
  opacity: 0.6;
}

.dt-seg button:focus-visible,
.dt-tool:focus-visible,
.dt-page-btn:focus-visible {
  outline: 2px solid var(--accent-color);
  outline-offset: 1px;
}

.dt-search {
  position: relative;
  display: flex;
  align-items: center;
  flex: 1 1 calc(120 * var(--dp));
  max-width: calc(260 * var(--dp));
  min-width: calc(100 * var(--dp));
}

.dt-search-icon {
  position: absolute;
  left: var(--space-4);
  color: var(--text-muted);
  pointer-events: none;
}

.dt-search input {
  width: 100%;
  box-sizing: border-box;
  padding: calc(3 * var(--dp)) calc(22 * var(--dp)) calc(3 * var(--dp)) calc(24 * var(--dp));
  border: 1px solid var(--border-color);
  border-radius: var(--radius-sm);
  background: var(--bg-secondary);
  color: var(--text-primary);
  font-family: inherit;
  font-size: var(--font-size-sm);
  line-height: var(--input-line-height, normal);
  outline: none;
}

.dt-search input:focus {
  border-color: var(--accent-color);
}

.dt-search input::-webkit-search-cancel-button,
.dt-search input::-webkit-search-decoration {
  -webkit-appearance: none;
  appearance: none;
  display: none;
}

.dt-search-clear {
  position: absolute;
  right: 2px;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: calc(16 * var(--dp));
  height: calc(16 * var(--dp));
  padding: 0;
  border: 0;
  border-radius: var(--radius-full);
  background: transparent;
  color: var(--text-secondary);
  cursor: pointer;
}

.dt-tool {
  display: inline-flex;
  align-items: center;
  gap: var(--space-2);
  padding: calc(3 * var(--dp)) var(--space-4);
  border: 1px solid var(--border-color);
  border-radius: var(--radius-sm);
  background: transparent;
  color: var(--text-secondary);
  font-family: inherit;
  font-size: var(--font-size-sm);
  line-height: 1.4;
  cursor: pointer;
  white-space: nowrap;
}

.dt-tool.is-on {
  border-color: var(--accent-color);
  color: var(--accent-color);
}

.dt-badge {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  min-width: calc(13 * var(--dp));
  height: calc(13 * var(--dp));
  padding: 0 calc(3 * var(--dp));
  border-radius: var(--radius-full);
  background: var(--accent-color);
  color: #fff;
  font-size: var(--font-size-2xs);
  line-height: 1;
}

.dt-count {
  margin-left: auto;
  color: var(--text-secondary);
  font-size: var(--font-size-sm);
  font-variant-numeric: tabular-nums;
  white-space: nowrap;
}

/* ── 网格 ───────────────────────────────────────────────────────────── */

.dt-scroll {
  flex: 1;
  min-height: 0;
  overflow: auto;
  overscroll-behavior: contain;
}

.dt-grid {
  width: max-content;
  min-width: 100%;
  border-collapse: separate;
  border-spacing: 0;
  font-size: var(--font-size-sm);
}

.dt .dt-grid th,
.dt .dt-grid td {
  padding: calc(5 * var(--dp)) var(--space-4);
  border-bottom: 1px solid var(--border-color);
  text-align: left;
  vertical-align: top;
  background: var(--bg-primary);
}

.dt.is-data .dt-grid th {
  position: sticky;
  top: 0;
  z-index: 2;
  padding-right: calc(26 * var(--dp));
  background: var(--bg-secondary);
  color: var(--text-secondary);
  font-weight: var(--font-weight-medium);
  white-space: nowrap;
  cursor: pointer;
  user-select: none;
}

.dt.is-data .dt-grid th:hover {
  background: var(--bg-tertiary);
}

.dt.is-data .dt-grid td {
  padding-right: calc(26 * var(--dp));
}

.dt-th-label {
  display: inline-block;
  max-width: calc(200 * var(--dp));
  overflow: hidden;
  text-overflow: ellipsis;
  vertical-align: bottom;
}

.is-data .dt-arrow {
  position: absolute;
  right: calc(18 * var(--dp));
  top: 50%;
  transform: translateY(-50%);
  color: var(--text-muted);
  font-size: var(--font-size-2xs);
  opacity: 0;
  transition: opacity var(--duration-base);
}

.dt.is-data .dt-grid th:hover .dt-arrow {
  opacity: 1;
}

.dt.is-data .dt-grid th.is-sorted {
  color: var(--text-primary);
}

.dt.is-data .dt-grid th.is-sorted .dt-arrow {
  color: var(--accent-color);
  opacity: 1;
}

.dt-funnel {
  position: absolute;
  right: calc(2 * var(--dp));
  top: 50%;
  transform: translateY(-50%);
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: calc(15 * var(--dp));
  height: calc(18 * var(--dp));
  padding: 0;
  border: 0;
  border-radius: calc(3 * var(--dp));
  background: transparent;
  color: var(--text-muted);
  cursor: pointer;
  opacity: 0.55;
}

.dt-funnel:hover {
  background: var(--bg-hover);
  color: var(--text-primary);
  opacity: 1;
}

.dt-funnel.is-on {
  color: var(--accent-color);
  opacity: 1;
}

/* 每列筛选行 */
.dt-filter-cell {
  position: sticky;
  top: calc(28 * var(--dp));
  z-index: 2;
  height: calc(28 * var(--dp));
  padding: 0 !important;
  background: var(--bg-secondary) !important;
  border-bottom: 1px solid var(--border-color);
}

.dt-filter-input {
  display: block;
  width: 100%;
  min-width: 0;
  box-sizing: border-box;
  padding: 0 var(--space-4);
  border: 0;
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

.dt-filter-input::placeholder {
  color: var(--text-muted);
}

.dt-filter-input:focus {
  box-shadow: inset 0 calc(-2 * var(--dp)) 0 var(--accent-color);
}

/* 单元格 */
.dt .dt-grid td {
  position: relative;
  color: var(--text-primary);
}

.dt.is-data .dt-row:hover td {
  background: var(--bg-secondary);
}

.dt-cell {
  position: relative;
  z-index: 1;
  display: block;
  max-width: calc(320 * var(--dp));
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

/* 普通表保持 markdown 表格的老样子：允许换行、宽度跟内容走。
   `white-space: inherit` 让 td 上的「自动换行」开关继续生效。 */
.dt:not(.is-data) .dt-cell {
  display: inline;
  max-width: none;
  overflow: visible;
  text-overflow: clip;
  white-space: inherit;
  word-break: inherit;
}

.dt-cell-bar {
  position: absolute;
  left: 0;
  top: calc(3 * var(--dp));
  bottom: calc(3 * var(--dp));
  z-index: 0;
  border-radius: 0 calc(3 * var(--dp)) calc(3 * var(--dp)) 0;
  background: color-mix(in srgb, var(--accent-color) 18%, transparent);
  pointer-events: none;
}

.is-numeric {
  text-align: right;
}

.is-numeric .dt-cell {
  font-variant-numeric: tabular-nums;
}

/* 冻结列 */
.dt.is-data .dt-grid th.is-frozen,
.dt.is-data .dt-grid .dt-filter-cell.is-frozen {
  z-index: 4;
  background: var(--bg-secondary);
}

.dt.is-data .dt-grid td.is-frozen {
  position: sticky;
  z-index: 3;
  background: var(--bg-primary);
  box-shadow: 1px 0 0 var(--border-color);
}

.dt.is-data .dt-row:hover td.is-frozen {
  background: var(--bg-secondary);
}

.dt-empty {
  padding: calc(30 * var(--dp)) var(--space-6) !important;
  color: var(--text-secondary);
  font-size: var(--font-size-sm);
  text-align: center;
}

/* 合计行 */
.dt .dt-grid tfoot td {
  position: sticky;
  bottom: 0;
  z-index: 2;
  border-top: 1px solid var(--border-color);
  border-bottom: 0;
  background: var(--bg-secondary);
  color: var(--text-primary);
  font-weight: var(--font-weight-medium);
}

/* ── 普通表模式 ─────────────────────────────────────────────────────── */

.dt:not(.is-data) .dt-grid th,
.dt:not(.is-data) .dt-grid td {
  padding-right: var(--space-4);
  border: 1px solid var(--border-color);
  background: transparent;
}

.dt:not(.is-data) .dt-grid th {
  background: var(--bg-secondary);
  color: var(--text-primary);
  font-weight: var(--font-weight-semibold);
  cursor: default;
}

/* 简单表的换行开关。聊天里外层 `.table-block-wrapper th/td` 的规则带
   `!important`，这里必须用更高特异性 + `!important` 才能压过它。 */
.dt.no-wrap:not(.is-data) .dt-grid th,
.dt.no-wrap:not(.is-data) .dt-grid td {
  white-space: nowrap !important;
}

.dt:not(.is-data) .is-numeric {
  text-align: left;
}

.dt:not(.is-data) .is-numeric .dt-cell {
  font-variant-numeric: normal;
}

.dt:not(.is-data) .dt-grid tfoot,
.dt:not(.is-data) .dt-grid .dt-filter-row {
  display: none;
}

/* ── 分页 ───────────────────────────────────────────────────────────── */

.dt-pager {
  display: flex;
  align-items: center;
  justify-content: center;
  gap: var(--space-4);
  flex-shrink: 0;
  padding: var(--space-3) var(--space-4);
  border-top: 1px solid var(--border-color);
}

.dt-page-btn {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: calc(24 * var(--dp));
  height: calc(24 * var(--dp));
  padding: 0;
  border: 1px solid var(--border-color);
  border-radius: var(--radius-sm);
  background: transparent;
  color: var(--text-secondary);
  cursor: pointer;
}

.dt-page-btn:disabled {
  opacity: 0.4;
  cursor: default;
}

.dt-page-info {
  color: var(--text-secondary);
  font-size: var(--font-size-sm);
  font-variant-numeric: tabular-nums;
}

/* ── 浮层 ───────────────────────────────────────────────────────────── */

.dt-pop {
  position: fixed;
  z-index: var(--z-popover, 60);
  min-width: calc(180 * var(--dp));
  max-width: calc(240 * var(--dp));
  max-height: min(72vh, calc(440 * var(--dp)));
  overflow: auto;
  padding: var(--space-1);
  border: 1px solid var(--border-color);
  border-radius: var(--radius-md);
  background: var(--bg-secondary);
  box-shadow: var(--shadow-md);
  color: var(--text-primary);
  font-size: var(--font-size-sm);
}

.dt-pop-head {
  padding: var(--space-2) var(--space-3);
  overflow: hidden;
  color: var(--text-secondary);
  font-size: var(--font-size-xs);
  text-overflow: ellipsis;
  white-space: nowrap;
}

.dt-pop-item {
  display: flex;
  align-items: center;
  gap: var(--space-3);
  width: 100%;
  padding: calc(5 * var(--dp)) var(--space-3);
  border: 0;
  border-radius: var(--radius-sm);
  background: transparent;
  color: var(--text-primary);
  font-family: inherit;
  font-size: var(--font-size-sm);
  text-align: left;
  white-space: nowrap;
  cursor: pointer;
}

.dt-pop-item:hover {
  background: var(--bg-hover);
}

.dt-pop-item:disabled {
  color: var(--text-muted);
  background: transparent;
  cursor: default;
}

.dt-pop-icon {
  flex-shrink: 0;
  color: var(--text-secondary);
}

.dt-pop-mini {
  margin-left: auto;
  color: var(--text-muted);
  font-size: var(--font-size-2xs);
  font-variant-numeric: tabular-nums;
}

.dt-pop-sep {
  height: 1px;
  margin: var(--space-1) calc(2 * var(--dp));
  background: var(--border-color);
}

.dt-pop-filter {
  padding: 0 var(--space-2) var(--space-1);
}

.dt-pop-filter input {
  width: 100%;
  box-sizing: border-box;
  padding: calc(3 * var(--dp)) var(--space-3);
  border: 1px solid var(--border-color);
  border-radius: var(--radius-sm);
  background: var(--bg-primary);
  color: var(--text-primary);
  font-family: inherit;
  font-size: var(--font-size-sm);
  outline: none;
}

.dt-pop-filter input:focus {
  border-color: var(--accent-color);
}

.dt-pop-search {
  padding: 0 var(--space-2) var(--space-2);
}

.dt-pop-search input {
  width: 100%;
  box-sizing: border-box;
  padding: calc(3 * var(--dp)) var(--space-3);
  border: 1px solid var(--border-color);
  border-radius: var(--radius-sm);
  background: var(--bg-primary);
  color: var(--text-primary);
  font-family: inherit;
  font-size: var(--font-size-sm);
  outline: none;
}

.dt-pop-search input:focus {
  border-color: var(--accent-color);
}

.dt-pop-list {
  max-height: calc(220 * var(--dp));
  overflow: auto;
}

.dt-pop-check {
  display: flex;
  align-items: center;
  gap: var(--space-3);
  padding: calc(4 * var(--dp)) var(--space-3);
  border-radius: var(--radius-sm);
  cursor: pointer;
}

.dt-pop-check:hover {
  background: var(--bg-hover);
}

.dt-pop-check input {
  flex-shrink: 0;
  margin: 0;
  accent-color: var(--accent-color);
}

.dt-pop-value {
  flex: 1;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.dt-pop-count {
  color: var(--text-muted);
  font-size: var(--font-size-2xs);
  font-variant-numeric: tabular-nums;
}

.dt-pop-empty {
  padding: var(--space-4) var(--space-3);
  color: var(--text-muted);
  text-align: center;
}

.dt-pop-foot {
  display: flex;
  gap: var(--space-2);
  margin-top: var(--space-1);
  padding: var(--space-2) var(--space-2) 0;
  border-top: 1px solid var(--border-color);
}

.dt-pop-btn {
  flex: 1;
  padding: calc(4 * var(--dp)) var(--space-3);
  border: 1px solid var(--border-color);
  border-radius: var(--radius-sm);
  background: transparent;
  color: var(--text-secondary);
  font-family: inherit;
  font-size: var(--font-size-sm);
  cursor: pointer;
}

.dt-pop-btn:disabled {
  opacity: 0.5;
  cursor: default;
}

.dt-pop-btn.is-primary {
  border-color: color-mix(in srgb, var(--accent-color) 40%, transparent);
  background: color-mix(in srgb, var(--accent-color) 14%, transparent);
  color: var(--accent-color);
}

.dt-stat-sec {
  padding: var(--space-2) var(--space-3) 2px;
  color: var(--accent-color);
  font-size: var(--font-size-2xs);
}

.dt-stat-list {
  margin: 0;
  padding: 0;
}

.dt-stat-row {
  display: flex;
  justify-content: space-between;
  gap: var(--space-5);
  padding: 2px var(--space-3);
}

.dt-stat-row dt {
  color: var(--text-secondary);
}

.dt-stat-row dd {
  margin: 0;
  font-variant-numeric: tabular-nums;
}

.dt-stat-hint {
  margin: 0;
  padding: var(--space-2) var(--space-3) var(--space-3);
  color: var(--text-muted);
  font-size: var(--font-size-xs);
}

@media (hover: hover) {
  .dt-tool:hover:not(.is-on) {
    border-color: var(--text-secondary);
    color: var(--text-primary);
  }

  .dt-search-clear:hover {
    background: var(--bg-tertiary);
    color: var(--text-primary);
  }

  .dt-page-btn:not(:disabled):hover {
    border-color: var(--accent-color);
    color: var(--accent-color);
  }
}
</style>
