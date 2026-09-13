<template>
  <div
    ref="dividerRef"
    class="split-view__divider"
    :class="[`split-view__divider--${orientation}`, { 'split-view__divider--dragging': dragging }]"
    role="separator"
    :aria-orientation="ariaOrientation"
    :aria-valuenow="ariaValueNow"
    :aria-valuemin="ariaValueMin"
    :aria-valuemax="ariaValueMax"
    :title="title"
    @pointerdown="onDividerPointerDown"
  >
    <div class="split-view__gutter-line" />
  </div>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'

const props = withDefaults(defineProps<{
  /** 'horizontal' = left/right panes (vertical divider line, drag on X).
   *  'vertical'   = top/bottom panes (horizontal divider line, drag on Y). */
  orientation?: 'horizontal' | 'vertical'
  title?: string
  ariaValueNow?: number
  ariaValueMin?: number
  ariaValueMax?: number
}>(), {
  orientation: 'horizontal',
  title: '拖动调整面板宽度',
})

const isVertical = computed(() => props.orientation === 'vertical')

/** A vertical divider separates left/right panes; a horizontal one separates
 *  top/bottom. ARIA describes the separator line itself. */
const ariaOrientation = computed(() => (isVertical.value ? 'horizontal' : 'vertical'))

const emit = defineEmits<{
  (e: 'dragstart'): void
  (e: 'dragmove', clientPos: number): void
  (e: 'dragend'): void
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
/* Divider: a single 1px line by default — no visible gap. On hover/drag it
   expands (via negative margins so layout does NOT shift) into a grab-able
   gap with an accent highlight. */
.split-view__divider {
  position: relative;
  flex: 0 0 auto;
  margin: 0;
  touch-action: none;
  -webkit-tap-highlight-color: transparent;
  z-index: 2;
  transition: width var(--duration-base) ease, height var(--duration-base) ease, margin var(--duration-base) ease, background var(--duration-base) ease;
}

/* ── Horizontal split (left | right): vertical 1px line, drag on X ── */
.split-view__divider--horizontal {
  width: var(--split-gutter, 1px);
  cursor: col-resize;
}
/* invisible wider hit area so hover/touch can catch the 1px line */
.split-view__divider--horizontal::before {
  content: '';
  position: absolute;
  top: 0;
  bottom: 0;
  left: -6px;
  right: -6px;
}
/* Expanded highlight. `:active` covers mouse press; `--dragging` covers the
   whole pointer session, which is what touch needs (see the `dragging` ref). */
.split-view__divider--horizontal:active,
.split-view__divider--horizontal.split-view__divider--dragging {
  width: 12px;
  margin: 0 -5.5px;
  background: color-mix(in srgb, var(--accent-color, #0066cc) 12%, transparent);
}
@media (hover: hover) {
  .split-view__divider--horizontal:hover {
    width: 12px;
    margin: 0 -5.5px;
    background: color-mix(in srgb, var(--accent-color, #0066cc) 12%, transparent);
  }
}

/* ── Vertical split (top / bottom): horizontal 1px line, drag on Y ── */
.split-view__divider--vertical {
  height: var(--split-gutter, 1px);
  cursor: row-resize;
}
.split-view__divider--vertical::before {
  content: '';
  position: absolute;
  left: 0;
  right: 0;
  top: -6px;
  bottom: -6px;
}
/* Touch: a 1px line is an unhittable target (and the negative-margin hover
   growth is hover-only). Give touch pointers a fat invisible grab band via the
   pseudo-element, without changing the visual 1px line. */
@media (pointer: coarse) {
  .split-view__divider--vertical::before {
    top: -12px;
    bottom: -12px;
  }
  .split-view__divider--horizontal::before {
    left: -12px;
    right: -12px;
  }
  /* A slightly thicker resting line reads as draggable on touch. */
  .split-view__divider--vertical {
    height: 3px;
  }
  .split-view__divider--horizontal {
    width: 3px;
  }
}
.split-view__divider--vertical:active,
.split-view__divider--vertical.split-view__divider--dragging {
  height: 12px;
  margin: -5.5px 0;
  background: color-mix(in srgb, var(--accent-color, #0066cc) 12%, transparent);
}
@media (hover: hover) {
  .split-view__divider--vertical:hover {
    height: 12px;
    margin: -5.5px 0;
    background: color-mix(in srgb, var(--accent-color, #0066cc) 12%, transparent);
  }
}

.split-view__gutter-line {
  position: absolute;
  background: var(--border-color, rgba(0, 0, 0, 0.12));
  transition: background var(--duration-base) ease;
}
.split-view__divider--horizontal .split-view__gutter-line {
  left: 50%;
  top: 0;
  bottom: 0;
  width: 1px;
  transform: translateX(-50%);
}
.split-view__divider--vertical .split-view__gutter-line {
  top: 50%;
  left: 0;
  right: 0;
  height: 1px;
  transform: translateY(-50%);
}
.split-view__divider:active .split-view__gutter-line,
.split-view__divider--dragging .split-view__gutter-line {
  background: var(--accent-color, #0066cc);
}
@media (hover: hover) {
  .split-view__divider:hover .split-view__gutter-line {
    background: var(--accent-color, #0066cc);
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
