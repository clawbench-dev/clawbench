import { ref, onMounted, onUnmounted } from 'vue'
import { useSessionIdentity } from '@/composables/useSessionIdentity.ts'
import { useToast } from '@/composables/useToast.ts'
import { gt } from '@/composables/useLocale'
import { closestElement, getLineInfo, getFileInfo, buildQuoteMessage } from '@/utils/quoteQuestionUtils.ts'
import { useChatContext } from '@/composables/useChatContext.ts'
import type { QuoteData } from '@/composables/useChatContext.ts'
import { useWideScreenLayout } from '@/composables/useWideScreenLayout.ts'

// Module-level singleton: bar visibility state shared across all consumers.
// quoteData is stored in useChatContext (global singleton) so ChatInputBar
// can render a quote chip in any tab.
const { quoteData, setQuoteData, addAttachedFile, clearAll } = useChatContext()
const { isWideScreen } = useWideScreenLayout()
const barVisible = ref(false)
const barPinned = ref(false)  // When pinned, selection loss won't auto-hide the bar
const sheetOpen = ref(false)

let debounceTimer: ReturnType<typeof setTimeout> | null = null

function onSelectionChange() {
  if (debounceTimer) clearTimeout(debounceTimer)
  debounceTimer = setTimeout(() => {
    const sel = window.getSelection()
    if (!sel || sel.isCollapsed || !sel.toString().trim()) {
      // Wide-screen mode: auto-pin so the ChatInputBar quote tag survives
      // focus switches (e.g. clicking the input textarea clears selection).
      // There is no QuoteQuestionBar in wide-screen, so the quote tag in
      // ChatInputBar is the only UI surface — losing it would be confusing.
      if (isWideScreen.value && quoteData.value) {
        barPinned.value = true
        barVisible.value = false
        return
      }
      // Narrow mode: when the selection is gone, drop the quote — unless the
      // user explicitly pinned it via "引用提问". A stale quote chip must not
      // linger in the chat input after the user deselects the text.
      if (!barPinned.value) {
        barVisible.value = false
        setQuoteData(null)
      }
      return
    }

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
    // Wide-screen: auto-pin so the quote tag persists when user focuses input
    if (isWideScreen.value) {
      barPinned.value = true
    }
  }, 150)
}

// Global listener management
let listenerCount = 0

/** Reset the bar pinned state (for use when quoteData is cleared externally). */
export function resetQuotePin() {
  barPinned.value = false
}

/** Restore bar visibility when transitioning from wide-screen to narrow. */
export function restoreBarVisibility() {
  if (quoteData.value && barPinned.value) {
    barVisible.value = true
  }
}

export function useQuoteQuestion() {
  const toast = useToast()
  const sessionIdentity = useSessionIdentity()

  onMounted(() => {
    listenerCount++
    if (listenerCount === 1) {
      document.addEventListener('selectionchange', onSelectionChange)
    }
  })

  onUnmounted(() => {
    listenerCount--
    if (listenerCount === 0) {
      document.removeEventListener('selectionchange', onSelectionChange)
    }
  })

  function closeSheet() {
    // Clear selection when closing
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
   * 编程式显示引用问答栏（供双击复制后调用，不依赖 selectionchange 事件）
   * 延迟 400ms 显示，避免双击的 pointerdown 事件触发"点击外部关闭"
   */
  function showBar(data: QuoteData) {
    setTimeout(() => {
      setQuoteData(data)
      barVisible.value = true
      if (isWideScreen.value) {
        barPinned.value = true
      }
    }, 400)
  }

  async function sendMessage(userMessage: string) {
    if (!quoteData.value || !userMessage.trim()) return

    const q = quoteData.value

    // Add the quoted file (with line info) as an attached file — unified channel.
    // addAttachedFile handles dedup: if file already attached without line info,
    // it upgrades the entry with startLine/endLine.
    if (q.filePath) {
      addAttachedFile(q.filePath, false, q.startLine, q.endLine)
    }

    // Embed quoted code text in the message body as context for the AI
    const message = buildQuoteMessage(userMessage, q.text, q.filePath, q.language, q.startLine, q.endLine)

    // Capture animation coordinates BEFORE any await — the bar's handleSend()
    // sets expanded=false synchronously right after emit('send'), so the
    // .qq-send-btn element will be removed from DOM on the next tick.
    const sendBtn = document.querySelector('.qq-send-btn')
    const dockChatBtn = document.querySelector('.dock-center')?.querySelector('.dock-btn')
    const animFrom = sendBtn?.getBoundingClientRect() ?? null
    const animTo = dockChatBtn?.getBoundingClientRect() ?? null

    // Clear quoteData BEFORE sending — ChatPanelContent.sendMessage also checks
    // quoteData and would double-embed the quote if it's still set.
    clearAll()
    barVisible.value = false
    barPinned.value = false

    // Delegate to session identity singleton — it routes to ChatPanel's
    // sendMessage if registered, otherwise falls back to a direct API call.
    try {
      await sessionIdentity.sendMessage(message)
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
    sendMessage,
  }
}
