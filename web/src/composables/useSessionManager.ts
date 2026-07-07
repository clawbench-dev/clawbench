import { watch, type Ref } from 'vue'
import { useSessionIdentity, runningSessions } from '@/composables/useSessionIdentity.ts'
import { cancelChat } from '@/utils/api'
import { useToast } from '@/composables/useToast.ts'
import { gt } from '@/composables/useLocale'
import { usePendingStore } from '@/composables/usePendingStore.ts'

/**
 * Unified session manager — a thin coordination layer that ensures
 * consistent cleanup + queue sync around every session operation.
 *
 * All session switching paths (SessionDrawer @select, useSwipeSession,
 * identity proxy from App.vue/QuoteQuestionBar, ChatPanel handlers)
 * MUST go through this manager so that:
 *   1. cleanupActiveStream() is always called before switching
 *   2. pending messages are synced via pendingStore on session change
 *   3. backend queue is cleared on session deletion
 *
 * This composable does NOT own useChatSession or useChatStream.
 * It receives their functions as options and wraps them.
 *
 * Pending messages live in a per-session pendingStore (usePendingStore),
 * completely separate from messages.value which only contains persisted
 * (DB) messages.
 */

export interface UseSessionManagerOptions {
  // Core state refs (owned by ChatPanel)
  messages: Ref<any[]>
  loading: Ref<boolean>
  pendingStore: ReturnType<typeof usePendingStore>

  // Session operations (from useChatSession)
  switchSessionCore: (sessionId: string) => Promise<void>
  createSessionCore: (agentId?: string) => Promise<void>
  deleteSessionCore: (sessionId: string, backend?: string) => Promise<void>
  continueFromExecutionCore: (taskId: number, execId: number, switchTabFn: (tab: string) => void) => Promise<boolean>
  forkSessionCore: (sessionId: string, beforeMessageId?: number) => Promise<boolean>
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

  // Resend a queued message as a new chat (for stuck-queue recovery)
  sendMessageNow: (text: string, filePaths: string[], files: string[]) => Promise<void>
}

