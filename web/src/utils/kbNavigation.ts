/**
 * "Open this document in the knowledge base" — a one-shot hand-off between a
 * chat citation and the KB panel.
 *
 * The citation lives deep in the chat tree; the panel lives in a lazily mounted
 * dock tab. Passing a prop down that chain would mean every intermediate
 * component knowing about the knowledge base, so the request is parked in a
 * module-level ref instead: the citation sets it and switches the tab, the
 * panel consumes it once and clears it.
 */
import { ref } from 'vue'

export interface KBPendingOpen {
  /** Identifies this request so a later one is not consumed by an older open. */
  token: number
  documentId: number
  /** 0 when the message predates chunk ids (or the caller has none). */
  chunkId: number
}

let seq = 0

export const kbPendingOpen = ref<KBPendingOpen | null>(null)

export function requestOpenKBDocument(documentId: number, chunkId = 0): void {
  if (!documentId) return
  kbPendingOpen.value = { token: ++seq, documentId, chunkId: chunkId > 0 ? chunkId : 0 }
}

export function consumeKBPendingOpen(token: number): void {
  if (kbPendingOpen.value?.token === token) kbPendingOpen.value = null
}
