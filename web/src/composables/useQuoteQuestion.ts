import { ref, computed, onMounted, onUnmounted } from 'vue'
import { useSessionIdentity } from '@/composables/useSessionIdentity.ts'
import { useToast } from '@/composables/useToast.ts'
import { gt } from '@/composables/useLocale'
import { closestElement, getLineInfo, getFileInfo, getQuoteSource, messageIdFromKey } from '@/utils/quoteQuestionUtils.ts'
import { injectChatInput } from '@/utils/chatInputInjection.ts'
import { useChatContext } from '@/composables/useChatContext.ts'
import type { QuoteData } from '@/composables/useChatContext.ts'

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
 * `onAdd` lets the caller navigate (e.g. switch to the chat tab) without this
 * composable knowing about tabs.
 */
export interface QuoteComposerContext {
  url?: string
  filePath?: string
  label: string
  onAdd?: () => void
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
  addStagedQuote,
  addAttachedFile,
  addUrlAttachment,
  clearAll,
} = useChatContext()
const barVisible = ref(false)
const barPinned = ref(false)  // When pinned, selection loss won't auto-hide the bar
const sheetOpen = ref(false)
// Non-null while the bar was opened by an entry point rather than by a text
// selection. In this mode the bar is useful even with no quote.
const composerContext = ref<QuoteComposerContext | null>(null)
const composerMode = computed(() => composerContext.value !== null)

