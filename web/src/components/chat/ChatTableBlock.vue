<template>
  <div class="table-block-wrapper is-data-table">
    <DataTable :headers="headers" :rows="rows" max-height="min(60vh, 420px)" />
  </div>
</template>

<script setup lang="ts">
/**
 * 聊天消息里的表格块。
 *
 * 只负责外壳（外边距、圆角），表格和它上方的表头栏都由 DataTable 渲染 ——
 * 和 CSV 预览、markdown 预览是同一份实现。
 */
import DataTable from '@/components/common/DataTable.vue'

defineProps<{
  headers: string[]
  rows: string[][]
}>()
</script>
