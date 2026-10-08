<template>
  <BottomSheet :open="open" auto @close="handleClose">
    <template #header>
      <Bot :size="16" class="bs-header-icon" />
      <span class="bs-header-title">{{ title }}</span>
      <!-- Group mode: the chosen host decides the group's mode. The subtitle
           sits to the right of the title and flips between host / free as the
           user sets or clears the host. -->
      <span v-if="groupMode" class="agent-header-mode">{{ hostId ? t('group.hostMode') : t('group.freeMode') }}</span>
    </template>
    <div class="agent-list">
      <LoadingIndicator v-if="agentsLoading" size="md" />
      <div v-else-if="agents.length === 0" class="agent-list-empty">{{ t('chat.messageList.noAgentsTitle') }}</div>
      <div
        v-for="(agent, idx) in agents"
        :key="agent.id"
        class="agent-option"
        :class="{ selected: isSelected(agent.id), 'agent-option-active': listNav.activeIndex.value === idx, 'agent-option-disabled': isExcluded(agent.id) }"
        role="button"
        tabindex="0"
        @click="handleSelect(agent.id)"
        @keydown.enter="handleSelect(agent.id)"
        @keydown.space.prevent="handleSelect(agent.id)"
      >
        <span v-if="multiple" class="agent-option-check" :class="{ checked: isSelected(agent.id) }">
          <Check v-if="isSelected(agent.id)" :size="14" />
        </span>
        <span class="agent-option-icon"><AgentIcon :backend="agent.backend" :name="agent.name" :avatar="agent.avatar" size="md" /></span>
        <div class="agent-option-detail">
          <span class="agent-option-name">{{ agent.name }}</span>
          <span class="agent-option-specialty">{{ agent.specialty }}</span>
          <div class="agent-option-tags">
            <span class="agent-tag backend-tag">{{ agent.backend }}</span>
            <span v-if="defaultModelName(agent.id)" class="agent-tag model-tag">{{ defaultModelName(agent.id) }}</span>
          </div>
        </div>
        <!-- Group mode: the host control sits where the per-row default star
             normally is, but is a labelled button so it reads as an action.
             Only selected rows can be host (the host must be a member). -->
        <button
          v-if="groupMode && isSelected(agent.id)"
          class="agent-host-btn"
          :class="{ active: agent.id === hostId }"
          type="button"
          :aria-pressed="agent.id === hostId"
          :title="agent.id === hostId ? hostActiveLabel : hostLabel"
          @click.stop="handleSetHost(agent.id)"
        >
          <Crown :size="12" />
          <span class="agent-host-btn-label">{{ hostBadge }}</span>
        </button>
        <span v-if="isExcluded(agent.id)" class="agent-tag agent-added-tag">{{ addedLabel }}</span>
        <span v-if="showAgentActions && isDefaultAgent(agent.id)" class="agent-default-badge-pill">{{ defaultBadge }}</span>
        <button v-else-if="showAgentActions" class="agent-set-default-btn" @click.stop="handleSetDefaultAgent(agent.id)" :title="setDefaultTitle">
          <Star :size="14" />
        </button>
        <button v-if="showAgentActions" class="agent-config-btn" @click.stop="handleOpenAgentConfig(agent.id)" :title="configTitle">
          <Settings :size="14" />
        </button>
      </div>
    </div>
    <template v-if="multiple" #footer>
      <button class="agent-multi-confirm" :disabled="confirmDisabled" @click="handleConfirmMulti">{{ confirmLabel }}</button>
    </template>
  </BottomSheet>
</template>

<script setup lang="ts">
import { ref, watch, inject, computed } from 'vue'
import { Bot, Star, Settings, Check, Crown } from 'lucide-vue-next'
import { useI18n } from 'vue-i18n'
import BottomSheet from '@/components/common/BottomSheet.vue'
import AgentIcon from '@/components/common/AgentIcon.vue'
import LoadingIndicator from '@/components/common/LoadingIndicator.vue'
import { useListNav } from '@/composables/useListNav'
import { useListKeys } from '@/composables/useListKeys'
import { useAgents } from '@/composables/useAgents'
import { setPendingSettingsCategory } from '@/composables/useSettingsNavigation'

const { t } = useI18n()

