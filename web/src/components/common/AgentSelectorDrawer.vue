<template>
  <BottomSheet :open="open" auto @close="handleClose">
    <template #header>
      <!-- Group-creation mode reads as a group, not a single agent: the header
           glyph is the same Users mark the session list uses for group
           sessions, so the two surfaces stay one system. -->
      <Users v-if="groupMode" :size="16" class="bs-header-icon" />
      <Bot v-else :size="16" class="bs-header-icon" />
      <span class="bs-header-title agent-header-title">{{ title }}</span>
      <!-- Single vs group creation share this one page; the segmented control
           on the right flips between them. Shown whenever at least one agent is
           selectable: a group needs two MEMBERS, but instances of the SAME agent
           count as separate members (allowInstances), so a single installed
           agent can still form a two-instance group via the row's "+" stepper.
           Only the empty list hides the switch — there is nothing to build a
           group from. @click.stop is required — the whole .bs-header is a close
           target (BottomSheet's own click handler). -->
      <div v-if="showModeSwitch && selectableAgents.length > 0" class="agent-mode-switch" role="tablist">
        <button
          type="button"
          class="agent-mode-tab"
          :class="{ active: !groupMode }"
          role="tab"
          :aria-selected="!groupMode"
          data-mode="single"
          @click.stop="emit('switch-mode', 'single')"
        >{{ t('session.singleChat') }}</button>
        <button
          type="button"
          class="agent-mode-tab"
          :class="{ active: groupMode }"
          role="tab"
          :aria-selected="groupMode"
          data-mode="group"
          @click.stop="emit('switch-mode', 'group')"
        >{{ t('session.groupChat') }}</button>
      </div>
    </template>
    <div class="agent-list">
      <PanelSkeleton v-if="agentsLoading" variant="list" :rows="8" :meta="false" :label="t('common.loading')" />
      <div v-else-if="selectableAgents.length === 0" class="agent-list-empty">{{ t('chat.messageList.noAgentsTitle') }}</div>
      <div
        v-for="(agent, idx) in selectableAgents"
        :key="agent.id"
        class="agent-option"
        :class="{ selected: isSelected(agent.id), 'agent-option-active': listNav.activeIndex.value === idx }"
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
        <!-- Group/instance trailing controls: a full-height column whose two
             ends line up with the row's text — the instance stepper near the top
             (name/description) and the host action at the bottom (model-tag
             row). The plain star/config actions (single chat / task) keep their
             original horizontal slots. -->
        <div v-if="(groupMode || allowInstances) && isSelected(agent.id)" class="agent-row-actions">
          <!-- Instance stepper: − count + . Only the count is shown (no nickname
               preview). "+" is disabled at the group-wide instance limit. -->
          <div v-if="allowInstances" class="agent-stepper" @click.stop>
            <button
              class="agent-stepper-btn"
              type="button"
              :aria-label="t('group.decreaseInstance')"
              :disabled="countOf(agent.id) <= 1"
              @click.stop="decrement(agent.id)"
            >
              <Minus :size="12" />
            </button>
            <span class="agent-stepper-count">{{ countOf(agent.id) }}</span>
            <button
              class="agent-stepper-btn"
              type="button"
              :aria-label="t('group.increaseInstance')"
              :disabled="atInstanceLimit"
              @click.stop="increment(agent.id)"
            >
              <Plus :size="12" />
            </button>
          </div>
          <!-- Host control sits at the bottom, level with the model-tag row. It
               is a labelled button so it reads as an action. Only selected rows
               can be host (the host must be a member). The label states the
               ACTION ("set as host") until the row IS the host, at which point it
               shows the resulting state badge ("Host"). -->
          <button
            v-if="groupMode"
            class="agent-host-btn"
            :class="{ active: agent.id === hostId }"
            type="button"
            :aria-pressed="agent.id === hostId"
            :title="agent.id === hostId ? hostActiveLabel : hostLabel"
            @click.stop="handleSetHost(agent.id)"
          >
            <Crown :size="12" />
            <span class="agent-host-btn-label">{{ agent.id === hostId ? hostBadge : hostLabel }}</span>
          </button>
        </div>
        <span v-if="!allowInstances && showAgentActions && isDefaultAgent(agent.id)" class="agent-default-badge-pill">{{ defaultBadge }}</span>
        <button v-else-if="!allowInstances && showAgentActions" class="agent-set-default-btn" @click.stop="handleSetDefaultAgent(agent.id)" :title="setDefaultTitle">
          <Star :size="14" />
        </button>
        <button v-if="!allowInstances && showAgentActions" class="agent-config-btn" @click.stop="handleOpenAgentConfig(agent.id)" :title="configTitle">
          <Settings :size="14" />
        </button>
      </div>
    </div>
    <!-- Confirm is a plain footer pill (the shared .fbtn language), not a
         full-width bar: .bs-footer already right-aligns its content, so the
         button hugs the trailing edge like every other drawer confirm. -->
    <template v-if="multiple" #footer>
      <!-- Leading group (count hint + mode hint) absorbs the free space via a
           SINGLE margin-right:auto; two separate auto margins would split the
           slack and strand the confirm pill mid-footer. -->
      <div class="agent-footer-lead">
        <!-- Group mode: the chosen host decides the group's mode. The chip, on
             hover / tap, explains how the current mode routes (title + one line
             + tips). Same glyph pairing as the session-list mode chip (crown for
             host, @ for the free relay) so the two surfaces read as one system. -->
        <GroupModeHint v-if="groupMode" :mode="hostId ? 'host' : 'free'" class="agent-mode-hint" />
        <!-- Instance count (group create / add members): the cap counts every
             instance across all agents, so the running total is surfaced here.
             At the limit the line turns into a warning so the disabled "+" is
             explained rather than feeling broken. -->
        <span
          v-if="allowInstances && maxInstances > 0"
          class="agent-count-hint"
          :class="{ 'at-limit': atInstanceLimit }"
        >
          <AlertTriangle v-if="atInstanceLimit" :size="12" />
          {{ atInstanceLimit
            ? t('group.memberLimitReached')
            : t('group.memberCountHint', { count: totalInstances, max: maxInstances }) }}
        </span>
      </div>
      <button class="fbtn fbtn-primary agent-multi-confirm" :disabled="confirmDisabled" @click="handleConfirmMulti">{{ confirmLabel }}</button>
    </template>
  </BottomSheet>
</template>

<script setup lang="ts">
import { ref, watch, inject, computed } from 'vue'
import { Bot, Star, Settings, Check, Crown, Users, Plus, Minus, AlertTriangle } from 'lucide-vue-next'
import { useI18n } from 'vue-i18n'
import BottomSheet from '@/components/common/BottomSheet.vue'
import AgentIcon from '@/components/common/AgentIcon.vue'
import PanelSkeleton from '@/components/common/PanelSkeleton.vue'
import GroupModeHint from '@/components/common/GroupModeHint.vue'
import { useListNav } from '@/composables/useListNav'
import { useListKeys } from '@/composables/useListKeys'
import { useAgents } from '@/composables/useAgents'
import { setPendingSettingsCategory } from '@/composables/useSettingsNavigation'
// The confirm button uses the shared .fbtn language; import it directly rather
// than relying on BottomSheet happening to load it (design-guide rule).
import '@/assets/modal-footer-btn.css'

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
  /** Allow the same agent to be selected multiple times (group creation / add
   *  members). A selected row then shows a − count + stepper instead of a plain
   *  toggle; the confirm payload repeats the agent id once per instance. */
  allowInstances?: boolean
  /** Max TOTAL instances across all agents (group member cap). 0 = unlimited.
   *  At the limit the "+" buttons disable and the footer warns. Only meaningful
   *  with allowInstances. */
  maxInstances?: number
  /** Instances ALREADY in the group (add-members flow). The cap is group-wide,
   *  so the picker must count these against maxInstances even though they are
   *  not in the selection buffer. 0 for group creation. */
  usedInstances?: number
  /** Show the single/group segmented switch in the header (create page only).
   *  Hidden only when no agent is selectable; one agent is enough because
   *  repeat instances of the same agent are valid distinct members. */
  showModeSwitch?: boolean
}>(), {
  modelValue: '',
  multiple: false,
  title: 'Select Agent',
  defaultBadge: 'Default',
  setDefaultTitle: 'Set as default',
  configTitle: 'Agent settings',
  confirmLabel: 'OK',
  showAgentActions: true,
  groupMode: false,
  hostId: '',
  hostLabel: 'Host',
  hostActiveLabel: 'Host',
  hostBadge: 'Host',
  allowInstances: false,
  maxInstances: 0,
  usedInstances: 0,
  showModeSwitch: false,
})

