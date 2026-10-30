<template>
  <!-- First-load placeholder for a dock panel. Replaces the lone spinner that
       used to sit in the middle of an otherwise empty panel: a ring says
       "waiting" but nothing about *what* is coming, and the layout collapses to
       a single centred glyph. The blocks below preview the shape of the real
       content, so the panel keeps its geometry while the data is in flight.

       Purely decorative — every visible block is `aria-hidden`, and the one
       `role="status"` line announces the load (otherwise a screen reader
       walking the panel would hear nothing at all). -->
  <div class="panel-skeleton" :class="`panel-skeleton--${variant}`">
    <span class="panel-skeleton-sr" role="status">{{ label }}</span>

    <!-- ── list: rows of [leading] text + trailing meta ── -->
    <template v-if="variant === 'list'">
      <div
        v-for="(w, i) in listRows"
        :key="i"
        class="panel-skeleton-row"
        aria-hidden="true"
      >
        <div v-if="leading" class="skeleton-block panel-skeleton-leading"></div>
        <div class="panel-skeleton-row-body">
          <div class="skeleton-block panel-skeleton-line" :style="{ width: w.primary }"></div>
          <div
            v-if="w.secondary"
            class="skeleton-block panel-skeleton-line panel-skeleton-line--sub"
            :style="{ width: w.secondary }"
          ></div>
        </div>
        <div v-if="meta" class="skeleton-block panel-skeleton-meta"></div>
      </div>
    </template>

    <!-- ── grid: square tiles with a caption, matching the file grid ── -->
    <div v-else-if="variant === 'grid'" class="panel-skeleton-grid" aria-hidden="true">
      <div v-for="i in rows" :key="i" class="panel-skeleton-tile">
        <div class="skeleton-block panel-skeleton-thumb"></div>
        <div class="skeleton-block panel-skeleton-caption"></div>
      </div>
    </div>

    <!-- ── text: paragraph lines, for previews / diffs / outlines ── -->
    <div v-else class="panel-skeleton-text" aria-hidden="true">
      <div
        v-for="(w, i) in textRows"
        :key="i"
        class="skeleton-block panel-skeleton-line"
        :style="{ width: w }"
      ></div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'

const props = withDefaults(defineProps<{
  /**
   * Shape of the panel being previewed.
   *  - `list`  rows of text (optionally with a leading icon and trailing meta)
   *  - `grid`  square tiles with captions
   *  - `text`  paragraph lines
   */
  variant?: 'list' | 'grid' | 'text'
  /** How many rows / tiles / lines to draw. */
  rows?: number
  /** `list` only: reserve a leading icon block (files, avatars, agent icons). */
  leading?: boolean
  /** `list` only: reserve a trailing meta block (sizes, timestamps). */
  meta?: boolean
  /** Announced to assistive technology. */
  label?: string
}>(), {
  variant: 'list',
  rows: 6,
  leading: true,
  meta: true,
  label: '',
})

/**
 * Fixed widths, cycled — never randomized. The shimmer already says "loading";
 * a shifting placeholder reads as content arriving and then changing. The
 * cycle is short enough that adjacent rows still look varied.
 */
const PRIMARY = ['76%', '62%', '84%', '58%', '70%', '66%']
const SECONDARY = ['42%', '34%', '48%', '30%', '38%', '36%']
const TEXT = ['92%', '78%', '86%', '64%', '88%', '52%', '74%', '40%']

const listRows = computed(() =>
  Array.from({ length: props.rows }, (_, i) => ({
    primary: PRIMARY[i % PRIMARY.length],
    secondary: SECONDARY[i % SECONDARY.length],
  })),
)

const textRows = computed(() =>
  Array.from({ length: props.rows }, (_, i) => TEXT[i % TEXT.length]),
)
</script>

<style scoped>
/* ── Per-surface inset (`--panel-skeleton-inset`) ──
   The one thing every caller has to get right is the HORIZONTAL inset: the
   placeholder must sit exactly where the real rows' text sits, or the panel
   jumps sideways the moment the data lands. There is no single correct number —
   row padding ranges from 0 (full-bleed rows) to 18dp (a card row inside a
   padded list) — so it is a variable with the most common value as its
   fallback, and each surface overrides it next to the rule that declares the
   real row it mirrors:

     .projects-skeleton { --panel-skeleton-inset: calc(16 * var(--dp)); }

   The variable is read on the three shape rules below (not on the root), so a
   caller may set it on the skeleton itself or on any ancestor. Defaults:
   rows 8dp (the modal row padding), tiles 8dp (matches .file-grid's gutter),
   paragraph bodies 0 (those are dropped into a container that already pads —
   `.forge-detail-body`, the dialogs, the share pages). */

/* `width: 100%` is load-bearing, not cosmetic. The root is placed inside the
   panels' legacy loading wrappers, which are `display: flex` with the DEFAULT
   row direction: as a flex item this element then sizes to its content, and
   every child width is a percentage of that content — which is zero. The whole
   placeholder collapsed to 0px wide (a blank panel) in the forge detail and
   shrunk to the fixed-width leading/meta blocks in every list. The `--skeleton`
   modifier classes set `align-items: stretch`, but that is the CROSS axis of a
   row container (height), so it never gave the skeleton any width.
   `min-width: 0` stays so a long row inside a narrow pane can still shrink. */
.panel-skeleton {
  display: flex;
  flex-direction: column;
  width: 100%;
  min-width: 0;
}

/* Visually hidden but still announced: the skeleton itself is aria-hidden, so
   without this a screen reader would hear nothing during the load. */
.panel-skeleton-sr {
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

/* ── list ─────────────────────────────────────────────────────────────── */
.panel-skeleton-row {
  display: flex;
  align-items: center;
  gap: var(--space-4);
  min-height: calc(44 * var(--dp));
  padding: var(--space-3) var(--panel-skeleton-inset, var(--space-4));
  min-width: 0;
}

.panel-skeleton-leading {
  width: calc(24 * var(--dp));
  height: calc(24 * var(--dp));
  flex-shrink: 0;
}

.panel-skeleton-row-body {
  display: flex;
  flex-direction: column;
  gap: var(--space-2);
  flex: 1;
  min-width: 0;
}

.panel-skeleton-line {
  height: calc(13 * var(--dp));
}

.panel-skeleton-line--sub {
  height: calc(10 * var(--dp));
  opacity: 0.6;
}

.panel-skeleton-meta {
  width: calc(40 * var(--dp));
  height: calc(10 * var(--dp));
  flex-shrink: 0;
}

/* ── grid ─────────────────────────────────────────────────────────────── */
/* Mirrors .file-grid so the tiles land where the real ones will. */
.panel-skeleton-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(calc(90 * var(--dp)), 1fr));
  gap: var(--space-4);
  padding: var(--space-4) var(--panel-skeleton-inset, var(--space-4));
  align-content: start;
}

.panel-skeleton-tile {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: var(--space-3);
  min-width: 0;
}

.panel-skeleton-thumb {
  width: 100%;
  aspect-ratio: 1;
  --skeleton-radius: var(--radius-sm);
}

.panel-skeleton-caption {
  width: 70%;
  height: calc(11 * var(--dp));
}

/* ── text ─────────────────────────────────────────────────────────────── */
.panel-skeleton-text {
  display: flex;
  flex-direction: column;
  gap: var(--space-4);
  padding: var(--space-4) var(--panel-skeleton-inset, 0);
  min-width: 0;
}
</style>
