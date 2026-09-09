<template>
  <div class="md-preview-content">
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

    <!-- Rendered markdown scroll pane -->
    <div
      v-else-if="status === 'ready'"
      ref="scrollEl"
      class="md-preview-scroll"
    >
      <!-- Top Expand Bar: buttons hide once the slice is pinned at the render
           cap, but the "N lines remaining" hint stays visible -->
      <div
        v-if="canExpandAbove"
        class="code-preview-expand-bar expand-above"
        role="region"
        :aria-label="t('file.codePreview.expandAbove', { n: stepAbove })"
      >
        <span
          v-if="remainingAbove > 0"
          class="code-preview-expand-hint"
        >{{ t('file.codePreview.linesRemaining', { n: remainingAbove }) }}</span>
        <span v-if="!hideExpandButtons" class="code-preview-expand-actions">
          <button
            type="button"
            class="code-preview-expand-btn"
            :title="t('file.codePreview.expandAbove', { n: stepAbove })"
            @click="expandAbove(stepAbove)"
          >
            <ChevronUp :size="13" />
            <span>{{ t('file.codePreview.expandAbove', { n: stepAbove }) }}</span>
          </button>
          <button
            v-if="remainingAbove > stepAbove"
            type="button"
            class="code-preview-expand-btn expand-all"
            :title="t('file.codePreview.expandToTop')"
            @click="expandAbove(remainingAbove)"
          >
            <ChevronsUp :size="13" />
            <span>{{ t('file.codePreview.expandToTop') }}</span>
          </button>
        </span>
      </div>

      <div
        class="markdown-body md-preview-body"
        :data-file-path="filePath"
        @dragstart="onMarkdownDragStart"
        @dragend="onMarkdownDragEnd"
        @click="handleBodyClick"
      >
        <div class="markdown-content" v-html="renderedHtml" />
      </div>

      <!-- Bottom Expand Bar: buttons hide once the slice is pinned at the render
           cap, but the "N lines remaining" hint stays visible -->
      <div
        v-if="canExpandBelow"
        class="code-preview-expand-bar expand-below"
        role="region"
        :aria-label="t('file.codePreview.expandBelow', { n: stepBelow })"
      >
        <span
          v-if="remainingBelow > 0"
          class="code-preview-expand-hint"
        >{{ t('file.codePreview.linesRemaining', { n: remainingBelow }) }}</span>
        <span v-if="!hideExpandButtons" class="code-preview-expand-actions">
          <button
            type="button"
            class="code-preview-expand-btn"
            :title="t('file.codePreview.expandBelow', { n: stepBelow })"
            @click="expandBelow(stepBelow)"
          >
            <ChevronDown :size="13" />
            <span>{{ t('file.codePreview.expandBelow', { n: stepBelow }) }}</span>
          </button>
          <button
            v-if="remainingBelow > stepBelow"
            type="button"
            class="code-preview-expand-btn expand-all"
            :title="t('file.codePreview.expandToBottom')"
            @click="expandBelow(remainingBelow)"
          >
            <ChevronsDown :size="13" />
            <span>{{ t('file.codePreview.expandToBottom') }}</span>
          </button>
        </span>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, nextTick, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { ChevronDown, ChevronsDown, ChevronUp, ChevronsUp } from 'lucide-vue-next'
import { onMdImageDragStart, onMdImageDragEnd } from '@/utils/mdImageDrag'
import { onMermaidDragStart, onMermaidDragEnd } from '@/utils/mdMermaidDrag'
import { handleMdImageAttachClick, type MdImageAttachActions } from '@/utils/mdImageAttach'
import { handleMermaidAttachClick, type MermaidAttachActions } from '@/utils/mdMermaidAttach'
import { handleBlockAttachClick } from '@/utils/mdBlockAttach'
import { handleMdImageOpenClick } from '@/utils/mdImageOpen'
import { openFilePath } from '@/composables/useFilePathAnnotation'
import { useChatContext } from '@/composables/useChatContext'
import { useToast } from '@/composables/useToast'
import { gt } from '@/composables/useLocale'

/**
 * Rendered-markdown sibling of CodePreviewBody.
 *
 * The code-slice preview (CodePreviewBody) renders each line as a row. When a
 * Markdown file is previewed without a target line range, the whole document is
 * rendered through the shared markdown pipeline and displayed read-only in a
 * `.markdown-body` container — the same styling as the full MarkdownPreview.
 *
 * The expand bars are kept purely as an affordance on very large documents: a
 * "whole file" render is still split by the slicing limits (MAX_RENDER_LINES /
 * MAX_RENDER_BYTES) so we never inject megabytes of HTML at once, and clicking
 * an expand bar asks the parent to grow the source slice before re-rendering.
 * Everything else (search / scroll-to-line / target highlighting) lives in the
 * code view; this component mirrors CodePreviewBody's exposed surface so the
 * parent can swap the two transparently.
 */

export type MarkdownBodyStatus = 'idle' | 'loading' | 'ready' | 'error'

