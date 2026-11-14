<template>
  <div class="excel-table">
    <div v-if="notices.length" class="table-notices">
      <div v-for="notice in notices" :key="notice" class="table-notice">
        <AlertTriangle :size="13" />
        <span>{{ notice }}</span>
      </div>
    </div>

    <!-- 工作表页签由父级统一那一条工具栏渲染（和「表格 / 原样」同一行） -->

    <div v-if="loading" class="excel-state">
      <PanelSkeleton class="excel-skeleton" variant="text" :rows="10" :label="t('common.loading')" />
    </div>

    <div v-else-if="error" class="excel-state excel-error">
      <FileX :size="40" />
      <div class="excel-error-title">{{ t('file.excel.parseFailed') }}</div>
      <div class="excel-error-desc">{{ error }}</div>
      <button type="button" class="excel-retry-btn" @click="load">{{ t('common.retry') }}</button>
    </div>

    <DataTable v-else-if="active" ref="tableRef" :headers="active.headers" :rows="active.rows" fill />

    <div v-else class="excel-state excel-empty">{{ t('file.table.empty') }}</div>
  </div>
</template>

<script setup lang="ts">
/**
 * Excel（.xlsx）表格视图。
 *
 * 和 CSV 预览共用 DataTable：搜索 / 筛选 / 排序 / 冻结 / 合计 / 复制都一样。
 * 代价是只取值，格式（颜色、合并、列宽、图表）都不保留 —— 要看原样就切回
 * 「原样」渲染（由 OfficePreview 那个开关负责）。
 */
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { AlertTriangle, FileX } from 'lucide-vue-next'
import DataTable from '@/components/common/DataTable.vue'
import PanelSkeleton from '@/components/common/PanelSkeleton.vue'
import { appLog } from '@/utils/appLog'
import { MAX_SHEET_COLUMNS, MAX_SHEET_ROWS, parseWorkbook, type SheetTable } from '@/utils/sheetRows'

const TAG = 'ExcelTableBody'

const props = defineProps<{
  /** 文件下载地址（文件查看器与分享页各自解析好的 URL）。 */
  src: string
  /** 当前显示第几张工作表。页签在父级那条工具栏里，这里只认序号。 */
  activeSheet?: number
}>()

const emit = defineEmits<{
  /** 解析完成后把工作表名交给父级渲染页签。 */
  sheets: [names: string[]]
  'update:activeSheet': [index: number]
}>()

const { t } = useI18n()

const loading = ref(true)
const error = ref('')
const sheets = ref<SheetTable[]>([])
const activeIndex = computed(() => props.activeSheet ?? 0)

const active = computed<SheetTable | null>(() => sheets.value[activeIndex.value] ?? sheets.value[0] ?? null)

const notices = computed(() => {
  const list: string[] = []
  if (active.value?.rowsTruncated) list.push(t('file.excel.rowsTruncated', { n: MAX_SHEET_ROWS }))
  if (active.value?.columnsTruncated) list.push(t('file.table.columnsTruncated', { n: MAX_SHEET_COLUMNS }))
  return list
})

const tableRef = ref<InstanceType<typeof DataTable> | null>(null)

/** 每次加载一个代号，切文件/重试时旧请求的结果直接丢掉。 */
let loadSeq = 0

async function load(): Promise<void> {
  const token = ++loadSeq
  loading.value = true
  error.value = ''
  sheets.value = []
  emit('sheets', [])

  try {
    const response = await fetch(props.src)
    if (!response.ok) throw new Error(`${response.status} ${response.statusText}`)
    const buffer = await response.arrayBuffer()
    const parsed = await parseWorkbook(buffer)
    if (token !== loadSeq) return
    sheets.value = parsed
    emit('sheets', parsed.map(sheet => sheet.name))
    if (activeIndex.value >= parsed.length) emit('update:activeSheet', 0)
  } catch (err) {
    if (token !== loadSeq) return
    appLog.e(TAG, 'Failed to parse workbook:', err)
    error.value = err instanceof Error ? err.message : String(err)
  } finally {
    if (token === loadSeq) loading.value = false
  }
}

watch(() => props.src, load, { immediate: true })
</script>

<style scoped>
.excel-table {
  display: flex;
  flex: 1;
  flex-direction: column;
  min-height: 0;
  min-width: 0;
  background: var(--bg-primary);
}

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

.excel-state {
  display: flex;
  flex: 1;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: var(--space-4);
  padding: calc(32 * var(--dp)) var(--space-6);
  color: var(--text-muted);
  text-align: center;
}

.excel-skeleton {
  width: 100%;
  max-width: calc(720 * var(--dp));
}

.excel-error-title {
  font-size: var(--font-size-lg);
  font-weight: var(--font-weight-medium);
  color: var(--text-primary);
}

.excel-error-desc {
  max-width: calc(400 * var(--dp));
  font-size: var(--font-size-sm);
  overflow-wrap: anywhere;
}

.excel-retry-btn {
  padding: var(--space-3) var(--space-7);
  border: 1px solid var(--border-color);
  border-radius: var(--radius-lg);
  background: transparent;
  color: var(--text-secondary);
  font-size: var(--font-size-md);
  cursor: pointer;
}

@media (hover: hover) {
  .excel-retry-btn:hover {
    border-color: var(--accent-color);
    color: var(--accent-color);
  }
}
</style>

<style>
[data-theme-base='dark'] .table-notice {
  background: rgba(245, 158, 11, 0.15);
  color: #fbbf24;
  border-bottom-color: rgba(245, 158, 11, 0.3);
}
</style>
