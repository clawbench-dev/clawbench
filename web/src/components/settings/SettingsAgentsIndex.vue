<template>
  <div class="settings-agents-index">
    <!-- Rescan row -->
    <div class="settings-agents-index__rescan-row refresh-spin" :class="{ 'settings-agents-index__rescan-row--disabled': rescanning, 'refresh-spin--active': rescanning }" @click="handleRescan">
      <span class="settings-agents-index__rescan-label">{{ rescanning ? t('settings.items.agentRescanning') : t('settings.items.agentRescan') }}</span>
      <RefreshCw :size="16" class="settings-agents-index__rescan-icon" />
    </div>
    <VueDraggable
      v-if="localAgents.length > 0"
      v-model="localAgents"
      handle=".settings-agents-index__drag-handle"
      @end="onDragEnd"
    >
      <div
        v-for="agent in localAgents"
        :key="agent.id"
        class="settings-agents-index__row"
        @click="$emit('navigate', `agents:${agent.id}`)"
      >
        <span class="settings-agents-index__drag-handle" :title="t('settings.items.agentDragHandle')" @click.stop>≡</span>
        <div class="settings-agents-index__left">
          <span class="settings-agents-index__icon"><AgentIcon :backend="agent.backend" :name="agent.name" :avatar="agent.avatar" size="md" /></span>
          <div class="settings-agents-index__text">
            <div class="settings-agents-index__name-row">
              <span class="settings-agents-index__name">{{ agent.name }}</span>
              <span v-if="agent.id === defaultAgentId" class="settings-agents-index__default-badge">{{ t('chat.sessionSetting.defaultBadge') }}</span>
            </div>
            <span v-if="agent.specialty" class="settings-agents-index__specialty">{{ agent.specialty }}</span>
          </div>
        </div>
        <ChevronRight class="settings-agents-index__arrow" :size="18" />
      </div>
    </VueDraggable>
    <div v-if="agentList.length === 0" class="settings-agents-index__empty">
      {{ t('settings.items.agentNoAgents') }}
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { ChevronRight, RefreshCw } from 'lucide-vue-next'
import { VueDraggable } from 'vue-draggable-plus'
import { useI18n } from 'vue-i18n'
import { useAgents } from '@/composables/useAgents'
import AgentIcon from '@/components/common/AgentIcon.vue'
import { useToast } from '@/composables/useToast'

defineEmits<{
  navigate: [categoryId: string]
}>()

const { t } = useI18n()
const toast = useToast()
const { agents, defaultAgentId, loadAgents, rescanAgents, reorderAgents } = useAgents()

const rescanning = ref(false)

onMounted(() => {
  loadAgents(true)
})

const agentList = computed(() =>
  [...agents.value].sort((a, b) => (a.sortOrder ?? 0) - (b.sortOrder ?? 0))
)

// Local mirror the draggable mutates in place. Synced from the server order so
// a failed reorder (rolled back) or a background reload lands here too.
const localAgents = ref([...agentList.value])
watch(agentList, (val) => {
  localAgents.value = [...val]
}, { deep: true })

async function onDragEnd() {
  const ids = localAgents.value.map(a => a.id)
  try {
    await reorderAgents(ids)
  } catch {
    toast.show(t('settings.items.agentReorderFailed'), { icon: 'warning', type: 'error', duration: 3000 })
    localAgents.value = [...agentList.value]
  }
}

async function handleRescan() {
  rescanning.value = true
  try {
    await rescanAgents()
    toast.show(t('settings.items.agentRescanSuccess'), { icon: 'success', type: 'success', duration: 3000 })
  } catch {
    toast.show(t('settings.items.agentRescanFailed'), { icon: 'warning', type: 'error', duration: 3000 })
  } finally {
    rescanning.value = false
  }
}
</script>

<style scoped>
.settings-agents-index {
  padding: var(--space-4) 0;
  background: var(--bg-secondary);
  min-height: 100%;
}

.settings-agents-index__rescan-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  min-height: 38px;
  padding: var(--space-4) var(--space-7);
  cursor: pointer;
  background: var(--bg-primary);
  position: relative;
}

.settings-agents-index__rescan-row::after {
  content: '';
  position: absolute;
  bottom: 0;
  left: 0;
  right: 0;
  height: 0.5px;
  background: var(--border-color);
}

@media (hover: hover) {
  .settings-agents-index__rescan-row:hover {
    background: var(--bg-secondary);
  }
}

.settings-agents-index__rescan-row:active {
  background: var(--bg-tertiary);
}

.settings-agents-index__rescan-label {
  font-size: var(--font-size-md);
  color: var(--text-primary);
}

.settings-agents-index__rescan-icon {
  color: var(--text-muted);
  flex-shrink: 0;
}

.settings-agents-index__rescan-row--disabled {
  opacity: var(--opacity-muted);
  cursor: not-allowed;
  pointer-events: none;
}

.settings-agents-index__row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  min-height: 38px;
  padding: var(--space-4) var(--space-7);
  cursor: pointer;
  gap: var(--space-6);
  background: var(--bg-primary);
  position: relative;
}

.settings-agents-index__row:not(:last-child)::after {
  content: '';
  position: absolute;
  bottom: 0;
  /* Inset to the text start: row padding + drag handle + row gap + icon + gap. */
  left: calc(var(--space-7) + var(--icon-size-md) + var(--space-6) + var(--icon-size-md) + var(--space-6));
  right: 0;
  height: 0.5px;
  background: var(--border-color);
}

@media (hover: hover) {
  .settings-agents-index__row:hover {
    background: var(--bg-secondary);
  }
}

.settings-agents-index__row:active {
  background: var(--bg-tertiary);
}

.settings-agents-index__drag-handle {
  flex-shrink: 0;
  /* Fixed width (the icon's own size) so the divider inset below stays aligned
     no matter how wide the glyph renders. */
  width: var(--icon-size-md);
  text-align: center;
  cursor: grab;
  color: var(--text-muted);
  font-size: var(--font-size-2xl);
  line-height: 1;
  user-select: none;
}

.settings-agents-index__drag-handle:active {
  cursor: grabbing;
}

.settings-agents-index__left {
  display: flex;
  align-items: center;
  gap: var(--space-6);
  min-width: 0;
  flex: 1;
}

.settings-agents-index__icon {
  flex-shrink: 0;
  line-height: 1;
}

.settings-agents-index__text {
  display: flex;
  flex-direction: column;
  gap: var(--space-1);
  min-width: 0;
}

.settings-agents-index__name-row {
  display: flex;
  align-items: center;
  gap: var(--space-3);
  min-width: 0;
}

.settings-agents-index__name {
  font-size: var(--font-size-md);
  color: var(--text-primary);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.settings-agents-index__default-badge {
  flex-shrink: 0;
  font-size: var(--font-size-2xs);
  font-weight: var(--font-weight-semibold);
  line-height: 1;
  padding: 1px 5px;
  border-radius: var(--radius-xs);
  background: var(--accent-color, #0066cc);
  color: #fff;
  white-space: nowrap;
}

.settings-agents-index__specialty {
  font-size: var(--font-size-sm);
  color: var(--text-secondary);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.settings-agents-index__arrow {
  flex-shrink: 0;
  color: var(--text-muted);
  margin-left: var(--space-2);
}

.settings-agents-index__empty {
  padding:24px var(--space-7);
  text-align: center;
  color: var(--text-muted);
  font-size: var(--font-size-lg);
}
</style>
