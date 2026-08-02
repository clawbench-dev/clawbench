<template>
  <Teleport to="body">
    <Transition name="overlay-fade">
      <div v-if="mode" class="connection-overlay">
        <div class="connection-overlay__content">
          <Server :size="40" class="connection-overlay__icon" />
          <div class="connection-overlay__spinner"></div>
          <div class="connection-overlay__text">{{ overlayText }}</div>
        </div>
      </div>
    </Transition>
  </Teleport>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { Server } from 'lucide-vue-next'
import { useI18n } from 'vue-i18n'
import { useConnectionOverlay } from '@/composables/useConnectionOverlay'

const { t } = useI18n()
const { mode } = useConnectionOverlay()

const overlayText = computed(() =>
  mode.value === 'restart'
    ? t('settings.restartingPleaseWait')
    : t('systemResources.overlayReconnecting'),
)
</script>

<style scoped>
.connection-overlay {
  position: fixed;
  inset: 0;
  z-index: 9999;
  display: flex;
  align-items: center;
  justify-content: center;
  background: rgba(0, 0, 0, 0.45);
  backdrop-filter: blur(4px);
  -webkit-backdrop-filter: blur(4px);
}

.connection-overlay__content {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 20px;
  padding: 40px 48px;
  border-radius: 16px;
  background: var(--bg-primary);
  box-shadow: var(--shadow-md);
}

.connection-overlay__icon {
  color: var(--accent-color);
}

.connection-overlay__spinner {
  width: 36px;
  height: 36px;
  border: 3px solid var(--border-color);
  border-top-color: var(--accent-color);
  border-radius: 50%;
  animation: overlay-spin 0.8s linear infinite;
}

.connection-overlay__text {
  font-size: 15px;
  font-weight: 500;
  color: var(--text-primary);
  white-space: nowrap;
}

@keyframes overlay-spin {
  from { transform: rotate(0deg); }
  to { transform: rotate(360deg); }
}

/* Fade transition (teleported to body) */
.overlay-fade-enter-active,
.overlay-fade-leave-active {
  transition: opacity 0.2s ease;
}
.overlay-fade-enter-from,
.overlay-fade-leave-to {
  opacity: 0;
}
</style>
