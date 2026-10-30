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
import { installPreviewLinkHandler, installPreviewScrollbar, installPreviewSearchStyles, watchPreviewTheme } from '@/utils/htmlPreview.ts'
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
 * A new target mounts a frame that would otherwise keep the OLD document's
 * height until its own `load` fires — a tall document followed by a short one
 * would flash at the stale size.
 *
 * Watches `filePath` as well as `htmlSrc`: the srcdoc branch (an external file
 * with no usable URL) keeps `htmlSrc` at `''` forever, so an htmlSrc-only
 * watcher never fired there — a 3000px external document followed by a 200px
 * one kept the old inline height until the new document loaded, and the card
 * visibly collapsed from the stale size. `filePath` changes for BOTH branches
 * (it is what the parent navigates by); `htmlSrc` is still watched because a
 * refresh of the SAME file only changes the cache buster in the URL.
 */
watch(
  () => [props.filePath, props.htmlSrc],
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
 * `load` does NOT cover everything, though: webfonts usually arrive after it,
 * and the text reflow they cause can make the document taller. That one gap is
 * closed by scheduleFontRemeasure — a single one-shot compensation, not an
 * observer.
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
  const height = readDocHeight(doc)
  if (height > 0) docHeight.value = height
}

/** The document's natural height, or 0 when neither element reports one. */
function readDocHeight(doc: Document): number {
  return doc.body?.scrollHeight || doc.documentElement?.scrollHeight || 0
}

/**
 * Cap on how much the webfont re-measure may grow the frame.
 *
 * The compensation below runs exactly once, so it cannot oscillate the way an
 * observer would. It can still be DERIVED from the first measurement: a
 * document sized in `vh` reports a height tied to the frame's own height, and
 * the frame was just sized from that report. A growth factor keeps that
 * feedback to one plausible reflow step instead of letting it multiply the
 * card, while still absorbing the real case (a webfont reflow adds tens of
 * percent, not multiples).
 */
const FONT_REMEASURE_GROWTH_CAP = 4

/**
 * Re-measure ONCE after the document's webfonts settle.
 *
 * `load` fires before `document.fonts.ready` in practice, so a document whose
 * layout depends on a webfont is measured at its fallback-font height and then
 * never corrected: the frame stays too short and the card shows a scrollbar
 * over text that would have fit. `lazy` images entering the viewport and the
 * page's own post-load rendering are out of scope — covering those needs an
 * observer, which is what the `vh` feedback above rules out.
 *
 * Only GROWTH is adopted: a reflow that shortens the document leaves the
 * (already correct, if slightly generous) height alone rather than risk
 * shrinking the frame while the user is reading.
 */
function scheduleFontRemeasure() {
  let doc: Document | null
  try {
    doc = iframeRef.value?.contentDocument ?? null
  } catch {
    return
  }
  // Not every engine exposes the CSS Font Loading API on a sub-document
  // (jsdom has no `fonts` at all), and its absence must not break the load.
  const fonts = doc?.fonts
  if (!fonts || typeof fonts.ready?.then !== 'function') return

  void fonts.ready.then(() => {
    // Read the CURRENT document, not the one this pass was scheduled for: the
    // user may have switched files (or the frame reloaded) while the fonts were
    // loading, and the old document's height describes neither the new
    // document nor anything on screen. After a switch `docHeight` is also
    // `null` until the new document loads, so the stale pass exits below.
    let current: Document | null
    try {
      current = iframeRef.value?.contentDocument ?? null
    } catch {
      return
    }
    if (!current) return
    const prev = docHeight.value
    const next = readDocHeight(current)
    if (prev === null || next <= prev) return
    if (next > prev * FONT_REMEASURE_GROWTH_CAP) return
    docHeight.value = next
  })
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
let disposeThemeWatcher: (() => void) | null = null
/** Takes over outbound-link clicks the sandbox would otherwise swallow. */
let disposeLinkHandler: (() => void) | null = null

/**
 * Give the previewed document the app's scrollbar bar, the search-match
 * highlight styles, and the three in-document hooks that cannot live on the
 * host: the Ctrl+F shortcut, the selection→quote bridge, and outbound-link
 * handoff to the system browser.
 *
 * Runs on every load — including the srcdoc fallback and reloads. A reload is a
 * NEW document, so all hooks must be re-attached unconditionally: the previous
 * listeners died with the old document. (`dispose` only removes listeners and
 * deliberately leaves the quote bar alone, so a user mid-comment keeps their
 * text.)
 */
function onLoad() {
  const iframe = iframeRef.value
  installPreviewScrollbar(iframe)
  installPreviewSearchStyles(iframe)
  measureDocHeight()
  // `load` fires before webfonts settle, so the first measurement can be a
  // fallback-font height; one compensation pass follows (see the function).
  scheduleFontRemeasure()

  // The injected colours are RESOLVED from the host theme, so a theme switch
  // while this preview is open would otherwise leave the bar in the previous
  // palette until the document reloads — which may never happen. Re-registered
  // per load so a reload cannot stack listeners.
  disposeThemeWatcher?.()
  disposeThemeWatcher = watchPreviewTheme(() => iframeRef.value)

  disposeLinkHandler?.()
  disposeLinkHandler = installPreviewLinkHandler(iframe)

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
  disposeThemeWatcher?.()
  disposeThemeWatcher = null
  disposeLinkHandler?.()
  disposeLinkHandler = null
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
