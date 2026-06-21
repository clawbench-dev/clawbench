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
 * When queue_drain fires, the pending flag is removed.
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
 * Atomically process a queue_drain event.
 *
 * This replaces the old 2-step drain flow (queue_done → queue_consume)
 * with a single atomic operation that:
 *
 * 1. Finalizes the current streaming assistant message (removes streaming flag,
 *    marks unfinished tool_use blocks as done) — WITHOUT deleting it, even if
 *    it appears empty. This prevents v-for key shifts from index-based keys.
 * 2. Finds and un-marks the pending user message matching the draining content.
 *    If not found (e.g. page was hidden during enqueue), creates it.
 * 3. Pushes a new streaming assistant placeholder for the next message.
 *
 * Note: syncPendingFromBackend is NOT called here — the caller handles it.
 * This avoids a double-sync bug where the backendQueue (which no longer contains
 * the drained message) would cause the pending message to be removed before
 * drainQueueMessage can un-mark it.
 *
 * Returns the new streaming assistant message.
 */
export function drainQueueMessage(
  messages: any[],
  userContent: string,
  userFiles: string[],
  currentBackend: string,
  callbacks: {
    onRenderNeeded: (forceFull?: boolean) => void
    onExtractScheduledTasks?: (msgs: any[]) => void
  }
): any {
  // 1. Finalize any streaming assistant message — never delete to avoid key shifts
  const streamingMsg = messages.find((m: any) => m.role === 'assistant' && m.streaming)
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

  // 2. Find and un-mark the pending user message
  const pendingMsg = messages.find(
    (m: any) => m.role === 'user' && m.pending && m.content === userContent
  )
  if (pendingMsg) {
    delete pendingMsg.pending
    if (userFiles.length > 0) {
      pendingMsg.files = userFiles.map(p => ({ path: p }))
    }
  } else {
    // Pending message not found — create it
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
  liveBlock: any | undefined,
  overlayMsgId: number | string | undefined,
  originalMsgId: number | string,
): number | string {
  return liveBlock ? (overlayMsgId ?? originalMsgId) : originalMsgId
}


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


