/**
 * Pure functions and constants extracted from useChatStream composable.
 * These have no Vue reactivity dependencies and can be tested in isolation.
 */

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
  if (/^[{}\[\],:]+$/.test(trimmed)) return true
  return false
}

/**
 * Tool names that modify files on disk (canonical PascalCase, guaranteed by backend normalization).
 * Used to trigger file preview refresh after tool completion.
 */
export const FILE_MODIFYING_TOOLS = new Set(['Write', 'Edit'])

/**
 * Find the most recent block of a given type by searching backward.
 * tool_use blocks act as natural boundaries — text/thinking after a tool_use
 * should not be merged with text/thinking before it.
 */
export function findLastBlockOfType(blocks: any[], type: string): any | undefined {
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
  messages: any[],
  callbacks: {
    onRenderNeeded: (forceFull?: boolean) => void
    onExtractScheduledTasks?: (msgs: any[]) => void
  }
): any | undefined {
  const streamingMsg = messages.find((m: any) => m.role === 'assistant' && m.streaming)
  if (streamingMsg) {
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
    const hasContent = streamingMsg.content || (streamingMsg.blocks && streamingMsg.blocks.length > 0)
    if (!hasContent) {
      const idx = messages.indexOf(streamingMsg)
      if (idx !== -1) messages.splice(idx, 1)
    }
  }
  callbacks.onRenderNeeded(true)
  return streamingMsg
}

// ── New architecture: single source of truth in messages.value ──

/**
 * Find the current streaming assistant message in the messages array.
 * Replaces the old closure-captured streamingMsg variable — this lookup
 * is always fresh and never goes stale after loadHistory replaces the array.
 */
export function findStreamingMsg(messages: any[]): any | undefined {
  return messages.find((m: any) => m.role === 'assistant' && m.streaming)
}

/**
 * Create a pending user message object.
 * Pending messages live in messages.value with a `pending: true` flag.
 * They are rendered with special styling (dashed border, spinner).
 * When queue_consume fires, the pending flag is removed.
 */
export function createPendingUserMessage(text: string, files: string[] = []): any {
  return {
    role: 'user',
    content: text || '',
    blocks: text ? [{ type: 'text', text }] : [],
    files: files.map(p => ({ path: p })),
    createdAt: new Date().toISOString(),
    pending: true,
  }
}

/**
 * Process a queue_consume event using the single-array architecture.
 *
 * 1. Finalize any stale streaming assistant message
 * 2. Find the pending user message matching the consumed content and un-mark it
 *    (if not found — e.g. page was hidden during enqueue — create it)
 * 3. Push a new streaming assistant placeholder
 *
 * Returns the new streaming assistant message.
 */
export function consumePendingMessage(
  messages: any[],
  userContent: string,
  userFiles: string[],
  currentBackend: string,
  callbacks: {
    onRenderNeeded: (forceFull?: boolean) => void
    onExtractScheduledTasks?: (msgs: any[]) => void
  }
): any {
  // 1. Finalize any stale streaming message
  forceCleanupStreamingState(messages, callbacks)

  // 2. Find and un-mark the pending user message
  const pendingMsg = messages.find(
    (m: any) => m.role === 'user' && m.pending && m.content === userContent
  )
  if (pendingMsg) {
    delete pendingMsg.pending
    // Update files in case they differ (backend may have normalized paths)
    if (userFiles.length > 0) {
      pendingMsg.files = userFiles.map(p => ({ path: p }))
    }
  } else {
    // Pending message not found (page was hidden during enqueue, or
    // sendMessageNow created it without pending flag). Push it now.
    const existingUserMsg = messages.find(
      (m: any) => m.role === 'user' && m.content === userContent && !m.id
    )
    if (!existingUserMsg) {
      messages.push({
        role: 'user',
        content: userContent,
        blocks: userContent ? [{ type: 'text', text: userContent }] : [],
        files: userFiles.map(p => ({ path: p })),
        createdAt: new Date().toISOString(),
      })
    }
  }

  // 3. Push new streaming assistant placeholder
  const newStreamingMsg = {
    role: 'assistant' as const,
    content: '',
    blocks: [] as any[],
    streaming: true,
    createdAt: new Date().toISOString(),
    backend: currentBackend,
  }
  messages.push(newStreamingMsg)
  return newStreamingMsg
}

/**
 * Sync pending messages in messages.value with the authoritative backend queue.
 * Called on queue_update SSE event and on visibility change.
 *
 * The backend queue contains items like { text, files, filePaths }.
 * We compare by text content and add/remove pending messages as needed.
 */
export function syncPendingFromBackend(
  messages: any[],
  backendQueue: any[]
): void {
  const currentPending = messages.filter((m: any) => m.role === 'user' && m.pending)

  // Add pending messages that are in the backend queue but not locally
  for (const item of backendQueue) {
    const text = item.text || ''
    const exists = currentPending.some((m: any) => m.content === text)
    if (!exists) {
      messages.push(createPendingUserMessage(text, [
        ...(item.files || []),
        ...(item.filePaths || []),
      ]))
    }
  }

  // Remove pending messages that are no longer in the backend queue
  // (iterate in reverse to avoid index shifting during splice)
  for (let i = messages.length - 1; i >= 0; i--) {
    const m = messages[i]
    if (m.role === 'user' && m.pending) {
      const inBackend = backendQueue.some((item: any) => (item.text || '') === m.content)
      if (!inBackend) {
        messages.splice(i, 1)
      }
    }
  }
}


