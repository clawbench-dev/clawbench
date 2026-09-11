<template>
  <div
    ref="rootRef"
    class="split-view"
    :class="[`split-view--${orientation}`, { 'split-view--active': enabled }]"
    :style="{
      '--split-gutter': `${gutterSize}px`,
      '--split-min-first': `${minLeft}px`,
      '--split-min-second': `${minRight}px`,
    }"
  >
    <div
      class="split-view__left split-view__first"
      :class="{ 'split-view__left--collapsed': enabled && collapsed }"
      :style="firstStyle"
    >
      <slot v-if="isVertical" name="top" />
      <slot v-else name="left" />
    </div>
    <SplitDivider
      v-if="enabled && !collapsed && !rightCollapsed"
      :orientation="orientation"
      :title="title"
      :aria-value-now="ariaValueNow"
      :aria-value-min="ariaValueMin"
      :aria-value-max="ariaValueMax"
      @dragmove="onMove"
    />
    <div class="split-view__right split-view__second" :class="{ 'split-view__right--collapsed': enabled && rightCollapsed }">
      <slot v-if="isVertical" name="bottom" />
      <slot v-else name="right" />
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { clampRatio, normalizeRatio, MIN_PANEL_WIDTH } from '@/utils/splitRatio'
import SplitDivider from './SplitDivider.vue'

const props = withDefaults(defineProps<{
  enabled: boolean
  /** 'horizontal' = side-by-side (left/right slots, drag on X).
   *  'vertical'   = stacked (top/bottom slots, drag on Y). */
  orientation?: 'horizontal' | 'vertical'
  ratio?: number
  minLeft?: number
  minRight?: number
  gutterSize?: number
  title?: string
  collapsed?: boolean
  rightCollapsed?: boolean
}>(), {
  orientation: 'horizontal',
  ratio: 0.5,
  minLeft: MIN_PANEL_WIDTH,
  minRight: MIN_PANEL_WIDTH,
  gutterSize: 1,
  title: '拖动调整面板宽度',
  collapsed: false,
  rightCollapsed: false,
})

const emit = defineEmits<{ (e: 'update:ratio', ratio: number): void }>()

const isVertical = computed(() => props.orientation === 'vertical')

const rootRef = ref<HTMLDivElement | null>(null)
const internalRatio = ref(normalizeRatio(props.ratio))
// The split axis extent: width for a horizontal split, height for a vertical one.
const containerExtent = ref(0)
let observer: ResizeObserver | null = null

watch(() => props.ratio, (r) => {
  internalRatio.value = normalizeRatio(r)
})

const firstStyle = computed(() => {
  if (!props.enabled) return {}
  // Second pane hidden → the first pane must take the full extent. A fixed
  // percentage size (below) would leave the far side blank, and inline size
  // beats any CSS override — so switch to a flex-grow that fills.
  if (props.rightCollapsed) return { flex: '1 1 auto' }
  return isVertical.value
    ? { height: `${internalRatio.value * 100}%` }
    : { width: `${internalRatio.value * 100}%` }
})

const minFirstRatio = computed(() => (containerExtent.value > 0 ? props.minLeft / containerExtent.value : 0))
const maxFirstRatio = computed(() => (containerExtent.value > 0 ? 1 - props.minRight / containerExtent.value : 1))

const ariaValueNow = computed(() => Math.round(internalRatio.value * 100))
const ariaValueMin = computed(() => Math.round(minFirstRatio.value * 100))
const ariaValueMax = computed(() => Math.round(maxFirstRatio.value * 100))

function measureContainer() {
  const el = rootRef.value
  if (!el) return
  const rect = el.getBoundingClientRect()
  containerExtent.value = isVertical.value ? rect.height : rect.width
}

function onMove(clientPos: number) {
  const rect = rootRef.value?.getBoundingClientRect()
  if (!rect) return
  const extent = isVertical.value ? rect.height : rect.width
  if (extent <= 0) return
  const origin = isVertical.value ? rect.top : rect.left
  const raw = (clientPos - origin) / extent
  const ratio = clampRatio(raw, extent, props.minLeft, props.minRight)
  internalRatio.value = ratio
  emit('update:ratio', ratio)
}

