<template>
  <BottomSheet :open="open" auto @close="handleClose">
    <template #header>
      <Cpu :size="16" class="bs-header-icon" />
      <span class="bs-header-title">{{ agentName }}</span>
    </template>

    <!-- Horizontally scrollable tab strip. Five tabs no longer fit a phone
         width, and a squeezed label is worse than a scrollable one. Touch pans
         natively (with momentum); a mouse drags (attachDragScroll, wired below)
         and the wheel scrolls sideways. The scrollbar is hidden — it would be
         taller than the tab row itself on mobile. -->
    <div
      ref="tabStripRef"
      class="session-setting-tabs"
      :class="{ 'is-overflowing': tabStripOverflow }"
      @wheel="onHorizontalWheel"
    >
      <button class="model-tab" :class="{ active: activeTab === 'model' }" @click="activeTab = 'model'">
        <Cpu :size="13" />{{ t('chat.modelSwitcher.title') }}
      </button>
      <button class="model-tab" :class="{ active: activeTab === 'thinking' }" @click="activeTab = 'thinking'">
        <Brain :size="13" />{{ t('chat.thinkingEffortSwitcher.title') }}
      </button>
      <button class="model-tab" :class="{ active: activeTab === 'mode' }" @click="activeTab = 'mode'">
        <ShieldAlert :size="13" />{{ t('chat.modeSwitcher.title') }}
      </button>
      <button class="model-tab" :class="{ active: activeTab === 'transport' }" @click="activeTab = 'transport'">
        <Cable :size="13" />{{ t('chat.transportSwitcher.title') }}
      </button>
      <!-- Context usage tab. Same body as the session-info popup (they share
           ContextUsageDetail), but reachable from the settings drawer so the
           usage numbers live next to the other per-session settings instead of
           only behind a tiny bar in the info row. The info-row popup stays: it
           is the one-tap path while reading. -->
      <button class="model-tab" :class="{ active: activeTab === 'usage' }" @click="activeTab = 'usage'">
        <Activity :size="13" />{{ t('chat.sessionInfo.contextUsage') }}
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
        <RefreshButton v-if="canRefresh" class="refresh-btn" :loading="refreshing" :disabled="refreshing" :title="t('chat.sessionSetting.refresh')" @click="handleRefresh" />
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
            :class="{ current: m.id === effectiveModelId, 'is-default': m.id === defaultModelId, 'nav-active': listNav.activeIndex.value === idx }"
            @click="selectModel(m)"
            @contextmenu.prevent="showDefaultMenu(m)"
            @touchstart.passive="onTouchStart(m, $event)"
            @touchend="onTouchEnd"
            @touchmove.passive="onTouchMove"
          >
            <span class="model-item-indicator" :class="{ active: m.id === effectiveModelId }"></span>
            <ModelRow :name="m.name" :model-id="m.id" :active="m.id === effectiveModelId" />
            <span v-if="m.id === defaultModelId" class="default-label">
              <span class="default-text">{{ t('chat.sessionSetting.defaultBadge') }}</span>
              <span class="default-star"><Star :size="12" fill="currentColor" /></span>
            </span>
            <button v-if="m.id !== defaultModelId" class="set-default-btn" @click.stop="setDefaultModel(m)" :title="t('chat.sessionSetting.setAsDefault')" :aria-label="t('chat.sessionSetting.setAsDefault')" :aria-pressed="false">
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
            :class="{ current: level.id === effectiveThinkingEffort, 'is-default': level.id === defaultThinkingEffort, 'nav-active': listNav.activeIndex.value === idx }"
            @click="selectThinkingEffort(level.id)"
            @contextmenu.prevent="showThinkingDefaultMenu(level.id)"
            @touchstart.passive="onTouchStartThinking(level.id, $event)"
            @touchend="onTouchEnd"
            @touchmove.passive="onTouchMove"
          >
            <span class="model-item-indicator" :class="{ active: level.id === effectiveThinkingEffort }"></span>
            <span class="model-item-name">{{ level.name }}</span>
            <span v-if="level.id === defaultThinkingEffort" class="default-label">
              <span class="default-text">{{ t('chat.sessionSetting.defaultBadge') }}</span>
              <span class="default-star"><Star :size="12" fill="currentColor" /></span>
            </span>
            <button v-if="level.id !== defaultThinkingEffort" class="set-default-btn" @click.stop="setDefaultThinkingEffort(level.id)" :title="t('chat.sessionSetting.setAsDefault')" :aria-label="t('chat.sessionSetting.setAsDefault')" :aria-pressed="false">
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
        <!-- Only show ACP option for agents that support ACP -->
        <div v-if="supportsACP(props.agentId || '')" class="model-item-wrapper">
          <button
            class="thinking-item"
            :class="{ current: isACP, 'is-default': defaultTransport === 'acp-stdio' }"
            @click="selectTransport('acp-stdio')"
          >
            <span class="model-item-indicator" :class="{ active: isACP }"></span>
            <span class="model-item-name">{{ t('chat.transportSwitcher.acp') }}</span>
            <span v-if="defaultTransport === 'acp-stdio'" class="default-label">
              <span class="default-text">{{ t('chat.sessionSetting.defaultBadge') }}</span>
              <span class="default-star"><Star :size="12" fill="currentColor" /></span>
            </span>
            <button v-if="defaultTransport !== 'acp-stdio'" class="set-default-btn" @click.stop="setDefaultTransport('acp-stdio')" :title="t('chat.sessionSetting.setAsDefault')" :aria-label="t('chat.sessionSetting.setAsDefault')" :aria-pressed="false">
              <Star :size="12" />
            </button>
          </button>
          <div class="model-divider"></div>
        </div>
        <!-- Only show CLI option for agents that support CLI -->
        <div v-if="supportsCLI(props.agentId || '')" class="model-item-wrapper">
          <button
            class="thinking-item"
            :class="{ current: !isACP, 'is-default': defaultTransport !== 'acp-stdio' }"
            @click="selectTransport('cli')"
          >
            <span class="model-item-indicator" :class="{ active: !isACP }"></span>
            <span class="model-item-name">{{ t('chat.transportSwitcher.cli') }}</span>
            <span v-if="defaultTransport !== 'acp-stdio'" class="default-label">
              <span class="default-text">{{ t('chat.sessionSetting.defaultBadge') }}</span>
              <span class="default-star"><Star :size="12" fill="currentColor" /></span>
            </span>
            <button v-if="defaultTransport === 'acp-stdio'" class="set-default-btn" @click.stop="setDefaultTransport('cli')" :title="t('chat.sessionSetting.setAsDefault')" :aria-label="t('chat.sessionSetting.setAsDefault')" :aria-pressed="false">
              <Star :size="12" />
            </button>
          </button>
        </div>
      </div>
    </div>

    <!-- Mode tab -->
    <div v-if="activeTab === 'mode'" class="model-tab-content">
      <div v-if="effectiveModes.length === 0 || !isACP" class="tab-empty-hint">
        {{ t('chat.sessionSetting.notAvailable') }}
      </div>
      <template v-else>
      <div class="model-list">
        <div
          v-for="(mode, idx) in effectiveModes"
          :key="mode.id"
          class="model-item-wrapper"
        >
          <button
            class="thinking-item"
            :class="{ current: mode.id === effectiveModeId, 'is-default': mode.id === defaultModeId, 'nav-active': listNav.activeIndex.value === idx }"
            @click="selectMode(mode)"
            @contextmenu.prevent="showModeDefaultMenu(mode)"
            @touchstart.passive="onTouchStartMode(mode, $event)"
            @touchend="onTouchEnd"
            @touchmove.passive="onTouchMove"
          >
            <span class="model-item-indicator" :class="{ active: mode.id === effectiveModeId }"></span>
            <span class="model-item-name">{{ mode.name || mode.id }}</span>
            <span v-if="mode.id === defaultModeId" class="default-label">
              <span class="default-text">{{ t('chat.sessionSetting.defaultBadge') }}</span>
              <span class="default-star"><Star :size="12" fill="currentColor" /></span>
            </span>
            <button v-if="mode.id !== defaultModeId" class="set-default-btn" @click.stop="setDefaultMode(mode)" :title="t('chat.sessionSetting.setAsDefault')" :aria-label="t('chat.sessionSetting.setAsDefault')" :aria-pressed="false">
              <Star :size="12" />
            </button>
          </button>
          <div v-if="idx < effectiveModes.length - 1" class="model-divider"></div>
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
            <input type="checkbox" :checked="effectiveAutoApprove" @change="applyAutoApprove($event.target.checked)" />
            <span class="toggle-slider"></span>
          </label>
        </div>
        <!-- Auto-cruise sits directly under auto-approve: they are the two
             "run unattended" switches, and cruise FORCES auto-approve on
             (one-directional), so the user sees the link where it happens.
             The session-info chip stays as the quick toggle. -->
        <div class="auto-approve-toggle auto-cruise-toggle">
          <div class="auto-approve-label">
            <span class="auto-approve-title">{{ t('chat.cruise.label') }}</span>
            <span class="auto-approve-desc">{{ t('chat.cruise.hint') }}</span>
          </div>
          <label class="toggle-switch">
            <input type="checkbox" :checked="effectiveCruise" @change="onCruiseToggle($event.target.checked)" />
            <span class="toggle-slider"></span>
          </label>
        </div>
      </div>
      </template>
    </div>

    <!-- Context usage tab. ContextUsageDetail deliberately carries NO outer
         padding of its own (its single-chat host, `.usage-popup`, supplies it),
         so the padding has to come from this wrapper — otherwise the rows sit
         flush against the sheet edges. -->
    <div v-if="activeTab === 'usage'" class="model-tab-content usage-tab-content">
      <p v-if="!hasUsage" class="tab-empty-hint">{{ t('chat.sessionSetting.noUsage') }}</p>
      <ContextUsageDetail v-else :usage="usageSnapshot" :show-compact="canCompactHere" :compacting="compacting" @compact="onCompact" />
      <p v-if="compactError" class="usage-error">{{ compactError }}</p>
    </div>

    <!-- Long-press PopupMenu for "Set as Default" (kept for backward compat) -->
    <PopupMenu v-model:show="showDefaultPopupMenu" :target-element="longPressTarget" :max-width="180" :max-height="100" :menu-items-count="1">
      <button class="popup-set-default" @click="setAsDefault">
        {{ t('chat.sessionSetting.setAsDefault') }}
      </button>
    </PopupMenu>
  </BottomSheet>
