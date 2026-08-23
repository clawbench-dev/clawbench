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
      :is="showConfirm ? Check : iconComp"
      :size="size"
      :data-confirm="showConfirm || undefined"
      :style="showConfirm ? confirmStyle : svgStyle"
    />
  </button>
</template>

<script setup lang="ts">
import { computed, ref, watch, nextTick, onMounted, onBeforeUnmount } from 'vue'
import { RefreshCw, RotateCw, RotateCcw, Check } from 'lucide-vue-next'

/**
 * RefreshButton — unified refresh/rescan button with spin feedback.
 *
 * The spinning state is driven by the `loading` prop, which the caller owns
 * (local ref, shared module ref, composable state, or a parent prop) so the
 * spin always tracks the real load duration. The component only renders the
 * icon and wires the shared `refresh-spin` utility classes.
 *
 * Rotation is driven by the Web Animations API instead of a CSS `animation:
 * infinite`, so that when `loading` flips to false the icon always finishes an
 * exact whole number of revolutions — it never freezes mid-turn. Speed is
 * 0.5s per revolution; the CSS animation in refresh-spin.css only applies to
 * non-RefreshButton native buttons.
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
let finishTimer: ReturnType<typeof setTimeout> | null = null

// Success confirmation: after the spin completes a whole revolution, the icon
// briefly swaps to a green Check (bounce-in), then reverts to the original icon.
const CONFIRM_MS = 400
const showConfirm = ref(false)
let confirmTimer: ReturnType<typeof setTimeout> | null = null

// Check's style overrides the inline `animation: none` (svgStyle) so the bounce
// animation from refresh-spin.css's `check-in` keyframes can run.
const confirmStyle = computed(() => ({
  color: 'var(--color-green, #16a34a)',
  animation: 'check-in 0.4s ease-out',
}))

function clearFinishTimer() {
  if (finishTimer) { clearTimeout(finishTimer); finishTimer = null }
}

function svgEl(): SVGSVGElement | null {
  return btnEl.value?.querySelector('svg') ?? null
}

function startSpin() {
  clearFinishTimer()
  const svg = svgEl()
  if (!svg || typeof svg.animate !== 'function') return
  // If an animation is already running (e.g. rapid re-toggle), keep it going —
  // cancel & restart would jump the angle back to 0.
  if (spinAnim) return
  spinAnim = svg.animate(
    [{ transform: 'rotate(0deg)' }, { transform: 'rotate(360deg)' }],
    { duration: ROTATION_MS, iterations: Infinity, easing: 'linear' },
  )
}

/**
 * Stop spinning at an exact whole number of revolutions. We do NOT cancel the
 * animation immediately — it keeps running the remainder of the current turn
 * (minimum 50ms so the finish is perceptible) and is then cancelled exactly at
 * a 360° boundary.
 */
function stopSpin() {
  const anim = spinAnim
  if (!anim) return
  clearFinishTimer()

  let remain = ROTATION_MS
  try {
    const current = Number(anim.currentTime ?? 0)
    const progress = (current / ROTATION_MS) % 1
    if (Number.isFinite(progress)) {
      // Time left until the next full revolution (progress in 0..1 of a turn)
      remain = (1 - progress) * ROTATION_MS
    }
  } catch {
    // currentTime can throw if the animation was already cancelled
    remain = 0
  }
  // Guarantee at least a perceptible finishing motion and a complete turn when
  // the load was essentially instant.
  remain = Math.max(remain, 50)

  finishTimer = setTimeout(() => {
    finishTimer = null
    if (spinAnim) {
      spinAnim.cancel()
      spinAnim = null
    }
    playConfirm()
  }, remain)
}

/**
 * Success confirmation: swap the icon to a green Check with a bounce-in, then
 * restore the original icon after a short window. Skipped if a new refresh has
 * already started (`loading` flipped back to true → startSpin reset showConfirm).
 */
function playConfirm() {
  if (props.loading) return
  showConfirm.value = true
  if (confirmTimer) { clearTimeout(confirmTimer); confirmTimer = null }
  confirmTimer = setTimeout(() => {
    showConfirm.value = false
    confirmTimer = null
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

onMounted(() => {
  // Component may mount already-loading (e.g. a file refresh is in flight when
  // the panel opens) — the watch above only fires on changes.
  if (props.loading) {
    showConfirm.value = false
    nextTick(() => { if (props.loading) startSpin() })
  }
})

onBeforeUnmount(() => {
  clearFinishTimer()
  if (confirmTimer) { clearTimeout(confirmTimer); confirmTimer = null }
  if (spinAnim) {
    spinAnim.cancel()
    spinAnim = null
  }
})

function handleClick(e: MouseEvent) {
  if (props.loading || props.disabled) return
  emit('click', e)
}
</script>
