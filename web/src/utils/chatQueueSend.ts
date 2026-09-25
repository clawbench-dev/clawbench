/**
 * Shared "enqueue while generating" orchestration.
 *
 * Both the normal input path (ChatPanelContent.sendMessage) and the
 * AskUserQuestion-card path (ChatPanelContent.handleToolSendMessage) enqueue a
 * user message while the AI is still generating. The backend handles the
 * "session not running" race internally (EnqueueAndMaybeStart's self-heal), so
 * no needs_start/resubmit round-trip is needed on the frontend.
 *
 * The optimistic entry goes into the QUEUE STORE, not the conversation
 * `messages` array: a queued message is not part of the conversation until it
 * is dequeued, and keeping it out is what removes every pending/queued special
 * case from the message list.
 *
 * Side effects are injected as callbacks so the orchestration stays pure and
 * unit-testable.
 */

import { dedupeFiles, type FileEntry } from '@/utils/fileAttachmentUtils'
import { trackInFlightSend, untrackInFlightSend } from '@/utils/chatStreamUtils'
import { addQueued, removeQueued } from '@/composables/useMessageQueue.ts'

/** Generate a unique queue ID for matching a queue entry to backend events. */
export function generateQueueId(): string {
  return `pending-${Date.now()}-${Math.random().toString(36).slice(2, 8)}`
}

export interface EnqueueAndMaybeStartOptions {
  sessionId: string
  text: string
  attachedFiles: FileEntry[]
  pendingFiles: FileEntry[]
  queueId?: string
  /** Notified after the optimistic queue entry is rendered. */
  onPendingRendered?: () => void
  enqueue: (
    sessionId: string,
    text: string,
    attachedFiles: FileEntry[],
    pendingFiles: FileEntry[],
    queueId: string,
  ) => Promise<boolean>
}

/**
 * Push an optimistic queue entry and enqueue it for delivery. Returns the
 * queueId. The backend persists the message and either starts an execution or
 * lets the running drain loop claim it.
 *
 * Throws when the enqueue call fails (rejects, or resolves with `false` — the
 * signal enqueueMessage returns when it swallowed a fetch error). Callers
 * should restore the input box on failure so the user's text isn't lost.
 */
export async function enqueueAndMaybeStart(opts: EnqueueAndMaybeStartOptions): Promise<string> {
  const queueId = opts.queueId || generateQueueId()
  const allFiles = dedupeFiles([...opts.pendingFiles, ...opts.attachedFiles])

  addQueued(opts.sessionId, {
    queueId,
    text: opts.text || '',
    files: allFiles,
    createdAt: new Date().toISOString(),
  })
  opts.onPendingRendered?.()

  // Guard this optimistic entry against a stale loadHistory snapshot that was
  // fetched before the enqueue POST committed its row. The queue store is
  // rebuilt wholesale from loadHistory's `queue` field, so without the guard a
  // snapshot taken before the commit would drop the entry. The registry clears
  // itself once a db_load's queue contains the entry (see useChatSession).
  trackInFlightSend(queueId)

  let ok: boolean
  try {
    ok = await opts.enqueue(opts.sessionId, opts.text, opts.attachedFiles, opts.pendingFiles, queueId)
  } catch (err) {
    // The POST never committed — release the guard and the entry so a later
    // rebuild is not left holding an entry no backend row will ever back.
    untrackInFlightSend(queueId)
    removeQueued(opts.sessionId, queueId)
    throw err
  }
  if (ok === false) {
    untrackInFlightSend(queueId)
    removeQueued(opts.sessionId, queueId)
    throw new Error('enqueue failed')
  }

  return queueId
}
