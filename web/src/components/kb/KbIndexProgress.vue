<template>
  <div v-if="visible" class="kb-progress" :class="{ 'kb-progress--compact': compact }">
    <!-- No embedder: say so instead of showing a vector bar that will never
         move. The knowledge base still works, just with keyword search only. -->
    <div v-if="!vectorEnabled && hasDocuments" class="kb-progress__note">
      {{ t('kb.indexOffHint') }}
    </div>

    <!-- Phase 1: extract + chunk. `pending_docs` covers both pending and
         extracting, so "done" is everything that left the queue. -->
    <div v-if="showDocs" class="kb-progress__row" data-kb-progress="docs">
      <div class="kb-progress__line">
        <span class="kb-progress__label">
          {{ t('kb.indexDocsProgress', { done: docsDone, total: stats!.documents }) }}
        </span>
        <span v-if="stats!.failed_docs > 0" class="kb-progress__failed">
          {{ t('kb.indexFailedCount', { count: stats!.failed_docs }) }}
        </span>
      </div>
      <div class="kb-progress__track">
        <div
          class="kb-progress__bar"
          :class="{ 'is-active': docsActive }"
          :style="{ width: percent(docsDone, stats!.documents) }"
        />
      </div>
    </div>

    <!-- Phase 2: embedding. Runs after extraction, on the chunk table, so it
         has its own denominator (passages, not files). -->
    <div v-if="showVectors" class="kb-progress__row" data-kb-progress="vectors">
      <div class="kb-progress__line">
        <span class="kb-progress__label">
          {{ t('kb.indexVectorsProgress', { done: stats!.embedded_chunks, total: stats!.chunks }) }}
        </span>
      </div>
      <div class="kb-progress__track">
        <div
          class="kb-progress__bar"
          :class="{ 'is-active': vectorsActive }"
          :style="{ width: percent(stats!.embedded_chunks, stats!.chunks) }"
        />
      </div>
    </div>

    <div v-if="showReadyNote" class="kb-progress__note">{{ t('kb.indexReady') }}</div>
  </div>
</template>

<script setup lang="ts">
/**
 * Two-phase knowledge-base index progress: extraction, then embedding.
 *
 * One component for both places that show it — the settings card and the dock
 * panel — so the two can never disagree about what "done" means. It is purely
 * presentational; whoever renders it owns the polling and passes fresh stats.
 */
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { KBStats } from '@/composables/useKnowledgeBase'

const props = defineProps<{
  stats: KBStats | null
  vectorEnabled: boolean
  /** Panel placement: only show rows that still have work, and hide entirely
   *  when idle. The settings card keeps the full picture. */
  compact?: boolean
}>()

const { t } = useI18n()

const hasDocuments = computed(() => (props.stats?.documents ?? 0) > 0)
const docsDone = computed(() => Math.max(0, (props.stats?.documents ?? 0) - (props.stats?.pending_docs ?? 0)))
const docsActive = computed(() => (props.stats?.pending_docs ?? 0) > 0)
const vectorsActive = computed(
  () => props.vectorEnabled && (props.stats?.chunks ?? 0) > (props.stats?.embedded_chunks ?? 0),
)
const active = computed(() => docsActive.value || vectorsActive.value)

const showDocs = computed(() => hasDocuments.value && (!props.compact || docsActive.value))
const showVectors = computed(
  () => props.vectorEnabled && (props.stats?.chunks ?? 0) > 0 && (!props.compact || vectorsActive.value),
)
const showReadyNote = computed(() => !props.compact && hasDocuments.value && props.vectorEnabled && !active.value)

/** Hidden when there is nothing to say: no documents, or a compact placement
 *  that is idle and has an embedder configured. The "no embedder" note is
 *  always worth showing once there are documents. */
const visible = computed(() => {
  if (!hasDocuments.value) return false
  if (!props.vectorEnabled) return true
  return !props.compact || active.value
})

function percent(done: number, total: number): string {
  if (total <= 0) return '0%'
  return `${Math.min((done / total) * 100, 100)}%`
}
</script>

<style scoped>
.kb-progress {
  display: flex;
  flex-direction: column;
  gap: var(--space-3);
}

.kb-progress__row {
  display: flex;
  flex-direction: column;
  gap: var(--space-2);
}

.kb-progress__line {
  display: flex;
  align-items: baseline;
  gap: var(--space-3);
  font-size: var(--font-size-sm);
  color: var(--text-secondary);
}

.kb-progress__label {
  font-variant-numeric: tabular-nums;
}

.kb-progress__failed {
  color: var(--color-red);
  font-variant-numeric: tabular-nums;
}

.kb-progress__track {
  height: calc(3 * var(--dp));
  border-radius: var(--radius-xs);
  background: var(--bg-tertiary);
  overflow: hidden;
}

.kb-progress__bar {
  height: 100%;
  border-radius: var(--radius-xs);
  background: var(--accent-color);
  transition: width 0.5s ease;
}

/* Stripes mean "moving". A stalled queue keeps its width but stops claiming
   progress — same rule as the settings progress rows. */
.kb-progress__bar.is-active {
  background-image: linear-gradient(
    -45deg,
    rgba(255, 255, 255, 0.15) 25%,
    transparent 25%,
    transparent 50%,
    rgba(255, 255, 255, 0.15) 50%,
    rgba(255, 255, 255, 0.15) 75%,
    transparent 75%
  );
  background-size: calc(12 * var(--dp)) calc(12 * var(--dp));
  animation: kb-progress-stripe 0.8s linear infinite;
}

@keyframes kb-progress-stripe {
  0% { background-position: 0 0; }
  100% { background-position: 12px 0; }
}

.kb-progress__note {
  font-size: var(--font-size-sm);
  line-height: 1.5;
  color: var(--text-muted);
}

.kb-progress--compact {
  padding: 0 var(--space-5) var(--space-3);
}
</style>
