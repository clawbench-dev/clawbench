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
  /**
   * Client-side monotonic sequence for messages not yet backed by a DB row
   * (pending user messages, streaming placeholders, cross-device remotes).
   * Used by sortMessages() to keep them ordered among themselves and after
   * every DB-backed message. Never used for DB-backed messages (their numeric
   * `id` is the authoritative ordering key).
   */
  seq?: number
  /**
   * For a streaming assistant placeholder: the sort value it should appear
   * right AFTER (the parent user message's sort value + 0.5). This keeps a
   * reply anchored directly below its own question, even when later messages
   * are still pending. Absent for DB-backed and pending-user messages.
   *
   * DEPRECATED in favor of `parentQueueId`: afterSort is a frozen snapshot of
   * the parent's sort value at creation time. When the parent later adopts a
   * DB id (loadHistory), the frozen value (TRANSIENT_BASE + seq, huge) pins
   * the reply after every DB message until the next loadHistory deletes it —
   * the pre-refresh ordering mess. New code sets parentQueueId and resolves
   * the parent's CURRENT sort value at sort time instead.
   */
  afterSort?: number
  /**
   * Pointer anchor for a streaming/finalized reply: the queueId (or fallback
   * parent id) of the user message this reply answers. sortMessages resolves
   * the parent's CURRENT sort value dynamically, so when the parent adopts a
   * DB id the reply follows automatically — no loadHistory needed to fix the
   * order. Distinct from `queueId` (which marks a queued USER message) so
   * hasMore's `!m.queueId` filter on user messages stays correct.
   */
  parentQueueId?: string
  /** DB-assigned queue id (backend echoes the frontend queueId for a queued
   *  message). Lets the frontend match an optimistic pending bubble to its DB
   *  row and lets queue_cancel remove pending bubbles whose id became numeric. */
  queueId?: string
  /** True while this message is still waiting for the drain loop (queued=1 in
   *  chat_history). The frontend treats it as a pending bubble until queue_drain. */
  queued?: boolean
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
 * A single created/modified file along with the Write/Edit tool call IDs that
 * produced it. toolIds let the drill-down view fetch the diff content on
 * demand from the tool-call API (blocks in loaded/summary view are slim and
 * carry no input).
 */
export interface FileChange {
  path: string
  toolIds: string[]
}

/**
 * Structured summary card metadata. Mirrors the backend model.SummaryCards.
 * createdFiles/modifiedFiles restore the file-changes banner in summary-only
 * view, where full content blocks are omitted and cannot be traversed.
 * Each entry is either a legacy plain path string or an object carrying the
 * path plus the Write/Edit tool call IDs ({ path, toolIDs }).
 */
export interface SummaryCards {
  tools?: Array<Record<string, unknown>>
  taskIDs?: number[]
  askQuestions?: Array<Record<string, unknown>>
  createdFiles?: Array<string | { path: string; toolIDs?: string[] }>
  modifiedFiles?: Array<string | { path: string; toolIDs?: string[] }>
}

// Merge a summaryCards file-change entry into a map of FileChange objects.
function mergeSummaryFileChanges(list: Array<string | { path: string; toolIDs?: string[] }> | undefined, map: Map<string, FileChange>): void {
  for (const item of list || []) {
    const path = typeof item === 'string' ? item : item?.path
    if (!path) continue
    let fc = map.get(path)
    if (!fc) {
      fc = { path, toolIds: [] }
      map.set(path, fc)
    }
    const ids = typeof item === 'string' ? [] : (item?.toolIDs || [])
    for (const id of ids) {
      if (id && !fc.toolIds.includes(id)) fc.toolIds.push(id)
    }
  }
}

/**
 * Extract file changes (created/modified) from tool_use blocks.
 * Write → created, Edit → modified. Deduplicates by file path.
 * Only considers blocks where done=true. For each file, collects the tool call
 * IDs so the diff drill-down can fetch content on demand.
 * When blocks are absent (summary-only view), falls back to the file-change
 * lists carried in summaryCards (which carry tool IDs from the backend).
 */
