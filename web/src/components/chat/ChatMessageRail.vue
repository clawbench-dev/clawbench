<template>
  <!--
    Chat-area message rail — the conversation index as an always-visible
    numbered timeline in the pane's right gutter.

    It reuses MessageIndexRow for BOTH halves so it can never drift from the
    index drawer: `variant="node"` paints the numbered node, `variant="body"`
    paints the preview row. The node layer sits ABOVE the hover card (z-index),
    so when the card opens the nodes read as part of it rather than as a second
    column beside it.

    The rail is deliberately NOT shown when the pane has no gutter to hold it
    (phones, narrow windows): there is nowhere to put it that does not overlap
    the messages.
  -->
  <div
    v-if="items.length"
    ref="railRef"
    class="msg-rail"
    :class="{
      'msg-rail--visible': visible,
      'msg-rail--scrollable': scrollable,
      'msg-rail--more-above': moreAbove,
      'msg-rail--more-below': moreBelow,
    }"
    :style="railStyle"
    role="navigation"
    :aria-label="t('chat.messageList.messageRailLabel')"
    @mouseenter="onEnter"
    @mousemove="onPointerMove"
    @mouseleave="scheduleCollapse"
    @wheel="onWheel"
    @pointerdown="onPointerDown"
    @pointermove="onPointerDrag"
    @pointerup="onPointerUp"
    @pointercancel="onPointerUp"
  >
    <div class="msg-rail-line" :style="lineStyle" aria-hidden="true"></div>
    <MessageIndexRow
      v-for="(it, k) in items"
      :key="'n' + it.key"
      variant="node"
      class="msg-rail-node"
      :class="{ 'msg-item--lit': litIndex === k }"
      :style="{ top: nodeTop(k) + 'px' }"
      :msg="it.msg"
      :index="it.ordinal"
      :active="isActive(it)"
      @select="onSelect"
    />
  </div>

  <div
    v-if="items.length"
    ref="cardRef"
    class="msg-rail-card"
    :class="{
      'msg-rail-card--show': expanded,
      'msg-rail-card--more-above': moreAbove,
      'msg-rail-card--more-below': moreBelow,
    }"
    :style="cardStyle"
    role="dialog"
    :aria-label="t('chat.messageList.conversationIndexTitle')"
    @mouseenter="onEnter"
    @mousemove="onPointerMove"
    @mouseleave="scheduleCollapse"
    @wheel="onWheel"
  >
    <MessageIndexRow
      v-for="(it, k) in items"
      :key="'r' + it.key"
      variant="body"
      class="msg-rail-row"
      :class="{ 'msg-item--lit': litIndex === k }"
      :style="{ top: rowTop(k) + 'px', height: gap + 'px' }"
      :msg="it.msg"
      :index="it.ordinal"
      :active="isActive(it)"
      @select="onSelect"
    />
  </div>
</template>

<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import MessageIndexRow from './MessageIndexRow.vue'
import { designToCss } from '@/utils/uiScale'
import type { SpeakerIdentity } from '@/utils/speakerIdentity'

/** One row of the conversation index (the shape /api/ai/chat/user-messages returns). */
export interface RailIndexMsg {
  id?: number | string
  role?: string
  summary?: string
  content?: string
  createdAt?: string
  files?: Array<string | { path?: string }>
  agentId?: string
}

const props = withDefaults(defineProps<{
  /** Conversation index rows, ordered oldest → newest (the drawer's own list). */
  messages: RailIndexMsg[]
  /** Id of the message currently in view; its node is drawn filled. */
  activeId?: number | string | null
  /** Resolves a row's speaker for the role chip inside the card. */
  resolveSpeaker?: ((agentId: string) => SpeakerIdentity | null) | null
  hasBtw?: (msg: RailIndexMsg) => boolean
  /** The scrolling message container — used to measure the available gutter. */
  getScroller?: () => HTMLElement | null
  /**
   * How much of the panel's foot the rail must leave free, in CSS px — the
   * composer block's height.
   *
   * The axis, the hover card and the scroll range all stop here. They must be
   * the same box: the numbers are the card's left column, so a number outside
   * the card would be an orphan with no row beside it, and a row inside with no
   * number. Stopping above the composer also keeps the card off the input box.
   *
   * A number rather than a measuring callback: the host measures its own
   * composer (whose height changes with attachments, the queue banner and the
   * plan card) and passes the result down.
   */
  bottomInset?: number
}>(), {
  activeId: null,
  resolveSpeaker: null,
  hasBtw: () => false,
  getScroller: () => null,
  bottomInset: 0,
})

