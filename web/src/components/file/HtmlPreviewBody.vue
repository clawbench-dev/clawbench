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
      :style="iframeStyle"
      :src="htmlSrc || undefined"
      :srcdoc="htmlSrc ? undefined : htmlContent"
      sandbox="allow-scripts allow-same-origin"
      :title="filePath"
      @load="onLoad"
    />
  </div>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue'
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
 *
 * The frame takes the measured height of the document it loaded (see
 * measureDocHeight). The surfaces that host it are content-sized, so without
 * that the frame fell back to the 150px replaced-element default and a
 * full-page document opened as a short strip.
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
 * Height of the previewed DOCUMENT, in CSS pixels, once it has loaded.
 *
 * Only the frame can report this, and only a content-sized host needs it: the
 * floating card and the touch sheet both grow to fit their content, so
 * `height: 100%` on the frame resolves against an indefinite parent and falls
 * back to the replaced-element default of 150px — a full-page document then
 * opens as a short strip. Feeding the document's own height in as the frame's
 * height gives it a definite size; the card's `max-height` caps it and the
 * document scrolls inside from then on.
 *
 * `null` until the first load, which leaves the frame at its stylesheet size.
 */
const docHeight = ref<number | null>(null)

/**
 * Drop the measurement when the previewed document changes.
 *
 * A new file mounts a new frame, and until its `load` fires the frame would
 * otherwise carry the PREVIOUS document's height — a tall document followed by
 * a short one would flash at the old size before settling.
 */
watch(
  () => props.htmlSrc,
  () => { docHeight.value = null },
)

/** Inline height for the measured case; unset keeps the stylesheet's `100%`. */
const iframeStyle = computed(() =>
  docHeight.value === null ? undefined : { height: `${docHeight.value}px` },
)

/**
 * Read the loaded document's natural height.
 *
 * `body` — not `documentElement` — is the measurement: `documentElement`'s
 * scrollHeight is floored at the frame's viewport height, so a short document
 * (120px) reports the full frame and the card never shrinks back. The body
 * reports the content's own height in both shapes; `documentElement` is kept
 * as a fallback for a document that styles its body out of flow.
 *
 * Measured once per load, deliberately NOT observed. A document sized in `vh`
 * (`height: 100vh`) would feed back: measuring grows the frame, the frame
 * grows the viewport, the next measure grows it again. `load` already fires
 * after subresources (images, stylesheets) have settled, which is what the
 * measurement needs.
 *
 * Same-origin is guaranteed by the sandbox this component sets; a frame that
 * somehow lost it throws on `contentDocument` and is left at its default size.
 */
function measureDocHeight() {
  let doc: Document | null
  try {
    doc = iframeRef.value?.contentDocument ?? null
  } catch {
    return
  }
  if (!doc) return
  const body = doc.body
  const height = body?.scrollHeight || doc.documentElement?.scrollHeight || 0
  if (height > 0) docHeight.value = height
}

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
/**
 * Shared `show` factory, loaded with the quote bar (same module graph).
 *
 * Deferred like the bar itself: importing the composable eagerly is what this
 * module avoids (see above). Until it loads there is no bridge either, so the
 * `null` fallback can never swallow a selection.
 */
let makeShowHandler: typeof import('@/composables/useQuoteQuestion.ts')['makeIframeQuoteShowHandler'] | null = null

async function loadQuoteQuestion() {
  const mod = await import('@/composables/useQuoteQuestion.ts')
  makeShowHandler = mod.makeIframeQuoteShowHandler
  return mod.useQuoteQuestion()
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
  measureDocHeight()

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
  // The factory is loaded alongside the bar (same dynamic import), so it is
  // always available here.
  if (!makeShowHandler) return
  disposeQuoteBridge = attachIframeSelectionQuoteBridge({
    win,
    filePath: () => props.filePath,
    // Shared with the full-screen HTML preview and the OpenAPI docs: maps the
    // frame's anchor into host coordinates, shows immediately, and suppresses
    // the in-page bar on Android (the system menu is the affordance there).
    show: makeShowHandler(bar.showBar),
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
  /* `flex-basis: auto` (NOT `flex: 1`) is load-bearing: the frame carries the
     document's measured height as an inline style, and `flex: 1` expands to
     `1 1 0%` — a zero basis ignores that height and the frame collapses to the
     replaced-element default of 150px. With `auto` the inline height IS the
     basis, so a content-sized host (floating card, touch sheet) grows to the
     document and its `max-height` caps + scrolls it. In a host that already has
     a definite height (the docked pane) `height: 100%` still fills it. */
  flex: 1 1 auto;
  width: 100%;
  height: 100%;
  border: none;
  background: #fff;
}
</style>