const props = withDefaults(defineProps<{
  open: boolean
  modelValue?: string | string[]
  multiple?: boolean
  title?: string
  defaultBadge?: string
  setDefaultTitle?: string
  configTitle?: string
  confirmLabel?: string
  /** Show the per-row default badge / set-default star and the settings button.
   *  False for the group "add members" picker, where those actions are noise. */
  showAgentActions?: boolean
  /** Agent ids that cannot be picked (already members). Rendered dimmed with an
   *  "added" tag; clicking them is a no-op. */
  excludedAgentIds?: string[]
  /** Label shown on excluded rows. */
  addedLabel?: string
  /** Group-creation mode: each SELECTED row grows a "host" radio dot so the
   *  user picks the host inline, in the same list as the members. */
  groupMode?: boolean
  /** The currently chosen host agent id (groupMode only). */
  hostId?: string
  /** Tooltip/label for the host button when the row is NOT the host. */
  hostLabel?: string
  /** Tooltip/label for the host button when the row IS the host (clicking
   *  again clears the host). */
  hostActiveLabel?: string
  /** Short label shown on the host button (groupMode only). */
  hostBadge?: string
}>(), {
  modelValue: '',
  multiple: false,
  title: 'Select Agent',
  defaultBadge: 'Default',
  setDefaultTitle: 'Set as default',
  configTitle: 'Agent settings',
  confirmLabel: 'OK',
  showAgentActions: true,
  excludedAgentIds: () => [],
  addedLabel: 'Added',
  groupMode: false,
  hostId: '',
  hostLabel: 'Host',
  hostActiveLabel: 'Host',
  hostBadge: 'Host',
})

const emit = defineEmits<{
  (e: 'update:open', value: boolean): void
  (e: 'update:modelValue', agentId: string | string[]): void
  (e: 'update:hostId', agentId: string): void
  (e: 'select', agentId: string | string[]): void
}>()

const { agents, loadAgents, isDefaultAgent, getAgentDefaultModelName, setDefaultAgent } = useAgents()

// Guard against accidental clicks right after opening the agent selector
let openTime = 0
const agentsLoading = ref(false)

// selectedIds is the live multi-select buffer. In single mode it mirrors the
// string modelValue; in multiple mode it is the working array until confirm.
const selectedIds = ref<string[]>([])

function isSelected(agentId: string): boolean {
  if (props.multiple) return selectedIds.value.includes(agentId)
  return agentId === props.modelValue
}

/** isExcluded reports whether the agent is already in the group (unpickable). */
function isExcluded(agentId: string): boolean {
  return props.excludedAgentIds.includes(agentId)
}

function handleClose() {
  emit('update:open', false)
}

function handleSelect(agentId: string) {
  // Already a member: not selectable (the backend would rejoin/no-op anyway).
  if (isExcluded(agentId)) return
  // Ignore clicks within 400ms of opening — prevents accidental selection
  // from touch events that propagate to the newly rendered dialog
  if (Date.now() - openTime < 400) return
  if (props.multiple) {
    // Toggle; do not close.
    const i = selectedIds.value.indexOf(agentId)
    if (i >= 0) {
      selectedIds.value.splice(i, 1)
      // Deselecting the host clears it (the host must be a member). No
      // re-seeding: leaving no host is a valid choice — it means a FREE group
      // (design §13.1), created from the confirm button.
      if (props.groupMode && props.hostId === agentId) emit('update:hostId', '')
    } else {
      selectedIds.value.push(agentId)
    }
    return
  }
  emit('update:modelValue', agentId)
  emit('select', agentId)
  handleClose()
}

// The host button only appears in group mode on SELECTED rows; clicking it
// sets the single host without toggling the row's selection. Clicking the
// ALREADY-active host clears it (toggle off) — leaving no host is a valid
// choice (a FREE group, design §13.1).
function handleSetHost(agentId: string) {
  if (Date.now() - openTime < 400) return
  emit('update:hostId', props.hostId === agentId ? '' : agentId)
}

// Confirm is blocked until there is a valid group. Host mode (a host chosen)
// needs the host among the members; free mode (no host) needs at least two
// members (design §13.1). Non-group multi-select has no such constraint.
const confirmDisabled = computed(() => {
  if (!props.groupMode) return false
  if (props.hostId) return !selectedIds.value.includes(props.hostId)
  return selectedIds.value.length < 2
})

function handleConfirmMulti() {
  const ids = [...selectedIds.value]
  emit('update:modelValue', ids)
  emit('select', ids)
  handleClose()
}


async function handleSetDefaultAgent(agentId: string) {
  await setDefaultAgent(agentId)
}

