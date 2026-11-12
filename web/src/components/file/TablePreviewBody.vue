<template>
  <div class="table-preview">
    <div v-if="notices.length" class="table-notices">
      <div v-for="notice in notices" :key="notice" class="table-notice">
        <AlertTriangle :size="13" />
        <span>{{ notice }}</span>
      </div>
    </div>
    <DataTable ref="tableRef" :headers="parsed.headers" :rows="parsed.rows" fill />
  </div>
</template>

<script setup lang="ts">
/**
 * CSV / TSV 文件预览。
 *
 * 只做「解析文件 + 提示」两件事，表格本身完全交给 DataTable —— 和聊天里的
 * markdown 表格是同一个组件、同一套交互（排序 / 筛选 / 统计 / 显示列 / 冻结 /
 * 数据条 / 合计），改一处两边都生效。
 */
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { AlertTriangle } from 'lucide-vue-next'
import DataTable from '@/components/common/DataTable.vue'
import { MAX_TABLE_COLUMNS, parseTable } from '@/utils/csvTable'

const props = defineProps<{
  file: { name?: string; path?: string; content?: string | null; truncated?: boolean } | null
}>()

const { t } = useI18n()

const parsed = computed(() => parseTable(props.file?.content ?? '', props.file?.name ?? ''))

const notices = computed(() => {
  const list: string[] = []
  if (props.file?.truncated) list.push(t('file.viewer.truncated'))
  if (parsed.value.columnsTruncated) list.push(t('file.table.columnsTruncated', { n: MAX_TABLE_COLUMNS }))
  return list
})

const tableRef = ref<InstanceType<typeof DataTable> | null>(null)

/** 文件查看器的搜索按钮 / Ctrl+F 走这里。 */
function focusSearch(): void {
  tableRef.value?.focusSearch()
}

function closeSearch(): void {
  tableRef.value?.closeSearch()
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
</style>

<style>
[data-theme-base="dark"] .table-notice {
  background: rgba(245, 158, 11, 0.15);
  color: #fbbf24;
  border-bottom-color: rgba(245, 158, 11, 0.3);
}
</style>
