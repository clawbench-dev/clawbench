<template>
  <BottomSheet :open="open" auto @close="handleClose">
    <template #header>
      <Bot :size="16" class="bs-header-icon" />
      <span class="bs-header-title">{{ title }}</span>
    </template>
    <div class="agent-list">
      <LoadingIndicator v-if="agentsLoading" size="sm" />
      <div v-else-if="agents.length === 0" class="agent-list-empty">{{ t('session.noAgentsTitle') }}</div>
      <div
        v-for="(agent, idx) in agents"
        :key="agent.id"
        class="agent-option"
        :class="{ selected: agent.id === modelValue, 'agent-option-active': listNav.activeIndex.value === idx }"
        role="button"
        tabindex="0"
        @click="handleSelect(agent.id)"
        @keydown.enter="handleSelect(agent.id)"
        @keydown.space.prevent="handleSelect(agent.id)"
      >
        <span class="agent-option-icon"><AgentIcon :backend="agent.backend" :name="agent.name" :size="16" /></span>
        <div class="agent-option-detail">
          <span class="agent-option-name">{{ agent.name }}</span>
          <span class="agent-option-specialty">{{ agent.specialty }}</span>
          <div class="agent-option-tags">
            <span class="agent-tag backend-tag">{{ agent.backend }}</span>
            <span v-if="defaultModelName(agent.id)" class="agent-tag model-tag">{{ defaultModelName(agent.id) }}</span>
          </div>
        </div>
        <span v-if="isDefaultAgent(agent.id)" class="agent-default-badge-pill">{{ defaultBadge }}</span>
        <button v-else class="agent-set-default-btn" @click.stop="handleSetDefaultAgent(agent.id)" :title="setDefaultTitle">
          <Star :size="14" />
        </button>
      </div>
    </div>
  </BottomSheet>
</template>

<script setup lang="ts">
import { ref, watch } from 'vue'
import { Bot, Star } from 'lucide-vue-next'
import { useI18n } from 'vue-i18n'
import BottomSheet from '@/components/common/BottomSheet.vue'
import AgentIcon from '@/components/common/AgentIcon.vue'
import LoadingIndicator from '@/components/common/LoadingIndicator.vue'
import { useListNav } from '@/composables/useListNav'
import { useListKeys } from '@/composables/useListKeys'
import { useAgents } from '@/composables/useAgents'

const { t } = useI18n()

const props = withDefaults(defineProps<{
  open: boolean
  modelValue?: string
  title?: string
  defaultBadge?: string
  setDefaultTitle?: string
}>(), {
  modelValue: '',
  title: 'Select Agent',
  defaultBadge: 'Default',
  setDefaultTitle: 'Set as default',
})

const emit = defineEmits<{
  (e: 'update:open', value: boolean): void
  (e: 'update:modelValue', agentId: string): void
  (e: 'select', agentId: string): void
}>()

const { agents, loadAgents, isDefaultAgent, getAgentDefaultModelName, setDefaultAgent } = useAgents()

// Guard against accidental clicks right after opening the agent selector
let openTime = 0
const agentsLoading = ref(false)

function handleClose() {
  emit('update:open', false)
}

function handleSelect(agentId: string) {
  // Ignore clicks within 400ms of opening — prevents accidental selection
  // from touch events that propagate to the newly rendered dialog
  if (Date.now() - openTime < 400) return
  emit('update:modelValue', agentId)
  emit('select', agentId)
  handleClose()
}

async function handleSetDefaultAgent(agentId: string) {
  await setDefaultAgent(agentId)
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
  font-size: 13px;
}

.agent-option {
  display: flex;
  align-items: center;
  gap: 6px;
  padding: 6px 8px;
  border: none;
  border-bottom: 1px solid var(--border-color, #e5e5e5);
  border-radius: 0;
  background: none;
  cursor: pointer;
  transition: background 0.12s;
  text-align: left;
}

.agent-option:last-child {
  border-bottom: none;
}

.agent-option:hover {
  background: var(--bg-secondary, #f8f9fa);
}

.agent-option-active {
  background: var(--bg-secondary, #f8f9fa);
  border-radius: 0;
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

.agent-option:active {
  background: var(--bg-hover, rgba(0,0,0,0.06));
}

.agent-option.selected {
  background: var(--accent-bg, rgba(0, 102, 204, 0.1));
}

.agent-option-icon {
  flex-shrink: 0;
}

.agent-option-detail {
  flex: 1;
  display: flex;
  flex-direction: column;
  gap: 2px;
  min-width: 0;
}

.agent-option-name {
  font-size: 13px;
  color: var(--text-primary, #1a1a1a);
  font-weight: 500;
}

.agent-set-default-btn {
  flex-shrink: 0;
  display: flex;
  align-items: center;
  justify-content: center;
  width: 28px;
  height: 28px;
  border: none;
  border-radius: 6px;
  background: none;
  color: var(--text-secondary, #666);
  cursor: pointer;
  opacity: 0.4;
  transition: opacity 0.15s, background 0.15s;
}

.agent-default-badge-pill {
  flex-shrink: 0;
  font-size: 10px;
  font-weight: 600;
  color: #fff;
  background: var(--accent-color, #0066cc);
  padding: 1px 5px;
  border-radius: 3px;
  white-space: nowrap;
}

.agent-set-default-btn:hover {
  opacity: 1;
  background: var(--hover-bg, rgba(0,0,0,0.06));
}

.agent-option:hover .agent-set-default-btn {
  opacity: 0.7;
}

.agent-option-specialty {
  font-size: 11px;
  color: var(--text-secondary, #666);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.agent-option-tags {
  display: flex;
  gap: 4px;
  margin-top: 2px;
}

.agent-tag {
  font-size: 9px;
  padding: 1px 4px;
  border-radius: 0;
  font-weight: 500;
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
</style>
