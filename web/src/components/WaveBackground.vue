<!--
  Animated wave wallpaper (XMB-style).

  Three low-frequency bands with crisp crests, drawn on a canvas inside the
  existing `.wallpaper-layer`. The palette follows the active theme, so it
  recolours automatically.

  ── Why this is a component and not a composable in App.vue ────────────────
  App.vue's root node carries `:key="projectKey"`, and switching projects
  reassigns it — which destroys and rebuilds the whole `.app-container` subtree.
  App.vue itself never unmounts, so a composable living there would never see
  onUnmounted fire and would leak one requestAnimationFrame loop per project
  switch, each still drawing into a detached canvas. As a child component the
  teardown is automatic: mount starts, unmount stops. (Same reasoning as
  web/src/directives/runningSweep.ts.)

  ── Why the colours are read every frame instead of cached ────────────────
  `getComputedStyle` reads measured 0.0007ms/frame for the two variables — 0.08%
  of the draw cost. Caching would need `clawbench-theme-change` to invalidate,
  and that event is deliberately NOT dispatched at startup (the listener is not
  registered yet when the stored theme is applied), so a cache would be wrong on
  cold start and need a second code path. Reading each frame is both simpler and
  always correct.
-->
<template>
  <canvas ref="canvasRef" class="wave-canvas" aria-hidden="true" />
</template>

<script setup lang="ts">
import { onMounted, onUnmounted, ref, watch } from 'vue'
import { appLog } from '@/utils/appLog'
import {
  WAVE_LAYERS,
  buildWavePalette,
  centerline,
  mixRgb,
  rgba,
  waveTimeScale,
  BLACK,
  type WavePalette,
} from '@/utils/waveMath'

const props = withDefaults(defineProps<{
  /** Speed slider value, 10–100 where 50 is 1x. */
  speed?: number
}>(), { speed: 50 })

const canvasRef = ref<HTMLCanvasElement | null>(null)

/**
 * Backing-store scale. Fixed 1.5x supersampling, NOT min(1.5, devicePixelRatio):
 * supersampling means drawing above the display resolution and letting the
 * browser downscale with bilinear filtering, which is itself antialiasing — so
 * a DPR1 screen benefits too. Writing min(1.5, dpr) would give exactly 1 on
 * those screens, i.e. no improvement at all, which is where the aliasing
 * complaint came from in the first place.
 */
const BACKING_SCALE = 1.5

/**
 * Sampling step, in BACKING pixels (not CSS px).
 *
 * Fixing this in backing units is what makes supersampling actually increase
 * vertex density. Fixing it in CSS px would just draw the same vertices over
 * more pixels and leave the aliasing untouched.
 */
const STEP_BACKING = 1.5

/** Frame cap. The wave is ambient; 30fps is plenty and halves the work. */
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
let warnedNaN = false
let resizeObserver: ResizeObserver | null = null

