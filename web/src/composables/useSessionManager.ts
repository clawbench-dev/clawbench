import { ref, watch, type Ref } from 'vue'
import { useSessionIdentity, runningSessions } from '@/composables/useSessionIdentity.ts'
import { cancelChat } from '@/utils/api'
import { useToast } from '@/composables/useToast.ts'
import { gt } from '@/composables/useLocale'

/**
 * Unified session manager — a thin coordination layer that ensures
 * consistent cleanup + queue sync around every session operation.
 *
 * All session switching paths (SessionDrawer @select, useSwipeSession,
 * identity proxy from App.vue/QuoteQuestionBar, ChatPanel handlers)
 * MUST go through this manager so that:
 *   1. cleanupActiveStream() is always called before switching
 *   2. pendingMessages is always synced on session change
 *   3. backend queue is cleared on session deletion
 *
 * This composable does NOT own useChatSession or useChatStream.
 * It receives their functions as options and wraps them.
 */

export interface UseSessionManagerOptions {
  // Core state refs (owned by ChatPanel)
  messages: Ref<any[]>
  loading: Ref<boolean>

  // Session operations (from useChatSession)
  switchSessionCore: (sessionId: string) => Promise<void>
  createSessionCore: (agentId?: string) => Promise<void>
  deleteSessionCore: (sessionId: string, backend?: string) => Promise<void>
  continueFromExecutionCore: (taskId: number, execId: number, switchTabFn: (tab: string) => void) => Promise<boolean>
  checkContinueSessionCore: (taskId: number, execId: number) => Promise<{ exists: boolean; sessionId: string }>

  // Stream operations (from useChatStream)
  disconnectStream: (calledFromCleanup?: boolean) => void
  stopPolling: () => void

  // Render callback
  updateRenderedContents: (forceFull?: boolean) => void

  // Input cleanup after enqueue (ChatPanel-specific)
  clearInputState: () => void

  // Scroll
  scrollBottom: (force?: boolean) => void
}

