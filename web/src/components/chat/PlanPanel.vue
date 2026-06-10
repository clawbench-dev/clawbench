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
            }">
          </div>
          <!-- Status node -->
          <div class="plan-entry__node">
            <span v-if="entry.status === 'completed'" class="plan-entry__check">✓</span>
            <span v-else-if="entry.status === 'in_progress'" class="plan-entry__dot"></span>
            <span v-else class="plan-entry__circle"></span>
          </div>
          <!-- Entry content -->
          <span class="plan-entry__text" :class="{ 'plan-entry__text--done': entry.status === 'completed' }">{{ entry.content }}</span>
          <!-- Priority tag -->
          <span class="plan-entry__priority" :class="'plan-entry__priority--' + entry.priority">{{ priorityLabel(entry.priority) }}</span>
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

function priorityLabel(priority: string): string {
  switch (priority) {
    case 'high': return t('chat.plan.priorityHigh')
    case 'medium': return t('chat.plan.priorityMedium')
    case 'low': return t('chat.plan.priorityLow')
    default: return ''
  }
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
  width: auto;
  margin: 0 10px 8px;
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
  gap: 6px;
}

/* Vertical line segment */
.plan-entry__line {
  position: absolute;
  left: 7px;
  top: 16px;
  bottom: -12px;
  width: 0;
  border-left: 2px solid var(--border-color, #dee2e6);
}

.plan-entry:last-child .plan-entry__line {
  display: none;
}

.plan-entry__line--dashed {
  border-left-style: dashed;
}

.plan-entry__line--pulsing {
  border-left-style: solid;
  border-left-color: var(--color-purple, #8b5cf6);
  animation: pulse-line 1.5s ease-in-out infinite;
}

:root[data-theme="dark"] .plan-entry__line--pulsing {
  border-left-color: #a78bfa;
}

/* Status node — neutral, no priority color */
.plan-entry__node {
  position: absolute;
  left: 0;
  top: 4px;
  width: 16px;
  height: 16px;
  border-radius: 50%;
  border: 2px solid var(--border-color, #dee2e6);
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

:root[data-theme="dark"] .plan-entry--completed .plan-entry__node {
  background: var(--color-green, #3fb950);
  border-color: var(--color-green, #3fb950);
}

.plan-entry--in_progress .plan-entry__node {
  border-color: var(--color-purple, #8b5cf6);
}

:root[data-theme="dark"] .plan-entry--in_progress .plan-entry__node {
  border-color: #a78bfa;
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
  background: var(--color-purple, #8b5cf6);
  animation: pulse 1.5s ease-in-out infinite;
}

:root[data-theme="dark"] .plan-entry__dot {
  background: #a78bfa;
}

.plan-entry__circle {
  width: 6px;
  height: 6px;
  border-radius: 50%;
  border: 1.5px solid var(--text-muted, #6c757d);
}

/* Entry text */
.plan-entry__text {
  flex: 1;
  font-size: 12px;
  color: var(--text-secondary, #495057);
  line-height: 1.4;
  padding-top: 2px;
  min-width: 0;
}

.plan-entry__text--done {
  text-decoration: line-through;
  color: var(--text-muted, #6c757d);
}

/* ── Priority tag ── */
.plan-entry__priority {
  flex-shrink: 0;
  font-size: 10px;
  font-weight: 500;
  padding: 1px 6px;
  border-radius: 8px;
  margin-top: 2px;
  line-height: 1.4;
  white-space: nowrap;
}

.plan-entry__priority--high {
  color: #dc2626;
  background: rgba(239, 68, 68, 0.12);
}

.plan-entry__priority--medium {
  color: #d97706;
  background: rgba(249, 115, 22, 0.12);
}

.plan-entry__priority--low {
  color: var(--text-muted, #6c757d);
  background: var(--bg-tertiary, #e9ecef);
}

:root[data-theme="dark"] .plan-entry__priority--high {
  color: #fca5a5;
  background: rgba(239, 68, 68, 0.18);
}

:root[data-theme="dark"] .plan-entry__priority--medium {
  color: #fdba74;
  background: rgba(249, 115, 22, 0.18);
}

:root[data-theme="dark"] .plan-entry__priority--low {
  color: #9ca3af;
  background: rgba(156, 163, 175, 0.14);
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
