import { ref, type Ref } from 'vue'

/**
 * Per-session pending message store.
 *
 * Pending messages are queued user messages that haven't been processed yet.
 * They are stored here — completely separate from messages.value which only
 * contains persisted (DB) messages. This isolation eliminates:
 *
 * 1. Cross-session contamination (each session's pending are independent)
 * 2. syncPendingFromBackend mutating messages.value
 * 3. loadHistory having to preserve/re-append pending messages
 * 4. SSE event routing confusion (events update pendingStore by sessionId)
 *
 * IMPORTANT: This is NOT a singleton — each call creates a new Map.
 * The canonical instance is created in ChatPanelContent.vue and
 * passed via options to useChatStream and useSessionManager.
 * Do NOT call usePendingStore() in other composables — always
 * receive the instance via options.
 */

export interface PendingMessage {
  role: 'user'
  content: string
  blocks: any[]
  files: { path: string }[]
  createdAt: string
  pending: true
}

export function usePendingStore() {
  const pendingStore: Ref<Map<string, PendingMessage[]>> = ref(new Map())

  /** Get pending messages for a session. Returns empty array if none. */
  function getPending(sessionId: string): PendingMessage[] {
    return pendingStore.value.get(sessionId) || []
  }

  /** Add a pending message to a session's queue. */
  function addPending(sessionId: string, msg: PendingMessage) {
    const current = pendingStore.value.get(sessionId) || []
    current.push(msg)
    pendingStore.value.set(sessionId, current)
  }

  /** Remove a pending message from a session's queue by content text. */
  function removePending(sessionId: string, text: string) {
    const current = pendingStore.value.get(sessionId)
    if (!current) return
    const idx = current.findIndex(m => m.content === text)
    if (idx !== -1) {
      current.splice(idx, 1)
      pendingStore.value.set(sessionId, current)
    }
  }

  /** Remove a pending message at a specific index. */
  function removePendingAt(sessionId: string, index: number) {
    const current = pendingStore.value.get(sessionId)
    if (!current || index < 0 || index >= current.length) return
    current.splice(index, 1)
    pendingStore.value.set(sessionId, current)
  }

  /**
   * Sync pending messages for a session from the backend queue state.
   * This is the authoritative sync — backend queue is the source of truth.
   *
   * - Adds pending messages that are in backendQueue but not locally
   * - Removes pending messages that are no longer in backendQueue
   */
  function syncFromBackendQueue(sessionId: string, backendQueue: any[]) {
    const current = pendingStore.value.get(sessionId) || []

    // Add pending messages from backend that aren't local
    for (const item of backendQueue) {
      const text = item.text || ''
      const exists = current.some(m => m.content === text)
      if (!exists) {
        current.push(createPendingMessage(text, [
          ...(item.files || []),
          ...(item.filePaths || []),
        ]))
      }
    }

    // Remove pending messages not in backend queue
    for (let i = current.length - 1; i >= 0; i--) {
      const inBackend = backendQueue.some((item: any) => (item.text || '') === current[i].content)
      if (!inBackend) {
        current.splice(i, 1)
      }
    }

    pendingStore.value.set(sessionId, current)
  }

  /** Clear all pending messages for a session. */
  function clearPending(sessionId: string) {
    pendingStore.value.delete(sessionId)
  }

  /** Clear all pending messages for all sessions. */
  function clearAllPending() {
    pendingStore.value.clear()
  }

  /** Check if a session has any pending messages. */
  function hasPending(sessionId: string): boolean {
    const msgs = pendingStore.value.get(sessionId)
    return !!msgs && msgs.length > 0
  }

  return {
    pendingStore,
    getPending,
    addPending,
    removePending,
    removePendingAt,
    syncFromBackendQueue,
    clearPending,
    clearAllPending,
    hasPending,
  }
}

/** Factory: create a pending user message object. */
export function createPendingMessage(text: string, files: string[] = []): PendingMessage {
  return {
    role: 'user',
    content: text || '',
    blocks: text ? [{ type: 'text', text }] : [],
    files: files.map(p => ({ path: p })),
    createdAt: new Date().toISOString(),
    pending: true,
  }
}
