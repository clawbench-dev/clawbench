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
      'msg-rail--engaged': engaged,
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
    <template v-for="(it, k) in items" :key="'n' + it.key">
      <!-- Pending slot reuses the numbered node as-is: it carries the ordinal
           the reply will get, so it needs no styling of its own. Clicking it
           jumps to the reply in progress. The `--pending` class is a marker
           only (no rules) — it exists so tests can address the slot. -->
      <MessageIndexRow
        variant="node"
        class="msg-rail-node"
        :class="{ 'msg-item--lit': litIndex === k, 'msg-rail-node--pending': it.pending }"
        :style="{ top: nodeTop(k) + 'px' }"
        :msg="it.msg"
        :index="it.ordinal"
        :active="isActive(it)"
        :resolve-speaker="resolveSpeaker"
        @select="it.pending ? onPendingSelect() : onSelect(it.msg)"
      />
    </template>
  </div>

  <div
    v-if="items.length"
    ref="cardRef"
    class="msg-rail-card"
    :class="{ 'msg-rail-card--show': expanded }"
    :style="cardStyle"
    role="dialog"
    :aria-label="t('chat.messageList.conversationIndexTitle')"
    @mouseenter="onEnter"
    @mousemove="onPointerMove"
    @mouseleave="scheduleCollapse"
    @wheel="onWheel"
  >
    <!-- The edge fades live on this inner layer, NOT on the card: a mask clips
         everything outside the element's box, including its own box-shadow,
         which left the card visually flat against the assistant bubbles behind
         it. Keeping the shadow on the outer box and the fade on this one lets
         both exist. -->
    <div
      class="msg-rail-card-body"
      :class="{
        'msg-rail-card-body--more-above': moreAbove,
        'msg-rail-card-body--more-below': moreBelow,
      }"
    >
      <template v-for="(it, k) in items" :key="'r' + it.key">
        <MessageIndexRow
          variant="body"
          class="msg-rail-row"
          :class="{ 'msg-item--lit': litIndex === k, 'msg-rail-row--pending': it.pending }"
          :style="{ top: rowTop(k) + 'px', height: gap + 'px' }"
          :msg="it.msg"
          :index="it.ordinal"
          :active="isActive(it)"
          :has-btw="hasBtw(it.msg)"
          :resolve-speaker="resolveSpeaker"
          @select="it.pending ? onPendingSelect() : onSelect(it.msg)"
        />
      </template>
    </div>
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
  /**
   * Whether an assistant reply is being written right now.
   *
   * The conversation index only lists FINALIZED messages, so without this the
   * rail would look complete while a reply is still streaming — the reader sees
   * no indication that a turn is in flight and the axis ends one row early.
   * Rendered as a trailing slot that reuses the ordinary numbered node — it
   * carries the ordinal the reply will get, so it needs no styling of its own.
   */
  generating?: boolean
}>(), {
  activeId: null,
  resolveSpeaker: null,
  hasBtw: () => false,
  getScroller: () => null,
  bottomInset: 0,
  generating: false,
})

const emit = defineEmits<{
  select: [msg: RailIndexMsg]
  /** The reader clicked the in-flight reply's node/row. */
  'select-pending': []
}>()

const { t } = useI18n()

/** Design px the rail needs on each side of the reading measure before it appears. */
const MIN_GUTTER_DP = 60
/** Card width. The rail column's own geometry lives in the stylesheet. */
const CARD_WIDTH_DP = 244
/**
 * The card's border, in CSS px. A hairline stays a literal 1px (design rule), and
 * `box-sizing: border-box` means it eats into the card's content box — the row
 * count must account for it or the last row is clipped by exactly this much.
 */
const CARD_BORDER = 1
/**
 * Distance from the pane's top and start edges, in design px.
 *
 * One number for both axes: the card is a floating surface inset from the pane,
 * so an asymmetric gap reads as a layout mistake rather than as intent. The
 * stylesheet carries the same value as `left` — CSS cannot read this constant,
 * so the two must be changed together.
 */
