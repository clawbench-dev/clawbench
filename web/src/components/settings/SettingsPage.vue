<template>
  <div class="settings-page">
    <header class="settings-page__header">
      <template v-if="navStack.length > 0">
        <button class="settings-page__back" @click="popNav">
          <ChevronLeft :size="22" />
        </button>
        <span class="settings-page__title">{{ currentCategoryTitle }}</span>
      </template>
      <template v-else>
        <Settings :size="20" class="settings-page__header-icon" />
        <span class="settings-page__title">{{ t('nav.settings') }}</span>
        <span v-if="serverVersion" class="settings-page__version">v{{ serverVersion }}</span>
      </template>
    </header>
    <div class="settings-page__body">
      <SettingsIndex v-if="navStack.length === 0" @navigate="pushNav" />
      <SettingsCategory
        v-else
        :category-id="currentCategory!"
        @navigate="pushNav"
        @restart-needed="handleRestartNeeded"
        @restart-requested="handleRestart"
      />
      <SettingsRestartDialog
        v-if="restartDialogVisible"
        :changed-fields="changedColdFields"
        @restart="handleRestart"
        @later="restartDialogVisible = false"
      />
    </div>
    <footer v-if="needsRestart" class="settings-page__footer">
      <button class="settings-restart-btn settings-restart-btn--pending" :disabled="restarting" @click="handleRestart">
        <RefreshCw :size="14" class="settings-restart-btn__icon" :class="{ 'settings-restart-btn__icon--spin': restarting }" />
        <span>{{ restarting ? t('settings.restarting') : t('settings.restartPending') }}</span>
      </button>
    </footer>
    <!-- Restart loading overlay -->
    <Teleport to="body">
      <div v-if="restartingOverlay" class="restart-overlay">
        <div class="restart-overlay__content">
          <div class="restart-overlay__spinner"></div>
          <div class="restart-overlay__text">{{ t('settings.restartingPleaseWait') }}</div>
        </div>
      </div>
    </Teleport>
  </div>
</template>

<script setup lang="ts">
import { computed, watch } from 'vue'
import { RefreshCw, ChevronLeft, Settings } from 'lucide-vue-next'
import SettingsIndex from './SettingsIndex.vue'
import SettingsCategory from './SettingsCategory.vue'
import SettingsRestartDialog from './SettingsRestartDialog.vue'
import { useSettingsNavigation } from '@/composables/useSettingsNavigation'
import { useSettingsConfig } from '@/composables/useSettingsConfig'
import { useAgents } from '@/composables/useAgents'
import { useFeatureBackHandler, PRIORITY_PAGE } from '@/composables/useEdgeSwipeBack'

const props = defineProps<{
  active?: boolean
}>()

const {
  t, loadConfig,
  navStack, currentCategory, pushNav, popNav, resetState,
  restartDialogVisible, changedColdFields, needsRestart,
  restarting, restartingOverlay,
  handleRestartNeeded, handleRestart,
} = useSettingsNavigation()

const { serverConfig } = useSettingsConfig()

// Register back handler for settings drill-down navigation
useFeatureBackHandler(
  'settings',
  () => !!props.active && navStack.value.length > 0,
  () => popNav(),
  PRIORITY_PAGE,
)

const currentCategoryTitle = computed(() => {
  const cat = currentCategory.value
  if (!cat) return ''
  // For agent detail pages (agents:{id}), show the agent name as title
  if (cat.startsWith('agents:')) {
    const { getAgent } = useAgents()
    const agentId = cat.slice(7)
    const agent = getAgent(agentId)
    return agent ? `${agent.icon} ${agent.name}` : t('settings.categories.agents')
  }
  return t(`settings.categories.${cat}`)
})

const serverVersion = computed(() => serverConfig.value?.version ?? '')

// Reset navigation when tab becomes active
watch(() => props.active, (val) => {
  if (val) {
    loadConfig()
    resetState()
  }
})
</script>

<style scoped>
.settings-page {
  display: flex;
  flex-direction: column;
  height: 100%;
  overflow: hidden;
}