export function useSessionManager(options: UseSessionManagerOptions) {
  const {
    messages,
    loading,
    pendingStore,
    switchSessionCore,
    createSessionCore,
    deleteSessionCore,
    continueFromExecutionCore,
    forkSessionCore,
    checkContinueSessionCore,
    disconnectStream,
    stopPolling,
    updateRenderedContents,
    clearInputState: _clearInputState,
    scrollBottom,
    sendMessageNow,
  } = options

  const identity = useSessionIdentity()
  const toast = useToast()

  // ── Pending message queue ──
  // Pending messages live in pendingStore (per-session), separate from messages.value.

  /** Fetch the current queue for a session from the backend and sync to pendingStore. */
  async function fetchQueue(sessionId: string) {
    if (!sessionId) return
    try {
      const resp = await fetch(`/api/ai/queue?session_id=${encodeURIComponent(sessionId)}`)
      if (resp.ok) {
        const data = await resp.json()
        pendingStore.syncFromBackendQueue(sessionId, data.queue || [])
      }
    } catch (_) {
      // Non-critical — queue will be empty until next SSE queue_update
    }
  }

  /** Enqueue a message for later delivery while AI is generating.
   *  Returns the enqueue result which may contain `needs_start` if the
   *  session is no longer running (race condition: user enqueues right
   *  as AI finishes). The caller should resubmit via sendMessageNow. */
  async function enqueueMessage(text: string, extraFilePaths: string[] = [], attachedFiles: string[] = [], pendingFilePaths: string[] = []): Promise<{ needsStart: boolean; message?: string; filePaths?: string[]; files?: string[] }> {
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

      // Race condition fix: backend detected session is not running and
      // dequeued the message. The frontend must resubmit as a new chat.
      if (data.needs_start) {
        // Remove the pending message from pendingStore — it will be
        // sent as a normal (non-pending) message via sendMessageNow.
        pendingStore.removePending(identity.currentSessionId.value, data.message || inputText)
        pendingStore.syncFromBackendQueue(identity.currentSessionId.value, data.queue || [])
        scrollBottom(true)
        return {
          needsStart: true,
          message: data.message || inputText,
          filePaths: data.filePaths || filePaths,
          files: data.files || allFiles,
        }
      }

      // Sync pending messages in pendingStore with backend queue state
      pendingStore.syncFromBackendQueue(identity.currentSessionId.value, data.queue || [])
    } catch (err) {
      toast.show(gt('session.queueFailed'), { icon: '⚠️', type: 'error' })
      // On enqueue failure, remove the pending message we just added
      pendingStore.removePending(identity.currentSessionId.value, inputText)
    }

    scrollBottom(true)
    return { needsStart: false }
  }

  /** Remove a pending message by its index in the pending list for the current session. */
  async function handleRemovePending(pendingIndex: number) {
    const sessionId = identity.currentSessionId.value
    const pending = pendingStore.getPending(sessionId)
    if (pendingIndex < 0 || pendingIndex >= pending.length) return

    try {
      const resp = await fetch(
        `/api/ai/queue?session_id=${encodeURIComponent(sessionId)}&index=${pendingIndex}`,
        { method: 'DELETE' }
      )
      const data = await resp.json()
      // Sync remaining pending messages with backend queue
      pendingStore.syncFromBackendQueue(sessionId, data.queue || [])
    } catch (err) {
      toast.show(gt('session.removeFailed'), { icon: '⚠️', type: 'error' })
    }
  }

  // ── Cleanup ──

  /** Clean up streaming state when user wants to interact with session management
   *  while AI is still generating. */
  function cleanupActiveStream() {
    if (!loading.value) return
    disconnectStream(true)
    stopPolling()
    const sm = messages.value.find(m => m.role === 'assistant' && m.streaming)
    if (sm) {
      delete sm.streaming
      if (sm.blocks) {
        for (const block of sm.blocks) {
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
    // pending messages are synced by the watch on currentSessionId below
  }

  async function createSession(agentId?: string) {
    cleanupActiveStream()
    pendingStore.clearPending(identity.currentSessionId.value)
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
    pendingStore.clearPending(sessionId)
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
    pendingStore.clearPending(deletedId)
    await deleteSessionCore(deletedId, identity.currentBackend.value)
    deleteDraft(deletedId)
  }

  /** Continue a task execution as a new chat session. */
  async function continueFromExecution(taskId: number, execId: number, switchTabFn: (tab: string) => void): Promise<boolean> {
    cleanupActiveStream()
    return await continueFromExecutionCore(taskId, execId, switchTabFn)
  }

  /** Fork the current session — create a new session with copied messages. */
  async function forkSession(sessionId: string, beforeMessageId?: number): Promise<boolean> {
    cleanupActiveStream()
    pendingStore.clearPending(identity.currentSessionId.value)
    return await forkSessionCore(sessionId, beforeMessageId)
  }

  /** Check whether a continued session already exists for a task execution. */
  async function checkContinueSession(taskId: number, execId: number): Promise<{ exists: boolean; sessionId: string }> {
    return await checkContinueSessionCore(taskId, execId)
  }

  // ── Queue sync on session change ──

  // When currentSessionId changes (from ANY path), fetch the queue.
  // immediate: true ensures fetchQueue runs on initial mount too —
  // critical because App.vue's initSessionFromAPI() may set currentSessionId
  // before useSessionManager is created, so the watch wouldn't fire without immediate.
  watch(() => identity.currentSessionId.value, async (newSessionId) => {
    if (newSessionId) {
      await fetchQueue(newSessionId)
    }
  }, { immediate: true })

  // When loading transitions from true → false while we still have pending messages,
  // the backend may have finished draining the queue while SSE was disconnected
  // (e.g. user left the page on mobile). Sync queue from backend to clear stale items.
  // If the backend still has queued items (stuck-queue race: message enqueued after
  // the drain loop exited), auto-resubmit the first one.
  watch(loading, async (newVal, oldVal) => {
    if (oldVal && !newVal && pendingStore.hasPending(identity.currentSessionId.value) && identity.currentSessionId.value) {
      const sessionId = identity.currentSessionId.value
      try {
        const resp = await fetch(`/api/ai/queue?session_id=${encodeURIComponent(sessionId)}`)
        if (resp.ok) {
          const data = await resp.json()
          const queue = data.queue || []
          pendingStore.syncFromBackendQueue(sessionId, queue)
          // Stuck-queue recovery: if backend queue still has items after
          // loading went false, the drain loop missed them. Dequeue and
          // resubmit the first one.
          if (queue.length > 0 && !loading.value) {
            // Remove the pending message locally — sendMessageNow will push its own
            const firstItem = queue[0]
            pendingStore.removePending(sessionId, firstItem.text || '')
            // Dequeue from backend
            try {
              await fetch(`/api/ai/queue?session_id=${encodeURIComponent(sessionId)}&index=0`, { method: 'DELETE' })
            } catch (_) {}
            // Resubmit as new chat
            await sendMessageNow(
              firstItem.text || '',
              firstItem.filePaths || [],
              firstItem.files || []
            )
          }
        }
      } catch (_) {
        // Non-critical — queue will be empty until next SSE queue_update
      }
    }
  })

  // When the page becomes visible after being in the background (e.g. mobile screen
  // unlock), sync pending messages with the backend. SSE events (queue_drain,
  // queue_update) are dropped while the page is hidden, so local
  // pending messages may be stale — showing ghost "queuing" items that the backend
  // has already consumed.
  function handleVisibilityChange() {
    if (document.visibilityState === 'visible' && pendingStore.hasPending(identity.currentSessionId.value) && identity.currentSessionId.value) {
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
      forkSession,
      checkContinueSession,
    })
  }

  return {
    // Queue operations
    fetchQueue,
    enqueueMessage,
    handleRemovePending,
    // Unified session operations
    switchSession,
    createSession,
    deleteSession,
    deleteCurrentSession,
    continueFromExecution,
    forkSession,
    checkContinueSession,
    // Cleanup (exposed for onStreamEnd and other edge cases)
    cleanupActiveStream,
    // Visibility change cleanup — call removeEventListener on unmount
    _visibilityHandler: handleVisibilityChange,
    // Identity registration
    registerIdentityActions,
  }
}