const RAIL_INSET_DP = 12

const items = computed(() => {
  const list = props.messages.map((msg, i) => ({
    // `key` must survive a re-fetch of the index: message ids are stable, but the
    // fallback position is not, so it is only used when the row has no id.
    key: msg.id != null && msg.id !== '' ? String(msg.id) : `i${i}`,
    ordinal: i + 1,
    msg,
    pending: false,
  }))
  if (!props.generating) return list
  // Trailing slot for the reply being written. It takes the ordinal the reply
  // will get and a synthetic row whose text is the "replying" label, so both
  // layers can be rendered by MessageIndexRow with no bespoke styling.
  return [...list, {
    key: 'pending',
    ordinal: list.length + 1,
    msg: { id: '', role: 'assistant', summary: t('chat.messageList.messageRailGenerating') } as RailIndexMsg,
    pending: true,
  }]
})

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
 * height minus the inset at the top and the composer at its foot.
 *
 * The top inset is subtracted here, not just applied to the card's `top`: the
 * card must be able to grow DOWN to the composer, and reserving the inset only
 * when positioning would let its bottom edge run past the band by exactly that
 * amount.
 */
const band = computed(() =>
  Math.max(0, trackHeight.value - props.bottomInset - railInset()),
)
/**
 * Card height, snapped to WHOLE rows.
 *
 * A card sized to the raw band would end mid-row, leaving an arbitrary sliver of
 * empty space at the bottom — visibly more than the top padding, which reads as
 * a layout mistake rather than as "there is more below" (the edge fade already
 * says that). Rounding to `pad + k·gap + pad` keeps the top and bottom padding
 * identical, and guarantees the last visible row is never cut in half.
 */
const cardHeight = computed(() => {
  const content = listContent()
  if (content + CARD_BORDER * 2 <= band.value) return content + CARD_BORDER * 2
  // Only WHOLE rows fit: `band` minus both borders minus both paddings, divided
  // by the row height. Without the border terms the last row overflows the
  // content box and gets clipped in half.
  const usable = band.value - CARD_BORDER * 2 - listPad() * 2
  const rows = Math.max(1, Math.floor(usable / gap.value))
  return CARD_BORDER * 2 + listPad() * 2 + rows * gap.value
})
const cardTop = computed(() => trackTop.value + railInset())

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
  // `top` and `left` are the same number on purpose: the card is inset from the
  // pane, and an asymmetric gap reads as a mistake. Both live here so there is
  // one place to change.
  top: `${cardTop.value}px`,
  left: `${railInset()}px`,
  width: `${designToCss(CARD_WIDTH_DP)}px`,
  height: `${cardHeight.value}px`,
}))

/**
 * The axis is the card's box, exactly: same top, same height.
 *
 * Deriving both from the SNAPPED card height (not the raw band) matters — a
 * taller rail would put numbers below the card's last row, which read as an
 * orphan with nothing beside it, and a different top would offset every number
 * from the row it labels.
 */
