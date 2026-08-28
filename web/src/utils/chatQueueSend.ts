/**
 * Shared "enqueue while generating" orchestration.
 *
 * Both the normal input path (ChatPanelContent.sendMessage) and the
 * AskUserQuestion-card path (ChatPanelContent.handleToolSendMessage) enqueue a
 * user message while the AI is still generating. The backend handles the
 * "session not running" race internally (EnqueueAndMaybeStart's B2 self-heal),
 * so no needs_start/resubmit round-trip is needed on the frontend.
 *
 * Side effects (pushing the message, rendering, enqueueing) are injected as
 * callbacks so the orchestration is pure and unit-testable.
 */

import { dedupeFiles, type FileEntry } from '@/utils/fileAttachmentUtils'
import { nextClientSeq } from '@/utils/chatStreamUtils'

/** Generate a unique queue ID for matching pending messages to queue_drain events. */
export function generateQueueId(): string {
  return `pending-${Date.now()}-${Math.random().toString(36).slice(2, 8)}`
}

/** A pending user message optimistically pushed to the messages list. */
export interface PendingUserMessage {
  role: 'user'
  id: string
  content: string
  blocks: Array<{ type: 'text'; text: string }>
  files: FileEntry[]
  createdAt: string
  pending: true
  /** Client-side monotonic sequence so this pending message sorts correctly
   *  among the other transient (string-id) messages. Without it the message
   *  sorts at TRANSIENT_BASE + 0, jumping above every earlier message. */
  seq?: number
}

export interface EnqueueAndMaybeStartOptions {
  sessionId: string
  text: string
  attachedFiles: FileEntry[]
  pendingFiles: FileEntry[]
  queueId?: string
  pushMessage: (msg: PendingUserMessage) => void
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
 * Push a pending user message and enqueue it for delivery. Returns the queueId
 * of the pushed pending message. The backend persists the message (queued=1)
 * and either starts an execution or lets the running drain loop pick it up.
 *
 * Throws when the enqueue call fails (rejects, or resolves with `false` — the
 * signal enqueueMessage returns when it swallowed a fetch error). Callers
 * should restore the input box on failure so the user's text isn't lost.
 */
export async function enqueueAndMaybeStart(opts: EnqueueAndMaybeStartOptions): Promise<string> {
  const queueId = opts.queueId || generateQueueId()
  const allFiles = dedupeFiles([...opts.pendingFiles, ...opts.attachedFiles])

  opts.pushMessage({
    role: 'user',
    id: queueId,
    content: opts.text || '',
    blocks: opts.text ? [{ type: 'text', text: opts.text }] : [],
    files: allFiles,
    createdAt: new Date().toISOString(),
    pending: true,
    seq: nextClientSeq(),
  })
  opts.onPendingRendered?.()

  const ok = await opts.enqueue(opts.sessionId, opts.text, opts.attachedFiles, opts.pendingFiles, queueId)
  if (ok === false) {
    throw new Error('enqueue failed')
  }

  return queueId
}
