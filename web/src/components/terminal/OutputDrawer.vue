<template>
  <BottomSheet :open="open" auto :title="t('terminal.copyOutput')" @close="handleClose">
    <template #header>
      <FileTextIcon :size="16" class="bs-header-icon" />
      <span class="bs-header-title">{{ t('terminal.copyOutput') }}</span>
    </template>

    <div class="od-body">
      <pre class="od-text hljs" v-html="highlightedHtml"></pre>
    </div>

    <template #footer>
      <div class="od-footer">
        <button class="od-btn od-btn-cancel" @click="handleClose">
          {{ t('common.cancel') }}
        </button>
        <button class="od-btn od-btn-copy" @click="handleCopyAll">
          {{ t('common.copy') }}
        </button>
      </div>
    </template>
  </BottomSheet>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { FileText as FileTextIcon } from 'lucide-vue-next'
import BottomSheet from '@/components/common/BottomSheet.vue'
import { useToast } from '@/composables/useToast'
import { copyText } from '@/utils/clipboard'
import { hljs } from '@/utils/globals.ts'
import { escapeHtml } from '@/utils/html.ts'

const props = defineProps<{
  open: boolean
  outputText: string
}>()

const emit = defineEmits<{
  close: []
}>()

const { t } = useI18n()
const toast = useToast()

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

function handleCopyAll() {
  if (props.outputText) {
    copyText(props.outputText, () => {
      toast.show(t('common.copied'), { type: 'success', duration: 1500 })
    })
  }
}
</script>

<style scoped>
.od-body {
  flex: 1;
  overflow: auto;
  padding: 4px 0;
  -webkit-overflow-scrolling: touch;
}

.od-text {
  margin: 0;
  padding: 0 12px;
  font-family: 'JetBrains Mono', 'Fira Code', 'Cascadia Code', monospace;
  font-size: 13px;
  line-height: 1.5;
  white-space: pre;
  background: transparent;
  user-select: text;
  -webkit-user-select: text;
}

.od-footer {
  display: flex;
  align-items: center;
  justify-content: flex-end;
  gap: 8px;
  width: 100%;
}

.od-btn {
  padding: 8px 20px;
  border: none;
  border-radius: var(--radius-sm, 6px);
  font-size: 14px;
  font-weight: 500;
  cursor: pointer;
  font-family: inherit;
  -webkit-tap-highlight-color: transparent;
  transition: opacity 0.15s;
}

.od-btn:active {
  opacity: 0.7;
}

.od-btn-cancel {
  background: var(--bg-tertiary, #eee);
  color: var(--text-primary, #1a1a1a);
}

.od-btn-copy {
  background: var(--accent, #4f8ef7);
  color: #fff;
}
</style>
