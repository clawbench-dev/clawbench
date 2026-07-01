<template>
  <Teleport to="body">
    <div
      v-if="everOpened"
      v-show="open || leaving"
      ref="overlayRef"
      class="modal-overlay"
      :class="{ 'modal-leaving': leaving }"
      :style="{ zIndex }"
      tabindex="-1"
      @click.self="handleClose"
      @keydown.escape="handleClose"
    >
      <div class="modal-dialog" :class="{ 'modal-leaving': leaving, 'modal-full-height': fullHeight }" @click.stop>
        <div class="modal-header">
          <slot name="header">
            <span class="modal-title">{{ title }}</span>
          </slot>
          <button class="modal-close-btn" @click="handleClose" :title="'Close'">
            <svg width="16" height="16" viewBox="0 0 14 14" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><line x1="3" y1="3" x2="11" y2="11"/><line x1="11" y1="3" x2="3" y2="11"/></svg>
          </button>
        </div>
        <div class="modal-body">
          <slot />
        </div>
        <div class="modal-footer" :class="{ 'modal-footer-default': !$slots.footer }">
          <slot name="footer" />
        </div>
        <slot name="after" />
      </div>
    </div>
  </Teleport>
</template>

<script setup>
import { ref, watch, nextTick } from 'vue'

const props = defineProps({
  open: Boolean,
  title: { type: String, default: '' },
  zIndex: { type: Number, default: 2100 },
  fullHeight: { type: Boolean, default: false },
})

const emit = defineEmits(['close'])

const leaving = ref(false)
const everOpened = ref(false)
const overlayRef = ref(null)
let leaveTimer = null

watch(() => props.open, (val) => {
  clearTimeout(leaveTimer)
  if (val) {
    everOpened.value = true
    leaving.value = false
    // Auto-focus overlay so Escape key works immediately
    nextTick(() => {
      overlayRef.value?.focus()
    })
  } else if (leaving.value) {
    // Close triggered externally while animating — cancel animation, hide now
    leaving.value = false
  }
}, { immediate: true })

function handleClose() {
  if (leaving.value) return
  leaving.value = true
  leaveTimer = setTimeout(() => {
    leaving.value = false
    leaveTimer = null
    emit('close')
  }, 250)
}

defineExpose({
  close: handleClose,
})
</script>

<style>
.modal-overlay {
  position: fixed;
  inset: 0;
  background: rgba(0, 0, 0, 0.5);
  z-index: 2100;
  display: flex;
  align-items: center;
  justify-content: center;
  padding: 44px 20px 48px;
  animation: modal-fadeIn 0.2s ease;
}

.modal-overlay.modal-leaving {
  animation: modal-fadeOut 0.25s ease forwards;
}

.modal-dialog {
  background: var(--bg-secondary, #fff);
  border: 1px solid var(--border-color, #e0e0e0);
  border-radius: 12px;
  width: 100%;
  max-height: 100%;
  height: auto;
  display: flex;
  flex-direction: column;
  overflow: hidden;
  box-shadow: 0 8px 32px rgba(0, 0, 0, 0.18);
  animation: modal-scaleIn 0.25s cubic-bezier(0.34, 1.56, 0.64, 1);
}

.modal-dialog.modal-full-height {
  height: 100%;
}

.modal-dialog.modal-leaving {
  animation: modal-scaleOut 0.25s ease forwards;
}

@keyframes modal-fadeIn {
  from { opacity: 0; }
  to   { opacity: 1; }
}

@keyframes modal-fadeOut {
  from { opacity: 1; }
  to   { opacity: 0; }
}

@keyframes modal-scaleIn {
  from {
    opacity: 0;
    transform: translateY(24px) scale(0.94);
  }
  to {
    opacity: 1;
    transform: translateY(0) scale(1);
  }
}

@keyframes modal-scaleOut {
  from {
    opacity: 1;
    transform: translateY(0) scale(1);
  }
  to {
    opacity: 0;
    transform: translateY(24px) scale(0.94);
  }
}

.modal-header {
  display: flex;
  align-items: center;
  gap: 7px;
  padding: 5px 9px;
  border-bottom: 1px solid color-mix(in srgb, var(--accent-color, #0066cc) 15%, var(--border-color, #e5e5e5));
  background: color-mix(in srgb, var(--accent-color, #0066cc) 6%, transparent);
  flex-shrink: 0;
}

.modal-close-btn {
  margin-left: auto;
  width: 22px;
  height: 22px;
  border-radius: 50%;
  border: none;
  background: var(--bg-tertiary, #eee);
  color: var(--text-muted, #888);
  display: flex;
  align-items: center;
  justify-content: center;
  cursor: pointer;
  flex-shrink: 0;
  padding: 0;
  transition: background 0.15s, color 0.15s;
}

.modal-close-btn:hover {
  background: var(--border-color, #ddd);
  color: var(--text-primary, #333);
}

.modal-close-btn:active {
  background: color-mix(in srgb, var(--accent-color, #0066cc) 20%, var(--bg-tertiary, #eee));
  color: var(--accent-color, #0066cc);
}

.modal-header-icon {
  flex-shrink: 0;
  color: var(--accent-color, #0066cc);
  display: flex;
  align-items: center;
}

.modal-title {
  font-weight: 600;
  font-size: 14px;
  color: var(--text-primary, #1a1a1a);
}

.modal-body {
  flex: 1;
  min-height: 0;
  overflow-y: auto;
  overflow-x: hidden;
  display: flex;
  flex-direction: column;
}

.modal-footer {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 6px 10px;
  border-top: 1px solid var(--border-color, #e5e5e5);
  background: color-mix(in srgb, var(--accent-color, #0066cc) 3%, var(--bg-tertiary, #f8f8f8));
  flex-shrink: 0;
  justify-content: flex-end;
  border-radius: 0 0 12px 12px;
}

.modal-footer-default {
  min-height: 8px;
  padding: 8px 10px;
  justify-content: center;
}
</style>
