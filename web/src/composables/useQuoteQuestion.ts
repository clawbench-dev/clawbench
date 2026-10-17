import { ref, computed, watch, onMounted, onUnmounted } from 'vue'
import { useSessionIdentity, getSessionId, getSessionTitle } from '@/composables/useSessionIdentity.ts'
import { closestElement, getLineInfo, getFileInfo, getQuoteSource, messageIdFromKey } from '@/utils/quoteQuestionUtils.ts'
import { buildMessageQuote } from '@/utils/quoteItem.ts'
import { useChatContext } from '@/composables/useChatContext.ts'
import type { QuoteData } from '@/composables/useChatContext.ts'
import { requestTarget, mintPendingQuoteId } from '@/composables/useConversationTarget.ts'
import type { PendingQuote } from '@/composables/useConversationTarget.ts'
import { usePlatformDetect } from '@/composables/usePlatformDetect'
import { appLog } from '@/utils/appLog'

const TAG = 'QuoteQuestion'

/**
 * Context for the "composer" flow: an entry point (e.g. the forge issue/PR
 * detail header, or the file browser header) opens the quote bar with NO quote
 * and only an attachment, so the user can type immediately and optionally
 * select text to quote.
 *
 * Exactly one attachment source is expected:
 * - `url` + `label`  — an external address (issue/PR), attached as a URL entry;
 * - `filePath`       — a local file, attached as a file entry.
 *
 * There is no `onAdd` navigation callback any more: committing always goes
 * through the session picker, whose "add and open" button is what reveals the
 * chat panel. A caller-side "switch to chat" would fight that choice — the user
 * may well have picked "add and stay".
 */
export interface QuoteComposerContext {
  url?: string
  filePath?: string
  label: string
}

/**
 * The attachment the composer is *about* to add, shown as a chip inside the
 * bar. It is deliberately not part of the chat context yet — see openComposer.
 */
export interface ComposerAttachment {
  kind: 'url' | 'file'
  label: string
  url?: string
  path?: string
}

// Module-level singleton: bar visibility state shared across all consumers.
// The active selection stays separate from staged quotes so dismissing a
// selection never discards snippets the user already added to the chat draft.
const {
  quoteData,
  setQuoteData,
} = useChatContext()
const barVisible = ref(false)
const barPinned = ref(false)  // When pinned, selection loss won't auto-hide the bar
const sheetOpen = ref(false)
// Bounding rect of the selection the bar is currently about, used to float the
// toolbar next to the text (issue #544). Null when there is no DOM selection
// behind the bar — entry-point composers, terminal/CodeMirror selections that
// live outside the global selection, and the OpenAPI preview's iframe — in
// which case the bar falls back to its pinned top position.
const anchorRect = ref<DOMRect | null>(null)
// Non-null while the bar was opened by an entry point rather than by a text
// selection. In this mode the bar is useful even with no quote.
const composerContext = ref<QuoteComposerContext | null>(null)
const composerMode = computed(() => composerContext.value !== null)

/**
 * Snapshot of the last DOM-selection quote the bar was about, used by the
 * Android native selection menu (see handleNativeSelectionAction).
 *
 * The native side taps its menu item and only then calls into JS, by which time
 * the action mode has finished and the WebView's DOM selection is gone — so
 * `window.getSelection()` alone would come back empty. This holds the derived
 * QuoteData from the last evaluation.
 *
 * Deliberately a SEPARATE ref rather than keeping `quoteData` alive with the bar
 * hidden: `quoteData` is the live staged quote that the chat draft snapshots
 * and restores (`liveStagedEmpty` / `snapshotAttachments` in useChatContext),
 * so parking a hidden quote there would resurface as a phantom card after a
 * session switch. Cleared wherever the bar is genuinely dismissed.
 */
const lastSelectionQuote = ref<QuoteData | null>(null)

/**
 * Bumped when something outside the component (the Android native menu) asks
 * for the comment input to open. The component owns `expanded`, so it watches
 * this counter and expands itself; a counter rather than a boolean so two
 * consecutive requests both register.
 */
const expandRequest = ref(0)

/**
 * Whether this is the Android app shell.
 *
 * Read LAZILY, not at module scope: usePlatformDetect() latches its axes on
 * first call and derives them from useAppMode(), which App.vue initializes
 * during setup. This module's top-level code runs at IMPORT time — before that
 * setup — so calling it here would latch `isAndroidApp = false` forever and the
 * native-menu behaviour would silently never engage.
 *
 * Android draws its OWN text-selection menu, so the in-page bar is suppressed
 * for DOM selections (see evaluateSelection) and the native menu drives the
 * actions instead.
 */