export function extractFileChanges(blocks: ContentBlock[], summaryCards?: SummaryCards | null): { created: FileChange[]; modified: FileChange[] } {
  const created = new Map<string, FileChange>()
  const modified = new Map<string, FileChange>()
  for (const block of blocks) {
    if (block.type !== 'tool_use' || !block.done) continue
    const filePath = (block.file_path || (block.input as Record<string, unknown>)?.file_path) as string | undefined
    if (!filePath) continue
    const map = block.name === 'Write' ? created : block.name === 'Edit' ? modified : null
    if (!map) continue
    let fc = map.get(filePath)
    if (!fc) {
      fc = { path: filePath, toolIds: [] }
      map.set(filePath, fc)
    }
    if (block.id && !fc.toolIds.includes(block.id)) fc.toolIds.push(block.id)
  }
  if (summaryCards) {
    mergeSummaryFileChanges(summaryCards.createdFiles, created)
    mergeSummaryFileChanges(summaryCards.modifiedFiles, modified)
  }
  return { created: [...created.values()], modified: [...modified.values()] }
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
    // NOTE: do NOT strip afterSort here. This reply is anchored to its question
    // via afterSort (messageSortValue prefers it). The question may still be a
    // transient string-id message, in which case dropping afterSort would make
    // the finalized reply sort by its (possibly numeric) id — above its own
    // still-transient question. loadHistory() replaces the whole array on
    // 'done'/reload and rebuilds without afterSort, restoring DB order.
    // Stripping it here would re-introduce the queued-message reply swap.
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

let seqCounter = 0

/**
 * Allocate the next client-side monotonic sequence number.
 * Transient messages (pending/streaming/string-id) use this to keep a stable
 * relative order that always sorts AFTER every DB-backed message.
 */
export function nextClientSeq(): number {
  seqCounter += 1
  return seqCounter
}

/**
 * A message is "transient" when its ordering is not yet governed by the DB:
 * it is still pending, still streaming, or has a string id (no DB row).
 * Such messages are ordered by their client sort value and always sort after
 * every DB-backed message (which carry a numeric auto-increment id).
 */
export function isTransientMessage(m: ChatMessage): boolean {
  return m.pending === true || m.streaming === true || typeof m.id !== 'number'
}

/**
 * Base offset for transient messages, high enough that every transient message
 * sorts after any plausible DB auto-increment id.
 */
const TRANSIENT_BASE = Number.MAX_SAFE_INTEGER / 4

/**
 * Numeric sort value for a single message. DB-backed messages sort by their
 * `id`; streaming assistants anchored to a parent use `afterSort`; other
 * transient messages use TRANSIENT_BASE + `seq`.
 */
export function messageSortValue(m: ChatMessage): number {
  // A message with an afterSort is a reply anchored to its parent question. It
  // must sort immediately after that parent — even if it already holds a numeric
  // DB id (e.g. set by stream_start) and even after it is finalized, because its
  // parent may still be transient (string id → TRANSIENT_BASE+seq, huge).
  // Falling back to the small numeric id here would sort the reply ABOVE its own
  // still-transient question — the queued-message reply/first-question swap.
  // loadHistory() rebuilds authoritative DB order once the stream ends, at which
  // point afterSort is gone and the numeric id is correct.
  if (typeof m.afterSort === 'number') return m.afterSort
  if (!isTransientMessage(m)) return m.id as number
  return TRANSIENT_BASE + (m.seq ?? 0)
}

/**
 * Sort value that places a message immediately AFTER the given parent message
 * (parent's value + 0.5). Returns undefined when there is no parent.
 */
export function computeAfterSort(parent?: ChatMessage): number | undefined {
  if (!parent) return undefined
  return messageSortValue(parent) + 0.5
}

/**
 * Always-stable message ordering — sorts `messages` in place.
 *
 * The DB (auto-increment `id` ASC) is the single source of truth for order.
 * Transient messages sort after all DB-backed messages; a streaming assistant
 * sorts immediately after its own question (parent + 0.5), so a reply can
 * never be displaced above an earlier reply. Array.prototype.sort is stable
 * (ES2019+), so equal-key messages keep their existing relative order.
 *
 * Callers must ONLY ever PUSH new messages (never splice by heuristic index),
 * then call this to restore order. Because physical position never encodes
 * ordering, a newer reply can never end up displayed above an older one no
 * matter how many mutations race during a stream transition.
 */
export function sortMessages(messages: ChatMessage[]): void {
  // First pass: index user messages by every stable key we can anchor to —
  // id (string or number, both normalized to string), queueId, _remoteQueueId.
  const byKey = new Map<string, ChatMessage>()
  for (const m of messages) {
    if (m.role !== 'user') continue
    if (m.id != null) byKey.set(String(m.id), m)
    if (m.queueId) byKey.set(m.queueId, m)
    const rq = (m as Record<string, unknown>)['_remoteQueueId']
    if (typeof rq === 'string' && rq) byKey.set(rq, m)
  }

  // Resolve a message's sort value, following parentQueueId chains dynamically.
  // A reply anchored to a transient parent resolves to TRANSIENT_BASE+seq (huge,
  // after every DB message); once the parent adopts a DB id the SAME reply
  // resolves to the small id + 0.5 — it follows automatically, no loadHistory
  // round-trip required (unlike the frozen afterSort snapshot it replaces).
  const resolve = (m: ChatMessage, depth: number): number => {
    if (depth > 4) return messageSortValue(m)
    const parentKey = (m as ChatMessage).parentQueueId
    if (parentKey) {
      const parent = byKey.get(parentKey)
      if (parent && parent !== m) return resolve(parent, depth + 1) + 0.5
    }
    return messageSortValue(m)
  }

  messages.sort((a, b) => resolve(a, 0) - resolve(b, 0))
}

/**
 * After loadHistory rebuilds the array from DB rows, anchor every queued
 * reply (an assistant message whose queueId matches a queued user message) to
 * its own question. This is required because queued user messages are
 * persisted (and receive their DB id) when they are enqueued — BEFORE later
 * queued messages and BEFORE the replies they eventually produce. So the raw
 * DB id order is msg2, msg3, reply2, reply3, which is not the conversational
 * order. By setting parentQueueId on each reply to its question's queueId,
 * sortMessages resolves the reply directly after its question.
 *
 * Only messages whose queueId matches an existing user message are anchored;
 * every other message keeps its natural id ordering. Idempotent — safe to run
 * on every loadHistory.
 */
export function anchorRepliesToQuestions(messages: ChatMessage[]): ChatMessage[] {
  // Build question lookup: queueId → the queued user message carrying it.
  const questionByQueueId = new Map<string, ChatMessage>()
  for (const m of messages) {
    if (m.role !== 'user') continue
    if (m.queueId) questionByQueueId.set(m.queueId, m)
  }
  for (const m of messages) {
    if (m.role !== 'assistant') continue
    if (!m.queueId) continue
    const q = questionByQueueId.get(m.queueId)
    if (!q) continue
    m.parentQueueId = m.queueId
  }
  return messages
}

/**
 * Atomically process a queue_drain event on the messages array.
 *
 * 1. Finalizes the current streaming assistant message (removes streaming flag,
 *    marks unfinished tool_use blocks as done) — WITHOUT deleting it, even if
 *    it appears empty. This prevents v-for key shifts from index-based keys.
 * 2. Finds the drained user message (by its stable queueId) and adopts its
 *    numeric DB id (dbMessageId). The row is already persisted in chat_history
 *    with queued=0 (the drain loop flipped it), so adopting the id is safe —
 *    the message is now a normal conversation record ordered by id.
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
  dbMessageId?: number,
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

  // 2. Find the queued user message by its STABLE key — the queueId that the
  //    frontend generated and sent to the backend, and which the backend echoes
  //    back in queue_drain. No content guessing: identity is the key.
  let pendingIdx = -1
  if (queueId) {
    pendingIdx = messages.findIndex((m) => m.role === 'user' && (m.pending || m._remote) && m.id === queueId)
  }
  if (pendingIdx === -1 && queueId) {
    // Cross-device: the remote user message stores the same queueId in _remoteQueueId.
    pendingIdx = messages.findIndex((m) => m.role === 'user' && m._remote && m['_remoteQueueId'] === queueId)
  }

  if (pendingIdx !== -1) {
    // Found by stable key — clear transient flags.
    //
    // Adopt the numeric DB id ONLY when the message's parent (the preceding
    // user question) is already in the DB-id domain. If the parent is still a
    // transient string-id message (its DB id not yet known), adopting the id
    // would move this drained message to the DB-id domain where it sorts ABOVE
    // its still-transient parent and earlier replies — the queued-message
    // display misalignment (M1). In that case keep the transient string id
    // (queueId) + seq; loadHistory reconciles the authoritative order later.
    delete messages[pendingIdx].pending
    delete messages[pendingIdx]._remote
    delete messages[pendingIdx]['_remoteQueueId']
    const parentIsDB = messages.slice(0, pendingIdx).some((m) => m.role === 'user' && typeof m.id === 'number')
    if (typeof dbMessageId === 'number' && dbMessageId > 0 && parentIsDB) {
      messages[pendingIdx].queueId = String(messages[pendingIdx].id)
      messages[pendingIdx].id = dbMessageId
      delete messages[pendingIdx].seq
    } else if (messages[pendingIdx].id == null) {
      messages[pendingIdx].id = drainId || generateDrainId()
    }
    if (typeof messages[pendingIdx].seq !== 'number') {
      messages[pendingIdx].seq = nextClientSeq()
    }
  } else if (userContent) {
    // Defensive: the queued message wasn't found by its key (its optimistic
    // push was dropped before this drain). Create it from the drain payload.
    const effectiveId = (typeof dbMessageId === 'number' && dbMessageId > 0) ? dbMessageId : (drainId || generateDrainId())
    if (!messages.some((m) => m.id === effectiveId)) {
      messages.push({
        role: 'user',
        id: effectiveId,
        queueId: queueId || undefined,
        content: userContent,
        blocks: userContent ? [{ type: 'text', text: userContent }] : [],
        files: userFiles.map(f => typeof f === 'string' ? { path: f, isDir: false } : f),
        createdAt: new Date().toISOString(),
        seq: nextClientSeq(),
      })
      pendingIdx = messages.length - 1
    }
  }

  // 3. Push a new streaming assistant placeholder, anchored right after the
  //    drained user message. Order is restored by sortMessages() — never
  //    encode ordering in physical array position. This is race-proof: a newer
  //    reply can never be spliced above an older one because we never
  //    splice-insert by heuristic index.
  const parent = pendingIdx !== -1 ? messages[pendingIdx] : messages[messages.length - 1]
  const newStreamingMsg = {
    role: 'assistant' as const,
    id: generateDrainId(),
    content: '',
    blocks: [] as ContentBlock[],
    streaming: true,
    createdAt: new Date().toISOString(),
    backend: currentBackend,
    seq: nextClientSeq(),
    // Anchor to the drained message via parentQueueId (dynamic resolution in
    // sortMessages) instead of afterSort (frozen TRANSIENT_BASE+seq snapshot).
    // When the parent later adopts a DB id, the reply follows automatically.
    parentQueueId: queueId || String(parent?.id ?? ''),
  }
  messages.push(newStreamingMsg)
  sortMessages(messages)

  return newStreamingMsg
}

/**
 * Remove pending messages from the messages array whose IDs match
 * the given queueIds. Used by the queue_cancel event handler.
 * Matches by id (optimistic pending bubble, string id = queueId) OR by the
 * queueId field (a queued message already adopted a numeric DB id via
 * drainQueueMessage or loadHistory). Returns the number of removed messages.
 */
export function cancelPendingMessages(
  messages: ChatMessage[],
  queueIds: string[]
): number {
  let removed = 0
  for (let i = messages.length - 1; i >= 0; i--) {
    if (messages[i].pending && (queueIds.includes(String(messages[i].id)) || queueIds.includes(messages[i].queueId || ''))) {
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
