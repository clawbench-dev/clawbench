/**
 * Pure functions and constants extracted from useChatStream composable.
 * These have no Vue reactivity dependencies and can be tested in isolation.
 *
 * Pending messages are stored in the messages array with pending: true flag.
 * No separate pendingStore — one source of truth.
 */

// ── Core chat types ──

import type { FileEntry } from '@/utils/fileAttachmentUtils'
import { extractPlainText } from '@/utils/userMsgIndexUtils'

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

/** Extract the textual content of a message: blocks' text concat, else content. */
export function messageText(m: ChatMessage): string {
  if (Array.isArray(m.blocks)) {
    const texts = m.blocks
      .filter((b) => (b.type === 'text' || b.type === 'warning') && typeof (b as { text?: unknown }).text === 'string')
      .map((b) => (b as { text: string }).text)
    if (texts.length > 0) return texts.join('')
  }
  const c = typeof m.content === 'string' ? m.content : ''
  if (c.trim().startsWith('{') || c.trim().startsWith('[')) {
    // Backend stores message content as JSON in several shapes: a blocks blob,
    // a bare content array, or an ACP notification wrapper (historical dirty
    // data / sync replay). Use the unified extractor so a literal JSON string
    // never leaks into content matching, which would break equality checks
    // against already-unwrapped bubbles.
    const text = extractPlainText(c)
    if (text) return text
    // Recognized wrapper with no text → normalize to empty so matches can
    // succeed; unrecognized JSON falls through to raw content.
    return isJsonContent(c) ? '' : c
  }
  return c
}

