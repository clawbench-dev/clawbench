/**
 * Pure functions extracted from useChatSession composable.
 * These have no Vue reactivity dependencies and can be tested in isolation.
 */
import { extractPlainText } from '@/utils/userMsgIndexUtils'

/**
 * Build a lightweight fingerprint of messages for change detection.
 * Used by polling to skip UI refresh when data is unchanged.
 */
export function buildMessageSnapshot(rawMsgs: Record<string, unknown>[]): string {
  return rawMsgs.map(m =>
    `${m.id ?? ''}:${m.role}:${((m.content as string) || '').length}:${m.createdAt || ''}:${m.streaming ? 1 : 0}`
  ).join('|')
}

/**
 * Parse raw message objects from API into the format used by the UI.
 * Adds blocks, metadata, cancelled, fromDB fields as needed.
 *
 * @param rawMsgs - Raw message objects from the API
 * @param onParseAssistantContent - Parser function for assistant message content
 * @param existingMessages - Optional: current messages array, used to preserve
 *   user-set showingSummary state across loadHistory refreshes. Without this,
 *   every loadHistory call would reset showingSummary to true for messages
 *   with summaries, discarding the user's explicit toggle to view original content.
 * @param sessionRunning - Whether the session is currently running. When false,
 *   stale streaming flags from DB (e.g. orphaned streaming=1 from a crash) are
 *   stripped so the loading indicator doesn't appear on completed sessions.
 */
export function parseMessages(
  rawMsgs: Record<string, unknown>[],
  onParseAssistantContent: (content: string) => Record<string, unknown>,
  existingMessages?: Record<string, unknown>[],
  sessionRunning?: boolean
): Record<string, unknown>[] {
  // Build lookup of existing showingSummary state by message ID
  const existingSummaryState = existingMessages
    ? new Map(existingMessages.map(m => [m.id, m.showingSummary]))
    : null

  return rawMsgs.map(msg => {
    if (msg.role === 'assistant') {
      const { blocks, metadata, cancelled } = onParseAssistantContent(msg.content as string)
      msg.blocks = blocks
      if (metadata) msg.metadata = metadata
      if (cancelled) msg.cancelled = cancelled
      if (msg.streaming) {
        if (sessionRunning === false) {
          // Session is not running — strip stale streaming flag from DB.
          // This prevents the loading indicator (three dots) from appearing
          // on messages from a completed/crashed session where FinalizeStreamingMessage
          // never ran (e.g. server crash, kill -9, or orphaned before cleanup).
          delete msg.streaming
        } else {
          msg.streaming = true
          msg.fromDB = true
        }
      }
      // `showingSummary` stores only the USER's explicit preference, preserved
      // across loadHistory refreshes. It is undefined until the user toggles.
      // The actual render decision is computed by shouldShowSummary(msg), which
      // accounts for whether a summary exists and whether content was stripped
      // by the backend. We must NOT derive a default boolean here, otherwise
      // the raw field would conflate "user chose original" with "no choice yet".
      const existingState = existingSummaryState?.get(msg.id)
      if (existingState === true || existingState === false) {
        msg.showingSummary = existingState
      }
    } else if (msg.role === 'user') {
      // User messages should never have streaming flag — strip if present
      if (msg.streaming) delete msg.streaming
      if (!msg.blocks) {
        // User messages may be plain text or JSON (e.g. from ACP LoadSession
        // sync/replay). Three shapes exist:
        //   - {"blocks":[...]} block format → parse into blocks directly.
        //   - Bare content-array / ACP notification wrapper JSON → unwrap to
        //     plain text so it never renders as a literal JSON string.
        //   - Plain text → wrapped in a single text block.
        const contentStr = typeof msg.content === 'string' ? msg.content : null
        if (contentStr && contentStr.startsWith('{"blocks":')) {
          const { blocks } = onParseAssistantContent(contentStr)
          // Unwrap nested JSON serializations embedded in text blocks (dirty
          // data from early sync versions or certain ACP agents), regardless
          // of whether the parser does it.
          msg.blocks = unwrapTextBlocks(Array.isArray(blocks) ? blocks : [])
        } else if (contentStr && (contentStr.trim().startsWith('{') || contentStr.trim().startsWith('['))) {
          // Unwrap any other JSON wrapper shape (content array, ACP
          // notification, nested dirty data) to its real text.
          msg.blocks = msg.content ? [{ type: 'text', text: extractPlainText(contentStr) }] : []
        } else {
          msg.blocks = msg.content ? [{ type: 'text', text: msg.content }] : []
        }
      }
    }
    return msg
  })
}

export type MessageDisplayMode = 'summary' | 'original'

