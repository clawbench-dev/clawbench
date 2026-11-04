<template>
  <BottomSheet
    :open="open"
    :title="t('file.table.menu.stats')"
    auto
    size="narrow"
    @close="emit('update:open', false)"
  >
    <div class="cs">
      <div class="cs-column" :title="column">{{ column }}</div>

      <div class="cs-section">{{ t('file.table.menu.statsBasic') }}</div>
      <dl class="cs-list">
        <div class="cs-row">
          <dt><Rows3 :size="14" class="cs-icon" />{{ t('file.table.menu.statRows') }}</dt>
          <dd>{{ formatCount(stats.rows) }}</dd>
        </div>
        <div class="cs-row">
          <dt><CircleSlash :size="14" class="cs-icon" />{{ t('file.table.menu.statEmpty') }}</dt>
          <dd>{{ formatCount(stats.empty) }}</dd>
        </div>
        <div class="cs-row">
          <dt><Layers :size="14" class="cs-icon" />{{ t('file.table.menu.statDistinct') }}</dt>
          <dd>{{ formatCount(stats.distinct) }}</dd>
        </div>
      </dl>

      <template v-if="numeric && stats.sum !== null">
        <div class="cs-section">{{ t('file.table.menu.statsNumeric') }}</div>
        <dl class="cs-list">
          <div class="cs-row">
            <dt><Sigma :size="14" class="cs-icon" />{{ t('file.table.menu.statSum') }}</dt>
            <dd>{{ formatStat(stats.sum) }}</dd>
          </div>
          <div class="cs-row">
            <dt><Divide :size="14" class="cs-icon" />{{ t('file.table.menu.statAvg') }}</dt>
            <dd>{{ formatStat(stats.avg) }}</dd>
          </div>
          <div class="cs-row">
            <dt><ArrowDownToLine :size="14" class="cs-icon" />{{ t('file.table.menu.statMin') }}</dt>
            <dd>{{ formatStat(stats.min) }}</dd>
          </div>
          <div class="cs-row">
            <dt><ArrowUpToLine :size="14" class="cs-icon" />{{ t('file.table.menu.statMax') }}</dt>
            <dd>{{ formatStat(stats.max) }}</dd>
          </div>
        </dl>
      </template>

      <p v-else class="cs-hint">{{ t('file.table.menu.statsTextOnly') }}</p>
    </div>
  </BottomSheet>
</template>

<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import {
  ArrowDownToLine,
  ArrowUpToLine,
  CircleSlash,
  Divide,
  Layers,
  Rows3,
  Sigma,
} from 'lucide-vue-next'
import BottomSheet from '@/components/common/BottomSheet.vue'
import type { ColumnStats } from '@/utils/csvTable'

defineProps<{
  open: boolean
  /** Column label shown above the numbers. */
  column: string
  numeric: boolean
  stats: ColumnStats
}>()

const emit = defineEmits<{ 'update:open': [value: boolean] }>()

const { t } = useI18n()

function formatCount(value: number): string {
  return value.toLocaleString()
}

/** Thousands separators for large numbers, two decimals for derived ones. */
function formatStat(value: number | null): string {
  if (value === null || !Number.isFinite(value)) return '—'
  const rounded = Math.round(value * 100) / 100
  return rounded.toLocaleString(undefined, { maximumFractionDigits: 2 })
}
</script>

<style scoped>
.cs {
  display: flex;
  flex-direction: column;
  /* .bs-body ships padding-free (see BottomSheet.vue) — the inset is ours. */
  padding: var(--space-6) var(--space-7);
}

.cs-column {
  overflow: hidden;
  color: var(--text-secondary);
  font-size: var(--font-size-sm);
  text-overflow: ellipsis;
  white-space: nowrap;
}

/* Group heading: an accent rule plus small caps reads as a divider between
   blocks. Without it the second block's first row looked like a continuation
   of the first (the report was "分组标题不突出"). */
.cs-section {
  display: flex;
  align-items: center;
  gap: var(--space-2);
  margin-top: var(--space-6);
  margin-bottom: var(--space-3);
  color: var(--accent-color);
  font-size: var(--font-size-xs);
  font-weight: var(--font-weight-semibold);
  letter-spacing: calc(0.4 * var(--dp));
}

.cs-section::before {
  content: '';
  width: calc(3 * var(--dp));
  height: calc(11 * var(--dp));
  border-radius: var(--radius-xs);
  background: var(--accent-color);
}

.cs-list {
  margin: 0;
  display: flex;
  flex-direction: column;
  border-top: 1px solid var(--border-color);
}

.cs-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: var(--space-6);
  padding: calc(6 * var(--dp)) 0;
  border-bottom: 1px solid var(--border-color);
  font-size: var(--font-size-md);
}

.cs-row dt {
  display: flex;
  align-items: center;
  gap: var(--space-3);
  color: var(--text-secondary);
}

.cs-icon {
  flex-shrink: 0;
  color: var(--text-muted);
}

.cs-row dd {
  margin: 0;
  color: var(--text-primary);
  font-variant-numeric: tabular-nums;
}

.cs-hint {
  margin: var(--space-3) 0 0;
  color: var(--text-muted);
  font-size: var(--font-size-sm);
}
</style>