const emit = defineEmits<{ select: [msg: RailIndexMsg] }>()

const { t } = useI18n()

/** Design px the rail needs on each side of the reading measure before it appears. */
const MIN_GUTTER_DP = 60
/** Card width. The rail column's own geometry lives in the stylesheet. */
const CARD_WIDTH_DP = 244

const items = computed(() => props.messages.map((msg, i) => ({
  // `key` must survive a re-fetch of the index: message ids are stable, but the
  // fallback position is not, so it is only used when the row has no id.
  key: msg.id != null && msg.id !== '' ? String(msg.id) : `i${i}`,
  ordinal: i + 1,
  msg,
})))

/* ── Geometry ─────────────────────────────────────────────────────────────
   Every node and every card row is absolutely positioned from the same
   `offset` and the same `gap`, so a node and its row are always the same
   distance from the top of their respective layers. That is what makes the
   node read as belonging to the row rather than as a separate column. */
const gap = ref(designToCss(38))
const offset = ref(0)
const maxOffset = ref(0)
const trackHeight = ref(0)
const trackTop = ref(0)
const visible = ref(false)

const railRef = ref<HTMLElement | null>(null)
const cardRef = ref<HTMLElement | null>(null)

const scrollable = computed(() => maxOffset.value > 0)
/**
 * The one box the axis, the card and the scroll range all live in: the panel's
 * height minus the composer at its foot.
 */
const band = computed(() =>
  Math.max(0, trackHeight.value - props.bottomInset),
)
const cardHeight = computed(() => Math.min(band.value, listContent()))
const cardTop = computed(() => trackTop.value + listOrigin())

/**
 * Whether rows exist beyond each end of the visible track.
 *
 * These drive the edge fades, so they must be EXACT: a stale "there is more
 * below" would fade the last node even after the reader scrolled to the bottom,
 * which reads as clipped content. `1px` of slack absorbs sub-pixel rounding
 * without hiding a genuinely unseen row (a row is ~38px tall).
 */
const moreAbove = computed(() => maxOffset.value > 0 && offset.value > 1)
const moreBelow = computed(() => maxOffset.value > 0 && offset.value < maxOffset.value - 1)

const cardStyle = computed(() => ({
  top: `${cardTop.value}px`,
  width: `${designToCss(CARD_WIDTH_DP)}px`,
  height: `${cardHeight.value}px`,
}))

/** The axis uses the same band as the card, so no number is ever an orphan. */
const railStyle = computed(() => ({
  height: `${band.value}px`,
}))

const lineStyle = computed(() => {
  const n = items.value.length
  const first = nodeTop(0)
  const last = nodeTop(n - 1)
  return { top: `${first}px`, height: `${Math.max(0, last - first)}px` }
})

/**
 * Slot geometry. There is exactly ONE definition of "where slot k sits", and
 * both layers derive from it:
 *
 *   card row top    = listPad() + k·gap − offset            (card-relative)
 *   card row centre = that + gap/2
 *   node centre     = the same value                        (rail-relative)
 *
 * The node is centred with `translateY(-50%)`, so its `top` IS its centre. That
 * is why the node takes `+ gap/2` and the row does not: both end up at the same
 * y, which is what makes a number read as belonging to its row.
 *
 * `listOrigin()` is the centring applied when the whole list fits: the card is
 * centred in the track, so the nodes must shift with it.
 */
function nodeTop(k: number) {
  return listOrigin() + listPad() + k * gap.value + gap.value / 2 - offset.value
}

/**
 * Card-relative top of row k.
 *
 * Derived from the NODE's rail-relative position rather than recomputed from
 * `listPad`, so a row is level with its number even though the card box is
 * shorter than the rail and starts at a different y (it is clipped above the
 * composer). Any independent formula here would drift the moment the two boxes
 * disagreed — which is exactly what happens whenever the composer changes
 * height.
 */
function rowTop(k: number) {
  return listPad() + k * gap.value - offset.value
}

