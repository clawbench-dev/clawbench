<template>
  <Teleport to="body">
    <Transition name="dlg">
      <div v-if="dlg.state.value.visible" ref="overlayRef" class="dlg-overlay" :style="{ zIndex: 3000 }" tabindex="-1" @click.self="handleOverlayClick" @keydown.escape="handleCancel" @keydown.enter="handleKeyEnter">
        <div class="dlg-box">
          <div v-if="dlg.state.value.title" class="dlg-title">
            <span class="dlg-title-icon">
              <Info v-if="dlg.state.value.type === 'alert'" :size="16" />
              <MessageSquareText v-else :size="16" />
            </span>
            <span>{{ dlg.state.value.title }}</span>
          </div>
          <div class="dlg-msg">{{ dlg.state.value.message }}</div>
          <textarea
            v-if="dlg.state.value.type === 'prompt'"
            ref="inputRef"
            v-model="inputVal"
            class="dlg-input dlg-textarea"
            :placeholder="dlg.state.value.placeholder"
            rows="3"
            @keydown.enter.prevent="handleConfirm"
          ></textarea>
          <div class="dlg-actions">
            <button
              v-if="dlg.state.value.extraText && dlg.state.value.type !== 'alert'"
              class="dlg-btn dlg-extra"
              :class="{ 'dlg-extra-primed': extraPrimed }"
              @click="handleExtraClick"
            >{{ extraPrimed ? (dlg.state.value.extraPrimedText || t('common.confirm')) : dlg.state.value.extraText }}</button>
            <button
              v-if="dlg.state.value.type !== 'alert'"
              class="dlg-btn dlg-cancel"
              @click="handleCancel"
            >{{ dlg.state.value.cancelText || t('common.cancel') }}</button>
            <button
              class="dlg-btn dlg-ok"
              :class="{ 'dlg-danger': dlg.state.value.dangerous }"
              @click="handleConfirm"
            >{{ dlg.state.value.confirmText || (dlg.state.value.type === 'alert' ? t('common.ok') : t('common.confirm')) }}</button>
          </div>
        </div>
      </div>
    </Transition>
  </Teleport>
</template>

<script setup lang="ts">
import { ref, watch, nextTick, onBeforeUnmount } from 'vue'
import { useI18n } from 'vue-i18n'
import { Info, MessageSquareText } from 'lucide-vue-next'
import { useDialog } from '@/composables/useDialog'
import { registerBackHandler, PRIORITY_OVERLAY } from '@/composables/useBackHandler'

const { t } = useI18n()
const dlg = useDialog()
const inputVal = ref('')
const inputRef = ref<HTMLTextAreaElement | null>(null)
const overlayRef = ref<HTMLElement | null>(null)
const extraPrimed = ref(false)
let unregisterBack: (() => void) | null = null

watch(() => dlg.state.value.visible, async (v) => {
  if (!v) {
    if (unregisterBack) { unregisterBack(); unregisterBack = null }
    return
  }
  inputVal.value = dlg.state.value.value ?? ''
  extraPrimed.value = false
  await nextTick()
  if (dlg.state.value.type === 'prompt') {
    inputRef.value?.focus()
    inputRef.value?.select()
  } else {
    overlayRef.value?.focus()
  }
  unregisterBack = registerBackHandler({
    id: 'dialog-overlay',
    canGoBack: () => dlg.state.value.visible,
    goBack: () => handleCancel(),
    priority: PRIORITY_OVERLAY + 1,
  })
}, { immediate: true })

function handleConfirm() {
  if (dlg.state.value.type === 'prompt') {
    dlg.resolve(inputVal.value || null)
  } else if (dlg.state.value.type === 'confirm') {
    dlg.resolve(true)
  } else {
    dlg.resolve(true)
  }
}

function handleCancel() {
  if (unregisterBack) { unregisterBack(); unregisterBack = null }
  dlg.resolve(dlg.state.value.type === 'prompt' ? null : false)
}

function handleOverlayClick() {
  extraPrimed.value = false
  handleCancel()
}

function handleKeyEnter() {
  // Prompt type: let the input handle Enter itself
  if (dlg.state.value.type === 'prompt') return
  // Confirm/alert: Enter triggers confirm
  handleConfirm()
}

function handleExtraClick() {
  if (extraPrimed.value) {
    dlg.state.value.onExtraAction?.()
    dlg.resolve(null)
  } else {
    extraPrimed.value = true
  }
}

