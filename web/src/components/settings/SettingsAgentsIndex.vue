<template>
  <div class="settings-agents-index">
    <!-- Rescan row -->
    <div class="settings-agents-index__rescan-row" :class="{ 'settings-agents-index__rescan-row--disabled': rescanning }" @click="handleRescan">
      <span class="settings-agents-index__rescan-label">{{ rescanning ? t('settings.items.agentRescanning') : t('settings.items.agentRescan') }}</span>
      <RefreshCw :size="16" :class="{ 'spin': rescanning }" class="settings-agents-index__rescan-icon" />
    </div>
    <div
      v-for="agent in agentList"
      :key="agent.id"
      class="settings-agents-index__row"
      @click="$emit('navigate', `agents:${agent.id}`)"
    >
      <div class="settings-agents-index__left">
        <span class="settings-agents-index__icon">{{ agent.icon }}</span>
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
    <div v-if="agentList.length === 0" class="settings-agents-index__empty">
      {{ t('settings.items.agentNoAgents') }}
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { ChevronRight, RefreshCw } from 'lucide-vue-next'
import { useI18n } from 'vue-i18n'
import { useAgents } from '@/composables/useAgents'
import { useToast } from '@/composables/useToast'

defineEmits<{
  navigate: [categoryId: string]
}>()

const { t } = useI18n()
const toast = useToast()
const { agents, defaultAgentId, loadAgents, rescanAgents } = useAgents()

const rescanning = ref(false)

onMounted(() => {
  loadAgents(true)
})

const agentList = computed(() =>
  [...agents.value].sort((a, b) => (a.sortOrder ?? 0) - (b.sortOrder ?? 0))
)

async function handleRescan() {
  rescanning.value = true
  try {
    await rescanAgents()
    toast.show(t('settings.items.agentRescanSuccess'), { icon: '✓', type: 'success', duration: 3000 })
  } catch {
    toast.show(t('settings.items.agentRescanFailed'), { icon: '⚠️', type: 'error', duration: 3000 })
  } finally {
    rescanning.value = false
  }
}
</script>

<style scoped>
.settings-agents-index {
  padding: 8px 0;
  background: var(--bg-secondary);
  min-height: 100%;
}

.settings-agents-index__rescan-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  min-height: 48px;
  padding: 8px 16px;
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
  font-size: 15px;
  color: var(--text-primary);
}

.settings-agents-index__rescan-icon {
  color: var(--text-muted);
  flex-shrink: 0;
}

.settings-agents-index__rescan-row--disabled {
  opacity: 0.6;
  cursor: not-allowed;
  pointer-events: none;
}

.spin {
  animation: spin 1s linear infinite;
}

@keyframes spin {
  from { transform: rotate(0deg); }
  to { transform: rotate(360deg); }
}

.settings-agents-index__row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  min-height: 48px;
  padding: 8px 16px;
  cursor: pointer;
  gap: 12px;
  background: var(--bg-primary);
  position: relative;
}

.settings-agents-index__row:not(:last-child)::after {
  content: '';
  position: absolute;
  bottom: 0;
  left: 48px;
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

.settings-agents-index__left {
  display: flex;
  align-items: center;
  gap: 12px;
  min-width: 0;
  flex: 1;
}

.settings-agents-index__icon {
  flex-shrink: 0;
  font-size: 20px;
  line-height: 1;
}

.settings-agents-index__text {
  display: flex;
  flex-direction: column;
  gap: 2px;
  min-width: 0;
}

.settings-agents-index__name-row {
  display: flex;
  align-items: center;
  gap: 6px;
  min-width: 0;
}

.settings-agents-index__name {
  font-size: 15px;
  color: var(--text-primary);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.settings-agents-index__default-badge {
  flex-shrink: 0;
  font-size: 10px;
  font-weight: 600;
  line-height: 1;
  padding: 1px 5px;
  border-radius: 3px;
  background: var(--accent-color, #0066cc);
  color: #fff;
  white-space: nowrap;
}

.settings-agents-index__specialty {
  font-size: 12px;
  color: var(--text-secondary);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.settings-agents-index__arrow {
  flex-shrink: 0;
  color: var(--text-muted);
  margin-left: 4px;
}

.settings-agents-index__empty {
  padding: 24px 16px;
  text-align: center;
  color: var(--text-muted);
  font-size: 14px;
}
</style>
