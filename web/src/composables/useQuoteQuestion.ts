import { ref, computed, watch, onMounted, onUnmounted } from 'vue'
import { useSessionIdentity, getSessionId, getSessionTitle } from '@/composables/useSessionIdentity.ts'
import { closestElement, getLineInfo, getFileInfo, getQuoteSource, messageIdFromKey } from '@/utils/quoteQuestionUtils.ts'
import { buildMessageQuote } from '@/utils/quoteItem.ts'
import { useChatContext } from '@/composables/useChatContext.ts'
import type { QuoteData } from '@/composables/useChatContext.ts'
import { requestTarget, mintPendingQuoteId } from '@/composables/useConversationTarget.ts'
import type { PendingQuote } from '@/composables/useConversationTarget.ts'
import { usePlatformDetect } from '@/composables/usePlatformDetect'
import { openInertPathPicker } from '@/composables/useInertPathPicker.ts'
import { unionRectOfRects } from '@/utils/selectionAnchor.ts'
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
 * Pending `showBar` delay (issue #572). Held so a dismissal can CANCEL it: a
 * show queued before the user started dragging would otherwise fire with the
 * button still down, surfacing the bar mid-drag — the very defect being fixed.
 */
let showTimer: ReturnType<typeof setTimeout> | null = null

/**
 * Rect the floating toolbar should attach to: the BOUNDING BOX of the whole
 * selection (issue #572).
 *
 * The rule itself lives in `utils/selectionAnchor.ts` because a preview iframe
 * needs the SAME rule over its own (differently-origined) rects; two copies is
 * exactly how the host path got the union while the iframe path kept the last
 * box. See that module for why the union is required.
 *
 * Returns null when the selection has no laid-out boxes (e.g. a collapsed
 * range), letting the caller fall back to the pinned top position.
 *
 * The rect is stored RAW (zoom-scaled): conversion to pre-zoom `position:fixed`
 * space happens in computeSelectionToolbarStyle, so both sides of that
 * comparison stay in one space.
 */
function selectionAnchorRect(sel: Selection): DOMRect | null {
  if (sel.rangeCount === 0) return null
  const rects = sel.getRangeAt(0).getClientRects()
  if (!rects.length) return null
  const u = unionRectOfRects(rects)
  if (!u) return null
  return new DOMRect(u.left, u.top, u.right - u.left, u.bottom - u.top)
}

// Active pointer (mouse/touch) tracking. Browsers fire selectionchange while the
// user is still dragging, so a debounced evaluation can run mid-drag and surface
// the bar before the pointer is released — on desktop the bar then auto-expands
// and focuses its input, stealing focus and cutting the selection short. While
// any pointer button is held the bar is held back; pointerup re-runs the check.
let pointerCount = 0

/**
 * Window without any pointer activity after which the drag guard self-heals.
 *
 * Needed because a mobile native selection UI can swallow the matching
 * pointerup/touchend, which would leave the guard held forever and block the
 * bar from ever appearing.
 */
const POINTER_STUCK_MS = 700

/**
 * Absolute ceiling on how long a single press may hold the guard, however busy
 * the pointer looks.
 *
 * The liveness signal is only evidence, not proof: a programmatic scroll (the
 * chat list following a streaming reply, a smooth-scroll animation) keeps
 * refreshing it, so if a release really was swallowed the guard could be
 * extended indefinitely and the bar would never appear at all. Past this
 * ceiling the guard is dropped regardless — a swallowed release then costs one
 * spurious bar, instead of a permanently dead one.
 */
const POINTER_GUARD_MAX_MS = 5000

/**
 * When the current press began. Anchors `POINTER_GUARD_MAX_MS`; 0 while idle.
 */
let guardStartedAt = 0

/**
 * Timestamp of the last event that PROVED a button is still pressed — a
 * `pointerdown`, a `pointermove` carrying a pressed button, or a scroll (which
 * is what auto-scroll at the viewport edge looks like while the pointer holds
 * still).
 *
 * The self-heal timer consults it before dropping the guard (issue #572):
 * dragging across a long paragraph takes far longer than the window, and the
 * old unconditional reset released the guard mid-drag, surfacing the toolbar
 * over the text being selected at a fixed time after the drag started. A live
 * drag keeps refreshing this, so the timer only fires once the pointer has
 * genuinely gone quiet.
 */
let lastPointerActivity = 0

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
  // A press supersedes any show still waiting out its delay. `showBar` can be
  // called with the default 400ms delay (double-click-to-quote paths); if the
  // user starts a new drag inside that window the timer would fire with the
  // button down, surfacing the PREVIOUS selection's bar mid-drag — the very
  // defect issue #572 is about. The dismissal paths already cancel it; a press
  // is the one non-dismissal that must too.
  cancelPendingShow()
  pointerCount++
  lastPointerActivity = Date.now()
  if (guardStartedAt === 0) guardStartedAt = lastPointerActivity
  // Safety net: mobile native selection UI can swallow the matching pointerup/
  // touchend, which would leave the guard held forever and block the quote bar
  // from appearing. Release it after a short window so a settling selection can
  // still surface the bar (the collapsed bar no longer steals focus mid-drag).
  armStuckTimer()
}