onBeforeUnmount(() => {
  if (unregisterBack) { unregisterBack(); unregisterBack = null }
})
</script>

<style>
.dlg-overlay {
  position: fixed;
  inset: 0;
  background: rgba(0, 0, 0, 0.45);
  display: flex;
  align-items: center;
  justify-content: center;
  z-index: 3000;
  padding: 0 20px;
  outline: none;
}

.dlg-box {
  background: var(--bg-secondary, #fff);
  border-radius: 14px;
  padding: 18px 16px 14px;
  max-width: 320px;
  width: 100%;
  box-shadow: 0 8px 32px rgba(0, 0, 0, 0.2);
  animation: dlg-in 0.2s cubic-bezier(0.34, 1.56, 0.64, 1);
}

.dlg-title {
  display: flex;
  align-items: center;
  gap: 8px;
  font-weight: 600;
  font-size: 14px;
  color: var(--text-primary, #1a1a1a);
  margin-bottom: 8px;
}

.dlg-title-icon {
  flex-shrink: 0;
  width: 24px;
  height: 24px;
  border-radius: 7px;
  color: var(--accent-color, #0066cc);
  background: color-mix(in srgb, var(--accent-color, #0066cc) 12%, transparent);
  display: inline-flex;
  align-items: center;
  justify-content: center;
}

.dlg-msg {
  font-size: 13px;
  color: var(--text-secondary, #555);
  line-height: 1.5;
  margin-bottom: 14px;
  white-space: pre-line;
  word-break: break-word;
  overflow-wrap: break-word;
  max-height: 40vh;
  overflow-y: auto;
}

.dlg-input {
  width: 100%;
  padding: 8px 10px;
  border: 1px solid var(--border-color, #ddd);
  border-radius: 8px;
  font-size: 13px;
  font-family: inherit;
  background: var(--bg-primary, #fff);
  color: var(--text-primary, #1a1a1a);
  outline: none;
  margin-bottom: 14px;
  transition: border-color 0.15s;
}

.dlg-textarea {
  resize: none;
  min-height: 84px;
  line-height: 1.5;
  white-space: pre-wrap;
  word-break: break-word;
  overflow-wrap: break-word;
}

.dlg-input:focus {
  border-color: var(--accent-color, #0066cc);
}

.dlg-actions {
  display: flex;
  gap: 8px;
  justify-content: flex-end;
}

.dlg-btn {
  padding: 6px 16px;
  border-radius: 8px;
  font-size: 13px;
  font-weight: 500;
  border: none;
  cursor: pointer;
  transition: opacity 0.12s;
  -webkit-tap-highlight-color: transparent;
}

.dlg-btn:active { opacity: 0.7; }

.dlg-cancel {
  background: var(--bg-tertiary, #f0f0f0);
  color: var(--text-secondary, #555);
}

.dlg-ok {
  background: var(--accent-color, #0066cc);
  color: #fff;
}

.dlg-danger {
  background: #d32f2f;
  color: #fff;
}

.dlg-extra {
  background: transparent;
  color: #d32f2f;
  border: 1px solid #d32f2f;
  font-size: 12px;
  padding: 5px 10px;
  transition: background 0.15s, color 0.15s, border-color 0.15s;
}

.dlg-extra-primed {
  background: #d32f2f;
  color: #fff;
  border-color: #d32f2f;
}

[data-theme-base="dark"] .dlg-extra {
  border-color: #ef4444;
  color: #ef4444;
}

[data-theme-base="dark"] .dlg-extra-primed {
  background: #ef4444;
  color: #fff;
  border-color: #ef4444;
}

[data-theme-base="dark"] .dlg-box {
  box-shadow: 0 8px 32px rgba(0, 0, 0, 0.5);
}

[data-theme-base="dark"] .dlg-cancel {
  background: #333;
  color: #ccc;
}

.dlg-enter-active, .dlg-leave-active {
  transition: opacity 0.2s ease;
}

.dlg-enter-from, .dlg-leave-to {
  opacity: 0;
}

.dlg-enter-active .dlg-box {
  animation: dlg-in 0.2s cubic-bezier(0.34, 1.56, 0.64, 1);
}

.dlg-leave-active .dlg-box {
  animation: dlg-out 0.15s ease forwards;
}

@keyframes dlg-in {
  from { opacity: 0; transform: scale(0.9); }
  to { opacity: 1; transform: scale(1); }
}

@keyframes dlg-out {
  from { opacity: 1; transform: scale(1); }
  to { opacity: 0; transform: scale(0.9); }
}
</style>
