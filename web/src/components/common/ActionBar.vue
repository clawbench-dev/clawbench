<template>
  <div
    ref="rootRef"
    class="ab"
    :class="[
      {
        'ab--labels': showLabels,
        'ab--scroll': mode === 'scroll',
        'ab--overflowing': overflowing,
        'ab-dragging': dragging,
        'ab--end': align === 'end',
        'ab--header': size === 'header',
        'ab--bar': size === 'bar',
      },
    ]"
    :style="gap != null ? { gap: `calc(${gap} * var(--dp))` } : undefined"
    @scroll.passive="onScroll"
  >
    <slot />

    <!-- Menu mode: buttons that do not fit are hidden and offered here. -->
    <button
      v-if="mode === 'menu' && (hiddenItems.length > 0 || alwaysMore)"
      ref="moreBtnRef"
      class="ab-more"
      data-ab-more
      :title="moreText"
      :aria-label="moreText"
      @click.stop="menuOpen = !menuOpen"
    >
      <MoreHorizontal :size="16" />
    </button>

    <!-- Mounted only when there is something to show: PopupMenu teleports to
         <body>, and an always-present teleport breaks tests (and any host) that
         reset the body between mounts. -->
    <PopupMenu
      v-if="mode === 'menu' && (alwaysMore || hiddenItems.length > 0)"
      v-model:show="menuOpen"
      :target-element="moreBtnRef"
      :menu-items-count="hiddenItems.length + 4"
      anchor="right"
    >
      <button
        v-for="item in hiddenItems"
        :key="item.key"
        class="ab-menu-item"
        :disabled="item.disabled"
        @click="runItem(item)"
      >
        <span class="ab-menu-icon" v-html="item.iconHtml" />
        <span class="ab-menu-text">{{ item.label }}</span>
      </button>
      <!-- Permanent menu entries the caller owns (e.g. a file's "delete"),
           rendered in the same popup so there is one "more" affordance. -->
      <slot name="menu-extra" />
    </PopupMenu>
  </div>
</template>

<script setup lang="ts">
/**
 * ActionBar — the one control for a row of icon buttons.
 *
 * Three behaviours live here and nowhere else:
 *   1. labels: shown only when the row can hold them (`.action-label` spans in
 *      the slotted buttons, toggled by `.ab--labels` on the root);
 *   2. overflow: `mode="menu"` hides the buttons that do not fit and lists them
 *      in a "more" menu; `mode="scroll"` keeps them all and lets the row scroll;
 *   3. scroll affordances (scroll mode): drag-to-scroll with a mask fade on the
 *      side that still has hidden content.
 *
 * Call sites only provide plain `<button>`s; each button that wants a label puts
 * `<span class="action-label">…</span>` inside it. The width a labelled button
 * needs is therefore measured from the real DOM, not estimated.
 *
 * The gap between buttons is owned here too. It used to be left to each host's
 * CSS and one row ended up at 0: as soon as a button's background lit up (hover
 * or toggled on) the two tints touched and the pair read as a single control.
 *
 * Design notes: docs/spec/client/design-guide.md
 * 「图标按钮：空间够就带上文字标签」.
 */
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { MoreHorizontal } from 'lucide-vue-next'
import PopupMenu from '@/components/common/PopupMenu.vue'
import { attachDragScroll, canDragScroll } from '@/utils/dragScroll'

const props = withDefaults(
  defineProps<{
    /** How the row degrades when it runs out of room. */
    mode?: 'menu' | 'scroll'
    /** Gap between buttons, in design px. Defaults to the shared row rhythm;
     *  pass 0 for a keyboard-like strip whose keys are meant to touch. */
    gap?: number
    /** Width reserved for the "more" button, in design px. */
    moreWidth?: number
    /** Accessible name of the "more" button. Defaults to the shared "more". */
    moreLabel?: string
    /** Which edge the buttons hug when the row has spare room. */
    align?: 'start' | 'end'
    /**
     * Visual weight of the row. `header` = panel/app header bars, `bar` = a tool
     * strip inside content, `custom` = the bar keeps its own button geometry
     * (chat composer, terminal keys — they follow a different rhythm on purpose).
     * Both shared sizes are declared once in web/css/components.css so a new bar
     * cannot invent a third one.
     */
    size?: 'header' | 'bar' | 'custom'
    /**
     * Render the "more" button even when nothing is folded — for bars that own
     * permanent menu entries (a file's delete, details, …).
     */
    alwaysMore?: boolean
  }>(),
  {
    mode: 'menu',
    gap: 8,
    moreWidth: 28,
    moreLabel: '',
    align: 'start',
    size: 'custom',
    alwaysMore: false,
  },
)

const { t } = useI18n()
const moreText = computed(() => props.moreLabel || t('nav.more'))

const rootRef = ref<HTMLElement | null>(null)
const moreBtnRef = ref<HTMLElement | null>(null)

const showLabels = ref(false)
const overflowing = ref(false)
const dragging = ref(false)
const menuOpen = ref(false)

interface HiddenItem {
  key: number
  label: string
  iconHtml: string
  disabled: boolean
  run: () => void
}

const hiddenItems = ref<HiddenItem[]>([])

let stopDrag: (() => void) | null = null
let observer: ResizeObserver | null = null
let frame = 0
let lastWidth = -1

/** Spare room required before labels are turned on (CSS px). */
const TURN_ON_SLACK_PX = 12

function gapPx(el: HTMLElement): number {
  const gap = getComputedStyle(el).columnGap
  return parseFloat(gap) || 0
}

function toggleMenu(): void {
  menuOpen.value = !menuOpen.value
}

defineExpose({ measure, schedule, toggleMenu, menuOpen })

function designPx(value: number): number {
  // `--dp` is `1rem / 15`; read it so the reserve follows the UI scale.
  const root = document.documentElement
  const rem = parseFloat(getComputedStyle(root).fontSize) || 15
  return (rem / 15) * value
}

/** Everything the slot rendered, in DOM order, minus the bar's own "more". */
function items(): HTMLElement[] {
  const root = rootRef.value
  if (!root) return []
  return (Array.from(root.children) as HTMLElement[]).filter(el => !el.hasAttribute('data-ab-more'))
}

const isButton = (el: HTMLElement) => el.tagName === 'BUTTON'

/** The slotted buttons, in DOM order. */
function buttons(): HTMLElement[] {
  return items().filter(isButton)
}

/**
 * One synchronous pass: decide how many buttons stay inline, then whether the
 * labels of those buttons fit. Both decisions are made with the labels forced
 * on, so the answer never depends on the state we are about to set.
 */
function measure(): void {
  const root = rootRef.value
  if (!root) return

  const kids = buttons()
  const fixed = items().filter(el => !isButton(el))
  if (kids.length === 0) {
    hiddenItems.value = []
    showLabels.value = false
    // A row may legitimately hold no direct buttons — the code preview groups
    // them in a wrapper. Scroll mode still has to report its overflow, or the
    // fade hints and drag-to-scroll never turn on and the row just clips.
    updateScrollState()
    return
  }

  const gap = gapPx(root)
  // Content box, not border box: the bar's own horizontal padding is not room
  // the buttons can use, and counting it lets a labelled row overflow by exactly
  // that much.
  const cs = getComputedStyle(root)
  const innerWidth =
    root.clientWidth - (parseFloat(cs.paddingLeft) || 0) - (parseFloat(cs.paddingRight) || 0)
  if (innerWidth <= 0) {
    // Not laid out yet (hidden pane, jsdom): keep every button and assume room.
    // Clear the scroll flags too — a hidden bar must not keep advertising
    // "there is more this way" from the last time it was visible.
    kids.forEach(k => { k.style.display = '' })
    hiddenItems.value = []
    showLabels.value = true
    overflowing.value = false
    return
  }

  // A bar may also hold a title/breadcrumb. Pin those to their natural width
  // while measuring: otherwise they would shrink to absorb the buttons and the
  // row would always look like it fits.
  const pinned = fixed.map(el => {
    const prev = el.style.flex
    el.style.flex = '0 0 auto'
    return { el, prev }
  })
  const fixedWidth =
    fixed.reduce((sum, el) => sum + el.getBoundingClientRect().width, 0) + gap * fixed.length
  const availableWidth = Math.max(0, innerWidth - fixedWidth)

  // ── 1. Which buttons stay inline (icon widths) ──
  // Both classes must be off: if the labels are currently showing, their widths
  // would be measured as "icon width" and the row would fold far too early.
  const hadLabels = root.classList.contains('ab--labels')
  root.classList.remove('ab-measuring', 'ab--labels')
  kids.forEach(k => { k.style.display = '' })
  const widths = kids.map(k => k.getBoundingClientRect().width)
  if (hadLabels) root.classList.add('ab--labels')

  let visibleCount = kids.length
  let hidden: HTMLElement[] = []
  if (props.mode === 'menu') {
    const total = widths.reduce((a, b) => a + b, 0) + gap * (kids.length - 1)
    if (total > availableWidth) {
      const reserve = designPx(props.moreWidth) + gap
      const available = Math.max(0, availableWidth - reserve)
      let used = 0
      visibleCount = 0
      for (let i = 0; i < kids.length; i++) {
        const add = widths[i] + (i > 0 ? gap : 0)
        if (used + add > available) break
        used += add
        visibleCount++
      }
      // At least one button stays: a row of only "more" is useless.
      visibleCount = Math.max(1, visibleCount)
      hidden = kids.slice(visibleCount)
    }
  }
  kids.forEach((k, i) => { k.style.display = i < visibleCount ? '' : 'none' })

  // ── 2. Do the labels of the inline buttons fit? ──
  // Measure EVERY child, not just the buttons: the gaps between them, the fixed
  // children (a breadcrumb, a sort dropdown) and the bar's own padding all take
  // room. Adding up button widths alone missed those terms and let a row turn its
  // labels on while it was still a couple of pixels too wide.
  //
  // BOTH classes are required: `ab-measuring` only turns transitions off, while
  // the bars' own "grow when labelled" rules key on `.ab--labels`. Forcing only
  // the former measured icon widths, concluded "the labels fit", and turned them
  // on over a row that then stuck out of the bar.
  root.classList.add('ab--labels', 'ab-measuring')
  const all = items()
  const neededWithLabels =
    all.reduce((sum, el) => sum + el.getBoundingClientRect().width, 0) +
    gap * Math.max(0, all.length - 1)
  root.classList.remove('ab--labels', 'ab-measuring')
  if (hadLabels) root.classList.add('ab--labels')

  const overflow = neededWithLabels - innerWidth
  // Asymmetric on purpose: a bar dragged across the boundary would otherwise
  // flip at every pixel (on → 2px narrower → off → on).
  const roomForLabels = showLabels.value ? overflow <= 0 : overflow <= -TURN_ON_SLACK_PX
  // Degradation order is labels → icons → more menu: labels are the FIRST thing
  // sacrificed. A row that already had to fold buttons away is by definition
  // short of room, so showing labels on the few survivors would read as "there
  // is space to spare" while most of the toolbar is hidden in the menu.
  showLabels.value = hidden.length > 0 ? false : roomForLabels

  pinned.forEach(({ el, prev }) => { el.style.flex = prev })

  // ── 3. Menu items + scroll affordances ──
  hiddenItems.value = hidden
    .filter(el => el.querySelector('.action-label') || el.getAttribute('title'))
    .map((el, i) => ({
    key: i,
    label: el.querySelector('.action-label')?.textContent?.trim() || el.getAttribute('title') || '',
    iconHtml: el.querySelector('svg')?.outerHTML || '',
    disabled: (el as HTMLButtonElement).disabled === true,
    run: () => el.click(),
  }))
  if (hiddenItems.value.length === 0 && !props.alwaysMore) menuOpen.value = false

  updateScrollState()
}

function updateScrollState(): void {
  const root = rootRef.value
  if (!root || props.mode !== 'scroll') {
    overflowing.value = false
    return
  }
  overflowing.value = root.scrollWidth > root.clientWidth + 1
  updateFades()
  syncDrag()
}

function syncDrag(): void {
  const root = rootRef.value
  if (!root) return
  if (overflowing.value && !stopDrag) {
    stopDrag = attachDragScroll(root)
    root.addEventListener('pointerdown', onPointerDown)
    root.addEventListener('pointerup', onPointerUp)
    root.addEventListener('pointercancel', onPointerUp)
  } else if (!overflowing.value && stopDrag) {
    stopDrag()
    stopDrag = null
    root.removeEventListener('pointerdown', onPointerDown)
    root.removeEventListener('pointerup', onPointerUp)
    root.removeEventListener('pointercancel', onPointerUp)
    dragging.value = false
  }
}

function onPointerDown(e: PointerEvent): void {
  const root = rootRef.value
  if (!root || !canDragScroll(e.pointerType, e.button, root.scrollWidth, root.clientWidth)) return
  dragging.value = true
}

function onPointerUp(): void {
  dragging.value = false
}

function onScroll(): void {
  if (props.mode === 'scroll') updateFades()
}

// Fades are applied to the track as data attributes so the mask stays in CSS.
function updateFades(): void {
  const root = rootRef.value
  if (!root) return
  root.dataset.fadeLeft = root.scrollLeft > 1 ? '1' : '0'
  root.dataset.fadeRight = root.scrollLeft + root.clientWidth < root.scrollWidth - 1 ? '1' : '0'
}

function schedule(): void {
  if (frame) return
  frame = requestAnimationFrame(() => {
    frame = 0
    measure()
    updateFades()
  })
}

function onResize(): void {
  const width = rootRef.value?.clientWidth ?? 0
  if (lastWidth >= 0 && Math.abs(width - lastWidth) < 2) return
  lastWidth = width
  schedule()
}

function runItem(item: HiddenItem): void {
  menuOpen.value = false
  item.run()
}

onMounted(() => {
  measure()
  updateFades()
  lastWidth = rootRef.value?.clientWidth ?? -1
  if (typeof ResizeObserver !== 'undefined' && rootRef.value) {
    observer = new ResizeObserver(onResize)
    observer.observe(rootRef.value)
  }
  if (typeof MutationObserver !== 'undefined' && rootRef.value) {
    childObserver = new MutationObserver(() => schedule())
    childObserver.observe(rootRef.value, { childList: true })
  }
})

onBeforeUnmount(() => {
  observer?.disconnect()
  observer = null
  childObserver?.disconnect()
  childObserver = null
  if (frame) cancelAnimationFrame(frame)
  stopDrag?.()
  stopDrag = null
  const root = rootRef.value
  root?.removeEventListener('pointerdown', onPointerDown)
  root?.removeEventListener('pointerup', onPointerUp)
  root?.removeEventListener('pointercancel', onPointerUp)
})

// Conditional buttons appear/disappear WITHOUT resizing the bar, so the
// ResizeObserver never fires for them. A `watch` on a computed child count does
// not work either: reading the DOM inside the getter tracks nothing reactive.
// A MutationObserver is what actually sees the slot content change.
let childObserver: MutationObserver | null = null
</script>

<style scoped>
.ab {
  display: flex;
  align-items: center;
  min-width: 0;
  overflow: hidden;
}
/* Right-aligned bars (detail headers) hug the trailing edge; the "more" button
   then sits last, which is where the eye already looks for it. */
.ab--end {
  justify-content: flex-end;
}

/* Scroll mode: the row itself scrolls; buttons are never hidden. */
.ab--scroll {
  overflow-x: auto;
  overflow-y: hidden;
  scrollbar-width: none;
  /* Always start at the scroll origin. With `justify-content: flex-end` the
     overflow lands on the START side, where the scrollport cannot reach it —
     measured: the element even reports scrollWidth === clientWidth, so the
     first buttons are clipped and no amount of scrolling brings them back.
     A scroll row therefore ignores `align`. */
  justify-content: flex-start;
}
.ab--scroll::-webkit-scrollbar {
  display: none;
}
.ab--scroll.ab--overflowing {
  cursor: grab;
}
.ab--scroll.ab-dragging {
  cursor: grabbing;
  user-select: none;
}

/* Fade the side that still has content behind it. A mask, not an overlay: the
   bars sit on translucent surfaces (wallpaper), so a painted gradient would show
   a second colour. */
.ab--scroll[data-fade-left='1'] {
  -webkit-mask-image: linear-gradient(to right, transparent 0, #000 calc(20 * var(--dp)));
  mask-image: linear-gradient(to right, transparent 0, #000 calc(20 * var(--dp)));
}
.ab--scroll[data-fade-right='1'] {
  -webkit-mask-image: linear-gradient(to left, transparent 0, #000 calc(20 * var(--dp)));
  mask-image: linear-gradient(to left, transparent 0, #000 calc(20 * var(--dp)));
}
.ab--scroll[data-fade-left='1'][data-fade-right='1'] {
  -webkit-mask-image: linear-gradient(
    to right,
    transparent 0,
    #000 calc(20 * var(--dp)),
    #000 calc(100% - 20 * var(--dp)),
    transparent 100%
  );
  mask-image: linear-gradient(
    to right,
    transparent 0,
    #000 calc(20 * var(--dp)),
    #000 calc(100% - 20 * var(--dp)),
    transparent 100%
  );
}

/* Shape (height, radius) and colour come from the shared ActionBar rules in
   web/css/components.css — they must match the row's buttons, and a scoped rule
   here would be a second copy of the same numbers. */
.ab-more {
  display: flex;
  align-items: center;
  justify-content: center;
  flex-shrink: 0;
  padding: 0 calc(6 * var(--dp));
  border: none;
  background: transparent;
  color: var(--text-secondary);
  cursor: pointer;
}
@media (hover: hover) {
  .ab-more:hover {
    background: color-mix(in srgb, var(--text-primary) 12%, transparent);
  }
}

</style>
