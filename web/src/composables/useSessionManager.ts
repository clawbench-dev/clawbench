import { type Ref } from 'vue'
import { useSessionIdentity, runningSessions } from '@/composables/useSessionIdentity.ts'
import { cancelChat } from '@/utils/api'
import { useToast } from '@/composables/useToast.ts'
import { gt } from '@/composables/useLocale'
import { appLog } from '@/utils/appLog'
import type { FileEntry } from '@/utils/fileAttachmentUtils'
import type { ChatMessageAction } from '@/utils/chatStreamUtils.ts'

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
  /** Single write channel for the messages array (chatMessageReducer). */
  dispatch: (action: ChatMessageAction) => void
  loading: Ref<boolean>

  // Session operations (from useChatSession)
  switchSessionCore: (sessionId: string) => Promise<void>
  createSessionCore: (agentId?: string) => Promise<void>
  archiveSessionCore: (sessionId: string, backend?: string) => Promise<void>
  destroySessionCore: (sessionId: string) => Promise<void>
  continueFromExecutionCore: (taskId: number, execId: number, switchTabFn: (tab: string) => void) => Promise<boolean>
  forkSessionCore: (sessionId: string, beforeMessageId?: number, agentId?: string) => Promise<boolean>
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
    dispatch,
    loading,
    switchSessionCore,
    createSessionCore,
    archiveSessionCore,
    destroySessionCore,
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
    dispatch({ type: 'clear_pending' })
  }

  /** Enqueue a message for later delivery while AI is generating.
   *  The backend persists the message (queued=1) and either starts an execution
   *  or lets the running drain loop pick it up (B2 self-heal handles the
   *  session-ended race), so no needs_start resubmit is needed.
   *
   *  IMPORTANT: sessionId MUST be captured by the caller BEFORE any async
   *  boundary. */
  async function enqueueMessage(sessionId: string, text: string, attachedFiles: FileEntry[] = [], pendingFilePaths: string[] = [], queueId?: string): Promise<void> {
    const inputText = text !== undefined ? text : ''
    const filePaths = attachedFiles.map(f => f.path)
    const allFileEntries: FileEntry[] = [
      ...(pendingFilePaths || []).map(p => ({ path: p, isDir: false })),
      ...attachedFiles,
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
            // Required so the backend's user_message broadcast carries
            // senderClientId and this device can skip its own echo — without
            // it the queued message is rendered twice (pending bubble + remote
            // duplicate).
            clientId: localStorage.getItem('clawbench_client_id') || undefined,
          }),
        }
      )
      if (!resp.ok) {
        throw new Error(`enqueue failed: ${resp.status}`)
      }
    } catch {
      toast.show(gt('session.queueFailed'), { icon: '⚠️', type: 'error' })
      // On enqueue failure, remove the pending message we just added.
      if (queueId) {
        dispatch({ type: 'remove_pending', queueId })
      } else {
        // Rare path (no queueId) — content-match rollback via the reducer
        // (single write channel: never splice messages directly).
        dispatch({ type: 'optimistic_remove_content', content: inputText })
      }
    }

    scrollBottom(true)
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
      // Remove from local messages via the reducer.
      dispatch({ type: 'remove_pending', queueId })
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
    // No clearPendingMessages here — loadHistory's parseMessages + queueAppend
    // replaces the entire messages array, so old pending messages are naturally
    // removed. Explicit clearPendingMessages would erase pending messages before
    // loadHistory can restore them from the backend queue field.
    await switchSessionCore(sessionId)
  }

  async function createSession(agentId?: string) {
    cleanupActiveStream()
    _clearInputState()
    clearPendingMessages()
    await createSessionCore(agentId)
  }

  async function archiveSession(sessionId: string, backend?: string) {
    cleanupActiveStream()
    // Cancel running session before archiving to kill the CLI process
    if (runningSessions.value.has(sessionId)) {
      try { await cancelChat(sessionId) } catch {}
    }
    // Clear backend queue for archived session
    try {
      await fetch(`/api/ai/queue?session_id=${encodeURIComponent(sessionId)}`, { method: 'DELETE' })
    } catch {}
    clearPendingMessages()
    await archiveSessionCore(sessionId, backend)
  }

  /** Archive the current session (convenience for ChatInputBar button). */
  async function archiveCurrentSession(deleteDraft: (id: string) => void) {
    const archivedId = identity.currentSessionId.value
    if (!archivedId) return
    cleanupActiveStream()
    // Cancel running session before archiving to kill the CLI process
    if (runningSessions.value.has(archivedId)) {
      try { await cancelChat(archivedId) } catch {}
    }
    try {
      await fetch(`/api/ai/queue?session_id=${encodeURIComponent(archivedId)}`, { method: 'DELETE' })
    } catch {}
    clearPendingMessages()
    await archiveSessionCore(archivedId, identity.currentBackend.value)
    deleteDraft(archivedId)
  }

  /** Hard-delete (physically destroy) a specific session — irreversible. */
  async function destroySession(sessionId: string) {
    cleanupActiveStream()
    if (runningSessions.value.has(sessionId)) {
      try { await cancelChat(sessionId) } catch {}
    }
    try {
      await fetch(`/api/ai/queue?session_id=${encodeURIComponent(sessionId)}`, { method: 'DELETE' })
    } catch {}
    clearPendingMessages()
    await destroySessionCore(sessionId)
  }

  /** Hard-delete (physically destroy) the current session — irreversible. */
  async function destroyCurrentSession(deleteDraft: (id: string) => void) {
    const destroyedId = identity.currentSessionId.value
    if (!destroyedId) return
    cleanupActiveStream()
    if (runningSessions.value.has(destroyedId)) {
      try { await cancelChat(destroyedId) } catch {}
    }
    try {
      await fetch(`/api/ai/queue?session_id=${encodeURIComponent(destroyedId)}`, { method: 'DELETE' })
    } catch {}
    clearPendingMessages()
    await destroySessionCore(destroyedId)
    deleteDraft(destroyedId)
  }

  /** Continue a task execution as a new chat session. */
  async function continueFromExecution(taskId: number, execId: number, switchTabFn: (tab: string) => void): Promise<boolean> {
    cleanupActiveStream()
    return await continueFromExecutionCore(taskId, execId, switchTabFn)
  }

  /** Fork the current session — create a new session with copied messages. */
  async function forkSession(sessionId: string, beforeMessageId?: number, agentId?: string): Promise<boolean> {
    cleanupActiveStream()
    _clearInputState()
    clearPendingMessages()
    return await forkSessionCore(sessionId, beforeMessageId, agentId)
  }

  /** Check whether a continued session already exists for a task execution. */
  async function checkContinueSession(taskId: number, execId: number): Promise<{ exists: boolean; sessionId: string }> {
    return await checkContinueSessionCore(taskId, execId)
  }

  // ── Register identity actions ──

  /** Wire the identity singleton's proxy callbacks to our unified methods.
   *  Call this from ChatPanel's setup. */
  function registerIdentityActions(extra: {
    sendMessage: (text: string) => Promise<void>
    openChatPanel: () => void
  }) {
    identity.registerSessionActions({
      switchSession,
      createSession,
      archiveSession,
      destroySession,
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
    archiveSession,
    archiveCurrentSession,
    destroySession,
    destroyCurrentSession,
    continueFromExecution,
    forkSession,
    checkContinueSession,
    // Cleanup
    cleanupActiveStream,
    // Identity registration
    registerIdentityActions,
  }
}
