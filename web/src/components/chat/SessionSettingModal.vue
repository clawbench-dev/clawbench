<template>
  <ModalDialog
    :open="show"
    :title="agentName"
    @close="handleClose"
  >
    <!-- Tab bar -->
    <div class="session-setting-tabs">
      <button class="model-tab" :class="{ active: activeTab === 'model' }" @click="activeTab = 'model'">
        <Cpu :size="13" />{{ t('chat.modelSwitcher.title') }}
      </button>
      <button class="model-tab" :class="{ active: activeTab === 'thinking' }" @click="activeTab = 'thinking'">
        <Brain :size="13" />{{ t('chat.thinkingEffortSwitcher.title') }}
      </button>
      <button class="model-tab" :class="{ active: activeTab === 'mode' }" @click="activeTab = 'mode'">
        <Compass :size="13" />{{ t('chat.modeSwitcher.title') }}
      </button>
      <button class="model-tab" :class="{ active: activeTab === 'transport' }" @click="activeTab = 'transport'">
        <Cable :size="13" />{{ t('chat.transportSwitcher.title') }}
      </button>
    </div>

    <!-- Model tab -->
    <div v-if="activeTab === 'model'" class="model-tab-content">
      <!-- Search + Refresh row -->
      <div class="model-search-row">
        <input
          class="model-search-input"
          type="text"
          :placeholder="t('chat.sessionSetting.searchPlaceholder')"
          v-model="searchQuery"
        />
        <button v-if="canRefresh" class="refresh-btn" :class="{ loading: refreshing }" @click="handleRefresh" :disabled="refreshing" :title="t('chat.sessionSetting.refresh')">
          <RefreshCw :size="14" :class="{ spin: refreshing }" />
        </button>
      </div>
      <!-- Model list -->
      <div class="model-list">
        <div
          v-for="(m, idx) in filteredModels"
          :key="m.id"
          class="model-item-wrapper"
        >
          <button
            class="model-item"
            :class="{ current: m.id === currentModelId, 'is-default': m.id === defaultModelId }"
            @click="selectModel(m)"
            @contextmenu.prevent="showDefaultMenu(m)"
            @touchstart="onTouchStart(m, $event)"
            @touchend="onTouchEnd"
            @touchmove="onTouchMove"
          >
            <span class="model-item-indicator" :class="{ active: m.id === currentModelId }"></span>
            <span class="model-item-name">{{ m.name }}</span>
            <span v-if="m.id === defaultModelId" class="default-badge">{{ t('chat.sessionSetting.defaultBadge') }}</span>
            <button v-if="m.id !== defaultModelId" class="set-default-btn" @click.stop="setDefaultModel(m)" :title="t('chat.sessionSetting.setAsDefault')">
              <Star :size="12" />
            </button>
          </button>
          <div v-if="idx < filteredModels.length - 1" class="model-divider"></div>
        </div>
        <div v-if="filteredModels.length === 0" class="model-empty">
          {{ searchQuery ? t('chat.sessionSetting.noSearchResults') : t('chat.sessionSetting.noModels') }}
        </div>
      </div>
    </div>

    <!-- Thinking effort tab -->
    <div v-if="activeTab === 'thinking'" class="model-tab-content">
      <div v-if="thinkingLevels.length === 0" class="tab-empty-hint">
        {{ t('chat.sessionSetting.notAvailable') }}
      </div>
      <div v-else class="model-list">
        <div
          v-for="(level, idx) in thinkingLevels"
          :key="level.id"
          class="model-item-wrapper"
        >
          <button
            class="thinking-item"
            :class="{ current: level.id === currentThinkingEffort, 'is-default': level.id === defaultThinkingEffort }"
            @click="selectThinkingEffort(level.id)"
            @contextmenu.prevent="showThinkingDefaultMenu(level.id)"
            @touchstart="onTouchStartThinking(level.id, $event)"
            @touchend="onTouchEnd"
            @touchmove="onTouchMove"
          >
            <span class="model-item-indicator" :class="{ active: level.id === currentThinkingEffort }"></span>
            <span class="model-item-name">{{ level.name }}</span>
            <span v-if="level.id === defaultThinkingEffort" class="default-badge">{{ t('chat.sessionSetting.defaultBadge') }}</span>
            <button v-if="level.id !== defaultThinkingEffort" class="set-default-btn" @click.stop="setDefaultThinkingEffort(level.id)" :title="t('chat.sessionSetting.setAsDefault')">
              <Star :size="12" />
            </button>
          </button>
          <div v-if="idx < thinkingLevels.length - 1" class="model-divider"></div>
        </div>
      </div>
    </div>

    <!-- Transport tab -->
    <div v-if="activeTab === 'transport'" class="model-tab-content">
      <div class="model-list">
        <!-- Only show ACP option for agents that support dual transport -->
        <div v-if="supportsDualTransport(props.agentId || '')" class="model-item-wrapper">
          <button
            class="thinking-item"
            :class="{ current: isACP, 'is-default': defaultTransport === 'acp-stdio' }"
            @click="selectTransport('acp-stdio')"
          >
            <span class="model-item-indicator" :class="{ active: isACP }"></span>
            <span class="model-item-name">{{ t('chat.transportSwitcher.acp') }}</span>
            <span v-if="defaultTransport === 'acp-stdio'" class="default-badge">{{ t('chat.sessionSetting.defaultBadge') }}</span>
            <button v-if="defaultTransport !== 'acp-stdio'" class="set-default-btn" @click.stop="setDefaultTransport('acp-stdio')" :title="t('chat.sessionSetting.setAsDefault')">
              <Star :size="12" />
            </button>
          </button>
          <div class="model-divider"></div>
        </div>
        <div class="model-item-wrapper">
          <button
            class="thinking-item"
            :class="{ current: !isACP, 'is-default': defaultTransport !== 'acp-stdio' }"
            @click="selectTransport('cli')"
          >
            <span class="model-item-indicator" :class="{ active: !isACP }"></span>
            <span class="model-item-name">{{ t('chat.transportSwitcher.cli') }}</span>
            <span v-if="defaultTransport !== 'acp-stdio'" class="default-badge">{{ t('chat.sessionSetting.defaultBadge') }}</span>
            <button v-if="defaultTransport === 'acp-stdio'" class="set-default-btn" @click.stop="setDefaultTransport('cli')" :title="t('chat.sessionSetting.setAsDefault')">
              <Star :size="12" />
            </button>
          </button>
        </div>
      </div>
    </div>

    <!-- Mode tab -->
    <div v-if="activeTab === 'mode'" class="model-tab-content">
      <div v-if="availableModes.length === 0 || !isACP" class="tab-empty-hint">
        {{ t('chat.sessionSetting.notAvailable') }}
      </div>
      <template v-else>
      <div class="model-list">
        <div
          v-for="(mode, idx) in availableModes"
          :key="mode.id"
          class="model-item-wrapper"
        >
          <button
            class="thinking-item"
            :class="{ current: mode.id === currentModeId }"
            @click="selectMode(mode)"
          >
            <span class="model-item-indicator" :class="{ active: mode.id === currentModeId }"></span>
            <span class="model-item-name">{{ mode.name || mode.id }}</span>
          </button>
          <div v-if="idx < availableModes.length - 1" class="model-divider"></div>
        </div>
      </div>
      <!-- Auto-Approve toggle (embedded in Mode tab) -->
      <div class="auto-approve-section">
        <div class="model-divider"></div>
        <div class="auto-approve-toggle">
          <div class="auto-approve-label">
            <span class="auto-approve-title">{{ t('chat.autoApprove.title') }}</span>
            <span class="auto-approve-desc">{{ t('chat.autoApprove.description') }}</span>
          </div>
          <label class="toggle-switch">
            <input type="checkbox" :checked="autoApprove" @change="toggleAutoApprove($event.target.checked)" />
            <span class="toggle-slider"></span>
          </label>
        </div>
      </div>
      </template>
    </div>

    <!-- Long-press PopupMenu for "Set as Default" (kept for backward compat) -->
    <PopupMenu v-model:show="showDefaultPopupMenu" :target-element="longPressTarget" :max-width="180" :max-height="100" :menu-items-count="1">
      <button class="popup-set-default" @click="setAsDefault">
        {{ t('chat.sessionSetting.setAsDefault') }}
      </button>
    </PopupMenu>
  </ModalDialog>