.settings-page__header {
  display: flex;
  align-items: center;
  height: 44px;
  padding: 0 4px 0 12px;
  border-bottom: 1px solid var(--border-color);
  flex-shrink: 0;
  background: var(--bg-primary);
  gap: 8px;
}

.settings-page__back {
  display: flex;
  align-items: center;
  justify-content: center;
  width: 36px;
  height: 36px;
  border: none;
  border-radius: 10px;
  background: transparent;
  color: var(--text-primary);
  cursor: pointer;
  flex-shrink: 0;
  -webkit-tap-highlight-color: transparent;
}

@media (hover: hover) {
  .settings-page__back:hover {
    background: var(--bg-tertiary);
  }
}

.settings-page__back:active {
  background: var(--bg-tertiary);
}

.settings-page__title {
  font-size: 17px;
  font-weight: 600;
  color: var(--text-primary);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.settings-page__header-icon {
  flex-shrink: 0;
  color: var(--text-secondary);
}

.settings-page__version {
  margin-left: auto;
  font-size: 12px;
  font-weight: 500;
  color: var(--text-muted);
  background: var(--bg-tertiary);
  padding: 2px 8px;
  border-radius: 999px;
  flex-shrink: 0;
}

.settings-page__body {
  flex: 1;
  overflow-y: auto;
  -webkit-overflow-scrolling: touch;
  position: relative;
}

.settings-page__footer {
  display: flex;
  align-items: center;
  justify-content: flex-end;
  padding: 8px 12px;
  border-top: 1px solid var(--border-color);
  flex-shrink: 0;
  gap: 8px;
  padding-bottom: calc(8px + env(safe-area-inset-bottom, 0px));
}

/* Restart footer button */
.settings-restart-btn {
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 6px;
  width: 100%;
  padding: 10px 16px;
  border: none;
  border-radius: 10px;
  background: var(--bg-tertiary);
  color: var(--text-secondary);
  font-size: 14px;
  font-weight: 500;
  cursor: pointer;
  text-align: center;
  transition: background 0.2s, color 0.2s, box-shadow 0.2s;
}

.settings-restart-btn:disabled {
  opacity: 0.6;
  cursor: not-allowed;
}

.settings-restart-btn--pending {
  background: var(--accent-color);
  color: #fff;
  animation: restart-pulse 0.8s ease-in-out infinite;
}

@keyframes restart-pulse {
  0%, 100% { box-shadow: 0 0 0 0 color-mix(in srgb, var(--accent-color, #0066cc) 0%, transparent); }
  50% { box-shadow: 0 0 8px 3px color-mix(in srgb, var(--accent-color, #0066cc) 40%, transparent); }
}

@media (hover: hover) {
  .settings-restart-btn:hover:not(:disabled):not(.settings-restart-btn--pending) {
    background: var(--bg-secondary);
  }
  .settings-restart-btn.settings-restart-btn--pending:hover:not(:disabled) {
    background: var(--accent-hover);
  }
}

.settings-restart-btn:active:not(.settings-restart-btn--pending) {
  background: var(--bg-secondary);
}

.settings-restart-btn:active.settings-restart-btn--pending:not(:disabled) {
  background: var(--accent-hover);
}

.settings-restart-btn__icon--spin {
  animation: spin 1s linear infinite;
}

@keyframes spin {
  from { transform: rotate(0deg); }
  to { transform: rotate(360deg); }
}

/* Restart loading overlay */
.restart-overlay {
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

.restart-overlay__content {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 20px;
  padding: 40px 48px;
  border-radius: 16px;
  background: var(--bg-primary);
  box-shadow: var(--shadow-md);
}

.restart-overlay__spinner {
  width: 36px;
  height: 36px;
  border: 3px solid var(--border-color);
  border-top-color: var(--accent-color);
  border-radius: 50%;
  animation: spin 0.8s linear infinite;
}

.restart-overlay__text {
  font-size: 15px;
  font-weight: 500;
  color: var(--text-primary);
  white-space: nowrap;
}
</style>
