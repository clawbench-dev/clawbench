/**
 * Pure functions and constants extracted from useChatStream composable.
 * These have no Vue reactivity dependencies and can be tested in isolation.
 *
 * Queued messages are NOT part of this array: they live in the separate queue
 * store (useMessageQueue) until the backend materializes them into
 * chat_history. This array holds conversation messages only, which is why
 * ordering is a plain DB-id sort and no reply anchor is needed.
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
  error_code?: number
  http_status?: number
  error_source?: string
  /**
   * The agent's own failure reason when the structured code alone is
   * uninformative — CodeBuddy reports -32603 for every internal failure and
   * puts the real cause here (e.g. "Bad substitution: o.gaps.join").
   */
  error_detail?: string
  /**
   * Parent Agent tool-call id when this block was produced by a sub-agent
   * spawned by that Agent call; empty/undefined for top-level content. Used to
   * group a sub-agent's thinking/text/tool_use under its parent Agent card.
   */
  parent_tool_call_id?: string
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
  backend?: string
  createdAt?: string
  files?: FileEntry[]
  /**
   * Client-side monotonic sequence for messages not yet backed by a DB row
   * (optimistic sends, streaming placeholders, cross-device remotes). Used by
   * sortMessages() to keep them ordered among themselves and after every
   * DB-backed message. Never used for DB-backed messages (their numeric `id`
   * is the authoritative ordering key).
   */
  seq?: number
  /**
   * Correlation key for the in-flight direct-send guard ONLY (see
   * trackInFlightSend). A directly-sent message's optimistic bubble is pushed
   * with its string pending id as `id`, then adopts the numeric DB id from the
   * POST response — this field preserves that string id so the guard can still
   * recognise the bubble afterwards.
   *
   * It is deliberately NOT an ordering anchor: ordering is by DB id, because a
   * queued message is materialized into chat_history only at dequeue time.
   */
  queueId?: string
  [key: string]: unknown
}

