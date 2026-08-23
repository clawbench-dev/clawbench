<template>
  <BottomSheet :open="open" auto :title="t('terminal.input')" @close="$emit('close')">
    <template #header>
      <PenLineIcon :size="16" class="bs-header-icon" />
      <span class="bs-header-title">{{ t('terminal.input') }}</span>
      <button class="ti-btn ti-btn-fill" @click.stop="fillFromClipboard" :title="t('terminal.inputFillClipboard')">
        <ClipboardPasteIcon :size="16" />
      </button>
      <button class="ti-btn ti-btn-clear" @click.stop="clearText" :disabled="!text" :title="t('terminal.inputClear')">
        <EraserIcon :size="16" />
      </button>
      <button class="ti-btn ti-btn-send" @click.stop="doInput" :disabled="!text" :title="t('terminal.inputSend')">
        <SendHorizontalIcon :size="16" />
      </button>
    </template>

    <div class="ti-content">
      <textarea
        ref="textareaRef"
        v-model="text"
        class="ti-textarea"
        :placeholder="t('terminal.inputPlaceholder')"
        rows="8"
        @keydown.enter.exact.prevent="doInput"
      />
    </div>
  </BottomSheet>
</template>

<script setup lang="ts">
import { ref, watch, nextTick } from 'vue'
import { useI18n } from 'vue-i18n'
import BottomSheet from '@/components/common/BottomSheet.vue'
import { PenLine as PenLineIcon, ClipboardPaste as ClipboardPasteIcon, SendHorizontal as SendHorizontalIcon, Eraser as EraserIcon } from 'lucide-vue-next'
import { readClipboardText } from '@/utils/clipboard'
import { useToast } from '@/composables/useToast'

const props = defineProps({
  open: Boolean,
})

const emit = defineEmits(['close', 'input'])

const { t } = useI18n()
const toast = useToast()

const text = ref('')
const textareaRef = ref<HTMLElement | null>(null)

watch(() => props.open, (isOpen) => {
  if (!isOpen) return
  text.value = ''
  nextTick(() => textareaRef.value?.focus())
})

async function fillFromClipboard() {
  try {
    const clip = await readClipboardText()
    if (!clip) {
      toast.show(t('terminal.clipboardEmpty'), { icon: '📋', type: 'info' })
      return
    }
    text.value = clip
    nextTick(() => textareaRef.value?.focus())
  } catch {
    toast.show(t('terminal.clipboardReadFailed'), { icon: '❌', type: 'error' })
  }
}

function clearText() {
  text.value = ''
  nextTick(() => textareaRef.value?.focus())
}

function doInput() {
  const value = text.value
  if (!value) return
  emit('input', value)
  emit('close')
}
</script>

<style>
.ti-content {
  padding: 4px 14px 14px;
}

.ti-textarea {
  display: block;
  width: 100%;
  box-sizing: border-box;
  resize: none;
  border: none;
  border-radius: 0;
  background: transparent;
  color: var(--text-primary, #1a1a1a);
  font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;
  font-size: 13px;
  line-height: 1.6;
  padding: 8px 0;
  outline: none;
}

.ti-textarea::placeholder {
  color: var(--text-muted, #999);
}

/* Icon-only buttons: no shape, no background — just the icon */
.ti-btn {
  margin-left: 2px;
  border: none;
  background: none;
  padding: 4px;
  color: var(--text-muted, #999);
  cursor: pointer;
  display: flex;
  align-items: center;
  justify-content: center;
  line-height: 0;
  transition: color 0.15s;
}

@media (hover: hover) {
  .ti-btn:hover {
    color: var(--text-primary, #1a1a1a);
  }
}

.ti-btn:disabled {
  opacity: 0.3;
  cursor: default;
  color: var(--text-muted, #999);
}

.ti-btn-send {
  margin-left: auto;
  color: var(--accent-color, #0066cc);
}

@media (hover: hover) {
  .ti-btn-send:hover {
    color: var(--accent-color, #0066cc);
  }
}
</style>
