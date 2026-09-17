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
        class="markdown-body md-preview-body"
        :data-file-path="filePath"
        @dragstart="onMarkdownDragStart"
        @dragend="onMarkdownDragEnd"
        @click="handleBodyClick"
      >
        <div class="markdown-content" v-html="renderedHtml" />
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
 * The expand bars are kept purely as an affordance on very large documents: the
 * document is still split by the slicing limits (MAX_RENDER_BYTES) so we never
 * inject megabytes of HTML at once, and scrolling to a bar pulls in the next
 * chunk of source lines before re-rendering. Everything else (search /
 * scroll-to-line / target highlighting) lives in the code view; this component
 * mirrors CodePreviewBody's exposed surface so the parent can swap the two
 * transparently.
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
  /** Which side a load is currently extending, for the spinner. */
  loadingDirection: 'above' | 'below' | null
  /**
   * Loading more cannot grow the slice (a byte ceiling cut it short), so the
   * scroll handlers must stop asking. The "N lines remaining" hints stay up.
   */
  loadMoreBlocked?: boolean
  /** Pull in the next chunk of source lines above the current slice. */
  loadMoreAbove: () => Promise<void> | void
  /** Pull in the next chunk of source lines below the current slice. */
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

/**
 * Loading above prepends content, and the v-html re-render resets the
 * container's scroll. Capture the anchor first and let the renderedHtml watcher
 * (restoreScrollAfterRerender) put the read position back once the new HTML has
 * been written.
 */
async function loadAbove() {
  if (loadPending || props.loadMoreBlocked) return
  loadPending = true
  try {
    anchorScrollBeforeRerender()
    await props.loadMoreAbove()
    await nextTick()
  } finally {
    loadPending = false
  }
}

/**
 * Loading below appends content, so the browser's own scrollTop preservation is
 * already correct — no anchoring needed.
 */
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
 *
 * Progress is measured on the rendered HTML, since this view has no line rows:
 * a chunk that renders to nothing new must not loop forever.
 */
async function fillViewport() {
  await nextTick()
  for (let guard = 0; guard < 64; guard++) {
    const el = scrollEl.value
    if (!el) return
    if (!canExpandBelow.value || props.loadMoreBlocked) return
    if (el.scrollHeight > el.clientHeight + 1) return
    const before = props.renderedHtml
    await loadBelow()
    await nextTick()
    if (props.renderedHtml === before) return
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
  padding: var(--space-5) var(--space-7) var(--space-7);
}

.md-preview-scroll .markdown-content {
  width: 100%;
}
</style>
