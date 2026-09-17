<template>
  <div class="code-preview-content">
    <!-- Loading -->
    <div v-if="status === 'loading'" class="code-preview-status" aria-live="polite">
      <div class="code-preview-spinner" />
      <span>{{ t('file.codePreview.loading') }}</span>
    </div>

    <!-- Error -->
    <div v-else-if="status === 'error'" class="code-preview-status" role="status">
      <span>{{ errorMessageText }}</span>
      <button v-if="errorCode === 'network'" class="code-preview-btn" @click="emit('refresh')">
        {{ t('file.codePreview.retry') }}
      </button>
    </div>

    <!-- Code viewer / Scroll pane -->
    <div
      v-else-if="status === 'ready'"
      ref="scrollEl"
      class="code-preview-scroll"
      :class="{ 'is-word-wrap': isWordWrap }"
      @scroll.passive="onScroll"
    >
      <!-- Top hint: lines exist above the slice. Reaching the top loads them
           automatically, so this bar only reports how many are left. No live
           region: the count changes on every load and would be re-announced
           repeatedly while scrolling. -->
      <div v-if="canExpandAbove" class="code-preview-expand-bar expand-above">
        <span class="code-preview-expand-hint">{{ t('file.codePreview.linesRemaining', { n: remainingAbove }) }}</span>
        <span v-if="loadingAbove" class="code-preview-expand-loading">
          <span class="code-preview-expand-spinner" aria-hidden="true" />
          {{ t('file.codePreview.loadingMoreLines') }}
        </span>
      </div>

      <div
        class="code-preview-lines"
        :style="{ '--gutter-digits': gutterDigits }"
      >
        <div
          v-for="(line, idx) in codeLines"
          :key="line.lineNum"
          class="code-preview-line-row"
          :class="{
            'is-target-line': line.isTarget,
            'is-search-match': matchingLineIndices.includes(idx),
            'is-current-search-match': matchingLineIndices[activeMatchIndex] === idx
          }"
        >
          <div
            v-if="showLineNumbers"
            class="code-preview-line-number"
            :class="{ 'is-target-line': line.isTarget }"
            aria-hidden="true"
          >
            {{ line.lineNum }}
          </div>
          <div class="code-preview-line-code">
            <code class="hljs" v-html="line.html || '&nbsp;'" />
          </div>
        </div>
      </div>

      <!-- Bottom hint: reaching the end loads the next lines automatically, so
           this bar only reports how many are left. -->
      <div v-if="canExpandBelow" class="code-preview-expand-bar expand-below">
        <span class="code-preview-expand-hint">{{ t('file.codePreview.linesRemaining', { n: remainingBelow }) }}</span>
        <span v-if="loadingBelow" class="code-preview-expand-loading">
          <span class="code-preview-expand-spinner" aria-hidden="true" />
          {{ t('file.codePreview.loadingMoreLines') }}
        </span>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, nextTick, watch, onMounted } from 'vue'
import { useI18n } from 'vue-i18n'

export interface FormattedCodeLine {
  lineNum: number
  html: string
  isTarget: boolean
}

export type PreviewBodyStatus = 'idle' | 'loading' | 'ready' | 'error'

const props = defineProps<{
  status: PreviewBodyStatus
  errorMessageText: string
  errorCode: string | null
  isWordWrap: boolean
  showLineNumbers: boolean
  codeLines: FormattedCodeLine[]
  matchingLineIndices: number[]
  activeMatchIndex: number
  remainingAbove: number
  remainingBelow: number
  /** Which side a load is currently extending, for the spinner. */
  loadingDirection: 'above' | 'below' | null
  /**
   * Loading more cannot grow the slice (a byte ceiling cut it short), so the
   * scroll handlers must stop asking. The "N lines remaining" hints stay up.
   */
  loadMoreBlocked?: boolean
  /** Pull in the next chunk of lines above the current slice. */
  loadMoreAbove: () => Promise<void> | void
  /** Pull in the next chunk of lines below the current slice. */
  loadMoreBelow: () => Promise<void> | void
}>()

const emit = defineEmits<{
  (e: 'refresh'): void
}>()

const { t } = useI18n()

const scrollEl = ref<HTMLElement | null>(null)

const canExpandAbove = computed(() => props.remainingAbove > 0)
const canExpandBelow = computed(() => props.remainingBelow > 0)

const loadingAbove = computed(() => props.loadingDirection === 'above')
const loadingBelow = computed(() => props.loadingDirection === 'below')

/**
 * Distance from an edge that counts as "reached it". Roughly a screenful, so
 * the next chunk is usually in place before the user scrolls into blank space.
 */
const LOAD_THRESHOLD_PX = 240

/** Re-entrancy latch: a scroll burst must not stack fetches. */
let loadPending = false

