/**
 * Pure functions and constants extracted from useChatStream composable.
 * These have no Vue reactivity dependencies and can be tested in isolation.
 *
 * Pending messages are stored in the messages array with pending: true flag.
 * No separate pendingStore — one source of truth.
 */

// ── Core chat types ──

import type { FileEntry } from '@/utils/fileAttachmentUtils'

/** A content block within a chat message (text, thinking, tool_use, error, warning). */
export interface ContentBlock {
  type: string
  text?: string
  name?: string
  id?: string
  done?: boolean
  status?: string
  input?: Record<string, unknown>
  output?: string
  summary?: string
  display_name?: string
  file_path?: string
  duration_ms?: number
  _key?: string
  reason?: string
  [key: string]: unknown
}

/** A chat message in the messages array. */
export interface ChatMessage {
  role: 'user' | 'assistant' | 'system'
  id: string | number
  content: string
  blocks?: ContentBlock[]
  metadata?: Record<string, unknown>
  cancelled?: boolean
  streaming?: boolean
  pending?: boolean
  backend?: string
  createdAt?: string
  files?: FileEntry[]
  [key: string]: unknown
}

/** SSE event data for content events */
export interface ContentEventData {
  content?: string
}

/** SSE event data for thinking events */
export interface ThinkingEventData {
  text?: string
}

/** SSE event data for tool_use/tool_result events */
export interface ToolUseEventData {
  id?: string
  name?: string
  input?: Record<string, unknown>
  done?: boolean
  status?: string
  summary?: string
  display_name?: string
  file_path?: string
  duration_ms?: number
}

/** SSE event data for mode/config/thinking_effort events */
export interface SseJsonData {
  [key: string]: unknown
}

/** Chat stream event data via WebSocket */
export interface ChatStreamEventData {
  session_id: string
  event_type: string
  payload: Record<string, unknown>
}

/** Polling response data */
export interface PollResponseData {
  messages?: ChatMessage[]
  running?: boolean
  sessionId?: string
}

/** Queue event data */
export interface QueueEventData {
  queueId?: string
  text?: string
  sessionId?: string
  filePaths?: string[]
  files?: FileEntry[]
  messageId?: number
}

/** Error event data */
export interface ErrorEventData {
  reason?: string
  error?: string
}

/**
 * Detect garbage output values that come from intermediate ACP ToolCallUpdate
 * events (e.g., a lone "}" from partial JSON streaming). Real tool output
 * from completed tools is always meaningful — at least a few words long.
 */
function isGarbageOutput(output: string | undefined): boolean {
  if (!output) return false
  const trimmed = output.trim()
  // Single character or just braces/brackets — not meaningful output
  if (trimmed.length <= 1) return true
  // Very short strings that are just JSON delimiters
  if (/^[{}[\],:]+$/.test(trimmed)) return true
  return false
}

/**
 * Tool names that modify files on disk (canonical PascalCase, guaranteed by backend normalization).
 * Used to trigger file preview refresh after tool completion.
 */
export const FILE_MODIFYING_TOOLS = new Set(['Write', 'Edit'])

/**
 * Structured summary card metadata. Mirrors the backend model.SummaryCards.
 * createdFiles/modifiedFiles restore the file-changes banner in summary-only
 * view, where full content blocks are omitted and cannot be traversed.
 */
export interface SummaryCards {
  tools?: Array<Record<string, unknown>>
  taskIDs?: number[]
  askQuestions?: Array<Record<string, unknown>>
  createdFiles?: string[]
  modifiedFiles?: string[]
}

/**
 * Extract file changes (created/modified) from tool_use blocks.
 * Write → created, Edit → modified. Deduplicates by file path.
 * Only considers blocks where done=true.
 * When blocks are absent (summary-only view), falls back to the file-change
 * lists carried in summaryCards.
 */
export function extractFileChanges(blocks: ContentBlock[], summaryCards?: SummaryCards | null): { created: string[]; modified: string[] } {
  const createdSet = new Set<string>()
  const modifiedSet = new Set<string>()
  for (const block of blocks) {
    if (block.type !== 'tool_use' || !block.done) continue
    const filePath = (block.file_path || (block.input as Record<string, unknown>)?.file_path) as string | undefined
    if (!filePath) continue
    if (block.name === 'Write') {
      createdSet.add(filePath)
    } else if (block.name === 'Edit') {
      modifiedSet.add(filePath)
    }
  }
  if (summaryCards) {
    for (const p of summaryCards.createdFiles || []) createdSet.add(p)
    for (const p of summaryCards.modifiedFiles || []) modifiedSet.add(p)
  }
  return { created: [...createdSet], modified: [...modifiedSet] }
}

/**
 * Find the most recent block of a given type by searching backward.
 * tool_use blocks act as natural boundaries — text/thinking after a tool_use
 * should not be merged with text/thinking before it.
 */
