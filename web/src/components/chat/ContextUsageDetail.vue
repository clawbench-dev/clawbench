<template>
  <div class="usage-detail">
    <div class="usage-popup-bar">
      <div class="usage-popup-bar-track">
        <div class="usage-popup-bar-fill" :style="{ width: Math.min(pct, 100) + '%', background: color }"></div>
      </div>
      <span class="usage-popup-pct" :style="{ color }">{{ pct }}%</span>
    </div>
    <div class="usage-popup-row">
      <span class="usage-popup-label">{{ t('chat.sessionInfo.used') }}</span>
      <span class="usage-popup-value">{{ (usage?.used ?? 0).toLocaleString() }}</span>
    </div>
    <div class="usage-popup-row">
      <span class="usage-popup-label">{{ t('chat.sessionInfo.size') }}</span>
      <span class="usage-popup-value">{{ (usage?.size ?? 0).toLocaleString() }}</span>
    </div>
    <div class="usage-popup-row">
      <span class="usage-popup-label">{{ t('chat.sessionInfo.remaining') }}</span>
      <span class="usage-popup-value">{{ remaining.toLocaleString() }}</span>
    </div>
    <!-- Token detail section: every token/cost row grouped under one header.
         used/size/remaining (matching the progress bar above) stay outside.
         cacheHitTokens is deliberately rendered even though it duplicates
         cachedReadTokens ("缓存读") — both labels are user-requested. -->
    <div v-if="showTokenDetail" class="usage-popup-section-title">{{ t('chat.sessionInfo.tokenDetail') }}</div>
    <div v-if="n('inputTokens') > 0" class="usage-popup-row">
      <span class="usage-popup-label">{{ t('chat.sessionInfo.inputTokens') }}</span>
      <span class="usage-popup-value">{{ n('inputTokens').toLocaleString() }}</span>
    </div>
    <div v-if="n('outputTokens') > 0" class="usage-popup-row">
      <span class="usage-popup-label">{{ t('chat.sessionInfo.outputTokens') }}</span>
      <span class="usage-popup-value">{{ n('outputTokens').toLocaleString() }}</span>
    </div>
    <div v-if="n('totalTokens') > 0" class="usage-popup-row">
      <span class="usage-popup-label">{{ t('chat.sessionInfo.totalTokens') }}</span>
      <span class="usage-popup-value">{{ n('totalTokens').toLocaleString() }}</span>
    </div>
    <div v-if="n('cachedReadTokens') > 0" class="usage-popup-row">
      <span class="usage-popup-label">{{ t('chat.sessionInfo.cachedReadTokens') }}</span>
      <span class="usage-popup-value">{{ n('cachedReadTokens').toLocaleString() }}</span>
    </div>
    <div v-if="n('cacheHitTokens') > 0" class="usage-popup-row">
      <span class="usage-popup-label">{{ t('chat.sessionInfo.cacheHitTokens') }}</span>
      <span class="usage-popup-value">{{ n('cacheHitTokens').toLocaleString() }}</span>
    </div>
    <div v-if="hitRate !== null" class="usage-popup-row">
      <span class="usage-popup-label">{{ t('chat.sessionInfo.cacheHitRate') }}</span>
      <span class="usage-popup-value">{{ hitRate.toFixed(1) }}%</span>
    </div>
    <div v-if="n('cachedWriteTokens') > 0" class="usage-popup-row">
      <span class="usage-popup-label">{{ t('chat.sessionInfo.cachedWriteTokens') }}</span>
      <span class="usage-popup-value">{{ n('cachedWriteTokens').toLocaleString() }}</span>
    </div>
    <div v-if="n('cacheCreationTokens') > 0" class="usage-popup-row">
      <span class="usage-popup-label">{{ t('chat.sessionInfo.cacheCreationTokens') }}</span>
      <span class="usage-popup-value">{{ n('cacheCreationTokens').toLocaleString() }}</span>
    </div>
    <div v-if="n('cacheMissTokens') > 0" class="usage-popup-row">
      <span class="usage-popup-label">{{ t('chat.sessionInfo.cacheMissTokens') }}</span>
      <span class="usage-popup-value">{{ n('cacheMissTokens').toLocaleString() }}</span>
    </div>
    <div v-if="n('thoughtTokens') > 0" class="usage-popup-row">
      <span class="usage-popup-label">{{ t('chat.sessionInfo.thoughtTokens') }}</span>
      <span class="usage-popup-value">{{ n('thoughtTokens').toLocaleString() }}</span>
    </div>
    <div v-if="n('credit') > 0" class="usage-popup-row">
      <span class="usage-popup-label">{{ t('chat.sessionInfo.credit') }}</span>
      <span class="usage-popup-value">{{ n('credit').toFixed(4) }}</span>
    </div>
    <div v-if="n('cost') > 0" class="usage-popup-row">
      <span class="usage-popup-label">{{ t('chat.sessionInfo.contextCost') }}</span>
      <span class="usage-popup-value">{{ costDisplay }}</span>
    </div>
    <!-- Context breakdown section (CodeBuddy usageByCategory) -->
    <template v-if="categoryRows.length">
      <div class="usage-popup-section-title">{{ t('chat.sessionInfo.categoryBreakdown') }}</div>
      <div v-for="row in categoryRows" :key="row.key" class="usage-popup-row">
        <span class="usage-popup-label">{{ row.label }}</span>
        <span class="usage-popup-value">{{ row.value.toLocaleString() }}</span>
      </div>
    </template>
    <!-- Compaction is a single-chat action: it sends "/compact" into the current
         session. A group member has no command channel, so its panel passes
         showCompact=false and the row disappears. -->
    <div v-if="showCompact" class="usage-popup-compact">
      <button class="usage-popup-compact-btn" :disabled="compacting" @click.stop="emit('compact')" :title="t('chat.sessionInfo.compact')" :aria-label="t('chat.sessionInfo.compact')">
        <LoadingIndicator v-if="compacting" size="sm" inline :label="t('chat.sessionInfo.compacting')" />
        <template v-else>
          <Minimize2 :size="13" />
          {{ t('chat.sessionInfo.compact') }}
        </template>
      </button>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { Minimize2 } from 'lucide-vue-next'
