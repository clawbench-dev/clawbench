<template>
  <!-- Teleport to body: this full-viewport overlay renders inside the settings
       tab-panel whose `isolation: isolate` stacking context traps it below the
       chat column in wide-screen mode, no matter the z-index. Escaping to body
       keeps it above the whole app. -->
  <Teleport to="body">
    <Transition name="ug-fade">
      <div v-if="visible" class="ug-overlay">
        <div class="ug-panel">
        <!-- Header -->
        <div class="ug-header">
          <h3>{{ t('upgrade.title') }}</h3>
          <button v-if="canClose" class="ug-close" @click="close" aria-label="Close">
            <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" width="18" height="18">
              <path d="M18 6L6 18M6 6l12 12"/>
            </svg>
          </button>
        </div>

        <!-- Checking -->
        <div v-if="checking && !isInProgress" class="ug-progress-area">
          <LoadingIndicator size="md" :label="t('upgrade.checking')" />
        </div>

        <!-- Version info (before upgrade, after check) -->
        <div v-if="!checking && !isInProgress && !isCompleted && !isFailed && hasUpgrade" class="ug-versions">
          <span class="ug-ver-current">{{ state.current_version }}</span>
          <span class="ug-arrow">→</span>
          <span class="ug-ver-latest">{{ state.latest_version }}</span>
        </div>

        <!-- Release notes link -->
        <a
          v-if="!checking && !isInProgress && !isCompleted && !isFailed && hasUpgrade && releaseNotesUrl"
          class="ug-release-link"
          :href="releaseNotesUrl"
          target="_blank"
          rel="noopener noreferrer"
        >
          {{ t('upgrade.releaseNotes', { version: state.latest_version }) }}
        </a>

        <!-- No upgrade available -->
        <div v-if="!checking && !isInProgress && !isCompleted && !isFailed && !hasUpgrade && state.latest_version" class="ug-no-upgrade">
          <p>{{ t('upgrade.alreadyLatest') }}</p>
        </div>

        <!-- Pre-flight warning: install directory not writable. Shown before the
             user starts, so they never download 40MB only to fail at backup. -->
        <div v-if="showWritableWarning" class="ug-warn">
          <p class="ug-warn-title">{{ t('upgrade.installDirNotWritableTitle') }}</p>
          <p class="ug-warn-body">{{ t('upgrade.installDirNotWritableBody', { dir: installDir || '—' }) }}</p>
          <p class="ug-warn-hint">{{ t('upgrade.installDirNotWritableHint') }}</p>
        </div>

        <!-- Docker advisory: a container CAN self-upgrade, but a later rebuild
             from the unchanged image reverts it — recommend the image path
             without blocking the in-place upgrade. -->
        <div v-if="showDockerHint" class="ug-hint">
          <p class="ug-hint-title">{{ t('upgrade.dockerHintTitle') }}</p>
          <p class="ug-hint-body">{{ t('upgrade.dockerHintBody') }}</p>
          <code class="ug-hint-cmd">docker pull ghcr.io/clawbench-dev/clawbench:latest &amp;&amp; docker compose up -d</code>
          <p class="ug-hint-warn">{{ t('upgrade.dockerHintRestart') }}</p>
        </div>

        <!-- Progress area -->
        <div v-if="isInProgress || isRestarting" class="ug-progress-area">
          <template v-if="state.phase === 'downloading'">
            <div class="ug-progress-bar">
              <div class="ug-progress-fill" :style="{ width: state.progress + '%' }" />
            </div>
            <p class="ug-message">{{ phaseMessage }}</p>
          </template>
          <LoadingIndicator v-else size="md" :label="phaseMessage" />
        </div>

        <!-- Completed -->
        <div v-if="isCompleted" class="ug-completed">
          <p class="ug-success">{{ t('upgrade.completed') }}</p>
          <p v-if="state.backup_path" class="ug-backup-path">
            {{ t('upgrade.backupPath', { path: state.backup_path }) }}
          </p>
        </div>

        <!-- Failed: install-dir failures get an actionable, localized message
             instead of the raw "permission denied" string. -->
        <div v-if="isFailed" class="ug-failed">
          <template v-if="state.error_code === ERR_INSTALL_DIR_NOT_WRITABLE">
            <p class="ug-error-title">{{ t('upgrade.installDirNotWritableTitle') }}</p>
            <p class="ug-error">{{ t('upgrade.installDirNotWritableBody', { dir: installDir || '—' }) }}</p>
            <p class="ug-error-hint">{{ t('upgrade.installDirNotWritableHint') }}</p>
          </template>
          <template v-else>
            <p>{{ t('upgrade.failed') }}</p>
            <p class="ug-error">{{ state.error }}</p>
          </template>
        </div>

        <!-- Actions -->
        <div class="ug-footer">
          <button v-if="hasUpgrade && !isInProgress && !isCompleted" class="fbtn fbtn-primary ug-start" @click="startUpgrade">
            {{ isFailed ? t('upgrade.retry') : t('upgrade.start') }}
          </button>
          <button v-if="canClose" class="fbtn ug-cancel" @click="close">
            {{ isCompleted ? t('upgrade.close') : t('upgrade.cancel') }}
          </button>
        </div>
      </div>
    </div>
    </Transition>
  </Teleport>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import LoadingIndicator from '@/components/common/LoadingIndicator.vue'
