<template>
  <Teleport to="body">
    <Transition name="overlay-fade">
      <div v-if="mode" class="connection-overlay">
        <div class="connection-overlay__content">
          <Server :size="40" class="connection-overlay__icon" />
          <LoadingIndicator class="connection-overlay__spinner" size="lg" inline />
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
import LoadingIndicator from '@/components/common/LoadingIndicator.vue'

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
  gap: var(--space-8);
  padding: 40px 48px;
  border-radius: var(--radius-lg);
  background: var(--bg-primary);
  box-shadow: var(--shadow-md);
}

.connection-overlay__icon {
  color: var(--accent-color);
}

.connection-overlay__text {
  font-size: var(--font-size-xl);
  font-weight: var(--font-weight-medium);
  color: var(--text-primary);
  white-space: nowrap;
}

/* Fade transition (teleported to body) */
.overlay-fade-enter-active,
.overlay-fade-leave-active {
  transition: opacity var(--duration-slow) ease;
}
.overlay-fade-enter-from,
.overlay-fade-leave-to {
  opacity: 0;
}
</style>