/** Read the theme colours from the live CSS variables. */
function readPalette(): WavePalette {
  const cs = getComputedStyle(document.documentElement)
  return buildWavePalette(
    cs.getPropertyValue('--accent-color'),
    cs.getPropertyValue('--bg-primary'),
    {
      // Optional hand-tuned overrides; absent for derived themes.
      top: cs.getPropertyValue('--wave-top') || undefined,
      bottom: cs.getPropertyValue('--wave-bottom') || undefined,
      rim: cs.getPropertyValue('--wave-band') || undefined,
      body: cs.getPropertyValue('--wave-body') || undefined,
    },
  )
}

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
  // Bail before any arithmetic: a zero width would make u = x/cssW NaN, and
  // createLinearGradient throws on a non-finite stop (which aborts the frame).
  if (cssW <= 0 || cssH <= 0) return

  ctx.setTransform(scale, 0, 0, scale, 0, 0)

  const palette = readPalette()
  const stepCss = STEP_BACKING / scale
  const timeScale = waveTimeScale(props.speed)
  const t = time * timeScale

  // ① Background: a shallow vertical gradient. This is the base the wave sits on.
  const bg = ctx.createLinearGradient(0, 0, 0, cssH)
  bg.addColorStop(0, rgba(mixRgb(palette.bgBottom, BLACK, 0.34), 1))
  bg.addColorStop(1, rgba(palette.bgBottom, 1))
  ctx.fillStyle = bg
  ctx.fillRect(0, 0, cssW, cssH)

  // ② Bands, back to front. Each is a solid fill plus a bright crest stroke —
  //    the stroke is what makes the wave read as crisp rather than a blur.
  for (const layer of WAVE_LAYERS) {
    const points: [number, number][] = []
    for (let x = 0; x <= cssW; x += stepCss) {
      points.push([x, centerline(x / cssW, layer, { time: t }) * cssH])
    }
    // Close the right edge: stepCss rarely divides cssW exactly.
    if (points.length === 0 || points[points.length - 1][0] < cssW) {
      points.push([cssW, centerline(1, layer, { time: t }) * cssH])
    }

    // Canvas silently discards a path containing NaN coordinates — no throw, no
    // warning — so a layer missing a field presents as "the wave vanished" with
    // a clean console. Detect it explicitly instead of failing invisibly.
    if (points.some((p) => !Number.isFinite(p[1]))) {
      if (!warnedNaN) {
        warnedNaN = true
        appLog.w('WaveBg', `layer produced non-finite y; skipping. Check WAVE_LAYERS fields: ${JSON.stringify(layer)}`)
      }
      continue
    }

    // The gradient anchor uses the layer's highest crest, not a per-column
    // value, so the band's tint stays consistent instead of banding along x.
    let yPeak = Infinity
    for (const p of points) if (p[1] < yPeak) yPeak = p[1]
    const fadeEnd = yPeak + layer.band * cssH

    const alpha = Math.min(0.95, layer.alpha)
    const fill = ctx.createLinearGradient(0, yPeak, 0, fadeEnd)
    // The first two stops sit ~2% apart: a tight bright lip at the crest reads
    // as a crisp edge, then a long fade gives the in-wave gradient. Widening
    // this transition past a few percent makes the edge look soft and fuzzy.
    fill.addColorStop(0, rgba(palette.waveRim, alpha))
    fill.addColorStop(0.02, rgba(palette.waveRim, alpha * 0.88))
    fill.addColorStop(0.28, rgba(palette.waveBody, alpha * 0.5))
    fill.addColorStop(1, rgba(palette.waveBody, 0))

    ctx.beginPath()
    ctx.moveTo(points[0][0], points[0][1])
    for (let i = 1; i < points.length; i++) ctx.lineTo(points[i][0], points[i][1])
    ctx.lineTo(cssW, fadeEnd)
    ctx.lineTo(0, fadeEnd)
    ctx.closePath()
    ctx.fillStyle = fill
    ctx.fill()

    // Crest stroke — the main contributor to edge crispness.
    const edgeAlpha = Math.min(1, alpha * 1.25) * layer.edge
    if (edgeAlpha > 0.01) {
      ctx.beginPath()
      for (let i = 0; i < points.length; i++) {
        if (i === 0) ctx.moveTo(points[i][0], points[i][1])
        else ctx.lineTo(points[i][0], points[i][1])
      }
      ctx.strokeStyle = rgba(palette.waveRim, edgeAlpha)
      ctx.lineWidth = 1.5
      ctx.lineJoin = 'round'
      ctx.lineCap = 'round'
      ctx.stroke()
    }
  }

  // ③ Fade the left/right edges so the bands do not stop abruptly at the frame.
  ctx.globalCompositeOperation = 'destination-in'
  const mask = ctx.createLinearGradient(0, 0, cssW, 0)
  mask.addColorStop(0, 'rgba(0,0,0,0)')
  mask.addColorStop(0.06, 'rgba(0,0,0,1)')
  mask.addColorStop(0.94, 'rgba(0,0,0,1)')
  mask.addColorStop(1, 'rgba(0,0,0,0)')
  ctx.fillStyle = mask
  ctx.fillRect(0, 0, cssW, cssH)
  ctx.globalCompositeOperation = 'source-over'
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
  // jsdom returns null for getContext('2d'); the wave is decoration, so skip it
  // rather than throwing (mirrors runningSweep.ts).
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

// Speed changes only rescale time inside draw(); the canvas is not rebuilt and
// the phase is not reset, so adjusting the slider does not restart the motion.
watch(() => props.speed, () => {
  if (!running) draw()
})
</script>

<style scoped>
/* The canvas is transparent; the base colour comes from the stage behind it,
   so the horizontal alpha mask fades only the wave. */
.wave-canvas {
  position: absolute;
  inset: 0;
  width: 100%;
  height: 100%;
  display: block;
  pointer-events: none;
}
</style>
