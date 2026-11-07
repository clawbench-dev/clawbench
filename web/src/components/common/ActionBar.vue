<template>
  <div
    ref="rootRef"
    class="ab"
    :class="{ 'ab--labels': showLabels, 'ab--scroll': mode === 'scroll', 'ab--overflowing': overflowing, 'ab-dragging': dragging }"
    :style="{ gap: `calc(${gap} * var(--dp))` }"
    @scroll.passive="onScroll"
  >
    <slot />

    <!-- Menu mode: buttons that do not fit are hidden and offered here. -->
    <button
      v-if="mode === 'menu' && hiddenItems.length > 0"
      ref="moreBtnRef"
      class="ab-more"
      data-ab-more
      :title="moreText"
      :aria-label="moreText"
      @click.stop="menuOpen = !menuOpen"
    >
      <MoreHorizontal :size="16" />
    </button>

    <PopupMenu
      v-model:show="menuOpen"
      :target-element="moreBtnRef"
      :menu-items-count="hiddenItems.length"
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
 * Design notes: docs/spec/client/design-guide.md
 * 「图标按钮：空间够就带上文字标签」.
 */
import { computed, defineAsyncComponent, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { MoreHorizontal } from 'lucide-vue-next'
// Loaded only when the menu opens: PopupMenu pulls the settings/i18n chain,
// and every test that mounts a bar would otherwise have to mock all of it.
const PopupMenu = defineAsyncComponent(() => import('@/components/common/PopupMenu.vue'))
import { attachDragScroll, canDragScroll } from '@/utils/dragScroll'

const props = withDefaults(
  defineProps<{
    /** How the row degrades when it runs out of room. */
    mode?: 'menu' | 'scroll'
    /** Gap between buttons, in design px (`--dp` units). */
    gap?: number
    /** Width reserved for the "more" button, in design px. */
    moreWidth?: number
    /** Accessible name of the "more" button. Defaults to the shared "more". */
    moreLabel?: string
  }>(),
  {
    mode: 'menu',
    gap: 6,
    moreWidth: 28,
    moreLabel: '',
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
    return
  }

  const gap = gapPx(root)
  if (root.clientWidth <= 0) {
    // Not laid out yet (hidden pane, jsdom): keep every button and assume room.
    kids.forEach(k => { k.style.display = '' })
    hiddenItems.value = []
    showLabels.value = true
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
  const availableWidth = Math.max(0, root.clientWidth - fixedWidth)

  // ── 1. Which buttons stay inline (icon widths) ──
  root.classList.remove('ab--labels')
  kids.forEach(k => { k.style.display = '' })
  const widths = kids.map(k => k.getBoundingClientRect().width)

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
  // The track loses the "more" button's width when one appears, so compare
  // against the room the track will really have.
  const moreWidth = hidden.length > 0 ? designPx(props.moreWidth) + gap : 0
  const availableForTrack = availableWidth - moreWidth

  root.classList.add('ab--labels')
  const visible = kids.slice(0, visibleCount)
  const needed =
    visible.reduce((sum, k) => sum + k.getBoundingClientRect().width, 0) +
    gap * Math.max(0, visible.length - 1)
  root.classList.remove('ab--labels')

  const spare = availableForTrack - needed
  // Asymmetric on purpose: a bar dragged across the boundary would otherwise
  // flip at every pixel (on → 2px narrower → off → on).
  showLabels.value = showLabels.value ? spare >= -2 : spare >= TURN_ON_SLACK_PX

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
  if (hiddenItems.value.length === 0) menuOpen.value = false

  updateScrollState()
}

function updateScrollState(): void {
  const root = rootRef.value
  if (!root || props.mode !== 'scroll') {
    overflowing.value = false
    return
  }
  overflowing.value = root.scrollWidth > root.clientWidth + 1
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
})

onBeforeUnmount(() => {
  observer?.disconnect()
  observer = null
  if (frame) cancelAnimationFrame(frame)
  stopDrag?.()
  stopDrag = null
  const root = rootRef.value
  root?.removeEventListener('pointerdown', onPointerDown)
  root?.removeEventListener('pointerup', onPointerUp)
  root?.removeEventListener('pointercancel', onPointerUp)
})

// The slotted buttons can appear/disappear (conditional buttons), which does not
// resize the bar. Re-measure after every render.
watch(
  () => buttons().length,
  () => nextTick(schedule),
)
</script>

<style scoped>
.ab {
  display: flex;
  align-items: center;
  min-width: 0;
  overflow: hidden;
}

/* Scroll mode: the row itself scrolls; buttons are never hidden. */
.ab--scroll {
  overflow-x: auto;
  overflow-y: hidden;
  scrollbar-width: none;
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

.ab-more {
  display: flex;
  align-items: center;
  justify-content: center;
  gap: calc(3 * var(--dp));
  flex-shrink: 0;
  padding: 0 calc(6 * var(--dp));
  height: calc(26 * var(--dp));
  border: none;
  border-radius: var(--radius-xs);
  background: transparent;
  color: var(--text-secondary);
  cursor: pointer;
}
.ab-more:hover {
  background: color-mix(in srgb, var(--accent-color) 12%, transparent);
}

.ab-menu-item {
  display: flex;
  align-items: center;
  gap: var(--space-4);
  width: 100%;
  padding: var(--space-3) var(--space-5);
  border: none;
  background: none;
  color: var(--text-primary);
  font-size: var(--font-size-sm);
  text-align: left;
  cursor: pointer;
  white-space: nowrap;
}
.ab-menu-item:hover:not(:disabled) {
  background: var(--bg-hover);
}
.ab-menu-item:disabled {
  opacity: var(--opacity-muted);
  cursor: not-allowed;
}
.ab-menu-icon {
  display: inline-flex;
  flex-shrink: 0;
}
.ab-menu-icon :deep(svg) {
  width: calc(14 * var(--dp));
  height: calc(14 * var(--dp));
}
</style>