</template>

<script setup>
import { ref, computed, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { RefreshCw, Star, Cpu, Brain, Compass, Cable } from 'lucide-vue-next'
import ModalDialog from '@/components/common/ModalDialog.vue'
import PopupMenu from '@/components/common/PopupMenu.vue'
import { useAgents, restoreOriginalModels, populateACPStateFromCache, invalidateACPStateCache } from '@/composables/useAgents'
import { useSessionIdentity, clearModeState, clearCommandState, clearThinkingEffortState } from '@/composables/useSessionIdentity'
import { apiPost } from '@/utils/api'
import { patchAgentPref } from '@/composables/useSettingsConfig'
import { useToast } from '@/composables/useToast'

const props = defineProps({
  show: Boolean,
  agentId: String,
  initialTab: { type: String, default: 'model' },
})

const emit = defineEmits(['update:show', 'switch-model', 'switch-thinking-effort', 'switch-mode', 'switch-transport'])

const { t } = useI18n()
const toast = useToast()
const { getAgentModels, getAgentThinkingEffortLevels, getAgent, updateAgentField, getDefaultModelId, canRefreshModels, supportsDualTransport, getAgentTransport } = useAgents()
const { currentModelId, currentThinkingEffort, currentModeId, currentTransport, availableThinkingEfforts, availableModes, autoApprove, toggleAutoApprove } = useSessionIdentity()

const activeTab = ref('model')
const searchQuery = ref('')
const refreshing = ref(false)
const showDefaultPopupMenu = ref(false)
const longPressTarget = ref(null)
const pendingDefaultModel = ref(null)
const pendingDefaultThinking = ref(null)

// Long-press state
let longPressTimer = null
const longPressTriggered = ref(false)

// Computed data
const models = computed(() => getAgentModels(props.agentId || ''))
// Thinking levels: prefer ACP-provided levels (with id+name), fallback to agent config
const thinkingLevels = computed(() => {
  const acpLevels = availableThinkingEfforts.value
  if (acpLevels.length > 0) return acpLevels
  // Fallback: agent YAML config (string array)
  return getAgentThinkingEffortLevels(props.agentId || '').map(id => ({ id, name: id }))
})
const canRefresh = computed(() => canRefreshModels(props.agentId || ''))

const isACP = computed(() => {
  // Prefer session-level transport, fallback to agent config
  if (currentTransport.value) return currentTransport.value === 'acp-stdio'
  return getAgentTransport(props.agentId || '') === 'acp-stdio'
})

const agentName = computed(() => {
  const agent = getAgent(props.agentId || '')
  return agent ? `${agent.icon} ${agent.name}` : ''
})

const defaultModelId = computed(() => {
  const agent = getAgent(props.agentId || '')
  return agent?.preferredModel || ''
})

const defaultThinkingEffort = computed(() => {
  const agent = getAgent(props.agentId || '')
  return agent?.preferredThinkingEffort || ''
})

const defaultTransport = computed(() => {
  return getAgentTransport(props.agentId || '')
})

const filteredModels = computed(() => {
  const q = searchQuery.value.toLowerCase().trim()
  if (!q) return models.value
  return models.value.filter(m => m.name.toLowerCase().includes(q) || m.id.toLowerCase().includes(q))
})

// Reset search when tab changes or modal reopens
watch(() => props.show, (val) => {
  if (val) {
    searchQuery.value = ''
    activeTab.value = props.initialTab || 'model'
  }
})

// --- Model selection ---

function selectModel(model) {
  if (longPressTriggered.value) {
    longPressTriggered.value = false
    return
  }
  emit('switch-model', model)
  emit('update:show', false)
}

// --- Thinking effort selection ---

function selectThinkingEffort(level) {
  if (longPressTriggered.value) {
    longPressTriggered.value = false
    return
  }
  emit('switch-thinking-effort', level)
  emit('update:show', false)
}

// --- Mode selection ---

function selectMode(mode) {
  emit('switch-mode', mode)
  emit('update:show', false)
}

// --- Transport selection ---

async function selectTransport(transport) {
  if (transport === 'acp-stdio' && isACP.value) return
  if (transport === 'cli' && !isACP.value) return

  // Session-scoped: update local ref only (like selectModel)
  currentTransport.value = transport

  // When switching to CLI, clear ACP-specific state and restore CLI models
  if (transport === 'cli') {
    clearModeState()
    clearCommandState()
    clearThinkingEffortState()
    restoreOriginalModels(props.agentId || '')
    invalidateACPStateCache(props.agentId || '')
  }

  // When switching to ACP, re-populate ACP state from cache (force-refresh)
  if (transport === 'acp-stdio') {
    invalidateACPStateCache(props.agentId || '')
    await populateACPStateFromCache(props.agentId || '')
  }

  emit('switch-transport', transport)
  emit('update:show', false)
}

// --- Refresh ---

async function handleRefresh() {
  if (refreshing.value) return
  refreshing.value = true
  try {
    const data = await apiPost(`/api/agents/${props.agentId}/refresh-models`, {})
    if (data?.models) {
      // Update agent models in memory
      updateAgentField(props.agentId, 'models', data.models)
      toast.show(t('chat.sessionSetting.refreshSuccess'), { icon: '✓', type: 'success', duration: 2000 })
    }
  } catch (err) {
    const msgKey = err?.msgKey
    if (msgKey === 'CLINotFound') {
      toast.show(t('chat.sessionSetting.cliNotFound'), { icon: '⚠️', type: 'error', duration: 4000 })
    } else if (msgKey === 'ModelDiscoveryNotSupported') {
      toast.show(t('chat.sessionSetting.discoveryNotSupported'), { icon: '⚠️', type: 'error', duration: 4000 })
    } else {
      toast.show(t('chat.sessionSetting.refreshFailed'), { icon: '⚠️', type: 'error', duration: 3000 })
    }
  } finally {
    refreshing.value = false
  }
}

// --- Set default model/thinking directly via star button ---

async function setDefaultModel(model) {
  try {
    await patchAgentPref(props.agentId, 'preferred_model', model.id)
    updateAgentField(props.agentId, 'preferredModel', model.id)
  } catch (err) {
    toast.show(t('settings.saveFailed'), { icon: '⚠️', type: 'error', duration: 3000 })
  }
}

async function setDefaultThinkingEffort(level) {
  try {
    await patchAgentPref(props.agentId, 'preferred_thinking_effort', level)
    updateAgentField(props.agentId, 'preferredThinkingEffort', level)
  } catch (err) {
    toast.show(t('settings.saveFailed'), { icon: '⚠️', type: 'error', duration: 3000 })
  }
}

async function setDefaultTransport(transport) {
  try {
    await patchAgentPref(props.agentId, 'transport', transport)
    updateAgentField(props.agentId, 'transport', transport)
  } catch (err) {
    toast.show(t('settings.saveFailed'), { icon: '⚠️', type: 'error', duration: 3000 })
  }
}

// --- Long-press for "Set as Default" (kept for popup menu compat) ---

function onTouchStart(model, event) {
  longPressTriggered.value = false
  longPressTimer = setTimeout(() => {
    // Resolve element at callback time, not at touchstart time,
    // to avoid stale DOM reference if Vue re-renders during the delay.
    const el = event.target.closest('.model-item, .thinking-item')
    longPressTriggered.value = true
    pendingDefaultModel.value = model.id
    pendingDefaultThinking.value = null
    longPressTarget.value = el
    showDefaultPopupMenu.value = true
  }, 500)
}

function onTouchStartThinking(level, event) {
  longPressTriggered.value = false
  longPressTimer = setTimeout(() => {
    const el = event.target.closest('.model-item, .thinking-item')
    longPressTriggered.value = true
    pendingDefaultThinking.value = level
    pendingDefaultModel.value = null
    longPressTarget.value = el
    showDefaultPopupMenu.value = true
  }, 500)
}

function onTouchEnd() {
  clearTimeout(longPressTimer)
  // Reset longPressTriggered after a tick so the click handler can check it
  if (longPressTriggered.value) {
    setTimeout(() => { longPressTriggered.value = false }, 100)
  }
}

function onTouchMove() {
  clearTimeout(longPressTimer)
}

function showDefaultMenu(model) {
  pendingDefaultModel.value = model.id
  pendingDefaultThinking.value = null
  // For contextmenu, use the event target as anchor
  longPressTarget.value = null
  showDefaultPopupMenu.value = true
}

function showThinkingDefaultMenu(level) {
  pendingDefaultThinking.value = level
  pendingDefaultModel.value = null
  longPressTarget.value = null
  showDefaultPopupMenu.value = true
}

async function setAsDefault() {
  showDefaultPopupMenu.value = false
  longPressTarget.value = null
  try {
    if (pendingDefaultModel.value !== null) {
      await patchAgentPref(props.agentId, 'preferred_model', pendingDefaultModel.value)
      updateAgentField(props.agentId, 'preferredModel', pendingDefaultModel.value)
    } else if (pendingDefaultThinking.value !== null) {
      await patchAgentPref(props.agentId, 'preferred_thinking_effort', pendingDefaultThinking.value)
      updateAgentField(props.agentId, 'preferredThinkingEffort', pendingDefaultThinking.value)
    }
  } catch (err) {
    toast.show(t('settings.saveFailed'), { icon: '⚠️', type: 'error', duration: 3000 })
  }
}

function handleClose() {
  emit('update:show', false)
}

defineExpose({
  activeTab,
  searchQuery,
  showDefaultPopupMenu,
  refreshing,
  _setActiveTab(val) { activeTab.value = val },
  _setSearchQuery(val) { searchQuery.value = val },
  _getActiveTab() { return activeTab.value },
  _getFilteredModels() { return filteredModels.value },
  _getSearchQuery() { return searchQuery.value },
})
</script>

<style scoped>
.session-setting-tabs {
  display: flex;
  gap: 0;
  border-bottom: 1px solid var(--border-color, #e5e5e5);
  flex-shrink: 0;
}

.model-tab {
  flex: 1;
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 4px;
  padding: 10px 12px;
  border: none;
  background: none;
  color: var(--text-muted, #999);
  font-size: 13px;
  font-weight: 500;
  cursor: pointer;
  border-bottom: 2px solid transparent;
  transition: color 0.15s, border-color 0.15s;
  -webkit-tap-highlight-color: transparent;
}

.model-tab.active {
  color: var(--accent-color, #0066cc);
  border-bottom-color: var(--accent-color, #0066cc);
}

.model-tab-content {
  display: flex;
  flex-direction: column;
  min-height: 0;
  flex: 1;
}

.model-search-row {
  display: flex;
  align-items: center;
  gap: 6px;
  padding: 8px 10px;
  border-bottom: 1px solid var(--border-color, #e5e5e5);
  flex-shrink: 0;
}

.model-search-input {
  flex: 1;
  padding: 6px 10px;
  border: 1px solid var(--border-color, #e5e5e5);
  border-radius: 8px;
  background: var(--bg-primary, #fff);
  color: var(--text-primary);
  font-size: 13px;
  outline: none;
  transition: border-color 0.15s;
}

.model-search-input:focus {
  border-color: var(--accent-color, #0066cc);
}

.model-search-input::placeholder {
  color: var(--text-muted, #999);
}

.refresh-btn {
  display: flex;
  align-items: center;
  justify-content: center;
  width: 30px;
  height: 30px;
  border: none;
  border-radius: 8px;
  background: var(--bg-tertiary, #f0f0f0);
  color: var(--text-muted, #999);
  cursor: pointer;
  flex-shrink: 0;
  transition: background 0.15s, color 0.15s;
}

.refresh-btn:hover:not(:disabled) {
  background: var(--accent-color, #0066cc);
  color: #fff;
}

.refresh-btn:disabled {
  opacity: 0.5;
  cursor: not-allowed;
}

.refresh-btn.loading {
  color: var(--accent-color, #0066cc);
}

.spin {
  animation: spin 1s linear infinite;
}

@keyframes spin {
  from { transform: rotate(0deg); }
  to { transform: rotate(360deg); }
}

.model-list {
  flex: 1;
  overflow-y: auto;
  padding: 0;
}

.model-item-wrapper {
  /* No extra styling — items are flush */
}

.model-divider {
  height: 1px;
  background: var(--border-color, #e5e5e5);
}

.model-item,
.thinking-item {
  display: flex;
  align-items: center;
  gap: 8px;
  width: 100%;
  padding: 10px 14px;
  border: none;
  background: none;
  color: var(--text-primary);
  font-size: 13px;
  cursor: pointer;
  text-align: left;
  transition: background 0.12s;
  -webkit-tap-highlight-color: transparent;
}

.model-item:hover,
.thinking-item:hover {
  background: var(--bg-tertiary, #f0f0f0);
}

.model-item.current,
.thinking-item.current {
  background: color-mix(in srgb, var(--accent-color, #0066cc) 6%, transparent);
}

.model-item-indicator {
  width: 8px;
  height: 8px;
  border-radius: 50%;
  flex-shrink: 0;
  background: transparent;
  transition: background 0.15s;
}

.model-item-indicator.active {
  background: var(--accent-color, #0066cc);
}

.model-item-name {
  flex: 1;
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.model-item.current .model-item-name,
.thinking-item.current .model-item-name {
  font-weight: 600;
}

.default-badge {
  font-size: 10px;
  font-weight: 600;
  color: #fff;
  background: var(--accent-color, #0066cc);
  padding: 1px 5px;
  border-radius: 3px;
  flex-shrink: 0;
  white-space: nowrap;
}

.set-default-btn {
  display: flex;
  align-items: center;
  justify-content: center;
  width: 22px;
  height: 22px;
  border: none;
  border-radius: 4px;
  background: transparent;
  color: var(--text-muted, #999);
  cursor: pointer;
  flex-shrink: 0;
  opacity: 0.4;
  transition: opacity 0.15s, color 0.15s, background 0.15s;
}

.model-item:hover .set-default-btn,
.thinking-item:hover .set-default-btn {
  opacity: 0.7;
}

.set-default-btn:hover {
  opacity: 1 !important;
  color: var(--accent-color, #0066cc);
  background: color-mix(in srgb, var(--accent-color, #0066cc) 12%, transparent);
}

.model-empty {
  padding: 24px 14px;
  text-align: center;
  color: var(--text-muted, #999);
  font-size: 13px;
}

.tab-empty-hint {
  padding: 32px 14px;
  text-align: center;
  color: var(--text-muted, #999);
  font-size: 13px;
}

.auto-approve-section {
  flex-shrink: 0;
}

.auto-approve-toggle {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  padding: 10px 14px;
}

.auto-approve-label {
  display: flex;
  flex-direction: column;
  gap: 2px;
  min-width: 0;
}

.auto-approve-title {
  font-size: 13px;
  font-weight: 600;
  color: var(--text-primary);
}

.auto-approve-desc {
  font-size: 11px;
  color: var(--text-muted, #999);
  line-height: 1.3;
}

.toggle-switch {
  position: relative;
  display: inline-block;
  width: 36px;
  height: 20px;
  flex-shrink: 0;
}

.toggle-switch input {
  opacity: 0;
  width: 0;
  height: 0;
}

.toggle-slider {
  position: absolute;
  cursor: pointer;
  top: 0;
  left: 0;
  right: 0;
  bottom: 0;
  background-color: var(--bg-tertiary, #ccc);
  transition: 0.2s;
  border-radius: 20px;
}

.toggle-slider::before {
  position: absolute;
  content: "";
  height: 16px;
  width: 16px;
  left: 2px;
  bottom: 2px;
  background-color: white;
  transition: 0.2s;
  border-radius: 50%;
}

.toggle-switch input:checked + .toggle-slider {
  background-color: var(--accent-color, #0066cc);
}

.toggle-switch input:checked + .toggle-slider::before {
  transform: translateX(16px);
}
</style>

<style>
/* Unscoped for PopupMenu content */
.popup-set-default {
  display: flex;
  align-items: center;
  gap: 6px;
  padding: 8px 14px;
  width: 100%;
  border: none;
  background: none;
  color: var(--text-primary);
  font-size: 13px;
  cursor: pointer;
  text-align: left;
  white-space: nowrap;
}

.popup-set-default:hover {
  background: var(--accent-color, #0066cc);
  color: #fff;
}
</style>
