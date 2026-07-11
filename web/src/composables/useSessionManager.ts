import { watch, type Ref } from 'vue'
import { useSessionIdentity, runningSessions } from '@/composables/useSessionIdentity.ts'
import { cancelChat } from '@/utils/api'
import { useToast } from '@/composables/useToast.ts'
import { gt } from '@/composables/useLocale'
import { appLog } from '@/utils/appLog'
import { findLastIndexCompat } from '@/utils/chatStreamUtils.ts'

const TAG = 'SessionManager'

/**
 * Unified session manager — a thin coordination layer that ensures
 * consistent cleanup + queue sync around every session operation.
 *
 * All session switching paths (SessionDrawer @select, useSwipeSession,
 * identity proxy from App.vue/QuoteQuestionBar, ChatPanel handlers)
 * MUST go through this manager so that:
 *   1. cleanupActiveStream() is always called before switching
 *   2. pending messages in messages.value are cleaned up on session change
 *   3. backend queue is cleared on session deletion
 *
 * Pending messages live in messages.value with pending: true flag.
 * No separate pendingStore — one source of truth.
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

  // ── Pending message helpers ──
  // Pending messages are in messages.value with pending: true.

  /** Remove all pending messages from messages.value */
  function clearPendingMessages() {
    for (let i = messages.value.length - 1; i >= 0; i--) {
      if (messages.value[i].pending) messages.value.splice(i, 1)
    }
  }

  /** Sync pending messages from backend queue into messages.value.
   *  Removes stale local pending messages and adds missing ones from backend. */
  function syncPendingFromBackendQueue(backendQueue: any[]) {
    // Remove all existing pending messages
    clearPendingMessages()
    // Push backend queue items as pending messages
    for (const item of backendQueue) {
      const itemFiles = [...(item.files || []), ...(item.filePaths || [])]
      messages.value.push({
        role: 'user',
        id: `queue-${Date.now()}-${Math.random().toString(36).slice(2, 8)}`,
        content: item.text || '',
        blocks: item.text ? [{ type: 'text', text: item.text }] : [],
        files: itemFiles.map((p: string) => ({ path: p })),
        createdAt: item.createdAt || new Date().toISOString(),
        pending: true,
      })
    }
  }

  /** Fetch the current queue for a session from the backend and sync pending messages. */
  async function fetchQueue(sessionId: string) {
    if (!sessionId) return
    try {
      const resp = await fetch(`/api/ai/queue?session_id=${encodeURIComponent(sessionId)}`)
      if (resp.ok) {
        const data = await resp.json()
        syncPendingFromBackendQueue(data.queue || [])
      }
    } catch (_) {
      // Non-critical — queue will be empty until next SSE event
    }
  }

  /** Enqueue a message for later delivery while AI is generating.
   *  Returns the enqueue result which may contain `needs_start` if the
   *  session is no longer running (race condition: user enqueues right
   *  as AI finishes). The caller should resubmit via sendMessageNow.
   *
   *  IMPORTANT: sessionId MUST be captured by the caller BEFORE any async
   *  boundary. */
  async function enqueueMessage(sessionId: string, text: string, extraFilePaths: string[] = [], attachedFiles: string[] = [], pendingFilePaths: string[] = []): Promise<{ needsStart: boolean; message?: string; filePaths?: string[]; files?: string[] }> {
    const inputText = text !== undefined ? text : ''
    const filePaths = [...(extraFilePaths || []), ...(attachedFiles.length > 0 ? attachedFiles : [])]
    const allFiles = [...(pendingFilePaths || []), ...filePaths]

    appLog.d(TAG, `[enqueueMessage] targetSid=${sessionId.slice(0,8)} currentSid=${identity.currentSessionId.value.slice(0,8)} text="${inputText.slice(0,40)}" same=${sessionId === identity.currentSessionId.value}`)

    try {
      const resp = await fetch(
        `/api/ai/queue?session_id=${encodeURIComponent(sessionId)}`,
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
        // Remove the pending message from messages.value
        const idx = findLastIndexCompat(
          messages.value,
          (m: any) => m.role === 'user' && m.pending && m.content === (data.message || inputText)
        )
        if (idx !== -1) messages.value.splice(idx, 1)
        scrollBottom(true)
        return {
          needsStart: true,
          message: data.message || inputText,
          filePaths: data.filePaths || filePaths,
          files: data.files || allFiles,
        }
      }

      // Don't full-sync the queue here. The optimistically pushed pending
      // message is already in messages.value with correct order. Full sync
      // (clear all + re-push from backend) causes races when two enqueueMessage
      // calls overlap: the first sync clears the second's optimistic message.
      // The queue will be synced by fetchQueue (session switch),
      // handleVisibilityChange (mobile unlock), watch(loading), and SSE events.
    } catch (err) {
      toast.show(gt('session.queueFailed'), { icon: '⚠️', type: 'error' })
      // On enqueue failure, remove the pending message we just added
      const idx = findLastIndexCompat(
        messages.value,
        (m: any) => m.role === 'user' && m.pending && m.content === inputText
      )
      if (idx !== -1) messages.value.splice(idx, 1)
    }

    scrollBottom(true)
    return { needsStart: false }
  }

  /** Remove a pending message by its index in the pending list for the current session.
   *  The index is computed by the caller BEFORE the optimistic splice, so it's
   *  valid against the pre-splice messages array. We must NOT re-validate against
   *  the current messages.value (which has already been spliced) — that would
   *  reject the index as out-of-range and silently skip the backend DELETE. */
  async function handleRemovePending(pendingIndex: number) {
    if (pendingIndex < 0) return  // Negative index is always invalid
    const sessionId = identity.currentSessionId.value

    try {
      const resp = await fetch(
        `/api/ai/queue?session_id=${encodeURIComponent(sessionId)}&index=${pendingIndex}`,
        { method: 'DELETE' }
      )
      const data = await resp.json()
      // Sync remaining pending messages with backend queue
      syncPendingFromBackendQueue(data.queue || [])
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
    _clearInputState()
    // Clear pending messages BEFORE switching session. These belong to the
    // current (old) session — they're in the backend queue for that session
    // and will be drained by its AI goroutine. If we don't clear them here,
    // they remain in messages.value during the session switch, which causes:
    //   1. The watch(loading) handler fires after currentSessionId changes,
    //      sees pending messages, and fetches the NEW session's queue (wrong!)
    //   2. The watch(currentSessionId) handler fetches the new session's
    //      queue, but syncPendingFromBackendQueue clears all pending and
    //      re-pushes from the new session's queue (which may be empty),
    //      making old session's pending messages vanish from UI.
    //   3. Stuck-queue recovery in watch(loading) may call sendMessageNow
    //      against the wrong session.
    // The old session's pending messages are safely in its backend queue;
    // they'll be drained when its AI finishes or shown if user switches back.
    clearPendingMessages()
    await switchSessionCore(sessionId)
    // pending messages are synced by the watch on currentSessionId below
  }

  async function createSession(agentId?: string) {
    cleanupActiveStream()
    _clearInputState()
    clearPendingMessages()
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
    clearPendingMessages()
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
    clearPendingMessages()
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
    _clearInputState()
    clearPendingMessages()
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
  // IMPORTANT: We must guard against the session having changed between the
  // loading transition and this async callback. If the user switched sessions,
  // identity.currentSessionId.value points to the NEW session, but the pending
  // messages belong to the OLD session. We must NOT call sendMessageNow against
  // the wrong session.
  watch(loading, async (newVal, oldVal) => {
    if (oldVal && !newVal && messages.value.some((m: any) => m.pending) && identity.currentSessionId.value) {
      const sessionId = identity.currentSessionId.value
      try {
        const resp = await fetch(`/api/ai/queue?session_id=${encodeURIComponent(sessionId)}`)
        if (resp.ok) {
          const data = await resp.json()
          const queue = data.queue || []
          // Guard: if session changed while we were fetching, don't sync —
          // the pending messages belong to the old session and will be
          // handled by its own fetchQueue or queue_drain events.
          if (sessionId !== identity.currentSessionId.value) return
          syncPendingFromBackendQueue(queue)
          // Stuck-queue recovery: if backend queue still has items after
          // loading went false, the drain loop missed them. Dequeue and
          // resubmit the first one. Only do this if the session hasn't
          // changed — sendMessageNow reads identity.currentSessionId.value
          // which must match the queue's session.
          if (queue.length > 0 && !loading.value && sessionId === identity.currentSessionId.value) {
            const firstItem = queue[0]
            // Remove the pending message locally — sendMessageNow will push its own
            const idx = messages.value.findIndex(
              (m: any) => m.pending && m.content === (firstItem.text || '')
            )
            if (idx !== -1) messages.value.splice(idx, 1)
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
        // Non-critical — queue will be empty until next SSE event
      }
    }
  })

  // When the page becomes visible after being in the background (e.g. mobile screen
  // unlock), sync pending messages with the backend. SSE events (queue_drain,
  // queue_queued) are dropped while the page is hidden, so local
  // pending messages may be stale — showing ghost "queuing" items that the backend
  // has already consumed.
  function handleVisibilityChange() {
    if (document.visibilityState === 'visible' && messages.value.some((m: any) => m.pending) && identity.currentSessionId.value) {
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