/**
 * (Re)arm the self-heal timer that drops a guard whose release event was
 * swallowed.
 *
 * It only drops the guard once the pointer has been quiet for the whole window.
 * A drag in progress keeps refreshing `lastPointerActivity`, so a busy pointer
 * just re-arms the timer — that is what keeps a long sweep from surfacing the
 * bar mid-drag (issue #572).
 */
function armStuckTimer() {
  if (pointerReleaseTimer) clearTimeout(pointerReleaseTimer)
  pointerReleaseTimer = setTimeout(() => {
    pointerReleaseTimer = null
    const busy = Date.now() - lastPointerActivity < POINTER_STUCK_MS
    const overCeiling = guardStartedAt > 0 && Date.now() - guardStartedAt >= POINTER_GUARD_MAX_MS
    if (pointerCount > 0 && busy && !overCeiling) {
      armStuckTimer()
      return
    }
    pointerCount = 0
    guardStartedAt = 0
    // Re-evaluate: a selection that settled while the guard was stuck can now
    // surface the bar.
    if (debounceTimer) {
      clearTimeout(debounceTimer)
      debounceTimer = null
    }
    debounceTimer = setTimeout(evaluateSelection, 0)
  }, POINTER_STUCK_MS)
}

/**
 * Cancel a `showBar` that is still waiting out its delay.
 *
 * Every dismissal path needs this: a show queued before the user started
 * dragging (or before another surface took over) would otherwise fire later and
 * surface a bar nobody asked for — with the button possibly still down, which is
 * exactly the mid-drag bar issue #572 is about.
 */
function cancelPendingShow() {
  if (showTimer) {
    clearTimeout(showTimer)
    showTimer = null
  }
}

/**
 * Take back a bar the self-heal timer surfaced, when it turns out the pointer is
 * still down (issue #572).
 *
 * The timer cannot tell a swallowed release from a user who simply paused
 * mid-drag, so it may show the bar over text that is still being selected. The
 * next event that PROVES the button is down has to undo that — otherwise the bar
 * stays on top of the selection for the rest of the drag, which is exactly the
 * reported flicker. Pinned bars (the expanded composer) are left alone.
 */
function retractBarForDrag() {
  if (!barVisible.value || barPinned.value || composerContext.value) return
  cancelPendingShow()
  barVisible.value = false
  setQuoteData(null)
  anchorRect.value = null
  // Same reason as every other dismissal path: the snapshot belongs to the
  // selection this drag has just superseded, and the Android menu would quote it.
  lastSelectionQuote.value = null
}

/**
 * A pointer is moving (issue #572).
 *
 * Two facts live in this handler, and both are needed:
 *
 *  - `buttons !== 0` proves a drag is in progress. It refreshes
 *    `lastPointerActivity` so the self-heal timer keeps re-arming instead of
 *    releasing the guard part-way through a long sweep, and it re-arms the guard
 *    if the timer already dropped it (a long pause before the drag began). The
 *    bar the timer may have surfaced is retracted at the same time.
 *  - `buttons === 0` while the guard is up means the MOUSE button came up without
 *    a `pointerup` reaching us. Releasing here is immediate and precise, instead
 *    of waiting for a quiet window that a slow selection would never satisfy.
 *    Restricted to mouse: touch/pen drags report `buttons` inconsistently across
 *    engines, and treating a 0 there as "released" would surface the bar in the
 *    middle of every touch selection.
 *
 * Only ever raises the count to 1 (never increments), so the release paths stay
 * balanced.
 */
