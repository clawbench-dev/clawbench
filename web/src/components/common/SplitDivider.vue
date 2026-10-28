<template>
  <div
    ref="dividerRef"
    class="split-view__divider resize-divider"
    :class="[`split-view__divider--${orientation}`, { 'resize-divider--expanded': dragging }]"
    role="separator"
    :aria-orientation="ariaOrientation"
    :aria-valuenow="ariaValueNow"
    :aria-valuemin="ariaValueMin"
    :aria-valuemax="ariaValueMax"
    :title="resolvedTitle"
    @pointerdown="onDividerPointerDown"
  >
    <div class="resize-divider__line" />
    <!-- Collapse handles: one on each side of the line, so either pane can be
         hidden from the same boundary. They live INSIDE the divider so hovering
         the line reveals them and so that moving the pointer onto a handle
         keeps the divider `:hover` (a hovered descendant keeps its ancestors
         hovered) — an out-of-tree overlay would flicker the moment the pointer
         left the 1px line.

         Inline SVG rather than lucide components: this file sits under
         SplitView, which several tests import with a narrow `lucide-vue-next`
         mock that only lists a handful of icons. -->
    <button
      v-if="collapseLeft"
      type="button"
      class="split-view__collapse-btn split-view__collapse-btn--first"
      data-collapse="left"
      :title="resolvedCollapseLeftTitle"
      :aria-label="resolvedCollapseLeftTitle"
      @pointerdown.stop
      @click.stop="emit('collapse', 'left')"
    >
      <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
        <path :d="isVertical ? 'm18 15-6-6-6 6' : 'm15 18-6-6 6-6'" />
      </svg>
    </button>
    <button
      v-if="collapseRight"
      type="button"
      class="split-view__collapse-btn split-view__collapse-btn--second"
      data-collapse="right"
      :title="resolvedCollapseRightTitle"
      :aria-label="resolvedCollapseRightTitle"
      @pointerdown.stop
      @click.stop="emit('collapse', 'right')"
    >
      <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
        <path :d="isVertical ? 'm6 9 6 6 6-6' : 'm9 18 6-6-6-6'" />
      </svg>
    </button>
  </div>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'

const { t } = useI18n()

const props = withDefaults(defineProps<{
  /** 'horizontal' = left/right panes (vertical divider line, drag on X).
   *  'vertical'   = top/bottom panes (horizontal divider line, drag on Y). */
  orientation?: 'horizontal' | 'vertical'
  title?: string
  ariaValueNow?: number
  ariaValueMin?: number
  ariaValueMax?: number
  /** Show a collapse handle on the FIRST-pane side of the line. Omitted ⇒ no
   *  handle, which keeps existing consumers (session sidebar, TOC dock, file
   *  manager) unchanged. */
  collapseLeft?: boolean
  /** Show a collapse handle on the SECOND-pane side of the line. */
  collapseRight?: boolean
  /** Tooltips for the two handles; each falls back to a generic i18n label. */
  collapseLeftTitle?: string
  collapseRightTitle?: string
}>(), {
  orientation: 'horizontal',
})

/**
 * The aria title. Resolved in a computed rather than a prop default so it
 * follows the live locale: `withDefaults` is evaluated once at setup, before
 * any reactive locale change, and would freeze the string in the boot language.
 */
const resolvedTitle = computed(() => props.title ?? t('common.dragToResize'))
const resolvedCollapseLeftTitle = computed(() => props.collapseLeftTitle ?? t('common.close'))
const resolvedCollapseRightTitle = computed(() => props.collapseRightTitle ?? t('common.close'))

const isVertical = computed(() => props.orientation === 'vertical')

/** A vertical divider separates left/right panes; a horizontal one separates
 *  top/bottom. ARIA describes the separator line itself. */
const ariaOrientation = computed(() => (isVertical.value ? 'horizontal' : 'vertical'))

