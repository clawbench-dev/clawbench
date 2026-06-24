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
 *
 * All mutation methods create new array references (immutable updates)
 * to guarantee Vue reactivity triggers on every Map.set() call.
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

  /** Add a pending message to a session's queue. Creates a new array to ensure Vue reactivity. */
  function addPending(sessionId: string, msg: PendingMessage) {
    const current = pendingStore.value.get(sessionId) || []
    pendingStore.value.set(sessionId, [...current, msg])
  }

  /** Remove a pending message from a session's queue by content text. Creates a new array. */
  function removePending(sessionId: string, text: string) {
    const current = pendingStore.value.get(sessionId)
    if (!current) return
    const idx = current.findIndex(m => m.content === text)
    if (idx !== -1) {
      pendingStore.value.set(sessionId, current.filter((_, i) => i !== idx))
    }
  }

  /** Remove a pending message at a specific index. Creates a new array. */
  function removePendingAt(sessionId: string, index: number) {
    const current = pendingStore.value.get(sessionId)
    if (!current || index < 0 || index >= current.length) return
    pendingStore.value.set(sessionId, current.filter((_, i) => i !== index))
  }

  /**
   * Sync pending messages for a session from the backend queue state.
   * This is the authoritative sync — backend queue is the source of truth.
   *
   * - Adds pending messages that are in backendQueue but not locally
   * - Removes pending messages that are no longer in backendQueue
   *
   * Always creates a new array to guarantee Vue reactivity triggers.
   */
  function syncFromBackendQueue(sessionId: string, backendQueue: any[]) {
    const current = pendingStore.value.get(sessionId) || []

    // Keep existing messages that are still in backend queue
    const kept = current.filter(m =>
      backendQueue.some((item: any) => (item.text || '') === m.content)
    )

    // Add backend messages not already in local pending list
    const toAdd = backendQueue
      .filter((item: any) => {
        const text = item.text || ''
        return !current.some(m => m.content === text)
      })
      .map((item: any) => createPendingMessage(item.text || '', [
        ...(item.files || []),
        ...(item.filePaths || []),
      ]))

    // Create new array — guarantees Vue detects the change
    pendingStore.value.set(sessionId, [...kept, ...toAdd])
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