function onPointerMove(e: PointerEvent) {
  if (e.buttons === 0) {
    if (pointerCount > 0 && e.pointerType === 'mouse') releasePointer()
    return
  }
  lastPointerActivity = Date.now()
  if (pointerCount === 0) pointerCount = 1
  if (guardStartedAt === 0) guardStartedAt = lastPointerActivity
  // Unconditional: re-arming is idempotent (it clears the old timer first) and
  // it is what keeps a SECOND finger's drag alive — `releasePointer` cancels the
  // timer on every release, so a press that is still held would otherwise be
  // left without one and could never self-heal.
  armStuckTimer()
  retractBarForDrag()
}


function releasePointer() {
  if (pointerReleaseTimer) {
    clearTimeout(pointerReleaseTimer)
    pointerReleaseTimer = null
  }
  if (pointerCount > 0) pointerCount--
  if (pointerCount === 0) {
    guardStartedAt = 0
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
  // Scrolling while a pointer is still held is PROOF the drag is alive: this is
  // what auto-scroll at the edge of the viewport looks like — the pointer sits
  // still while the content moves under it. Without this the self-heal timer
  // sees a quiet pointer, drops the guard, surfaces the bar, and the very next
  // scroll event hides it again — the reported flicker (issue #572). Must come
  // BEFORE the `barVisible` early return, which is false for most of a drag.
  if (pointerCount > 0) lastPointerActivity = Date.now()

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

/** True when the host draws its own selection menu (Android shell). */
export function isNativeSelectionShell(): boolean {
  return androidShell()
}

/** The `showBar` shape the iframe bridge helper needs (structural — no import cycle). */
type ShowBarFn = (
  data: QuoteData,
  opts?: { delay?: number; anchor?: DOMRect | null; suppressBar?: boolean },
) => void

/**
 * Build the `show` callback a preview iframe's quote bridge needs.
 *
 * Every iframe host (the full-screen HTML preview, the inline quick preview, the
 * OpenAPI/Swagger docs) needs the SAME three decisions, and each is easy to
 * forget in a copy:
 *
 *   - pass the mapped `anchor` through, or the bar falls back to its pinned top
 *     position, far from the text the user just selected;
 *   - `delay: 0`, because the bridge's own debounce has already coalesced the
 *     selection;
 *   - `suppressBar` on Android, where the system's selection menu is the visible
 *     affordance and an in-page bar on top of it would be a duplicate (the quote
 *     is still recorded for that menu's action to read back).
 *
 * They were three hand-written copies and one of them (OpenApiPreview) silently
 * missed `anchor` and `suppressBar` — the reported "menu pops up at the top"
 * defect survived there. Callers now share this factory, so a fourth host
 * cannot drift.
 */
export function makeIframeQuoteShowHandler(showBar: ShowBarFn): (data: QuoteData, anchor: DOMRect | null) => void {
  return (data, anchor) => showBar(data, {
    delay: 0,
    anchor,
    suppressBar: isNativeSelectionShell(),
  })
}

export function useQuoteQuestion() {
  const sessionIdentity = useSessionIdentity()

  onMounted(() => {
    listenerCount++
    if (listenerCount === 1) {
      document.addEventListener('selectionchange', onSelectionChange)
      document.addEventListener('pointerdown', onPointerDown)
      // Drag liveness (issue #572): a long sweep must keep the guard up, so the
      // self-heal timer is refreshed by every move with a button still pressed.
      document.addEventListener('pointermove', onPointerMove)
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
      document.removeEventListener('pointermove', onPointerMove)
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
    cancelPendingShow()
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
    cancelPendingShow()
    barVisible.value = false
    barPinned.value = false
    anchorRect.value = null
    lastSelectionQuote.value = null
  }

  /** Close the composer without touching unrelated staged quotes. */
  function hideComposer() {
    if (!composerContext.value) return
    composerContext.value = null
    cancelPendingShow()
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
    cancelPendingShow()
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
   * `anchor` is the selection's rect in HOST viewport coordinates, supplied by
   * callers whose selection is not in the host document — an iframe reports its
   * rects in its OWN viewport, so it must map them first (see
   * mapIframeRectToHost). Without it the bar can only pin to the top of the
   * page, far from the text the user just selected.
   *
   * The host selection is still read as a fallback, and it must be read
   * SYNCHRONOUSLY before the delay: by the time the timeout runs the selection
   * may already be cleared (the double-click that triggered this collapses it),
   * and a cleared selection has no rect to measure.
   *
   * `suppressBar` keeps the quote but hides the bar. Android uses it for
   * selections made in a surface that ALSO gets the system's own selection
   * menu (an iframe): the native menu is the visible affordance there, and an
   * in-page bar on top of it would be a duplicate — but the quote still has to
   * be recorded, because that menu's action reads it back
   * (handleNativeSelectionAction).
   */
  function showBar(
    data: QuoteData,
    opts: { delay?: number; anchor?: DOMRect | null; suppressBar?: boolean } = {},
  ) {
    const sel = window.getSelection()
    const rect = opts.anchor !== undefined
      ? opts.anchor
      : (sel && !sel.isCollapsed && sel.toString().trim() ? selectionAnchorRect(sel) : null)
    const suppress = opts.suppressBar === true
    if (suppress) {
      // The native menu's action runs AFTER the action mode has dismissed and
      // cleared the selection, so this snapshot is the only copy left.
      lastSelectionQuote.value = data
    }
    // Held so a dismissal can cancel it (issue #572): a show queued before the
    // user started dragging would otherwise fire with the button still down.
    if (showTimer) clearTimeout(showTimer)
    showTimer = setTimeout(() => {
      showTimer = null
      if (suppress) {
        // The bar is hidden because the OS draws its own menu, so NOTHING on
        // screen would explain a quote card appearing in the composer. Writing
        // `quoteData` here did exactly that: on Android, merely selecting text
        // inside a preview (then dismissing the system menu) left a stray quote
        // card above the input. The snapshot in `lastSelectionQuote` is enough —
        // `handleNativeSelectionAction` writes `quoteData` itself when the user
        // actually picks an action from that menu.
        anchorRect.value = rect
        return
      }
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
    if (action !== 'quote' && action !== 'comment' && action !== 'search-file') {
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

    // "搜索文件" treats the selected TEXT as a path and opens the filename
    // search drawer — the SAME drawer tapping a dashed (verified-missing) path
    // opens — but available for any string, including one the renderer never
    // annotated.
    //
    // It deliberately does NOT touch the quote state: no card is staged, the
    // bar is not pinned, and the selection is not adopted as a quote. The user
    // asked to navigate, not to quote.
    if (action === 'search-file') {
      openSelectionAsFile(data.text)
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
      // Same commit as tapping 引用 in the in-page toolbar: deliver the card to
      // the active conversation.
      //
      // The native menu has NO long-press: this item is a single tap and there
      // is no second gesture available on it, so cross-conversation delivery is
      // not reachable from here. It is reachable via 讨论, which opens the
      // in-page bar — and that bar's commit buttons DO support long-press
      // ("send to another conversation"). Keeping 引用 one-tap for the 99% case
      // and leaving the detour to 讨论 is the deliberate trade-off; the
      // alternative (making 引用 open a picker) reinstates exactly the friction
      // issue #560 removed.
      addToConversation('')
      return
    }

    // comment → surface the bar and ask the component to open its input.
    barVisible.value = true
    expandRequest.value++
  }

  /**
   * Open the filename-search picker for the selected text.
   *
   * This is the "搜索文件" entry from the native selection menu, and it opens
   * EXACTLY the same drawer as clicking a dashed (verified-missing) path:
   * `InertPathPicker`. Delegating to `openInertPathPicker` is deliberate — that
   * drawer already owns the candidate list, the line-range hand-off and the
   * "open at line" behaviour, so re-implementing a picker here would guarantee
   * the two entry points drift.
   *
   * The drawer searches by BASENAME (`deriveSearchQuery` strips the directory
   * and any `:42` line suffix), which is what makes an arbitrary selected
   * string work: a chat message often shows only a partial path, and the file
   * usually lives under a different prefix.
   */
  function openSelectionAsFile(raw: string): void {
    const candidate = pickPathLikeToken(raw)
    if (!candidate) return
    openInertPathPicker({ path: candidate })
  }

  /**
   * Choose the most path-like token in a selection.
   *
   * A selection often carries surrounding prose ("see src/a.ts", "in
   * `config.yaml`"). Taking the FIRST token would search for "see" — a word that
   * matches hundreds of files. So prefer a token that looks like a path (a
   * separator, an extension, or a dot-prefixed name), and fall back to the whole
   * trimmed text only when no token looks like one (a bare filename such as
   * "main" is still a legitimate query).
   *
   * Longest wins among path-like tokens: in "src/a.ts or src/b.ts" either is
   * acceptable, but in "a and web/src/long/name.ts" the specific one is what the
   * user pointed at.
   */
  function pickPathLikeToken(raw: string): string {
    const text = raw.trim()
    if (!text) return ''
    // Strip wrapping punctuation a selection may carry: a leading backtick,
    // quote, paren, bracket or brace, and a trailing one plus sentence
    // punctuation ("see src/a.ts.").
    const LEAD = /^[`'"([{]+/
    const TRAIL = /[`'")},.;:\]]+$/
    const tokens = text
      .split(/\s+/)
      .map(t => t.replace(LEAD, '').replace(TRAIL, ''))
      .filter(Boolean)
    if (tokens.length === 0) return ''
    const pathLike = tokens.filter(t => /[/\\]/.test(t) || /\.[A-Za-z0-9]{1,10}$/.test(t))
    if (pathLike.length === 0) return tokens[0]
    return pathLike.reduce((a, b) => (b.length > a.length ? b : a))
  }

  /**
   * Commit the current quote (or composer target) as a card.
   *
   * The destination is the conversation the user is working in — a comment on
   * what you are reading belongs there, and asking "which conversation?" on
   * every single comment is the friction this removed (issue #560). When there
   * is no active session at all, `requestTarget` falls back to the picker.
   *
   * `pickTarget` forces the picker for THIS commit. It is what the long-press /
   * right-click entry sets: cross-session delivery is still reachable, it just
   * stops occupying the primary path.
   */
  function addToConversation(note = '', opts: { pickTarget?: boolean } = {}) {
    // Normalise the annotation once, at capture. It used to be trimmed by
    // addStagedQuote on the way into the draft; now the pending request is the
    // intermediate representation, so trimming has to happen here or the
    // picker would show (and deliver) surrounding whitespace.
    const trimmedNote = note.trim()
    const deliverOpts = opts.pickTarget ? {} : { deliverTo: 'active' as const }
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
      // The card is NOT staged locally: requestTarget delivers it (active
      // session, or the session the user picks).
      requestTarget({
        mode: 'add',
        quotes: [{ ...quote, id: mintPendingQuoteId() }],
        attachments: [],
        text: '',
      }, deliverOpts)

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
    }, deliverOpts)

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
   * The destination is the conversation the user is working in (issue #560),
   * same as the add path; `pickTarget` (long-press) forces the picker.
   */
  async function sendComposerMessage(userMessage: string, opts: { pickTarget?: boolean } = {}) {
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

    requestTarget(
      { mode: 'send', quotes: pendingQuotes, attachments: [], text: input },
      opts.pickTarget ? {} : { deliverTo: 'active' },
    )
    clearComposerState()
  }

  async function sendMessage(userMessage: string, opts: { pickTarget?: boolean } = {}) {
    if (composerContext.value) return sendComposerMessage(userMessage, opts)
    if (!quoteData.value || !userMessage.trim()) return

    const q = quoteData.value

    requestTarget({
      mode: 'send',
      quotes: [{ ...q, note: '', id: mintPendingQuoteId() }],
      attachments: [],
      text: userMessage.trim(),
    }, opts.pickTarget ? {} : { deliverTo: 'active' })

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
