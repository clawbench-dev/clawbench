import { ref, onMounted, onUnmounted } from 'vue'
import { useSessionIdentity } from '@/composables/useSessionIdentity.ts'
import { useToast } from '@/composables/useToast.ts'
import { gt } from '@/composables/useLocale'
import { closestElement, getLineInfo, getFileInfo, buildMultiQuoteMessage } from '@/utils/quoteQuestionUtils.ts'
import { useChatContext } from '@/composables/useChatContext.ts'
import type { QuoteData } from '@/composables/useChatContext.ts'

// Module-level singleton: bar visibility state shared across all consumers.
// The active selection stays separate from staged quotes so dismissing a
// selection never discards snippets the user already added to the chat draft.
const {
  quoteData,
  stagedQuotes,
  setQuoteData,
  addStagedQuote,
  addAttachedFile,
  clearQuotes,
  clearAll,
} = useChatContext()
const barVisible = ref(false)
const barPinned = ref(false)  // When pinned, selection loss won't auto-hide the bar
const sheetOpen = ref(false)

let debounceTimer: ReturnType<typeof setTimeout> | null = null
let pointerSelecting = false
let pendingShow: QuoteData | null = null

function scheduleSelectionChange(delay = 150) {
  if (debounceTimer) clearTimeout(debounceTimer)
  debounceTimer = setTimeout(() => {
    // A native text selection can emit selectionchange while the pointer is
    // still down. Do not show the bar until the drag has finished.
    if (pointerSelecting) return

    const sel = window.getSelection()
    if (!sel || sel.isCollapsed || !sel.toString().trim()) {
      // Drop only the active selection. Staged quotes remain in the chat draft.
      if (!barPinned.value) {
        barVisible.value = false
        setQuoteData(null)
      }
      return
    }

    // CodeMirror viewers (CodeMirrorViewer) manage their own selection + quote
    // bar via an internal selection listener. This DOM selection is only a
    // shadow of CM's internal one, so skip it — otherwise it would hide/show
    // the bar in parallel with the editor's own handler.
    if (closestElement(sel.anchorNode, '.cm-editor')) return

    // Check if selection is within a code, markdown, or office preview area
    const container = closestElement(sel.anchorNode, '.raw-content-pre, .markdown-body, .office-preview-body')
    if (!container) {
      if (!barPinned.value) {
        barVisible.value = false
      }
      return
    }

    const text = sel.toString().trim()
    if (!text) {
      if (!barPinned.value) {
        barVisible.value = false
      }
      return
    }

    const { filePath, language } = getFileInfo(container)
    const { startLine, endLine } = getLineInfo(sel)

    setQuoteData({ text, filePath, language, startLine, endLine })
    barVisible.value = true
  }, delay)
}

function onSelectionChange() {
  scheduleSelectionChange()
}

function applyShow(data: QuoteData) {
  setQuoteData(data)
  barVisible.value = true
}

function onPointerDown(event: PointerEvent) {
  if (event.button === 0) pointerSelecting = true
}

function onPointerUp() {
  if (!pointerSelecting) return
  pointerSelecting = false

  // CodeMirror reports its selection independently of the DOM. If its
  // callback fired during the drag, flush that request after release.
  if (pendingShow) {
    const data = pendingShow
    pendingShow = null
    applyShow(data)
  }

  // Process the final native selection immediately after pointerup.
  scheduleSelectionChange(0)
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
    }
  })

  onUnmounted(() => {
    listenerCount--
    if (listenerCount === 0) {
      document.removeEventListener('selectionchange', onSelectionChange)
      document.removeEventListener('pointerdown', onPointerDown)
      document.removeEventListener('pointerup', onPointerUp)
      document.removeEventListener('pointercancel', onPointerUp)
      if (debounceTimer) clearTimeout(debounceTimer)
      debounceTimer = null
      pointerSelecting = false
      pendingShow = null
    }
  })

  function closeSheet() {
    pendingShow = null
    const sel = window.getSelection()
    if (sel) sel.removeAllRanges()
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
    pendingShow = null
    barVisible.value = false
    barPinned.value = false
    setQuoteData(null)
  }

  /**
   * 编程式显示引用问答栏（不依赖 selectionchange 事件）。
   * 默认延迟 400ms 显示，避免双击的 pointerdown 事件触发"点击外部关闭"
   * （markdown 预览双击复制依赖此延迟）。传 { delay: 0 } 可立即显示
   * （代码模式使用此路径；拖选期间仍会统一等到 pointerup）。
   */
  function showBar(data: QuoteData, opts: { delay?: number } = {}) {
    setTimeout(() => {
      if (pointerSelecting) {
        pendingShow = data
        return
      }
      applyShow(data)
    }, opts.delay ?? 400)
  }

  function addToConversation(note = '') {
    if (!quoteData.value) return
    addStagedQuote(quoteData.value, note)
    const sel = window.getSelection()
    if (sel) sel.removeAllRanges()
    setQuoteData(null)
    barVisible.value = false
    barPinned.value = false
    toast.show(gt('quoteBar.addedToChat'), { icon: '📎', type: 'success', duration: 1500 })
  }

  async function sendMessage(userMessage: string) {
    if (!quoteData.value || !userMessage.trim()) return

    const q = quoteData.value
    // Reuse the staging dedupe rule so reselecting an already staged range
    // does not include the same quote twice in an immediate send.
    addStagedQuote(q)
    const quotes = [...stagedQuotes.value]

    for (const quote of quotes) {
      if (quote.filePath) {
        addAttachedFile(quote.filePath, false, quote.startLine, quote.endLine)
      }
    }

    const message = buildMultiQuoteMessage(userMessage, quotes)

    // Capture animation coordinates BEFORE any await — the bar's handleSend()
    // sets expanded=false synchronously right after emit('send'), so the
    // .qq-send-btn element will be removed from DOM on the next tick.
    const sendBtn = document.querySelector('.qq-send-btn')
    const dockChatBtn = document.querySelector('.dock-center')?.querySelector('.dock-btn')
    const animFrom = sendBtn?.getBoundingClientRect() ?? null
    const animTo = dockChatBtn?.getBoundingClientRect() ?? null

    // Keep attached files long enough for ChatPanelContent to capture them, but
    // clear quotes before delegating so they are not embedded a second time.
    clearQuotes()
    barVisible.value = false
    barPinned.value = false

    // Delegate to session identity singleton — it routes to ChatPanel's
    // sendMessage if registered, otherwise falls back to a direct API call.
    try {
      const sendPromise = sessionIdentity.sendMessage(message)
      // The registered ChatPanel handler captures files synchronously before its
      // first await. Clear this batch now so a later response cannot wipe the
      // next set of quotes the user starts collecting while the request runs.
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
    openSheet: () => { sheetOpen.value = true },
    closeSheet,
    pinBar,
    unpinBar,
    showBar,
    hideBar,
    addToConversation,
    sendMessage,
  }
}
