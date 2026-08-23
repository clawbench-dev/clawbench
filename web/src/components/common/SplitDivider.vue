<template>
  <div
    ref="dividerRef"
    class="split-view__divider"
    role="separator"
    aria-orientation="vertical"
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

withDefaults(defineProps<{
  title?: string
  ariaValueNow?: number
  ariaValueMin?: number
  ariaValueMax?: number
}>(), {
  title: '拖动调整面板宽度',
})

const emit = defineEmits<{
  (e: 'dragstart'): void
  (e: 'dragmove', clientX: number): void
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
  emit('dragmove', e.clientX)
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
  margin: 0;
  cursor: col-resize;
  touch-action: none;
  -webkit-tap-highlight-color: transparent;
  z-index: 2;
  transition: width 0.15s ease, margin 0.15s ease, background 0.15s ease;
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
.split-view__divider:active {
  width: 12px;
  margin: 0 -5.5px;
  background: color-mix(in srgb, var(--accent-color, #0066cc) 12%, transparent);
}
@media (hover: hover) {
  .split-view__divider:hover {
    width: 12px;
    margin: 0 -5.5px;
    background: color-mix(in srgb, var(--accent-color, #0066cc) 12%, transparent);
  }
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
.split-view__divider:active .split-view__gutter-line {
  background: var(--accent-color, #0066cc);
}
@media (hover: hover) {
  .split-view__divider:hover .split-view__gutter-line {
    background: var(--accent-color, #0066cc);
  }
}
:global(body.split-view-dragging) {
  user-select: none;
  cursor: col-resize;
}
</style>