const emit = defineEmits<{
  (e: 'dragstart'): void
  (e: 'dragmove', clientPos: number): void
  (e: 'dragend'): void
  (e: 'collapse', side: 'left' | 'right'): void
}>()

const dividerRef = ref<HTMLDivElement | null>(null)
/**
 * Drives the expanded highlight while dragging. `:active` alone is not enough
 * on touch: once `setPointerCapture` takes over the pointer, the browser drops
 * `:active`, so a fast swipe showed no highlight at all (only a held press did).
 * This mirrors the pointer lifecycle instead of relying on the pseudo-class.
 */
const dragging = ref(false)
let dragActive = false

function onDividerPointerDown(e: PointerEvent) {
  if (e.button !== 0) return
  dragActive = true
  dragging.value = true
  dividerRef.value?.setPointerCapture?.(e.pointerId)
  document.body.classList.add('split-view-dragging')
  document.body.classList.add(isVertical.value ? 'split-view-dragging--vertical' : 'split-view-dragging--horizontal')
  emit('dragstart')
}

function onPointerMove(e: PointerEvent) {
  if (!dragActive) return
  // Vertical split reads the pointer's Y; horizontal reads X.
  emit('dragmove', isVertical.value ? e.clientY : e.clientX)
}

function onPointerUp(e: PointerEvent) {
  if (!dragActive) return
  dragActive = false
  dragging.value = false
  dividerRef.value?.releasePointerCapture?.(e.pointerId)
  document.body.classList.remove('split-view-dragging')
  document.body.classList.remove('split-view-dragging--vertical', 'split-view-dragging--horizontal')
  emit('dragend')
}

onMounted(() => {
  window.addEventListener('pointermove', onPointerMove)
  window.addEventListener('pointerup', onPointerUp)
  window.addEventListener('pointercancel', onPointerUp)
})

onBeforeUnmount(() => {
  window.removeEventListener('pointermove', onPointerMove)
  window.removeEventListener('pointerup', onPointerUp)
  window.removeEventListener('pointercancel', onPointerUp)
  document.body.classList.remove('split-view-dragging')
  document.body.classList.remove('split-view-dragging--vertical', 'split-view-dragging--horizontal')
})
</script>

<style scoped>
/* Divider: a single 1px line by default — no visible gap. On hover/drag the
   line ITSELF turns accent and thickens to 2px: the host grows 1px → 2px (via
   negative margins so layout does NOT shift) and the shared line rules fill it.
   There is deliberately NO tinted band — a band straddles the divider's edge,
   and against a pane it covered the adjacent scrollbar. Only a thin line is
   highlighted now.

   The line itself (resting thickness, the accent colour and its hover/drag
   triggers) lives in the shared `assets/resize-divider.css` — the same file
   TocDock.vue consumes. Only the host geometry stays here, because it differs
   per consumer. */
.split-view__divider {
  position: relative;
  flex: 0 0 auto;
  margin: 0;
  touch-action: none;
  -webkit-tap-highlight-color: transparent;
  z-index: 2;
  transition: width var(--duration-base) ease, height var(--duration-base) ease, margin var(--duration-base) ease;
}

/* ── Horizontal split (left | right): vertical 1px line, drag on X ── */
.split-view__divider--horizontal {
  width: var(--split-gutter, 1px);
  cursor: col-resize;
}
/* Invisible hit area so hover/touch can catch the 1px line. ±4px (9px total):
   enough for a mouse, narrow enough not to swallow the scrollbar that sits
   flush against the pane edge. */
.split-view__divider--horizontal::before {
  content: '';
  position: absolute;
  top: 0;
  bottom: 0;
  left: calc(-4 * var(--dp));
  right: calc(-4 * var(--dp));
}
/* 2px highlight. `:active` covers mouse press; `--expanded` covers the whole
   pointer session, which is what touch needs (see the `dragging` ref).
   `margin: 0 -0.5px` widens the 1px host symmetrically about its edge:
   (2 - 1) / 2 = 0.5, so the outer footprint stays 1px and neither pane shifts.
   This block must stay in lockstep with the `(hover: hover)` block below —
   both are host geometry, the line's own rules are shared. */