/**
 * The attachment the composer would add, derived from its context.
 *
 * This is only a *preview*: the real entry lives in useChatContext.attachedFiles
 * and is written on commit (see commitComposerAttachment). Keeping it out of the
 * chat context until then is the whole point — the chat input renders its chips
 * straight from attachedFiles, so attaching on open put the issue/PR/file in the
 * main chat input the moment the button was clicked, and dismissing the bar left
 * it behind.
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
    }
    return
  }

  // Check if selection is within a code, markdown, or office preview area, or
  // inside a chat message's content.
  //
  // The chat selector must be `.chat-message .msg-content-wrapper`, NOT bare
  // `.chat-message`: the row also contains the meta bar, so the wider selector
  // would surface the bar for selections in the timestamp/action area (the
  // chrome guard above catches the buttons, but the wrapper is the precise
  // content boundary).
  const container = closestElement(sel.anchorNode, '.raw-content-pre, .markdown-body, .office-preview-body, .chat-message .msg-content-wrapper')
  if (!container) {
    if (!barPinned.value && !composerContext.value) {
      barVisible.value = false
    }
    return
  }

  const text = sel.toString().trim()
  if (!text) {
    if (!barPinned.value && !composerContext.value) {
      barVisible.value = false
    }
    return
  }

  // A labelled non-file source (an issue/PR body) supplies the quote's identity.
  // It has no meaningful file line numbers, so those stay 0 — appending ":0"
  // would be noise in the fence header.
  const source = getQuoteSource(container)
  if (source) {
    setQuoteData({
      text,
      filePath: source.label,
      language: source.language,
      startLine: 0,
      endLine: 0,
      sourceKind: source.url ? 'url' : 'file',
      ...(source.url ? { url: source.url } : {}),
    })
    barVisible.value = true
    return
  }

  // A selection inside a chat message has no file and no line numbers; it is
  // attributed to the message it came from so the quote can be traced back.
  // Optimistic messages have no DB id — the quote is still valid, just not
  // addressable later.
  const msgEl = container.closest('.chat-message')
  if (msgEl) {
    const messageId = messageIdFromKey(msgEl.getAttribute('data-msg-key'))
    setQuoteData({
      text,
      filePath: '',
      language: '',
      startLine: 0,
      endLine: 0,
      sourceKind: 'message',
      ...(messageId !== undefined ? { messageId } : {}),
    })
    barVisible.value = true
    return
  }

  const { filePath, language } = getFileInfo(container)
  const { startLine, endLine } = getLineInfo(sel)

  setQuoteData({ text, filePath, language, startLine, endLine })
  barVisible.value = true
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
  const toast = useToast()
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
      pointerCount = 0
      if (pointerReleaseTimer) {
        clearTimeout(pointerReleaseTimer)
        pointerReleaseTimer = null
      }
    }
  })

  function closeSheet() {
    const sel = window.getSelection()
    if (sel) sel.removeAllRanges()
    barVisible.value = false
    barPinned.value = false
    composerContext.value = null
    setQuoteData(null)
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
    if (!sel || sel.isCollapsed || !sel.toString().trim()) {
      setQuoteData(null)
    }
    barVisible.value = true
    barPinned.value = true
  }

  /**
   * Move the composer's pending attachment into the chat context. Called on
   * commit only — both add* helpers dedupe, so committing twice adds one chip.
   */
  function commitComposerAttachment(ctx: QuoteComposerContext | null) {
    if (!ctx) return
    if (ctx.url) {
      addUrlAttachment(ctx.url, ctx.label)
    } else if (ctx.filePath) {
      addAttachedFile(ctx.filePath)
    }
  }

  /** Close the composer without touching unrelated staged quotes. */
  function hideComposer() {
    if (!composerContext.value) return
    composerContext.value = null
    barVisible.value = false
    barPinned.value = false
    setQuoteData(null)
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
   */
  function hideBar() {
    barVisible.value = false
    barPinned.value = false
    composerContext.value = null
    setQuoteData(null)
  }

  /**
   * 编程式显示引用问答栏（不依赖 selectionchange 事件）。
   * 默认延迟 400ms 显示，避免双击的 pointerdown 事件触发"点击外部关闭"
   * （markdown 预览双击复制依赖此延迟）。传 { delay: 0 } 可立即显示
   * （代码模式拖选无 pointerdown 干扰）。
   */
  function showBar(data: QuoteData, opts: { delay?: number } = {}) {
    setTimeout(() => {
      setQuoteData(data)
      barVisible.value = true
    }, opts.delay ?? 400)
  }

  function addToConversation(note = '') {
    if (composerContext.value) {
      const ctx = composerContext.value
      // Capture the selection BEFORE clearing it — this is the quote the
      // composer is about (empty when the user selected nothing).
      const q = quoteData.value
      composerContext.value = null
      setQuoteData(null)
      barVisible.value = false
      barPinned.value = false
      // The user committed, so the previewed attachment becomes real now.
      commitComposerAttachment(ctx)
      if (q) {
        // One card holding the quoted content plus the typed annotation,
        // matching the file browser and forge selection flows. The text is NOT
        // injected into the chat input — that was the old composer behaviour.
        addStagedQuote(q, note)
      } else if (note.trim()) {
        // Nothing was quoted, so there is no card to attach the text to.
        // Injecting it keeps the user's typing instead of discarding it
        // silently; the attachment chip still rides along.
        injectChatInput(note)
      }
      ctx.onAdd?.()
      return
    }

    if (!quoteData.value) return
    addStagedQuote(quoteData.value, note)
    const sel = window.getSelection()
    if (sel) sel.removeAllRanges()
    setQuoteData(null)
    barVisible.value = false
    barPinned.value = false
    toast.show(gt('quoteBar.addedToChat'), { icon: '📎', type: 'success', duration: 1500 })
  }

  /**
   * Send from composer mode. Unlike the file-quote flow this works with NO
   * quote: the message is then just the user's input, carrying the URL
   * attachment that openComposer added.
   *
   * The typed text stays the MESSAGE (it is the user's question); the quote
   * rides along as a card. Only the "+" button folds the text into the card as
   * an annotation — "send" means send now.
   */
  async function sendComposerMessage(userMessage: string) {
    const q = quoteData.value
    const input = userMessage.trim()
    if (!q && !input) return

    // Capture animation coordinates BEFORE any await — the bar's handleSend()
    // collapses synchronously right after emit('send').
    const sendBtn = document.querySelector('.qq-send-btn')
    const dockChatBtn = document.querySelector('.dock-center')?.querySelector('.dock-btn')
    const animFrom = sendBtn?.getBoundingClientRect() ?? null
    const animTo = dockChatBtn?.getBoundingClientRect() ?? null

    const ctx = composerContext.value
    composerContext.value = null
    barVisible.value = false
    barPinned.value = false
    setQuoteData(null)
    // Commit the previewed attachment and stage the captured selection so both
    // ride along as cards. The registered ChatPanel handler reads them
    // synchronously before its first await, so they must be in place now.
    commitComposerAttachment(ctx)
    if (q) addStagedQuote(q)

    try {
      const sendPromise = sessionIdentity.sendMessage(input)
      // The registered ChatPanel handler captures files synchronously before its
      // first await, so the attachments ride along; clear after.
      clearAll()
      await sendPromise
      toast.show(gt('quoteBar.sentToSession'), { icon: '✅', type: 'success', duration: 2000 })
      if (animFrom && animTo) {
        window.dispatchEvent(new CustomEvent('quote-sent', {
          detail: {
            from: { x: animFrom.left + animFrom.width / 2, y: animFrom.top + animFrom.height / 2 },
            to: { x: animTo.left + animTo.width / 2, y: animTo.top + animTo.height / 2 },
          }
        }))
      }
    } catch (err: unknown) {
      toast.show(gt('quoteBar.sendFailed', { error: (err as Error).message }), { icon: '⚠️', type: 'error' })
    }
  }

  async function sendMessage(userMessage: string) {
    if (composerContext.value) return sendComposerMessage(userMessage)
    if (!quoteData.value || !userMessage.trim()) return

    const q = quoteData.value
    // Reuse the staging dedupe rule so reselecting an already staged range
    // does not include the same quote twice in an immediate send.
    addStagedQuote(q)

    // Capture animation coordinates BEFORE any await — the bar's handleSend()
    // sets expanded=false synchronously right after emit('send'), so the
    // .qq-send-btn element will be removed from DOM on the next tick.
    const sendBtn = document.querySelector('.qq-send-btn')
    const dockChatBtn = document.querySelector('.dock-center')?.querySelector('.dock-btn')
    const animFrom = sendBtn?.getBoundingClientRect() ?? null
    const animTo = dockChatBtn?.getBoundingClientRect() ?? null

    // Keep the staged quotes for ChatPanelContent to materialise into cards.
    // They are deliberately NOT baked into the message text any more.
    barVisible.value = false
    barPinned.value = false
    setQuoteData(null)

    // Delegate to session identity singleton — it routes to ChatPanel's
    // sendMessage if registered, otherwise falls back to a direct API call.
    try {
      const sendPromise = sessionIdentity.sendMessage(userMessage.trim())
      // The registered ChatPanel handler captures the quotes synchronously
      // before its first await. Clear this batch now so a later response cannot
      // wipe the next set of quotes the user starts collecting while the
      // request runs.
      clearAll()
      await sendPromise
      toast.show(gt('quoteBar.sentToSession'), { icon: '✅', type: 'success', duration: 2000 })
      // Dispatch animation event with pre-captured coordinates
      if (animFrom && animTo) {
        window.dispatchEvent(new CustomEvent('quote-sent', {
          detail: {
            from: { x: animFrom.left + animFrom.width / 2, y: animFrom.top + animFrom.height / 2 },
            to: { x: animTo.left + animTo.width / 2, y: animTo.top + animTo.height / 2 },
          }
        }))
      }
    } catch (err: unknown) {
      toast.show(gt('quoteBar.sendFailed', { error: (err as Error).message }), { icon: '⚠️', type: 'error' })
    }
  }

  return {
    visible: barVisible,
    quoteData,
    sheetOpen,
    composerMode,
    composerAttachment,
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
  }
}
