import { ref, onMounted, onUnmounted } from 'vue'
import { useSessionIdentity } from '@/composables/useSessionIdentity.ts'
import { useToast } from '@/composables/useToast.ts'
import { gt } from '@/composables/useLocale'
import { buildQuoteMessage } from '@/utils/doubleClickUtils.ts'
import { closestElement, getLineInfo, getFileInfo } from '@/utils/quoteQuestionUtils.ts'
import { useChatContext } from '@/composables/useChatContext.ts'
import type { QuoteData } from '@/composables/useChatContext.ts'

// Module-level singleton: bar visibility state shared across all consumers.
// quoteData is stored in useChatContext (global singleton) so ChatInputBar
// can render a quote chip in any tab.
const { quoteData, setQuoteData, addAttachedFile, hasAttachedFile, clearAll } = useChatContext()
const barVisible = ref(false)
const barPinned = ref(false)  // When pinned, selection loss won't auto-hide the bar
const sheetOpen = ref(false)

let debounceTimer: ReturnType<typeof setTimeout> | null = null

function onSelectionChange() {
  if (debounceTimer) clearTimeout(debounceTimer)
  debounceTimer = setTimeout(() => {
    const sel = window.getSelection()
    if (!sel || sel.isCollapsed || !sel.toString().trim()) {
      // When bar is pinned (user clicked "引用提问"), don't auto-hide on selection loss
      if (!barPinned.value) {
        barVisible.value = false
        setQuoteData(null)
      }
      return
    }

    // Check if selection is within a code or markdown preview area
    const container = closestElement(sel.anchorNode, '.raw-content-pre, .markdown-body')
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
  }, 150)
}

// Global listener management
let listenerCount = 0

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
    }, 400)
  }

  async function sendMessage(userMessage: string) {
    if (!quoteData.value || !userMessage.trim()) return

    const q = quoteData.value
    const message = buildQuoteMessage(userMessage, q.text, q.filePath, q.language, q.startLine, q.endLine)

    // Pass the quoted file as a file attachment so the backend builds
    // the [当前文件: ...] prompt prefix and sets the CLI work_dir.
    const filePaths = q.filePath ? [q.filePath] : []

    // Also add the file to the global attachedFiles so ChatInputBar shows a tag
    if (q.filePath && !hasAttachedFile(q.filePath)) {
      addAttachedFile(q.filePath)
    }

    // Capture animation coordinates BEFORE any await — the bar's handleSend()
    // sets expanded=false synchronously right after emit('send'), so the
    // .qq-send-btn element will be removed from DOM on the next tick.
    const sendBtn = document.querySelector('.qq-send-btn')
    const dockChatBtn = document.querySelector('.dock-center')?.querySelector('.dock-btn')
    const animFrom = sendBtn?.getBoundingClientRect() ?? null
    const animTo = dockChatBtn?.getBoundingClientRect() ?? null

    // Delegate to session identity singleton — it routes to ChatPanel's
    // sendMessage if registered, otherwise falls back to a direct API call.
    try {
      await sessionIdentity.sendMessage(message, filePaths)
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
    } catch (err) {
      toast.show(gt('quoteBar.sendFailed', { error: (err as Error).message }), { icon: '⚠️', type: 'error' })
    }

    // Clear all chat context (attachedFiles + quoteData) and close the bar.
    clearAll()
    barVisible.value = false
    barPinned.value = false
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