onMounted(() => {
  measureContainer()
  if (typeof ResizeObserver !== 'undefined') {
    observer = new ResizeObserver(measureContainer)
    if (rootRef.value) observer.observe(rootRef.value)
  }
})

onBeforeUnmount(() => {
  observer?.disconnect()
})
</script>

<style scoped>
.split-view {
  position: relative;
  height: 100%;
  width: 100%;
}
.split-view--active {
  display: flex;
  align-items: stretch;
}
.split-view--horizontal.split-view--active {
  flex-direction: row;
}
.split-view--vertical.split-view--active {
  flex-direction: column;
}
.split-view__left,
.split-view__right {
  position: absolute;
  inset: 0;
}
/* IMPORTANT: every pane rule below uses the child combinator (`>`), not a
   descendant combinator. SplitViews nest (the file manager's vertical split
   lives inside App's horizontal split), and a descendant selector would leak
   the outer split's sizing onto the inner split's panes — e.g. the outer
   horizontal `max-width: calc(100% - 320px)` would cap the inner vertical
   pane's width. `>` keeps each split styling only its own two panes. */
/* Disabled (single-column) mode: the wrappers are pure pass-throughs. If both
   stayed absolutely positioned, the later one would overlay the other and,
   when its slot content is v-show hidden, silently block every pointer event
   (touch/scroll) on the visible pane — mobile regression on non-chat tabs. */
.split-view:not(.split-view--active) > .split-view__left,
.split-view:not(.split-view--active) > .split-view__right {
  display: contents;
}
.split-view--active > .split-view__left,
.split-view--active > .split-view__right {
  position: relative;
  inset: auto;
}
.split-view--horizontal.split-view--active > .split-view__left,
.split-view--horizontal.split-view--active > .split-view__right {
  height: 100%;
}
.split-view--vertical.split-view--active > .split-view__left,
.split-view--vertical.split-view--active > .split-view__right {
  width: 100%;
}
/* Vertical panes are flex columns so slot content using `flex: 1; min-height: 0`
   (the file list) resolves against the pane's definite height and scrolls
   instead of growing the pane. */
.split-view--vertical.split-view--active > .split-view__left,
.split-view--vertical.split-view--active > .split-view__right {
  display: flex;
  flex-direction: column;
  min-height: 0;
  overflow: hidden;
}
.split-view--active > .split-view__left {
  flex: 0 0 auto;
}
/* Collapsed: hide the first pane entirely so the second takes the full extent.
   display:none (rather than a 0 size) also removes the pane from the flex
   container, letting the second pane fill the whole row/column. */
.split-view--active > .split-view__left--collapsed {
  display: none;
}
.split-view--active > .split-view__right {
  flex: 1 1 auto;
}
/* Second pane collapsed: hide it so the first takes the full extent. The first
   pane switches to flex-grow via its inline style (firstStyle) — drop the
   min/max size caps that assumed the second pane is visible. */
.split-view--active > .split-view__right--collapsed {
  display: none;
}
/* Pane minimums come from the minLeft/minRight props via CSS vars, so JS
   clamping (clampRatio) and the CSS floors can never disagree — important when
   the caller passes smaller mobile minimums. */
.split-view--horizontal.split-view--active > .split-view__left {
  min-width: var(--split-min-first, 320px);
  max-width: calc(100% - var(--split-min-second, 320px) - var(--split-gutter, 1px));
}
.split-view--horizontal.split-view--active > .split-view__right {
  min-width: var(--split-min-second, 320px);
}
.split-view--vertical.split-view--active > .split-view__left {
  min-height: var(--split-min-first, 160px);
  max-height: calc(100% - var(--split-min-second, 160px) - var(--split-gutter, 1px));
}
.split-view--vertical.split-view--active > .split-view__right {
  min-height: var(--split-min-second, 160px);
}
.split-view--active:has(> .split-view__right--collapsed) > .split-view__left {
  min-width: 0;
  min-height: 0;
  max-width: none;
  max-height: none;
}
</style>