/** Whether the content string is valid JSON (recognized or not). */
function isJsonContent(c: string): boolean {
  const trimmed = c.trim()
  if (!trimmed.startsWith('{') && !trimmed.startsWith('[')) return false
  try {
    JSON.parse(trimmed)
    return true
  } catch {
    return false
  }
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
    // The finalized reply keeps its parentQueueId anchor: its question may
    // still be transient (string id), in which case sorting by the reply's
    // (possibly numeric) id would place it above its own question.
    // sortMessages resolves parentQueueId dynamically, so the reply follows
    // its question whether transient or DB-backed. loadHistory replaces the
    // whole array on 'done'/reload with authoritative DB order.
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
 * Base offset for transient messages, high enough that every transient message
 * sorts after any plausible DB auto-increment id.
 */
const TRANSIENT_BASE = Number.MAX_SAFE_INTEGER / 4

/**
 * Numeric sort value for a USER message (assistant replies are resolved by
 * sortMessages against their parent, never through this function).
 *
 * - Pure DB-backed user (numeric id, no live markers): the id itself.
 * - Live message (pending / streaming / string id / carries a queueId AND a
 *   client seq — i.e. still part of the in-flight send pipeline): TRANSIENT_BASE
 *   + seq, ordering purely by send order. A queued message adopts its DB id
 *   when the drain loop starts, but that id is a persist-time artifact (larger
 *   than history ids, yet smaller than a later message's id) — using it would
 *   reorder messages by persist time. DB-loaded history that merely retains a
 *   queueId (no seq) is NOT live and sorts by id.
 *
 *   A cross-device remote message that already carries a numeric DB id
 *   (user_message MessageID) sorts by id — its `_remoteQueueId` must NOT pull
 *   it into seq space, otherwise it interleaves with local seq by receive order
 *   instead of by DB id. Remote messages with a string id (MessageID absent)
 *   are covered by the `typeof m.id !== 'number'` branch.
 */
export function messageSortValue(m: ChatMessage): number {
  const isLive =
    m.pending === true ||
    m.streaming === true ||
    typeof m.id !== 'number' ||
    (m.queueId != null && m.seq != null)
  if (!isLive) return m.id as number
  return TRANSIENT_BASE + (m.seq ?? 0)
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
  // round-trip required.
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
  //
  //    Three-channel OR match, robust against loadHistory having replaced the
  //    bubble with its DB row (id becomes numeric, but queueId field survives):
  //      m.id === queueId          → optimistic bubble (string id = queueId)
  //      m.queueId === queueId     → bubble already adopted a numeric DB id
  //      m['_remoteQueueId']       → cross-device remote user message
  //
  //    The match deliberately does NOT require (m.pending || m._remote). A
  //    loadHistory snapshot that arrived between the backend persisting the
  //    drained row (queued=0) and this queue_drain WS event may have already
  //    replaced the bubble with its DB row (rebuildFromDb drops the transient
  //    bubble when queued=0), so gating on pending would miss it and fall back
  //    to pushing a SECOND user message — the reported "AAA" duplicate.
  //    queueId is the stable identity; pending/_remote are UI state, not
  //    identity.
  let pendingIdx = -1
  if (queueId) {
    pendingIdx = messages.findIndex(
      (m) => m.role === 'user' &&
        (m.id === queueId || m.queueId === queueId || (m as Record<string, unknown>)['_remoteQueueId'] === queueId)
    )
  }

  if (pendingIdx !== -1) {
    // Found by stable key — clear transient flags.
    //
    // Adopt the numeric DB id. Sorting stays in seq space while streaming (the
    // message keeps its client seq), so an adopted message still orders by
    // send order relative to not-yet-adopted bubbles. loadHistory (idle) later
    // clears seq and orders by DB id.
    delete messages[pendingIdx].pending
    delete messages[pendingIdx]._remote
    delete messages[pendingIdx]['_remoteQueueId']
    if (typeof dbMessageId === 'number' && dbMessageId > 0 && typeof messages[pendingIdx].id !== 'number') {
      // Adopt the DB id. A message that already carries a numeric id (e.g. a
      // cross-device _remote that arrived persisted) keeps it — replacing would
      // churn the v-for key. Drop seq so the message moves to the id domain
      // (sorts by DB id) like every other adopted message — keeps the sort
      // space uniform so direct-sent, queued and remote messages never
      // interleave by client receive order. Replies anchored via parentQueueId
      // resolve dynamically and stay with their parent. loadHistory (idle)
      // later reconciles the authoritative DB order.
      messages[pendingIdx].queueId = String(messages[pendingIdx].id)
      messages[pendingIdx].id = dbMessageId
      delete messages[pendingIdx].seq
    } else if (messages[pendingIdx].id == null) {
      messages[pendingIdx].id = drainId || generateDrainId()
      if (typeof messages[pendingIdx].seq !== 'number') {
        messages[pendingIdx].seq = nextClientSeq()
      }
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
    // sortMessages).
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
 * Matches by id (optimistic pending bubble, string id = queueId), by the
 * queueId field (a queued message already adopted a numeric DB id via
 * drainQueueMessage or loadHistory), or by _remoteQueueId (a cross-device
 * _remote bubble). Returns the number of removed messages.
 */
export function cancelPendingMessages(
  messages: ChatMessage[],
  queueIds: string[]
): number {
  let removed = 0
  for (let i = messages.length - 1; i >= 0; i--) {
    const m = messages[i]
    if (m.pending && (queueIds.includes(String(m.id)) || queueIds.includes(m.queueId || ''))) {
      messages.splice(i, 1)
      removed++
    } else if (m._remote && queueIds.includes((m as Record<string, unknown>)['_remoteQueueId'] as string)) {
      // Cross-device bubble: cancel removes it too — the backend row is gone,
      // so any later loadHistory would drop it anyway; remove it now so the
      // current UI doesn't show a stale pending message.
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

// ─────────────────────────────────────────────────────────────
// chatMessageReducer — single write channel for the messages array.
//
// Every mutation of the chat message list (optimistic pushes, WS events,
// loadHistory DB merges, enqueue/cancel) flows through this pure reducer.
// Components and composables only collect events and dispatch(action); they
// never touch the array directly. This eliminates the multi-writer races that
// previously corrupted streaming state (a loadHistory full-replace wiping
// optimistic bubbles / streaming placeholders, queue_drain failing to match a
// bubble that was replaced by its DB row, etc.).
//
// The reducer is a pure function (state, action) => state: fully unit-testable
// by feeding action sequences and asserting the resulting array.
// ─────────────────────────────────────────────────────────────

/** Action that mutates the chat message list. */
export type ChatMessageAction =
  // ── Optimistic / structural ──
  | { type: 'optimistic_push'; msg: ChatMessage }
  | { type: 'optimistic_remove'; id: string | number }
  | { type: 'optimistic_remove_content'; content: string }
  | { type: 'optimistic_adopt_id'; id: string | number; dbId: number }
  | { type: 'stream_placeholder'; msg: ChatMessage }
  | { type: 'clear_pending' }
  | { type: 'remove_pending'; queueId: string }
  | { type: 'clear' }
  | { type: 'prepend_older'; olderMsgs: ChatMessage[] }
  // ── WS structural events ──
  | { type: 'ws_stream_start'; messageId: number }
  | { type: 'ws_user_message'; data: { messageId?: number; content?: string; files?: FileEntry[]; senderClientId?: string; queueId?: string; backend?: string } }
  | { type: 'ws_queue_drain'; queueId: string; text: string; files: FileEntry[]; dbMessageId?: number; backend?: string }
  | { type: 'ws_queue_cancel'; queueIds: string[] }
  | { type: 'ws_error'; text: string; reason?: string }
  | { type: 'stream_finalize' }
  // ── WS block-level (in-place blocks mutation, same array reference) ──
  | { type: 'ws_content'; text: string }
  | { type: 'ws_thinking'; text: string; key?: string }
  | { type: 'ws_thinking_done' }
  | { type: 'ws_content_reset' }
  | { type: 'ws_tool_use'; data: ToolUseEventData }
  | { type: 'ws_tool_result'; data: ToolUseEventData }
  | { type: 'ws_metadata'; metadata: Record<string, unknown> }
  | { type: 'ws_warning'; text: string; reason?: string }
  // ── DB rebuild (loadHistory) ──
  | { type: 'db_load'; dbMessages: ChatMessage[] }

function findBlockByTypeBackward(blocks: ContentBlock[], type: string): ContentBlock | undefined {
  for (let i = blocks.length - 1; i >= 0; i--) {
    if (blocks[i].type === type) return blocks[i]
    if (blocks[i].type === 'tool_use') return undefined
  }
  return undefined
}

/**
 * Rebuild the messages array from the authoritative DB snapshot (loadHistory),
 * preserving ONLY the transient messages that correspond to a real DB row:
 *
 *   - live streaming placeholder: matched when the DB snapshot has a
 *     streaming=1 assistant row whose id (assigned by ws_stream_start) or
 *     queue_id (the answered queue) matches the placeholder. Its object
 *     identity is kept (v-for key stable) and the DB id merged in.
 *   - pending user bubble: matched when a queued=1 DB row carries the same
 *     queueId.
 *   - _remote cross-device bubble: matched by DB id.
 *
 * EVERYTHING else transient (duplicate leftovers, orphan placeholders,
 * string-id bubbles with no DB row) is DROPPED — the DB is authoritative, so
 * any message not present there is garbage. This makes every loadHistory
 * converge to exactly what an app restart would show, which is what makes the
 * refresh button behave identically to a restart.
 */
export function rebuildFromDb(state: ChatMessage[], dbMessages: ChatMessage[]): ChatMessage[] {
  // The live streaming placeholder (at most one) — the object whose identity
  // must be preserved so the streamed content already rendered keeps its DOM.
  const live = state.find((m) => m.role === 'assistant' && m.streaming)

  // Index DB rows by id and by queueId (queued rows + streaming rows carrying
  // the answered queue).
  const dbById = new Map<string, ChatMessage>()
  const dbByQueueId = new Map<string, ChatMessage>()
  for (const db of dbMessages) {
    if (db.id != null) dbById.set(String(db.id), db)
    if (db.role === 'user' && db.queueId) dbByQueueId.set(db.queueId, db)
  }

  // Find the DB streaming row that corresponds to the live placeholder.
  // Preferred channels: the DB id ws_stream_start already assigned to the
  // placeholder, or the answered queue (queue_id) matching the placeholder's
  // anchor. Fallback: any streaming row while the live placeholder is empty.
  let liveDb: ChatMessage | undefined
  if (live) {
    // Channel 1 — exact id match. ws_stream_start assigns the DB row's id to
    // the placeholder, so an id hit is the strongest identity proof — even
    // against a finalized row (done missed while the session went idle and
    // parseMessages stripped the streaming flag): keeping the placeholder
    // object and finalizing it is smoother than dropping it (no v-for key
    // churn) and content is identical either way.
    if (dbById.has(String(live.id))) {
      const row = dbById.get(String(live.id))!
      if (row.role === 'assistant') liveDb = row
    }
    if (!liveDb) {
      liveDb = dbMessages.find(
        (r) =>
          r.role === 'assistant' &&
          r.streaming === true &&
          (r.queueId === live.parentQueueId || r.queueId === String(live.id)),
      )
    }
    if (!liveDb) {
      liveDb = dbMessages.find(
        (r) =>
          r.role === 'assistant' &&
          r.streaming === true &&
          messageText(live) === '' &&
          (live.blocks ?? []).length === 0,
      )
    }
  }

  const used = new Set<ChatMessage>()
  const merged: ChatMessage[] = []

  for (const db of dbMessages) {
    // 1. Live streaming placeholder → keep its object, merge DB identity.
    if (db === liveDb && live) {
      used.add(live)
      if (typeof db.id === 'number' && typeof live.id !== 'number') {
        live.id = db.id
        delete live.seq
      }
      // If the matched DB row is already finalized (streaming=0 — the done
      // event was missed while the session went idle), finalize the placeholder
      // too so it renders as a normal reply (identical content, stable v-for
      // key). When the row is still streaming=1, keep streaming.
      if (db.streaming !== true) delete live.streaming
      // Merge DB fields (summary, metadata, files) onto the live object.
      if (db.summary) live.summary = db.summary
      if (db.summaryCards) live.summaryCards = db.summaryCards
      if (db.metadata && !live.metadata) live.metadata = db.metadata
      if (db.files) live.files = db.files
      // Backfill partial content: if the live placeholder is empty (e.g. freshly
      // created by a stream_start event after a re-subscribe) but the DB row has
      // already-flushed content, copy it in so previously-streamed text isn't lost.
      // Never overwrite a live placeholder that already has content — its live
      // stream is fresher than the DB's 500ms rate-limited flush.
      const liveIsEmpty = (live.blocks ?? []).length === 0 && messageText(live) === ''
      if (liveIsEmpty) {
        if (Array.isArray(db.blocks) && db.blocks.length > 0) {
          live.blocks = db.blocks
        } else if (db.content) {
          live.content = db.content
        }
      }
      merged.push(live)
      continue
    }

    // 2. Pending user bubble → keep the bubble object, sync queued state.
    const queuedRow = db.role === 'user' && db.queued === true && db.queueId
    if (queuedRow) {
      const bubble = state.find(
        (m) =>
          m.role === 'user' &&
          m.pending === true &&
          (m.queueId === db.queueId || String(m.id) === db.queueId),
      )
      if (bubble) {
        used.add(bubble)
        if (db.summary) bubble.summary = db.summary
        if (db.files) bubble.files = db.files
        bubble.pending = true
        bubble.queued = true
        merged.push(bubble)
        continue
      }
    }

    // 3. _remote cross-device bubble adopted by this DB row.
    if (db.id != null) {
      const remote = state.find(
        (m) => m.role === 'user' && m._remote === true && String(m.id) === String(db.id),
      )
      if (remote) {
        used.add(remote)
        delete (remote as Record<string, unknown>)['_remote']
        delete (remote as Record<string, unknown>)['_remoteQueueId']
        if (db.queueId && !remote.queueId) remote.queueId = db.queueId
        merged.push(remote)
        continue
      }
    }

    // 4. Everything else → append the authoritative DB row.
    const row = { ...db }
    if (row.role === 'user' && row.queued === true) row.pending = true
    merged.push(row)
  }

  // Drop all remaining state not covered by a DB row. This is the crux of the
  // rebuild: duplicate leftovers, orphaned placeholders and string-id bubbles
  // without a DB row are NOT in the authoritative DB, so they are dropped —
  // every loadHistory converges to exactly what an app restart would show.
  // While dropping, record any dropped user bubble that HAS a DB row so replies
  // anchored to the bubble's string id can be rewritten to the DB id.
  const parentAdoption = new Map<string, string>()
  for (const m of state) {
    if (used.has(m)) continue
    const isTransient = m.pending === true || m.streaming === true || typeof m.id === 'string'
    if (!isTransient) continue
    // A dropped optimistic/transient user bubble that corresponds to a DB row:
    // its replies anchor to the string id — rewrite them to the DB id below.
    if (m.role === 'user' && typeof m.id === 'string') {
      let row = dbById.get(String(m.id)) || (m.queueId ? dbByQueueId.get(m.queueId) : undefined)
      if (!row) {
        // No id/queueId identity on the DB row (a directly-sent message whose
        // row was persisted without queue_id): match by content, gated by a
        // createdAt window so two genuinely distinct identical-text messages
        // (e.g. "build" sent twice minutes apart) never rewrite each other's
        // reply anchors. This is used ONLY to rewrite reply anchors — the
        // bubble itself is still dropped, so it cannot resurrect a duplicate.
        const mText = messageText(m)
        const mTime = m.createdAt ? new Date(m.createdAt).getTime() : 0
        if (mText !== '') {
          row = dbMessages.find((r) => {
            if (r.role !== 'user' || messageText(r) !== mText) return false
            const rTime = r.createdAt ? new Date(r.createdAt).getTime() : 0
            if (rTime === 0 || mTime === 0) return false
            return Math.abs(rTime - mTime) < 5000
          })
        }
      }
      if (row && row.id != null && String(row.id) !== String(m.id)) {
        parentAdoption.set(String(m.id), String(row.id))
      }
    }
    // Deliberately NOT pushed to `merged`.
  }
  if (parentAdoption.size > 0) {
    for (const m of merged) {
      if (m.parentQueueId) {
        const newId = parentAdoption.get(m.parentQueueId)
        if (newId !== undefined) m.parentQueueId = newId
      }
    }
  }

  anchorRepliesToQuestions(merged)
  sortMessages(merged)
  return merged
}

/** The chat message reducer. Returns the next state array. */
export function chatMessageReducer(state: ChatMessage[], action: ChatMessageAction): ChatMessage[] {
  switch (action.type) {
    case 'optimistic_push': {
      state.push(action.msg)
      sortMessages(state)
      return state
    }
    case 'optimistic_remove': {
      const idx = state.findIndex((m) => String(m.id) === String(action.id))
      if (idx !== -1) state.splice(idx, 1)
      return state
    }
    case 'optimistic_remove_content': {
      // Remove the LAST pending user message matching the content (the one just
      // optimistically pushed for an enqueue that failed). Content-match is the
      // only stable key when no queueId was generated.
      const idx = state.findLastIndex(
        (m) => m.role === 'user' && m.pending && m.content === action.content
      )
      if (idx !== -1) state.splice(idx, 1)
      return state
    }
    case 'optimistic_adopt_id': {
      // A directly-sent message (sendMessageNow) learned its DB id from the
      // user_message self-echo (MessageID). Adopt it immediately so the bubble
      // no longer sorts as a transient after later queued messages. Preserve
      // the old id as queueId so replies anchored via parentQueueId keep
      // resolving to it, and DROP seq: a directly-sent message's DB id IS its
      // real conversational position (send order = persist order), so it must
      // sort by id alongside history — NOT in seq space (where it would
      // interleave with queued/remote messages by client receive order).
      // loadHistory (idle) later reconciles the authoritative DB order.
      // A PENDING bubble is a queued message still waiting for the drain loop —
      // it must NOT be adopted here (the drain carries the authoritative id and
      // clears pending). Adopting early would flip it to a normal message.
      const idx = state.findIndex((m) => String(m.id) === String(action.id))
      if (idx === -1) return state
      const target = state[idx]
      if (target.pending) return state
      const oldId = String(target.id)
      target.id = action.dbId
      target.queueId = oldId
      delete target.seq
      delete target.pending
      sortMessages(state)
      return state
    }
    case 'stream_placeholder': {
      state.push(action.msg)
      sortMessages(state)
      return state
    }
    case 'clear_pending': {
      for (let i = state.length - 1; i >= 0; i--) {
        if (state[i].pending) state.splice(i, 1)
      }
      return state
    }
    case 'remove_pending': {
      for (let i = state.length - 1; i >= 0; i--) {
        const m = state[i]
        if (m.pending && (String(m.id) === action.queueId || m.queueId === action.queueId)) {
          state.splice(i, 1)
        } else if (m._remote && (m as Record<string, unknown>)['_remoteQueueId'] === action.queueId) {
          // Cross-device bubble: cancel removes it too (backend row deleted).
          state.splice(i, 1)
        }
      }
      return state
    }
    case 'clear':
      return []
    case 'prepend_older': {
      state.unshift(...action.olderMsgs)
      sortMessages(state)
      return state
    }
    case 'ws_stream_start': {
      const sm = state.find((m) => m.role === 'assistant' && m.streaming)
      if (sm) sm.id = action.messageId
      return state
    }
    case 'ws_user_message': {
      const data = action.data
      const myClientId = typeof localStorage !== 'undefined' ? localStorage.getItem('clawbench_client_id') : null
      if (data.senderClientId && data.senderClientId === myClientId) return state
      const userContent = data.content || ''
      const userFiles: FileEntry[] = (data.files || []).map((f) => typeof f === 'string' ? { path: f, isDir: false } : f)
      const msgId = data.messageId || 0
      const remoteQueueId = data.queueId || ''
      const alreadyExists = state.some((m) => {
        if (m.role !== 'user') return false
        if (msgId > 0 && m.id === msgId) return true
        if (remoteQueueId && (m.id === remoteQueueId || m.queueId === remoteQueueId)) return true
        if (m.content === userContent && !m.pending && !m._remote) return true
        return false
      })
      if (alreadyExists) return state
      state.push({
        role: 'user',
        id: msgId > 0 ? msgId : `remote-${Date.now()}-${Math.random().toString(36).slice(2, 8)}`,
        content: userContent,
        blocks: userContent ? [{ type: 'text', text: userContent }] : [],
        files: userFiles,
        createdAt: new Date().toISOString(),
        _remote: true,
        ...(data.backend ? { backend: data.backend } : {}),
        ...(remoteQueueId ? { _remoteQueueId: remoteQueueId } : {}),
        ...((data as { queued?: boolean }).queued ? { pending: true, queued: true } : {}),
        seq: nextClientSeq(),
      } as ChatMessage)
      sortMessages(state)
      return state
    }
    case 'ws_queue_drain': {
      drainQueueMessage(
        state, action.queueId, action.text, action.files, action.backend || '',
        { onRenderNeeded: () => {}, onExtractScheduledTasks: () => {} },
        undefined, action.dbMessageId,
      )
      return state
    }
    case 'ws_queue_cancel': {
      cancelPendingMessages(state, action.queueIds)
      return state
    }
    case 'stream_finalize': {
      forceCleanupStreamingState(state, { onRenderNeeded: () => {} })
      return state
    }
    // ── Block-level: mutate the streaming message's blocks in place ──
    case 'ws_content': {
      const sm = state.find((m) => m.role === 'assistant' && m.streaming)
      if (!sm) return state
      const blocks = sm.blocks!
      const existingText = findBlockByTypeBackward(blocks, 'text')
      if (existingText) existingText.text += action.text
      else blocks.push({ type: 'text', text: action.text })
      return state
    }
    case 'ws_thinking': {
      const sm = state.find((m) => m.role === 'assistant' && m.streaming)
      if (!sm) return state
      const blocks = sm.blocks!
      const existing = findBlockByTypeBackward(blocks, 'thinking')
      if (existing) existing.text += action.text
      else blocks.push({ type: 'thinking', text: action.text, ...(action.key ? { _key: action.key } : {}) })
      return state
    }
    case 'ws_error': {
      // Display an error block. Prefer the live streaming assistant; when the
      // stream already ended (findStreamingMsg is null — e.g. a backend crash
      // after the last 'done'), append the error to the LAST assistant message
      // so the user sees it immediately instead of only after a reload.
      const errorBlock: ContentBlock = { type: 'error', text: action.text || 'Unknown error' }
      if (action.reason) errorBlock.reason = action.reason
      const sm = state.find((m) => m.role === 'assistant' && m.streaming)
      if (sm) {
        sm.blocks = [errorBlock]
        return state
      }
      for (let i = state.length - 1; i >= 0; i--) {
        const m = state[i]
        if (m.role === 'assistant') {
          if (!m.blocks) m.blocks = []
          // Replace empty/placeholder blocks with the error; otherwise append.
          const hasContent = m.blocks.some((b) => b.type === 'text' && (b as { text?: string }).text)
          if (!hasContent && !m.streaming) m.blocks = [errorBlock]
          else m.blocks.push(errorBlock)
          return state
        }
      }
      // No assistant message at all — create one.
      state.push({
        role: 'assistant', id: generateDrainId(), content: '', blocks: [errorBlock],
        streaming: false, seq: nextClientSeq(),
      })
      sortMessages(state)
      return state
    }
    case 'ws_thinking_done': {
      const sm = state.find((m) => m.role === 'assistant' && m.streaming)
      if (!sm || !sm.blocks) return state
      const existing = findBlockByTypeBackward(sm.blocks, 'thinking')
      if (existing) existing.done = true
      return state
    }
    case 'ws_content_reset': {
      const sm = state.find((m) => m.role === 'assistant' && m.streaming)
      if (!sm) return state
      sm.blocks = []
      sm.metadata = undefined
      return state
    }
    case 'ws_tool_use': {
      const sm = state.find((m) => m.role === 'assistant' && m.streaming)
      if (!sm) return state
      const data = action.data
      const blocks = sm.blocks!
      const existing = blocks.find((b) => b.type === 'tool_use' && b.id === data.id)
      if (existing) {
        if (data.input && Object.keys(data.input).length > 0) existing.input = data.input
        if (data.name) existing.name = data.name
        if (data.status !== undefined) existing.status = data.status
        if (data.summary !== undefined) existing.summary = data.summary
        if (data.display_name !== undefined) existing.display_name = data.display_name
        if (data.file_path !== undefined) existing.file_path = data.file_path
        if (data.duration_ms !== undefined) existing.duration_ms = data.duration_ms
        if (data.done) existing.done = true
      } else {
        blocks.push({
          type: 'tool_use',
          name: data.name,
          id: data.id,
          input: data.input,
          done: data.done ?? false,
          ...(data.status ? { status: data.status } : {}),
          ...(data.summary ? { summary: data.summary } : {}),
          ...(data.display_name ? { display_name: data.display_name } : {}),
          ...(data.file_path ? { file_path: data.file_path } : {}),
          ...(data.duration_ms !== undefined ? { duration_ms: data.duration_ms } : {}),
        } as ContentBlock)
      }
      return state
    }
    case 'ws_tool_result': {
      const sm = state.find((m) => m.role === 'assistant' && m.streaming)
      if (!sm || !sm.blocks) return state
      const data = action.data
      const block = sm.blocks.find((b) => b.type === 'tool_use' && b.id === data.id)
      if (block) {
        if (data.name) block.name = data.name
        if (data.status !== undefined) block.status = data.status
        block.done = true
        if (data.duration_ms !== undefined) block.duration_ms = data.duration_ms
      }
      return state
    }
    case 'ws_metadata': {
      const sm = state.find((m) => m.role === 'assistant' && m.streaming)
      if (sm) sm.metadata = action.metadata
      return state
    }
    case 'ws_warning': {
      const sm = state.find((m) => m.role === 'assistant' && m.streaming)
      if (!sm) return state
      sm.blocks!.push({ type: 'warning', text: action.text, ...(action.reason ? { reason: action.reason } : {}) })
      return state
    }
    case 'db_load': {
      return rebuildFromDb(state, action.dbMessages)
    }
    default:
      return state
  }
}
