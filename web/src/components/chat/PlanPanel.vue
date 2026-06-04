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
  background: var(--bg-tertiary, #e9ecef);
  border: 1px solid var(--border-color, #dee2e6);
  cursor: pointer;
  transition: border-color 0.3s ease;
}

.plan-chip--updated {
  border-color: #8b5cf6;
  animation: plan-chip-glow 0.5s ease-out;
}

:root[data-theme="dark"] .plan-chip--updated {
  border-color: #a78bfa;
}

.plan-chip__pulse {
  width: 8px;
  height: 8px;
  border-radius: 50%;
  background: #8b5cf6;
  animation: pulse 1.5s ease-in-out infinite;
  flex-shrink: 0;
}

:root[data-theme="dark"] .plan-chip__pulse {
  background: #a78bfa;
}

.plan-chip__text {
  flex: 1;
  font-size: 12px;
  color: var(--text-secondary, #495057);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.plan-chip__toggle {
  font-size: 10px;
  color: var(--text-muted, #6c757d);
  flex-shrink: 0;
}

/* ── Expanded timeline ── */
.plan-expanded {
  background: var(--bg-secondary, #f8f9fa);
  border: 1px solid var(--border-color, #dee2e6);
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
  color: var(--text-primary, #212529);
}

.plan-expanded__toggle {
  font-size: 10px;
  color: var(--text-muted, #6c757d);
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
  background: var(--bg-secondary, #f8f9fa);
  box-sizing: border-box;
}

.plan-entry--completed .plan-entry__node {
  background: var(--color-green, #16a34a);
  border-color: var(--color-green, #16a34a);
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
  color: var(--text-secondary, #495057);
  line-height: 1.4;
  padding-top: 2px;
}

.plan-entry__text--done {
  text-decoration: line-through;
  color: var(--text-muted, #6c757d);
}

/* ── Priority color overrides (light theme) ── */
.plan-entry--high > .plan-entry__line { border-color: #ef4444; }
.plan-entry--medium > .plan-entry__line { border-color: #f97316; }
.plan-entry--low > .plan-entry__line { border-color: #9ca3af; }

.plan-entry--high > .plan-entry__node { border-color: #ef4444; }
.plan-entry--medium > .plan-entry__node { border-color: #f97316; }
.plan-entry--low > .plan-entry__node { border-color: #9ca3af; }

/* ── Priority colors: dark theme adjustments ── */
:root[data-theme="dark"] .plan-entry--high > .plan-entry__line { border-color: #f87171; }
:root[data-theme="dark"] .plan-entry--medium > .plan-entry__line { border-color: #fb923c; }
:root[data-theme="dark"] .plan-entry--low > .plan-entry__line { border-color: #9ca3af; }

:root[data-theme="dark"] .plan-entry--high > .plan-entry__node { border-color: #f87171; }
:root[data-theme="dark"] .plan-entry--medium > .plan-entry__node { border-color: #fb923c; }
:root[data-theme="dark"] .plan-entry--low > .plan-entry__node { border-color: #9ca3af; }

:root[data-theme="dark"] .plan-entry--completed > .plan-entry__node {
  background: var(--color-green, #3fb950);
  border-color: var(--color-green, #3fb950);
}

:root[data-theme="dark"] .plan-entry__check {
  color: #fff;
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
  0% { border-color: #8b5cf6; box-shadow: 0 0 6px rgba(139, 92, 246, 0.5); }
  100% { border-color: var(--border-color, #dee2e6); box-shadow: none; }
}

:root[data-theme="dark"] .plan-chip-glow {
  0% { border-color: #a78bfa; box-shadow: 0 0 6px rgba(167, 139, 250, 0.5); }
  100% { border-color: var(--border-color, #30363d); box-shadow: none; }
}
</style>
