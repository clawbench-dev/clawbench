<template>
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
          <div class="ug-spinner" />
          <p class="ug-message">{{ t('upgrade.checking') }}</p>
        </div>

        <!-- Version info (before upgrade, after check) -->
        <div v-if="!checking && !isInProgress && !isCompleted && !isFailed && hasUpgrade" class="ug-versions">
          <span class="ug-ver-current">{{ state.current_version }}</span>
          <span class="ug-arrow">→</span>
          <span class="ug-ver-latest">{{ state.latest_version }}</span>
        </div>

        <!-- No upgrade available -->
        <div v-if="!checking && !isInProgress && !isCompleted && !isFailed && !hasUpgrade && state.latest_version" class="ug-no-upgrade">
          <p>{{ t('upgrade.alreadyLatest') }}</p>
        </div>

        <!-- Progress area -->
        <div v-if="isInProgress || isRestarting" class="ug-progress-area">
          <div v-if="state.phase === 'downloading'" class="ug-progress-bar">
            <div class="ug-progress-fill" :style="{ width: state.progress + '%' }" />
          </div>
          <div v-else class="ug-spinner" />
          <p class="ug-message">{{ phaseMessage }}</p>
        </div>

        <!-- Completed -->
        <div v-if="isCompleted" class="ug-completed">
          <p class="ug-success">{{ t('upgrade.completed') }}</p>
          <p v-if="state.backup_path" class="ug-backup-path">
            {{ t('upgrade.backupPath', { path: state.backup_path }) }}
          </p>
        </div>

        <!-- Failed -->
        <div v-if="isFailed" class="ug-failed">
          <p>{{ t('upgrade.failed') }}</p>
          <p class="ug-error">{{ state.error }}</p>
        </div>

        <!-- Actions -->
        <div class="ug-footer">
          <button v-if="hasUpgrade && !isInProgress && !isCompleted" class="ug-start" @click="startUpgrade">
            {{ isFailed ? t('upgrade.retry') : t('upgrade.start') }}
          </button>
          <button v-if="canClose" class="ug-cancel" @click="close">
            {{ isCompleted ? t('upgrade.close') : t('upgrade.cancel') }}
          </button>
        </div>
      </div>
    </div>
  </Transition>
</template>

<script setup lang="ts">
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useUpgrade } from '@/composables/useUpgrade'

const visible = ref(false)
defineExpose({ show })

const { t } = useI18n()
const { state, checking, hasUpgrade, isInProgress, isRestarting, isCompleted, isFailed, checkUpgrade, startUpgrade } = useUpgrade()

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

.ug-close:hover { background: var(--border-color); }

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

.ug-spinner {
  width: 28px;
  height: 28px;
  border: 3px solid var(--border-color);
  border-top-color: var(--accent-color);
  border-radius: 50%;
  animation: ug-spin 0.8s linear infinite;
}

@keyframes ug-spin { to { transform: rotate(360deg); } }

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

.ug-footer {
  padding: 8px 16px 14px;
  display: flex;
  gap: 8px;
}

.ug-start {
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

.ug-start:hover { opacity: 0.9; }

.ug-cancel {
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

.ug-cancel:hover {
  border-color: var(--accent-color);
  color: var(--accent-color);
}

.ug-fade-enter-active { transition: opacity 0.2s ease; }
.ug-fade-leave-active { transition: opacity 0.15s ease; }
.ug-fade-enter-from, .ug-fade-leave-to { opacity: 0; }
</style>
