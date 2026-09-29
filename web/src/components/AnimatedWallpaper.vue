<!--
  Animated wallpaper host.

  Owns the whole canvas lifecycle (rAF loop with a frame cap, visibility pause,
  ResizeObserver, reduced-motion static frame) and delegates the actual painting
  to a style from the registry. A style is a pure `draw(frame)` — it cannot
  forget the lifecycle, and it cannot leak a loop.

  ── Why this is a component and not a composable in App.vue ────────────────
  App.vue's root node carries `:key="projectKey"`, and switching projects
  reassigns it — which destroys and rebuilds the whole `.app-container` subtree.
  App.vue itself never unmounts, so a composable living there would never see
  onUnmounted fire and would leak one requestAnimationFrame loop per project
  switch, each still drawing into a detached canvas. As a child component the
  teardown is automatic: mount starts, unmount stops. (Same reasoning as
  web/src/directives/runningSweep.ts.)

  ── Why the style reads params every frame instead of caching ──────────────
  `props.params` is read fresh inside `draw()` each frame, so a slider drag takes
  effect on the next frame with no watcher at all. The static (reduced-motion)
  path has no frame loop, so it DOES need a watcher to redraw on change — that is
  the only place a watcher is required.
-->
<template>
  <canvas ref="canvasRef" class="wave-canvas" aria-hidden="true" />
</template>

<script setup lang="ts">
import { onMounted, onUnmounted, ref, watch } from 'vue'
import { appLog } from '@/utils/appLog'
import { getAnimatedStyle, type ParamValue } from '@/utils/animatedWallpapers'

const props = withDefaults(defineProps<{
  /** Registry id of the style to render. Unknown ids fall back to the default. */
  styleId?: string
  /** Speed slider value, 10–100 where 50 is 1x. Mapped per style. */
  speed?: number
  /** Resolved parameter bag for the style (see useAnimatedWallpaperParams). */
  params?: Record<string, ParamValue>
}>(), { styleId: 'xmb', speed: 50, params: () => ({}) })

const canvasRef = ref<HTMLCanvasElement | null>(null)

/**
 * Backing-store scale. Fixed 1.5x supersampling, NOT min(1.5, devicePixelRatio):
 * supersampling means drawing above the display resolution and letting the
 * browser downscale with bilinear filtering, which is itself antialiasing — so
 * a DPR1 screen benefits too. Writing min(1.5, dpr) would give exactly 1 on
 * those screens, i.e. no improvement at all.
 */
const BACKING_SCALE = 1.5

/** Frame cap. The wallpapers are ambient; 30fps is plenty and halves the work. */
const MIN_FRAME_MS = 1000 / 30

/** Largest dt accepted, so returning from a background tab does not jump. */
const MAX_DT = 0.25

let ctx: CanvasRenderingContext2D | null = null
let cssW = 0
let cssH = 0
let scale = BACKING_SCALE
let rafId = 0
let lastTs = 0
let acc = 0
let time = 0
let running = false
let resizeObserver: ResizeObserver | null = null

function resize() {
  const el = canvasRef.value
  if (!el || !ctx) return
  const rect = el.getBoundingClientRect()
  cssW = Math.max(1, rect.width)
  cssH = Math.max(1, rect.height)

  scale = BACKING_SCALE
  el.width = Math.max(1, Math.round(cssW * scale))
  el.height = Math.max(1, Math.round(cssH * scale))
}

function draw() {
  if (!ctx) return
  // The style also guards this, but bailing here keeps a bad frame from even
  // reaching it (and covers a style that forgets).
  if (cssW <= 0 || cssH <= 0) return

  ctx.setTransform(scale, 0, 0, scale, 0, 0)

  const style = getAnimatedStyle(props.styleId)
  const [lo, hi] = style.speedRange
  const slider = Number.isFinite(props.speed) ? props.speed : 50
  // The shared 10–100 slider (50 = 1x) maps into THIS style's own bounds, which
  // differ per style (xmb ships 0.2–2.0x, silk uses the prototype's 0.25–2.5x).
  const speedScale = Math.min(hi, Math.max(lo, slider / 50))

  try {
    style.draw({
      ctx,
      cssW,
      cssH,
      scale,
      time,
      speed: speedScale,
      params: props.params,
    })
  } catch (e) {
    // A throwing style must not kill the loop silently: log once per failure and
    // keep going, so one bad frame does not freeze the wallpaper forever.
    appLog.w('AnimatedWallpaper', `style "${style.id}" draw threw:`, e)
  }
}

function tick(ts: number) {
  rafId = requestAnimationFrame(tick)

  if (!lastTs) lastTs = ts
  let dt = (ts - lastTs) / 1000
  lastTs = ts
  if (dt > MAX_DT) dt = MAX_DT

  acc += dt * 1000
  if (acc < MIN_FRAME_MS) return
  acc = 0
  time += dt

  draw()
}

function start() {
  if (running || !ctx) return
  running = true
  lastTs = 0
  acc = 0
  rafId = requestAnimationFrame(tick)
}

function stop() {
  if (!running) return
  running = false
  if (rafId) cancelAnimationFrame(rafId)
  rafId = 0
}

/** Pause while hidden: saves power and avoids a burst of catch-up frames. */
function onVisibilityChange() {
  if (document.hidden) stop()
  else if (!prefersReducedMotion()) start()
}

/** jsdom has no matchMedia, and some older engines lack the query. */
function prefersReducedMotion(): boolean {
  if (typeof window === 'undefined' || typeof window.matchMedia !== 'function') return false
  try {
    return window.matchMedia('(prefers-reduced-motion: reduce)').matches
  } catch {
    return false
  }
}

onMounted(() => {
  const el = canvasRef.value
  // jsdom returns null for getContext('2d'); the wallpaper is decoration, so skip
  // it rather than throwing (mirrors runningSweep.ts).
  ctx = el?.getContext('2d') ?? null
  if (!el || !ctx) return

  resize()

  if (prefersReducedMotion()) {
    // Draw one static frame and never start the loop.
    draw()
  } else {
    start()
  }

  document.addEventListener('visibilitychange', onVisibilityChange)
  // A ResizeObserver (rather than a window listener) also covers layout changes
  // that do not resize the window, e.g. the dock collapsing.
  if (typeof ResizeObserver !== 'undefined') {
    resizeObserver = new ResizeObserver(() => {
      resize()
      if (!running) draw()
    })
    resizeObserver.observe(el)
  }
})

onUnmounted(() => {
  stop()
  document.removeEventListener('visibilitychange', onVisibilityChange)
  resizeObserver?.disconnect()
  resizeObserver = null
  ctx = null
})

// When the loop is running, params and speed are read fresh each frame, so these
// watchers only matter for the static (reduced-motion) path — without them,
// changing a slider there would leave the frozen frame stale forever.
watch(() => props.speed, () => {
  if (!running) draw()
})
watch(() => props.styleId, () => {
  if (!running) draw()
})
watch(() => props.params, () => {
  if (!running) draw()
}, { deep: true })
</script>

<style scoped>
/* The canvas is transparent; the base colour comes from the stage behind it,
   so the horizontal alpha mask fades only the drawn artwork. */
.wave-canvas {
  position: absolute;
  inset: 0;
  width: 100%;
  height: 100%;
  display: block;
  pointer-events: none;
}
</style>