function androidShell(): boolean {
  return usePlatformDetect().isAndroidApp.value
}

/**
 * The quote target the composer is about, derived from its context.
 *
 * This is only a *preview*: nothing is staged until the user commits (see
 * buildComposerQuote). Keeping it out of the chat context until then is the
 * whole point — the chat input renders its cards straight from stagedQuotes, so
 * staging on open put the file/issue in the chat the moment the button was
 * clicked, and dismissing the bar left it behind.
 */
const composerAttachment = computed<ComposerAttachment | null>(() => {
  const ctx = composerContext.value
  if (!ctx) return null
  if (ctx.url) return { kind: 'url', label: ctx.label, url: ctx.url }
  if (ctx.filePath) return { kind: 'file', label: ctx.label, path: ctx.filePath }
  return null
})

let debounceTimer: ReturnType<typeof setTimeout> | null = null
let pointerReleaseTimer: ReturnType<typeof setTimeout> | null = null

/**
 * Rect the floating toolbar should attach to: the LAST line box of the
 * selection (the point the user's pointer just left). For a multi-line
 * selection that is the bottom line, which is where "just below the text" reads
 * correctly. Returns null when the selection has no laid-out boxes (e.g. a
 * collapsed range), letting the caller fall back to the pinned top position.
 *
 * The rect is stored RAW (zoom-scaled): conversion to pre-zoom `position:fixed`
 * space happens in computeSelectionToolbarStyle, so both sides of that
 * comparison stay in one space.
 */
function selectionAnchorRect(sel: Selection): DOMRect | null {
  if (sel.rangeCount === 0) return null
  const rects = sel.getRangeAt(0).getClientRects()
  if (!rects.length) return null
  return rects[rects.length - 1]
}

// Active pointer (mouse/touch) tracking. Browsers fire selectionchange while the
// user is still dragging, so a debounced evaluation can run mid-drag and surface
// the bar before the pointer is released — on desktop the bar then auto-expands
// and focuses its input, stealing focus and cutting the selection short. While
// any pointer button is held the bar is held back; pointerup re-runs the check.
let pointerCount = 0

function evaluateSelection() {
  const sel = window.getSelection()
  if (!sel || sel.isCollapsed || !sel.toString().trim()) {
    // In composer mode the bar stays open (it was opened deliberately, not by a
    // selection) and an already-captured quote survives — otherwise deselecting
    // would silently discard the snippet the user picked.
    if (composerContext.value) return
    // Drop only the active selection. Staged quotes remain in the chat draft.
    if (!barPinned.value) {
      barVisible.value = false
      setQuoteData(null)
      anchorRect.value = null
    }
    return
  }

  // The pointer is still pressed — the selection is mid-drag and not final yet.
  if (pointerCount > 0) return

  // CodeMirror viewers (CodeMirrorViewer) manage their own selection + quote
  // bar via an internal selection listener. This DOM selection is only a
  // shadow of CM's internal one, so skip it — otherwise it would hide/show
  // the bar in parallel with the editor's own handler.
  if (closestElement(sel.anchorNode, '.cm-editor')) return

  // Chat chrome is not message content. The meta bar (timestamps, copy/speak
  // buttons), tool pills, card strips and attachment chips all live inside
  // .chat-message, so without this a selection that merely brushes a button
  // label or a filename would offer to quote it.
  //
  // Scoped to CHAT on purpose: in a file preview, `button`/`a` also match real
  // content (a link inside a markdown body), and excluding those there would
  // silently stop offering the bar for ordinary prose.
  const chatMsg = closestElement(sel.anchorNode, '.chat-message')
  if (chatMsg && closestElement(sel.anchorNode, '.chat-meta-bar, button, a, .chat-file-attachment, .chat-tool-call, .chat-card-strip')) {
    if (!barPinned.value && !composerContext.value) {
      barVisible.value = false
      anchorRect.value = null
      // A different, non-quotable selection supersedes the snapshot — without
      // this, tapping the Android menu item here would quote the PREVIOUS
      // selection's text.
      lastSelectionQuote.value = null
    }
    return
  }

  // Only selections inside quotable content (see quotableContainer) are ours.
  const container = quotableContainer(sel)
  if (!container) {
    if (!barPinned.value && !composerContext.value) {
      barVisible.value = false
      anchorRect.value = null
      lastSelectionQuote.value = null
    }
    return
  }

  // Capture where the selection sits so the bar can float next to it. Done once
  // here — every quotable branch below (labelled source, chat message, file)
  // surfaces the same bar and wants the same anchor.
  anchorRect.value = selectionAnchorRect(sel)

  const derived = deriveQuoteData(sel, container)
  if (!derived) {
    if (!barPinned.value && !composerContext.value) {
      barVisible.value = false
      anchorRect.value = null
      lastSelectionQuote.value = null
    }
    return
  }

  // Remember it for the Android native selection menu: by the time that menu's
  // item is tapped the DOM selection is already gone, so this is the only copy.
  lastSelectionQuote.value = derived

  setQuoteData(derived)
  // Android shows its OWN selection menu (Copy / Select all / our 引用 · 评论),
  // so the in-page floating bar would be a duplicate — and it sits UNDER that
  // system window anyway. Keep the quote data (the native menu's action needs
  // it) but do not surface the bar.
  //
  // Only THIS path is suppressed. Terminal / CodeMirror / OpenAPI-iframe
  // selections reach the bar through showBar() and have no native menu, so they
  // must keep it.
  if (androidShell()) {
    barVisible.value = false
    return
  }
  barVisible.value = true
}

