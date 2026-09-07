<template>
  <div class="settings-page">
    <header class="settings-page__header">
      <template v-if="navStack.length > 0">
        <button class="settings-page__back" @click="handleBack">
          <ChevronLeft :size="22" />
        </button>
        <span class="settings-page__title">{{ currentCategoryTitle }}</span>
      </template>
      <template v-else>
        <Settings :size="20" class="settings-page__header-icon" />
        <span class="settings-page__title">{{ t('nav.settings') }}</span>
        <span v-if="serverVersion" class="settings-page__version">{{ serverVersion }}</span>
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
      <button class="fbtn settings-restart-btn settings-restart-btn--pending refresh-spin" :class="{ 'refresh-spin--active': restarting }" :disabled="restarting" @click="handleRestart">
        <RefreshCw :size="14" class="settings-restart-btn__icon" />
        <span>{{ restarting ? t('settings.restarting') : t('settings.restartPending') }}</span>
      </button>
    </footer>
  </div>
</template>

<script setup lang="ts">
import { computed, watch, onMounted } from 'vue'
import { RefreshCw, ChevronLeft, Settings } from 'lucide-vue-next'
import SettingsIndex from './SettingsIndex.vue'
import SettingsCategory from './SettingsCategory.vue'
import SettingsRestartDialog from './SettingsRestartDialog.vue'
import { useSettingsNavigation, consumePendingSettingsCategory, pendingSettingsCategory } from '@/composables/useSettingsNavigation'
import { useSettingsConfig } from '@/composables/useSettingsConfig'
import '@/assets/modal-footer-btn.css'
import { useAgents } from '@/composables/useAgents'
import { useDialog } from '@/composables/useDialog'
import { useFeatureBackHandler, PRIORITY_PAGE } from '@/composables/useEdgeSwipeBack'
import { isSubPageRoute, getSubPageTitleKey } from './settingsFieldMap'

const props = defineProps<{
  active?: boolean
}>()

const {
  t, loadConfig,
  navStack, currentCategory, pushNav, popNav,
  restartDialogVisible, changedColdFields, needsRestart,
  restarting,
  handleRestartNeeded, handleRestart,
  checkAllGuards,
} = useSettingsNavigation()

const { serverConfig } = useSettingsConfig()
const dialog = useDialog()

// ── Back navigation with unsaved changes guard ──

async function handleBack() {
  // Check all registered panel guards (module-level registry)
  if (!checkAllGuards()) {
    const confirmed = await dialog.confirm(
      t('settings.panel.unsavedMessage'),
      {
        title: t('settings.panel.unsavedTitle'),
        confirmText: t('settings.panel.discard'),
        cancelText: t('settings.panel.continueEditing'),
      },
    )
    if (!confirmed) return
  }
  popNav()
}

// Register back handler for settings navigation
useFeatureBackHandler(
  'settings',
  () => !!props.active && navStack.value.length > 0,
  () => handleBack(),
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
    return agent ? agent.name : t('settings.categories.agents')
  }
  // For sub-page routes (colon-separated, except agents), use data-driven title lookup
  if (isSubPageRoute(cat)) {
    const titleKey = getSubPageTitleKey(cat)
    return titleKey ? t(titleKey) : cat
  }
  return t(`settings.categories.${cat}`)
})

const serverVersion = computed(() => serverConfig.value?.version ?? '')

// ── Deep-link into a category from OUTSIDE the settings tab ──
// (AppHeader theme picker → "more appearance options"). The request is stored
// at module level by the caller; when a category is currently open the
// deep-link is pushed on top so the back button returns to it. The request is
// consumed up front so a rejected deep-link (user cancels the unsaved-changes
// confirm) is dropped instead of lingering for the next activation.
async function openPendingDeepLink() {
  const categoryId = consumePendingSettingsCategory()
  if (!categoryId) return
  if (navStack.value[navStack.value.length - 1] === categoryId) return
  // Leaving a category with unsaved panel edits must confirm first (same as back).
  if (!checkAllGuards()) {
    const confirmed = await dialog.confirm(
      t('settings.panel.unsavedMessage'),
      {
        title: t('settings.panel.unsavedTitle'),
        confirmText: t('settings.panel.discard'),
        cancelText: t('settings.panel.continueEditing'),
      },
    )
    if (!confirmed) return
  }
  pushNav(categoryId)
}

// Case: the settings tab was ALREADY active when the deep-link fired and
// switchTab() no-ops — the module-level ref change is what triggers here.
watch(pendingSettingsCategory, (pending) => {
  if (pending && props.active) void openPendingDeepLink()
})

// First-ever open: SettingsPage is lazily mounted by the TabPanel with
// props.active already true, so the props.active watch never fires — the
// deep-link must be consumed on mount.
onMounted(() => {
  void openPendingDeepLink()
})

// Refresh config values when tab becomes active (preserve navigation state)
watch(() => props.active, (val) => {
  if (val) {
    loadConfig()
    void openPendingDeepLink()
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
  height: var(--header-height);
  padding: 0 4px 0 12px;
  border-bottom: 1px solid var(--border-color);
  flex-shrink: 0;
  background: var(--bg-primary);
  gap: 6px;
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

/* Restart footer button — layout + pulse only; pill visuals come from .fbtn. */
.settings-restart-btn {
  width: 100%;
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
  .settings-restart-btn.settings-restart-btn--pending:hover:not(:disabled) {
    background: var(--accent-hover);
  }
}

.settings-restart-btn:active.settings-restart-btn--pending:not(:disabled) {
  background: var(--accent-hover);
}
</style>
