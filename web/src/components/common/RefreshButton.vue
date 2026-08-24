<template>
  <button
    ref="btnEl"
    type="button"
    class="refresh-spin"
    :class="{ 'refresh-spin--active': loading }"
    :disabled="disabled || loading"
    :title="title"
    @click="handleClick"
  >
    <component
      :is="showConfirm ? CheckCircle2 : iconComp"
      :size="size"
      :data-confirm="showConfirm || undefined"
      :style="showConfirm ? confirmStyle : svgStyle"
      @animationend="onCheckAnimationEnd"
    />
  </button>
</template>

<script setup lang="ts">
import { computed, ref, watch, nextTick, onMounted, onBeforeUnmount } from 'vue'
import { RefreshCw, RotateCw, RotateCcw, CheckCircle2 } from 'lucide-vue-next'

/**
 * RefreshButton — unified refresh/rescan button with spin feedback.
 *
 * The spinning state is driven by the `loading` prop, which the caller owns
 * (local ref, shared module ref, composable state, or a parent prop) so the
 * spin always tracks the real load duration. The component only renders the
 * icon and wires the shared `refresh-spin` utility classes.
 *
 * Rotation is driven by the Web Animations API instead of a CSS `animation:
 * infinite`. When `loading` flips to false the spin is cancelled immediately
 * (no need to finish a whole revolution — the icon is about to swap to the
 * check confirmation anyway) and the check-in bounce plays. The bounce always
 * plays to completion: the swap-back is driven by the check-in animation's
 * `animationend` event, with a generous fallback timer for environments where
 * CSS animations never run.
 * Speed is 0.5s per revolution; the CSS animation in refresh-spin.css only
 * applies to non-RefreshButton native buttons. After the spin stops, the icon
 * briefly swaps to a green circled check (CheckCircle2) with a bounce-in.
 *
 * Usage:
 *   <RefreshButton :loading="refreshing" title="刷新" @click="onRefresh" />
 *   <RefreshButton icon="RotateCcw" :size="14" :loading="reconnecting" class="port-action-btn reconnect" @click="..."/>
 *
 * `class` / other attrs fall through to the <button> via attribute fallthrough,
 * so callers keep their existing sizing/shape classes (`.toolbar-btn`,
 * `.drilldown-refresh-btn`, ...) unchanged.
 */

const ROTATION_MS = 500 // 0.5s per full revolution

const props = withDefaults(defineProps<{
  /** Driving state: true → spinning + disabled + pointer-events none */
  loading?: boolean
  /** Which lucide icon to render */
  icon?: 'RefreshCw' | 'RotateCw' | 'RotateCcw'
  /** Icon pixel size */
  size?: number
  /** Native title / tooltip */
  title?: string
  /** Extra disabled condition (e.g. port not enabled) */
  disabled?: boolean
}>(), {
  loading: false,
  icon: 'RefreshCw',
  size: 14,
  title: undefined,
  disabled: false,
})

const emit = defineEmits<{ click: [event: MouseEvent] }>()

const iconMap = { RefreshCw, RotateCw, RotateCcw } as const
const iconComp = computed(() => iconMap[props.icon])

// Reference to the root button element; the icon <svg> lives inside it.
const btnEl = ref<HTMLButtonElement | null>(null)

// The WAAPI animation takes over rotation, so disable the global CSS animation
// (`.refresh-spin--active svg`) on this icon to avoid a double-animation conflict.
const svgStyle = computed(() => ({ animation: 'none' }))

let spinAnim: Animation | null = null
let spinAnimEl: SVGSVGElement | null = null

// Success confirmation: after the spin stops, the icon briefly swaps to a
// green circled check (CheckCircle2, bounce-in), then reverts to the original
// icon. The revert is driven by the check-in animation's `animationend` so the
// bounce always plays to completion; a fallback timer covers environments
// where CSS animations never fire (jsdom, reduced-motion).
const CONFIRM_MS = 400
const showConfirm = ref(false)
let confirmTimer: ReturnType<typeof setTimeout> | null = null