/**
 * Centring offset for a list shorter than the track.
 *
 * The node layer and the card must use the SAME origin or a number sits beside
 * the wrong row. Derived from the full track (not the card's clipped band)
 * because that is also where the scroll range is computed — a shorter list is
 * never scrolled anyway, so the two can only agree here.
 */
function listOrigin() {
  return Math.max(0, (band.value - listContent()) / 2)
}

/** Total height the list needs, including the card's top/bottom padding. */
function listContent() {
  return items.value.length * gap.value + listPad() * 2
}

function listPad() {
  return designToCss(10)
}

/* ── Measuring ────────────────────────────────────────────────────────── */
function measure() {
  const rail = railRef.value
  if (!rail) return
  // Read the HOST's box, never the rail's own: the rail's height is WRITTEN from
  // this value, so measuring the rail here would feed back into itself and
  // settle at zero.
  const host = rail.parentElement?.getBoundingClientRect()
  if (!host) return
  trackHeight.value = host.height
  trackTop.value = 0

  // One row of the index is the node plus its vertical padding (8 design px on
  // each side). Deriving it from the rendered node keeps the rail in step with
  // the UI scale and with any future change to the node size.
  const node = rail.querySelector('.msg-rail-node .msg-node') as HTMLElement | null
  const nodeH = node?.offsetHeight || 0
  if (nodeH > 0) gap.value = nodeH + designToCss(16)

  // Scrollable range covers the rows PLUS the card's own vertical padding, so
  // the last row can be brought fully into view instead of stopping short by it.
  maxOffset.value = Math.max(0, listContent() - band.value)
  offset.value = clamp(offset.value)

  const scroller = props.getScroller?.()
  if (scroller) {
    // The right inset is exactly the room between the reading measure and the
    // pane edge (already net of the scrollbar lane). Reading it from the
    // scroller rather than recomputing the measure keeps the two in step.
    const inset = parseFloat(getComputedStyle(scroller).paddingRight) || 0
    visible.value = inset >= designToCss(MIN_GUTTER_DP)
  }
}

function clamp(v: number) {
  return Math.max(0, Math.min(maxOffset.value, v))
}

function setOffset(v: number) {
  const next = clamp(v)
  if (Math.abs(next - offset.value) < 0.5) return
  offset.value = next
  // The pointer now sits over a different row.
  if (expanded.value) relight()
}

/** Keep the active node inside the visible window without moving when it already is. */
function ensureVisible(index: number) {
  if (maxOffset.value <= 0 || index < 0) return
  const top = listPad() + index * gap.value - offset.value
  const bottom = top + gap.value
  if (top < 0) setOffset(offset.value + top)
  else if (bottom > band.value) setOffset(offset.value + bottom - band.value)
}

const activeIndex = computed(() => {
  const id = props.activeId
  if (id == null || id === '') return -1
  return items.value.findIndex(it => String(it.msg.id) === String(id))
})

function isActive(it: { msg: RailIndexMsg }) {
  const id = props.activeId
  return id != null && id !== '' && String(it.msg.id) === String(id)
}

/* ── Hover ────────────────────────────────────────────────────────────── */
const expanded = ref(false)
const litIndex = ref(-1)
let pointerY = 0
let hideTimer: ReturnType<typeof setTimeout> | null = null

function nearestIndex(clientY: number) {
  const rail = railRef.value
  if (!rail || items.value.length === 0) return -1
  const base = rail.getBoundingClientRect().top + listOrigin() + listPad() + gap.value / 2 - offset.value
  const raw = Math.round((clientY - base) / gap.value)
  return Math.max(0, Math.min(items.value.length - 1, raw))
}

function relight() {
  const idx = nearestIndex(pointerY)
  if (idx >= 0) litIndex.value = idx
}

function onEnter() {
  if (hideTimer) { clearTimeout(hideTimer); hideTimer = null }
}

function onPointerMove(e: MouseEvent) {
  onEnter()
  pointerY = e.clientY
  if (!expanded.value) expanded.value = true
  relight()
}

function scheduleCollapse() {
  if (hideTimer) clearTimeout(hideTimer)
  hideTimer = setTimeout(() => {
    hideTimer = null
    expanded.value = false
    litIndex.value = -1
  }, 140)
}

