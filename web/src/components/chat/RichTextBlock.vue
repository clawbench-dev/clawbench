<template>
  <template v-for="(segment, index) in segments" :key="index">
    <div v-if="segment.kind === 'html'" class="md-seg" v-html="segment.html"></div>
    <ChatTableBlock v-else :headers="segment.headers" :rows="segment.rows" />
  </template>
</template>

<script setup lang="ts">
/**
 * 消息正文：把「数字表」交给 DataTable 组件，其余 HTML 原样渲染。
 *
 * 流式输出期间不拆（每来一小段都重新解析一遍太贵），定稿后再拆。
 * 没有表格的正文仍然只有一个 div，DOM 结构和以前完全一样。
 */
import { computed } from 'vue'
import ChatTableBlock from './ChatTableBlock.vue'
import { splitTableBlocks } from '@/utils/tableBlockSplit'

const props = defineProps<{
  html: string
  /** 正在流式输出：跳过拆分，整段直接渲染。 */
  streaming?: boolean
}>()

const segments = computed(() => splitTableBlocks(props.html, !props.streaming))
</script>
