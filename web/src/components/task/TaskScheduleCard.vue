<template>
  <div class="overview-card">
    <h3 class="card-title">
      <CalendarClock class="card-icon" :size="14" />
      {{ t('task.overview.schedule') }}
    </h3>
    <div class="overview-row">
      <span class="overview-value font-mono">{{ cronExpr }}</span>
      <span class="overview-subtext">{{ humanizeCron(cronExpr) }}</span>
    </div>
    <div class="overview-divider"></div>
    <div class="overview-row">
      <span class="overview-label">{{ t('chat.contentBlocks.repeat') }}</span>
      <span class="overview-value">{{ repeatLabel(repeatMode, maxRuns) }}</span>
    </div>
    <div v-if="runCount > 0" class="overview-row">
      <span class="overview-label">{{ t('chat.contentBlocks.statusExecutions', { count: runCount }) }}</span>
    </div>
    <div v-if="nextRunAt" class="overview-row highlight">
      <span class="overview-label">{{ t('chat.contentBlocks.nextRun') }}</span>
      <span class="overview-value">{{ formatDateTimeWithYear(nextRunAt) }}</span>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { CalendarClock } from 'lucide-vue-next'
import { humanizeCron, repeatLabel, formatDateTimeWithYear } from '@/utils/format'

const { t } = useI18n()

const props = defineProps<{
  task: Record<string, unknown>
}>()

const cronExpr = computed(() => (props.task.cronExpr as string) || '')
const repeatMode = computed(() => (props.task.repeatMode as string) || 'unlimited')
const maxRuns = computed(() => (props.task.maxRuns as number) || 0)
const runCount = computed(() => (props.task.runCount as number) || 0)
const nextRunAt = computed(() => props.task.nextRunAt as string | undefined)
</script>

<style scoped>
/* Card chrome is owned by the parent (.overview-card in TaskOverviewTab's
   non-scoped global styles) so both trigger variants stay identical. */
.overview-card {
  background: var(--bg-secondary, #f8f9fa);
  border: 1px solid var(--border-color, #e5e5e5);
  border-radius: var(--radius-sm, 6px);
  padding: 10px;
  display: flex;
  flex-direction: column;
  gap: 6px;
}
.card-title {
  display: flex;
  align-items: center;
  gap: 8px;
  font-size: 13px;
  font-weight: 600;
  color: var(--text-primary, #1a1a1a);
  margin: 0;
}
.card-icon { color: var(--text-muted, #999); }
.overview-divider {
  height: 1px;
  background: var(--border-color, #e5e5e5);
  margin: 2px 0;
}
.overview-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
}
.overview-row.highlight {
  background: rgba(0, 102, 204, 0.05);
  padding: 6px;
  border-radius: var(--radius-sm, 6px);
  margin: -2px -6px;
}
.overview-row.highlight .overview-value {
  color: var(--accent-color, #0066cc);
  font-weight: 500;
}
.overview-label {
  font-size: 12px;
  color: var(--text-secondary, #666);
  flex-shrink: 0;
}
.overview-value {
  font-size: 13px;
  color: var(--text-primary, #1a1a1a);
  text-align: right;
  word-break: break-word;
}
.overview-value.font-mono {
  font-family: var(--font-mono, 'SF Mono', 'Menlo', monospace);
  background: var(--bg-primary, #fff);
  padding: 2px 6px;
  border-radius: 4px;
  border: 1px solid var(--border-color, #e5e5e5);
  font-size: 12px;
}
.overview-subtext {
  font-size: 11px;
  color: var(--text-muted, #999);
}
</style>
