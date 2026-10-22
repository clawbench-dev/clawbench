<template>
  <div class="html-preview-content">
    <!-- Loading -->
    <div v-if="status === 'loading'" class="code-preview-status" aria-live="polite">
      <span class="li-spinner code-preview-spinner" aria-hidden="true" />
      <span>{{ t('file.codePreview.loading') }}</span>
    </div>

    <!-- Error -->
    <div v-else-if="status === 'error'" class="code-preview-status" role="status">
      <span>{{ errorMessageText }}</span>
      <button v-if="errorCode === 'network'" class="code-preview-btn" @click="emit('refresh')">
        {{ t('file.codePreview.retry') }}
      </button>
    </div>

    <!-- Rendered HTML document. Loaded by real URL so the document's own
         relative references (stylesheets, scripts, images, fonts) resolve
         beside it; an external (absolute-path) file has no usable URL and
         falls back to srcdoc (see buildHtmlPreviewSrc). -->
    <iframe
      v-else-if="status === 'ready'"
      ref="iframeRef"
      class="html-preview-iframe"
      :src="htmlSrc || undefined"
      :srcdoc="htmlSrc ? undefined : htmlContent"
      sandbox="allow-scripts allow-same-origin"
      :title="filePath"
      @load="onLoad"
    />
  </div>
</template>