/**
 * Unwrap nested JSON serializations embedded in a message's text blocks.
 * Historical sync data (or certain ACP agents) stored a serialized JSON string
 * (e.g. an ACP notification) inside a text block's `text` field; without this
 * the block would render as a literal JSON string.
 */
export function unwrapTextBlocks(blocks: Record<string, unknown>[]): Record<string, unknown>[] {
  if (!Array.isArray(blocks)) return blocks
  return blocks.map(b => {
    if (b && typeof b === 'object' && b.type === 'text' && typeof b.text === 'string') {
      return { ...b, text: extractPlainText(b.text) }
    }
    return b
  })
}

/**
 * Decide whether a message should render its summary view.
 *
 * The `showingSummary` field stores only the USER's explicit preference
 * (true = show summary, false = show original, undefined = no explicit choice).
 * Rendering must NOT be driven by that raw field alone, because it can be
 * stale relative to message state:
 *
 *  - A message with no summary can never show one.
 *  - When the backend stripped content (blocks empty), there is nothing
 *    to show in original view — we must fall back to the summary even if the
 *    user previously toggled to original. This happens after a stream is
 *    interrupted: the summary is generated asynchronously AFTER the message
 *    was already marked showingSummary=false.
 *  - Otherwise (content present), respect the user's preference, defaulting
 *    to the global `defaultMode` when a summary exists and the user has not
 *    chosen. In 'original' mode with stripped content, this returns false so
 *    the component can lazily fetch the full content.
 */
export function shouldShowSummary(
  msg: Record<string, unknown> | { summary?: unknown; blocks?: unknown; showingSummary?: unknown },
  defaultMode: MessageDisplayMode = 'summary',
): boolean {
  const hasSummary = msg.summary != null && msg.summary !== ''
  if (!hasSummary) return false
  const blocksArr = msg.blocks as unknown as Array<unknown> | undefined
  const contentStripped = !blocksArr || blocksArr.length === 0
  // Explicit per-message preference wins whenever content is available. When
  // content was stripped by the backend, the summary is the only thing we can
  // render, so fall back to it regardless of preference (stream-interruption
  // regression, see comment above the function).
  if (msg.showingSummary !== undefined) {
    if (contentStripped) return true
    return msg.showingSummary !== false
  }
  // No explicit preference: use the global default. In original mode with
  // stripped content this returns false so the component triggers a lazy
  // fetch of the full content.
  return defaultMode === 'summary'
}

/**
 * Effective summary-visibility decision used by the UI. Wraps
 * `shouldShowSummary` with the lazy-load placeholder state: while the full
 * content is being fetched (`_loadingOriginal`), or a fetch attempt already
 * ended without content (`_loadAttempted`, e.g. a failed load or genuinely
 * empty content), the summary stays visible so the message bubble is never
 * blank. Components must use this (not `shouldShowSummary` directly) so the
 * toggle button and the rendered view never disagree.
 */
export function isShowingSummary(
  msg: Record<string, unknown> | {
    summary?: unknown; blocks?: unknown; showingSummary?: unknown; _loadingOriginal?: unknown; _loadAttempted?: unknown
  },
  defaultMode: MessageDisplayMode = 'summary',
): boolean {
  const hasSummary = msg.summary != null && msg.summary !== ''
  if (!hasSummary) return false
  const blocksArr = msg.blocks as unknown as Array<unknown> | undefined
  const contentStripped = !blocksArr || blocksArr.length === 0
  if (contentStripped && hasSummary && (msg._loadingOriginal === true || msg._loadAttempted === true)) return true
  return shouldShowSummary(msg, defaultMode)
}

/**
 * Apply a summary update to a message object.
 * Auto-switches to summary view only when the user is at the bottom of the chat.
 * If the user has scrolled up to read earlier messages, the summary is stored
 * but the view stays on original content to avoid interrupting their reading.
 *
 * @param msg - The message object to update (mutated in place)
 * @param summary - The summary text from the WebSocket event
 * @param summaryCards - Structured summary metadata (tools, taskIDs, askQuestions) from the WebSocket event
 * @param atBottom - Whether the user is currently at the bottom of the chat
 */
export function applySummaryUpdate(msg: Record<string, unknown>, summary: string | null | undefined, summaryCards: Record<string, unknown> | null | undefined, _atBottom: boolean): void {
  msg.summary = summary
  if (summaryCards !== undefined && summaryCards !== null) {
    msg.summaryCards = summaryCards
  }
  // Do NOT touch showingSummary here. It stores the user's explicit preference
  // only; the render decision is computed by shouldShowSummary(msg). Leaving it
  // undefined (until the user toggles) means "default to summary when one
  // exists", which is the correct behavior for both live and reloaded messages.
}
