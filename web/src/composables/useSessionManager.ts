import { type Ref } from 'vue'
import { useSessionIdentity, runningSessions } from '@/composables/useSessionIdentity.ts'
import { cancelChat } from '@/utils/api'
import { useToast } from '@/composables/useToast.ts'
import { gt } from '@/composables/useLocale'
import { appLog } from '@/utils/appLog'
import type { FileEntry } from '@/utils/fileAttachmentUtils'

const TAG = 'SessionManager'

/**
 * Unified session manager — ensures consistent cleanup around session operations.
 *
 * Queue sync is now handled by loadHistory: the backend includes the in-memory
 * queue in the /api/ai/chat GET response, and loadHistory appends queue items
 * as pending messages after replacing messages.value. This eliminates the race
 * where loadHistory replaces messages and erases pending messages.
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
  } = options

  const identity = useSessionIdentity()
  const toast = useToast()

  // ── Pending message helpers ──

  /** Remove all pending messages from messages.value */
  function clearPendingMessages() {
    for (let i = messages.value.length - 1; i >= 0; i--) {
      if (messages.value[i].pending) messages.value.splice(i, 1)
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

  // ── Unified session operations (cleanup + core) ──

  async function switchSession(sessionId: string) {
    cleanupActiveStream()
    _clearInputState()
    // Clear pending messages BEFORE switching session — they belong to the
    // old session. loadHistory will restore pending messages for the new
    // session from the queue field in the /api/ai/chat response.
    clearPendingMessages()
    await switchSessionCore(sessionId)
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
      try { await cancelChat(sessionId) } catch {}
    }
    // Clear backend queue for deleted session
    try {
      await fetch(`/api/ai/queue?session_id=${encodeURIComponent(sessionId)}`, { method: 'DELETE' })
    } catch {}
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
      try { await cancelChat(deletedId) } catch {}
    }
    try {
      await fetch(`/api/ai/queue?session_id=${encodeURIComponent(deletedId)}`, { method: 'DELETE' })
    } catch {}
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
    // Cleanup
    cleanupActiveStream,
    // Identity registration
    registerIdentityActions,
  }
}
