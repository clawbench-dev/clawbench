<template>
  <Transition name="vm-fade">
    <div v-if="visible" class="vm-overlay" @click.self="close">
      <div class="vm-panel">
        <div class="vm-header">
          <h3>{{ t('versionMismatch.title') }}</h3>
          <button class="vm-close" @click="close" :aria-label="t('versionMismatch.skip')">
            <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" width="18" height="18">
              <path d="M18 6L6 18M6 6l12 12"/>
            </svg>
          </button>
        </div>
        <p class="vm-body">{{ t('versionMismatch.message', { appVersion, serverVersion }) }}</p>
        <div class="vm-footer">
          <button class="vm-download" @click="downloadApk" :aria-label="t('versionMismatch.download')">{{ t('versionMismatch.download') }}</button>
          <button class="vm-skip" @click="skip">{{ t('versionMismatch.skip') }}</button>
        </div>
      </div>
    </div>
  </Transition>
</template>

<script setup lang="ts">
import { ref, computed, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useAppMode } from '@/composables/useAppMode'
import { useSettingsConfig } from '@/composables/useSettingsConfig'
import { appLog } from '@/utils/appLog'
import { normalizeVersion, isVersionedBuild, compareVersions, extractBaseVersion } from '@/utils/version'

const STORAGE_KEY = 'clawbench_version_mismatch_skip'

defineExpose({ show })

const { t } = useI18n()
const { isAppMode } = useAppMode()
const { serverConfig } = useSettingsConfig()

const visible = ref(false)
const hasAttemptedShow = ref(false)

const appVersion = computed(() => {
  if (!isAppMode.value) return ''
  try {
    const native = (window as unknown as { AndroidNative?: { getAppVersion?: () => string } }).AndroidNative
    return native?.getAppVersion?.() ?? ''
  } catch {
    return ''
  }
})

const serverVersion = computed(() => (serverConfig.value?.version as string) ?? '')

const normalizedServerVersion = computed(() => normalizeVersion(serverVersion.value))

function show() {
  hasAttemptedShow.value = true
  tryShow()
}

/** Attempt to show the dialog; silently skips if conditions aren't met yet. */
function tryShow() {
  if (!isAppMode.value) return
  if (!appVersion.value || !serverVersion.value) {
    appLog.d('VersionMismatch', 'Skipping: version info not yet available', { appVersion: appVersion.value, serverVersion: serverVersion.value })
    return
  }
  // Skip check for non-versioned builds (short hashes, "dev", etc.)
  if (!isVersionedBuild(appVersion.value) || !isVersionedBuild(normalizedServerVersion.value)) {
    appLog.d('VersionMismatch', 'Skipping: non-versioned build', { appVersion: appVersion.value, serverVersion: normalizedServerVersion.value })
    return
  }
  // Show only when APK is older than server (needs upgrade)
  if (compareVersions(appVersion.value, normalizedServerVersion.value) >= 0) return
  // Check skip preference — only skip if the same base server version was previously dismissed
  const serverBase = extractBaseVersion(normalizedServerVersion.value)
  const skipped = localStorage.getItem(STORAGE_KEY)
  if (serverBase === skipped) return
  visible.value = true
}

// Auto-retry when serverConfig loads after show() was called
watch(serverVersion, (newVal) => {
  if (hasAttemptedShow.value && newVal && !visible.value) {
    tryShow()
  }
})

/** Close dialog temporarily (no skip preference persisted) */
function close() {
  visible.value = false
}

/** Close dialog and persist skip preference */
function skip() {
  localStorage.setItem(STORAGE_KEY, extractBaseVersion(normalizedServerVersion.value))
  visible.value = false
}

function downloadApk() {
  window.location.href = '/api/apk'
}
</script>

<style scoped>
.vm-overlay {
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

.vm-panel {
  background: var(--bg-secondary);
  border: 1px solid var(--border-color);
  border-radius: 12px;
  width: 100%;
  max-width: 380px;
  box-shadow: var(--shadow-lg, 0 8px 32px rgba(0,0,0,0.15));
  overflow: hidden;
}

.vm-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 14px 16px 8px;
}

.vm-header h3 {
  margin: 0;
  font-size: 16px;
  font-weight: 700;
  color: var(--text-primary);
}

.vm-close {
  display: flex;
  align-items: center;
  justify-content: center;
  width: 28px;
  height: 28px;
  border: none;
  border-radius: 50%;
  background: var(--bg-tertiary);
  color: var(--text-secondary);
  cursor: pointer;
  transition: background 0.2s;
}

.vm-close:hover {
  background: var(--border-color);
}

.vm-body {
  margin: 0 16px 12px;
  font-size: 13px;
  color: var(--text-secondary);
  line-height: 1.6;
  white-space: pre-line;
}

.vm-footer {
  padding: 8px 16px 14px;
  display: flex;
  gap: 8px;
}

.vm-download {
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

.vm-download:hover {
  opacity: 0.9;
}

.vm-skip {
  padding: 8px 16px;
  border: 1px solid var(--border-color);
  border-radius: 8px;
  background: var(--bg-secondary);
  color: var(--text-secondary);
  font-size: 13px;
  font-weight: 500;
  cursor: pointer;
  transition: all 0.2s;
}

.vm-skip:hover {
  border-color: var(--accent-color);
  color: var(--accent-color);
}

/* Transition */
.vm-fade-enter-active {
  transition: opacity 0.2s ease;
}
.vm-fade-leave-active {
  transition: opacity 0.15s ease;
}
.vm-fade-enter-from,
.vm-fade-leave-to {
  opacity: 0;
}
</style>
