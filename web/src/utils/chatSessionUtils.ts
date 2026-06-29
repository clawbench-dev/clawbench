/**
 * Pure functions extracted from useChatSession composable.
 * These have no Vue reactivity dependencies and can be tested in isolation.
 */

/**
 * Build a lightweight fingerprint of messages for change detection.
 * Used by polling to skip UI refresh when data is unchanged.
 */
export function buildMessageSnapshot(rawMsgs: any[]): string {
  return rawMsgs.map(m =>
    `${m.id ?? ''}:${m.role}:${(m.content || '').length}:${m.createdAt || ''}:${m.streaming ? 1 : 0}`
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
  rawMsgs: any[],
  onParseAssistantContent: (content: string) => any,
  existingMessages?: any[],
  sessionRunning?: boolean
): any[] {
  // Build lookup of existing showingSummary state by message ID
  const existingSummaryState = existingMessages
    ? new Map(existingMessages.map(m => [m.id, m.showingSummary]))
    : null

  return rawMsgs.map(msg => {
    if (msg.role === 'assistant') {
      const { blocks, metadata, cancelled } = onParseAssistantContent(msg.content)
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
      // Preserve existing showingSummary state if the user explicitly toggled it.
      // Only set the default (true when summary exists) for messages not yet seen.
      const existingState = existingSummaryState?.get(msg.id)
      if (existingState === true || existingState === false) {
        // User has explicitly toggled this message — preserve their choice
        msg.showingSummary = existingState
      } else if (msg.summary != null && msg.summary !== '') {
        msg.showingSummary = true
      } else {
        msg.showingSummary = false
      }
    } else if (msg.role === 'user') {
      // User messages should never have streaming flag — strip if present
      if (msg.streaming) delete msg.streaming
      if (!msg.blocks) {
        // User messages may be plain text or block-format JSON (e.g. from ACP LoadSession).
        // If content starts with {"blocks":, parse it the same way as assistant messages.
        const contentStr = typeof msg.content === 'string' ? msg.content : null
        if (contentStr && contentStr.startsWith('{"blocks":')) {
          const { blocks } = onParseAssistantContent(contentStr)
          msg.blocks = blocks
        } else {
          msg.blocks = msg.content ? [{ type: 'text', text: msg.content }] : []
        }
      }
    }
    return msg
  })
}

/**
 * Apply a summary update to a message object.
 * Auto-switches to summary view only when the user is at the bottom of the chat.
 * If the user has scrolled up to read earlier messages, the summary is stored
 * but the view stays on original content to avoid interrupting their reading.
 *
 * @param msg - The message object to update (mutated in place)
 * @param summary - The summary text from the WebSocket event
 * @param atBottom - Whether the user is currently at the bottom of the chat
 */
export function applySummaryUpdate(msg: any, summary: string | null | undefined, _atBottom: boolean): void {
  msg.summary = summary
  // Only set default when showingSummary hasn't been set yet.
  // If the user has already toggled (or parseMessages initialized it), don't override.
  if (msg.showingSummary === undefined) {
    msg.showingSummary = summary != null && summary !== ''
  }
}
