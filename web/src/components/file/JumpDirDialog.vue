<template>
  <ModalDialog :open="open" :title="t('jump.title')" :z-index="2400" @close="$emit('close')">
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
      <button class="jump-cancel-btn" @click="$emit('close')">{{ t('jump.cancel') }}</button>
      <button class="jump-confirm-btn" @click="doConfirm">{{ t('jump.confirm') }}</button>
    </template>
  </ModalDialog>
</template>

<script setup>
import { ref, watch, nextTick } from 'vue'
import { useI18n } from 'vue-i18n'
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
  padding: 12px 16px;
}
.jump-path-input {
  width: 100%;
  box-sizing: border-box;
  padding: 8px 10px;
  border: 1px solid var(--border-color, #dee2e6);
  border-radius: var(--radius-sm, 6px);
  font-size: 13px;
  background: var(--bg-primary);
  color: var(--text-primary);
  outline: none;
}
.jump-path-input:focus {
  border-color: var(--accent-color, #4a90d9);
}
.jump-cancel-btn {
  padding: 7px 14px;
  background: var(--bg-tertiary, #f0f0f0);
  color: var(--text-secondary, #666);
  border: 1px solid var(--border-color, #dee2e6);
  border-radius: var(--radius-sm, 6px);
  font-size: 13px;
  font-weight: 500;
  cursor: pointer;
  flex-shrink: 0;
}
@media (hover: hover) {
  .jump-cancel-btn:hover { background: var(--bg-secondary); }
}
.jump-confirm-btn {
  padding: 7px 14px;
  background: var(--accent-color, #0066cc);
  color: #fff;
  border: none;
  border-radius: var(--radius-sm, 6px);
  font-size: 13px;
  font-weight: 500;
  cursor: pointer;
  flex-shrink: 0;
}
@media (hover: hover) {
  .jump-confirm-btn:hover { background: #0055aa; }
}
</style>