const emit = defineEmits<{
  (e: 'update:open', value: boolean): void
  (e: 'update:modelValue', agentId: string | string[]): void
  (e: 'update:hostId', agentId: string): void
  (e: 'select', agentId: string | string[]): void
  /** User flipped the single/group segmented switch (create page only). */
  (e: 'switch-mode', mode: 'single' | 'group'): void
}>()

const { selectableAgents, loadAgents, isDefaultAgent, getAgentDefaultModelName, setDefaultAgent } = useAgents()

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

/** countOf reports how many instances of the agent are currently selected. */
function countOf(agentId: string): number {
  return selectedIds.value.filter(id => id === agentId).length
}

/** totalInstances is the instance count across ALL agents — the group member
 *  cap counts every instance, not unique agents. Includes instances already in
 *  the group (usedInstances, add-members flow) plus the current selection. */
const totalInstances = computed(() => props.usedInstances + selectedIds.value.length)

/** atInstanceLimit is true when the group-wide instance cap is reached, so no
 *  further "+" is allowed. maxInstances <= 0 means unlimited. */
const atInstanceLimit = computed(() => props.maxInstances > 0 && totalInstances.value >= props.maxInstances)

/** increment adds one more instance of the agent (used by the stepper's +). */
function increment(agentId: string) {
  if (Date.now() - openTime < 400) return
  if (atInstanceLimit.value) return
  selectedIds.value.push(agentId)
}