export function findLastBlockOfType(blocks: ContentBlock[], type: string): ContentBlock | undefined {
  for (let i = blocks.length - 1; i >= 0; i--) {
    if (blocks[i].type === type) return blocks[i]
    // tool_use blocks are natural boundaries — don't merge across them
    if (blocks[i].type === 'tool_use') return undefined
  }
  return undefined
}

/**
 * Clean up streaming state for the current assistant message.
 * Marks all unfinished tool_use blocks as done, removes streaming flag.
 * Returns the streaming message if found (for caller to do further processing).
 */
export function forceCleanupStreamingState(
  messages: ChatMessage[],
  callbacks: {
    onRenderNeeded: (forceFull?: boolean) => void
    onExtractScheduledTasks?: (msgs: ChatMessage[]) => void
  }
): ChatMessage | undefined {
  const streamingMsg = messages.find((m) => m.role === 'assistant' && m.streaming)
  if (streamingMsg) {
    const hasContent = streamingMsg.content || (streamingMsg.blocks && streamingMsg.blocks.length > 0)
    delete streamingMsg.streaming
    // Mark all unfinished tool_use blocks as done so spinner stops.
    // Exception: PermissionApproval blocks require user interaction —
    // marking them done without a real result makes the card appear
    // "Approved" when it's actually stuck (no user response received).
    if (streamingMsg.blocks) {
      for (const block of streamingMsg.blocks) {
        if (block.type === 'tool_use' && !block.done && block.name !== 'PermissionApproval') {
          block.done = true
          // Clear garbage output that may have been set by intermediate
          // ACP ToolCallUpdate events (e.g., a lone "}" from partial JSON).
          // Real output arrives via tool_result events which set done=true.
          if (isGarbageOutput(block.output)) {
            block.output = ''
          }
        }
      }
    }
    // Extract scheduled tasks from the just-finished message
    // (this path doesn't go through loadHistory, so we must call it explicitly)
    callbacks.onExtractScheduledTasks?.(messages)

    // If the streaming message received no content at all (e.g. network lost
    // before any SSE event arrived), remove it entirely so the user doesn't
    // see an empty AI reply bubble.
    if (!hasContent) {
      const idx = messages.indexOf(streamingMsg)
      if (idx !== -1) messages.splice(idx, 1)
    }
  }
  callbacks.onRenderNeeded(true)
  return streamingMsg
}

/**
 * Find the current streaming assistant message in the messages array.
 * Replaces the old closure-captured streamingMsg variable — this lookup
 * is always fresh and never goes stale after loadHistory replaces the array.
 */
export function findStreamingMsg(messages: ChatMessage[]): ChatMessage | undefined {
  return messages.find((m) => m.role === 'assistant' && m.streaming)
}

/**
 * Generate a unique temporary ID for a drain-pushed user message.
 * Format: `drain-{timestamp}-{randomSuffix}`
 *
 * These IDs are:
 * - Stable: never change after creation
 * - Unique: never collide (timestamp + random suffix)
 * - Distinguishable: `drain-` prefix separates them from DB IDs (integers)
 *   and optimistic push IDs (`local-` prefix)
 * - Self-cleaning: loadHistory replaces messages.value with DB-loaded
 *   messages (numeric IDs), automatically removing drain IDs
 */
export function generateDrainId(): string {
  return `drain-${Date.now()}-${Math.random().toString(36).slice(2, 8)}`
}

/**
 * Atomically process a queue_drain event on the messages array.
 *
 * 1. Finalizes the current streaming assistant message (removes streaming flag,
 *    marks unfinished tool_use blocks as done) — WITHOUT deleting it, even if
 *    it appears empty. This prevents v-for key shifts from index-based keys.
 * 2. Pushes the drained user message into messages (it was persisted to DB by
 *    the backend via AddChatMessage before the queue_drain SSE event, but
 *    loadHistory hasn't run yet so it's not in messages). This makes the user
 *    message immediately visible instead of waiting until the stream ends.
 *    The message gets a stable drain ID for Vue v-for key stability.
 * 3. Pushes a new streaming assistant placeholder for the next message.
 *
 * Returns the new streaming assistant message.
 */
