/**
 * Shared "enqueue while generating, resubmit on needs_start" orchestration.
 *
 * Both the normal input path (ChatPanelContent.sendMessage) and the
 * AskUserQuestion-card path (ChatPanelContent.handleToolSendMessage) enqueue a
 * user message while the AI is still generating. This helper centralizes that
 * logic so both paths get identical handling of the needs_start race: when the
 * backend dequeues the message because the session was no longer running, the
 * message is resubmitted as a fresh chat instead of being silently lost (which
 * would leave no assistant placeholder and no loading indicator).
 *
 * Side effects (pushing the message, rendering, enqueueing, resubmitting) are
 * injected as callbacks so the orchestration is pure and unit-testable.
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

/** Result of the backend enqueue call (mirrors POST /api/ai/queue). */
export interface EnqueueResult {
  needsStart: boolean
  message?: string
  filePaths?: string[]
  files?: FileEntry[]
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
  ) => Promise<EnqueueResult>
  resubmit: (text: string, filePaths: string[], files: FileEntry[]) => Promise<void>
}

/**
 * Push a pending user message, enqueue it for delivery, and — if the backend
 * returned needs_start — resubmit it as a fresh chat so the reply is not lost.
 * Returns the queueId of the pushed pending message.
 */
export async function enqueueAndMaybeStart(opts: EnqueueAndMaybeStartOptions): Promise<string> {
  const queueId = opts.queueId || generateQueueId()
  const allFiles = dedupeFiles([...opts.pendingFiles, ...opts.attachedFiles])
  const filePaths = opts.attachedFiles.map(f => f.path)

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

  const result = await opts.enqueue(opts.sessionId, opts.text, opts.attachedFiles, opts.pendingFiles, queueId)
  if (result.needsStart) {
    await opts.resubmit(result.message || opts.text, result.filePaths || filePaths, result.files || allFiles)
  }

  return queueId
}