/** SSE event data for content events */
export interface ContentEventData {
  content?: string
  /** Parent Agent tool-call id when this content belongs to a sub-agent. */
  parent_tool_call_id?: string
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
  /** Parent Agent tool-call id when this thinking belongs to a sub-agent. */
  parent_tool_call_id?: string
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
  /** Parent Agent tool-call id when this tool call belongs to a sub-agent. */
  parent_tool_call_id?: string
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

/** Queue event data (queue_drain / queue_inject). */
export interface QueueEventData {
  queueId?: string
  sessionId?: string
  messageId?: number
}

/** Error event data */
export interface ErrorEventData {
  reason?: string
  error?: string
  error_code?: number
  http_status?: number
  error_source?: string
  error_detail?: string
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
  /** Warning/error banners carried into summary view, where content blocks are
   *  stripped. Mirrors the ContentBlock fields the banner renderer reads. */
  warnings?: Array<{
    type?: 'warning' | 'error'
    text?: string
    reason?: string
    error_code?: number
    http_status?: number
    error_source?: string
    error_detail?: string
  }>
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
    // Extract tasks from the just-finished message
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
 * In-flight direct-send queueIds.
 *
 * A DIRECT-send POST (`/api/ai/chat`, idle session) persists its row and returns
 * quickly, but a loadHistory GET that was already in flight BEFORE the POST
 * committed (e.g. the `done`-triggered reload of the previous turn, or a slow
 * panel-open load) can return a DB snapshot that predates the new row. When
 * rebuildFromDb applies that stale snapshot it drops the just-pushed optimistic
 * bubble as "transient without a DB row", and nothing ever re-creates it (the
 * user_message self-echo only adopts an EXISTING bubble) — the user's own
 * message vanishes from the list until the next forced reload.
 *
 * While a queueId is tracked here, rebuildFromDb treats a matching transient
 * user bubble that has no DB row in the snapshot as "still being persisted",
 * NOT as a stale duplicate, and keeps it. The entry is cleared as soon as a
 * db_load snapshot actually contains the row (marking the send fully acked and
 * any pre-commit GET settled), or explicitly by the caller on failure.
 */
const inFlightDirectSends = new Set<string>()

/** Mark a direct-send queueId as in-flight (POST started, row not yet acked). */
export function trackInFlightSend(queueId: string): void {
  if (queueId) inFlightDirectSends.add(queueId)
}

/** Stop tracking a direct-send queueId (row observed in a db_load, or failure). */
export function untrackInFlightSend(queueId: string): void {
  if (queueId) inFlightDirectSends.delete(queueId)
}

/** Whether a direct-send with this queueId is still awaiting DB acknowledgment. */
export function isInFlightSend(queueId?: string): boolean {
  return !!queueId && inFlightDirectSends.has(queueId)
}

/** Test hook: clear the registry (imported by unit tests via before/afterEach). */
export function resetInFlightSendsForTest(): void {
  inFlightDirectSends.clear()
}

/**
 * Base offset for transient messages, high enough that every transient message
 * sorts after any plausible DB auto-increment id.
 */
const TRANSIENT_BASE = Number.MAX_SAFE_INTEGER / 4

/**
 * Numeric sort value for a message.
 *
 * - DB-backed (numeric id, not streaming): the id itself.
 * - Transient (streaming placeholder, optimistic send with a string id):
 *   TRANSIENT_BASE + seq, ordering purely by send order and after every
 *   DB-backed message.
 *
 * A queued message is not in this array at all (it lives in the queue store
 * until dequeued), so no queueId handling is needed.
 */
export function messageSortValue(m: ChatMessage): number {
  const isLive = m.streaming === true || typeof m.id !== 'number'
  if (!isLive) return m.id as number
  return TRANSIENT_BASE + (m.seq ?? 0)
}

/**
 * Always-stable message ordering — sorts `messages` in place.
 *
 * The DB (auto-increment `id` ASC) is the single source of truth for order,
 * and it is now genuinely conversational order: a queued message is
 * materialized into chat_history only when it is dequeued, so its id always
 * precedes the reply it produces. Transient messages sort after all DB-backed
 * messages. Array.prototype.sort is stable (ES2019+), so equal-key messages
 * keep their existing relative order.
 *
 * Callers must ONLY ever PUSH new messages (never splice by heuristic index),
 * then call this to restore order.
 */
export function sortMessages(messages: ChatMessage[]): void {
  messages.sort((a, b) => messageSortValue(a) - messageSortValue(b))
}

/**
 * Finalize the current streaming assistant message in place — WITHOUT deleting
 * it, even if it appears empty. This prevents v-for key shifts from
 * index-based keys when the next turn's placeholder is pushed.
 *
 * Called when a queued message starts its own turn (queue_drain): the reply
 * that was streaming is now a finished message, and the new turn's placeholder
 * is created by the following stream_start.
 */
export function finalizeStreamingForDrain(
  messages: ChatMessage[],
  callbacks: {
    onExtractScheduledTasks?: (msgs: ChatMessage[]) => void
  } = {},
): void {
  const streamingMsg = messages.find((m) => m.role === 'assistant' && m.streaming)
  if (!streamingMsg) return
  delete streamingMsg.streaming
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
  | { type: 'clear' }
  | { type: 'prepend_older'; olderMsgs: ChatMessage[] }
  // ── WS structural events ──
  | { type: 'ws_stream_start'; messageId: number }
  | { type: 'ws_stream_split'; messageId: number }
  // A queued message started its own turn: the reply that was streaming is now
  // complete (the backend only emits `done` when the whole drain loop exits).
  | { type: 'ws_queue_drain' }
  | { type: 'ws_user_message'; data: { messageId?: number; content?: string; files?: FileEntry[]; senderClientId?: string; queueId?: string; backend?: string } }
  | { type: 'ws_error'; text: string; reason?: string; errorCode?: number; httpStatus?: number; errorSource?: string; errorDetail?: string }
  | { type: 'stream_finalize' }
  // ── WS block-level (in-place blocks mutation, same array reference) ──
  | { type: 'ws_content'; text: string; parentToolCallId?: string }
  | { type: 'ws_thinking'; text: string; key?: string; parentToolCallId?: string }
  | { type: 'ws_thinking_done' }
  | { type: 'ws_content_reset' }
  | { type: 'ws_tool_use'; data: ToolUseEventData }
  | { type: 'ws_tool_result'; data: ToolUseEventData }
  | { type: 'ws_metadata'; metadata: Record<string, unknown> }
  | { type: 'ws_warning'; text: string; reason?: string; errorCode?: number; httpStatus?: number; errorSource?: string; errorDetail?: string }
  // ── DB rebuild (loadHistory) ──
  // `sessionRunning` lets the rebuild distinguish a stale snapshot (fetched
  // before the backend committed the streaming row) from real convergence —
  // see rebuildFromDb.
  | { type: 'db_load'; dbMessages: ChatMessage[]; sessionRunning?: boolean }

function findBlockByTypeBackward(blocks: ContentBlock[], type: string, parent?: string): ContentBlock | undefined {
  const wantParent = parent || ''
  for (let i = blocks.length - 1; i >= 0; i--) {
    // Sub-agent boundary: a block belonging to a different parent (or top-level)
    // must not absorb this event's deltas.
    if ((blocks[i].parent_tool_call_id || '') !== wantParent) return undefined
    if (blocks[i].type === type) return blocks[i]
    if (blocks[i].type === 'tool_use') return undefined
  }
  return undefined
}

/** Concatenated text of the text blocks in an array, in order. */
function joinTextBlocks(blocks: ContentBlock[]): string {
  return blocks
    .filter((b) => b.type === 'text' && typeof b.text === 'string')
    .map((b) => b.text as string)
    .join('')
}

/**
 * Cumulative text spans of the DB text blocks: `index` is the block's index in
 * `dbBlocks`, `start`/`end` its character range in the concatenated DB text.
 * Used to anchor a live text block (or a DB-only block) at a text position.
 */
function dbTextSpans(dbBlocks: ContentBlock[]): Array<{ index: number; start: number; end: number }> {
  const spans: Array<{ index: number; start: number; end: number }> = []
  let offset = 0
  for (let i = 0; i < dbBlocks.length; i++) {
    const b = dbBlocks[i]
    if (b.type !== 'text' || typeof b.text !== 'string') continue
    spans.push({ index: i, start: offset, end: offset + b.text.length })
    offset += b.text.length
  }
  return spans
}

/** DB text-block index whose span contains `offset` (the last span when the
 *  offset is at or beyond the end of the DB text). */
function dbTextIndexAtOffset(
  spans: Array<{ index: number; start: number; end: number }>,
  offset: number,
): number {
  for (const s of spans) {
    if (offset < s.end) return s.index
  }
  return spans.length > 0 ? spans[spans.length - 1].index : -1
}

/** Index in `dbBlocks` of the block a live block corresponds to, or -1 when the
 *  DB flush has no counterpart (a live-only tool, an in-progress thinking the
 *  flush deliberately omits, a warning/error block). Used to anchor DB-only
 *  blocks: a block is spliced before the first base element whose DB index is
 *  greater than its own. */
function dbAnchorOfLiveBlock(lb: ContentBlock, dbBlocks: ContentBlock[]): number {
  if (lb.type === 'tool_use' && lb.id) {
    return dbBlocks.findIndex((b) => b.type === 'tool_use' && b.id === lb.id)
  }
  return -1
}

/**
 * Order-preserving merge of the DB flushed blocks into the live blocks.
 *
 * The result is built in LIVE order — a live block is never relocated — with
 * the DB's text substituted for the live text when `takeDbText` is set (case 0:
 * the DB flush is ahead, so the live text is a stale prefix). Every DB block
 * the live placeholder lacks is then spliced in at the position of the first
 * base element that follows it in DB order; blocks with no such anchor (the
 * newest ones) are appended.
 *
 * This is what keeps text and tool_use interleaved. An earlier implementation
 * prepended every DB-only non-text block and moved all DB text blocks to the
 * first live-text slot, which rendered a turn as "all tools stacked on top, all
 * text stacked below" whenever a snapshot arrived while the placeholder held
 * little or no content.
 */
function mergeOrderedBlocks(dbBlocks: ContentBlock[], liveBlocks: ContentBlock[], takeDbText: boolean): ContentBlock[] {
  const liveToolIds = new Set(liveBlocks.filter((b) => b.type === 'tool_use' && b.id).map((b) => b.id))
  const liveHasThinking = liveBlocks.some((b) => b.type === 'thinking')
  const spans = dbTextSpans(dbBlocks)
  const emittedText = new Set<number>()

  const out: ContentBlock[] = []
  const anchors: number[] = []
  const push = (block: ContentBlock, anchor: number) => {
    out.push(block)
    anchors.push(anchor)
  }

  // Walk the live blocks in order, emitting the DB text in step with the live
  // text it replaces (case 0) so a tool that sits between two text blocks in
  // the DB is not jumped over.
  let liveTextOffset = 0
  let dbTextPtr = 0
  let dbTextEmitted = 0
  for (const lb of liveBlocks) {
    if (lb.type === 'text') {
      const len = typeof lb.text === 'string' ? lb.text.length : 0
      if (takeDbText) {
        liveTextOffset += len
        while (dbTextPtr < spans.length && dbTextEmitted < liveTextOffset) {
          const s = spans[dbTextPtr++]
          push(dbBlocks[s.index], s.index)
          emittedText.add(s.index)
          dbTextEmitted = s.end
        }
      } else {
        push(lb, dbTextIndexAtOffset(spans, liveTextOffset))
        liveTextOffset += len
      }
      continue
    }
    push(lb, dbAnchorOfLiveBlock(lb, dbBlocks))
  }

  // Every DB block the live placeholder lacks. In case 0 the DB text blocks the
  // live text did not reach are included here too — they are the continuation
  // of the reply and must land after the live-covered region, not at the end of
  // the array.
  const extras: Array<{ block: ContentBlock; dbIndex: number }> = []
  dbBlocks.forEach((b, i) => {
    if (b.type === 'text') {
      if (takeDbText && !emittedText.has(i)) extras.push({ block: b, dbIndex: i })
      return
    }
    if (b.type === 'tool_use' && b.id && liveToolIds.has(b.id)) return
    if (b.type === 'thinking' && liveHasThinking) return
    extras.push({ block: b, dbIndex: i })
  })
  extras.sort((a, b) => a.dbIndex - b.dbIndex)

  for (const { block, dbIndex } of extras) {
    let p = anchors.findIndex((a) => a > dbIndex)
    if (p === -1) p = out.length
    out.splice(p, 0, block)
    anchors.splice(p, 0, dbIndex)
  }

  return out
}

/**
 * Merge the DB streaming row's flushed blocks into a live placeholder that
 * already holds content. Called when the placeholder was (re)created by a
 * stream_start event and WS increment events appended content BEFORE the
 * loadHistory DB snapshot arrived — so the DB row's rate-limited flushed
 * history (tool_use + earlier text) may be a prefix the placeholder lacks.
 *
 * Cases:
 *   1. Continuous streaming (liveText starts with dbText): the live text
 *      already covers the DB flush (a stale subset). Only DB non-text blocks
 *      (tool_use) that live is missing are adopted, spliced in at their DB
 *      position.
 *   2. Re-subscribed mid-stream (switch-back): the DB text is a true prefix
 *      the live increment does not cover. Evidence: the DB text/live text
 *      overlap at a seam, OR the DB carries a tool_use block live lacks
 *      (tools finished before the switch). The DB blocks are prepended, with
 *      the text seam deduped so a boundary re-emitted by both paths never
 *      repeats.
 *   3. No evidence (unrelated text, no tool_use anchor): leave live alone —
 *      the DB flush is stale (e.g. a content_reset boundary), and merging
 *      would duplicate unrelated content.
 *
 *   0. (checked first) The DB text is a SUPERSET of the live text — the DB
 *      flush got further than the live stream did. That is the shape a turn
 *      takes when this client missed increments (WS dropped / App backgrounded)
 *      while the backend kept flushing. Nothing in the cases above covers it:
 *      case 1 asks for the opposite containment, and case 2 only prepends a DB
 *      prefix that is strictly *shorter* than live. Left unhandled it fell
 *      through to case 3 and the live prefix silently won — which is how a
 *      finished reply got truncated to whatever had arrived before the
 *      disconnect.
 *
 * Every case builds the result in LIVE order (a live block is never relocated)
 * and splices DB-only blocks in at their DB position — see mergeOrderedBlocks.
 * The reply's text/tool interleaving is therefore preserved.
 */
function mergeStreamBlocks(dbBlocks: ContentBlock[], liveBlocks: ContentBlock[]): ContentBlock[] {
  const dbText = joinTextBlocks(dbBlocks)
  const liveText = joinTextBlocks(liveBlocks)

  // Case 0 — the DB flush is a strict superset of the live text: the live
  // stream is behind, so the DB's text is authoritative. (Equal text is left to
  // case 1, which preserves the existing "adopt DB non-text extras onto live"
  // behavior.)
  //
  // No `liveText` truthiness requirement: an empty live text is the *strongest*
  // form of this shape — the placeholder holds only a tool block and has not
  // received its text yet, while the DB already has it.
  //
  // Only the TEXT is taken from the DB. The live non-text blocks stay in place,
  // in their original order, because the DB's rate-limited flush deliberately
  // omits in-progress thinking (session_executor.go only writes a slim marker
  // for DONE thinking) — returning the DB array wholesale would silently drop
  // the reasoning the user is currently watching, along with any live
  // warning/error block. DB-only non-text blocks (tools that finished before the
  // placeholder was recreated) are spliced in at their DB position.
  if (dbText && dbText !== liveText && dbText.startsWith(liveText)) {
    return mergeOrderedBlocks(dbBlocks, liveBlocks, true)
  }

  // Case 1 — live already covers the DB text. Adopt only the DB non-text
  // blocks (tool_use finished before the placeholder was recreated) that live
  // is missing, spliced in at their DB position so the text/tool interleaving
  // is preserved.
  if (dbText && liveText.startsWith(dbText)) {
    return mergeOrderedBlocks(dbBlocks, liveBlocks, false)
  }

  // Case 3 — no overlap and the DB has no non-text history live lacks: the DB
  // flush is stale/unrelated. Do not merge.
  const dbLiveMissingTool = dbBlocks.some(
    (b) => b.type === 'tool_use' && b.id && !liveBlocks.some((l) => l.type === 'tool_use' && l.id === b.id),
  )
  let overlap = 0
  if (dbText && liveText) {
    const maxO = Math.min(dbText.length, liveText.length)
    for (let k = maxO; k > 0; k--) {
      if (dbText.slice(dbText.length - k) === liveText.slice(0, k)) {
        overlap = k
        break
      }
    }
  }
  if (!dbLiveMissingTool && overlap === 0) return liveBlocks

  // Case 2 — the DB text is a genuine prefix the live increment does not
  // cover. Prepend the DB blocks, trimming the text seam: if the DB text tail
  // repeats the live text head (a boundary re-emitted by both paths), cut it
  // from the DB tail.
  const copy = dbBlocks.map((b) => ({ ...b }))
  if (overlap > 0) {
    let remaining = overlap
    for (let i = copy.length - 1; i >= 0 && remaining > 0; i--) {
      const b = copy[i]
      if (b.type === 'text' && typeof b.text === 'string') {
        if (b.text.length <= remaining) {
          remaining -= b.text.length
          copy.splice(i, 1)
        } else {
          b.text = b.text.slice(0, b.text.length - remaining)
          remaining = 0
        }
      }
    }
  }
  return [...copy, ...liveBlocks]
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
 *
 * `sessionRunning` is the ONE exception to "the DB is authoritative". When the
 * session is still running, a snapshot with no counterpart row for the live
 * placeholder is a STALE SNAPSHOT (fetched before the backend committed the
 * streaming row), not convergence — see the preserveLive block below. When the
 * session is not running the DB really is final, so nothing is preserved.
 */
export function rebuildFromDb(state: ChatMessage[], dbMessages: ChatMessage[], sessionRunning = false): ChatMessage[] {
  // The live streaming placeholder (at most one) — the object whose identity
  // must be preserved so the streamed content already rendered keeps its DOM.
  const live = state.find((m) => m.role === 'assistant' && m.streaming)

  // Index DB rows by id.
  const dbById = new Map<string, ChatMessage>()
  for (const db of dbMessages) {
    if (db.id != null) dbById.set(String(db.id), db)
  }

  // A snapshot that CONTAINS an in-flight direct send's row means the send has
  // been fully acknowledged and persisted — any older pre-commit GET is settled
  // by now, so the in-flight protection is no longer needed. Releasing here is
  // what keeps the registry from leaking across turns: the stale-snapshot guard
  // below keys off this set, so an entry left behind would keep resurrecting a
  // user bubble that a later authoritative snapshot no longer contains (e.g.
  // after a rewind). Every finished turn ends in a loadHistory that includes
  // the just-committed row, so this always fires.
  //
  // The release is keyed off the STATE bubble's queueId matched to the DB row
  // carrying the same id — NOT off `db.queueId`. A chat_history row has no
  // queue_id (the column was dropped when the queue moved to its own table, and
  // ChatMessage has no such field), so keying off the snapshot left every direct
  // send tracked forever: the bubble then matched the in-flight branch below
  // even though the snapshot already carried its row, and the DB row was
  // appended as well → the SAME message rendered twice until a full reload.
  // The bubble's adopted numeric id is the identity both sides actually share.
  for (const m of state) {
    if (m.role !== 'user' || !m.queueId || typeof m.id !== 'number') continue
    const row = dbById.get(String(m.id))
    if (row && row.role === 'user') untrackInFlightSend(m.queueId)
  }

  // Find the DB streaming row that corresponds to the live placeholder.
  // Preferred channel: the DB id ws_stream_start already assigned to the
  // placeholder. Fallback: any streaming row while the live placeholder is
  // empty.
  let liveDb: ChatMessage | undefined
  if (live) {
    // Exact id match. ws_stream_start assigns the DB row's id to the
    // placeholder, so an id hit is the strongest identity proof — even
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
      // Anchor the elapsed timer to the TRUE stream start. The live placeholder
      // may have been recreated by a stream_start event fired on re-subscribe
      // after a session switch (createdAt = switch time) — without this the
      // streaming elapsed counter ("⋯ 45s") would reset to ~0 every time the
      // user switches away from a running session and back. The DB row's
      // created_at is the moment the stream actually began; when it is older
      // than the placeholder's, adopt it so the counter keeps counting from
      // the real start.
      const liveTime = live.createdAt ? Date.parse(live.createdAt) : NaN
      const dbTime = db.createdAt ? Date.parse(db.createdAt) : NaN
      if (Number.isFinite(dbTime) && (!Number.isFinite(liveTime) || dbTime < liveTime)) {
        live.createdAt = db.createdAt
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
      // A FINALIZED, summary-stripped row is the whole record of a turn that is
      // already over, and it carries NO blocks on purpose: the backend replaces
      // the content of a summarized non-streaming assistant row with
      // {"blocks":[]} (summarizeContentForView) and keeps the summary in a
      // separate table. Summarization runs synchronously inside Finalize, so by
      // the time a backgrounded client resumes, the reply it missed has already
      // been summarized and stripped.
      //
      // The live placeholder must therefore be EMPTIED rather than merged. Both
      // merge branches below require db.blocks to be non-empty, so a stripped
      // row skipped them entirely and the stale pre-disconnect prefix survived.
      // That produced the reported symptom twice over: the reply rendered
      // truncated (in 'mixed'/'original' mode the last reply renders as
      // original, i.e. exactly those stale blocks), AND because blocks were
      // non-empty, `shouldShowSummary` and `needsLazyOriginal` both concluded
      // "content is present" — so neither the summary was shown nor the full
      // content lazily fetched. Switching sessions rebuilds the array from the
      // DB row, which is why that appeared to fix it.
      //
      // Clearing blocks + content restores the intended contract: empty content
      // + a summary is exactly the state that renders the summary and triggers
      // the lazy original fetch. This must be an exclusive branch — falling
      // through to the backfill below would copy db.content (which IS the
      // stripped `{"blocks":[]}` JSON) straight back in.
      const dbBlocksEmpty = !Array.isArray(db.blocks) || db.blocks.length === 0
      const liveIsEmpty = (live.blocks ?? []).length === 0 && messageText(live) === ''
      if (db.streaming !== true && dbBlocksEmpty && db.summary) {
        live.blocks = []
        live.content = ''
      } else if (liveIsEmpty) {
        // Backfill partial content: if the live placeholder is empty (e.g. freshly
        // created by a stream_start event after a re-subscribe) but the DB row has
        // already-flushed content, copy it in so previously-streamed text isn't lost.
        // Never overwrite a live placeholder that already has content — its live
        // stream is fresher than the DB's 500ms rate-limited flush.
        if (Array.isArray(db.blocks) && db.blocks.length > 0) {
          live.blocks = db.blocks
        } else if (db.content) {
          live.content = db.content
        }
      } else if (Array.isArray(db.blocks) && db.blocks.length > 0 && Array.isArray(live.blocks)) {
        // Live placeholder already holds streamed content, but the DB row may
        // still carry earlier flushed history the live stream lacks — e.g. the
        // placeholder was recreated by a stream_start after a session switch
        // while the REST loadHistory was in flight, so the first WS increments
        // appended onto an EMPTY base before db_load arrived. Merge the DB
        // blocks (see mergeStreamBlocks for the containment rules).
        //
        // Deliberately NOT special-cased on "the row is finalized": the
        // streaming flag cannot distinguish "the turn is over" from "this
        // snapshot was read before the turn ended". The handler reads the
        // history rows and only afterwards samples IsSessionRunning, and
        // parseMessages deletes `streaming` whenever that sample says the
        // session is idle — so a snapshot taken mid-finalize arrives looking
        // finalized while holding only the last rate-limited flush. Adopting
        // such a snapshot wholesale would discard the newer live tail.
        // mergeStreamBlocks' containment checks are order-agnostic and handle
        // both directions correctly without needing that flag.
        live.blocks = mergeStreamBlocks(db.blocks, live.blocks)
      }
      merged.push(live)
      continue
    }

    // 2. _remote cross-device bubble adopted by this DB row.
    if (db.id != null) {
      const remote = state.find(
        (m) => m.role === 'user' && m._remote === true && String(m.id) === String(db.id),
      )
      if (remote) {
        used.add(remote)
        delete (remote as Record<string, unknown>)['_remote']
        delete (remote as Record<string, unknown>)['_remoteQueueId']
        merged.push(remote)
        continue
      }
    }

    // 3. Everything else → append the authoritative DB row.
    merged.push({ ...db })
  }

  // Drop all remaining state not covered by a DB row. This is the crux of the
  // rebuild: duplicate leftovers, orphaned placeholders and string-id bubbles
  // without a DB row are NOT in the authoritative DB, so they are dropped —
  // every loadHistory converges to exactly what an app restart would show.
  //
  // The live placeholder is preserved in exactly one case (see below): the
  // snapshot contains NO streaming assistant row at all while the session is
  // still running. Requiring "no streaming row in the snapshot" — rather than
  // merely "no row matched the placeholder" — is what makes the preserve safe
  // from duplicates: if the snapshot DOES carry a streaming row, the normal
  // matching above already claimed it (the empty-placeholder fallback matches
  // any streaming row), so an unmatched placeholder then means a genuine
  // mismatch and must be dropped rather than rendered alongside the snapshot's
  // row.
  const dbHasStreamingRow = dbMessages.some((r) => r.role === 'assistant' && r.streaming === true)
  for (const m of state) {
    if (used.has(m)) continue

    // Live streaming placeholder whose row is missing from this snapshot.
    //
    // While the session is still running, "no row in the snapshot" does NOT
    // mean "no row in the DB": the backend commits the streaming assistant row
    // only after the ACP connection is spawned/resumed (seconds), while this
    // GET can be served in ~200ms. Every loadHistory trigger that can fire in
    // that window — the WS reconnect resync (each send-queue-full reconnect),
    // the panel-open load, a foreground return — therefore returns a snapshot
    // that contains the user row but not the streaming row.
    //
    // Dropping the placeholder in that case is unrecoverable for the rest of
    // the turn: content/thinking/tool events carry no message id, so they are
    // buffered until the NEXT stream_start (useChatStream), which for this turn
    // has already passed. The user then sees an assistant bubble with no
    // content and no loading indicator until a refresh rebuilds it from the
    // (by then flushed) DB row.
    //
    // This is the same class of protection as the in-flight direct-send guard
    // below: a snapshot that provably predates a known-live write is stale, not
    // authoritative. Gated on !dbHasStreamingRow so a snapshot that DOES carry
    // the streaming row still converges through the normal path, and on
    // sessionRunning so a finished session still converges strictly to the DB
    // (a genuinely orphaned placeholder is dropped).
    if (m === live && sessionRunning && !dbHasStreamingRow) {
      merged.push(m)
      continue
    }

    // In-flight direct-send bubble: its POST is still awaiting DB ack, so the
    // snapshot may legitimately predate the row. Keep the bubble — a restart at
    // this moment WOULD show the row, so dropping it is a stale-snapshot
    // artifact, not convergence. The row in this snapshot would have cleared
    // the registry above (untrackInFlightSend), so reaching here means the row
    // genuinely is NOT in this snapshot → keep the optimistic bubble. This must
    // run before the transient gate below: after optimistic_adopt_id the bubble
    // carries a NUMERIC id (non-transient) but is still awaiting the row in a
    // db_load, so it must be protected on the same basis.
    const inFlightKey = (typeof m.queueId === 'string' ? m.queueId : '') || (typeof m.id === 'string' ? m.id : undefined)
    if (m.role === 'user' && isInFlightSend(inFlightKey)) {
      merged.push(m)
      continue
    }

    // User bubble announced by a user_message event (a queued message that was
    // just materialized, or a cross-device send). The backend emits
    // user_message ONLY after the row is committed (drain's claim deletes the
    // queue row and inserts the history row in ONE transaction; the direct-send
    // path persists before emitting). So this bubble's numeric id IS a real DB
    // id, and a snapshot that lacks it is provably STALE — the same
    // read-before-write race the in-flight guard above covers, and the same
    // reasoning as the live placeholder.
    //
    // Nothing else protects this case, which is why it must be handled here: a
    // queued message's optimistic entry lived in the queue store, and
    // removeQueued RELEASES its in-flight guard the moment this bubble is
    // rendered — so between that release and the next authoritative snapshot
    // the bubble is unprotected, and a stale db_load (a GET issued before the
    // row committed) silently dropped it. The user then saw the assistant reply
    // with no question above it until a later reload happened to include the
    // row — the "queued message shows only the assistant reply" symptom.
    //
    // Numeric id only: a string-id bubble (msgId was 0) is transient and has no
    // row to be stale about. Gated on sessionRunning so a finished session still
    // converges strictly to the DB — a rewind must drop the bubble, and it
    // cancels the run first, so this gate is false by the time it reloads.
    if (m.role === 'user' && m._remote === true && typeof m.id === 'number' && sessionRunning) {
      merged.push(m)
      continue
    }

    // Deliberately NOT pushed to `merged`.
  }

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
      // Remove the LAST optimistic user message matching the content (the one
      // just pushed for a direct send that failed). Content-match is the only
      // stable key when no id was generated.
      const idx = state.findLastIndex(
        (m) => m.role === 'user' && typeof m.id === 'string' && m.content === action.content
      )
      if (idx !== -1) state.splice(idx, 1)
      return state
    }
    case 'optimistic_adopt_id': {
      // A directly-sent message (sendMessageNow) learned its DB id from the POST
      // response or the user_message self-echo (MessageID). Adopt it immediately
      // and DROP seq: a directly-sent message's DB id IS its real conversational
      // position (send order = persist order), so it must sort by id alongside
      // history — NOT in seq space. loadHistory (idle) later reconciles the
      // authoritative DB order.
      const idx = state.findIndex((m) => String(m.id) === String(action.id))
      if (idx === -1) return state
      const target = state[idx]
      target.id = action.dbId
      delete target.seq
      sortMessages(state)
      return state
    }
    case 'stream_placeholder': {
      // Dedup by id: a placeholder whose id already exists in the array must
      // NOT be pushed again. This is the root-cause fix for the transient
      // duplicate reported after session switches:
      //
      //   switchSession(A) → clear → fetch A → db_load puts the DB rows into
      //   the array (including the finalized assistant row 39789). A late
      //   stream_start WS event (resubscription resync racing the fetch) then
      //   finds `findStreamingMsg` empty — the row is finalized, not streaming
      //   — and dispatches a fresh placeholder with the SAME id 39789. Without
      //   the dedup below the same reply would render twice; the duplicate only
      //   vanished on the next forced db_load (refresh / switching again), which
      //   is exactly the reported "switch again → back to normal" behavior.
      //
      //   Id (numeric DB id or drain-*) is the stable identity; two messages
      //   with the same id are always the same message. When the existing copy
      //   is a finalized DB row, we merely re-mark it streaming so subsequent
      //   content/tool_use/done events find it and append onto the authoritative
      //   object — no second row is ever created.
      const existing = state.find(
        (m) =>
          m.role === 'assistant' &&
          action.msg.id != null &&
          String(m.id) === String(action.msg.id),
      )
      if (existing) {
        // Re-activate the existing copy (a finalized DB row) as the live
        // streaming message so the incoming stream events have a target.
        if (!existing.streaming) existing.streaming = true
        return state
      }
      state.push(action.msg)
      sortMessages(state)
      return state
    }
    case 'clear':
      return []
    case 'prepend_older': {
      // Defensive dedup: the "older" page must never contain rows already in
      // the array. A raced loadMore with an empty/string before_id cursor makes
      // the backend return the most recent window again (instead of strictly
      // older rows), which would otherwise prepend a full copy of the loaded
      // history — the reported AABBCC "every message doubled" bug that only a
      // forced db_load (refresh) could heal. Skip any incoming row whose id
      // already exists in the array (string or numeric, same identity).
      const existing = new Set(state.map((m) => String(m.id)))
      const fresh = action.olderMsgs.filter((m) => !existing.has(String(m.id)))
      if (fresh.length === 0) return state
      state.unshift(...fresh)
      sortMessages(state)
      return state
    }
    case 'ws_stream_start': {
      const sm = state.find((m) => m.role === 'assistant' && m.streaming)
      if (sm) {
        // Adopt the DB id ONLY when this placeholder does not already carry one.
        // A mid-turn split opens a second streaming row in the same turn; if a
        // stale/duplicate stream_start for the FIRST row arrived after the split
        // it would otherwise rename the new "after" bubble to the "before" row's
        // id, collapsing the two messages back into one. A placeholder that
        // already holds a numeric DB id is authoritative for its own row.
        if (typeof sm.id !== 'number') {
          sm.id = action.messageId
        }
      }
      return state
    }
    case 'ws_stream_split': {
      // The assistant reply was split in two at a mid-turn injection point. The
      // backend finalized the "before" half and opened a new streaming row, so:
      //   1. the current streaming bubble becomes the "before" half — drop its
      //      streaming flag (it is complete) but keep its blocks and id;
      //   2. push a fresh empty streaming bubble for the "after" half, carrying
      //      the new row's DB id.
      // The injected question was materialized into chat_history before the
      // "after" row, so it sorts between the two halves by DB id — exactly the
      // conversational order, no anchor needed.
      const before = state.find((m) => m.role === 'assistant' && m.streaming)
      if (before) {
        delete before.streaming
        // Unfinished tool blocks belong to the completed half: mark them done so
        // their spinners stop (PermissionApproval excluded — see
        // forceCleanupStreamingState for why).
        if (before.blocks) {
          for (const block of before.blocks) {
            if (block.type === 'tool_use' && !block.done && block.name !== 'PermissionApproval') {
              block.done = true
              if (isGarbageOutput(block.output)) block.output = ''
            }
          }
        }
      }

      // A placeholder for the new row may already exist (e.g. a replayed
      // stream_split, or a db_load raced ahead). Only push when absent.
      const exists = state.some(
        (m) => m.role === 'assistant' && (String(m.id) === String(action.messageId) || m._dbMessageId === action.messageId),
      )
      if (!exists) {
        state.push({
          role: 'assistant',
          id: action.messageId,
          content: '',
          blocks: [],
          streaming: true,
          createdAt: new Date().toISOString(),
          seq: nextClientSeq(),
          _dbMessageId: action.messageId,
        } as ChatMessage)
      }
      sortMessages(state)
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
        // Content is only a dedup key when it actually identifies the message.
        // An attachment-only message (a file/image sent from IM) has content "",
        // so matching on it would collapse every such message into the first
        // one and silently drop files sent from another device.
        if (userContent !== '' && m.content === userContent && !m._remote) return true
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
        seq: nextClientSeq(),
      } as ChatMessage)
      sortMessages(state)
      return state
    }
    case 'ws_queue_drain': {
      finalizeStreamingForDrain(state)
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
      const parent = action.parentToolCallId
      const existingText = findBlockByTypeBackward(blocks, 'text', parent)
      if (existingText) existingText.text += action.text
      else blocks.push({ type: 'text', text: action.text, ...(parent ? { parent_tool_call_id: parent } : {}) })
      return state
    }
    case 'ws_thinking': {
      const sm = state.find((m) => m.role === 'assistant' && m.streaming)
      if (!sm) return state
      const blocks = sm.blocks!
      const parent = action.parentToolCallId
      const existing = findBlockByTypeBackward(blocks, 'thinking', parent)
      if (existing) existing.text += action.text
      else blocks.push({ type: 'thinking', text: action.text, ...(action.key ? { _key: action.key } : {}), ...(parent ? { parent_tool_call_id: parent } : {}) })
      return state
    }
    case 'ws_error': {
      // Display an error block. Prefer the live streaming assistant; when the
      // stream already ended (findStreamingMsg is null — e.g. a backend crash
      // after the last 'done'), append the error to the LAST assistant message
      // so the user sees it immediately instead of only after a reload.
      const errorBlock: ContentBlock = { type: 'error', text: action.text || 'Unknown error' }
      if (action.reason) errorBlock.reason = action.reason
      if (action.errorCode) errorBlock.error_code = action.errorCode
      if (action.httpStatus) errorBlock.http_status = action.httpStatus
      if (action.errorSource) errorBlock.error_source = action.errorSource
      if (action.errorDetail) errorBlock.error_detail = action.errorDetail
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
      // thinking_done has no parent in the payload; it marks the most recently
      // emitted thinking block (which may belong to a sub-agent). Scan backward
      // with the tool_use boundary (original semantics) but WITHOUT the parent
      // boundary — a parent-aware lookup returns undefined for the whole
      // duration of a sub-agent run, leaving child thinking marked done in the
      // DB but not live.
      for (let i = sm.blocks.length - 1; i >= 0; i--) {
        const b = sm.blocks[i]
        if (b.type === 'thinking') {
          b.done = true
          break
        }
        if (b.type === 'tool_use') break
      }
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
        if (data.parent_tool_call_id) existing.parent_tool_call_id = data.parent_tool_call_id
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
          ...(data.parent_tool_call_id ? { parent_tool_call_id: data.parent_tool_call_id } : {}),
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
        if (data.parent_tool_call_id) block.parent_tool_call_id = data.parent_tool_call_id
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
      const warningBlock: ContentBlock = { type: 'warning', text: action.text }
      if (action.reason) warningBlock.reason = action.reason
      if (action.errorCode) warningBlock.error_code = action.errorCode
      if (action.httpStatus) warningBlock.http_status = action.httpStatus
      if (action.errorSource) warningBlock.error_source = action.errorSource
      if (action.errorDetail) warningBlock.error_detail = action.errorDetail
      sm.blocks!.push(warningBlock)
      return state
    }
    case 'db_load': {
      return rebuildFromDb(state, action.dbMessages, action.sessionRunning === true)
    }
    default:
      return state
  }
}