.split-view__divider--horizontal:active,
.split-view__divider--horizontal.resize-divider--expanded {
  width: calc(2 * var(--dp));
  margin: 0 -0.5px;
}
@media (hover: hover) {
  .split-view__divider--horizontal:hover {
    width: calc(2 * var(--dp));
    margin: 0 -0.5px;
  }
}

/* ── Vertical split (top / bottom): horizontal 1px line, drag on Y ── */
.split-view__divider--vertical {
  height: var(--split-gutter, 1px);
  cursor: row-resize;
}
/* Invisible hit area — ±4px, same rationale as the horizontal band above. */
.split-view__divider--vertical::before {
  content: '';
  position: absolute;
  left: 0;
  right: 0;
  top: calc(-4 * var(--dp));
  bottom: calc(-4 * var(--dp));
}
/* Touch: a 1px line is an unhittable target (and the negative-margin hover
   growth is hover-only). Give touch pointers a fatter invisible grab band via
   the pseudo-element, without changing the visual 1px line. */
@media (pointer: coarse) {
  .split-view__divider--vertical::before {
    top: calc(-12 * var(--dp));
    bottom: calc(-12 * var(--dp));
  }
  .split-view__divider--horizontal::before {
    left: calc(-12 * var(--dp));
    right: calc(-12 * var(--dp));
  }
  /* A slightly thicker resting line reads as draggable on touch. */
  .split-view__divider--vertical {
    height: calc(3 * var(--dp));
  }
  .split-view__divider--horizontal {
    width: calc(3 * var(--dp));
  }
}
.split-view__divider--vertical:active,
.split-view__divider--vertical.resize-divider--expanded {
  height: calc(2 * var(--dp));
  margin: -0.5px 0;
}
@media (hover: hover) {
  .split-view__divider--vertical:hover {
    height: calc(2 * var(--dp));
    margin: -0.5px 0;
  }
}

/* ── Collapse handles ──
   One round button on EACH side of the line, each hiding the pane it points at.
   They are children of the divider but positioned just outside its 1px box, so
   they read as "belonging to" the pane they will hide rather than to the line.

   At rest they are invisible AND inert: an invisible click target floating over
   the pane edge would swallow clicks meant for the content beside it, so
   `pointer-events` tracks the same conditions as `opacity` (including
   `:focus-within`, without which a keyboard user could focus a button they
   cannot see).

   Being DOM children is what makes the reveal stable: `:hover` propagates to
   ancestors of the hovered element, so moving the pointer off the 1px line and
   onto a handle keeps the divider hovered. A body-level overlay would flicker
   the instant the pointer left the line.

   SIZING — scales, but with a CAP. The handles follow `--ui-scale` like the rest
   of the UI (at 1.5× they are 30px, at 2× they would be 40px), and 40px is too
   heavy for a hint floating over a pane's content. `min()` stops the growth at
   28px: measured against the chat composer the handle stays at roughly 0.6 of
   its height from 1.4× upwards instead of climbing to 0.77. The cap is a literal
   px on purpose (like `--radius-full`) — it exists precisely to STOP following
   the scale — and it applies to the icon and the gap too, otherwise the glyph
   would keep growing inside a frozen circle. */