</template>

<script setup>
import { ref, computed, watch, onMounted, onBeforeUnmount, nextTick } from 'vue'
import { useI18n } from 'vue-i18n'
import { Star, Cpu, Brain, ShieldAlert, Cable, Activity } from 'lucide-vue-next'
import BottomSheet from '@/components/common/BottomSheet.vue'
import PopupMenu from '@/components/common/PopupMenu.vue'
import RefreshButton from '@/components/common/RefreshButton.vue'
import ModelRow from '@/components/common/ModelRow.vue'
import ContextUsageDetail from '@/components/chat/ContextUsageDetail.vue'
import { apiGet, apiPatch, apiPost } from '@/utils/api'
import { attachDragScroll, canDragScroll } from '@/utils/dragScroll'
import { onHorizontalWheel } from '@/utils/horizontalWheelScroll'
import { appLog } from '@/utils/appLog'
import { compactGroupMember } from '@/composables/useGroupChat'
import { useAgents, restoreOriginalModels, resetModelViewForTransport, applyRefreshedModelList, populateACPStateFromCache, invalidateACPStateCache } from '@/composables/useAgents'
import { useListNav } from '@/composables/useListNav'
import { useListKeys } from '@/composables/useListKeys'
import { useSessionIdentity, clearModeState, clearCommandState, clearThinkingEffortState } from '@/composables/useSessionIdentity'
import { formatModelIdForDisplay } from '@/utils/modelDisplay'
import { patchAgentPref } from '@/composables/useSettingsConfig'
import { useToast } from '@/composables/useToast'

