import { onMounted, onUnmounted, type Ref } from 'vue'
import { cancelChat } from '@/utils/api'
import { useReconnect } from './useReconnect'
import { gt } from '@/composables/useLocale'
import { updateModeState, updateAvailableModes, updateCommandState, updateThinkingEffortState, updateAvailableThinkingEfforts, currentAgentId } from './useSessionIdentity'
import { updateACPModelList } from './useAgents'
import { updatePlanEntries } from './usePlanProgress'
import { FILE_MODIFYING_TOOLS, findLastBlockOfType, forceCleanupStreamingState as _forceCleanupStreamingState } from '@/utils/chatStreamUtils.ts'

export interface UseChatStreamOptions {
  messages: Ref<any[]>
  currentSessionId: Ref<string>
  currentBackend: Ref<string>
  loading: Ref<boolean>
  onRenderNeeded: (forceFull?: boolean) => void
  onScrollBottom: (force?: boolean) => void
  onLoadHistory: () => Promise<void>
  onMessage: () => void
  onOpen: () => void
  isOpen: Ref<boolean>
  onParseAssistantContent: (content: string) => { blocks: any[]; metadata?: any; cancelled?: boolean }
  onToast: (msg: string, opts?: any) => void
  onNotification: (title: string, opts?: any) => void
  onStreamEnd?: (reason: 'done' | 'cancelled' | 'error') => void
  onQueueUpdate?: (queue: any[]) => void
  onQueueConsume?: () => void
  onFileModified?: (filePath: string) => void
  onExtractScheduledTasks?: (msgs: any[]) => void
}