/** decrement removes one instance; dropping to 0 deselects the row. Removing
 *  the last instance also clears the host if the host was this agent (the host
 *  must remain a member). */
function decrement(agentId: string) {
  if (Date.now() - openTime < 400) return
  const i = selectedIds.value.lastIndexOf(agentId)
  if (i < 0) return
  selectedIds.value.splice(i, 1)
  if (props.groupMode && props.hostId === agentId && countOf(agentId) === 0) {
    emit('update:hostId', '')
  }
}

function handleClose() {
  emit('update:open', false)
}

function handleSelect(agentId: string) {
  // Ignore clicks within 400ms of opening — prevents accidental selection
  // from touch events that propagate to the newly rendered dialog
  if (Date.now() - openTime < 400) return
  if (props.multiple) {
    // Toggle; do not close.
    if (props.allowInstances) {
      // Instance mode: clicking a row selects one instance; clicking an
      // already-selected row clears ALL its instances (back to 0). Fine-grained
      // counts are adjusted with the row's − / + stepper, not by the row click.
      const n = countOf(agentId)
      if (n > 0) {
        selectedIds.value = selectedIds.value.filter(id => id !== agentId)
        if (props.groupMode && props.hostId === agentId) emit('update:hostId', '')
      } else {
        // Selecting a fresh agent adds one instance; refuse at the group-wide
        // limit (same rule as the stepper's +).
        if (atInstanceLimit.value) return
        selectedIds.value.push(agentId)
      }
      return
    }
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
  getCount: () => selectableAgents.value.length,
  onConfirm: (idx) => handleSelect(selectableAgents.value[idx].id),
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

watch(selectableAgents, () => listNav.reset())

// Single ↔ group switch on the shared create page: the member buffer and the
// keyboard highlight belong to the OLD mode, so both are cleared. Re-arm the
// touch guard too, otherwise the first tap right after switching is swallowed
// by the same 400ms window that protects the initial open.
watch(() => props.groupMode, () => {
  selectedIds.value = []
  listNav.reset()
  openTime = Date.now()
})

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
  width: calc(28 * var(--dp));
  height: calc(28 * var(--dp));
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
  padding: 1px calc(5 * var(--dp));
  border-radius: var(--radius-xs);
  white-space: nowrap;
}

.agent-config-btn {
  flex-shrink: 0;
  display: flex;
  align-items: center;
  justify-content: center;
  width: calc(28 * var(--dp));
  height: calc(28 * var(--dp));
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
  padding: 1px var(--space-2);
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
  max-width: calc(120 * var(--dp));
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

/* Multi-select checkbox and confirm button */
.agent-option-check {
  flex-shrink: 0;
  width: calc(20 * var(--dp));
  height: calc(20 * var(--dp));
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

/* Instance-mode trailing actions: a full-height column whose two ends line up
   with the row's text — the instance stepper near the top (name/description
   line) and the host action at the bottom (model-tag line). `align-self:
   stretch` + `justify-content: space-between` let the row's own height drive the
   placement, so the two controls read as belonging to different text lines
   instead of one glued block. */
.agent-row-actions {
  flex-shrink: 0;
  align-self: stretch;
  display: flex;
  flex-direction: column;
  align-items: flex-end;
  justify-content: space-between;
}

/* Instance stepper: − count + for a selected row when the same agent may be
   added several times. Compact pill matching the host button's language. */
.agent-stepper {
  flex-shrink: 0;
  display: flex;
  align-items: center;
  gap: var(--space-1);
  padding: calc(2 * var(--dp)) var(--space-1);
  border: 1px solid var(--border-color, #ccc);
  border-radius: var(--radius-full);
  background: none;
}

.agent-stepper-btn {
  display: flex;
  align-items: center;
  justify-content: center;
  width: calc(20 * var(--dp));
  height: calc(20 * var(--dp));
  border: none;
  border-radius: var(--radius-full);
  background: none;
  color: var(--text-secondary, #666);
  cursor: pointer;
  transition: background var(--duration-base), color var(--duration-base);
}

.agent-stepper-btn:disabled {
  opacity: var(--opacity-disabled, 0.4);
  cursor: default;
}

@media (hover: hover) {
  .agent-stepper-btn:not(:disabled):hover {
    background: var(--bg-hover, rgba(0,0,0,0.06));
    color: var(--accent-color, #0066cc);
  }
}

.agent-stepper-count {
  min-width: calc(14 * var(--dp));
  text-align: center;
  font-size: var(--font-size-xs);
  font-weight: var(--font-weight-medium);
  color: var(--text-primary, #1a1a1a);
  font-variant-numeric: tabular-nums;
}

/* Group mode: the host control sits where the default star normally is, but is
   a labelled button so it reads as an action rather than a decorative dot.
   Only one may be active; clicking it never toggles the row's selection. */
.agent-host-btn {
  flex-shrink: 0;
  display: flex;
  align-items: center;
  gap: var(--space-1);
  padding: calc(3 * var(--dp)) var(--space-2);
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

/* Header title: the base .bs-header-title is flex-shrink:0 + nowrap, which
   overflows the narrow drawer header once the segmented switch is added. Let it
   absorb the slack and ellipsize instead of pushing the switch off-screen. */
.agent-header-title {
  flex: 1 1 auto;
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
}

/* Single/group segmented switch (create page). Same connected-tab language as
   SessionListTabs / SummaryToggle, compacted for the header line. */
.agent-mode-switch {
  flex-shrink: 0;
  margin-left: auto;
  display: inline-flex;
  align-items: stretch;
  border: 1px solid var(--border-color, #ccc);
  border-radius: var(--radius-sm);
  overflow: hidden;
}

.agent-mode-tab {
  border: none;
  background: none;
  color: var(--text-secondary, #666);
  font-size: var(--font-size-xs);
  font-weight: var(--font-weight-medium);
  line-height: 1;
  padding: calc(4 * var(--dp)) var(--space-4);
  cursor: pointer;
  white-space: nowrap;
  transition: color var(--duration-base), background var(--duration-base);
}

.agent-mode-tab + .agent-mode-tab {
  border-left: 1px solid var(--border-color, #ccc);
}

.agent-mode-tab.active {
  color: var(--accent-color, #0066cc);
  background: color-mix(in srgb, var(--accent-color, #0066cc) 12%, transparent);
}

@media (hover: hover) {
  .agent-mode-tab:not(.active):hover {
    color: var(--text-primary, #1a1a1a);
    background: color-mix(in srgb, var(--text-primary) 5%, transparent);
  }
}

/* Footer leading group: one auto margin absorbs the slack so the confirm pill
   keeps the trailing edge; the children lay out inline with a small gap. */
.agent-footer-lead {
  margin-right: auto;
  display: inline-flex;
  align-items: center;
  gap: var(--space-3);
  min-width: 0;
}

/* Footer instance count / limit warning. The warning state uses the danger hue
   with an icon. */
.agent-count-hint {
  display: inline-flex;
  align-items: center;
  gap: var(--space-1);
  font-size: var(--font-size-xs);
  color: var(--text-secondary, #666);
  font-variant-numeric: tabular-nums;
  white-space: nowrap;
}

.agent-count-hint.at-limit {
  color: var(--color-red, #dc2626);
  font-weight: var(--font-weight-medium);
}

/* Confirm button: shape/colour come from the shared .fbtn / .fbtn-primary
   language (modal-footer-btn.css). Nothing to declare here — a scoped override
   would out-specify the shared rules and silently re-diverge. */
</style>