export function useSessionManager(options: UseSessionManagerOptions) {
  const {
    messages,
    loading,
    switchSessionCore,
    createSessionCore,
    deleteSessionCore,
    continueFromExecutionCore,
    checkContinueSessionCore,
    disconnectStream,
    stopPolling,
    updateRenderedContents,
    clearInputState,
    scrollBottom,
  } = options

  const identity = useSessionIdentity()
  const toast = useToast()

  // ── Pending message queue ──

  const pendingMessages = ref([])

  /** Fetch the current queue for a session from the backend. */
  async function fetchQueue(sessionId: string) {
    if (!sessionId) return
    try {
      const resp = await fetch(`/api/ai/queue?session_id=${encodeURIComponent(sessionId)}`)
      if (resp.ok) {
        const data = await resp.json()
        pendingMessages.value = data.queue || []
      }
    } catch (_) {
      // Non-critical — queue will be empty until next SSE queue_update
    }
  }

  /** Enqueue a message for later delivery while AI is generating. */
  async function enqueueMessage(text: string, extraFilePaths: string[] = [], attachedFiles: string[] = [], pendingFilePaths: string[] = []) {
    const inputText = text !== undefined ? text : ''
    const filePaths = [...(extraFilePaths || []), ...(attachedFiles.length > 0 ? attachedFiles : [])]
    const allFiles = [...(pendingFilePaths || []), ...filePaths]

    try {
      const resp = await fetch(
        `/api/ai/queue?session_id=${encodeURIComponent(identity.currentSessionId.value)}`,
        {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({
            message: inputText,
            filePaths,
            files: allFiles,
          }),
        }
      )
      const data = await resp.json()
      if (data.queue) {
        pendingMessages.value = data.queue
      }
    } catch (err) {
      toast.show(gt('session.queueFailed'), { icon: '⚠️', type: 'error' })
    }

    scrollBottom(true)
  }

  /** Remove a pending message from the queue by index. */
  async function handleRemovePending(index: number) {
    try {
      const resp = await fetch(
        `/api/ai/queue?session_id=${encodeURIComponent(identity.currentSessionId.value)}&index=${index}`,
        { method: 'DELETE' }
      )
      const data = await resp.json()
      pendingMessages.value = data.queue || []
    } catch (err) {
      toast.show(gt('session.removeFailed'), { icon: '⚠️', type: 'error' })
    }
  }

  /** Set pendingMessages from external source (e.g. SSE queue_update, sendMessageNow response). */
  function setPendingMessages(queue: any[]) {
    pendingMessages.value = queue
  }

  // ── Cleanup ──

  /** Clean up streaming state when user wants to interact with session management
   *  while AI is still generating. */
  function cleanupActiveStream() {
    if (!loading.value) return
    disconnectStream(true)
    stopPolling()
    const streamingMsg = messages.value.find(m => m.role === 'assistant' && m.streaming)
    if (streamingMsg) {
      delete streamingMsg.streaming
      if (streamingMsg.blocks) {
        for (const block of streamingMsg.blocks) {
          if (block.type === 'tool_use' && !block.done) block.done = true
        }
      }
    }
    updateRenderedContents(true)
    loading.value = false
  }

  // ── Unified session operations (cleanup + core + queue sync) ──

  async function switchSession(sessionId: string) {
    cleanupActiveStream()
    await switchSessionCore(sessionId)
    // pendingMessages is synced by the watch on currentSessionId below
  }

  async function createSession(agentId?: string) {
    cleanupActiveStream()
    pendingMessages.value = []
    await createSessionCore(agentId)
  }

  async function deleteSession(sessionId: string, backend?: string) {
    cleanupActiveStream()
    // Cancel running session before deleting to kill the CLI process
    if (runningSessions.value.has(sessionId)) {
      try { await cancelChat(sessionId) } catch (_) {}
    }
    // Clear backend queue for deleted session
    try {
      await fetch(`/api/ai/queue?session_id=${encodeURIComponent(sessionId)}`, { method: 'DELETE' })
    } catch (_) {}
    await deleteSessionCore(sessionId, backend)
  }

  /** Delete the current session (convenience for ChatInputBar button). */
  async function deleteCurrentSession(deleteDraft: (id: string) => void) {
    const deletedId = identity.currentSessionId.value
    if (!deletedId) return
    cleanupActiveStream()
    // Cancel running session before deleting to kill the CLI process
    if (runningSessions.value.has(deletedId)) {
      try { await cancelChat(deletedId) } catch (_) {}
    }
    try {
      await fetch(`/api/ai/queue?session_id=${encodeURIComponent(deletedId)}`, { method: 'DELETE' })
    } catch (_) {}
    pendingMessages.value = []
    await deleteSessionCore(deletedId, identity.currentBackend.value)
    deleteDraft(deletedId)
  }

  /** Continue a task execution as a new chat session. */
  async function continueFromExecution(taskId: number, execId: number, switchTabFn: (tab: string) => void): Promise<boolean> {
    cleanupActiveStream()
    return await continueFromExecutionCore(taskId, execId, switchTabFn)
  }

  /** Check whether a continued session already exists for a task execution. */
  async function checkContinueSession(taskId: number, execId: number): Promise<{ exists: boolean; sessionId: string }> {
    return await checkContinueSessionCore(taskId, execId)
  }

  // ── Queue sync on session change ──

  // When currentSessionId changes (from ANY path), fetch the queue
  watch(() => identity.currentSessionId.value, async (newSessionId) => {
    if (newSessionId) {
      await fetchQueue(newSessionId)
    } else {
      pendingMessages.value = []
    }
  })

  // When loading transitions from true → false while we still show pending messages,
  // the backend may have finished draining the queue while SSE was disconnected
  // (e.g. user left the page on mobile). Sync queue from backend to clear stale items.
  watch(loading, async (newVal, oldVal) => {
    if (oldVal && !newVal && pendingMessages.value.length > 0 && identity.currentSessionId.value) {
      await fetchQueue(identity.currentSessionId.value)
    }
  })

  // When the page becomes visible after being in the background (e.g. mobile screen
  // unlock), sync pendingMessages with the backend. SSE events (queue_consume,
  // queue_update, queue_done) are dropped while the page is hidden, so the local
  // pendingMessages may be stale — showing ghost "queuing" items that the backend
  // has already consumed. This is the definitive fix for the semi-transparent
  // pending overlay persisting after screen unlock.
  function handleVisibilityChange() {
    if (document.visibilityState === 'visible' && pendingMessages.value.length > 0 && identity.currentSessionId.value) {
      fetchQueue(identity.currentSessionId.value)
    }
  }
  document.addEventListener('visibilitychange', handleVisibilityChange)

  // ── Register identity actions ──

  /** Wire the identity singleton's proxy callbacks to our unified methods.
   *  Call this from ChatPanel's setup. */
  function registerIdentityActions(extra: {
    sendMessage: (text: string, filePaths?: string[]) => Promise<void>
    openChatPanel: () => void
  }) {
    identity.registerSessionActions({
      switchSession,
      createSession,
      deleteSession,
      sendMessage: extra.sendMessage,
      openChatPanel: extra.openChatPanel,
      continueFromExecution,
      checkContinueSession,
    })
  }

  return {
    // State
    pendingMessages,
    // Queue operations
    fetchQueue,
    enqueueMessage,
    handleRemovePending,
    setPendingMessages,
    // Unified session operations
    switchSession,
    createSession,
    deleteSession,
    deleteCurrentSession,
    continueFromExecution,
    checkContinueSession,
    // Cleanup (exposed for onStreamEnd and other edge cases)
    cleanupActiveStream,
    // Visibility change cleanup — call removeEventListener on unmount
    _visibilityHandler: handleVisibilityChange,
    // Identity registration
    registerIdentityActions,
  }
}