// Deep-link into the settings tab at this agent's detail page. Mirrors the
// AppHeader "more appearance options" pattern: a module-level pending settings
// category is set first, then the settings tab is switched to; SettingsPage
// consumes the request whether it was already mounted or is mounted lazily.
// The agent selector is closed so it does not linger over the settings page.
const switchTab = inject<(tab: string) => void>('switchTab', () => {})
function handleOpenAgentConfig(agentId: string) {
  if (!agentId) return
  handleClose()
  setPendingSettingsCategory(`agents:${agentId}`)
  switchTab('settings')
}

function defaultModelName(agentId: string): string {
  return getAgentDefaultModelName(agentId) || ''
}

// ── Keyboard ↑/↓ + Enter navigation over the agent list ──
const listNav = useListNav({
  getCount: () => agents.value.length,
  onConfirm: (idx) => handleSelect(agents.value[idx].id),
  onActiveChange: scrollActiveIntoView,
})
// Document-level keys so navigation works regardless of where focus is inside the drawer
useListKeys({ isOpen: () => props.open, nav: listNav })

function scrollActiveIntoView(index: number) {
  const items = document.querySelectorAll('.agent-list .agent-option')
  const el = items[index]
  if (el && typeof el.scrollIntoView === 'function') {
    el.scrollIntoView({ behavior: 'auto', block: 'nearest' })
  }
}

watch(agents, () => listNav.reset())

// Auto-reset touch guard and preload agents when drawer opens
watch(() => props.open, async (val) => {
  if (val) {
    openTime = Date.now()
    // Seed the multi-select buffer from the incoming modelValue.
    if (props.multiple) {
      selectedIds.value = Array.isArray(props.modelValue) ? [...props.modelValue] : []
    }
    agentsLoading.value = true
    try {
      await loadAgents()
    } finally {
      agentsLoading.value = false
    }
  }
}, { immediate: true })
</script>

<style scoped>
.agent-list {
  display: flex;
  flex-direction: column;
  gap: 0;
  padding: 0;
  overflow-y: auto;
}