const props = defineProps({
  open: Boolean,
  agentId: String,
  initialTab: { type: String, default: 'model' },
  /**
   * Session whose settings this drawer edits. Empty means "the current
   * session" — the shared session-identity refs, which is how single chat has
   * always worked.
   *
   * Non-empty is the group-member path: clicking a member in the roster opens
   * this drawer on THAT member's row. A member row is a real session (it owns
   * the agent binding and its own context window) but it is NOT the current
   * session, so every value must come from this prop instead of the singleton
   * refs — otherwise the drawer would show the group's host state while
   * claiming to describe the member.
   */
  targetSessionId: { type: String, default: '' },
})

const emit = defineEmits(['close', 'switch-model', 'switch-thinking-effort', 'switch-mode', 'switch-transport', 'changed'])

const { t } = useI18n()
const toast = useToast()
const { getAgentModels, getAgentThinkingEffortLevels, getAgent, updateAgentField, canRefreshModels, supportsACP, supportsCLI, getAgentTransport } = useAgents()
const identity = useSessionIdentity()
const { currentModelId, currentThinkingEffort, currentModeId, currentTransport, availableThinkingEfforts, availableModes, autoApprove, toggleAutoApprove, cruise, toggleCruise } = identity

// ── Target-session state ──
// When targetSessionId is set (group member), the drawer describes THAT session
// and the singleton refs above describe the CURRENT one. Every read below goes
// through these indirections so a single component serves both cases.
const isTargeted = computed(() => !!props.targetSessionId)
const target = ref(null) // GET /api/ai/session/settings payload
const targetUsage = ref(null)
const compacting = ref(false)
const compactError = ref('')

const effectiveModelId = computed(() => isTargeted.value ? (target.value?.modelId || '') : currentModelId.value)
const effectiveModeId = computed(() => isTargeted.value ? (target.value?.modeState?.currentModeId || '') : currentModeId.value)
const effectiveThinkingEffort = computed(() => isTargeted.value ? (target.value?.thinkingEffortState?.currentId || '') : currentThinkingEffort.value)
const effectiveTransport = computed(() => isTargeted.value ? (target.value?.transport || '') : currentTransport.value)
const effectiveAutoApprove = computed(() => isTargeted.value ? target.value?.autoApprove === true : autoApprove.value)
const effectiveCruise = computed(() => isTargeted.value ? target.value?.cruise === true : cruise.value)
const effectiveModes = computed(() => isTargeted.value ? (target.value?.modeState?.availableModes || []) : availableModes.value)
const effectiveEfforts = computed(() => isTargeted.value ? (target.value?.thinkingEffortState?.availableLevels || []) : availableThinkingEfforts.value)

/** Load the targeted session's state. No-op for the single-chat path. */
async function loadTarget() {
  if (!props.targetSessionId) {
    target.value = null
    targetUsage.value = null
    return
  }
  try {
    const data = await apiGet(`/api/ai/session/settings?session_id=${encodeURIComponent(props.targetSessionId)}`)
    target.value = data
    targetUsage.value = data?.usageState || null
  } catch (err) {
    target.value = null
    targetUsage.value = null
    appLog.w('SessionDrawer', 'load target session settings failed', err)
  }
}

watch(() => [props.open, props.targetSessionId], ([open]) => {
  if (open) loadTarget()
}, { immediate: true })

/** Usage snapshot for the usage tab (shared shape with the info-row popup). */
const usageSnapshot = computed(() => {
  if (isTargeted.value) return targetUsage.value
  return {
    used: identity.contextUsed.value,
    size: identity.contextSize.value,
    inputTokens: identity.contextInputTokens.value,
    outputTokens: identity.contextOutputTokens.value,
    totalTokens: identity.contextTotalTokens.value,
    cachedReadTokens: identity.contextCachedReadTokens.value,
    cachedWriteTokens: identity.contextCachedWriteTokens.value,
    thoughtTokens: identity.contextThoughtTokens.value,
    cost: identity.contextCost.value,
    currency: identity.contextCurrency.value,
    cacheCreationTokens: identity.contextCacheCreationTokens.value,
    cacheHitTokens: identity.contextCacheHitTokens.value,
    cacheMissTokens: identity.contextCacheMissTokens.value,
    credit: identity.contextCredit.value,
    usageByCategory: identity.contextUsageByCategory.value,
  }
})
const hasUsage = computed(() => (usageSnapshot.value?.size ?? 0) > 0)
// Compaction is offered for a group member (it is a group-only action) and for
// the current single chat. A member that never reported a window has nothing to
// compact, and a left member cannot run a turn.
const canCompactHere = computed(() => hasUsage.value && (!isTargeted.value || !target.value?.isGroupMember ? !isTargeted.value : true))