import { useUpgrade, ERR_INSTALL_DIR_NOT_WRITABLE } from '@/composables/useUpgrade'
import { registerBackHandler, PRIORITY_OVERLAY } from '@/composables/useBackHandler'
import '@/assets/modal-footer-btn.css'

const visible = ref(false)
let unregisterBack: (() => void) | null = null
defineExpose({ show })

const { t } = useI18n()
const {
  state, checking, hasUpgrade, isInProgress, isRestarting, isCompleted, isFailed,
  installWritable, installDir, isDocker, checkUpgrade, startUpgrade, releaseNotesUrl,
} = useUpgrade()

/**
 * Show the writability warning only while the user is deciding whether to
 * start: an upgrade must be available and the check must have settled. Hidden
 * during an active upgrade, after completion, after any failure (failures
 * render their own dedicated block), and when there is nothing to upgrade.
 */
const showWritableWarning = computed(() =>
  hasUpgrade.value &&
  !checking.value &&
  !installWritable.value &&
  !isInProgress.value &&
  !isCompleted.value &&
  !isFailed.value,
)

/**
 * Docker advisory — same visibility window as the writability warning: only
 * while the user is deciding. Informational, never blocking.
 */
const showDockerHint = computed(() =>
  hasUpgrade.value &&
  !checking.value &&
  isDocker.value &&
  !isInProgress.value &&
  !isCompleted.value &&
  !isFailed.value,
)

/** Show the dialog and check for upgrades */
function show() {
  visible.value = true
  if (!state.phase) {
    checkUpgrade()
  }
}

const canClose = computed(() => {
  if (isCompleted.value || isFailed.value) return true
  if (!isInProgress.value) return true
  return false
})

const phaseMessage = computed(() => {
  const phase = state.phase
  if (phase === 'checking') return t('upgrade.checking')
  if (phase === 'downloading') return t('upgrade.downloading')
  if (phase === 'extracting') return t('upgrade.extracting')
  if (phase === 'backing_up') return t('upgrade.backingUp')
  if (phase === 'replacing') return t('upgrade.replacing')
  if (phase === 'restarting') return t('upgrade.restarting')
  return state.message
})

function close() {
  visible.value = false
}

// Register back handler when dialog opens (only if canClose), unregister on close
watch(visible, (v) => {
  if (v) {
    unregisterBack = registerBackHandler({
      id: 'upgrade-dialog',
      canGoBack: () => visible.value && canClose.value,
      goBack: () => close(),
      priority: PRIORITY_OVERLAY,
    })
  } else if (unregisterBack) {
    unregisterBack()
    unregisterBack = null
  }
})
</script>

<style scoped>
.ug-overlay {
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

.ug-panel {
  background: var(--bg-secondary);
  border: 1px solid var(--border-color);
  border-radius: 12px;
  width: 100%;
  max-width: 380px;
  box-shadow: var(--shadow-lg, 0 8px 32px rgba(0,0,0,0.15));
  overflow: hidden;
}

.ug-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 14px 16px 8px;
}

.ug-header h3 {
  margin: 0;
  font-size: 16px;
  font-weight: 700;
  color: var(--text-primary);
}