export function useChatStream(options: UseChatStreamOptions) {
  const {
    messages,
    currentSessionId,
    currentBackend,
    loading,
    onRenderNeeded,
    onScrollBottom,
    onLoadHistory,
    onMessage,
    onOpen,
    isOpen,
    onParseAssistantContent,
    onToast,
    onNotification,
    onStreamEnd,
    onQueueUpdate,
    onQueueConsume,
    onFileModified,
    onExtractScheduledTasks,
  } = options

  let eventSource: EventSource | null = null
  let streamTimeout: ReturnType<typeof setTimeout> | null = null
  let renderTimer: ReturnType<typeof setTimeout> | null = null
  let pollingInterval: ReturnType<typeof setInterval> | null = null
  // Flag to indicate the EventSource was closed intentionally by cleanupActiveStream
  // (session switch), so the stale onerror handler should not schedule reconnects.
  let disconnectedByCleanup = false
  // Track tool_use timeout timers so we can clean them up
  const toolUseTimeouts: Map<string, ReturnType<typeof setTimeout>> = new Map()

  const STREAM_TIMEOUT_MS = 30000 // 30 seconds without any SSE event = try reconnect
  const PERMISSION_STREAM_TIMEOUT_MS = 300000 // 5 min when permission approval is pending (user deciding)
  const TOOL_USE_TIMEOUT_MS = 30000 // 30 seconds without 'done' event = mark as done

  const reconnect = useReconnect({
    maxAttempts: 3,
    baseDelay: 2000,
    onReconnect: () => connectStream(currentSessionId.value, true),
  })

  function debouncedRender() {
    if (renderTimer) clearTimeout(renderTimer)
    // Panel not visible: skip rendering and scrolling — data still accumulates,
    // rendering will catch up when the tab becomes active (loadHistory on re-activate)
    if (!isOpen.value) {
      renderTimer = null
      return
    }
    renderTimer = window.setTimeout(() => {
      onRenderNeeded()
      onScrollBottom()
      renderTimer = null
    }, 80)
  }

  function hasPendingPermissionApproval(): boolean {
    const streamingMsg = messages.value.find((m: any) => m.role === 'assistant' && m.streaming)
    if (!streamingMsg?.blocks) return false
    return streamingMsg.blocks.some(
      (b: any) =>
        b.type === 'tool_use' &&
        b.name === 'PermissionApproval' &&
        !b.done &&
        !b.input?.autoApproved
    )
  }

  function resetStreamTimeout() {
    if (streamTimeout) clearTimeout(streamTimeout)
    // Extend timeout when a permission approval is pending — the user needs time to decide
    const timeoutMs = hasPendingPermissionApproval() ? PERMISSION_STREAM_TIMEOUT_MS : STREAM_TIMEOUT_MS
    streamTimeout = setTimeout(() => {
      console.warn('SSE stream timeout - no events received, reconnecting')
      // No SSE event received for too long — reconnect instead of killing the session
      disconnectStream()
      // The AI session continues on the backend; just reconnect SSE
      if (currentSessionId.value && loading.value && reconnect.shouldReconnect()) {
        reconnect.scheduleReconnect()
      } else {
        // Too many reconnect attempts or session no longer active, fall back to polling.
        // Do NOT call forceCleanupStreamingState() here — it deletes the streaming
        // flag, which makes pollUntilDone's incremental update unable to find the
        // streaming message (messages.value.find(m => m.streaming) returns null),
        // causing it to push a duplicate message from DB. This is the same fix as
        // the onerror non-recoverable path (see ISS-xxx comment there).
        pollUntilDone()
      }
    }, STREAM_TIMEOUT_MS)
  }

  function disconnectStream(calledFromCleanup = false) {
    if (streamTimeout) { clearTimeout(streamTimeout); streamTimeout = null }
    clearToolUseTimeouts()
    if (eventSource) {
      eventSource.close()
      eventSource = null
    }
    // When called from cleanupActiveStream (session switch), mark that
    // the disconnection was intentional so the stale onerror handler
    // can skip reconnect/polling logic.
    if (calledFromCleanup) {
      disconnectedByCleanup = true
    }
  }

  function clearToolUseTimeouts() {
    for (const timer of toolUseTimeouts.values()) {
      clearTimeout(timer)
    }
    toolUseTimeouts.clear()
  }

  /**
   * Clean up streaming state for the current assistant message.
   * Delegates to the extracted pure function, then handles composable-specific
   * cleanup (tool_use timeouts, loading state).
   */
  function forceCleanupStreamingState() {
    clearToolUseTimeouts()
    _forceCleanupStreamingState(messages.value, {
      onRenderNeeded,
      onExtractScheduledTasks,
    })
    loading.value = false
  }

  function stopPolling() {
    if (pollingInterval) { clearInterval(pollingInterval); pollingInterval = null }
  }

  function pollUntilDone() {
    stopPolling()
    let jsonParseFailures = 0
    const MAX_JSON_PARSE_FAILURES = 5
    pollingInterval = setInterval(async () => {
      try {
        const resp = await fetch(`/api/ai/chat?session_id=${encodeURIComponent(currentSessionId.value)}&limit=1`, { credentials: 'same-origin' })
        if (!resp.ok) {
          throw new Error(`HTTP ${resp.status}`)
        }
        let data
        try {
          data = await resp.json()
          jsonParseFailures = 0
        } catch {
          jsonParseFailures++
          if (jsonParseFailures >= MAX_JSON_PARSE_FAILURES) {
            console.error('Polling: too many invalid JSON responses, giving up')
            throw new Error('Invalid JSON response')
          }
          console.error('Polling: invalid JSON response')
          return
        }
        // Parse messages from server response
        const latestMsgs = (data.messages || []).map(msg => {
          if (msg.role === 'assistant') {
            const { blocks, metadata, cancelled } = onParseAssistantContent(msg.content)
            msg.blocks = blocks
            if (metadata) msg.metadata = metadata
            if (cancelled) msg.cancelled = cancelled
          } else if (msg.role === 'user' && !msg.blocks) {
            msg.blocks = msg.content ? [{ type: 'text', text: msg.content }] : []
          }
          return msg
        })

        if (!data.running) {
          stopPolling()
          // Use onLoadHistory() for the final load — it handles the full render
          // pipeline (KaTeX, annotations, etc.) correctly for completed messages,
          // and properly manages session state (model sync, expandedTools, etc.)
          // This avoids the flickering caused by directly replacing messages.value
          // which destroys and rebuilds the entire Vue component tree.
          onLoadHistory().finally(() => {
            loading.value = false
            onMessage()
            onStreamEnd?.('done')
            if (!isOpen.value) {
              const lastMsg = messages.value[messages.value.length - 1]
              if (lastMsg?.role === 'assistant') {
                onToast(gt('chat.stream.aiReplied'), { icon: '🤖', duration: 5000, onClick: () => onOpen() })
                onNotification(gt('chat.stream.aiReplied'), {
                  body: gt('chat.stream.clickToViewReply'),
                  onClick: () => onOpen()
                })
              }
            }
          })
          return
        }
        // Session still running — incremental update: only mutate the streaming
        // assistant message's blocks in place to avoid destroying/rebuilding the
        // entire Vue component tree (which causes severe UI flickering every 2s).
        const lastAssistant = latestMsgs.findLast(m => m.role === 'assistant')
        const existingStreaming = messages.value.find((m: any) => m.role === 'assistant' && m.streaming)

        if (lastAssistant && existingStreaming) {
          // Update blocks in place — Vue tracks array mutations on reactive proxies,
          // so ContentBlocks.vue picks up the change without a full component rebuild.
          existingStreaming.blocks = lastAssistant.blocks
          if (lastAssistant.metadata) existingStreaming.metadata = lastAssistant.metadata
          if (lastAssistant.cancelled) existingStreaming.cancelled = lastAssistant.cancelled
        } else if (lastAssistant && !existingStreaming) {
          // No existing streaming message — find the local assistant message by DB id
          // to avoid pushing a duplicate. This can happen when forceCleanupStreamingState
          // was called before pollUntilDone (e.g. from cancelStream's catch block).
          const existingById = lastAssistant.id
            ? messages.value.find((m: any) => m.id === lastAssistant.id)
            : null
          if (existingById) {
            // Reuse the existing message — restore streaming flag and update content
            existingById.streaming = true
            existingById.blocks = lastAssistant.blocks
            if (lastAssistant.metadata) existingById.metadata = lastAssistant.metadata
            if (lastAssistant.cancelled) existingById.cancelled = lastAssistant.cancelled
          } else {
            // Truly no existing message — push new one
            lastAssistant.streaming = true
            messages.value.push(lastAssistant)
          }
        }

        currentSessionId.value = data.sessionId || currentSessionId.value
        // Incremental render via debounce — same as the SSE streaming path.
        // NOT onRenderNeeded(true) which triggers full pipeline (KaTeX, annotations)
        // and causes flickering during streaming.
        if (isOpen.value) {
          debouncedRender()
        }
      } catch (err) {
        console.error('Polling error:', err)
        stopPolling()
        // Clean up streaming state — if a non-empty streaming message exists,
        // remove the streaming flag so it stops showing the loading indicator.
        // Without this, a streaming message with content stays stuck in loading
        // state forever after polling errors out (e.g. server not ready during restart).
        const streamingIdx = messages.value.findIndex((m: any) => m.role === 'assistant' && m.streaming)
        if (streamingIdx !== -1) {
          const streamingMsg = messages.value[streamingIdx]
          const hasContent = streamingMsg.content || (streamingMsg.blocks && streamingMsg.blocks.length > 0)
          if (hasContent) {
            delete streamingMsg.streaming
          } else {
            messages.value.splice(streamingIdx, 1)
          }
        }
        onToast(gt('chat.stream.connectionFailed'), { icon: '⚠️' })
        loading.value = false
        onRenderNeeded(true)
        onStreamEnd?.('error')
      }
    }, 2000)
  }

  function connectStream(sessionId: string, isRetry = false) {
    disconnectStream()
    stopPolling()
    // Reset the cleanup flag — any new connection is intentional
    disconnectedByCleanup = false
    // Only reset reconnect state for fresh/intentional connections (user action,
    // foreground return, network recovery). Do NOT reset for automatic reconnection
    // attempts — that would clear reconnectAttempts, making maxAttempts useless.
    if (!isRetry) {
      reconnect.reset()
    }

    // Find existing streaming message or create a new one
    let streamingMsg = messages.value.find(m => m.role === 'assistant' && m.streaming)
    if (!streamingMsg) {
      // No streaming message from DB — create empty assistant message
      messages.value.push({
        role: 'assistant',
        content: '',
        blocks: [],
        streaming: true,
        createdAt: new Date().toISOString(),
        backend: currentBackend.value
      })
      // Re-acquire from the reactive array so that all subsequent mutations
      // (blocks.push, text +=, metadata assignment) go through Vue's reactive
      // proxy and trigger UI re-renders. Without this, the local variable
      // still points to the raw object — Vue never sees the changes.
      streamingMsg = messages.value[messages.value.length - 1]
      // Keep renderedContents in sync with messages array
      onRenderNeeded()
    }
    onScrollBottom()

    // Guard: skip events if session changed or message was removed
    const guard = () => {
      if (currentSessionId.value !== sessionId) return false
      if (!messages.value.includes(streamingMsg)) return false
      return true
    }

    eventSource = new EventSource(`/api/ai/chat/stream?session_id=${encodeURIComponent(sessionId)}`, { withCredentials: true })

    // Capture reference to THIS EventSource instance so event handlers can
    // safely close only the stale connection without affecting a new session's
    // EventSource (the `eventSource` variable may be reassigned by connectStream).
    const esRef = eventSource

    // Start stream timeout
    resetStreamTimeout()

    eventSource.addEventListener('resume_split', () => {
      if (!guard()) return
      resetStreamTimeout()
      // AutoResumeBackend detected ExitPlanMode and will auto-resume.
      // Phase 1 content is already finalized in the DB — keep the current
      // streamingMsg visible (user sees their Phase 1 reply) and create a
      // NEW streaming message for Phase 2. This prevents:
      //   1. Phase 1 content visually disappearing (blocks=[] was too aggressive)
      //   2. guard() stale-reference issues if loadHistory replaces messages
      // Finalize the Phase 1 message (remove streaming flag so it stays visible)
      delete streamingMsg.streaming
      // Create a new Phase 2 streaming message
      messages.value.push({
        role: 'assistant',
        content: '',
        blocks: [],
        streaming: true,
        createdAt: new Date().toISOString(),
        backend: currentBackend.value
      })
      // Re-acquire from the reactive array so subsequent mutations go through Vue's proxy
      streamingMsg = messages.value[messages.value.length - 1]
      onRenderNeeded()
      debouncedRender()
    })

    eventSource.addEventListener('content', (e) => {
      if (!guard()) return
      resetStreamTimeout()
      let data: any
      try { data = JSON.parse(e.data) } catch { console.warn('SSE content: invalid JSON, skipping'); return }
      // Coalesce content into the most recent text block
      const blocks = streamingMsg.blocks
      const existingText = findLastBlockOfType(blocks, 'text')
      if (existingText) {
        existingText.text += data.content
      } else {
        blocks.push({ type: 'text', text: data.content })
      }
      // Note: Task creation is now handled by the backend automatically
      debouncedRender()
    })

    eventSource.addEventListener('thinking', (e) => {
      if (!guard()) return
      resetStreamTimeout()
      let data: any
      try { data = JSON.parse(e.data) } catch { console.warn('SSE thinking: invalid JSON, skipping'); return }
      const blocks = streamingMsg.blocks
      // Coalesce thinking into the most recent thinking block
      const existingThinking = findLastBlockOfType(blocks, 'thinking')
      if (existingThinking) {
        existingThinking.text += data.text
      } else {
        blocks.push({ type: 'thinking', text: data.text })
      }
      // Trigger debounced render for inline thinking content during streaming
      debouncedRender()
      // Skip scroll when panel not visible
      if (isOpen.value) {
        onScrollBottom()
      }
    })

    // ACP think tool completed — mark the last thinking block as done
    // so the spinner disappears immediately instead of waiting for the
    // entire AI response to finish.
    eventSource.addEventListener('thinking_done', () => {
      if (!guard()) return
      const blocks = streamingMsg.blocks
      // Mark the last thinking block as done
      for (let i = blocks.length - 1; i >= 0; i--) {
        if (blocks[i].type === 'thinking') {
          blocks[i].done = true
          break
        }
      }
      onRenderNeeded()
    })

    eventSource.addEventListener('tool_use', (e) => {
      if (!guard()) return // Check guard first to prevent stale events corrupting new session (ISS-304)
      resetStreamTimeout()
      let data: any
      try { data = JSON.parse(e.data) } catch { console.warn('SSE tool_use: invalid JSON, skipping'); return }
      const blocks = streamingMsg.blocks
      // Always check for existing block with same ID first — the backend may
      // emit multiple tool_use events for the same call (start + stop), and
      // we should merge them rather than creating duplicates.
      const existing = blocks.find(b => b.type === 'tool_use' && b.id === data.id)
      if (data.done) {
        if (existing) {
          existing.input = data.input || existing.input
          existing.done = true
          if (data.output !== undefined) existing.output = data.output
          if (data.status !== undefined) existing.status = data.status
        }
        // Clear timeout if set
        const timer = toolUseTimeouts.get(data.id)
        if (timer) { clearTimeout(timer); toolUseTimeouts.delete(data.id) }

        // Notify file modification: when a file-modifying tool completes,
        // extract the file_path from its input and call the callback.
        // This provides reliable preview refresh even when fsnotify SSE
        // is disconnected (defense-in-depth with the file watcher).
        if (FILE_MODIFYING_TOOLS.has(data.name) && onFileModified) {
          const input = data.input || existing?.input
          const filePath = input?.file_path
          if (filePath) {
            onFileModified(filePath)
          }
        }
      } else {
        if (existing) {
          // Update existing block with new input data (may be richer than start event)
          if (data.input && Object.keys(data.input).length > 0) {
            existing.input = data.input
          }
          if (data.name) existing.name = data.name
          if (data.output !== undefined) existing.output = data.output
          if (data.status !== undefined) existing.status = data.status
        } else {
          // New tool call — start timeout as safety net
          const newBlock = { type: 'tool_use', name: data.name, id: data.id, input: data.input || {}, done: false, output: data.output || '', status: data.status || '' }
          blocks.push(newBlock)
          // PermissionApproval blocks wait for user interaction — don't timeout
          if (data.name !== 'PermissionApproval') {
            const timer = setTimeout(() => {
              if (!newBlock.done) {
                console.warn(`tool_use block ${data.id} timed out without 'done', marking as done`)
                newBlock.done = true
                onRenderNeeded()
              }
              toolUseTimeouts.delete(data.id)
            }, TOOL_USE_TIMEOUT_MS)
            toolUseTimeouts.set(data.id, timer)
          }
        }
      }
      // Skip scroll when panel not visible
      if (isOpen.value) {
        onScrollBottom()
      }
    })

    eventSource.addEventListener('tool_result', (e) => {
      if (!guard()) return // Check guard first to prevent stale events corrupting new session (ISS-304)
      resetStreamTimeout()
      let data: any
      try { data = JSON.parse(e.data) } catch { console.warn('SSE tool_result: invalid JSON, skipping'); return }
      const blocks = streamingMsg.blocks
      // Find the matching tool_use block and update output/status/done
      const existing = blocks.find(b => b.type === 'tool_use' && b.id === data.id)
      if (existing) {
        // Update input if provided (ACP tool_call_update completed events
        // may carry rawInput that was missing from earlier tool_use events)
        if (data.input && Object.keys(data.input).length > 0) {
          existing.input = data.input
        }
        if (data.name) existing.name = data.name
        if (data.output !== undefined) existing.output = data.output
        if (data.status !== undefined) existing.status = data.status
        existing.done = true
      }
      // Clear timeout if set
      const timer = toolUseTimeouts.get(data.id)
      if (timer) { clearTimeout(timer); toolUseTimeouts.delete(data.id) }
      onRenderNeeded()
      // Skip scroll when panel not visible
      if (isOpen.value) {
        onScrollBottom()
      }
    })

    eventSource.addEventListener('metadata', (e) => {
      if (!guard()) return
      resetStreamTimeout()
      let data: any
      try { data = JSON.parse(e.data) } catch { console.warn('SSE metadata: invalid JSON, skipping'); return }
      streamingMsg.metadata = data
    })

    eventSource.addEventListener('done', () => {
      // ISS-246: check guard() BEFORE touching shared state — if session
      // changed, this event belongs to a stale connection and must be ignored.
      if (!guard()) {
        // Close only the stale EventSource that fired this event, not the
        // shared `eventSource` variable which may point to a new session.
        esRef.close()
        reconnect.reset()
        return
      }
      if (streamTimeout) { clearTimeout(streamTimeout); streamTimeout = null }
      clearToolUseTimeouts()
      disconnectStream()
      reconnect.reset() // Stream completed — reset reconnect state for future sessions
      // Reload from DB to ensure complete content — SSE events may have been
      // dropped during transmission, so the local state may be incomplete.
      onLoadHistory().finally(() => {
        loading.value = false
        onMessage()
        // Only scroll when panel is visible; loadHistory on
        // re-activate will handle the refresh
        if (isOpen.value) {
          onScrollBottom(true)
        }
        onStreamEnd?.('done')
        if (!isOpen.value) {
          const lastMsg = messages.value[messages.value.length - 1]
          if (lastMsg?.role === 'assistant') {
            onToast(gt('chat.stream.aiReplied'), { icon: '🤖', duration: 5000, onClick: () => onOpen() })
            onNotification(gt('chat.stream.aiReplied'), {
              body: gt('chat.stream.clickToViewReply'),
              onClick: () => onOpen()
            })
          }
        }
      })
    })

    eventSource.addEventListener('cancelled', () => {
      if (streamTimeout) { clearTimeout(streamTimeout); streamTimeout = null }
      // ISS-245/ISS-278: check guard() BEFORE disconnectStream().
      // disconnectStream() closes the shared `eventSource` variable which may
      // have been reassigned to a new session's EventSource. If the session
      // changed, this cancelled event is stale — close only THIS EventSource
      // instance (not the shared variable) and skip state mutations.
      if (!guard()) {
        // Close only the stale EventSource that fired this event, not the
        // shared `eventSource` variable which may point to a new session.
        esRef.close()
        return
      }
      disconnectStream()
      streamingMsg.cancelled = true
      // If no content was received, add error block so the UI shows the error card instead of loading dots
      if ((!streamingMsg.blocks || streamingMsg.blocks.length === 0) && !streamingMsg.content) {
        streamingMsg.blocks = [{ type: 'error', text: gt('chat.stream.userCancelled') }]
      }
      _forceCleanupStreamingState(messages.value, { onRenderNeeded, onExtractScheduledTasks })
      loading.value = false
      onStreamEnd?.('cancelled')
    })

    eventSource.addEventListener('warning', (e) => {
      if (!guard()) return
      resetStreamTimeout()
      let data: any
      try { data = JSON.parse(e.data) } catch { console.warn('SSE warning: invalid JSON, skipping'); return }
      // Flush any streaming text before adding warning block
      if (streamingMsg.streamingText) {
        streamingMsg.blocks.push({ type: 'text', text: streamingMsg.streamingText })
        streamingMsg.streamingText = ''
      }
      const warningBlock = { type: 'warning', text: data.text }
      if (data.reason) warningBlock.reason = data.reason
      streamingMsg.blocks.push(warningBlock)
      // Skip render when panel not visible — data is accumulated regardless
      if (isOpen.value) {
        onRenderNeeded()
      }
    })

    eventSource.addEventListener('mode_update', (e) => {
      if (!guard()) return
      let data: any
      try { data = JSON.parse(e.data) } catch { console.warn('SSE mode_update: invalid JSON, skipping'); return }
      // Agent mode changes take priority over user manual selection.
      // Update both currentModeId and availableModes from the SSE event.
      // Backend validates the mode is in availableModes before forwarding.
      if (data.currentModeId || data.availableModes?.length > 0) {
        updateModeState(data.currentModeId || '', data.availableModes || [])
      }
    })

    eventSource.addEventListener('config_update', (e) => {
      if (!guard()) return
      let data: any
      try { data = JSON.parse(e.data) } catch { console.warn('SSE config_update: invalid JSON, skipping'); return }
      // Process each config option by category
      for (const opt of (data.options || [])) {
        if (opt.category === 'mode' || opt.id === 'mode') {
          const modes = (opt.values || []).map((v: any) => ({ id: v.id, name: v.name || v.id }))
          // Agent mode changes take priority over user manual selection.
          // Update both currentModeId and availableModes from the SSE event.
          // Backend validates the mode is in availableModes before forwarding.
          const currentModeId = data.currentValueId || ''
          if (currentModeId || modes.length > 0) {
            updateModeState(currentModeId, modes)
          }
        }
      }
    })

    eventSource.addEventListener('thinking_effort_update', (e) => {
      if (!guard()) return
      let data: any
      try { data = JSON.parse(e.data) } catch { console.warn('SSE thinking_effort_update: invalid JSON, skipping'); return }
      // Only update available levels; currentId is managed by user action + DB
      if (data.availableLevels?.length > 0) {
        const levels = (data.availableLevels || []).map((l: any) => ({ id: l.id, name: l.name || l.id }))
        updateAvailableThinkingEfforts(levels)
      }
    })

    eventSource.addEventListener('commands_update', (e) => {
      if (!guard()) return
      let data: any
      try { data = JSON.parse(e.data) } catch { console.warn('SSE commands_update: invalid JSON, skipping'); return }
      if (Array.isArray(data.commands)) {
        updateCommandState(data.commands)
      }
    })

    eventSource.addEventListener('model_list_update', (e) => {
      if (!guard()) return
      let data: any
      try { data = JSON.parse(e.data) } catch { console.warn('SSE model_list_update: invalid JSON, skipping'); return }
      // Only update available models; currentModelId is managed by user action + DB
      if (Array.isArray(data.models) && data.models.length > 0) {
        const aid = currentAgentId.value
        if (aid) {
          updateACPModelList(aid, data.models)
        }
      }
    })

    eventSource.addEventListener('plan_update', (e) => {
      if (!guard()) return
      let data: any
      try { data = JSON.parse(e.data) } catch { console.warn('SSE plan_update: invalid JSON, skipping'); return }
      if (Array.isArray(data.entries)) {
        updatePlanEntries(data.entries)
      }
    })

    eventSource.addEventListener('queue_consume', (e) => {
      resetStreamTimeout()
      // Always update pending queue — it's independent of the streaming message
      onQueueConsume?.()
      if (!guard()) return
      let data: any
      try { data = JSON.parse(e.data) } catch { console.warn('SSE queue_consume: invalid JSON, skipping'); return }

      // Add user message bubble (DB message already persisted by backend).
      // Deduplicate: if a local user message with the same content already exists
      // (e.g. from sendMessageNow's optimistic push when data.running was true),
      // skip pushing a duplicate. The existing local message will be replaced by
      // the DB version when onLoadHistory runs after the final 'done' event.
      const userContent = data.text || ''
      const userFiles = (data.files || []).map(p => ({ path: p }))
      const existingUserMsg = messages.value.find(
        (m: any) => m.role === 'user' && m.content === userContent && !m.id
      )
      if (!existingUserMsg) {
        messages.value.push({
          role: 'user',
          content: userContent,
          blocks: userContent ? [{ type: 'text', text: userContent }] : [],
          files: userFiles,
          createdAt: new Date().toISOString(),
        })
      }

      // Create new streaming assistant placeholder
      messages.value.push({
        role: 'assistant',
        content: '',
        blocks: [],
        streaming: true,
        createdAt: new Date().toISOString(),
        backend: currentBackend.value,
      })
      // Re-acquire from the reactive array so mutations go through
      // Vue's reactive proxy (see connectStream for the same pattern)
      streamingMsg = messages.value[messages.value.length - 1]

      // Skip render/scroll when panel not visible
      if (isOpen.value) {
        onRenderNeeded()
        // Force scroll: queue_done removes the streaming indicator which shrinks layout,
        // making isAtBottom=false even though the user is visually at the bottom.
        // Since new messages are being injected, always scroll to show them.
        onScrollBottom(true)
      }
    })

    eventSource.addEventListener('queue_update', (e) => {
      resetStreamTimeout()
      let data: any
      try { data = JSON.parse(e.data) } catch { console.warn('SSE queue_update: invalid JSON, skipping'); return }
      // Always update pending queue — it's independent of the streaming message
      onQueueUpdate?.(data.queue || [])
      if (!guard()) return // Check guard after queue update to prevent stale events corrupting new session (ISS-304)
    })

    eventSource.addEventListener('queue_done', () => {
      if (!guard()) return
      resetStreamTimeout()
      // Current streaming message is finalized — clear loading state
      // before the next queued message starts (queue_consume)
      _forceCleanupStreamingState(messages.value, { onRenderNeeded, onExtractScheduledTasks })
      // Skip scroll when panel not visible
      if (isOpen.value) {
        // Re-sync scroll position: removing the streaming indicator and pending
        // messages shrinks the layout, which can make isAtBottom=false even when
        // the user is visually at the bottom. Scroll to ensure isAtBottom stays
        // accurate before queue_consume arrives.
        onScrollBottom()
      }
    })

    eventSource.addEventListener('error', (e) => {
      if (streamTimeout) { clearTimeout(streamTimeout); streamTimeout = null }
      if (!guard()) return
      // Mark this connection as terminated by a server-sent error event.
      // Without this flag, onerror (which fires immediately after) would see
      // loading=true and attempt reconnect or pollUntilDone, creating duplicate
      // messages or stuck loading states for already-resolved sessions.
      sseErrorHandled = true
      disconnectStream()
      // Check if this is an sse_busy error — another client is already consuming
      // the SSE stream. In this case, do NOT call onLoadHistory() because:
      // 1. loadHistory() sees data.running=true → sets loading=true → calls
      //    connectStream() again → gets sse_busy again → infinite loop
      // 2. The competing onLoadHistory() and onerror's forceCleanupStreamingState()
      //    cause loading to oscillate between true/false → cancel button flickers
      // Instead, let onerror handle the fallback to pollUntilDone() which reads
      // from DB incrementally without re-attempting SSE.
      let errorData: any
      try { errorData = JSON.parse(e.data) } catch { /* ignore parse failure */ }
      if (errorData?.reason === 'sse_busy') {
        // sse_busy is expected for second clients — skip onLoadHistory to avoid
        // the reconnection loop. onerror will fall back to polling.
        // Reset the flag so onerror can handle the fallback.
        sseErrorHandled = false
        return
      }
      // Non-sse_busy errors (e.g. session not running) — reload from DB for final state
      onLoadHistory().catch(() => {
        if (!guard()) return
        const errorBlock = { type: 'error', text: errorData?.error || 'Unknown error' }
        if (errorData?.reason) errorBlock.reason = errorData.reason
        streamingMsg.blocks = [errorBlock]
        _forceCleanupStreamingState(messages.value, { onRenderNeeded, onExtractScheduledTasks })
        loading.value = false
      })
      onStreamEnd?.('error')
    })

    // Flag to coordinate between the SSE 'error' named event and onerror.
    // When the server sends `event: error\ndata: ...`, both the addEventListener('error')
    // handler and onerror fire. The flag lets onerror know the error was already handled.
    let sseErrorHandled = false

    eventSource.onerror = () => {
      // SSE connection error — distinguish recoverable vs non-recoverable.
      // ISS-248/ISS-279: Use EventSource readyState to detect fatal errors.
      // CONNECTING (0) / OPEN (1) = transient, safe to reconnect.
      // CLOSED (2) = permanent failure (e.g. 404, server shutdown), fall back.
      if (streamTimeout) { clearTimeout(streamTimeout); streamTimeout = null }
      // If the EventSource was closed by cleanupActiveStream (session switch),
      // this onerror is from a stale connection — ignore it entirely to avoid
      // setting loading=true or scheduling reconnects for the new session.
      if (disconnectedByCleanup) {
        disconnectedByCleanup = false
        return
      }
      // If the SSE 'error' named event handler already processed this (e.g.
      // server sent `event: error\ndata: {"error":"SessionNotRunning"}`),
      // skip all reconnect/polling logic — the session is already finalized.
      if (sseErrorHandled) {
        sseErrorHandled = false
        disconnectStream()
        reconnect.reset()
        return
      }
      // Use esRef (captured at connectStream time) to check readyState,
      // since `eventSource` may have been reassigned by a new session.
      const wasRecoverable = esRef.readyState !== EventSource.CLOSED
      disconnectStream()
      if (wasRecoverable && currentSessionId.value && loading.value && reconnect.shouldReconnect()) {
        // Transient error (network blip, server restart) — reconnect SSE
        reconnect.scheduleReconnect()
      } else {
        // Non-recoverable error (404, 403, server shutdown) or max retries —
        // fall back to polling which will detect the terminal state
        reconnect.reset() // Clear reconnect state before falling back to polling
        // Do NOT call forceCleanupStreamingState() here — it deletes the
        // streaming flag, which makes pollUntilDone's incremental update
        // unable to find the streaming message. Instead, keep the streaming
        // message alive and let pollUntilDone update it incrementally.
        // Only set loading=false momentarily — pollUntilDone will manage it.
        loading.value = true  // Keep loading true — session is still running
        pollUntilDone()
      }
    }
  }

  async function cancelStream() {
    if (!currentSessionId.value || !loading.value) return
    try {
      await cancelChat(currentSessionId.value)
      // Backend will send 'cancelled' SSE event which triggers onStreamEnd.
      // If the SSE connection is already dead, forceCleanup won't happen here —
      // the onerror handler or global polling will take over.
    } catch (err) {
      console.error('Failed to cancel:', err)
      // Force local state reset even if API call fails
      disconnectStream()
      forceCleanupStreamingState()
      onStreamEnd?.('cancelled')
    }
  }

  // Network recovery: when the browser regains connectivity after a temporary
  // loss (e.g., WiFi→cellular, tunnel), the SSE connection may be silently dead.
  // The 'online' event lets us reconnect immediately instead of waiting for timeout.
  function handleOnline() {
    if (!loading.value || !currentSessionId.value) return
    // Only reconnect if we have an active EventSource that might be stale
    if (eventSource) {
      console.info('Network recovered, reconnecting SSE stream')
      disconnectStream()
      // connectStream with isRetry=false will reset reconnect state
      connectStream(currentSessionId.value)
    }
  }
  window.addEventListener('online', handleOnline)

  // Visibility change: always close SSE and polling when going to background.
  // Mobile OS will throttle/kill background connections anyway, so keeping SSE
  // alive is a waste of resources. On foreground, ChatPanel's visibility handler
  // calls loadHistory which reconnects the stream if the session is still running.
  function handleStreamVisibility() {
    if (document.visibilityState === 'hidden') {
      disconnectStream()
      stopPolling()
    }
  }

  // Cleanup on unmount
  onMounted(() => {
    document.addEventListener('visibilitychange', handleStreamVisibility)
  })

  onUnmounted(() => {
    disconnectStream()
    stopPolling()
    clearToolUseTimeouts()
    window.removeEventListener('online', handleOnline)
    document.removeEventListener('visibilitychange', handleStreamVisibility)
  })

  return {
    connectStream,
    disconnectStream,
    cancelStream,
    stopPolling,
  }
}