/**
 * Turn a DOM selection into the QuoteData the bar (and the AI prompt) needs,
 * or null when the selection is not quotable content.
 *
 * Pure derivation: it neither mutates nor reads module state, so the Android
 * native path (handleNativeSelectionAction) can reuse it for a selection that
 * is still live, and it is directly unit-testable. `container` is passed in
 * rather than re-derived so the caller's whitelist decision is the only one.
 */
function deriveQuoteData(sel: Selection, container: HTMLElement): QuoteData | null {
  const text = sel.toString().trim()
  if (!text) return null

  // A labelled non-file source (an issue/PR body, a commit, a task, a CI run)
  // supplies the quote's identity. It has no meaningful file line numbers, so
  // those stay 0 — appending ":0" would be noise in the fence header.
  //
  // The locator attributes on the same region are what make the quote
  // addressable: the label is a human-readable name, so without them neither
  // the jump handler nor the AI can find the origin.
  const source = getQuoteSource(container)
  if (source) {
    return {
      text,
      filePath: source.label,
      language: source.language,
      startLine: 0,
      endLine: 0,
      sourceKind: source.url ? 'url' : 'file',
      ...(source.url ? { url: source.url } : {}),
      ...(source.commitSha ? { commitSha: source.commitSha } : {}),
      ...(source.taskId !== undefined ? { taskId: source.taskId } : {}),
      ...(source.sessionId ? { sessionId: source.sessionId } : {}),
      ...(source.messageId !== undefined ? { messageId: source.messageId } : {}),
      ...(source.executionId ? { executionId: source.executionId } : {}),
    }
  }

  // A selection inside a chat message has no file and no line numbers; it is
  // attributed to the message it came from so the quote can be traced back.
  // Optimistic messages have no DB id — the quote is still valid, just not
  // addressable later.
  //
  // The session id/name are attached too: a message id alone is only unique
  // within its session, so without them the quote could not be reopened from
  // another project or after a reload.
  //
  // Built by the SAME builder as the meta-bar "quote this message" button, so
  // quoting a snippet and quoting the whole message produce the same kind of
  // card. They used to be written separately and had drifted (the button's path
  // had no session id at all).
  const msgEl = container.closest('.chat-message')
  if (msgEl) {
    const messageId = messageIdFromKey(msgEl.getAttribute('data-msg-key'))
    return buildMessageQuote(
      text,
      { id: getSessionId(), title: getSessionTitle() },
      messageId,
    )
  }

  const { filePath, language } = getFileInfo(container)
  const { startLine, endLine } = getLineInfo(sel)

  // No file path means no addressable source (e.g. a git diff whose container
  // carried no file name). Tag it explicitly: leaving sourceKind unset would
  // make the client infer 'message' and the drawer would call it a chat quote.
  return {
    text,
    filePath,
    language,
    startLine,
    endLine,
    ...(filePath ? {} : { sourceKind: 'selection' as const }),
  }
}