async function onCompact() {
  if (!isTargeted.value || compacting.value) return
  compacting.value = true
  compactError.value = ''
  try {
    const usage = await compactGroupMember(target.value?.sessionId || props.targetSessionId, props.targetSessionId)
    if (usage) targetUsage.value = usage
    emit('changed')
  } catch (err) {
    compactError.value = err instanceof Error ? err.message : String(err)
  } finally {
    compacting.value = false
  }
}

/** Auto-cruise toggle. Targeted sessions write through their own endpoint. */
async function onCruiseToggle(enabled) {
  if (!isTargeted.value) {
    toggleCruise(enabled)
    return
  }
  try {
    await apiPatch(`/api/ai/session/settings`, {
      sessionId: props.targetSessionId,
      cruise: enabled,
    })
    // Cruise forces auto-approve on server-side (one-directional); mirror it
    // locally so the switch next to it does not contradict the state we just
    // read back.
    if (target.value) {
      target.value = { ...target.value, cruise: enabled, autoApprove: enabled ? true : target.value.autoApprove }
    }
    emit('changed')
  } catch (err) {
    appLog.w('SessionDrawer', 'toggle cruise failed', err)
    toast.show(t('settings.saveFailed'), { icon: 'warning', type: 'error', duration: 3000 })
  }
}

const activeTab = ref('model')
const searchQuery = ref('')
const refreshing = ref(false)
const showDefaultPopupMenu = ref(false)
const longPressTarget = ref(null)
const pendingDefaultModel = ref(null)
const pendingDefaultThinking = ref(null)
const pendingDefaultMode = ref(null)

// Long-press state
let longPressTimer = null
const longPressTriggered = ref(false)

// Initialize activeTab from initialTab prop
activeTab.value = props.initialTab || 'model'

// Computed data
const models = computed(() => getAgentModels(props.agentId || ''))
// Thinking levels: ACP mode shows ACP-reported levels only; CLI mode shows static levels
const thinkingLevels = computed(() => {
  if (isACP.value) {
    const acpLevels = effectiveEfforts.value
    if (acpLevels.length > 0) return acpLevels
    return []
  }
  // CLI mode: agent BackendSpec static levels
  return getAgentThinkingEffortLevels(props.agentId || '').map(id => ({ id, name: id }))
})
const canRefresh = computed(() => canRefreshModels(props.agentId || ''))

const isACP = computed(() => {
  // Prefer session-level transport, fallback to agent config. The targeted
  // transport comes from the member row, which is authoritative for that row.
  if (effectiveTransport.value) return effectiveTransport.value === 'acp-stdio'
  return getAgentTransport(props.agentId || '') === 'acp-stdio'
})

const agentName = computed(() => {
  const agent = getAgent(props.agentId || '')
  return agent ? agent.name : ''
})

const defaultModelId = computed(() => {
  const agent = getAgent(props.agentId || '')
  return agent?.preferredModel || ''
})

const defaultThinkingEffort = computed(() => {
  const agent = getAgent(props.agentId || '')
  return agent?.preferredThinkingEffort || ''
})

const defaultModeId = computed(() => {
  const agent = getAgent(props.agentId || '')
  return agent?.preferredMode || ''
})

const defaultTransport = computed(() => {
  return getAgentTransport(props.agentId || '')
})

const filteredModels = computed(() => {
  const q = searchQuery.value.toLowerCase().trim()
  if (!q) return models.value
  // Match the formatted id too: the list shows the readable form, so a query
  // typed against what the user sees must hit (the raw id may not contain it).
  return models.value.filter(m =>
    m.name.toLowerCase().includes(q) ||
    m.id.toLowerCase().includes(q) ||
    formatModelIdForDisplay(m.id).toLowerCase().includes(q),
  )
})

// ── Keyboard ↑/↓ + Enter navigation over the active tab's list ──
const listNav = useListNav({
  getCount: () => {
    if (activeTab.value === 'model') return filteredModels.value.length
    if (activeTab.value === 'thinking') return thinkingLevels.value.length
    if (activeTab.value === 'mode') return effectiveModes.value.length
    return 0
  },
  onConfirm: (idx) => {
    if (activeTab.value === 'model') selectModel(filteredModels.value[idx])
    else if (activeTab.value === 'thinking') selectThinkingEffort(thinkingLevels.value[idx].id)
    else if (activeTab.value === 'mode') selectMode(effectiveModes.value[idx])
  },
  onActiveChange: scrollActiveIntoView,
})

// Document-level keys so navigation works regardless of where focus is inside the drawer
useListKeys({ isOpen: () => props.open, nav: listNav })

function scrollActiveIntoView(index) {
  const items = document.querySelectorAll('.model-list .model-item, .model-list .thinking-item')
  const el = items[index]
  if (el && typeof el.scrollIntoView === 'function') {
    el.scrollIntoView({ behavior: 'auto', block: 'nearest' })
  }
}