.split-view__collapse-btn {
  position: absolute;
  width: min(calc(20 * var(--dp)), 28px);
  height: min(calc(20 * var(--dp)), 28px);
  padding: 0;
  display: flex;
  align-items: center;
  justify-content: center;
  /* CONTRAST — the panes on either side are `--bg-secondary`, and in 15 of the
     36 themes secondary↔tertiary is under 1.12 (ayu-dark: 1.062), so a plain
     `--bg-tertiary` fill would vanish. Mixing `--text-primary` INTO the pane
     colour guarantees the direction of the step in every theme (design-guide
     red line 4).

     Measured over all 36 themes, `--text-primary` at 26% gives 1.87:1 against
     the pane (median; worst case 1.33 on solarized-light), and the ring at 90%
     gives 9.83:1 (worst 3.11) — a clearly drawn disc rather than the ~1.05–1.2
     a bare `bg-primary` fill produced. `--border-color` is deliberately NOT
     used for the ring: several themes keep it nearly identical to the panel
     background, which is what made the first attempt read as "almost the same
     colour as the panes". The glyph stays at full `--text-primary`: 2.73:1 on
     the fill, enough for a non-text icon. */
  border: 1px solid color-mix(in srgb, var(--text-primary) 90%, transparent);
  border-radius: var(--radius-full);
  background: color-mix(in srgb, var(--text-primary) 26%, var(--bg-secondary));
  color: var(--text-primary);
  box-shadow: var(--shadow-md);
  cursor: pointer;
  opacity: 0;
  pointer-events: none;
  transition: opacity var(--duration-base) ease, color var(--duration-base), border-color var(--duration-base), background var(--duration-base);
  -webkit-tap-highlight-color: transparent;
}
.split-view__collapse-btn svg {
  width: min(calc(12 * var(--dp)), 16px);
  height: min(calc(12 * var(--dp)), 16px);
}
.split-view__divider:hover .split-view__collapse-btn,
.split-view__divider:focus-within .split-view__collapse-btn {
  opacity: 1;
  pointer-events: auto;
}
@media (hover: hover) {
  .split-view__collapse-btn:hover {
    color: var(--accent-color);
    border-color: var(--accent-color);
    background: color-mix(in srgb, var(--accent-color) 16%, var(--bg-secondary));
  }
}
.split-view__collapse-btn:focus-visible {
  outline: 2px solid var(--accent-color);
  outline-offset: 1px;
}

/* ── Handle placement ──
   Each orientation needs its own pair of offsets: the panes sit on the X axis
   for a horizontal split and on the Y axis for a vertical one. The gap keeps
   the handles clear of the pane's scrollbar (which is flush against the edge),
   so a hover does not read as "the scrollbar grew a button". The gap takes the
   same cap as the size so the pair does not drift away from the line on a
   large scale. */
.split-view__divider--horizontal .split-view__collapse-btn--first {
  top: 50%;
  left: 0;
  transform: translate(calc(-100% - min(var(--space-2), 6px)), -50%);
}
.split-view__divider--horizontal .split-view__collapse-btn--second {
  top: 50%;
  left: 100%;
  transform: translate(min(var(--space-2), 6px), -50%);
}
.split-view__divider--vertical .split-view__collapse-btn--first {
  left: 50%;
  top: 0;
  transform: translate(-50%, calc(-100% - min(var(--space-2), 6px)));
}
.split-view__divider--vertical .split-view__collapse-btn--second {
  left: 50%;
  top: 100%;
  transform: translate(-50%, min(var(--space-2), 6px));
}

/* Touch has no hover, so the handles are always shown — and always
   hit-testable, matching their visible state. The cap is higher here because a
   finger needs a real target (28px would be below the comfortable minimum). */
@media (pointer: coarse) {
  .split-view__collapse-btn {
    opacity: 1;
    pointer-events: auto;
    width: min(calc(26 * var(--dp)), 34px);
    height: min(calc(26 * var(--dp)), 34px);
  }
  .split-view__collapse-btn svg {
    width: min(calc(14 * var(--dp)), 18px);
    height: min(calc(14 * var(--dp)), 18px);
  }
}

:global(body.split-view-dragging) {
  user-select: none;
}
:global(body.split-view-dragging--horizontal) {
  cursor: col-resize;
}
:global(body.split-view-dragging--vertical) {
  cursor: row-resize;
}
</style>

<style>
/* Shared with TocDock.vue — NOT scoped, so the class names stay literal
   instead of getting a per-component data-v attribute. Both dividers must
   receive the identical line rules. */
@import '@/assets/resize-divider.css';
</style>
