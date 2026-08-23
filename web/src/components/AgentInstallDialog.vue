<template>
  <Teleport to="body">
    <div ref="overlayRef" tabindex="-1" class="install-overlay" @click.self="$emit('close')" @keydown.escape="handleEscape">
      <div class="install-box">
        <div class="install-title">
          <span class="install-title-icon"><PackagePlus :size="16" /></span>
          <span>{{ t('welcomeInfo.install') }} {{ backendName }}</span>
        </div>
        <div class="install-hint">{{ t('welcomeInfo.manualInstallHint') }}</div>
        <div class="install-cmd-row">
          <code class="install-cmd">{{ installCmd }}</code>
          <button class="btn-copy" @click="copyCmd">
            <svg v-if="!copied" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" width="14" height="14">
              <rect x="9" y="9" width="13" height="13" rx="2"/>
              <path d="M5 15H4a2 2 0 01-2-2V4a2 2 0 012-2h9a2 2 0 012 2v1"/>
            </svg>
            <svg v-else viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" width="14" height="14">
              <path d="M20 6L9 17l-5-5"/>
            </svg>
          </button>
        </div>
        <div class="install-actions">
          <button class="dlg-btn dlg-cancel" @click="$emit('close')">{{ t('common.close') }}</button>
        </div>
      </div>
    </div>
  </Teleport>
</template>

<script setup lang="ts">
import { ref, onMounted, onBeforeUnmount, nextTick } from 'vue'
import { useI18n } from 'vue-i18n'
import { PackagePlus } from 'lucide-vue-next'
import { registerBackHandler, PRIORITY_OVERLAY } from '@/composables/useBackHandler'

const props = defineProps<{
  backendName: string
  installCmd: string
}>()

const emit = defineEmits<{
  close: []
}>()

const { t } = useI18n()
const copied = ref(false)
const overlayRef = ref<HTMLDivElement | null>(null)
let unregisterBack: (() => void) | null = null

// Escape (PC) closes the dialog, mirroring the Android back handler.
function handleEscape() {
  emit('close')
}

onMounted(() => {
  // Focus the overlay so Escape key works immediately (same as BottomSheet/ModalDialog).
  nextTick(() => overlayRef.value?.focus())
  unregisterBack = registerBackHandler({
    id: 'agent-install-dialog',
    canGoBack: () => true,
    goBack: () => emit('close'),
    priority: PRIORITY_OVERLAY,
  })
})

onBeforeUnmount(() => {
  if (unregisterBack) { unregisterBack(); unregisterBack = null }
})

function copyCmd() {
  navigator.clipboard.writeText(props.installCmd)
  copied.value = true
  setTimeout(() => { copied.value = false }, 2000)
}
</script>

<style scoped>
.install-overlay {
  position: fixed;
  inset: 0;
  background: rgba(0, 0, 0, 0.45);
  display: flex;
  align-items: center;
  justify-content: center;
  z-index: 3000;
  padding: 0 20px;
  animation: overlay-in 0.15s ease;
  outline: none;
}

.install-box {
  background: var(--bg-secondary, #fff);
  border-radius: 14px;
  padding: 18px 16px 14px;
  max-width: 420px;
  width: 100%;
  display: flex;
  flex-direction: column;
  box-shadow: 0 8px 32px rgba(0, 0, 0, 0.2);
  animation: dlg-in 0.2s cubic-bezier(0.34, 1.56, 0.64, 1);
}

.install-title {
  display: flex;
  align-items: center;
  gap: 8px;
  font-weight: 600;
  font-size: 14px;
  color: var(--text-primary, #1a1a1a);
  margin-bottom: 10px;
}

.install-title-icon {
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

.install-hint {
  font-size: 12px;
  color: var(--text-secondary, #555);
  margin-bottom: 8px;
}

.install-cmd-row {
  display: flex;
  align-items: center;
  gap: 6px;
  background: var(--bg-tertiary, #f0f0f0);
  border: 1px solid var(--border-color, #ddd);
  border-radius: 8px;
  padding: 8px 10px;
  margin-bottom: 14px;
}

.install-cmd {
  flex: 1;
  font-family: 'SF Mono', 'Fira Code', 'Cascadia Code', monospace;
  font-size: 12px;
  color: var(--text-primary, #1a1a1a);
  word-break: break-all;
  min-width: 0;
}

.btn-copy {
  flex-shrink: 0;
  display: flex;
  align-items: center;
  justify-content: center;
  width: 28px;
  height: 28px;
  border: none;
  border-radius: 6px;
  background: var(--bg-primary, #fff);
  color: var(--text-secondary, #555);
  cursor: pointer;
  transition: all 0.15s;
}

@media (hover: hover) {
  .btn-copy:hover {
    color: var(--accent-color);
  }
}

.install-actions {
  display: flex;
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
}

.dlg-btn:active { opacity: 0.7; }

.dlg-cancel {
  background: var(--bg-tertiary, #f0f0f0);
  color: var(--text-secondary, #555);
}
</style>

<style>
@keyframes dlg-in {
  from { opacity: 0; transform: scale(0.9); }
  to { opacity: 1; transform: scale(1); }
}

@keyframes overlay-in {
  from { opacity: 0; }
  to { opacity: 1; }
}
</style>