// Reset highlight when switching tabs or the model search changes
// (thinkingLevels/availableModes are computed lazily, so we avoid watching them
// directly to prevent eager evaluation in mocked/test contexts.)
watch([activeTab, searchQuery], () => listNav.reset())

// Reset search when tab changes or drawer reopens
watch(() => props.open, (val) => {
  if (val) {
    searchQuery.value = ''
    activeTab.value = props.initialTab || 'model'
  }
})

// ── Write routing ──
// A targeted session (group member) writes through its own endpoint; the
// current session keeps using the shared identity actions, which own the
// optimistic chips in the info bar. Both paths land on the same server code.
async function persistTargeted(payload) {
  await apiPatch('/api/ai/session/settings', { sessionId: props.targetSessionId, ...payload })
  emit('changed')
  await loadTarget()
}

function applyAutoApprove(checked) {
  if (isTargeted.value) {
    // toggleAutoApprove() writes the CURRENT session, which would silently
    // change the group's setting while the drawer claims to edit the member.
    persistTargeted({ autoApprove: checked }).catch((err) => {
      appLog.w('SessionDrawer', 'toggle auto-approve failed', err)
      toast.show(t('settings.saveFailed'), { icon: 'warning', type: 'error', duration: 3000 })
    })
    if (target.value) target.value = { ...target.value, autoApprove: checked }
    return
  }
  toggleAutoApprove(checked)
}

// ── Tab strip scrolling ──
// Five tabs overflow a phone width. Touch already pans natively; this adds the
// two paths a finger cannot use: mouse drag (the scrollbar is hidden) and the
// wheel (which would otherwise scroll the drawer body vertically).
const tabStripRef = ref(null)
const tabStripOverflow = ref(false)
let stopTabDragScroll = null
let tabResizeObserver = null

/** Attach/detach drag-to-scroll to match whether the strip actually overflows. */
function syncTabDragScroll() {
  const el = tabStripRef.value
  if (!el) return
  const shouldAttach = canDragScroll('mouse', 0, el.scrollWidth, el.clientWidth)
  tabStripOverflow.value = shouldAttach
  if (shouldAttach && !stopTabDragScroll) {
    stopTabDragScroll = attachDragScroll(el)
  } else if (!shouldAttach && stopTabDragScroll) {
    stopTabDragScroll()
    stopTabDragScroll = null
  }
}

/** Keep the ACTIVE tab visible: a tab reached by keyboard nav or by the
 *  `initialTab` prop can sit off-screen at the far end of the strip. */
function scrollActiveTabIntoView() {
  const el = tabStripRef.value
  if (!el) return
  const active = el.querySelector('.model-tab.active')
  if (!active || typeof active.scrollIntoView !== 'function') return
  active.scrollIntoView({ behavior: 'auto', block: 'nearest', inline: 'nearest' })
}

onMounted(() => {
  nextTick(() => {
    syncTabDragScroll()
    scrollActiveTabIntoView()
  })
  // The strip's overflow depends on its rendered width (drawer resize, phone
  // rotation, a locale with longer labels). Re-measure instead of assuming the
  // mount-time result holds.
  if (typeof ResizeObserver !== 'undefined') {
    tabResizeObserver = new ResizeObserver(() => syncTabDragScroll())
    if (tabStripRef.value) tabResizeObserver.observe(tabStripRef.value)
  }
})

onBeforeUnmount(() => {
  stopTabDragScroll?.()
  stopTabDragScroll = null
  tabResizeObserver?.disconnect()
  tabResizeObserver = null
})

// The drawer is a keep-alive-ish sheet: it mounts once and toggles `open`, so
// the strip can go from hidden (zero width) to laid out on every open. Re-measure
// when it opens, and bring the active tab back into view.
watch(() => props.open, (open) => {
  if (!open) return
  nextTick(() => {
    syncTabDragScroll()
    scrollActiveTabIntoView()
  })
})

watch(activeTab, () => nextTick(scrollActiveTabIntoView))

// --- Model selection ---

function selectModel(model) {
  if (longPressTriggered.value) {
    longPressTriggered.value = false
    return
  }
  if (isTargeted.value) {
    persistTargeted({ modelId: model.id }).catch((err) => {
      appLog.w('SessionDrawer', 'switch model failed', err)
      toast.show(err?.message || t('chat.modelSwitcher.switchFailed'), { icon: 'warning', type: 'error' })
    })
    emit('close')
    return
  }
  emit('switch-model', model)
  emit('close')
}

// --- Thinking effort selection ---

function selectThinkingEffort(level) {
  if (longPressTriggered.value) {
    longPressTriggered.value = false
    return
  }
  if (isTargeted.value) {
    persistTargeted({ thinkingEffort: level }).catch((err) => {
      appLog.w('SessionDrawer', 'switch thinking effort failed', err)
      toast.show(t('settings.saveFailed'), { icon: 'warning', type: 'error', duration: 3000 })
    })
    emit('close')
    return
  }
  emit('switch-thinking-effort', level)
  emit('close')
}

// --- Mode selection ---

function selectMode(mode) {
  if (longPressTriggered.value) {
    longPressTriggered.value = false
    return
  }
  if (isTargeted.value) {
    persistTargeted({ modeId: mode.id }).catch((err) => {
      appLog.w('SessionDrawer', 'switch mode failed', err)
      toast.show(t('settings.saveFailed'), { icon: 'warning', type: 'error', duration: 3000 })
    })
    emit('close')
    return
  }
  emit('switch-mode', mode)
  emit('close')
}