<script setup lang="ts">
import { onBeforeUnmount, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { installPreviewScrollbar, installPreviewSearchStyles } from '@/utils/htmlPreview.ts'
import {
  attachIframeSelectionQuoteBridge,
  installIframeSearchShortcut,
  resolveIframeWindow,
} from '@/utils/iframeSelectionQuote.ts'

/**
 * Rendered-HTML sibling of CodePreviewBody / MarkdownPreviewBody.
 *
 * The code-slice preview renders a file as numbered source lines. When an HTML
 * file is previewed without a target line range, the document is rendered in a
 * sandboxed iframe instead — the same presentation the full-screen viewer uses,
 * and deliberately showing the COMPLETE file with no line slicing (the iframe
 * loads the document whole).
 *
 * The sandbox (`allow-scripts allow-same-origin`) and the srcdoc fallback for
 * external files are the decisions recorded in fileViewerSandbox.test.ts; see
 * buildHtmlPreviewSrc for the rationale. `htmlSrc === ''` means the caller
 * wants the srcdoc fallback, and `htmlContent` carries the document text.
 *
 * It mirrors MarkdownPreviewBody's exposed surface (status/error shell plus
 * no-op scroll helpers) so the parent can swap the rendered bodies without a
 * special case.
 */

export type HtmlBodyStatus = 'idle' | 'loading' | 'ready' | 'error'

const props = defineProps<{
  status: HtmlBodyStatus
  errorMessageText: string
  errorCode: string | null
  /** Iframe URL. Empty string selects the srcdoc fallback. */
  htmlSrc: string
  /** Raw document text, used only for the srcdoc fallback. */
  htmlContent: string
  /** Previewed file path — used as the iframe title for accessibility. */
  filePath: string
  /**
   * Forward Ctrl/Cmd+F pressed inside the previewed document to the host.
   *
   * Omitted when the surface has no find bar of its own. The full-screen viewer
   * passes its handler; the quick-preview card does too (it owns a search bar).
   */
  onOpenSearch?: () => void
  /**
   * Enable selection → quote-bar for text selected inside the previewed
   * document. Off by default so surfaces without a chat target do not arm a
   * quote bar nothing can receive.
   */
  chatQuote?: boolean
}>()

const emit = defineEmits<{
  (e: 'refresh'): void
}>()

const { t } = useI18n()

const iframeRef = ref<HTMLIFrameElement | null>(null)

/**
 * Quote bar handle, loaded on first use.
 *
 * `useQuoteQuestion` transitively imports `useChatContext`, and the quick-preview
 * card sits on a module graph that several suites `vi.mock('@/composables/
 * useChatContext')` with a module-level `const` factory. A static import here
 * pulled that module into their graph BEFORE the const was initialized, so the
 * hoisted `vi.mock` factory dereferenced it and the whole suite failed to load
 * ("Cannot access 'mockAddAttachedFile' before initialization"). Deferring the
 * import keeps the card's default path free of that dependency — the quote
 * bridge is opt-in via `chatQuote` anyway.
 */
type QuoteBar = Awaited<ReturnType<typeof loadQuoteQuestion>>
let quoteQuestion: QuoteBar | null = null
/** Native-shell check, loaded with the quote bar (same module graph). */
let nativeSelectionShell: (() => boolean) | null = null

async function loadQuoteQuestion() {
  const mod = await import('@/composables/useQuoteQuestion.ts')
  nativeSelectionShell = mod.isNativeSelectionShell
  return mod.useQuoteQuestion()
}

/**
 * Whether the host draws its own selection menu (Android).
 *
 * Deferred like the quote bar itself: importing the composable eagerly is what
 * this module avoids. Until it loads there is no bridge either, so the fallback
 * (false) can never suppress a bar that would otherwise have shown.
 */
function isNativeSelectionShell(): boolean {
  return nativeSelectionShell?.() ?? false
}

async function getQuoteQuestion(): Promise<QuoteBar> {
  if (!quoteQuestion) quoteQuestion = await loadQuoteQuestion()
  return quoteQuestion
}

let disposeSearchShortcut: (() => void) | null = null
let disposeQuoteBridge: (() => void) | null = null

/**
 * Give the previewed document the app's scrollbar bar, the search-match
 * highlight styles, and the two in-document hooks that cannot live on the host:
 * the Ctrl+F shortcut and the selection→quote bridge.
 *
 * Runs on every load — including the srcdoc fallback and reloads. A reload is a
 * NEW document, so both hooks must be re-attached unconditionally: the previous
 * listeners died with the old document. (`dispose` only removes listeners and
 * deliberately leaves the quote bar alone, so a user mid-comment keeps their
 * text.)
 */
function onLoad() {
  const iframe = iframeRef.value
  installPreviewScrollbar(iframe)
  installPreviewSearchStyles(iframe)

  disposeSearchShortcut?.()
  disposeSearchShortcut = null
  if (props.onOpenSearch && iframe?.contentWindow) {
    disposeSearchShortcut = installIframeSearchShortcut(iframe.contentWindow, props.onOpenSearch)
  }

  disposeQuoteBridge?.()
  disposeQuoteBridge = null
  if (props.chatQuote) void attachQuoteBridge()
}

async function attachQuoteBridge() {
  const win = await resolveIframeWindow(() => iframeRef.value)
  if (!win) return
  const bar = await getQuoteQuestion()
  disposeQuoteBridge = attachIframeSelectionQuoteBridge({
    win,
    filePath: () => props.filePath,
    // `anchor` is already in HOST viewport coordinates (the bridge maps the
    // frame's own rects), so the bar floats next to the selected text.
    //
    // On Android the system's selection menu is the visible affordance for a
    // selection in the previewed page, so the in-page bar is suppressed —
    // only the quote is recorded for that menu's action to pick up.
    show: (data, anchor) => bar.showBar(data, {
      delay: 0,
      anchor,
      suppressBar: isNativeSelectionShell(),
    }),
    hide: () => bar.hideBar(),
  })
}

function disposeAll() {
  disposeSearchShortcut?.()
  disposeSearchShortcut = null
  disposeQuoteBridge?.()
  disposeQuoteBridge = null
}

onBeforeUnmount(disposeAll)

/** No-op target-line centering: the rendered view has no highlighted rows.
 *  Kept so the parent's bodyRef surface is identical across body components. */
function scrollToTargetLine() {
  // rendered view: nothing to center
}

/** No-op search navigation: the rendered view has no per-line rows. */
function scrollLineIntoView(_lineIdx: number) {
  // rendered view: no row-level scrolling
}

defineExpose({
  scrollToTargetLine,
  scrollLineIntoView,
  get scrollContainer(): HTMLElement | null {
    return null
  },
})
</script>

<style scoped>
/* The component owns the outer shell and the iframe it renders. The iframe is
   a real template element (not v-html), so scoped styling reaches it — no
   global rule needed, and a global one would leak onto the full-screen
   viewer's iframe, which styles the same class locally. */
.html-preview-content {
  flex: 1;
  min-height: 0;
  overflow: hidden;
  display: flex;
  flex-direction: column;
}

.html-preview-iframe {
  flex: 1;
  width: 100%;
  height: 100%;
  border: none;
  background: #fff;
}
</style>
