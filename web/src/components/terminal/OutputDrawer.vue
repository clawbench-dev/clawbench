<template>
  <BottomSheet :open="open" auto :title="t('terminal.copyOutput')" @close="handleClose">
    <template #header>
      <FileTextIcon :size="16" class="bs-header-icon" />
      <span class="bs-header-title">{{ t('terminal.copyOutput') }}</span>
    </template>

    <div class="od-body">
      <pre class="od-text hljs" :style="{ fontSize: fontSize + 'px' }" v-html="highlightedHtml"></pre>
    </div>
  </BottomSheet>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { FileText as FileTextIcon } from 'lucide-vue-next'
import BottomSheet from '@/components/common/BottomSheet.vue'
import { hljs } from '@/utils/globals.ts'
import { escapeHtml } from '@/utils/html.ts'

const props = defineProps<{
  open: boolean
  outputText: string
  fontSize: number
}>()

const emit = defineEmits<{
  close: []
}>()

const { t } = useI18n()

const highlightedHtml = computed(() => {
  if (!props.outputText) return ''
  try {
    return hljs.highlight(props.outputText, { language: 'bash', ignoreIllegals: true }).value
  } catch {
    return escapeHtml(props.outputText)
  }
})

function handleClose() {
  emit('close')
}
</script>

<style scoped>
.od-body {
  flex: 1;
  overflow: auto;
  padding: 0;
  -webkit-overflow-scrolling: touch;
}

.od-text {
  margin: 0;
  padding: 12px;
  font-family: 'JetBrains Mono', 'Fira Code', 'Cascadia Code', monospace;
  font-size: 13px;
  line-height: 1.5;
  white-space: pre-wrap;
  word-break: break-all;
  background: transparent;
  user-select: text;
  -webkit-user-select: text;
}
</style>
