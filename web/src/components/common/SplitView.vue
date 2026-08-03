<template>
  <div
    ref="rootRef"
    class="split-view"
    :class="{ 'split-view--active': enabled }"
    :style="{ '--split-gutter': `${gutterSize}px` }"
  >
    <div class="split-view__left" :style="leftStyle">
      <slot name="left" />
    </div>
    <div
      v-if="enabled"
      ref="dividerRef"
      class="split-view__divider"
      role="separator"
      aria-orientation="vertical"
      :aria-valuenow="Math.round(internalRatio * 100)"
      :aria-valuemin="Math.round(minLeftRatio * 100)"
      :aria-valuemax="Math.round(maxLeftRatio * 100)"
      :title="title"
      @pointerdown="onDividerPointerDown"
    >
      <div class="split-view__gutter-line" />
    </div>
    <div class="split-view__right">
      <slot name="right" />
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { clampRatio, normalizeRatio, MIN_PANEL_WIDTH } from '@/utils/splitRatio'

const props = withDefaults(defineProps<{
  enabled: boolean
  ratio?: number
  minLeft?: number
  minRight?: number
  gutterSize?: number
  title?: string
}>(), {
  ratio: 0.5,
  minLeft: MIN_PANEL_WIDTH,
  minRight: MIN_PANEL_WIDTH,
  gutterSize: 1,
  title: '拖动调整面板宽度',
})

const emit = defineEmits<{ (e: 'update:ratio', ratio: number): void }>()

const rootRef = ref<HTMLDivElement | null>(null)
const dividerRef = ref<HTMLDivElement | null>(null)
const internalRatio = ref(normalizeRatio(props.ratio))
const containerWidth = ref(0)
let dragActive = false
let observer: ResizeObserver | null = null

watch(() => props.ratio, (r) => {
  internalRatio.value = normalizeRatio(r)
})

const leftStyle = computed(() => {
  if (!props.enabled) return {}
  return { width: `${internalRatio.value * 100}%` }
})

const minLeftRatio = computed(() => (containerWidth.value > 0 ? props.minLeft / containerWidth.value : 0))
const maxLeftRatio = computed(() => (containerWidth.value > 0 ? 1 - props.minRight / containerWidth.value : 1))

function measureContainer() {
  if (rootRef.value) containerWidth.value = rootRef.value.getBoundingClientRect().width
}

function onMove(e: PointerEvent) {
  const rect = rootRef.value?.getBoundingClientRect()
  if (!rect || rect.width <= 0) return
  const raw = (e.clientX - rect.left) / rect.width
  const ratio = clampRatio(raw, rect.width, props.minLeft, props.minRight)
  internalRatio.value = ratio
  emit('update:ratio', ratio)
}

function onDividerPointerDown(e: PointerEvent) {
  if (e.button !== 0) return
  dragActive = true
  dividerRef.value?.setPointerCapture?.(e.pointerId)
  document.body.classList.add('split-view-dragging')
  onMove(e)
}

function onPointerMove(e: PointerEvent) {
  if (dragActive) onMove(e)
}

function onPointerUp(e: PointerEvent) {
  if (!dragActive) return
  dragActive = false
  dividerRef.value?.releasePointerCapture?.(e.pointerId)
  document.body.classList.remove('split-view-dragging')
}

onMounted(() => {
  measureContainer()
  if (typeof ResizeObserver !== 'undefined') {
    observer = new ResizeObserver(measureContainer)
    if (rootRef.value) observer.observe(rootRef.value)
  }
  window.addEventListener('pointermove', onPointerMove)
  window.addEventListener('pointerup', onPointerUp)
  window.addEventListener('pointercancel', onPointerUp)
})

onBeforeUnmount(() => {
  observer?.disconnect()
  window.removeEventListener('pointermove', onPointerMove)
  window.removeEventListener('pointerup', onPointerUp)
  window.removeEventListener('pointercancel', onPointerUp)
  document.body.classList.remove('split-view-dragging')
})
</script>

<style scoped>
.split-view {
  position: relative;
  height: 100%;
  width: 100%;
}
.split-view--active {
  display: flex;
  flex-direction: row;
  align-items: stretch;
}
.split-view__left,
.split-view__right {
  position: absolute;
  inset: 0;
}
/* Disabled (single-column) mode: the wrappers are pure pass-throughs. If both
   stayed absolutely positioned, the later one would overlay the other and,
   when its slot content is v-show hidden, silently block every pointer event
   (touch/scroll) on the visible pane — mobile regression on non-chat tabs. */
.split-view:not(.split-view--active) .split-view__left,
.split-view:not(.split-view--active) .split-view__right {
  display: contents;
}
.split-view--active .split-view__left,
.split-view--active .split-view__right {
  position: relative;
  inset: auto;
  height: 100%;
}
.split-view--active .split-view__left {
  flex: 0 0 auto;
  min-width: 320px;
  max-width: calc(100% - 320px - var(--split-gutter, 1px));
}
.split-view--active .split-view__right {
  flex: 1 1 auto;
  min-width: 320px;
}
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
.split-view__divider:hover,
.split-view__divider:active {
  width: 12px;
  margin: 0 -5.5px;
  background: color-mix(in srgb, var(--accent-color, #0066cc) 12%, transparent);
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
.split-view__divider:hover .split-view__gutter-line,
.split-view__divider:active .split-view__gutter-line {
  background: var(--accent-color, #0066cc);
}
:global(body.split-view-dragging) {
  user-select: none;
  cursor: col-resize;
}
</style>