const railStyle = computed(() => ({
  top: `${cardTop.value}px`,
  left: `${railInset()}px`,
  height: `${cardHeight.value}px`,
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
 * The rail box is positioned at the card's own top and given its height, so both
 * layers share an origin and no extra term is needed here — `railInset` is
 * already baked into `cardTop`, which the rail's `top` uses.
 */
function nodeTop(k: number) {
  // The rail box is the card's OUTER box (it starts at `cardTop`), while the
  // rows sit in the card's padding box. Adding the border keeps a node's centre
  // exactly on its row's centre instead of 1px above it.
  return CARD_BORDER + listPad() + k * gap.value + gap.value / 2 - offset.value
}

/**
 * Card-relative top of row k.
 *
 * Absolutely positioned children are placed against the PADDING box, so the
 * border already offsets them: no border term here.
 */
function rowTop(k: number) {
  return listPad() + k * gap.value - offset.value
}

/**
 * The card's inset from the pane's top edge — the SAME number as its `left`.
 *
 * It used to centre the card when the list was shorter than the track, which
 * made the top gap depend on the row count: a list that happened to fill the
 * band left a gap of `(band − cardHeight)/2`, anywhere from 0 to half a row,
 * while the left gap stayed fixed at 12. Flush at the top with 12 on the left
 * reads as a mistake, and for short conversations the axis floated in the middle
 * of the pane. Both are gone: the card now hangs from a fixed inset.
 */
function railInset() {
  return designToCss(RAIL_INSET_DP)
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

  // The scrollable viewport is the card's PADDING box — its height minus both
  // borders. Using the outer height leaves the list 2px short of its end, so at
  // full scroll the bottom padding comes out 2px larger than the top one.
  //
  // Read `cardHeight` HERE, after the node size above: the height is derived
  // from `gap`, which that measurement may have just changed. A stale gap makes
  // the row count — and therefore the scroll range — one row short, so the
  // newest node lands just below the clip edge and stays invisible.
  const viewport = Math.max(0, cardHeight.value - CARD_BORDER * 2)
  maxOffset.value = Math.max(0, listContent() - viewport)
  offset.value = clamp(offset.value)

  const scroller = props.getScroller?.()
  if (scroller) {
    // The right inset is exactly the room between the reading measure and the
    // pane edge (already net of the scrollbar lane). Reading it from the
    // scroller rather than recomputing the measure keeps the two in step.
    // The rail lives in the START gutter now, so that is the one that must be
    // wide enough. `paddingInlineStart` also covers RTL, where the physical left
    // side is the END edge and the axis would land on top of the messages.
    const inset = parseFloat(getComputedStyle(scroller).paddingInlineStart) || 0
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

/**
 * Keep node `index` inside the visible window, moving only when it is actually
 * out of view.
 *
 * Recomputes the scroll range itself instead of trusting `maxOffset`: callers
 * reach here right after the list GREW (a new turn's pending slot), and a range
 * measured against the shorter list is one row short — `setOffset` would then
 * clamp to the stale maximum and leave the new node just below the edge.
 */
function ensureVisible(index: number) {
  if (index < 0 || items.value.length === 0) return
  const viewport = Math.max(0, cardHeight.value - CARD_BORDER * 2)
  const range = Math.max(0, listContent() - viewport)
  if (range <= 0) return
  maxOffset.value = range
  const top = listPad() + index * gap.value - offset.value
  const bottom = top + gap.value
  if (top < 0) setOffset(offset.value + top)
  else if (bottom > viewport) setOffset(offset.value + bottom - viewport)
}

const activeIndex = computed(() => {
  const id = props.activeId
  if (id == null || id === '') return -1
  return items.value.findIndex(it => !it.pending && String(it.msg.id) === String(id))
})

/**
 * Which node carries the "you are here" highlight.
 *
 * While a reply streams that is the pending slot, not the last finished
 * message: the axis follows the tail, so the slot IS the newest row, and
 * leaving it unhighlighted made it the only node on the axis without the mark
 * — it read as "not a real message" rather than as "this one is being written".
 * The finished message loses the mark at the same time, so exactly one node is
 * ever highlighted.
 */
function isActive(it: { msg: RailIndexMsg; pending: boolean }) {
  if (props.generating) return it.pending
  const id = props.activeId
  if (id == null || id === '') return false
  return String(it.msg.id) === String(id)
}

/* ── Hover ────────────────────────────────────────────────────────────── */
const expanded = ref(false)
/**
 * Whether the reader is currently engaging with the rail (pointer over the axis
 * or its card). Drives the resting→full opacity change. Separate from
 * `expanded` because the two layers are siblings: `:hover` on the rail alone
 * would drop the axis back to its quiet state as soon as the pointer crossed
 * onto the card.
 */
const engaged = ref(false)
const litIndex = ref(-1)
let pointerY = 0
let hideTimer: ReturnType<typeof setTimeout> | null = null

function nearestIndex(clientY: number) {
  const rail = railRef.value
  if (!rail || items.value.length === 0) return -1
  // The rail box is the card's outer box, so the card's border is part of the
  // distance to the first row.
  const base = rail.getBoundingClientRect().top + CARD_BORDER + listPad() + gap.value / 2 - offset.value
  const raw = Math.round((clientY - base) / gap.value)
  return Math.max(0, Math.min(items.value.length - 1, raw))
}

function relight() {
  const idx = nearestIndex(pointerY)
  if (idx >= 0) litIndex.value = idx
}

function onEnter() {
  if (hideTimer) { clearTimeout(hideTimer); hideTimer = null }
  engaged.value = true
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
    engaged.value = false
    litIndex.value = -1
  }, 140)
}

function onSelect(msg: RailIndexMsg) {
  if (suppressClick) return
  expanded.value = false
  litIndex.value = -1
  emit('select', msg)
}

/**
 * Jump to the reply that is still being written.
 *
 * The pending slot has no index row (the API excludes in-flight messages), so
 * there is nothing to pass to `select`. The host knows which message is
 * streaming, so it gets a dedicated event instead of a fabricated payload.
 */
function onPendingSelect() {
  if (suppressClick) return
  expanded.value = false
  litIndex.value = -1
  emit('select-pending')
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

/**
 * Re-layout when the index itself grows.
 *
 * `refresh` only re-measures; the scroll offset must also FOLLOW the new tail,
 * otherwise a message that just landed (or the pending slot that preceded it)
 * is appended below the clip edge and the reader never sees it. Only when the
 * list got LONGER — shrinking (session switch, filter) must not move the view.
 */
watch(() => props.messages.length, async (next, prev) => {
  await refresh()
  if (next <= prev) return
  requestAnimationFrame(() => {
    measure()
    ensureVisible(items.value.length - 1)
  })
})

// Keep the active node on screen as the reader scrolls the conversation, and
// re-centre when the list itself changes length.
watch(activeIndex, (idx) => {
  if (idx < 0) return
  // While a reply streams the axis follows the TAIL, not the active message.
  // The active message is the last FINALIZED one, so tracking it parked its row
  // on the bottom edge — which left the pending slot exactly one row lower,
  // i.e. outside the clip box: invisible, and `elementFromPoint` at its centre
  // hit the panel behind it, so clicking it did nothing. Following the reader
  // instead would also make the slot vanish as soon as they scrolled up the
  // conversation, which is precisely when they reach for it. The slot is an
  // affordance ("take me back to the reply"), so it must stay on screen.
  ensureVisible(props.generating ? items.value.length - 1 : idx)
})

/**
 * Keep the pending slot on screen while a reply streams.
 *
 * A new reply is appended at the tail, which is usually BELOW the visible band —
 * without this the spinner is created outside the rail's clipped box and the
 * reader never sees it (the whole point of the slot). Runs after the layout has
 * accounted for the extra item, so `ensureVisible` measures the real range.
 */
watch(() => props.generating, async (on) => {
  if (!on) return
  // Two frames: the pending slot is a new ITEM, so the first render only adds it
  // to the list; `measure()` on the following frame sees the grown content height
  // and recomputes `maxOffset`, which `ensureVisible` needs (it bails out while
  // the range is 0).
  await nextTick()
  requestAnimationFrame(() => {
    measure()
    ensureVisible(items.value.length - 1)
  })
})

defineExpose({ refresh })
</script>

<style scoped>
/* ── Rail column ──
   Sits in the pane's LEFT gutter (the side the reading column leaves free, and
   the side the message list itself starts from, so the axis reads as its margin
   rather than as a floating widget on the far edge).
   Above the card (z-index) so the nodes land inside the card's own padding and
   read as one surface with it. */
.msg-rail {
  position: absolute;
  /* `top`, `left` and `height` all come from `railStyle`: the inset has to be
     the same number on both axes, and CSS cannot read the JS constant. */

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
  /* Resting state: present but quiet. The axis is a permanent fixture in the
     gutter, so at full opacity it competes with the conversation for attention;
     a reader only needs it at full strength once they reach for it. */
  opacity: 0.45;
  pointer-events: auto;
  transition: opacity var(--duration-base) ease;
}

/* Full strength while the reader is engaging with it (pointer over the axis or
   its card). Driven by the `engaged` state, not by `:hover`, because the card is
   a SIBLING of the rail — `:hover` on the rail would drop to 0.45 the moment
   the pointer moved onto the card. */
.msg-rail--visible.msg-rail--engaged {
  opacity: 1;
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
  /* Starts at the SAME x as the axis: the numbers are the card's left column, so
     the card must own that lane (its rows leave room for it) rather than sit
     beside it as a separate panel. `top`/`left` come from `cardStyle` so the two
     insets are one number. */
  z-index: 5;
  overflow: hidden;
  border: 1px solid var(--border-color);
  border-radius: var(--radius-lg);
  background: var(--bg-elevated);
  /* A popover over scrolling content needs a shadow, not just a border: the
     chat bubbles behind it are the same surface family, so a hairline alone
     does not separate the two. `--shadow-lg` (not md) because this panel is
     tall and wide enough that a soft small shadow disappears against the
     conversation. */
  box-shadow: var(--shadow-lg);
  opacity: 0;
  /* Slides in from the axis: the card sits to the RIGHT of the rail now, so it
     starts a little to the left of its resting place. */
  transform: translateX(calc(-6 * var(--dp))) scale(0.985);
  transform-origin: left center;
  pointer-events: none;
  transition: opacity var(--duration-base) ease, transform var(--duration-base) ease;
  --msg-row-bg: var(--bg-elevated);
}

.msg-rail-card--show {
  opacity: 1;
  transform: none;
  pointer-events: auto;
}

/* Inner layer: holds the rows and the edge fades.
   The mask must NOT live on `.msg-rail-card` — a mask clips everything outside
   the element's own box, box-shadow included, so the card lost the very shadow
   that separates it from the assistant bubbles scrolling behind it. */
.msg-rail-card-body {
  position: absolute;
  inset: 0;
  overflow: hidden;
  border-radius: inherit;
}

/* The card fades at whichever end still has messages beyond the window. */
.msg-rail-card-body--more-above {
  -webkit-mask-image: linear-gradient(to bottom, transparent 0, #000 calc(40 * var(--dp)));
  mask-image: linear-gradient(to bottom, transparent 0, #000 calc(40 * var(--dp)));
}

.msg-rail-card-body--more-below {
  -webkit-mask-image: linear-gradient(to top, transparent 0, #000 calc(40 * var(--dp)));
  mask-image: linear-gradient(to top, transparent 0, #000 calc(40 * var(--dp)));
}

.msg-rail-card-body--more-above.msg-rail-card-body--more-below {
  -webkit-mask-image: linear-gradient(to bottom, transparent 0, #000 calc(40 * var(--dp)), #000 calc(100% - 40 * var(--dp)), transparent 100%);
  mask-image: linear-gradient(to bottom, transparent 0, #000 calc(40 * var(--dp)), #000 calc(100% - 40 * var(--dp)), transparent 100%);
}

.msg-rail-row {
  position: absolute;
  left: 0;
  right: 0;
  border-radius: 0;
  /* The index drawer's 5% hover tint is calibrated for a WIDE list row where the
     pointer is already there. Here the row is only lit as a side effect of
     hovering the axis, over the chat's own surfaces, so it needs to be
     unmistakable: the accent tint matches the node that lights up beside it. */
  --msg-row-lit-strength: 14%;
  /* Leave the LEFT lane free: the node column is painted on top of it.
     `--msg-row-pad-end` keeps the preview's right rhythm; declaring it here (as
     the host that owns the row) is what makes it a real token — an undeclared
     custom property is invisible to the theme guard and to any future re-theme. */
  --msg-row-pad-start: calc(46 * var(--dp));
  --msg-row-pad-end: var(--space-4);
}
</style>
