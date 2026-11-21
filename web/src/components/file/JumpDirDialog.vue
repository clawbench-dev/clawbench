<template>
  <ModalDialog :open="open" :title="t('jump.title')" :z-index="2600" @close="$emit('close')">
    <!-- Same glyph as the button that opens this dialog, so the two read as one
         action. Every other ModalDialog consumer supplies a header icon; without
         one this is the only dialog whose title is bare text. -->
    <template #header>
      <CornerUpRight :size="16" class="modal-header-icon" />
      <span class="modal-title">{{ t('jump.title') }}</span>
    </template>
    <div class="jump-dialog-body">
      <input
        ref="inputRef"
        v-model="pathInput"
        class="jump-path-input"
        type="text"
        :placeholder="placeholder || t('jump.placeholder')"
        spellcheck="false"
        @keydown.enter="doConfirm"
      />
    </div>
    <template #footer>
      <button class="fbtn" @click="$emit('close')">{{ t('jump.cancel') }}</button>
      <button class="fbtn fbtn-primary" @click="doConfirm">{{ t('jump.confirm') }}</button>
    </template>
  </ModalDialog>
</template>

<script setup>
import { ref, watch, nextTick } from 'vue'
import { useI18n } from 'vue-i18n'
import { CornerUpRight } from 'lucide-vue-next'
import ModalDialog from '../common/ModalDialog.vue'

const props = defineProps({
  open: Boolean,
  /** Optional placeholder override. Defaults to the shared jump.placeholder. */
  placeholder: String,
})
const emit = defineEmits(['close', 'confirm'])

const { t } = useI18n()
const pathInput = ref('')
const inputRef = ref(null)

watch(() => props.open, (isOpen) => {
  if (isOpen) {
    pathInput.value = ''
    nextTick(() => inputRef.value?.focus())
  }
})

function doConfirm() {
  const value = pathInput.value.trim()
  if (!value) return
  emit('confirm', value)
}
</script>

<style scoped>
.jump-dialog-body {
  padding: var(--space-6) var(--space-7);
}
.jump-path-input {
  width: 100%;
  box-sizing: border-box;
  padding: var(--space-4) var(--space-5);
  border: 1px solid var(--border-color, #dee2e6);
  border-radius: var(--radius-sm, 6px);
  font-size: var(--font-size-md);
  background: var(--bg-primary);
  color: var(--text-primary);
  outline: none;
}
.jump-path-input:focus {
  border-color: var(--accent-color, #4a90d9);
}
</style>
