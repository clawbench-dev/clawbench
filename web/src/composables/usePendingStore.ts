/**
 * Stub for usePendingStore — removed from production code.
 * This file exists solely so that test files that import it can still resolve.
 * Do NOT use in production code.
 */
import { ref } from 'vue'

export function usePendingStore() {
  return {
    getPending: () => [],
    addPending: () => {},
    removePending: () => {},
    removePendingAt: () => {},
    syncFromBackendQueue: () => {},
    clearPending: () => {},
    clearAllPending: () => {},
    hasPending: () => false,
    pendingStore: ref(new Map()),
  }
}

export function createPendingMessage(text: string = '', _files: string[] = []) {
  return {
    role: 'user' as const,
    content: text,
    blocks: text ? [{ type: 'text', text }] : [],
    files: [] as { path: string }[],
    createdAt: new Date().toISOString(),
    pending: true as const,
  }
}