.agent-list-empty {
  min-height: 30vh;
  display: flex;
  align-items: center;
  justify-content: center;
  color: var(--text-muted, #999);
  font-size: var(--font-size-md);
}

.agent-option {
  display: flex;
  align-items: center;
  gap: var(--space-3);
  padding: var(--space-3) var(--space-4);
  border: none;
  border-bottom: 1px solid var(--border-color, #e5e5e5);
  border-radius: 0;
  background: none;
  cursor: pointer;
  transition: background var(--duration-base);
  text-align: left;
}

.agent-option:last-child {
  border-bottom: none;
}

@media (hover: hover) {
  .agent-option:hover {
    background: var(--bg-secondary, #f8f9fa);
  }

  .agent-option:hover .agent-option-name {
    color: var(--accent-color, #0066cc);
  }

  .agent-option:hover .agent-option-specialty {
    color: var(--text-secondary, #666);
  }

  .agent-option:hover .agent-tag {
    opacity: 1;
  }
}

.agent-option-active {
  background: var(--bg-secondary, #f8f9fa);
  border-radius: 0;
}

.agent-option:active {
  background: var(--bg-hover, rgba(0,0,0,0.06));
}

.agent-option.selected {
  background: color-mix(in srgb, var(--accent-color) 10%, transparent);
}

/* Already a member: dimmed, not clickable, and no hover affordance. */
.agent-option-disabled {
  opacity: var(--opacity-disabled, 0.4);
  cursor: default;
  pointer-events: none;
}
.agent-added-tag {
  flex-shrink: 0;
  padding: 1px var(--space-3);
  border-radius: var(--radius-full);
  background: var(--bg-tertiary, #eee);
  color: var(--text-muted, #999);
  font-size: var(--font-size-2xs);
  line-height: 16px;
  opacity: 1;
}

.agent-option-icon {
  flex-shrink: 0;
}

.agent-option-detail {
  flex: 1;
  display: flex;
  flex-direction: column;
  gap: var(--space-1);
  min-width: 0;
}

.agent-option-name {
  font-size: var(--font-size-md);
  color: var(--text-primary, #1a1a1a);
  font-weight: var(--font-weight-medium);
}

.agent-set-default-btn {
  flex-shrink: 0;
  display: flex;
  align-items: center;
  justify-content: center;
  width: 28px;
  height: 28px;
  border: none;
  border-radius: var(--radius-sm);
  background: none;
  color: var(--text-secondary, #666);
  cursor: pointer;
  opacity: var(--opacity-disabled);
  transition: opacity var(--duration-base), background var(--duration-base);
}

.agent-default-badge-pill {
  flex-shrink: 0;
  font-size: var(--font-size-2xs);
  font-weight: var(--font-weight-semibold);
  color: #fff;
  background: var(--accent-color, #0066cc);
  padding: 1px 5px;
  border-radius: var(--radius-xs);
  white-space: nowrap;
}

.agent-config-btn {
  flex-shrink: 0;
  display: flex;
  align-items: center;
  justify-content: center;
  width: 28px;
  height: 28px;
  border: none;
  border-radius: var(--radius-sm);
  background: none;
  color: var(--text-secondary, #666);
  cursor: pointer;
  opacity: var(--opacity-disabled);
  transition: opacity var(--duration-base), background var(--duration-base);
}

@media (hover: hover) {
  .agent-config-btn:hover {
    opacity: 1;
    background: var(--bg-hover);
  }
}

@media (hover: hover) {
  .agent-set-default-btn:hover {
    opacity: 1;
    background: var(--bg-hover);
  }

  .agent-option:hover .agent-set-default-btn {
    opacity: var(--opacity-soft);
  }
}

.agent-option-specialty {
  font-size: var(--font-size-xs);
  color: var(--text-secondary, #666);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.agent-option-tags {
  display: flex;
  gap: var(--space-2);
  margin-top: var(--space-1);
}

.agent-tag {
  font-size: var(--font-size-2xs);
  padding:1px var(--space-2);
  border-radius: 0;
  font-weight: var(--font-weight-medium);
  flex-shrink: 0;
}

.backend-tag {
  background: rgba(0, 102, 204, 0.1);
  color: var(--accent-color, #0066cc);
  text-transform: lowercase;
}

.model-tag {
  background: rgba(100, 100, 100, 0.08);
  color: var(--text-muted, #999);
  max-width: 120px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

/* Multi-select checkbox and confirm button */
.agent-option-check {
  flex-shrink: 0;
  width: 20px;
  height: 20px;
  border: 1px solid var(--border-color, #ccc);
  border-radius: var(--radius-sm, 4px);
  display: flex;
  align-items: center;
  justify-content: center;
  color: var(--accent-color, #0066cc);
}

.agent-option-check.checked {
  border-color: var(--accent-color, #0066cc);
  background: color-mix(in srgb, var(--accent-color, #0066cc) 12%, transparent);
}

/* Group mode: the host control sits where the default star normally is, but is
   a labelled button so it reads as an action rather than a decorative dot.
   Only one may be active; clicking it never toggles the row's selection. */
.agent-host-btn {
  flex-shrink: 0;
  display: flex;
  align-items: center;
  gap: var(--space-1);
  padding: 3px var(--space-2);
  border: 1px solid var(--border-color, #ccc);
  border-radius: var(--radius-full);
  background: none;
  color: var(--text-secondary, #666);
  font-size: var(--font-size-2xs);
  font-weight: var(--font-weight-medium);
  line-height: 1;
  cursor: pointer;
  white-space: nowrap;
  transition: color var(--duration-base), border-color var(--duration-base), background var(--duration-base);
}

.agent-host-btn.active {
  border-color: var(--accent-color, #0066cc);
  background: color-mix(in srgb, var(--accent-color, #0066cc) 12%, transparent);
  color: var(--accent-color, #0066cc);
}

.agent-host-btn-label {
  line-height: 1;
}

.agent-multi-confirm:disabled {
  opacity: var(--opacity-disabled, 0.4);
  cursor: not-allowed;
}

/* Group mode: the mode subtitle to the right of the header title. Muted and
   non-shrinking so it never pushes the title around as it flips between
   "host mode" and "free mode". */
.agent-header-mode {
  flex-shrink: 0;
  margin-left: auto;
  color: var(--text-muted, #999);
  font-size: var(--font-size-sm);
  font-weight: var(--font-weight-normal);
}

.agent-multi-confirm {
  width: 100%;
  padding: var(--space-3) var(--space-4);
  border: none;
  border-radius: var(--radius-md, 8px);
  background: var(--accent-color, #0066cc);
  color: #fff;
  font-size: var(--font-size-md);
  font-weight: var(--font-weight-medium);
  cursor: pointer;
}
</style>
