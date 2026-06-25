<template>
  <div class="settings-agents-index">
    <div
      v-for="agent in agentList"
      :key="agent.id"
      class="settings-agents-index__row"
      @click="$emit('navigate', `agents:${agent.id}`)"
    >
      <div class="settings-agents-index__left">
        <span class="settings-agents-index__icon">{{ agent.icon }}</span>
        <div class="settings-agents-index__text">
          <span class="settings-agents-index__name">{{ agent.name }}</span>
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
import { computed, onMounted } from 'vue'
import { ChevronRight } from 'lucide-vue-next'
import { useI18n } from 'vue-i18n'
import { useAgents } from '@/composables/useAgents'

defineEmits<{
  navigate: [categoryId: string]
}>()

const { t } = useI18n()
const { agents, loadAgents } = useAgents()

onMounted(() => {
  loadAgents(true)
})

const agentList = computed(() =>
  [...agents.value].sort((a, b) => (a.sortOrder ?? 0) - (b.sortOrder ?? 0))
)
</script>

<style scoped>
.settings-agents-index {
  padding: 8px 0;
  background: var(--bg-secondary);
  min-height: 100%;
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

.settings-agents-index__name {
  font-size: 15px;
  color: var(--text-primary);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
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
}

.settings-agents-index__empty {
  padding: 24px 16px;
  text-align: center;
  color: var(--text-muted);
  font-size: 14px;
}
</style>
