<template>
  <div
    ref="rootRef"
    class="anno"
    data-role="annotate-overlay"
    @pointerdown="onDown"
    @pointermove="onMove"
    @pointerup="onUp"
    @pointercancel="onUp"
    @pointerleave="onUp"
  >
    <video ref="videoRef" class="anno__video" autoplay playsinline muted @loadedmetadata="syncRect" />
    <div class="anno__ink" :style="inkWrapStyle">
      <LiveInkLayer :strokes="board.strokes.value" :laser="board.laser.value" active />
    </div>

    <div class="anno__bar" @pointerdown.stop @pointermove.stop @pointerup.stop>
      <button
        class="anno__btn"
        :class="{ 'anno__btn--on': mode === 'pen' }"
        type="button"
        :title="t('liveShare.annoPen')"
        data-action="anno-pen"
        @click="mode = 'pen'"
      >
        <Pencil :size="16" />
      </button>
      <button
        class="anno__btn"
        :class="{ 'anno__btn--on': mode === 'laser' }"
        type="button"
        :title="t('liveShare.annoLaser')"
        data-action="anno-laser"
        @click="mode = 'laser'"
      >
        <MousePointer2 :size="16" />
      </button>

      <span class="anno__sep" />

      <button
        v-for="c in COLORS"
        :key="c"
        class="anno__swatch"
        :class="{ 'anno__swatch--on': color === c }"
        :style="{ background: c }"
        type="button"
        :aria-label="c"
        @click="pickColor(c)"
      />

      <span class="anno__sep" />

      <button class="anno__btn" type="button" :title="t('liveShare.annoUndo')" data-action="anno-undo" @click="undo">
        <Undo2 :size="16" />
      </button>
      <button class="anno__btn" type="button" :title="t('liveShare.annoClear')" data-action="anno-clear" @click="clearAll">
        <Trash2 :size="16" />
      </button>

      <span class="anno__spacer" />

      <button class="anno__btn anno__btn--close" type="button" :title="t('common.close')" data-action="anno-close" @click="$emit('close')">
        <X :size="16" />
      </button>
    </div>
  </div>
</template>

<script setup lang="ts">
/**
 * Fullscreen drawing surface for the publisher.
 *
 * It shows the publisher's OWN capture, not a transparent overlay on the
 * desktop: a transparent window is a platform-specific feature (and impossible
 * in the browser), while drawing on a faithful preview works everywhere and
 * gives the same coordinates — the ink is normalised to the picture, so what
 * the publisher draws here is exactly where the viewers see it.
 */
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { MousePointer2, Pencil, Trash2, Undo2, X } from 'lucide-vue-next'
import LiveInkLayer from '@/share/LiveInkLayer.vue'
import { useInkBoard } from '@/share/useInkBoard'
import { contentRectOf, toNormalized, type ContentRect, type ControlMessage } from '@/share/liveControl'

const props = defineProps<{ stream: MediaStream | null }>()
const emit = defineEmits<{ close: []; send: [msg: ControlMessage] }>()

const { t } = useI18n()
const COLORS = ['#ff3b30', '#ffcc00', '#34c759', '#4a90d9']

const rootRef = ref<HTMLElement | null>(null)
const videoRef = ref<HTMLVideoElement | null>(null)
const board = useInkBoard()
const mode = ref<'pen' | 'laser'>('pen')
const color = ref(COLORS[0])
const inkRect = ref<ContentRect>({ left: 0, top: 0, width: 0, height: 0 })
const inkWrapStyle = computed(() => ({
  left: `${inkRect.value.left}px`,
  top: `${inkRect.value.top}px`,
  width: `${inkRect.value.width}px`,
  height: `${inkRect.value.height}px`,
}))

let drawing = false
let lastSent = 0

function syncRect(): void {
  const root = rootRef.value
  const video = videoRef.value
  if (!root) return
  inkRect.value = contentRectOf(
    {
      clientWidth: root.clientWidth,
      clientHeight: root.clientHeight,
      videoWidth: video?.videoWidth ?? 0,
      videoHeight: video?.videoHeight ?? 0,
    },
    { left: 0, top: 0 },
  )
}

function pointOf(ev: PointerEvent): { x: number; y: number } {
  return toNormalized(ev.clientX, ev.clientY, inkRect.value)
}

/** Pointer updates are throttled to ~40ms: 120Hz mice would flood the channel. */
function throttled(): boolean {
  const now = Date.now()
  if (now - lastSent < 40) return false
  lastSent = now
  return true
}

function onDown(ev: PointerEvent): void {
  const p = pointOf(ev)
  if (mode.value === 'laser') {
    emit('send', { t: 'laser', x: p.x, y: p.y, on: true })
    return
  }
  drawing = true
  ;(ev.currentTarget as HTMLElement).setPointerCapture?.(ev.pointerId)
  board.beginStroke(p, color.value, 3)
  emit('send', { t: 'ink', phase: 'start', x: p.x, y: p.y, color: color.value, width: 3 })
}