function onSelect(msg: RailIndexMsg) {
  if (suppressClick) return
  expanded.value = false
  litIndex.value = -1
  emit('select', msg)
}

/* ── Wheel: scroll the rail, then hand the rest to the chat ──────────────
   While the card is open the wheel is captured unconditionally: the card IS the
   index, so scrolling it must never leak into the conversation behind it. With
   the card closed the rail scrolls first and any leftover delta goes to the
   chat, so one gesture never dead-ends. */
function onWheel(e: WheelEvent) {
  const raw = e.deltaMode === 1 ? e.deltaY * gap.value : e.deltaY
  if (expanded.value) {
    e.preventDefault()
    if (maxOffset.value <= 0) return
    const step = Math.abs(raw) < gap.value ? Math.sign(raw) * gap.value : raw
    setOffset(offset.value + step)
    return
  }
  if (maxOffset.value > 0) {
    const step = Math.abs(raw) < gap.value ? Math.sign(raw) * gap.value : raw
    const next = clamp(offset.value + step)
    if (next !== offset.value) {
      e.preventDefault()
      setOffset(next)
      return
    }
  }
  e.preventDefault()
  props.getScroller?.()?.scrollBy({ top: raw })
}

let drag: { y: number; start: number; moved: boolean; id: number } | null = null
let suppressClick = false

function onPointerDown(e: PointerEvent) {
  if (maxOffset.value <= 0) return
  drag = { y: e.clientY, start: offset.value, moved: false, id: e.pointerId }
}

function onPointerDrag(e: PointerEvent) {
  if (!drag || e.pointerId !== drag.id) return
  const dy = e.clientY - drag.y
  if (!drag.moved) {
    // Capture only once the gesture is clearly a drag: capturing on pointerdown
    // retargets the subsequent click to the track, and the nodes stop working.
    if (Math.abs(dy) <= 3) return
    drag.moved = true
    railRef.value?.setPointerCapture(e.pointerId)
  }
  setOffset(drag.start - dy)
}

function onPointerUp(e: PointerEvent) {
  if (!drag) return
  if (drag.moved) {
    suppressClick = true
    setTimeout(() => { suppressClick = false }, 0)
  }
  drag = null
  if (railRef.value?.hasPointerCapture?.(e.pointerId)) {
    railRef.value.releasePointerCapture(e.pointerId)
  }
}

/* ── Lifecycle ────────────────────────────────────────────────────────── */
let observer: ResizeObserver | null = null

async function refresh() {
  await nextTick()
  measure()
}

onMounted(() => {
  refresh()
  if (typeof ResizeObserver !== 'undefined') {
    observer = new ResizeObserver(() => measure())
    if (railRef.value) observer.observe(railRef.value)
    const scroller = props.getScroller?.()
    if (scroller) observer.observe(scroller)
  }
})

onBeforeUnmount(() => {
  observer?.disconnect()
  observer = null
  if (hideTimer) clearTimeout(hideTimer)
})

watch(() => props.messages, refresh, { deep: false })

// Keep the active node on screen as the reader scrolls the conversation, and
// re-centre when the list itself changes length.
watch(activeIndex, (idx) => {
  if (idx >= 0) ensureVisible(idx)
})

defineExpose({ refresh })
</script>

<style scoped>
/* ── Rail column ──
   Sits in the pane's right gutter. Above the card (z-index) so the nodes land
   inside the card's right padding and read as one surface with it. */
.msg-rail {
  position: absolute;
  right: calc(12 * var(--dp));
  /* Full panel height — the axis runs all the way down past the input island.
     The height is written from the measured host box (see `railStyle`). */
  top: 0;
  width: calc(34 * var(--dp));
  z-index: 6;
  overflow: hidden;
  touch-action: none;
  /* The node's ring punches this colour; it must match the surface the node is
     drawn on, which is the card once it opens. */
  --msg-row-bg: var(--bg-elevated);
  opacity: 0;
  pointer-events: none;
  transition: opacity var(--duration-slow) ease;
}

.msg-rail--visible {
  opacity: 1;
  pointer-events: auto;
}

/* Scrolling affordance only — the fades below are separate, because a rail that
   CAN scroll is not the same as one that currently has content beyond an edge.
   Fading unconditionally left the last node dimmed after scrolling to the
   bottom, which reads as clipped content. */
.msg-rail--scrollable {
  cursor: grab;
}

