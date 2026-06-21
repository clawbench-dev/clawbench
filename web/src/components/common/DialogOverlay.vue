<template>
  <Teleport to="body">
    <Transition name="dlg">
      <div v-if="dlg.state.value.visible" class="dlg-overlay" :style="{ zIndex: 3000 }" @click.self="handleCancel">
        <div class="dlg-box">
          <div v-if="dlg.state.value.title" class="dlg-title">{{ dlg.state.value.title }}</div>
          <div class="dlg-msg">{{ dlg.state.value.message }}</div>
          <input
            v-if="dlg.state.value.type === 'prompt'"
            ref="inputRef"
            v-model="inputVal"
            class="dlg-input"
            :placeholder="dlg.state.value.placeholder"
            @keydown.enter="handleConfirm"
          />
          <div class="dlg-actions">
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

<script setup>
import { ref, watch, nextTick } from 'vue'
import { useI18n } from 'vue-i18n'
import { useDialog } from '@/composables/useDialog'

const { t } = useI18n()
const dlg = useDialog()
const inputVal = ref('')
const inputRef = ref(null)

watch(() => dlg.state.value.visible, async (v) => {
  if (!v) return
  inputVal.value = dlg.state.value.value ?? ''
  if (dlg.state.value.type === 'prompt') {
    await nextTick()
    inputRef.value?.focus()
    inputRef.value?.select()
  }
})

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
  dlg.resolve(dlg.state.value.type === 'prompt' ? null : false)
}
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
  font-weight: 600;
  font-size: 14px;
  color: var(--text-primary, #1a1a1a);
  margin-bottom: 8px;
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
  padding: 7px 10px;
  border: 1px solid var(--border-color, #ddd);
  border-radius: 8px;
  font-size: 13px;
  background: var(--bg-primary, #fff);
  color: var(--text-primary, #1a1a1a);
  outline: none;
  margin-bottom: 14px;
  transition: border-color 0.15s;
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

[data-theme="dark"] .dlg-box {
  box-shadow: 0 8px 32px rgba(0, 0, 0, 0.5);
}

[data-theme="dark"] .dlg-cancel {
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
