<template>
  <Transition name="up-fade">
    <div v-if="visible" class="up-overlay" @click.self="dismiss">
      <div class="up-panel">
        <div class="up-header">
          <h3>{{ t('upgrade.newVersion') }}</h3>
        </div>
        <p class="up-body">{{ t('upgrade.promptMessage', { version: latestVersion, currentVersion }) }}</p>
        <div class="up-version-badge">v{{ latestVersion }}</div>
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
const { skipVersion: doSkip, startUpgrade, releaseNotesUrl } = useUpgrade()

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
  z-index: 1001;
  display: flex;
  align-items: center;
  justify-content: center;
  background: color-mix(in srgb, var(--bg-primary) 80%, transparent);
  backdrop-filter: blur(4px);
  -webkit-backdrop-filter: blur(4px);
  padding: 16px;
}

.up-panel {
  background: var(--bg-secondary);
  border: 1px solid var(--border-color);
  border-radius: 12px;
  width: 100%;
  max-width: 380px;
  box-shadow: var(--shadow-lg, 0 8px 32px rgba(0,0,0,0.15));
  overflow: hidden;
}

.up-header {
  padding: 14px 16px 8px;
}

.up-header h3 {
  margin: 0;
  font-size: 16px;
  font-weight: 700;
  color: var(--text-primary);
}

.up-body {
  margin: 0 16px 8px;
  font-size: 13px;
  color: var(--text-secondary);
  line-height: 1.6;
}

.up-version-badge {
  display: inline-block;
  margin: 0 16px 12px;
  padding: 4px 12px;
  background: color-mix(in srgb, var(--accent-color) 15%, transparent);
  color: var(--accent-color);
  border-radius: 6px;
  font-size: 14px;
  font-weight: 700;
  font-variant-numeric: tabular-nums;
}

.up-release-link {
  display: block;
  margin: 0 16px 12px;
  font-size: 13px;
  font-weight: 500;
  color: var(--accent-color);
  text-decoration: none;
  cursor: pointer;
}

.up-release-link:hover {
  text-decoration: underline;
}

.up-footer {
  padding: 8px 16px 14px;
  display: flex;
  gap: 8px;
}

.up-upgrade {
  flex: 1;
  padding: 8px 16px;
  border: none;
  border-radius: 8px;
  background: var(--accent-color);
  color: #fff;
  font-size: 13px;
  font-weight: 600;
  cursor: pointer;
  transition: opacity 0.2s;
}

@media (hover: hover) {
  .up-upgrade:hover { opacity: 0.9; }
}

.up-skip {
  padding: 8px 12px;
  border: 1px solid var(--border-color);
  border-radius: 8px;
  background: var(--bg-secondary);
  color: var(--text-secondary);
  font-size: 13px;
  font-weight: 500;
  cursor: pointer;
  transition: all 0.2s;
}

@media (hover: hover) {
  .up-skip:hover {
    border-color: var(--accent-color);
    color: var(--accent-color);
  }
}

.up-later {
  padding: 8px 12px;
  border: none;
  border-radius: 8px;
  background: var(--bg-tertiary);
  color: var(--text-muted);
  font-size: 13px;
  cursor: pointer;
}

.up-fade-enter-active { transition: opacity 0.2s ease; }
.up-fade-leave-active { transition: opacity 0.15s ease; }
.up-fade-enter-from, .up-fade-leave-to { opacity: 0; }
</style>