/**
 * The container a selection is quotable in, or null.
 *
 * The chat selector must be `.chat-message .msg-content-wrapper`, NOT bare
 * `.chat-message`: the row also contains the meta bar, so the wider selector
 * would surface the bar for selections in the timestamp/action area (the
 * chrome guard in evaluateSelection catches the buttons, but the wrapper is the
 * precise content boundary).
 *
 * This stays a WHITELIST on purpose: "quote what I selected" is only useful
 * where the selection is content. An allow-list keeps the bar out of settings,
 * session lists and other chrome, where a quote would carry no meaning and the
 * drawer could only show a generic label.
 */
function quotableContainer(sel: Selection): HTMLElement | null {
  return closestElement(sel.anchorNode, '.raw-content-pre, .markdown-body, .office-preview-body, .chat-message .msg-content-wrapper, .git-diff-scroll')
}

function onSelectionChange() {
  if (debounceTimer) clearTimeout(debounceTimer)
  debounceTimer = setTimeout(evaluateSelection, 150)
}

function onPointerDown() {
  pointerCount++
  // Safety net: mobile native selection UI can swallow the matching pointerup/
  // touchend, which would leave the guard held forever and block the quote bar
  // from appearing. Release it after a short window so a settling selection can
  // still surface the bar (the collapsed bar no longer steals focus mid-drag).
  if (pointerReleaseTimer) clearTimeout(pointerReleaseTimer)
  pointerReleaseTimer = setTimeout(() => {
    pointerCount = 0
    pointerReleaseTimer = null
    // Re-evaluate: a selection that settled while the guard was stuck can now
    // surface the bar.
    if (debounceTimer) {
      clearTimeout(debounceTimer)
      debounceTimer = null
    }
    debounceTimer = setTimeout(evaluateSelection, 0)
  }, 700)
}

function releasePointer() {
  if (pointerReleaseTimer) {
    clearTimeout(pointerReleaseTimer)
    pointerReleaseTimer = null
  }
  if (pointerCount > 0) pointerCount--
  if (pointerCount === 0) {
    // Re-run the check, but deferred: on touch the final selection often settles
    // only after the pointer is released, so an immediate evaluate can see an
    // empty/collapsed selection and hide the bar (and, by clearing the debounce
    // timer, leave it hidden). On desktop the selection is already final, so the
    // short delay is harmless.
    if (debounceTimer) {
      clearTimeout(debounceTimer)
      debounceTimer = null
    }
    debounceTimer = setTimeout(evaluateSelection, 120)
  }
}

function onPointerUp() {
  releasePointer()
}

/**
 * Hide the floating bar when the page scrolls.
 *
 * The toolbar is `position: fixed` and anchored to the selection's on-screen
 * rect, so once the text scrolls away the bar would sit next to unrelated
 * content (issue #544 requires it to disappear on scroll). Skipped while the
 * bar is pinned — clicking an action pins it (see onRowPointerDown / expand),
 * which covers the expanded input: the user must be able to scroll the page
 * without their half-typed comment vanishing. Scrolls originating INSIDE the
 * bar (the textarea's own scrollbar) are ignored for the same reason.
 *
 * Registered in the capture phase so it sees scrolls from any scrollable
 * ancestor (the markdown body, the chat list, …), which do not bubble.
 */
function onScroll(e: Event) {
  if (!barVisible.value) return
  if (barPinned.value || composerContext.value) return
  const target = e.target
  if (target instanceof Element && target.closest('.quote-question-bar')) return
  barVisible.value = false
  setQuoteData(null)
  anchorRect.value = null
  lastSelectionQuote.value = null
}

/** True while any pointer button is held (used to gate mid-drag selection UI). */
export function isPointerPressed() {
  return pointerCount > 0
}

// Global listener management
let listenerCount = 0

/** Reset the bar pinned state (for use when quoteData is cleared externally). */
export function resetQuotePin() {
  barPinned.value = false
}

