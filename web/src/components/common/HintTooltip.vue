<template>
  <span ref="rootEl" class="hint-tooltip-host">
    <Teleport to="body">
      <div
        v-if="show"
        ref="tipEl"
        class="hint-tooltip"
        role="tooltip"
        :style="tipStyle"
      >
        {{ content }}
      </div>
    </Teleport>
  </span>
</template>

<script setup lang="ts">
import { ref, computed, watch, onMounted, onBeforeUnmount } from 'vue'
import { toFixedCSS, getZoomedViewport } from '@/composables/useSettingsConfig'

/**
 * HintTooltip — native-title replacement that works for truncated text.
 *
 * The `title` attribute only shows while the cursor sits directly over the
 * element's visible (clipped) area, so an ellipsized row can never reveal its
 * full text. This component renders a positioned popup instead.
 *
 * Usage:
 *   <HintTooltip :content="fullPathOrText" />
 *
 * `content` is the ONLY thing needed — the tooltip follows the pointer over
 * the component's parent element (the enclosing row). Desktop pointers
 * (hover:hover) see the popup after a short hover delay; touch devices never
 * trigger it (the delay swallows a tap's synthetic hover before the click).
 *
 * The wrapper span uses `display: contents` so it adds no layout box; DOM
 * events still bubble through it, which is why listeners are attached to the
 * parent row instead of the boxless span.
 */
const props = defineProps<{
  /** Text shown in the popup. Empty/falsy → tooltip never renders. */
  content?: string
  /** Hover delay before the popup appears (ms). Default 400. */
  delay?: number
}>()

const rootEl = ref<HTMLElement | null>(null)
const tipEl = ref<HTMLElement | null>(null)

let host: HTMLElement | null = null

const pos = ref<{ x: number; y: number } | null>(null)
const visible = ref(false)

const show = computed(() => !!props.content && pos.value !== null && visible.value)

const tipStyle = ref<Record<string, string>>({})

let delayTimer: ReturnType<typeof setTimeout> | null = null
let moveTimer: ReturnType<typeof requestAnimationFrame> | null = null

function clearTimers() {
  if (delayTimer) { clearTimeout(delayTimer); delayTimer = null }
  if (moveTimer) { cancelAnimationFrame(moveTimer); moveTimer = null }
}

/** Desktop pointer only — every touch device claims hover, so gate on it. */
const hoverCapable = typeof window !== 'undefined' && typeof window.matchMedia === 'function'
  && window.matchMedia('(hover: hover)').matches

/** True while the pointer is anywhere inside the host row. */
let inside = false

function onMove(e: MouseEvent) {
  pos.value = { x: e.clientX + 10, y: e.clientY + 6 }
  // Track the cursor once the popup is visible (show watch handles the first
  // render; mousemove handles the follow).
  if (visible.value) schedulePlace()
}

function onEnter() {
  clearTimers()
  if (!hoverCapable) return
  delayTimer = setTimeout(() => { visible.value = true }, props.delay ?? 400)
}

function onLeave() {
  clearTimers()
  visible.value = false
  pos.value = null
}

/**
 * mouseover/mouseout bubble (unlike mouseenter/leave) and the host is the
 * parent row. The delay starts once when the pointer first enters the row and
 * is NOT restarted by movement between the row's children (name/path/icon).
 */
function onOver(e: MouseEvent) {
  if (!host || inside) return
  if (host.contains(e.target as Node)) {
    inside = true
    // Seed the anchor position from the enter event — a real pointer always
    // moves before entering, but this also covers programmatic entries.
    pos.value = { x: e.clientX + 10, y: e.clientY + 6 }
    onEnter()
  }
}

function onOut(e: MouseEvent) {
  if (!host || !inside) return
  const rt = e.relatedTarget as Node | null
  if (!rt || !host.contains(rt)) {
    inside = false
    onLeave()
  }
}

/**
 * Position + clamp. Width/height measured once the popup has rendered. top is
 * additionally clamped to the header safe-area inset (--header-safe-area-top)
 * so the popup never hides behind the notched area of Android/iOS headers.
 */
function place() {
  if (!pos.value || !tipEl.value) return
  let x = pos.value.x
  let y = pos.value.y
  const vp = getZoomedViewport()
  const el = tipEl.value
  const safeTop = parseInt(getComputedStyle(document.documentElement).getPropertyValue('--header-safe-area-top')) || 0
  if (x + el.offsetWidth > vp.width - 8) x = vp.width - el.offsetWidth - 8
  if (x < 8) x = 8
  if (y + el.offsetHeight > vp.height - 8) y = y - el.offsetHeight - 12
  const minTop = 8 + safeTop
  if (y < minTop) y = minTop
  tipStyle.value = {
    left: toFixedCSS(x) + 'px',
    top: toFixedCSS(y) + 'px',
  }
}

function schedulePlace() {
  if (moveTimer) cancelAnimationFrame(moveTimer)
  moveTimer = requestAnimationFrame(place)
}

let _bound = false

function bind() {
  // The host is the component's parent — the enclosing menu row. The row is
  // an ancestor of all its text, so mousemove/over/out from any of its
  // children bubble through it (events follow the DOM tree, not layout boxes).
  const el = rootEl.value?.parentElement ?? null
  host = el
  if (!el || _bound) return
  _bound = true
  el.addEventListener('mousemove', onMove)
  el.addEventListener('mouseover', onOver)
  el.addEventListener('mouseout', onOut)
  document.addEventListener('scroll', schedulePlace, true)
}

function unbind() {
  if (!host || !_bound) return
  _bound = false
  host.removeEventListener('mousemove', onMove)
  host.removeEventListener('mouseover', onOver)
  host.removeEventListener('mouseout', onOut)
  document.removeEventListener('scroll', schedulePlace, true)
  host = null
}

watch(() => props.content, (c) => {
  if (!c) {
    clearTimers()
    visible.value = false
    pos.value = null
    inside = false
  }
})

watch(show, (s) => {
  if (s) {
    // Post-flush: the teleported popup must exist before we can measure it.
    schedulePlace()
  }
}, { flush: 'post' })

onMounted(() => bind())
onBeforeUnmount(() => {
  clearTimers()
  unbind()
})
</script>

<style scoped>
/* Zero-size wrapper — the popup itself is teleported to body, so the host
   row's overflow/transform can never clip or offset it. */
.hint-tooltip-host {
  display: contents;
}
</style>

<style>
/* Unscoped: the popup renders in <body> via Teleport. */
.hint-tooltip {
  position: fixed;
  background: var(--bg-primary);
  border: 1px solid var(--border-color);
  border-radius: 6px;
  padding: 6px 10px;
  max-width: min(360px, calc(100vw - 16px));
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
  pointer-events: none;
  z-index: 9999;
  box-shadow: 0 2px 8px rgba(0, 0, 0, 0.12);
  font-size: 12px;
  line-height: 1.4;
  color: var(--text-primary);
  opacity: 0;
  transition: opacity 0.12s ease;
}
</style>
