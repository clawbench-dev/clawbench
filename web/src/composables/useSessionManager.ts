import { ref, watch, type Ref } from 'vue'
import { useSessionIdentity, runningSessions } from '@/composables/useSessionIdentity.ts'
import { cancelChat } from '@/utils/api'
import { useToast } from '@/composables/useToast.ts'
import { gt } from '@/composables/useLocale'
import { appLog } from '@/utils/appLog'
import type { FileEntry } from '@/utils/fileAttachmentUtils'

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
 */

export interface UseSessionManagerOptions {
  // Core state refs (owned by ChatPanel)
  messages: Ref<Record<string, unknown>[]>
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

  // Render callback
  updateRenderedContents: (forceFull?: boolean) => void

  // Input cleanup after enqueue (ChatPanel-specific)
  clearInputState: () => void

  // Scroll
  scrollBottom: (force?: boolean) => void

  // History reload (from useChatSession)
  reloadHistory: () => Promise<void>
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
    updateRenderedContents,
    clearInputState: _clearInputState,
    scrollBottom,
    reloadHistory,
  } = options

  const identity = useSessionIdentity()
  const toast = useToast()

  // ── Queue sync guard ──
  // When switchSession/createSession is driving the session change,
  // it calls fetchQueue AFTER messages.value is populated. The watch on
  // currentSessionId should only fire for external changes (e.g. initial
  // mount) where no explicit fetchQueue call is made.
  const switchingSession = ref(false)

  // ── Pending message helpers ──
  // Pending messages are in messages.value with pending: true.

  /** Remove all pending messages from messages.value */
  function clearPendingMessages() {
    for (let i = messages.value.length - 1; i >= 0; i--) {
      if (messages.value[i].pending) messages.value.splice(i, 1)
    }
  }

  /** Sync pending messages from backend queue into messages.value.
   *  Used only on session switch to show queued messages for the new session. */
  function syncPendingFromBackendQueue(backendQueue: Array<Record<string, unknown>>) {
    // Remove all existing pending messages
    clearPendingMessages()
    // Push backend queue items as pending messages
    for (const item of backendQueue) {
      const itemFiles = [...(item.files as FileEntry[] || []).map(f => typeof f === 'string' ? { path: f, isDir: false } : f), ...(item.filePaths as string[] || []).map((p: string) => ({ path: p, isDir: false }))]
      messages.value.push({
        role: 'user',
        id: item.queueId || `queue-${Date.now()}-${Math.random().toString(36).slice(2, 8)}`,
        content: item.text || '',
        blocks: item.text ? [{ type: 'text', text: item.text }] : [],
        files: itemFiles,
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
    } catch {
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
  async function enqueueMessage(sessionId: string, text: string, extraFilePaths: string[] = [], attachedFiles: FileEntry[] = [], pendingFilePaths: string[] = [], queueId?: string): Promise<{ needsStart: boolean; queueId?: string; message?: string; filePaths?: string[]; files?: FileEntry[] }> {
    const inputText = text !== undefined ? text : ''
    const filePaths = [...(extraFilePaths || []), ...(attachedFiles.length > 0 ? attachedFiles.map(f => f.path) : [])]
    const allFileEntries: FileEntry[] = [
      ...(pendingFilePaths || []).map(p => ({ path: p, isDir: false })),
      ...(attachedFiles.length > 0 ? attachedFiles : []),
    ]

    appLog.d(TAG, `[enqueueMessage] sid=${sessionId.slice(0,8)} queueId=${queueId || 'none'} text="${inputText.slice(0,40)}"`)

    try {
      const resp = await fetch(
        `/api/ai/queue?session_id=${encodeURIComponent(sessionId)}`,
        {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({
            message: inputText,
            queueId,
            filePaths,
            files: allFileEntries,
          }),
        }
      )
      const data = await resp.json()

      // Race condition fix: backend detected session is not running and
      // dequeued the message. The frontend must resubmit as a new chat.
      if (data.needs_start) {
        // Remove the pending message from messages.value by queueId
        if (queueId) {
          const idx = messages.value.findIndex((m) => m.id === queueId && m.pending)
          if (idx !== -1) messages.value.splice(idx, 1)
        } else {
          // Fallback for callers without queueId
          const idx = messages.value.findLastIndex(
            (m) => m.role === 'user' && m.pending && m.content === (data.message || inputText)
          )
          if (idx !== -1) messages.value.splice(idx, 1)
        }
        scrollBottom(true)
        return {
          needsStart: true,
          queueId,
          message: data.message || inputText,
          filePaths: data.filePaths || filePaths,
          files: data.files || allFileEntries,
        }
      }
    } catch {
      toast.show(gt('session.queueFailed'), { icon: '⚠️', type: 'error' })
      // On enqueue failure, remove the pending message we just added
      if (queueId) {
        const idx = messages.value.findIndex((m) => m.id === queueId && m.pending)
        if (idx !== -1) messages.value.splice(idx, 1)
      } else {
        const idx = messages.value.findLastIndex(
          (m) => m.role === 'user' && m.pending && m.content === inputText
        )
        if (idx !== -1) messages.value.splice(idx, 1)
      }
    }

    scrollBottom(true)
    return { needsStart: false }
  }

  /** Remove a pending message by its queueId.
   *  Sends DELETE to backend with queueId parameter, then removes from messages.value. */
  async function handleRemovePending(queueId: string) {
    if (!queueId) return
    const sessionId = identity.currentSessionId.value

    try {
      await fetch(
        `/api/ai/queue?session_id=${encodeURIComponent(sessionId)}&queueId=${encodeURIComponent(queueId)}`,
        { method: 'DELETE' }
      )
      // Remove from local messages
      const idx = messages.value.findIndex((m) => m.id === queueId && m.pending)
      if (idx !== -1) messages.value.splice(idx, 1)
    } catch {
      toast.show(gt('session.removeFailed'), { icon: '⚠️', type: 'error' })
    }
  }

  // ── Cleanup ──

  /** Clean up streaming state when user wants to interact with session management
   *  while AI is still generating. */
  function cleanupActiveStream() {
    if (!loading.value) return
    disconnectStream(true)
    const sm = messages.value.find(m => m.role === 'assistant' && m.streaming)
    if (sm) {
      delete sm.streaming
      if (sm.blocks) {
        for (const block of sm.blocks as Array<Record<string, unknown>>) {
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
    // Suppress the watch on currentSessionId — it would fire when
    // clearSessionIdentity() sets the ID (before messages.value is
    // populated from REST), causing syncPendingFromBackendQueue to push
    // pending messages into the stale array which then gets replaced
    // wholesale by parseMessages(). We fetch the queue ourselves after
    // switchSessionCore completes so it runs against the final messages.
    switchingSession.value = true
    try {
      await switchSessionCore(sessionId)
      // Now messages.value is populated from the REST response.
      // Fetch the queue to restore any pending messages for this session.
      await fetchQueue(sessionId)
    } finally {
      switchingSession.value = false
    }
  }

  async function createSession(agentId?: string) {
    cleanupActiveStream()
    _clearInputState()
    clearPendingMessages()
    switchingSession.value = true
    try {
      await createSessionCore(agentId)
      // New sessions have no queued messages, but fetch for consistency
      const sid = identity.currentSessionId.value
      if (sid) await fetchQueue(sid)
    } finally {
      switchingSession.value = false
    }
  }

  async function deleteSession(sessionId: string, backend?: string) {
    cleanupActiveStream()
    // Cancel running session before deleting to kill the CLI process
    if (runningSessions.value.has(sessionId)) {
      try { await cancelChat(sessionId) } catch {}
    }
    // Clear backend queue for deleted session
    try {
      await fetch(`/api/ai/queue?session_id=${encodeURIComponent(sessionId)}`, { method: 'DELETE' })
    } catch {}
    clearPendingMessages()
    // deleteSessionCore may internally call switchSession (if deleting the
    // current session), which changes currentSessionId. Suppress the watch
    // to avoid premature fetchQueue, then fetch queue after completion.
    switchingSession.value = true
    try {
      await deleteSessionCore(sessionId, backend)
      const sid = identity.currentSessionId.value
      if (sid) await fetchQueue(sid)
    } finally {
      switchingSession.value = false
    }
  }

  /** Delete the current session (convenience for ChatInputBar button). */
  async function deleteCurrentSession(deleteDraft: (id: string) => void) {
    const deletedId = identity.currentSessionId.value
    if (!deletedId) return
    cleanupActiveStream()
    // Cancel running session before deleting to kill the CLI process
    if (runningSessions.value.has(deletedId)) {
      try { await cancelChat(deletedId) } catch {}
    }
    try {
      await fetch(`/api/ai/queue?session_id=${encodeURIComponent(deletedId)}`, { method: 'DELETE' })
    } catch {}
    clearPendingMessages()
    // deleteSessionCore may internally call switchSession, which changes
    // currentSessionId. Suppress the watch.
    switchingSession.value = true
    try {
      await deleteSessionCore(deletedId, identity.currentBackend.value)
      const sid = identity.currentSessionId.value
      if (sid) await fetchQueue(sid)
    } finally {
      switchingSession.value = false
    }
    deleteDraft(deletedId)
  }

  /** Continue a task execution as a new chat session. */
  async function continueFromExecution(taskId: number, execId: number, switchTabFn: (tab: string) => void): Promise<boolean> {
    cleanupActiveStream()
    // continueFromExecutionCore may internally call switchSession, which
    // changes currentSessionId. Suppress the watch.
    switchingSession.value = true
    try {
      const result = await continueFromExecutionCore(taskId, execId, switchTabFn)
      const sid = identity.currentSessionId.value
      if (sid) await fetchQueue(sid)
      return result
    } finally {
      switchingSession.value = false
    }
  }

  /** Fork the current session — create a new session with copied messages. */
  async function forkSession(sessionId: string, beforeMessageId?: number): Promise<boolean> {
    cleanupActiveStream()
    _clearInputState()
    clearPendingMessages()
    // forkSessionCore may internally call switchSession, which changes
    // currentSessionId. Suppress the watch.
    switchingSession.value = true
    try {
      const result = await forkSessionCore(sessionId, beforeMessageId)
      const sid = identity.currentSessionId.value
      if (sid) await fetchQueue(sid)
      return result
    } finally {
      switchingSession.value = false
    }
  }

  /** Check whether a continued session already exists for a task execution. */
  async function checkContinueSession(taskId: number, execId: number): Promise<{ exists: boolean; sessionId: string }> {
    return await checkContinueSessionCore(taskId, execId)
  }

  // ── Queue sync on session change ──

  // When currentSessionId changes from an EXTERNAL path (not from our own
  // switchSession/createSession), fetch the queue.
  // immediate: true ensures fetchQueue runs on initial mount too —
  // critical because App.vue's initSessionFromAPI() may set currentSessionId
  // before useSessionManager is created, so the watch wouldn't fire without immediate.
  watch(() => identity.currentSessionId.value, async (newSessionId) => {
    if (newSessionId && !switchingSession.value) {
      await fetchQueue(newSessionId)
    }
  }, { immediate: true })

  // When the page becomes visible after being in the background (e.g. mobile screen
  // unlock), sync queue with the backend. While backgrounded, queue_drain /
  // queue_cancel events are dropped because the WS is disconnected, so local
  // pending messages may be stale — showing ghost "queuing" items that the backend
  // has already consumed. Frontend pending messages are optimistic indicators only;
  // the backend queue is the source of truth.
  //
  // Same pattern as switchSession: loadHistory first (brings in drained messages
  // from DB as regular messages), then fetchQueue (restores still-queued pending
  // messages). This ensures drained messages reappear and pending messages stay.
  async function handleVisibilityChange() {
    if (document.visibilityState !== 'visible') return
    const sessionId = identity.currentSessionId.value
    if (!sessionId) return
    try {
      await reloadHistory()
    } catch {
      // Non-critical
    }
    await fetchQueue(sessionId)
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