export function drainQueueMessage(
  messages: ChatMessage[],
  queueId: string,
  userContent: string,
  userFiles: FileEntry[],
  currentBackend: string,
  callbacks: {
    onRenderNeeded: (forceFull?: boolean) => void
    onExtractScheduledTasks?: (msgs: ChatMessage[]) => void
  },
  drainId?: string,
  dbMessageId?: number
): ChatMessage {
  // 1. Finalize any streaming assistant message — never delete to avoid key shifts
  const streamingMsg = messages.find((m) => m.role === 'assistant' && m.streaming)
  if (streamingMsg) {
    delete streamingMsg.streaming
    // Mark unfinished tool_use blocks as done (except PermissionApproval)
    if (streamingMsg.blocks) {
      for (const block of streamingMsg.blocks) {
        if (block.type === 'tool_use' && !block.done && block.name !== 'PermissionApproval') {
          block.done = true
          if (isGarbageOutput(block.output)) {
            block.output = ''
          }
        }
      }
    }
    callbacks.onExtractScheduledTasks?.(messages)
  }

  // 2. Find the pending user message — prefer queueId matching (precise),
  //    fall back to _remoteQueueId matching (cross-device), then content matching.
  let pendingIdx = -1
  if (queueId) {
    pendingIdx = messages.findIndex((m) => m.role === 'user' && m.pending && m.id === queueId)
  }
  if (pendingIdx === -1 && queueId) {
    // Match _remote messages by their stored _remoteQueueId (precise cross-device matching)
    pendingIdx = messages.findIndex((m) => m.role === 'user' && m._remote && m['_remoteQueueId'] === queueId)
  }
  if (pendingIdx === -1 && userContent) {
    pendingIdx = messages.findIndex((m) => m.role === 'user' && (m.pending || m._remote) && m.content === userContent)
  }
  if (pendingIdx !== -1) {
    // Found the pending or remote message — clear flag, update id to stable DB id
    delete messages[pendingIdx].pending
    delete messages[pendingIdx]._remote
    delete messages[pendingIdx]['_remoteQueueId']
    if (dbMessageId) {
      messages[pendingIdx].id = dbMessageId
    } else if (drainId) {
      messages[pendingIdx].id = drainId
    }
  } else if (userContent) {
    // Fallback: pending message not found (queue event was missed).
    // Push it directly. Deduplicate by ID to avoid race with loadHistory.
    const effectiveDrainId = dbMessageId || drainId || generateDrainId()
    const alreadyExists = messages.some(
      (m) => m.id === effectiveDrainId
    )
    if (!alreadyExists) {
      messages.push({
        role: 'user',
        id: effectiveDrainId,
        _drain: true,
        content: userContent,
        blocks: userContent ? [{ type: 'text', text: userContent }] : [],
        files: userFiles.map(f => typeof f === 'string' ? { path: f, isDir: false } : f),
        createdAt: new Date().toISOString(),
      })
    }
  }

  // 3. Insert new streaming assistant placeholder right after the drain
  //    user message.
  const newStreamingMsg = {
    role: 'assistant' as const,
    id: generateDrainId(),
    content: '',
    blocks: [] as ContentBlock[],
    streaming: true,
    createdAt: new Date().toISOString(),
    backend: currentBackend,
  }
  // Find the user message that was just drained (pending flag cleared or fallback pushed)
  const drainUserIdx = pendingIdx !== -1
    ? pendingIdx
    : messages.findLastIndex((m) => m.role === 'user' && m.content === userContent)
  if (drainUserIdx !== -1) {
    messages.splice(drainUserIdx + 1, 0, newStreamingMsg)
  } else {
    messages.push(newStreamingMsg)
  }

  return newStreamingMsg
}

/**
 * Remove pending messages from the messages array whose IDs match
 * the given queueIds. Used by the queue_cancel event handler.
 * Returns the number of removed messages.
 */
export function cancelPendingMessages(
  messages: ChatMessage[],
  queueIds: string[]
): number {
  let removed = 0
  for (let i = messages.length - 1; i >= 0; i--) {
    if (messages[i].pending && queueIds.includes(String(messages[i].id))) {
      messages.splice(i, 1)
      removed++
    }
  }
  return removed
}

/**
 * Determine whether a failed tool call detail fetch should be retried.
 *
 * During streaming, tool call data may not yet be persisted to the DB (404),
 * or the msgId may point to a stale message. Instead of showing an error
 * immediately, we retry up to maxRetries times with a short delay.
 *
 * Pure function — no Vue reactivity dependencies.
 */
export function shouldRetryToolFetch(
  httpStatus: number,
  retryCount: number,
  overlayOpen: boolean,
  maxRetries: number = 3,
): boolean {
  return httpStatus === 404 && retryCount < maxRetries && overlayOpen
}

/**
 * Resolve the effective message ID for a tool detail fetch retry.
 *
 * After loadHistory replaces the messages array, the live block may have
 * a different (correct) msgId. If the live block is found, use the overlay's
 * current msgId; otherwise fall back to the original msgId.
 *
 * Pure function — no Vue reactivity dependencies.
 */
export function resolveEffectiveMsgId(
  liveBlock: ContentBlock | undefined,
  overlayMsgId: number | string | undefined,
  originalMsgId: number | string,
): number | string {
  return liveBlock ? (overlayMsgId ?? originalMsgId) : originalMsgId
}