.msg-rail--scrollable:active {
  cursor: grabbing;
}

/* Edge fades, each driven by "is there actually an unseen node on this side".
   The fade is only an affordance for hidden content; with none, the edge stays
   fully opaque. */
.msg-rail--more-above {
  -webkit-mask-image: linear-gradient(to bottom, transparent 0, #000 calc(36 * var(--dp)));
  mask-image: linear-gradient(to bottom, transparent 0, #000 calc(36 * var(--dp)));
}

.msg-rail--more-below {
  -webkit-mask-image: linear-gradient(to top, transparent 0, #000 calc(36 * var(--dp)));
  mask-image: linear-gradient(to top, transparent 0, #000 calc(36 * var(--dp)));
}

.msg-rail--more-above.msg-rail--more-below {
  -webkit-mask-image: linear-gradient(to bottom, transparent 0, #000 calc(36 * var(--dp)), #000 calc(100% - 36 * var(--dp)), transparent 100%);
  mask-image: linear-gradient(to bottom, transparent 0, #000 calc(36 * var(--dp)), #000 calc(100% - 36 * var(--dp)), transparent 100%);
}

/* Connector. Drawn as its own layer BEHIND the nodes: a node carries a
   transform (translateY), which opens a stacking context, so a negative
   z-index child would paint on top of the node's own background instead. */
.msg-rail-line {
  position: absolute;
  left: 50%;
  width: calc(2 * var(--dp));
  transform: translateX(-50%);
  /* Stronger than the index drawer's 26%: here the line is drawn over the
     CARD's surfaces (including the lit row's accent tint), where 26% washes out
     to invisible and the axis reads as broken. The node still punches it, so
     the "line runs node to node" look is unchanged. */
  background: color-mix(in srgb, var(--accent-color) 46%, transparent);
  pointer-events: none;
}

/* Node placement is absolute; the row variant's own box carries no padding. */
.msg-rail-node {
  position: absolute;
  left: 50%;
  transform: translate(-50%, -50%);
  z-index: 1;
  width: calc(22 * var(--dp));
  height: calc(22 * var(--dp));
}

/* Enlarge the hit area without changing the visual size. */
.msg-rail-node::after {
  content: '';
  position: absolute;
  inset: calc(-8 * var(--dp)) calc(-10 * var(--dp));
}

/* ── Hover card ── */
.msg-rail-card {
  position: absolute;
  right: calc(12 * var(--dp));
  z-index: 5;
  overflow: hidden;
  border: 1px solid var(--border-color);
  border-radius: var(--radius-lg);
  background: var(--bg-elevated);
  box-shadow: var(--shadow-md);
  opacity: 0;
  transform: translateX(calc(6 * var(--dp))) scale(0.985);
  transform-origin: right center;
  pointer-events: none;
  transition: opacity var(--duration-base) ease, transform var(--duration-base) ease;
  --msg-row-bg: var(--bg-elevated);
}

.msg-rail-card--show {
  opacity: 1;
  transform: none;
  pointer-events: auto;
}

/* The card fades at whichever end still has messages beyond the window. */
.msg-rail-card--more-above {
  -webkit-mask-image: linear-gradient(to bottom, transparent 0, #000 calc(40 * var(--dp)));
  mask-image: linear-gradient(to bottom, transparent 0, #000 calc(40 * var(--dp)));
}

.msg-rail-card--more-below {
  -webkit-mask-image: linear-gradient(to top, transparent 0, #000 calc(40 * var(--dp)));
  mask-image: linear-gradient(to top, transparent 0, #000 calc(40 * var(--dp)));
}

.msg-rail-card--more-above.msg-rail-card--more-below {
  -webkit-mask-image: linear-gradient(to bottom, transparent 0, #000 calc(40 * var(--dp)), #000 calc(100% - 40 * var(--dp)), transparent 100%);
  mask-image: linear-gradient(to bottom, transparent 0, #000 calc(40 * var(--dp)), #000 calc(100% - 40 * var(--dp)), transparent 100%);
}

.msg-rail-row {
  position: absolute;
  left: 0;
  right: 0;
  border-radius: 0;
  /* Leave the right-hand lane free: the node column is painted on top of it. */
  --msg-row-pad-end: calc(46 * var(--dp));
}
</style>
