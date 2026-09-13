<template>
  <Transition name="up-fade">
    <div v-if="visible" class="up-overlay" @click.self="dismiss">
      <div class="up-panel">
        <div class="up-header">
          <h3>{{ t('upgrade.newVersion') }}</h3>
        </div>
        <p class="up-body">{{ t('upgrade.promptMessage', { version: latestVersion, currentVersion }) }}</p>
        <div class="up-version-badge">v{{ latestVersion }}</div>
        <p v-if="isDocker" class="up-docker-hint">
          {{ t('upgrade.dockerHintBody') }}
          <span class="up-docker-restart">{{ t('upgrade.dockerHintRestart') }}</span>
        </p>
        <a v-if="releaseNotesUrl" class="up-release-link" :href="releaseNotesUrl" target="_blank" rel="noopener noreferrer">
          {{ t('upgrade.releaseNotes', { version: latestVersion }) }}
        </a>
        <div class="up-footer">
          <button class="up-upgrade" @click="upgradeNow">{{ t('upgrade.upgradeNow') }}</button>
          <button class="up-skip" @click="skipVersion">{{ t('upgrade.skipVersion') }}</button>
          <button class="up-later" @click="dismiss">{{ t('upgrade.remindLater') }}</button>
        </div>
      </div>
    </div>
  </Transition>
</template>

<script setup lang="ts">
import { ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useUpgrade } from '@/composables/useUpgrade'
import { registerBackHandler, PRIORITY_OVERLAY } from '@/composables/useBackHandler'

const { t } = useI18n()
const { skipVersion: doSkip, startUpgrade, releaseNotesUrl, isDocker } = useUpgrade()

const visible = ref(false)
const latestVersion = ref('')
const currentVersion = ref('')
let unregisterBack: (() => void) | null = null

defineExpose({ show })

function show(latest: string, current: string) {
  latestVersion.value = latest
  currentVersion.value = current
  visible.value = true
}

function upgradeNow() {
  visible.value = false
  startUpgrade()
}

function skipVersion() {
  doSkip(latestVersion.value)
  visible.value = false
}

function dismiss() {
  visible.value = false
}

// Register back handler when overlay opens, unregister on close
watch(visible, (v) => {
  if (v) {
    unregisterBack = registerBackHandler({
      id: 'upgrade-prompt',
      canGoBack: () => visible.value,
      goBack: () => dismiss(),
      priority: PRIORITY_OVERLAY,
    })
  } else if (unregisterBack) {
    unregisterBack()
    unregisterBack = null
  }
})
</script>

<style scoped>
.up-overlay {
  position: fixed;
  inset: 0;
  z-index: var(--z-overlay-raised);
  display: flex;
  align-items: center;
  justify-content: center;
  background: color-mix(in srgb, var(--bg-primary) 80%, transparent);
  backdrop-filter: blur(4px);
  -webkit-backdrop-filter: blur(4px);
  padding: var(--space-7);
}

.up-panel {
  background: var(--bg-secondary);
  border: 1px solid var(--border-color);
  border-radius: var(--radius-lg);
  width: 100%;
  max-width: 380px;
  box-shadow: var(--shadow-lg);
  overflow: hidden;
}

.up-header {
  padding:14px var(--space-7) var(--space-4);
}

.up-header h3 {
  margin: 0;
  font-size: var(--font-size-2xl);
  font-weight: var(--font-weight-bold);
  color: var(--text-primary);
}

.up-body {
  margin:0 var(--space-7) var(--space-4);
  font-size: var(--font-size-md);
  color: var(--text-secondary);
  line-height: var(--line-height-relaxed);
}

.up-version-badge {
  display: inline-block;
  margin:0 var(--space-7) var(--space-6);
  padding: var(--space-2) var(--space-6);
  background: color-mix(in srgb, var(--accent-color) 15%, transparent);
  color: var(--accent-color);
  border-radius: var(--radius-sm);
  font-size: var(--font-size-lg);
  font-weight: var(--font-weight-bold);
  font-variant-numeric: tabular-nums;
}

.up-release-link {
  display: block;
  margin:0 var(--space-7) var(--space-6);
  font-size: var(--font-size-md);
  font-weight: var(--font-weight-medium);
  color: var(--accent-color);
  text-decoration: none;
  cursor: pointer;
}

.up-docker-hint {
  margin:0 var(--space-7) var(--space-5);
  padding: var(--space-4) var(--space-5);
  border-radius: var(--radius-sm);
  background: color-mix(in srgb, var(--accent-color) 10%, transparent);
  border: 1px solid color-mix(in srgb, var(--accent-color) 30%, transparent);
  font-size: var(--font-size-sm);
  color: var(--text-secondary);
  line-height: var(--line-height-normal);
}

.up-docker-restart {
  display: block;
  margin-top: var(--space-2);
  color: var(--text-warning, #d69e2e);
}

.up-release-link:hover {
  text-decoration: underline;
}

.up-footer {
  padding: var(--space-4) var(--space-7) 14px;
  display: flex;
  gap: var(--space-4);
}

.up-upgrade {
  flex: 1;
  padding: var(--space-4) var(--space-7);
  border: none;
  border-radius: var(--radius-sm);
  background: var(--accent-color);
  color: #fff;
  font-size: var(--font-size-md);
  font-weight: var(--font-weight-semibold);
  cursor: pointer;
  transition: opacity var(--duration-slow);
}

@media (hover: hover) {
  .up-upgrade:hover { opacity: var(--opacity-hover); }
}

.up-skip {
  padding: var(--space-4) var(--space-6);
  border: 1px solid var(--border-color);
  border-radius: var(--radius-sm);
  background: var(--bg-secondary);
  color: var(--text-secondary);
  font-size: var(--font-size-md);
  font-weight: var(--font-weight-medium);
  cursor: pointer;
  transition: all var(--duration-slow);
}

@media (hover: hover) {
  .up-skip:hover {
    border-color: var(--accent-color);
    color: var(--accent-color);
  }
}

.up-later {
  padding: var(--space-4) var(--space-6);
  border: none;
  border-radius: var(--radius-sm);
  background: var(--bg-tertiary);
  color: var(--text-muted);
  font-size: var(--font-size-md);
  cursor: pointer;
}

.up-fade-enter-active { transition: opacity var(--duration-slow) ease; }
.up-fade-leave-active { transition: opacity var(--duration-base) ease; }
.up-fade-enter-from, .up-fade-leave-to { opacity: 0; }
</style>