export function useQuoteQuestion() {
  const sessionIdentity = useSessionIdentity()

  onMounted(() => {
    listenerCount++
    if (listenerCount === 1) {
      document.addEventListener('selectionchange', onSelectionChange)
      document.addEventListener('pointerdown', onPointerDown)
      document.addEventListener('pointerup', onPointerUp)
      document.addEventListener('pointercancel', onPointerUp)
      document.addEventListener('touchend', onPointerUp)
      document.addEventListener('touchcancel', onPointerUp)
      // Capture phase: scrolls on inner containers do not bubble to document.
      document.addEventListener('scroll', onScroll, true)
    }
  })

  onUnmounted(() => {
    listenerCount--
    if (listenerCount === 0) {
      document.removeEventListener('selectionchange', onSelectionChange)
      document.removeEventListener('pointerdown', onPointerDown)
      document.removeEventListener('pointerup', onPointerUp)
      document.removeEventListener('pointercancel', onPointerUp)
      document.removeEventListener('touchend', onPointerUp)
      document.removeEventListener('touchcancel', onPointerUp)
      document.removeEventListener('scroll', onScroll, true)
      pointerCount = 0
      if (pointerReleaseTimer) {
        clearTimeout(pointerReleaseTimer)
        pointerReleaseTimer = null
      }
    }
  })

  /**
   * Drop the snapshot when the session changes.
   *
   * `lastSelectionQuote` is module-level and deliberately survives a selection
   * collapse (the Android menu's action mode clears the selection before JS is
   * called). But it must not survive a SESSION switch: the text belonged to the
   * previous session's message/file, and on Android the bar never became
   * visible so none of the dismissal paths ran. Without this, tapping 引用 right
   * after switching would quote text from the session the user just left.
   */
  watch(() => sessionIdentity.currentSessionId.value, () => {
    lastSelectionQuote.value = null
  })

  function closeSheet() {
    const sel = window.getSelection()
    if (sel) sel.removeAllRanges()
    barVisible.value = false
    barPinned.value = false
    composerContext.value = null
    setQuoteData(null)
    anchorRect.value = null
    lastSelectionQuote.value = null
  }

  /**
   * Open the bar from an entry point with NO quote (e.g. the issue/PR detail
   * header). The bar previews the attachment as a chip; nothing reaches the chat
   * context until the user commits (send or the add button), so dismissing the
   * bar leaves the chat input untouched.
   */
  function openComposer(ctx: QuoteComposerContext) {
    if (!ctx?.url && !ctx?.filePath) return
    composerContext.value = ctx
    // A live selection carries over (select-then-click); otherwise start empty
    // so the full body is never quoted by default.
    const sel = window.getSelection()
    const live = sel && !sel.isCollapsed && sel.toString().trim()
    if (!live) {
      setQuoteData(null)
      // No selection behind the bar → keep it on the pinned top position. A
      // stale anchor from an earlier selection must not drag it off.
      anchorRect.value = null
    } else {
      anchorRect.value = selectionAnchorRect(sel)
    }
    barVisible.value = true
    barPinned.value = true
  }

  /**
   * Build the quote payload the composer's target produces, without staging it.
   *
   * Every entry point (file browser, issue/PR detail, CI run) behaves exactly
   * like quoting a text selection: it produces ONE quote card referencing the
   * object, with the typed text as its annotation. It does NOT add a separate
   * file/URL attachment chip, and does not inject the text into the chat input.
   *
   * The selection, when there is one, becomes the card's quoted content; with
   * no selection the card references the whole object (empty text), which the
   * prompt renders as a path/address for the AI to read.
   *
   * Split from the staging so the session picker can hold the payload while the
   * user chooses a destination — and so both delivery paths (live input vs.
   * another session's draft) build the identical card.
   */
  function buildComposerQuote(ctx: QuoteComposerContext | null, note = ''): PendingQuote | null {
    if (!ctx) return null
    const selection = quoteData.value
    if (selection) {
      // The user selected part of the object: that snippet is the content, and
      // it already carries the source label/lines/address.
      return { ...selection, note, id: '' }
    }
    // No selection: reference the whole object.
    return {
      text: '',
      filePath: ctx.filePath || ctx.label,
      language: '',
      startLine: 0,
      endLine: 0,
      sourceKind: ctx.url ? 'url' : 'file',
      ...(ctx.url ? { url: ctx.url } : {}),
      note,
      id: '',
    }
  }

  /**
   * Tear down the composer's visible state (bar + pending target).
   *
   * Mirrors the old inline sequence in the commit paths. Note it is NOT
   * `hideBar()`: that one deliberately refuses to touch `composerContext` (the
   * editor fires a selectionchange on click, and clearing it there made the
   * commit a silent no-op).
   */
  function clearComposerState() {
    composerContext.value = null
    setQuoteData(null)
    barVisible.value = false
    barPinned.value = false
    anchorRect.value = null
    lastSelectionQuote.value = null
  }

  /** Close the composer without touching unrelated staged quotes. */
  function hideComposer() {
    if (!composerContext.value) return
    composerContext.value = null
    barVisible.value = false
    barPinned.value = false
    setQuoteData(null)
    anchorRect.value = null
    lastSelectionQuote.value = null
  }

  function pinBar() {
    // Pin the bar so it survives selection loss (e.g. after clicking a button)
    barPinned.value = true
  }

  function unpinBar() {
    barPinned.value = false
  }

  /**
   * Programmatically hide the quote bar (used by CodeMirror-based viewers whose
   * selection is internal and never reaches the global selectionchange handler).
   *
   * Only the SELECTION half is torn down. An entry-point-opened composer (file
   * browser / issue / CI run) has no selection to lose and is still in use: the
   * editor fires a selectionchange the moment the user clicks into the bar or
   * types, and clearing composerContext here made the commit a no-op — the
   * button appeared to do nothing at all. The composer is dismissed by its own
   * paths (commit, Escape, click outside, or hideComposer).
   */
  function hideBar() {
    if (composerContext.value) return
    barVisible.value = false
    barPinned.value = false
    setQuoteData(null)
    anchorRect.value = null
    lastSelectionQuote.value = null
  }

  /**
   * 编程式显示引用问答栏（不依赖 selectionchange 事件）。
   * 默认延迟 400ms 显示，避免双击的 pointerdown 事件触发"点击外部关闭"
   * （markdown 预览双击复制依赖此延迟）。传 { delay: 0 } 可立即显示
   * （代码模式拖选无 pointerdown 干扰）。
   *
   * The anchor is captured SYNCHRONOUSLY, before the delay: by the time the
   * timeout runs the selection may already have been cleared (the double-click
   * that triggered this collapses it), and a cleared selection has no rect to
   * measure. Callers with no DOM selection (CodeMirror's internal selection,
   * the OpenAPI preview's iframe) leave the anchor null and the bar stays on
   * its pinned top position.
   */
  function showBar(data: QuoteData, opts: { delay?: number } = {}) {
    const sel = window.getSelection()
    const rect = sel && !sel.isCollapsed && sel.toString().trim() ? selectionAnchorRect(sel) : null
    setTimeout(() => {
      setQuoteData(data)
      barVisible.value = true
      anchorRect.value = rect
    }, opts.delay ?? 400)
  }

  /**
   * Handle an action tapped in the Android native text-selection menu.
   *
   * The native side sends only the action name — the text is read here, because
   * this side also knows the quote's identity (file path, line range, session /
   * message ids), which a bare string would lose.
   *
   * The DOM selection is usually ALREADY GONE by the time this runs: the native
   * menu dismisses its action mode (which clears the selection) before the JS
   * call arrives. So the live selection is only a best-effort first try, and
   * `lastSelectionQuote` — snapshotted while the selection was still live — is
   * the reliable source.
   *
   * Called via window.__clawbenchSelectionAction (installed in App.vue).
   */
  function handleNativeSelectionAction(action: string) {
    if (action !== 'quote' && action !== 'comment') {
      appLog.w(TAG, 'native selection action: unknown action', action)
      return
    }

    // Prefer a live selection (it may still be intact), else the snapshot.
    let data: QuoteData | null = null
    const sel = window.getSelection()
    if (sel && !sel.isCollapsed && sel.toString().trim()) {
      const container = quotableContainer(sel)
      if (container) data = deriveQuoteData(sel, container)
    }
    if (!data) data = lastSelectionQuote.value

    if (!data) {
      // Nothing quotable to act on — e.g. the selection was in a region the
      // whitelist rejects, or it was cleared before we were called.
      appLog.w(TAG, 'native selection action with no quotable selection', action)
      return
    }

    // Adopt it as the active quote so both actions below see it. `quote` reads
    // quoteData; `comment` renders it in the expanded bar.
    setQuoteData(data)
    lastSelectionQuote.value = data
    // The native menu's action mode is already gone, so nothing else will
    // re-evaluate and hide the bar out from under the action.
    barPinned.value = true

    if (action === 'quote') {
      // Same commit as tapping 引用 in the in-page toolbar: hand the card to the
      // session picker and dismiss the bar.
      addToConversation('')
      return
    }

    // comment → surface the bar and ask the component to open its input.
    barVisible.value = true
    expandRequest.value++
  }

  function addToConversation(note = '') {
    // Normalise the annotation once, at capture. It used to be trimmed by
    // addStagedQuote on the way into the draft; now the pending request is the
    // intermediate representation, so trimming has to happen here or the
    // picker would show (and deliver) surrounding whitespace.
    const trimmedNote = note.trim()
    if (composerContext.value) {
      const ctx = composerContext.value
      // Build the card BEFORE clearing composerContext/quoteData — the builder
      // reads both (the target from ctx, any selection from quoteData), so
      // clearing first would silently produce no card at all.
      const quote = buildComposerQuote(ctx, trimmedNote)
      if (!quote) return

      // The user committed: the target becomes a quote card whose annotation is
      // whatever they typed. Nothing is injected into the chat input and no
      // separate attachment chip is created — this is the same "one card"
      // interaction as quoting a selection.
      //
      // Destination is always the user's choice (the picker), so the card is
      // NOT staged locally; confirmTarget does that for the session they pick.
      requestTarget({
        mode: 'add',
        quotes: [{ ...quote, id: mintPendingQuoteId() }],
        attachments: [],
        text: '',
      })

      clearComposerState()
      return
    }

    if (!quoteData.value) return
    const quote = quoteData.value
    const sel = window.getSelection()
    if (sel) sel.removeAllRanges()

    requestTarget({
      mode: 'add',
      quotes: [{ ...quote, note: trimmedNote, id: mintPendingQuoteId() }],
      attachments: [],
      text: '',
    })

    setQuoteData(null)
    barVisible.value = false
    barPinned.value = false
    anchorRect.value = null
    lastSelectionQuote.value = null
  }

  /**
   * Send from composer mode. Unlike the file-quote flow this works with NO
   * quote: the message is then just the user's input, carrying the URL
   * attachment that openComposer added.
   *
   * The typed text stays the MESSAGE (it is the user's question); the quote
   * rides along as a card. Only the "+" button folds the text into the card as
   * an annotation — "send" means send now.
   *
   * The destination always comes from the picker, so nothing is sent from here:
   * the payload is snapshotted into the request and `confirmTarget` performs the
   * send for the session the user picks.
   */
  async function sendComposerMessage(userMessage: string) {
    const input = userMessage.trim()
    // "Send" means send now, and a message needs text: the send button is
    // disabled on empty input, so this only guards a programmatic call. (The
    // "+" button is the way to stage the card and keep typing.)
    if (!input) return

    const ctx = composerContext.value
    // Build the card BEFORE clearing anything: the builder reads both
    // composerContext (the target) and quoteData (any selection), so clearing
    // first would silently produce no card.
    const quote = buildComposerQuote(ctx, '')
    const pendingQuotes = quote ? [{ ...quote, id: mintPendingQuoteId() }] : []

    requestTarget({ mode: 'send', quotes: pendingQuotes, attachments: [], text: input })
    clearComposerState()
  }

  async function sendMessage(userMessage: string) {
    if (composerContext.value) return sendComposerMessage(userMessage)
    if (!quoteData.value || !userMessage.trim()) return

    const q = quoteData.value

    requestTarget({
      mode: 'send',
      quotes: [{ ...q, note: '', id: mintPendingQuoteId() }],
      attachments: [],
      text: userMessage.trim(),
    })

    // The bar is dismissed so it cannot float above the picker (--z-quote-bar
    // is above the dialog). The payload lives in the pending request now.
    barVisible.value = false
    barPinned.value = false
    setQuoteData(null)
    anchorRect.value = null
    lastSelectionQuote.value = null
    const sel = window.getSelection()
    if (sel) sel.removeAllRanges()
  }

  return {
    visible: barVisible,
    quoteData,
    anchorRect,
    sheetOpen,
    composerMode,
    composerAttachment,
    // Bumped by the Android native menu to ask the bar to open its input.
    expandRequest,
    openSheet: () => { sheetOpen.value = true },
    closeSheet,
    openComposer,
    hideComposer,
    pinBar,
    unpinBar,
    showBar,
    hideBar,
    addToConversation,
    sendMessage,
    // Entry point for window.__clawbenchSelectionAction (installed in App.vue).
    handleNativeSelectionAction,
  }
}