// --- Transport selection ---

async function selectTransport(transport) {
  if (transport === 'acp-stdio' && isACP.value) return
  if (transport === 'cli' && !isACP.value) return

  if (isTargeted.value) {
    persistTargeted({ transport }).catch((err) => {
      appLog.w('SessionDrawer', 'switch transport failed', err)
      toast.show(t('settings.saveFailed'), { icon: 'warning', type: 'error', duration: 3000 })
    })
    emit('close')
    return
  }

  await applyTransportSwitch(transport)

  emit('switch-transport', transport)
  emit('close')
}

// Apply the session-side effects of switching transport (shared by selection
// and set-as-default, which also selects the new transport as current).
async function applyTransportSwitch(transport) {
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

  // When switching to ACP, re-populate ACP state from cache (force-refresh).
  // resetModelViewForTransport runs AFTER the populate so it can clear the list
  // when the cache holds no ACP models — otherwise the CLI baseline left by the
  // branch above would survive the switch back and re-enter the ACP view.
  if (transport === 'acp-stdio') {
    invalidateACPStateCache(props.agentId || '')
    await populateACPStateFromCache(props.agentId || '')
    resetModelViewForTransport(props.agentId || '')
  }
}

// --- Refresh ---

async function handleRefresh() {
  if (refreshing.value) return
  refreshing.value = true
  try {
    const data = await apiPost(`/api/agents/${props.agentId}/refresh-models`, {})
    if (data?.models) {
      // The backend answers with the resolved list plus the pure CLI list (same
      // shape as GET /api/agents), so the ACP view is not downgraded to the raw
      // CLI list and the CLI baseline is rebased for a later transport switch.
      applyRefreshedModelList(props.agentId, data.models, data.cliModels)
      toast.show(t('chat.sessionSetting.refreshSuccess'), { icon: 'success', type: 'success', duration: 2000 })
    }
  } catch (err) {
    const msgKey = err?.msgKey
    if (msgKey === 'CLINotFound') {
      toast.show(t('chat.sessionSetting.cliNotFound'), { icon: 'warning', type: 'error', duration: 4000 })
    } else if (msgKey === 'ModelDiscoveryNotSupported') {
      toast.show(t('chat.sessionSetting.discoveryNotSupported'), { icon: 'warning', type: 'error', duration: 4000 })
    } else if (msgKey === 'ModelDiscoveryFailed') {
      // Show the backend's reason (e.g. which paths were probed) when available,
      // so users can tell an unsupported install layout from a transient failure.
      const detail = err?.detail?.detail
      toast.show(
        detail
          ? t('chat.sessionSetting.discoveryFailedWithDetail', { detail })
          : t('chat.sessionSetting.discoveryFailed'),
        { icon: 'warning', type: 'error', duration: 6000 },
      )
    } else {
      toast.show(t('chat.sessionSetting.refreshFailed'), { icon: 'warning', type: 'error', duration: 3000 })
    }
  } finally {
    refreshing.value = false
  }
}

// --- Set default model/thinking directly via set-as-default button ---
// Setting an item as default also selects it as the current session choice.

async function setDefaultModel(model) {
  try {
    await patchAgentPref(props.agentId, 'preferred_model', model.id)
    updateAgentField(props.agentId, 'preferredModel', model.id)
    emit('switch-model', model)
    toast.show(t('chat.sessionSetting.setDefaultSuccess', { name: model.name || model.id }), { icon: 'star', type: 'success', duration: 2000 })
  } catch {
    toast.show(t('settings.saveFailed'), { icon: 'warning', type: 'error', duration: 3000 })
  }
}

async function setDefaultThinkingEffort(level) {
  try {
    await patchAgentPref(props.agentId, 'preferred_thinking_effort', level)
    updateAgentField(props.agentId, 'preferredThinkingEffort', level)
    emit('switch-thinking-effort', level)
    const levelName = thinkingLevels.value.find(l => l.id === level)?.name || level
    toast.show(t('chat.sessionSetting.setDefaultSuccess', { name: levelName }), { icon: 'star', type: 'success', duration: 2000 })
  } catch {
    toast.show(t('settings.saveFailed'), { icon: 'warning', type: 'error', duration: 3000 })
  }
}

async function setDefaultTransport(transport) {
  try {
    await patchAgentPref(props.agentId, 'transport', transport)
    updateAgentField(props.agentId, 'transport', transport)
    await applyTransportSwitch(transport)
    emit('switch-transport', transport)
    const transportName = transport === 'acp-stdio' ? t('chat.transportSwitcher.acp') : t('chat.transportSwitcher.cli')
    toast.show(t('chat.sessionSetting.setDefaultSuccess', { name: transportName }), { icon: 'star', type: 'success', duration: 2000 })
    // Clear preferred thinking effort if it's not valid for the new transport
    const currentPref = getAgent(props.agentId || '')?.preferredThinkingEffort || ''
    if (currentPref) {
      const validLevels = transport === 'acp-stdio'
        ? availableThinkingEfforts.value.map(l => l.id)
        : getAgentThinkingEffortLevels(props.agentId || '')
      if (!validLevels.includes(currentPref)) {
        await patchAgentPref(props.agentId, 'preferred_thinking_effort', '')
        updateAgentField(props.agentId, 'preferredThinkingEffort', '')
      }
    }
  } catch {
    toast.show(t('settings.saveFailed'), { icon: 'warning', type: 'error', duration: 3000 })
  }
}

