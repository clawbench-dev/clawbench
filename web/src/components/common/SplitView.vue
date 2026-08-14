<template>
  <div
    ref="rootRef"
    class="split-view"
    :class="{ 'split-view--active': enabled }"
    :style="{ '--split-gutter': `${gutterSize}px` }"
  >
    <div class="split-view__left" :class="{ 'split-view__left--collapsed': enabled && collapsed }" :style="leftStyle">
      <slot name="left" />
    </div>
    <SplitDivider
      v-if="enabled && !collapsed"
      :title="title"
      :aria-value-now="ariaValueNow"
      :aria-value-min="ariaValueMin"
      :aria-value-max="ariaValueMax"
      @dragmove="onMove"
    />
    <div class="split-view__right">
      <slot name="right" />
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { clampRatio, normalizeRatio, MIN_PANEL_WIDTH } from '@/utils/splitRatio'
import SplitDivider from './SplitDivider.vue'

const props = withDefaults(defineProps<{
  enabled: boolean
  ratio?: number
  minLeft?: number
  minRight?: number
  gutterSize?: number
  title?: string
  collapsed?: boolean
}>(), {
  ratio: 0.5,
  minLeft: MIN_PANEL_WIDTH,
  minRight: MIN_PANEL_WIDTH,
  gutterSize: 1,
  title: '拖动调整面板宽度',
  collapsed: false,
})

const emit = defineEmits<{ (e: 'update:ratio', ratio: number): void }>()

const rootRef = ref<HTMLDivElement | null>(null)
const internalRatio = ref(normalizeRatio(props.ratio))
const containerWidth = ref(0)
let observer: ResizeObserver | null = null

watch(() => props.ratio, (r) => {
  internalRatio.value = normalizeRatio(r)
})

const leftStyle = computed(() => {
  if (!props.enabled) return {}
  return { width: `${internalRatio.value * 100}%` }
})

const minLeftRatio = computed(() => (containerWidth.value > 0 ? props.minLeft / containerWidth.value : 0))
const maxLeftRatio = computed(() => (containerWidth.value > 0 ? 1 - props.minRight / containerWidth.value : 1))

const ariaValueNow = computed(() => Math.round(internalRatio.value * 100))
const ariaValueMin = computed(() => Math.round(minLeftRatio.value * 100))
const ariaValueMax = computed(() => Math.round(maxLeftRatio.value * 100))

function measureContainer() {
  if (rootRef.value) containerWidth.value = rootRef.value.getBoundingClientRect().width
}

function onMove(clientX: number) {
  const rect = rootRef.value?.getBoundingClientRect()
  if (!rect || rect.width <= 0) return
  const raw = (clientX - rect.left) / rect.width
  const ratio = clampRatio(raw, rect.width, props.minLeft, props.minRight)
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
  flex-direction: row;
  align-items: stretch;
}
.split-view__left,
.split-view__right {
  position: absolute;
  inset: 0;
}
/* Disabled (single-column) mode: the wrappers are pure pass-throughs. If both
   stayed absolutely positioned, the later one would overlay the other and,
   when its slot content is v-show hidden, silently block every pointer event
   (touch/scroll) on the visible pane — mobile regression on non-chat tabs. */
.split-view:not(.split-view--active) .split-view__left,
.split-view:not(.split-view--active) .split-view__right {
  display: contents;
}
.split-view--active .split-view__left,
.split-view--active .split-view__right {
  position: relative;
  inset: auto;
  height: 100%;
}
.split-view--active .split-view__left {
  flex: 0 0 auto;
  min-width: 320px;
  max-width: calc(100% - 320px - var(--split-gutter, 1px));
}
/* Collapsed: hide the left pane entirely so the right pane takes full width.
   display:none (rather than width:0) also removes the pane from the flex
   container, letting the right pane fill the whole row. */
.split-view--active .split-view__left--collapsed {
  display: none;
}
.split-view--active .split-view__right {
  flex: 1 1 auto;
  min-width: 320px;
}
</style>
