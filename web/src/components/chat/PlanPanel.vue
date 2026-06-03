<template>
  <div v-if="entries.length > 0" class="plan-panel">
    <!-- Collapsed chip -->
    <div v-if="collapsed" class="plan-chip" :class="{ 'plan-chip--updated': hasUpdate }" @click="$emit('toggle-collapse')">
      <span class="plan-chip__pulse"></span>
      <span class="plan-chip__text">{{ chipText }}</span>
      <span class="plan-chip__toggle">▼</span>
    </div>

    <!-- Expanded timeline -->
    <div v-else class="plan-expanded">
      <div class="plan-expanded__header">
        <span class="plan-expanded__title">{{ t('chat.plan.title') }}</span>
        <span class="plan-expanded__toggle" @click="$emit('toggle-collapse')">▲</span>
      </div>
      <div class="plan-expanded__timeline">
        <div v-for="(entry, idx) in entries" :key="idx" class="plan-entry" :class="'plan-entry--' + entry.status">
          <!-- Vertical connector line -->
          <div v-if="idx < entries.length - 1" class="plan-entry__line"
            :class="{
              'plan-entry__line--solid': entry.status === 'completed',
              'plan-entry__line--dashed': entry.status !== 'completed',
              'plan-entry__line--pulsing': entry.status === 'in_progress',
            }"
            :style="{ borderColor: priorityColor(entry.priority) }">
          </div>
          <!-- Status node -->
          <div class="plan-entry__node" :style="{ borderColor: priorityColor(entry.priority) }">
            <span v-if="entry.status === 'completed'" class="plan-entry__check">✓</span>
            <span v-else-if="entry.status === 'in_progress'" class="plan-entry__dot"></span>
            <span v-else class="plan-entry__circle"></span>
          </div>
          <!-- Entry text -->
          <span class="plan-entry__text" :class="{ 'plan-entry__text--done': entry.status === 'completed' }">{{ entry.content }}</span>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { PlanEntry } from '@/composables/usePlanProgress'

const props = defineProps<{
  entries: PlanEntry[]
  collapsed: boolean
  hasUpdate: boolean
}>()

defineEmits<{
  'toggle-collapse': []
}>()

const { t } = useI18n()

const priorityColors: Record<string, string> = {
  high: '#ef4444',
  medium: '#f97316',
  low: '#9ca3af',
}

function priorityColor(priority: string): string {
  return priorityColors[priority] || priorityColors.low
}

const chipText = computed(() => {
  const inProgress = props.entries.find(e => e.status === 'in_progress')
  if (inProgress) return inProgress.content
  const completed = props.entries.filter(e => e.status === 'completed').length
  const total = props.entries.length
  return t('chat.plan.completedCount', { completed, total })
})
</script>

<style scoped>
.plan-panel {
  width: 100%;
}

/* ── Collapsed chip ── */
.plan-chip {
  display: flex;
  align-items: center;
  gap: 6px;
  padding: 4px 10px;
  border-radius: 16px;
  background: var(--color-bg-2, #1e1e2e);
  border: 1px solid var(--color-border, #3a3a4a);
  cursor: pointer;
  transition: border-color 0.3s ease;
}

.plan-chip--updated {
  animation: plan-chip-glow 0.5s ease-out;
}

.plan-chip__pulse {
  width: 8px;
  height: 8px;
  border-radius: 50%;
  background: #a78bfa;
  animation: pulse 1.5s ease-in-out infinite;
  flex-shrink: 0;
}

.plan-chip__text {
  flex: 1;
  font-size: 12px;
  color: var(--color-text-2, #a0a0b0);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.plan-chip__toggle {
  font-size: 10px;
  color: var(--color-text-3, #666);
  flex-shrink: 0;
}

/* ── Expanded timeline ── */
.plan-expanded {
  background: var(--color-bg-2, #1e1e2e);
  border: 1px solid var(--color-border, #3a3a4a);
  border-radius: 8px;
  padding: 8px 12px;
}

.plan-expanded__header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin-bottom: 6px;
}

.plan-expanded__title {
  font-size: 12px;
  font-weight: 600;
  color: var(--color-text-1, #e0e0e0);
}

.plan-expanded__toggle {
  font-size: 10px;
  color: var(--color-text-3, #666);
  cursor: pointer;
}

.plan-expanded__timeline {
  display: flex;
  flex-direction: column;
}

/* ── Timeline entry ── */
.plan-entry {
  display: flex;
  align-items: flex-start;
  position: relative;
  padding-left: 20px;
  min-height: 28px;
}

/* Vertical line segment */
.plan-entry__line {
  position: absolute;
  left: 7px;
  top: 16px;
  bottom: -12px;
  width: 0;
  border-left-width: 2px;
  border-left-style: solid;
}

.plan-entry:last-child .plan-entry__line {
  display: none;
}

.plan-entry__line--dashed {
  border-left-style: dashed;
}

.plan-entry__line--pulsing {
  animation: pulse-line 1.5s ease-in-out infinite;
}

/* Status node */
.plan-entry__node {
  position: absolute;
  left: 0;
  top: 4px;
  width: 16px;
  height: 16px;
  border-radius: 50%;
  border-width: 2px;
  border-style: solid;
  display: flex;
  align-items: center;
  justify-content: center;
  background: var(--color-bg-2, #1e1e2e);
  box-sizing: border-box;
}

.plan-entry--completed .plan-entry__node {
  background: #22c55e;
  border-color: #22c55e;
  animation: check-in 0.3s ease-out;
}

.plan-entry__check {
  font-size: 10px;
  color: #fff;
  line-height: 1;
}

.plan-entry__dot {
  width: 6px;
  height: 6px;
  border-radius: 50%;
  background: currentColor;
  animation: pulse 1.5s ease-in-out infinite;
}

.plan-entry--in_progress .plan-entry__node .plan-entry__dot {
  color: inherit;
}

.plan-entry__circle {
  width: 6px;
  height: 6px;
  border-radius: 50%;
  border: 1.5px solid currentColor;
}

/* Entry text */
.plan-entry__text {
  font-size: 12px;
  color: var(--color-text-2, #a0a0b0);
  line-height: 1.4;
  padding-top: 2px;
}

.plan-entry__text--done {
  text-decoration: line-through;
  color: var(--color-text-3, #666);
}

/* ── Animations ── */
@keyframes pulse {
  0%, 100% { opacity: 1; }
  50% { opacity: 0.4; }
}

@keyframes pulse-line {
  0%, 100% { opacity: 1; }
  50% { opacity: 0.4; }
}

@keyframes check-in {
  0% { transform: scale(0); opacity: 0; }
  50% { transform: scale(1.2); }
  100% { transform: scale(1); opacity: 1; }
}

@keyframes plan-chip-glow {
  0% { border-color: #a78bfa; box-shadow: 0 0 6px rgba(167, 139, 250, 0.5); }
  100% { border-color: var(--color-border, #3a3a4a); box-shadow: none; }
}
</style>