async function setDefaultMode(mode) {
  try {
    await patchAgentPref(props.agentId, 'preferred_mode', mode.id)
    updateAgentField(props.agentId, 'preferredMode', mode.id)
    emit('switch-mode', mode)
    toast.show(t('chat.sessionSetting.setDefaultSuccess', { name: mode.name || mode.id }), { icon: 'star', type: 'success', duration: 2000 })
  } catch {
    toast.show(t('settings.saveFailed'), { icon: 'warning', type: 'error', duration: 3000 })
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
    pendingDefaultMode.value = null
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
    pendingDefaultMode.value = null
    longPressTarget.value = el
    showDefaultPopupMenu.value = true
  }, 500)
}

function onTouchStartMode(mode, event) {
  longPressTriggered.value = false
  longPressTimer = setTimeout(() => {
    const el = event.target.closest('.model-item, .thinking-item')
    longPressTriggered.value = true
    pendingDefaultMode.value = mode.id
    pendingDefaultModel.value = null
    pendingDefaultThinking.value = null
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
  pendingDefaultMode.value = null
  // For contextmenu, use the event target as anchor
  longPressTarget.value = null
  showDefaultPopupMenu.value = true
}

function showThinkingDefaultMenu(level) {
  pendingDefaultThinking.value = level
  pendingDefaultModel.value = null
  pendingDefaultMode.value = null
  longPressTarget.value = null
  showDefaultPopupMenu.value = true
}

function showModeDefaultMenu(mode) {
  pendingDefaultMode.value = mode.id
  pendingDefaultModel.value = null
  pendingDefaultThinking.value = null
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
      const model = models.value.find(m => m.id === pendingDefaultModel.value)
      if (model) emit('switch-model', model)
      toast.show(t('chat.sessionSetting.setDefaultSuccess', { name: model?.name || model?.id || pendingDefaultModel.value }), { icon: 'star', type: 'success', duration: 2000 })
    } else if (pendingDefaultThinking.value !== null) {
      await patchAgentPref(props.agentId, 'preferred_thinking_effort', pendingDefaultThinking.value)
      updateAgentField(props.agentId, 'preferredThinkingEffort', pendingDefaultThinking.value)
      emit('switch-thinking-effort', pendingDefaultThinking.value)
      const levelName = thinkingLevels.value.find(l => l.id === pendingDefaultThinking.value)?.name || pendingDefaultThinking.value
      toast.show(t('chat.sessionSetting.setDefaultSuccess', { name: levelName }), { icon: 'star', type: 'success', duration: 2000 })
    } else if (pendingDefaultMode.value !== null) {
      await patchAgentPref(props.agentId, 'preferred_mode', pendingDefaultMode.value)
      updateAgentField(props.agentId, 'preferredMode', pendingDefaultMode.value)
      const mode = effectiveModes.value.find(m => m.id === pendingDefaultMode.value)
      if (mode) emit('switch-mode', mode)
      toast.show(t('chat.sessionSetting.setDefaultSuccess', { name: mode?.name || mode?.id || pendingDefaultMode.value }), { icon: 'star', type: 'success', duration: 2000 })
    }
  } catch {
    toast.show(t('settings.saveFailed'), { icon: 'warning', type: 'error', duration: 3000 })
  }
}

function handleClose() {
  emit('close')
}