import LoadingIndicator from '@/components/common/LoadingIndicator.vue'
import {
  usagePct,
  usageColor,
  cacheHitRate,
  hasTokenDetail,
  formatUsageCost,
  usageCategoryRows,
  type UsageSnapshot,
} from '@/utils/usageDisplay'

/**
 * ContextUsageDetail renders the context-window panel body.
 *
 * It is deliberately presentation-only: no fetching, no session identity, no
 * drawer state. Single chat mounts it inside its PopupMenu; the group member
 * sheet mounts it inside a BottomSheet. Both therefore show the same rows in
 * the same order with the same thresholds — the whole point of extracting it
 * (a second copy is what drifts).
 */
const props = defineProps<{
  /** Undefined = the producer never reported usage (e.g. a CLI-transport member). */
  usage?: UsageSnapshot | null
  /** Show the "compact context" action. Single chat only. */
  showCompact?: boolean
  /**
   * A compaction is in flight. Renders a spinner and disables the button: the
   * agent round-trip takes tens of seconds, and without this the panel looks
   * completely inert after the click (the request DOES run — it just has no
   * visible progress).
   */
  compacting?: boolean
}>()

const emit = defineEmits<{ (e: 'compact'): void }>()

const { t } = useI18n()

const pct = computed(() => usagePct(props.usage))
const color = computed(() => usageColor(pct.value))
const remaining = computed(() => Math.max((props.usage?.size ?? 0) - (props.usage?.used ?? 0), 0))
const hitRate = computed(() => cacheHitRate(props.usage))
const showTokenDetail = computed(() => hasTokenDetail(props.usage))
const costDisplay = computed(() => formatUsageCost(props.usage?.cost ?? 0, props.usage?.currency))
const categoryRows = computed(() => usageCategoryRows(props.usage, t))

/** Numeric accessor that keeps the template free of `?? 0` repetition. */
function n(key: keyof UsageSnapshot): number {
  const v = props.usage?.[key]
  return typeof v === 'number' ? v : 0
}
</script>

<style scoped>
.usage-detail {
  min-width: 180px;
}

.usage-popup-bar {
  display: flex;
  align-items: center;
  gap: var(--space-4);
  margin-bottom: var(--space-5);
}

.usage-popup-bar-track {
  flex: 1;
  height: 8px;
  border-radius: var(--radius-xs);
  background: color-mix(in srgb, var(--text-primary) 15%, transparent);
  overflow: hidden;
}

.usage-popup-bar-fill {
  height: 100%;
  border-radius: var(--radius-xs);
  transition: width 0.3s ease, background 0.3s ease;
}

.usage-popup-pct {
  font-size: var(--font-size-lg);
  font-weight: var(--font-weight-bold);
  flex-shrink: 0;
  min-width: 36px;
  text-align: right;
}

.usage-popup-section-title {
  font-size: var(--font-size-xs);
  font-weight: var(--font-weight-semibold);
  color: var(--text-secondary);
  margin-top: var(--space-4);
  margin-bottom: var(--space-2);
  padding-top: var(--space-3);
  border-top: 1px solid var(--border-color);
}

.usage-popup-row {
  display: flex;
  justify-content: space-between;
  align-items: center;
  gap: var(--space-4);
  padding: 3px 0;
  font-size: var(--font-size-sm);
}

.usage-popup-label {
  color: var(--text-secondary, #6c757d);
}

.usage-popup-value {
  color: var(--text-primary);
  font-weight: var(--font-weight-medium);
  font-variant-numeric: tabular-nums;
}

.usage-popup-compact {
  margin-top: var(--space-5);
  padding-top: var(--space-4);
  border-top: 1px solid color-mix(in srgb, var(--text-primary) 12%, transparent);
  display: flex;
  justify-content: center;
}

.usage-popup-compact-btn {
  display: inline-flex;
  align-items: center;
  gap: 5px;
  background: none;
  border: 1px solid color-mix(in srgb, var(--text-primary) 18%, transparent);
  border-radius: var(--radius-sm);
  cursor: pointer;
  padding: 5px var(--space-6);
  color: var(--text-secondary, #6c757d);
  font-size: var(--font-size-sm);
  line-height: var(--line-height-snug);
  transition: color var(--duration-base), border-color var(--duration-base);
  user-select: none;
  -webkit-user-select: none;
}

.usage-popup-compact-btn:active:not(:disabled) {
  color: var(--accent-color, #0066cc);
  border-color: var(--accent-color, #0066cc);
}

@media (hover: hover) {
  .usage-popup-compact-btn:hover:not(:disabled) {
    color: var(--accent-color, #0066cc);
    border-color: var(--accent-color, #0066cc);
  }
}
</style>
