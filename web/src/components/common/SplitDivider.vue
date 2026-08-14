<template>
  <div
    ref="dividerRef"
    class="split-view__divider"
    :class="`split-view__divider--${orientation}`"
    role="separator"
    :aria-orientation="orientation"
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
import { onBeforeUnmount, onMounted, ref } from 'vue'

const props = withDefaults(defineProps<{
  orientation?: 'vertical' | 'horizontal'
  title?: string
  ariaValueNow?: number
  ariaValueMin?: number
  ariaValueMax?: number
}>(), {
  orientation: 'vertical',
  title: '拖动调整面板宽度',
})

const emit = defineEmits<{
  (e: 'dragstart'): void
  (e: 'dragmove', value: number): void
  (e: 'dragend'): void
}>()

const dividerRef = ref<HTMLDivElement | null>(null)
let dragActive = false

function onDividerPointerDown(e: PointerEvent) {
  if (e.button !== 0) return
  dragActive = true
  dividerRef.value?.setPointerCapture?.(e.pointerId)
  document.body.classList.add('split-view-dragging')
  emit('dragstart')
}

function onPointerMove(e: PointerEvent) {
  if (!dragActive) return
  // For vertical (column split) the divider moves horizontally along clientX;
  // for horizontal (row split) it moves vertically along clientY.
  emit('dragmove', props.orientation === 'horizontal' ? e.clientY : e.clientX)
}

function onPointerUp(e: PointerEvent) {
  if (!dragActive) return
  dragActive = false
  dividerRef.value?.releasePointerCapture?.(e.pointerId)
  document.body.classList.remove('split-view-dragging')
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
})
</script>

<style scoped>
/* Divider: a single 1px line by default — no visible gap. On hover/drag it
   expands (via negative margins so layout does NOT shift) into a grab-able
   gap with an accent highlight. */
.split-view__divider {
  position: relative;
  flex: 0 0 auto;
  width: var(--split-gutter, 1px);
  height: 100%;
  margin: 0;
  cursor: col-resize;
  touch-action: none;
  -webkit-tap-highlight-color: transparent;
  z-index: 2;
  transition: width 0.15s ease, height 0.15s ease, margin 0.15s ease, background 0.15s ease;
}
/* horizontal (row split): full-width 1px-tall bar sitting above/below a panel */
.split-view__divider--horizontal {
  width: 100%;
  height: var(--split-gutter, 1px);
  cursor: row-resize;
}
/* invisible wider hit area so hover/touch can catch the 1px line */
.split-view__divider::before {
  content: '';
  position: absolute;
  top: 0;
  bottom: 0;
  left: -6px;
  right: -6px;
}
.split-view__divider--horizontal::before {
  top: -6px;
  bottom: -6px;
  left: 0;
  right: 0;
}
.split-view__divider:hover,
.split-view__divider:active {
  width: 12px;
  margin: 0 -5.5px;
  background: color-mix(in srgb, var(--accent-color, #0066cc) 12%, transparent);
}
.split-view__divider--horizontal:hover,
.split-view__divider--horizontal:active {
  width: 100%;
  height: 12px;
  margin: -5.5px 0;
}
.split-view__gutter-line {
  position: absolute;
  left: 50%;
  top: 0;
  bottom: 0;
  width: 1px;
  transform: translateX(-50%);
  background: var(--border-color, rgba(0, 0, 0, 0.12));
  transition: background 0.15s ease;
}
.split-view__divider--horizontal .split-view__gutter-line {
  left: 0;
  right: 0;
  top: 50%;
  bottom: auto;
  width: auto;
  height: 1px;
  transform: translateY(-50%);
}
.split-view__divider:hover .split-view__gutter-line,
.split-view__divider:active .split-view__gutter-line {
  background: var(--accent-color, #0066cc);
}
:global(body.split-view-dragging) {
  user-select: none;
  cursor: col-resize;
}
</style>
