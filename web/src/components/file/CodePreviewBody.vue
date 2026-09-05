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
    >
      <!-- Top Expand Bar -->
      <div
        v-if="canExpandAbove"
        class="code-preview-expand-bar expand-above"
        role="region"
        :aria-label="t('file.codePreview.expandAbove', { n: stepAbove })"
      >
        <button
          type="button"
          class="code-preview-expand-btn"
          :title="t('file.codePreview.expandAbove', { n: stepAbove })"
          @click="expandAbove(stepAbove)"
        >
          <svg viewBox="0 0 24 24" width="13" height="13" fill="none" stroke="currentColor" stroke-width="2">
            <polyline points="18 15 12 9 6 15" />
          </svg>
          <span>{{ t('file.codePreview.expandAbove', { n: stepAbove }) }}</span>
          <span class="code-preview-expand-hint">({{ t('file.codePreview.linesRemaining', { n: remainingAbove }) }})</span>
        </button>
        <button
          v-if="remainingAbove > stepAbove"
          type="button"
          class="code-preview-expand-btn expand-all"
          :title="t('file.codePreview.expandToTop')"
          @click="expandAbove(remainingAbove)"
        >
          <svg viewBox="0 0 24 24" width="13" height="13" fill="none" stroke="currentColor" stroke-width="2">
            <polyline points="17 11 12 6 7 11" />
            <polyline points="17 18 12 13 7 18" />
          </svg>
          <span>{{ t('file.codePreview.expandToTop') }}</span>
        </button>
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

      <!-- Bottom Expand Bar -->
      <div
        v-if="canExpandBelow"
        class="code-preview-expand-bar expand-below"
        role="region"
        :aria-label="t('file.codePreview.expandBelow', { n: stepBelow })"
      >
        <button
          type="button"
          class="code-preview-expand-btn"
          :title="t('file.codePreview.expandBelow', { n: stepBelow })"
          @click="expandBelow(stepBelow)"
        >
          <svg viewBox="0 0 24 24" width="13" height="13" fill="none" stroke="currentColor" stroke-width="2">
            <polyline points="6 9 12 15 18 9" />
          </svg>
          <span>{{ t('file.codePreview.expandBelow', { n: stepBelow }) }}</span>
          <span class="code-preview-expand-hint">({{ t('file.codePreview.linesRemaining', { n: remainingBelow }) }})</span>
        </button>
        <button
          v-if="remainingBelow > stepBelow"
          type="button"
          class="code-preview-expand-btn expand-all"
          :title="t('file.codePreview.expandToBottom')"
          @click="expandBelow(remainingBelow)"
        >
          <svg viewBox="0 0 24 24" width="13" height="13" fill="none" stroke="currentColor" stroke-width="2">
            <polyline points="7 13 12 18 17 13" />
            <polyline points="7 6 12 11 17 6" />
          </svg>
          <span>{{ t('file.codePreview.expandToBottom') }}</span>
        </button>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, nextTick } from 'vue'
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
  stepAbove: number
  stepBelow: number
  /** Invoked to actually expand the slice; implemented by the parent (calls the
      preview composable). The body preserves the scroll anchor afterwards. */
  expandAboveLines: (n: number) => Promise<void> | void
  expandBelowLines: (n: number) => Promise<void> | void
}>()

const emit = defineEmits<{
  (e: 'refresh'): void
}>()

const { t } = useI18n()

const scrollEl = ref<HTMLElement | null>(null)

const canExpandAbove = computed(() => props.remainingAbove > 0)
const canExpandBelow = computed(() => props.remainingBelow > 0)

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
 * Expand upward while anchoring the visible viewport: expanding above inserts
 * content, so scrollTop must shift by the added height to keep the read position.
 */
async function expandAbove(n: number) {
  const el = scrollEl.value
  const oldScrollHeight = el ? el.scrollHeight : 0
  const oldScrollTop = el ? el.scrollTop : 0
  await props.expandAboveLines(n)
  await nextTick()
  if (el) {
    const deltaHeight = el.scrollHeight - oldScrollHeight
    if (deltaHeight > 0) {
      el.scrollTop = oldScrollTop + deltaHeight
    }
  }
}

async function expandBelow(n: number) {
  await props.expandBelowLines(n)
  await nextTick()
}

defineExpose({
  scrollToTargetLine,
  scrollLineIntoView,
  get scrollContainer(): HTMLElement | null {
    return scrollEl.value
  },
})
</script>