function onScroll() {
  const el = scrollEl.value
  if (!el) return
  if (canExpandAbove.value && el.scrollTop <= LOAD_THRESHOLD_PX) {
    void loadAbove()
    return
  }
  if (
    canExpandBelow.value &&
    el.scrollHeight - el.scrollTop - el.clientHeight <= LOAD_THRESHOLD_PX
  ) {
    void loadBelow()
  }
}

/**
 * Keep pulling downward while the content is too short to scroll. Without this
 * a chunk that does not overflow the pane would strand the user: no scrollbar
 * means no scroll event, so the "N lines remaining" hint would never resolve.
 * Only downward — auto-loading upward would fight the scroll anchor.
 */
async function fillViewport() {
  await nextTick()
  for (let guard = 0; guard < 64; guard++) {
    const el = scrollEl.value
    if (!el) return
    if (!canExpandBelow.value || props.loadMoreBlocked) return
    if (el.scrollHeight > el.clientHeight + 1) return
    const before = props.codeLines.length
    await loadBelow()
    await nextTick()
    if (props.codeLines.length === before) return
  }
}

watch(
  () => props.status,
  (st) => {
    if (st === 'ready') void fillViewport()
  },
  { immediate: true }
)

onMounted(() => {
  if (props.status === 'ready') void fillViewport()
})

// Digit count of the widest visible line number, so the sticky gutter column
// hugs the actual line-number width instead of reserving a fixed 44px slot.
// codeLines are contiguous (startLine..endLine), so the last row is the widest.
const gutterDigits = computed(() => {
  const last = props.codeLines[props.codeLines.length - 1]
  const n = last ? last.lineNum : props.remainingAbove + 1
  return Math.max(1, String(Math.max(1, n)).length)
})

function getRelativeOffsetTop(child: HTMLElement, parent: HTMLElement): number {
  let top = 0
  let el: HTMLElement | null = child
  while (el && el !== parent) {
    top += el.offsetTop
    el = el.offsetParent as HTMLElement | null
  }
  return top
}

/**
 * Scroll the first `.is-target-line` row into the vertical center of the pane.
 * Mirrors the parent's previous scrollToTargetLine; lives here because it only
 * touches this component's own scroll container.
 */
function scrollToTargetLine() {
  nextTick(() => {
    const el = scrollEl.value
    if (!el) return
    const targetEls = el.querySelectorAll('.code-preview-line-row.is-target-line')
    if (targetEls.length === 0) return
    const firstEl = targetEls[0] as HTMLElement
    const lastEl = targetEls[targetEls.length - 1] as HTMLElement
    const rangeTop = getRelativeOffsetTop(firstEl, el)
    const rangeBottom = getRelativeOffsetTop(lastEl, el) + lastEl.clientHeight
    const rangeHeight = rangeBottom - rangeTop
    const containerHeight = el.clientHeight
    const idealScrollTop = rangeHeight >= containerHeight
      ? rangeTop
      : rangeTop - Math.floor((containerHeight - rangeHeight) / 2)
    el.scrollTop = Math.max(0, idealScrollTop)
  })
}

/**
 * Scroll the row at `lineIdx` (0-based index into codeLines) into view.
 * Used by the parent's in-preview search navigation.
 */
function scrollLineIntoView(lineIdx: number) {
  const el = scrollEl.value
  if (!el) return
  const lineRows = el.querySelectorAll('.code-preview-line-row')
  const row = lineRows[lineIdx] as HTMLElement | undefined
  if (row && typeof row.scrollIntoView === 'function') {
    row.scrollIntoView({ block: 'nearest', inline: 'nearest' })
  }
}

/**
 * Loading above inserts content, so scrollTop must shift by the added height to
 * keep the read position. Without this the pane would jump upward by exactly
 * the amount it just grew.
 */
async function loadAbove() {
  if (loadPending || props.loadMoreBlocked) return
  loadPending = true
  try {
    const el = scrollEl.value
    const oldScrollHeight = el ? el.scrollHeight : 0
    const oldScrollTop = el ? el.scrollTop : 0
    await props.loadMoreAbove()
    await nextTick()
    if (el) {
      const deltaHeight = el.scrollHeight - oldScrollHeight
      if (deltaHeight > 0) {
        el.scrollTop = oldScrollTop + deltaHeight
      }
    }
  } finally {
    loadPending = false
  }
}

async function loadBelow() {
  if (loadPending || props.loadMoreBlocked) return
  loadPending = true
  try {
    await props.loadMoreBelow()
    await nextTick()
  } finally {
    loadPending = false
  }
}

defineExpose({
  scrollToTargetLine,
  scrollLineIntoView,
  loadAbove,
  loadBelow,
  get scrollContainer(): HTMLElement | null {
    return scrollEl.value
  },
})
</script>