.ug-close {
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

@media (hover: hover) {
  .ug-close:hover { background: var(--border-color); }
}

.ug-versions {
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 10px;
  padding: 12px 16px;
  font-size: 14px;
}

.ug-ver-current { color: var(--text-secondary); }
.ug-arrow { color: var(--text-muted); }
.ug-ver-latest { color: var(--accent-color); font-weight: 600; }

.ug-release-link {
  display: block;
  text-align: center;
  margin: 0 16px 12px;
  font-size: 13px;
  font-weight: 500;
  color: var(--accent-color);
  text-decoration: none;
  cursor: pointer;
}

.ug-release-link:hover {
  text-decoration: underline;
}

.ug-progress-area {
  padding: 20px 16px;
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 14px;
}

.ug-progress-bar {
  width: 100%;
  height: 6px;
  background: var(--bg-tertiary);
  border-radius: 3px;
  overflow: hidden;
}

.ug-progress-fill {
  height: 100%;
  background: var(--accent-color);
  border-radius: 3px;
  transition: width 0.3s ease;
}

.ug-message {
  margin: 0;
  font-size: 13px;
  color: var(--text-secondary);
}

.ug-completed, .ug-failed {
  padding: 16px;
  text-align: center;
}

.ug-success {
  margin: 0 0 8px;
  font-size: 14px;
  font-weight: 600;
  color: var(--accent-color);
}

.ug-backup-path {
  margin: 0;
  font-size: 12px;
  color: var(--text-muted);
  word-break: break-all;
}

.ug-no-upgrade {
  padding: 16px;
  text-align: center;
}

.ug-no-upgrade p {
  margin: 0;
  font-size: 13px;
  color: var(--text-secondary);
}

.ug-error {
  margin: 8px 0 0;
  font-size: 12px;
  color: var(--text-danger, #e53e3e);
  word-break: break-word;
}

/* Pre-flight install-directory warning (amber, non-fatal) */
.ug-warn {
  margin: 0 16px 12px;
  padding: 10px 12px;
  border-radius: 8px;
  background: color-mix(in srgb, var(--text-warning, #d69e2e) 12%, transparent);
  border: 1px solid color-mix(in srgb, var(--text-warning, #d69e2e) 35%, transparent);
  text-align: left;
}

.ug-warn-title {
  margin: 0 0 6px;
  font-size: 13px;
  font-weight: 600;
  color: var(--text-warning, #d69e2e);
}

.ug-warn-body {
  margin: 0 0 6px;
  font-size: 12px;
  color: var(--text-secondary);
  word-break: break-word;
}

.ug-warn-hint {
  margin: 0;
  font-size: 12px;
  color: var(--text-muted);
}

/* Docker advisory (informational, non-fatal) */
.ug-hint {
  margin: 0 16px 12px;
  padding: 10px 12px;
  border-radius: 8px;
  background: color-mix(in srgb, var(--accent-color) 10%, transparent);
  border: 1px solid color-mix(in srgb, var(--accent-color) 30%, transparent);
  text-align: left;
}

.ug-hint-title {
  margin: 0 0 6px;
  font-size: 13px;
  font-weight: 600;
  color: var(--accent-color);
}

.ug-hint-body {
  margin: 0 0 8px;
  font-size: 12px;
  color: var(--text-secondary);
  word-break: break-word;
}

.ug-hint-cmd {
  display: block;
  padding: 6px 8px;
  border-radius: 6px;
  background: var(--bg-tertiary);
  color: var(--text-primary);
  font-size: 11px;
  font-family: var(--font-mono, monospace);
  word-break: break-all;
  user-select: all;
}

.ug-hint-warn {
  margin: 8px 0 0;
  font-size: 11px;
  color: var(--text-warning, #d69e2e);
  line-height: 1.5;
}

.ug-error-title {
  margin: 0;
  font-size: 13px;
  font-weight: 600;
  color: var(--text-danger, #e53e3e);
}

.ug-error-hint {
  margin: 6px 0 0;
  font-size: 12px;
  color: var(--text-muted);
}

.ug-footer {
  padding: 8px 16px 14px;
  display: flex;
  gap: 8px;
}

/* Layout only — visual styles come from the shared .fbtn pills. */
.ug-start {
  flex: 1;
}

.ug-fade-enter-active { transition: opacity 0.2s ease; }
.ug-fade-leave-active { transition: opacity 0.15s ease; }
.ug-fade-enter-from, .ug-fade-leave-to { opacity: 0; }
</style>