const props = defineProps<{
  status: MarkdownBodyStatus
  errorMessageText: string
  errorCode: string | null
  /** Rendered markdown HTML for .markdown-content innerHTML. */
  renderedHtml: string
  /** Previewed file path — stamped on .markdown-body (data-file-path). */
  filePath: string
  remainingAbove: number
  remainingBelow: number
  stepAbove: number
  stepBelow: number
  /** Hide the expand N lines / expand-all buttons but keep the remaining-lines
      hint. Set when the slice is pinned at the line-count render cap. */
  hideExpandButtons?: boolean
  /** Invoked to actually expand the slice; implemented by the parent. */
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

// Image attach-to-chat badge (touch devices). This component is a read-only
// rendered view; the only interactive bit it owns is the badge toggle.
const { addAttachedFile, removeAttachedFileByPath, hasAttachedFile } = useChatContext()
const { show: showToast } = useToast()
const mdImageAttachActions: MdImageAttachActions = {
  add: addAttachedFile,
  remove: removeAttachedFileByPath,
  has: hasAttachedFile,
  toast: (msg, opts) => showToast(msg, opts),
  messages: {
    added: gt('chat.attach.addedToChat'),
    removed: gt('chat.attach.removedFromChat'),
  },
}

// Mermaid range-reference badge: same singletons, ranged identity.
const mermaidAttachActions: MermaidAttachActions = {
  add: (path, startLine, endLine) => addAttachedFile(path, false, startLine, endLine),
  remove: (path, startLine, endLine) => removeAttachedFileByPath(path, startLine, endLine),
  has: (path, startLine, endLine) => hasAttachedFile(path, startLine, endLine),
  toast: (msg, opts) => showToast(msg, opts),
  messages: {
    added: gt('chat.attach.addedToChat'),
    removed: gt('chat.attach.removedFromChat'),
  },
}

/** Delegated click: only the image/mermaid attach badges react; everything else
    in the read-only document view is left untouched (parent / lightbox handles). */
function handleBodyClick(e: MouseEvent) {
  handleMdImageAttachClick(e, mdImageAttachActions)
  handleMdImageOpenClick(e, openFilePath)
  handleMermaidAttachClick(e, mermaidAttachActions)
  handleBlockAttachClick(e, mermaidAttachActions)
}

/** Delegated dragstart: images first, then mermaid diagrams (md range drag). */
function onMarkdownDragStart(e: DragEvent) {
  onMdImageDragStart(e)
  onMermaidDragStart(e)
}

function onMarkdownDragEnd(e: DragEvent) {
  onMdImageDragEnd(e)
  onMermaidDragEnd(e)
}

// ── Mermaid ────────────────────────────────────────────────────────────────
// MarkdownPreview.vue renders mermaid diagrams at the DOM level after the HTML
// string is mounted (v-html is replaced wholesale on each update, so mermaid
// rendering is idempotent — it re-runs whenever the slice changes).

let currentRenderSeq = 0

async function renderMermaid() {
  const seq = ++currentRenderSeq
  const el = scrollEl.value
  if (!el) return
  const content = el.querySelector('.markdown-content') as HTMLElement | null
  if (!content) return
  if (content.querySelectorAll('pre.mermaid:not([data-rendered])').length === 0) return
  const { renderMermaidInElement } = await import('@/composables/useMarkdownRenderer.ts')
  if (seq !== currentRenderSeq) return
  await renderMermaidInElement(content, 'md-preview-card')
}

// ── Scroll anchoring across HTML re-renders ──────────────────────────────
// Expanding the slice replaces .markdown-content via v-html, which resets the
// scroll container to the top before mermaid re-runs. Capture the anchor at
// expand time and restore it once the (async) re-render has been written.
let pendingScrollAnchor: { scrollTop: number; scrollHeight: number } | null = null

function anchorScrollBeforeRerender() {
  const el = scrollEl.value
  if (!el) return
  pendingScrollAnchor = { scrollTop: el.scrollTop, scrollHeight: el.scrollHeight }
}

function restoreScrollAfterRerender() {
  const el = scrollEl.value
  if (!pendingScrollAnchor || !el) {
    pendingScrollAnchor = null
    return
  }
  const { scrollTop, scrollHeight } = pendingScrollAnchor
  pendingScrollAnchor = null
  const delta = el.scrollHeight - scrollHeight
  if (delta > 0 || scrollTop > 0) {
    el.scrollTop = scrollTop + delta
  }
}

watch(
  () => [props.status, props.renderedHtml],
  async () => {
    if (props.status !== 'ready') return
    await nextTick()
    restoreScrollAfterRerender()
    await renderMermaid()
  },
  { immediate: false }
)

/**
 * No-op target-line centering: the rendered markdown view has no highlighted
 * line rows. Kept so the parent's bodyRef surface is identical across the two
 * body components.
 */
function scrollToTargetLine() {
  // nothing to center in the rendered view
}

/**
 * Search navigation in the rendered markdown view is delegated to the same
 * in-preview search used by the code view (it filters the source slice's
 * lines). The body component has no per-line rows to scroll, so keep it a
 * no-op compatible with CodePreviewBody's signature.
 */
function scrollLineIntoView(_lineIdx: number) {
  // rendered view: no row-level scrolling
}

async function expandAbove(n: number) {
  anchorScrollBeforeRerender()
  await props.expandAboveLines(n)
  await nextTick()
}

async function expandBelow(n: number) {
  anchorScrollBeforeRerender()
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

<style scoped>
/* The component only owns the outer layout shell; the rendered markdown
   inside is styled by the global .markdown-body rules (css/content.css). */
.md-preview-content {
  flex: 1;
  min-height: 0;
  overflow: hidden;
  display: flex;
  flex-direction: column;
}
</style>

<style>
/* Neutralize the global .markdown-body rules inside the preview card:
   the scroll container is .md-preview-scroll, so the inner body must not
   take over flex/overflow (which would create nested scrollbars). */
.md-preview-scroll {
  display: flex;
  flex-direction: column;
  overflow: auto;
  flex: 1;
  min-height: 0;
  position: relative;
  background: var(--bg-primary, #ffffff);
}

.md-preview-scroll .markdown-body {
  flex: none;
  overflow: visible;
  margin: 0;
  width: 100%;
  max-width: none;
  min-height: 0;
  padding: 10px 16px 16px;
}

.md-preview-scroll .markdown-content {
  width: 100%;
}
</style>