defineExpose({
  activeTab,
  searchQuery,
  showDefaultPopupMenu,
  refreshing,
  selectTransport,
  setDefaultTransport,
  setDefaultModel,
  setDefaultThinkingEffort,
  selectMode,
  selectThinkingEffort,
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
  /* Five tabs overflow a phone width. Scroll sideways instead of squeezing the
     labels: `overflow-x: auto` + `nowrap` keeps each tab at its natural width,
     touch pans natively with momentum, and the mouse gets drag/wheel below. */
  overflow-x: auto;
  overflow-y: hidden;
  overscroll-behavior-x: contain;
  -webkit-overflow-scrolling: touch;
  /* Hidden scrollbar: it is taller than the tab row on mobile, and a visible
     bar under the labels reads as a second border. */
  scrollbar-width: none;
}

.session-setting-tabs::-webkit-scrollbar {
  display: none;
}

/* Drag-to-scroll affordance, only while the strip actually overflows (the class
   is set from a real overflow measurement, so a strip that fits stays inert). */
.session-setting-tabs.is-overflowing {
  cursor: grab;
}

.session-setting-tabs.is-overflowing.is-dragging {
  cursor: grabbing;
  user-select: none;
  -webkit-user-select: none;
}

.model-tab {
  /* Natural width, not `flex: 1`: equal division is what forced the labels to
     squeeze/ellipsis once there were five. `flex-shrink: 0` keeps a tab from
     being compressed when the strip is narrower than its content. */
  flex: 0 0 auto;
  flex-shrink: 0;
  white-space: nowrap;
  display: flex;
  align-items: center;
  justify-content: center;
  gap: var(--space-2);
  padding: var(--space-5) var(--space-6);
  border: none;
  background: none;
  color: var(--text-muted, #999);
  font-size: var(--font-size-md);
  font-weight: var(--font-weight-medium);
  cursor: pointer;
  border-bottom: 2px solid transparent;
  transition: color var(--duration-base), border-color var(--duration-base);
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
  gap: var(--space-3);
  padding: var(--space-4) var(--space-5);
  border-bottom: 1px solid var(--border-color, #e5e5e5);
  flex-shrink: 0;
}

.model-search-input {
  flex: 1;
  padding: var(--space-3) var(--space-5);
  border: 1px solid var(--border-color, #e5e5e5);
  border-radius: var(--radius-sm);
  background: var(--bg-primary, #fff);
  color: var(--text-primary);
  font-size: var(--font-size-md);
  outline: none;
  transition: border-color var(--duration-base);
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
  border-radius: var(--radius-sm);
  background: var(--bg-tertiary, #f0f0f0);
  color: var(--text-muted, #999);
  cursor: pointer;
  flex-shrink: 0;
  transition: background var(--duration-base), color var(--duration-base);
}

@media (hover: hover) {
  .refresh-btn:hover:not(:disabled) {
    background: var(--accent-color, #0066cc);
    color: #fff;
  }
}

.refresh-btn:disabled {
  opacity: var(--opacity-muted);
  cursor: not-allowed;
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
  gap: var(--space-4);
  width: 100%;
  padding: var(--space-5) 14px;
  border: none;
  background: none;
  color: var(--text-primary);
  font-size: var(--font-size-md);
  cursor: pointer;
  text-align: left;
  transition: background var(--duration-base);
  -webkit-tap-highlight-color: transparent;
}

@media (hover: hover) {
  .model-item:hover,
  .thinking-item:hover {
    background: var(--bg-tertiary, #f0f0f0);
  }
}

.model-item.nav-active,
.thinking-item.nav-active {
  background: var(--bg-tertiary, #f0f0f0);
  border-radius: 0;
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
  transition: background var(--duration-base);
}

.model-item-indicator.active {
  background: var(--accent-color, #0066cc);
}

.model-item-name {
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.model-item.current .model-item-name,
.thinking-item.current .model-item-name {
  font-weight: var(--font-weight-semibold);
}

.default-label {
  display: inline-flex;
  align-items: center;
  gap: var(--space-2);
  flex-shrink: 0;
  /* Pin the badge to the row's right edge. The model tab already ends up there
     because its ModelRow (flex: 1) absorbs the free space; the thinking / mode /
     transport tabs render a bare .model-item-name with no growth, so without
     this the badge trails the text instead. */
  margin-left: auto;
  color: var(--accent-color, #0066cc);
}

.default-text {
  font-size: var(--font-size-2xs);
  font-weight: var(--font-weight-semibold);
  white-space: nowrap;
}

/* Fixed 22×22 slot so the default star stays aligned with the
   set-default star on non-default rows. */
.default-star {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 22px;
  height: 22px;
  flex-shrink: 0;
}

.set-default-btn {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 22px;
  height: 22px;
  border: none;
  border-radius: var(--radius-xs);
  background: transparent;
  color: var(--text-muted, #999);
  cursor: pointer;
  flex-shrink: 0;
  /* Same right-edge pin as .default-label, so a row keeps its trailing slot in
     the same place whether or not it is the default. */
  margin-left: auto;
  opacity: var(--opacity-soft);
  transition: opacity var(--duration-base), color var(--duration-base), background var(--duration-base);
}

@media (hover: hover) {
  .model-item:hover .set-default-btn,
  .thinking-item:hover .set-default-btn {
    opacity: var(--opacity-hover);
  }

  .set-default-btn:hover {
    opacity: 1 !important;
    color: var(--accent-color, #0066cc);
    background: color-mix(in srgb, var(--accent-color, #0066cc) 12%, transparent);
  }
}

.model-empty {
  padding: 24px 14px;
  text-align: center;
  color: var(--text-muted, #999);
  font-size: var(--font-size-md);
}

.tab-empty-hint {
  padding: 32px 14px;
  text-align: center;
  color: var(--text-muted, #999);
  font-size: var(--font-size-md);
}

.auto-approve-section {
  flex-shrink: 0;
}

.auto-approve-toggle {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: var(--space-6);
  padding: var(--space-5) 14px;
}

/* Auto-cruise sits under auto-approve in the same block; a hairline keeps the
   two "run unattended" switches readable as a pair without a second divider. */
.auto-cruise-toggle {
  border-top: 1px solid color-mix(in srgb, var(--border-color) 60%, transparent);
}

/* The usage panel body has no padding of its own (see the template comment).
   Match the other tabs' horizontal rhythm — `--space-5` is what the model
   search row and the setting rows use, so the numbers line up with them. */
.usage-tab-content {
  padding: var(--space-4) var(--space-5) var(--space-6);
  overflow-y: auto;
}

.usage-error {
  margin: var(--space-4) 0 0;
  font-size: var(--font-size-sm);
  color: var(--color-red, #dc2626);
  line-height: 1.4;
}

.auto-approve-label {
  display: flex;
  flex-direction: column;
  gap: var(--space-1);
  min-width: 0;
}

.auto-approve-title {
  font-size: var(--font-size-md);
  font-weight: var(--font-weight-semibold);
  color: var(--text-primary);
}

.auto-approve-desc {
  font-size: var(--font-size-xs);
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
  transition: var(--duration-slow);
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
  transition: var(--duration-slow);
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
  gap: var(--space-3);
  padding: var(--space-4) 14px;
  width: 100%;
  border: none;
  background: none;
  color: var(--text-primary);
  font-size: var(--font-size-md);
  cursor: pointer;
  text-align: left;
  white-space: nowrap;
}

@media (hover: hover) {
  .popup-set-default:hover {
    background: var(--accent-color, #0066cc);
    color: #fff;
  }
}
</style>
