<template>
  <!-- Placeholder shown while a stats query is in flight. It mirrors the shape
       of the real panel (overview cards → table → chart) so switching a range
       reads as "the same page is refreshing" instead of the layout staying
       frozen on stale numbers with no signal that anything is happening.

       Purely decorative: `aria-hidden` keeps the blocks out of the
       accessibility tree, and the `role="status"` line announces the load. -->
  <div class="stats-skeleton">
    <span class="stats-skeleton-sr" role="status">{{ label }}</span>

    <section v-if="cards > 0" class="stats-skeleton-section" aria-hidden="true">
      <div class="skeleton-block stats-skeleton-title"></div>
      <div class="stats-skeleton-cards">
        <div v-for="i in cards" :key="i" class="stats-skeleton-card">
          <div class="skeleton-block stats-skeleton-label"></div>
          <div class="skeleton-block stats-skeleton-value"></div>
        </div>
      </div>
    </section>

    <section v-if="tableRows > 0" class="stats-skeleton-section" aria-hidden="true">
      <div class="skeleton-block stats-skeleton-title"></div>
      <div class="stats-skeleton-rows">
        <div
          v-for="(w, i) in rowWidths"
          :key="i"
          class="skeleton-block stats-skeleton-row"
          :style="{ width: w }"
        ></div>
      </div>
    </section>

    <section v-if="chart" class="stats-skeleton-section" aria-hidden="true">
      <div class="skeleton-block stats-skeleton-title"></div>
      <div class="skeleton-block stats-skeleton-chart"></div>
    </section>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'

const props = withDefaults(defineProps<{
  /** Number of overview value cards to preview (0 hides the block). */
  cards?: number
  /** Number of table rows to preview (0 hides the block). */
  tableRows?: number
  /** Whether to preview the chart card. */
  chart?: boolean
  /** Announced to assistive technology. */
  label?: string
}>(), {
  cards: 4,
  tableRows: 5,
  chart: true,
  label: '',
})

/**
 * Fixed widths (not randomized): a shimmer already says "loading", and a
 * shifting placeholder would read as content arriving and then changing. The
 * cycle is short enough to look varied across rows without being random.
 */
const ROW_WIDTHS = ['92%', '78%', '86%', '64%', '74%']
const rowWidths = computed(() =>
  Array.from({ length: props.tableRows }, (_, i) => ROW_WIDTHS[i % ROW_WIDTHS.length]))
</script>

<style scoped>
.stats-skeleton {
  display: flex;
  flex-direction: column;
  gap: var(--space-5);
  min-height: 0;
}

/* Visually hidden but still announced: the skeleton itself is aria-hidden, so
   without this a screen reader would hear nothing at all during the load. */
.stats-skeleton-sr {
  position: absolute;
  width: 1px;
  height: 1px;
  padding: 0;
  margin: -1px;
  overflow: hidden;
  clip-path: inset(50%);
  white-space: nowrap;
  border: 0;
}

/* Own card chrome rather than reusing `.stats-card-panel`: that class lives in
   three scoped blocks already (usage / cloc / git), and a fourth copy here
   would make the drift worse without fixing it. Renaming keeps this component
   self-contained; the geometry is deliberately identical so the placeholder
   and the real cards line up. */
.stats-skeleton-section {
  background: var(--bg-secondary, #f8f9fa);
  border: 1px solid var(--border-color, #e5e5e5);
  border-radius: var(--radius-sm, 6px);
  padding: var(--space-5);
  display: flex;
  flex-direction: column;
  gap: var(--space-4);
}

.stats-skeleton-title {
  width: calc(96 * var(--dp));
  height: calc(13 * var(--dp));
}

.stats-skeleton-cards {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(calc(120 * var(--dp)), 1fr));
  gap: var(--space-4);
  min-width: 0;
}

.stats-skeleton-card {
  background: var(--bg-elevated, var(--bg-primary));
  border: 1px solid var(--border-color);
  border-radius: var(--radius-sm, 6px);
  padding: var(--space-4) var(--space-5);
  display: flex;
  flex-direction: column;
  gap: var(--space-3);
  min-width: 0;
}

.stats-skeleton-label {
  width: 62%;
  height: calc(11 * var(--dp));
}

.stats-skeleton-value {
  width: 84%;
  height: calc(22 * var(--dp));
}

.stats-skeleton-rows {
  display: flex;
  flex-direction: column;
  gap: var(--space-4);
}

.stats-skeleton-row {
  height: calc(13 * var(--dp));
}

.stats-skeleton-chart {
  height: calc(200 * var(--dp));
  --skeleton-radius: var(--radius-sm, 6px);
}
</style>