function onMove(ev: PointerEvent): void {
  const p = pointOf(ev)
  if (mode.value === 'laser') {
    if (!throttled()) return
    // Through the board so the idle timer applies here too; assigning the ref
    // directly left a frozen dot on the publisher's own preview.
    const msg = { t: 'laser' as const, x: p.x, y: p.y, on: true }
    board.apply(msg)
    emit('send', msg)
    return
  }
  if (!drawing) return
  if (!throttled()) return
  board.extendStroke(p)
  emit('send', { t: 'ink', phase: 'move', x: p.x, y: p.y, color: color.value, width: 3 })
}

function onUp(): void {
  if (mode.value === 'laser') {
    // Tell the viewers to drop the pointer now instead of waiting for their
    // 2.5s idle timeout.
    board.hideLaser()
    emit('send', { t: 'laser', x: 0, y: 0, on: false })
    return
  }
  if (!drawing) return
  drawing = false
  board.endStroke()
  emit('send', { t: 'ink', phase: 'end', x: 0, y: 0, color: color.value, width: 3 })
}

function undo(): void {
  board.undo()
  emit('send', { t: 'ink-undo' })
}

function clearAll(): void {
  board.clear()
  emit('send', { t: 'ink-clear' })
}

function pickColor(c: string): void {
  color.value = c
  mode.value = 'pen'
}

function onKey(ev: KeyboardEvent): void {
  if (ev.key === 'Escape') emit('close')
}

watch(
  () => props.stream,
  async (stream) => {
    await nextTick()
    if (videoRef.value) {
      videoRef.value.srcObject = stream
      void videoRef.value.play().catch(() => undefined)
      syncRect()
    }
  },
  { immediate: true },
)

onMounted(() => {
  window.addEventListener('resize', syncRect)
  window.addEventListener('keydown', onKey)
})

onBeforeUnmount(() => {
  window.removeEventListener('resize', syncRect)
  window.removeEventListener('keydown', onKey)
  // Closing the annotator must not leave a laser hanging on every viewer.
  emit('send', { t: 'laser', x: 0, y: 0, on: false })
  board.dispose()
})
</script>

<style scoped>
.anno {
  position: fixed;
  inset: 0;
  z-index: 3000;
  background: #000;
  cursor: crosshair;
  touch-action: none;
}

.anno__video {
  position: absolute;
  inset: 0;
  width: 100%;
  height: 100%;
  object-fit: contain;
}

.anno__ink {
  position: absolute;
  /* Own stacking context, on purpose. The ink canvas inside carries a z-index
     of its own (it is shared with the viewer layout, where it must sit above
     the picture), and a z-index on a child of a non-positioned-context parent
     competes directly with this toolbar. It won: wherever the picture filled
     the window the canvas covered the bar, so every click on a tool started a
     stroke instead — the toolbar looked present and was unusable. */
  z-index: 1;
  pointer-events: none;
}

.anno__bar {
  position: absolute;
  /* Above the ink (see `.anno__ink`): the bar is the only way to change tools,
     and the drawing surface underneath swallows clicks wherever they overlap. */
  z-index: 2;
  left: 50%;
  bottom: calc(24 * var(--dp) + env(safe-area-inset-bottom, 0px));
  transform: translateX(-50%);
  display: flex;
  align-items: center;
  gap: var(--space-2, 4px);
  padding: var(--space-2, 4px);
  border-radius: var(--radius-full, 999px);
  background: color-mix(in srgb, var(--bg-secondary, #161b22) 88%, transparent);
  border: 1px solid var(--border-color, #30363d);
  box-shadow: 0 6px 20px rgba(0, 0, 0, 0.45);
  cursor: default;
}

.anno__btn {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: calc(32 * var(--dp));
  height: calc(32 * var(--dp));
  border: none;
  border-radius: var(--radius-full, 999px);
  background: transparent;
  color: var(--text-secondary, #8b949e);
  cursor: pointer;
}

.anno__btn:hover {
  background: var(--bg-hover, rgba(255, 255, 255, 0.08));
  color: var(--text-primary, #e6edf3);
}

.anno__btn--on {
  background: color-mix(in srgb, var(--accent-color, #4a90d9) 28%, transparent);
  color: var(--accent-color, #4a90d9);
}

.anno__btn--close {
  color: var(--color-red, #cf222e);
}

.anno__swatch {
  width: calc(20 * var(--dp));
  height: calc(20 * var(--dp));
  border-radius: 50%;
  border: 2px solid transparent;
  cursor: pointer;
}

.anno__swatch--on {
  border-color: #fff;
}

.anno__sep {
  width: 1px;
  height: 18px;
  margin: 0 var(--space-2, 4px);
  background: var(--border-color, #30363d);
}

.anno__spacer {
  width: var(--space-4, 8px);
}
</style>
