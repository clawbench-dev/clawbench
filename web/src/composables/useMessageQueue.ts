/**
 * Per-session queued-message store.
 *
 * Queued messages are held OUTSIDE the conversation `messages` array: the
 * backend keeps them in their own table and only writes them into chat_history
 * when they are dequeued (or injected mid-turn). The queue panel is therefore
 * the only place they appear, and the message list needs no pending/queued
 * special cases.
 *
 * State is module-level and keyed by session id, so it survives the component
 * tree being torn down (project hot-switch, drawer close) and is shared by the
 * panel, the input bar and the WS event handler without prop drilling.
 */

import { ref, computed, type Ref } from 'vue'
import { dedupeFiles, type FileEntry } from '@/utils/fileAttachmentUtils'
import { isInFlightSend, untrackInFlightSend } from '@/utils/chatStreamUtils'

/** A message waiting in a session's queue. */
export interface QueuedMessage {
  /** Client-generated stable identity (the backend echoes it on queue_added). */
  queueId: string
  text: string
  files: FileEntry[]
  createdAt: string
}

/** Wire shape of GET /api/ai/queue and the `queue` field of GET /api/ai/chat. */
export interface QueuedMessageWire {
  queueId: string
  text?: string
  files?: FileEntry[]
  filePaths?: string[]
  createdAt?: string
}

// ── Module-level state ──

/** sessionId → ordered queue. Replaced wholesale on syncFromHistory. */
const queues = new Map<string, QueuedMessage[]>()

/**
 * Bumped on every mutation. The reactive panel reads `queueVersion` and derives
 * from the Map, so a mutation that does not replace the array still re-renders.
 */
const queueVersion = ref(0)

/** Which session the panel currently shows. Set by ChatPanelContent. */
const activeSessionId = ref('')

/**
 * Monotonic counter identifying a queue-snapshot REQUEST. `loadHistory` takes a
 * value with `beginQueueSnapshot()` when it starts fetching and hands it to
 * `syncFromHistory`, which uses it to tell a snapshot that predates an enqueue
 * POST's commit (stale — must NOT drop the optimistic entry) from one that
 * postdates it (authoritative — the entry's row is genuinely gone).
 */
let snapshotGeneration = 0

/** Claim the generation for a snapshot request about to be fetched. */
export function beginQueueSnapshot(): number {
  return ++snapshotGeneration
}

/**
 * queueId → the snapshot generation in effect when its enqueue POST resolved.
 *
 * A snapshot whose REQUEST started at a generation strictly greater than this
 * was issued after the row was committed, so its silence about the entry is
 * authoritative: the row was deleted (drained or cancelled) and this client
 * missed the event. Absent means the POST has not resolved yet, so the entry
 * must be preserved against ANY snapshot.
 */
const committedSends = new Map<string, number>()

/**
 * Record that an enqueue POST has resolved (its row is committed). Called by
 * `enqueueAndMaybeStart` after the POST succeeds — NOT at track time, because a
 * snapshot fetched before the commit legitimately lacks the row.
 */
export function markSendCommitted(queueId: string): void {
  if (queueId) committedSends.set(queueId, snapshotGeneration)
}

/**
 * Whether a snapshot can prove a guarded entry's row is gone.
 *
 * Only when the snapshot was requested strictly after the POST resolved: an
 * earlier request may have been served from a read that began before the commit
 * (the race the in-flight guard exists for), so it cannot prove anything.
 */
function snapshotProvesAbsent(queueId: string, snapshotGenerationValue?: number): boolean {
  if (snapshotGenerationValue === undefined) return false
  const committed = committedSends.get(queueId)
  if (committed === undefined) return false
  return snapshotGenerationValue > committed
}

function bump() {
  queueVersion.value += 1
}

function normalizeFiles(wire: QueuedMessageWire): FileEntry[] {
  const files: FileEntry[] = []
  for (const f of wire.files || []) {
    files.push(f)
  }
  for (const p of wire.filePaths || []) {
    if (!files.some((f) => f.path === p)) {
      files.push({ path: p, isDir: false })
    }
  }
  return dedupeFiles(files)
}

function fromWire(wire: QueuedMessageWire): QueuedMessage {
  return {
    queueId: wire.queueId,
    text: wire.text || '',
    files: normalizeFiles(wire),
    createdAt: wire.createdAt || new Date().toISOString(),
  }
}

// ── Public API ──

/** The queue for the currently active session. */
export const queuedMessages = computed<QueuedMessage[]>(() => {
  void queueVersion.value // reactive dependency
  return queues.get(activeSessionId.value) || []
})

/** Number of queued messages for the active session. */
export const queuedCount = computed(() => queuedMessages.value.length)

/** Set the session the panel reads from. */
export function setActiveQueueSession(sessionId: string): void {
  if (activeSessionId.value === sessionId) return
  activeSessionId.value = sessionId
  bump()
}

/** Read a session's queue (for handlers that act on a non-active session). */
export function getQueue(sessionId: string): QueuedMessage[] {
  return queues.get(sessionId) || []
}