// Check's style overrides the inline `animation: none` (svgStyle) so the bounce
// animation from refresh-spin.css's `check-in` keyframes can run. `forwards`
// keeps the final scale(1) pose if the fallback timer ever beats the animation.
const confirmStyle = computed(() => ({
  color: 'var(--color-green, #16a34a)',
  animation: 'check-in 0.4s ease-out forwards',
}))

function clearConfirmTimer() {
  if (confirmTimer) { clearTimeout(confirmTimer); confirmTimer = null }
}

function onCheckAnimationEnd(e: AnimationEvent) {
  // The listener is always attached (Vue won't (re)attach a handler that
  // starts as `undefined`), so filter by the animation name here. Only the
  // check-in bounce ever plays; other animationend events (e.g. from a parent
  // animation bubbling up) are ignored.
  if (!showConfirm.value || e.animationName !== 'check-in') return
  clearConfirmTimer()
  showConfirm.value = false
}

function svgEl(): SVGSVGElement | null {
  return btnEl.value?.querySelector('svg') ?? null
}

function startSpin() {
  const svg = svgEl()
  if (!svg || typeof svg.animate !== 'function') return
  // If the animation is bound to a DIFFERENT element than the currently-visible
  // svg (the `:is` swapped to/from the Check icon, or the caller changed the
  // `icon` prop while spinning), the old animation targets a detached node and
  // would never spin what the user sees. Cancel it and re-target the live icon.
  if (spinAnim && spinAnimEl !== svg) {
    spinAnim.cancel()
    spinAnim = null
  }
  // If an animation is already running on this same element, keep it going —
  // cancel & restart would jump the angle back to 0.
  if (spinAnim) return
  spinAnim = svg.animate(
    [{ transform: 'rotate(0deg)' }, { transform: 'rotate(360deg)' }],
    { duration: ROTATION_MS, iterations: Infinity, easing: 'linear' },
  )
  spinAnimEl = svg
}

/**
 * Stop spinning. We simply cancel the WAAPI animation right away — the icon may
 * snap back to its base angle, but it is immediately swapped to the check
 * confirmation, so completing a whole revolution no longer matters.
 */
function stopSpin() {
  const anim = spinAnim
  if (!anim) return
  anim.cancel()
  spinAnim = null
  spinAnimEl = null
  playConfirm()
}

/**
 * Success confirmation: swap the icon to a green circled check (CheckCircle2)
 * with a bounce-in, then restore the original icon after the bounce finishes
 * (animationend) or after a fallback window. Skipped if a new refresh has
 * already started (`loading` flipped back to true → startSpin reset showConfirm).
 */
function playConfirm() {
  if (props.loading) return
  showConfirm.value = true
  // Fallback for environments where the check-in animation never runs: clear
  // the Check after CONFIRM_MS anyway so the button never stays green forever.
  clearConfirmTimer()
  confirmTimer = setTimeout(() => {
    confirmTimer = null
    showConfirm.value = false
  }, CONFIRM_MS)
}

watch(() => props.loading, async (v) => {
  if (v) {
    // Reset any lingering confirm state first, then wait for the DOM to swap
    // back to the refresh icon so the WAAPI animation targets the svg the user
    // actually sees (not a soon-to-be-replaced Check icon).
    showConfirm.value = false
    await nextTick()
    // Re-check: loading may have flipped back while we awaited the DOM update.
    if (!props.loading) return
    startSpin()
  } else {
    stopSpin()
  }
})

// If the caller swaps the icon while spinning, re-target the animation onto
// the newly-rendered svg (startSpin aborts the stale detached-element spin).
watch(() => props.icon, async () => {
  if (!props.loading) return
  showConfirm.value = false
  await nextTick()
  if (props.loading) startSpin()
})

onMounted(() => {
  // Component may mount already-loading (e.g. a file refresh is in flight when
  // the panel opens) — the watch above only fires on changes.
  if (props.loading) {
    showConfirm.value = false
    nextTick(() => { if (props.loading) startSpin() })
  }
})

onBeforeUnmount(() => {
  clearConfirmTimer()
  if (spinAnim) {
    spinAnim.cancel()
    spinAnim = null
  }
  spinAnimEl = null
})

function handleClick(e: MouseEvent) {
  if (props.loading || props.disabled) return
  emit('click', e)
}
</script>
