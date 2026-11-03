<template>
  <canvas ref="canvasRef" class="live-ink-layer" :class="{ 'live-ink-layer--active': active }" />
</template>

<script setup lang="ts">
/**
 * Draws the shared ink layer over a video.
 *
 * Every coordinate is normalised (0..1) to the picture, so the same strokes
 * land in the same place on a phone and on a projector. The canvas is sized in
 * CSS pixels × devicePixelRatio so a 4K screen does not get a blurry line.
 */
import { onBeforeUnmount, onMounted, ref, watch } from 'vue'
import type { InkStroke } from '@/share/liveControl'
import type { LaserState } from '@/share/useInkBoard'

const props = defineProps<{
  strokes: InkStroke[]
  laser: LaserState | null
  /** Draw a crosshair cursor (the publisher's drawing mode). */
  active?: boolean
}>()

const canvasRef = ref<HTMLCanvasElement | null>(null)
let observer: ResizeObserver | null = null

function resize(): void {
  const canvas = canvasRef.value
  if (!canvas) return
  const rect = canvas.getBoundingClientRect()
  const dpr = window.devicePixelRatio || 1
  canvas.width = Math.max(1, Math.round(rect.width * dpr))
  canvas.height = Math.max(1, Math.round(rect.height * dpr))
  draw()
}

function draw(): void {
  const canvas = canvasRef.value
  const ctx = canvas?.getContext('2d')
  if (!canvas || !ctx) return
  const w = canvas.width
  const h = canvas.height
  ctx.clearRect(0, 0, w, h)
  if (!w || !h) return

  // Line width is expressed relative to an 800px-wide surface so a stroke looks
  // the same thickness whether it is drawn in a dialog or on a projector.
  // `w` is already in DEVICE pixels, and lineWidth is too — dividing by dpr
  // again would halve the stroke on a HiDPI screen.
  const scale = Math.max(1, w / 800)

  for (const stroke of props.strokes) {
    if (stroke.points.length === 0) continue
    ctx.beginPath()
    ctx.lineCap = 'round'
    ctx.lineJoin = 'round'
    ctx.strokeStyle = stroke.color
    ctx.lineWidth = Math.max(1, stroke.width * scale)
    const [first, ...rest] = stroke.points
    ctx.moveTo(first.x * w, first.y * h)
    if (rest.length === 0) {
      // A single tap must still leave a dot.
      ctx.lineTo(first.x * w + 0.01, first.y * h)
    } else {
      for (const p of rest) ctx.lineTo(p.x * w, p.y * h)
    }
    ctx.stroke()
  }

  if (props.laser?.on) {
    const x = props.laser.x * w
    const y = props.laser.y * h
    const r = 9 * (window.devicePixelRatio || 1)
    const glow = ctx.createRadialGradient(x, y, 0, x, y, r * 2.2)
    glow.addColorStop(0, 'rgba(255, 59, 48, 0.95)')
    glow.addColorStop(0.45, 'rgba(255, 59, 48, 0.45)')
    glow.addColorStop(1, 'rgba(255, 59, 48, 0)')
    ctx.fillStyle = glow
    ctx.beginPath()
    ctx.arc(x, y, r * 2.2, 0, Math.PI * 2)
    ctx.fill()
    ctx.fillStyle = '#ff3b30'
    ctx.beginPath()
    ctx.arc(x, y, r * 0.55, 0, Math.PI * 2)
    ctx.fill()
  }
}

watch(() => [props.strokes, props.laser], draw, { deep: false })

onMounted(() => {
  resize()
  if (typeof ResizeObserver !== 'undefined' && canvasRef.value) {
    observer = new ResizeObserver(resize)
    observer.observe(canvasRef.value)
  }
})

onBeforeUnmount(() => {
  observer?.disconnect()
  observer = null
})
</script>

<style scoped>
.live-ink-layer {
  position: absolute;
  inset: 0;
  width: 100%;
  height: 100%;
  /* Never eat pointer events on the viewer side; the publisher's drawing
     surface opts back in with its own element on top. */
  pointer-events: none;
  z-index: 4;
}

.live-ink-layer--active {
  cursor: crosshair;
  pointer-events: auto;
}
</style>