/**
 * Replace a session's queue from the authoritative backend snapshot
 * (loadHistory's `queue` field or GET /api/ai/queue). This is the single
 * source of truth: WS events are incremental deltas on top of it.
 *
 * An optimistic entry whose enqueue POST is still awaiting acknowledgement
 * (trackInFlightSend) is preserved when absent from the snapshot: the snapshot
 * may have been fetched before the POST committed its row. Its entry is cleared
 * once the snapshot contains it.
 *
 * `snapshotGen` is the generation claimed by `beginQueueSnapshot()` when the
 * request was ISSUED. It lets this function tell the two reasons an entry can be
 * absent from a snapshot apart:
 *
 *  - The request was issued before the enqueue POST resolved → the server may
 *    simply not have the row yet. Keep the optimistic entry (the original race
 *    this guard exists for).
 *  - The request was issued AFTER the POST resolved → the row was committed, so
 *    a snapshot that lacks it is authoritative: it was drained, injected or
 *    cancelled, and this client missed the event (typically because it was
 *    backgrounded and the WS dropped it). Drop the entry.
 *
 * Without that distinction the entry was preserved forever: the guard is only
 * ever released by a snapshot that CONTAINS the entry, and a drained row is
 * deleted server-side, so no later snapshot can ever contain it. The stale card
 * then survived even an explicit foreground reopen (whose authoritative empty
 * queue was overridden by the guard) — the reported "queued message card stays
 * after it has finished".
 */
export function syncFromHistory(
  sessionId: string,
  wire: QueuedMessageWire[] | undefined,
  snapshotGen?: number,
): void {
  if (!sessionId) return
  const incoming = (wire || []).map(fromWire)
  const incomingIds = new Set(incoming.map((m) => m.queueId))

  // The snapshot contains an in-flight entry → the POST is fully acked.
  for (const id of incomingIds) {
    untrackInFlightSend(id)
    committedSends.delete(id)
  }

  // Keep optimistic entries the snapshot cannot yet disprove.
  const existing = queues.get(sessionId) || []
  for (const m of existing) {
    if (incomingIds.has(m.queueId)) continue
    if (snapshotProvesAbsent(m.queueId, snapshotGen)) {
      // Authoritative absence: the row is gone. Release the guard so the entry
      // cannot be resurrected by an even later stale rebuild either.
      untrackInFlightSend(m.queueId)
      committedSends.delete(m.queueId)
      continue
    }
    if (isInFlightSend(m.queueId)) {
      incoming.push(m)
      incomingIds.add(m.queueId)
    }
  }

  if (incoming.length === 0) {
    if (queues.delete(sessionId)) bump()
    return
  }
  queues.set(sessionId, incoming)
  bump()
}

/** Add a queued message (optimistic push or queue_added from another device). */
export function addQueued(sessionId: string, wire: QueuedMessageWire): void {
  if (!sessionId || !wire.queueId) return
  const list = queues.get(sessionId) || []
  const idx = list.findIndex((m) => m.queueId === wire.queueId)
  const next = fromWire(wire)
  // Always publish a NEW array. Mutating `list` in place and calling bump()
  // does NOT re-render dependents: `queuedMessages` is a computed that returns
  // the stored array, and Vue skips notifying when the value it produces is
  // the same reference it produced before. The first add happens to work only
  // because `queues.get()` returned undefined and the `|| []` fallback created
  // a fresh array — so the reference changed. Every LATER add reused that same
  // array, so the collapsed panel's count froze at 1 until something else
  // (expanding the panel) forced a re-render.
  //
  // removeQueued/removeQueuedMany already publish new arrays (filter), which is
  // why only the add path was broken.
  queues.set(sessionId, idx === -1 ? [...list, next] : list.map((m, i) => (i === idx ? next : m)))
  bump()
}

/** Remove one queued message by queueId (cancel, drain or inject). */
export function removeQueued(sessionId: string, queueId: string): void {
  if (!sessionId || !queueId) return
  const list = queues.get(sessionId)
  if (!list) return
  const next = list.filter((m) => m.queueId !== queueId)
  if (next.length === list.length) return
  // The entry is gone, so its in-flight guard (set by enqueueAndMaybeStart)
  // must be released — it would otherwise leak for the process lifetime.
  untrackInFlightSend(queueId)
  committedSends.delete(queueId)
  if (next.length === 0) {
    queues.delete(sessionId)
  } else {
    queues.set(sessionId, next)
  }
  bump()
}

/** Remove several queued messages at once (queue_cancel). */
export function removeQueuedMany(sessionId: string, queueIds: string[]): void {
  if (!sessionId || !queueIds || queueIds.length === 0) return
  const list = queues.get(sessionId)
  if (!list) return
  const drop = new Set(queueIds)
  const next = list.filter((m) => !drop.has(m.queueId))
  if (next.length === list.length) return
  for (const id of queueIds) {
    untrackInFlightSend(id)
    committedSends.delete(id)
  }
  if (next.length === 0) {
    queues.delete(sessionId)
  } else {
    queues.set(sessionId, next)
  }
  bump()
}

/** Drop a session's whole queue (session cleared / cancelled). */
export function clearQueue(sessionId: string): void {
  if (!sessionId) return
  const list = queues.get(sessionId)
  if (list) {
    for (const m of list) {
      untrackInFlightSend(m.queueId)
      committedSends.delete(m.queueId)
    }
  }
  if (queues.delete(sessionId)) bump()
}

/** Test hook: reset all state. */
export function resetQueuesForTest(): void {
  queues.clear()
  committedSends.clear()
  snapshotGeneration = 0
  activeSessionId.value = ''
  queueVersion.value = 0
}

/** Reactive version, for consumers that need to react to any mutation. */
export const queueStateVersion: Ref<number> = queueVersion

export function useMessageQueue() {
  return {
    queuedMessages,
    queuedCount,
    setActiveQueueSession,
    getQueue,
    beginQueueSnapshot,
    markSendCommitted,
    syncFromHistory,
    addQueued,
    removeQueued,
    removeQueuedMany,
    clearQueue,
  }
}
