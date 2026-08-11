<template>
  <span
    ref="wrapperRef"
    class="hm-wrapper"
    :class="{ 'hm-draggable': isOverflow, 'hm-dragging': isDragging }"
    :title="title || text"
    @pointerdown="onPointerDown"
    @pointermove="onPointerMove"
    @pointerup="onPointerUp"
    @lostpointercapture="onPointerUp"
    @wheel="onWheel"
  >
    <span ref="textRef" class="hm-text"><slot /></span>
  </span>
</template>

<script setup>
import { ref, onMounted, onBeforeUnmount, watch, nextTick } from 'vue'

const props = defineProps({
  text: { type: String, default: '' },
  title: { type: String, default: '' },
})

const wrapperRef = ref(null)
const textRef = ref(null)
const isOverflow = ref(false)
const isDragging = ref(false)
const scrollOffset = ref(0)

// Pointer drag tracking (not reactive, internal only)
let pointerStartX = 0
let scrollStartOffset = 0

// Fling inertia (velocity in px/ms, negative = moving left)
const FLING_THRESHOLD = 0.3
const FLING_DECAY = 0.93
const FLING_STOP_VELOCITY = 0.02

let velocityX = 0
let lastMoveX = 0
let lastMoveT = 0
let rafId = null
let lastFlingFrame = 0

let ro = null

function checkOverflow() {
  if (!wrapperRef.value || !textRef.value) return
  const wrapperWidth = wrapperRef.value.offsetWidth
  const textWidth = textRef.value.offsetWidth
  isOverflow.value = textWidth > wrapperWidth - 8
  if (!isOverflow.value) {
    scrollOffset.value = 0
    applyScroll()
  }
}

function applyScroll() {
  if (!textRef.value) return
  textRef.value.style.transform = `translateX(${scrollOffset.value}px)`
}

function getMaxScroll() {
  if (!wrapperRef.value || !textRef.value) return 0
  return textRef.value.offsetWidth - wrapperRef.value.offsetWidth + 8
}

function clampOffset(offset) {
  const max = getMaxScroll()
  if (max <= 0) return 0
  return Math.max(-max, Math.min(0, offset))
}

function onPointerDown(e) {
  if (!isOverflow.value) return
  stopFling()
  isDragging.value = true
  pointerStartX = e.clientX
  scrollStartOffset = scrollOffset.value
  velocityX = 0
  lastMoveX = e.clientX
  lastMoveT = performance.now()
  wrapperRef.value?.setPointerCapture?.(e.pointerId)
}

function onPointerMove(e) {
  if (!isDragging.value) return
  const now = performance.now()
  const dx = e.clientX - pointerStartX
  scrollOffset.value = clampOffset(scrollStartOffset + dx)
  // Sample velocity from the most recent move for a responsive fling.
  const dt = now - lastMoveT
  if (dt > 0) velocityX = (e.clientX - lastMoveX) / dt
  lastMoveX = e.clientX
  lastMoveT = now
  applyScroll()
}

function onPointerUp(e) {
  if (!isDragging.value) return
  isDragging.value = false
  wrapperRef.value?.releasePointerCapture?.(e.pointerId)
  if (Math.abs(velocityX) > FLING_THRESHOLD) startFling(velocityX)
  velocityX = 0
}

function startFling(v) {
  if (v === 0) return
  velocityX = v
  lastFlingFrame = performance.now()
  const step = (now) => {
    if (rafId === null) return
    const dt = now - lastFlingFrame
    lastFlingFrame = now
    const prev = scrollOffset.value
    scrollOffset.value = clampOffset(prev + velocityX * dt)
    velocityX *= FLING_DECAY
    // Stop when velocity dies out or we hit a clamp boundary (no further travel).
    if (
      Math.abs(velocityX) < FLING_STOP_VELOCITY ||
      scrollOffset.value === prev
    ) {
      stopFling()
      return
    }
    rafId = requestAnimationFrame(step)
  }
  rafId = requestAnimationFrame(step)
}

function stopFling() {
  if (rafId !== null) {
    cancelAnimationFrame(rafId)
    rafId = null
  }
}

function normalizeWheelDelta(e) {
  let delta = e.deltaX !== 0 ? e.deltaX : e.deltaY
  if (e.deltaMode === 1) delta *= 40 // line mode → approx pixel
  if (e.deltaMode === 2) delta *= 800 // page mode
  return delta
}

function onWheel(e) {
  if (!isOverflow.value) return
  const delta = normalizeWheelDelta(e)
  if (delta === 0) return
  e.preventDefault()
  scrollOffset.value = clampOffset(scrollOffset.value - delta)
  applyScroll()
}

onMounted(() => {
  checkOverflow()
  ro = new ResizeObserver(checkOverflow)
  if (wrapperRef.value) ro.observe(wrapperRef.value)
  if (textRef.value) ro.observe(textRef.value)
})

onBeforeUnmount(() => {
  stopFling()
  ro?.disconnect()
})

// Re-check when text changes
watch(() => props.text, async () => {
  await nextTick()
  scrollOffset.value = 0
  applyScroll()
  checkOverflow()
})

defineExpose({ checkOverflow, isOverflow, isDragging, scrollOffset, getMaxScroll, clampOffset, normalizeWheelDelta, onPointerDown, onPointerMove, onPointerUp, onWheel, startFling, stopFling })
</script>

<style>
.hm-wrapper {
  display: inline-flex;
  align-items: center;
  min-width: 0;
  overflow: hidden;
  padding-left: 3px;
  width: 100%;
  max-width: 100%;
  user-select: none;
  /* pan-y only: the element handles horizontal drag itself via pointer events.
     pan-x would let the browser claim horizontal gestures for native scrolling,
     firing pointercancel after a few px and breaking the manual drag. */
  touch-action: pan-y;
}

.hm-text {
  display: inline-block;
  white-space: nowrap;
  flex-shrink: 0;
}

.hm-wrapper.hm-draggable .hm-text {
  will-change: transform;
}

.hm-wrapper.hm-draggable {
  cursor: grab;
}

.hm-wrapper.hm-dragging {
  cursor: grabbing;
}
</style>
