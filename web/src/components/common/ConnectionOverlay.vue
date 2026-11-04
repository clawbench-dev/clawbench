<template>
  <Teleport to="body">
    <Transition name="overlay-fade">
      <!-- Hidden while the switch-server dialog is open: ModalDialog teleports
           to <body> and paints below this mask (--z-popover), so the mask is
           dismissed instead of trying to out-stack it. -->
      <div v-if="mode && !switchOpen" class="connection-overlay">
        <div class="connection-overlay__content">
          <Server :size="40" class="connection-overlay__icon" />
          <LoadingIndicator class="connection-overlay__spinner" size="lg" inline />
          <div class="connection-overlay__text">{{ overlayText }}</div>
          <!-- The mask covers the header's server button, so a reconnect that
               never succeeds would leave the user with no way to reach another
               server. This is the escape hatch. -->
          <button
            v-if="mode === 'reconnect'"
            type="button"
            class="connection-overlay__switch"
            @click="switchOpen = true"
          >
            <ArrowLeftRight :size="14" />
            <span>{{ t('login.switchServer') }}</span>
          </button>
        </div>
      </div>
    </Transition>

    <ServerSwitchDialog
      :open="switchOpen"
      @close="switchOpen = false"
      @logout="handleLogout"
    />
  </Teleport>
</template>

<script setup lang="ts">
import { computed, ref } from 'vue'
import { ArrowLeftRight, Server } from 'lucide-vue-next'
import { useI18n } from 'vue-i18n'
import { useConnectionOverlay } from '@/composables/useConnectionOverlay'
import LoadingIndicator from '@/components/common/LoadingIndicator.vue'
import ServerSwitchDialog from '@/components/common/ServerSwitchDialog.vue'
import { getNative } from '@/utils/clawbenchNative'

const { t } = useI18n()
const { mode } = useConnectionOverlay()

const switchOpen = ref(false)

const overlayText = computed(() =>
  mode.value === 'restart'
    ? t('settings.restartingPleaseWait')
    : t('systemResources.overlayReconnecting'),
)

function handleLogout() {
  switchOpen.value = false
  const native = getNative()
  if (native?.showServerDialog) {
    native.showServerDialog()
  } else {
    window.location.href = '/login'
  }
}
</script>

<style scoped>
.connection-overlay {
  position: fixed;
  inset: 0;
  z-index: var(--z-popover);
  display: flex;
  align-items: center;
  justify-content: center;
  background: rgba(0, 0, 0, 0.45);
  backdrop-filter: blur(calc(4 * var(--dp)));
  -webkit-backdrop-filter: blur(calc(4 * var(--dp)));
}

.connection-overlay__content {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: var(--space-8);
  padding: calc(40 * var(--dp)) calc(48 * var(--dp));
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

.connection-overlay__switch {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  gap: var(--space-3);
  height: calc(34 * var(--dp));
  padding: 0 var(--space-7);
  border: 1px solid var(--border-color);
  border-radius: var(--radius-full);
  background: var(--bg-tertiary);
  color: var(--text-primary);
  font-size: var(--font-size-md);
  font-weight: var(--font-weight-medium);
  cursor: pointer;
  transition:
    background var(--duration-base) ease,
    border-color var(--duration-base) ease;
}

.connection-overlay__switch:hover {
  background: var(--bg-elevated);
  border-color: var(--accent-color);
}

.connection-overlay__switch:active {
  transform: scale(0.96);
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
