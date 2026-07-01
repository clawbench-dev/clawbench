import { onMounted, onUnmounted, type Ref } from 'vue'
import { cancelChat } from '@/utils/api'
import { appLog } from '@/utils/appLog'
import { useReconnect } from './useReconnect'
import { gt } from '@/composables/useLocale'
import { updateModeState, updateCommandState, updateAvailableThinkingEfforts, currentAgentId, updateUsageState } from './useSessionIdentity'
import { updateACPModelList } from './useAgents'
import { updatePlanEntries } from './usePlanProgress'
import { FILE_MODIFYING_TOOLS, findLastBlockOfType, forceCleanupStreamingState as _forceCleanupStreamingState, findStreamingMsg, drainQueueMessage } from '@/utils/chatStreamUtils.ts'

const TAG = 'ChatStream'

export interface UseChatStreamOptions {
  messages: Ref<any[]>
  currentSessionId: Ref<string>
  currentBackend: Ref<string>
  loading: Ref<boolean>
  pendingStore: ReturnType<typeof import('@/composables/usePendingStore').usePendingStore>
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
  onFileModified?: (filePath: string) => void
  onExtractScheduledTasks?: (msgs: any[]) => void
}

export function useChatStream(options: UseChatStreamOptions) {
  const {
    messages,
    currentSessionId,
    currentBackend,
    loading,
    pendingStore,
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
    onFileModified,
    onExtractScheduledTasks,
  } = options

  let eventSource: EventSource | null = null
  let streamTimeout: ReturnType<typeof setTimeout> | null = null
  let renderTimer: number | null = null
  let pollingInterval: number | null = null
  // Flag to indicate the EventSource was closed intentionally by cleanupActiveStream
  // (session switch), so the stale onerror handler should not schedule reconnects.
  let disconnectedByCleanup = false
  // Track tool_use timeout timers so we can clean them up
  const toolUseTimeouts: Map<string, ReturnType<typeof setTimeout>> = new Map()
  // Counter for assigning stable _key to thinking blocks during streaming
  let thinkingBlockCounter = 0

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
    const sm = findStreamingMsg(messages.value)
    if (!sm?.blocks) return false
    return sm.blocks.some(
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
      appLog.w(TAG, 'SSE stream timeout - no events received, reconnecting')
      // No SSE event received for too long — reconnect instead of killing the session
      disconnectStream()
      // The AI session continues on the backend; just reconnect SSE
      if (currentSessionId.value && loading.value && reconnect.shouldReconnect()) {
        reconnect.scheduleReconnect()
      } else {
        pollUntilDone()
      }
    }, timeoutMs)
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
    pollingInterval = window.setInterval(async () => {
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
            appLog.e(TAG, 'Polling: too many invalid JSON responses, giving up')
            throw new Error('Invalid JSON response')
          }
          appLog.e(TAG, 'Polling: invalid JSON response')
          return
        }
        // Parse messages from server response
        const latestMsgs = (data.messages || []).map((msg: any) => {
          if (msg.role === 'assistant') {
            const { blocks, metadata, cancelled } = onParseAssistantContent(msg.content)
            msg.blocks = blocks
            if (metadata) msg.metadata = metadata
            if (cancelled) msg.cancelled = cancelled
          } else if (msg.role === 'user' && !msg.blocks) {
            if (msg.content && msg.content.startsWith('{"blocks":')) {
              const { blocks } = onParseAssistantContent(msg.content)
              msg.blocks = blocks
            } else {
              msg.blocks = msg.content ? [{ type: 'text', text: msg.content }] : []
            }
          }
          return msg
        })

        if (!data.running) {
          stopPolling()
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
        // Session still running — incremental update
        const lastAssistant = latestMsgs.findLast((m: any) => m.role === 'assistant')
        const existingStreaming = findStreamingMsg(messages.value)

        if (lastAssistant && existingStreaming) {
          existingStreaming.blocks = lastAssistant.blocks
          if (lastAssistant.metadata) existingStreaming.metadata = lastAssistant.metadata
          if (lastAssistant.cancelled) existingStreaming.cancelled = lastAssistant.cancelled
        } else if (lastAssistant && !existingStreaming) {
          const existingById = lastAssistant.id
            ? messages.value.find((m: any) => m.id === lastAssistant.id)
            : null
          if (existingById) {
            existingById.streaming = true
            existingById.blocks = lastAssistant.blocks
            if (lastAssistant.metadata) existingById.metadata = lastAssistant.metadata
            if (lastAssistant.cancelled) existingById.cancelled = lastAssistant.cancelled
          } else {
            lastAssistant.streaming = true
            messages.value.push(lastAssistant)
          }
        }

        currentSessionId.value = data.sessionId || currentSessionId.value
        if (isOpen.value) {
          debouncedRender()
        }
      } catch (err) {
        appLog.e(TAG, 'Polling error:', err)
        stopPolling()
        const sm = findStreamingMsg(messages.value)
        if (sm) {
          const hasContent = sm.content || (sm.blocks && sm.blocks.length > 0)
          if (hasContent) {
            delete sm.streaming
          } else {
            const idx = messages.value.indexOf(sm)
            if (idx !== -1) messages.value.splice(idx, 1)
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
    disconnectedByCleanup = false
    if (!isRetry) {
      reconnect.reset()
    }

    // Ensure a streaming assistant message exists — create one if needed
    if (!findStreamingMsg(messages.value)) {
      messages.value.push({
        role: 'assistant',
        content: '',
        blocks: [],
        streaming: true,
        createdAt: new Date().toISOString(),
        backend: currentBackend.value
      })
      thinkingBlockCounter = 0
      onRenderNeeded()
    }
    onScrollBottom()

    eventSource = new EventSource(`/api/ai/chat/stream?session_id=${encodeURIComponent(sessionId)}`, { withCredentials: true })

    // Capture reference to THIS EventSource instance so event handlers can
    // safely close only the stale connection without affecting a new session's
    // EventSource (the `eventSource` variable may be reassigned by connectStream).
    const esRef = eventSource

    // Session guard: check if the session has changed since this connection was opened.
    // Simpler than the old guard() — no need to check streamingMsg references.
    const sessionChanged = () => currentSessionId.value !== sessionId

    // Start stream timeout
    resetStreamTimeout()

    // Receive streaming message ID from backend for tool call detail API queries
    eventSource.addEventListener('stream_start', (e) => {
      if (sessionChanged()) return
      let data
      try { data = JSON.parse(e.data) } catch { return }
      const sm = findStreamingMsg(messages.value)
      if (sm && data.message_id) {
        sm.id = data.message_id
      }
    })

    eventSource.addEventListener('resume_split', (e) => {
      if (sessionChanged()) return
      const sm = findStreamingMsg(messages.value)
      if (!sm) return
      resetStreamTimeout()
      // Finalize Phase 1 message
      delete sm.streaming
      // Create Phase 2 streaming message
      const phase2 = {
        role: 'assistant',
        content: '',
        blocks: [],
        streaming: true,
        createdAt: new Date().toISOString(),
        backend: currentBackend.value
      }
      // Set the new streaming message ID from the resume_split event data
      let data
      try { data = JSON.parse(e.data) } catch { /* empty */ }
      if (data?.message_id) {
        (phase2 as any).id = data.message_id
      }
      messages.value.push(phase2)
      thinkingBlockCounter = 0
      onRenderNeeded()
      debouncedRender()
    })

    eventSource.addEventListener('content', (e) => {
      if (sessionChanged()) return
      const sm = findStreamingMsg(messages.value)
      if (!sm) return
      resetStreamTimeout()
      let data: any
      try { data = JSON.parse(e.data) } catch { appLog.w(TAG, 'SSE content: invalid JSON, skipping'); return }
      const blocks = sm.blocks
      const existingText = findLastBlockOfType(blocks, 'text')
      if (existingText) {
        existingText.text += data.content
      } else {
        blocks.push({ type: 'text', text: data.content })
      }
      debouncedRender()
    })

    eventSource.addEventListener('thinking', (e) => {
      if (sessionChanged()) return
      const sm = findStreamingMsg(messages.value)
      if (!sm) return
      resetStreamTimeout()
      let data: any
      try { data = JSON.parse(e.data) } catch { appLog.w(TAG, 'SSE thinking: invalid JSON, skipping'); return }
      const blocks = sm.blocks
      const existingThinking = findLastBlockOfType(blocks, 'thinking')
      if (existingThinking) {
        existingThinking.text += data.text
      } else {
        blocks.push({ type: 'thinking', text: data.text, _key: `thinking-${thinkingBlockCounter++}` })
      }
      debouncedRender()
      if (isOpen.value) {
        onScrollBottom()
      }
    })

    eventSource.addEventListener('thinking_done', () => {
      if (sessionChanged()) return
      const sm = findStreamingMsg(messages.value)
      if (!sm) return
      const blocks = sm.blocks
      for (let i = blocks.length - 1; i >= 0; i--) {
        if (blocks[i].type === 'thinking') {
          blocks[i].done = true
          break
        }
      }
      onRenderNeeded()
    })

    eventSource.addEventListener('tool_use', (e) => {
      if (sessionChanged()) return
      const sm = findStreamingMsg(messages.value)
      if (!sm) return
      resetStreamTimeout()
      let data: any
      try { data = JSON.parse(e.data) } catch { appLog.w(TAG, 'SSE tool_use: invalid JSON, skipping'); return }
      const blocks = sm.blocks
      const existing = blocks.find((b: any) => b.type === 'tool_use' && b.id === data.id)
      if (data.done) {
        if (existing) {
          // Slim SSE: only input present for interactive tools
          if (data.input && Object.keys(data.input).length > 0) {
            existing.input = data.input
          }
          existing.done = true
          if (data.status !== undefined) existing.status = data.status
          // Slim fields
          if (data.summary !== undefined) existing.summary = data.summary
          if (data.display_name !== undefined) existing.display_name = data.display_name
          if (data.file_path !== undefined) existing.file_path = data.file_path
        } else {
          // No existing block — create a new done tool_use block.
          // This happens when the backend sends tool_use with done=true
          // (e.g. Pi's toolcall_end provides complete arguments in one event).
          const newBlock: any = {
            type: 'tool_use', name: data.name, id: data.id, done: true,
            status: data.status || '',
          }
          if (data.input && Object.keys(data.input).length > 0) {
            newBlock.input = data.input
          }
          if (data.summary) newBlock.summary = data.summary
          if (data.display_name) newBlock.display_name = data.display_name
          if (data.file_path) newBlock.file_path = data.file_path
          blocks.push(newBlock)
        }
        const timer = toolUseTimeouts.get(data.id)
        if (timer) { clearTimeout(timer); toolUseTimeouts.delete(data.id) }

        // Use file_path from slim meta (no need to read input)
        if (FILE_MODIFYING_TOOLS.has(data.name) && onFileModified) {
          const filePath = data.file_path || existing?.file_path
          if (filePath) {
            onFileModified(filePath)
          }
        }
      } else {
        if (existing) {
          // Slim SSE: only input present for interactive tools
          if (data.input && Object.keys(data.input).length > 0) {
            existing.input = data.input
          }
          if (data.name) existing.name = data.name
          if (data.status !== undefined) existing.status = data.status
          // Slim fields
          if (data.summary !== undefined) existing.summary = data.summary
          if (data.display_name !== undefined) existing.display_name = data.display_name
          if (data.file_path !== undefined) existing.file_path = data.file_path
        } else {
          const newBlock: any = {
            type: 'tool_use', name: data.name, id: data.id, done: false,
            status: data.status || '',
          }
          // Slim SSE: only input present for interactive tools (AskUserQuestion, PermissionApproval)
          if (data.input && Object.keys(data.input).length > 0) {
            newBlock.input = data.input
          }
          // Slim fields
          if (data.summary) newBlock.summary = data.summary
          if (data.display_name) newBlock.display_name = data.display_name
          if (data.file_path) newBlock.file_path = data.file_path
          blocks.push(newBlock)
          if (data.name !== 'PermissionApproval') {
            const timer = setTimeout(() => {
              if (!newBlock.done) {
                appLog.w(TAG, `tool_use block ${data.id} timed out without 'done', marking as done`)
                newBlock.done = true
                onRenderNeeded()
              }
              toolUseTimeouts.delete(data.id)
            }, TOOL_USE_TIMEOUT_MS)
            toolUseTimeouts.set(data.id, timer)
          }
        }
      }
      if (isOpen.value) {
        onScrollBottom()
      }
    })

    eventSource.addEventListener('tool_result', (e) => {
      if (sessionChanged()) return
      const sm = findStreamingMsg(messages.value)
      if (!sm) return
      resetStreamTimeout()
      let data: any
      try { data = JSON.parse(e.data) } catch { appLog.w(TAG, 'SSE tool_result: invalid JSON, skipping'); return }
      const blocks = sm.blocks
      const existing = blocks.find((b: any) => b.type === 'tool_use' && b.id === data.id)
      if (existing) {
        // Slim SSE: no input/output in tool_result events
        if (data.name) existing.name = data.name
        if (data.status !== undefined) existing.status = data.status
        existing.done = true
      }
      const timer = toolUseTimeouts.get(data.id)
      if (timer) { clearTimeout(timer); toolUseTimeouts.delete(data.id) }
      onRenderNeeded()
      if (isOpen.value) {
        onScrollBottom()
      }
    })

    eventSource.addEventListener('metadata', (e) => {
      if (sessionChanged()) return
      const sm = findStreamingMsg(messages.value)
      if (!sm) return
      resetStreamTimeout()
      let data: any
      try { data = JSON.parse(e.data) } catch { appLog.w(TAG, 'SSE metadata: invalid JSON, skipping'); return }
      sm.metadata = data
    })

    eventSource.addEventListener('done', () => {
      if (sessionChanged()) {
        esRef.close()
        reconnect.reset()
        return
      }
      if (streamTimeout) { clearTimeout(streamTimeout); streamTimeout = null }
      clearToolUseTimeouts()
      thinkingBlockCounter = 0

      // Finalize streaming state BEFORE loadHistory replaces the array.
      // This ensures: (1) streaming flag removed immediately (no stuck "three dots"),
      // (2) unfinished tool_use blocks marked done (no stuck spinners),
      // (3) if loadHistory is slow/fails, UI is already in a clean state.
      _forceCleanupStreamingState(messages.value, { onRenderNeeded, onExtractScheduledTasks })

      // Diagnostic: log message state when done event received
      const doneSummary = messages.value.map((m: any, i: number) =>
        `[${i}] ${m.role}${m.id ? ` id=${m.id}` : ''}${m.streaming ? ' STREAMING' : ''} content="${(m.content || '').slice(0, 30)}" blocks=${m.blocks?.length || 0}`
      ).join(' | ')
      const pendingCount = pendingStore.getPending(currentSessionId.value).length
      appLog.d(TAG, `[done] pendingStore has ${pendingCount} item(s); messages: ${doneSummary}`)

      disconnectStream()
      reconnect.reset()
      onLoadHistory().finally(() => {
        loading.value = false
        onMessage()
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
      if (sessionChanged()) {
        esRef.close()
        return
      }
      const sm = findStreamingMsg(messages.value)
      if (!sm) return
      disconnectStream()
      sm.cancelled = true
      if ((!sm.blocks || sm.blocks.length === 0) && !sm.content) {
        sm.blocks = [{ type: 'error', text: gt('chat.stream.userCancelled') }]
      }
      _forceCleanupStreamingState(messages.value, { onRenderNeeded, onExtractScheduledTasks })
      loading.value = false
      onStreamEnd?.('cancelled')
    })

    eventSource.addEventListener('warning', (e) => {
      if (sessionChanged()) return
      const sm = findStreamingMsg(messages.value)
      if (!sm) return
      resetStreamTimeout()
      let data: any
      try { data = JSON.parse(e.data) } catch { appLog.w(TAG, 'SSE warning: invalid JSON, skipping'); return }
      if (sm.streamingText) {
        sm.blocks.push({ type: 'text', text: sm.streamingText })
        sm.streamingText = ''
      }
      const warningBlock: any = { type: 'warning', text: data.text }
      if (data.reason) warningBlock.reason = data.reason
      sm.blocks.push(warningBlock)
      if (isOpen.value) {
        onRenderNeeded()
      }
    })

    eventSource.addEventListener('mode_update', (e) => {
      if (sessionChanged()) return
      let data: any
      try { data = JSON.parse(e.data) } catch { appLog.w(TAG, 'SSE mode_update: invalid JSON, skipping'); return }
      if (data.currentModeId || data.availableModes?.length > 0) {
        updateModeState(data.currentModeId || '', data.availableModes || [])
      }
    })

    eventSource.addEventListener('config_update', (e) => {
      if (sessionChanged()) return
      let data: any
      try { data = JSON.parse(e.data) } catch { appLog.w(TAG, 'SSE config_update: invalid JSON, skipping'); return }
      for (const opt of (data.options || [])) {
        if (opt.category === 'mode' || opt.id === 'mode') {
          const modes = (opt.values || []).map((v: any) => ({ id: v.id, name: v.name || v.id }))
          const currentModeId = data.currentValueId || ''
          if (currentModeId || modes.length > 0) {
            updateModeState(currentModeId, modes)
          }
        }
      }
    })

    eventSource.addEventListener('thinking_effort_update', (e) => {
      if (sessionChanged()) return
      let data: any
      try { data = JSON.parse(e.data) } catch { appLog.w(TAG, 'SSE thinking_effort_update: invalid JSON, skipping'); return }
      if (data.availableLevels?.length > 0) {
        const levels = (data.availableLevels || []).map((l: any) => ({ id: l.id, name: l.name || l.id }))
        updateAvailableThinkingEfforts(levels)
      }
    })

    eventSource.addEventListener('commands_update', (e) => {
      if (sessionChanged()) return
      let data: any
      try { data = JSON.parse(e.data) } catch { appLog.w(TAG, 'SSE commands_update: invalid JSON, skipping'); return }
      if (Array.isArray(data.commands)) {
        updateCommandState(data.commands)
      }
    })

    eventSource.addEventListener('model_list_update', (e) => {
      if (sessionChanged()) return
      let data: any
      try { data = JSON.parse(e.data) } catch { appLog.w(TAG, 'SSE model_list_update: invalid JSON, skipping'); return }
      if (Array.isArray(data.models) && data.models.length > 0) {
        const aid = currentAgentId.value
        if (aid) {
          updateACPModelList(aid, data.models)
        }
      }
    })

    eventSource.addEventListener('plan_update', (e) => {
      if (sessionChanged()) return
      let data: any
      try { data = JSON.parse(e.data) } catch { appLog.w(TAG, 'SSE plan_update: invalid JSON, skipping'); return }
      if (Array.isArray(data.entries)) {
        updatePlanEntries(data.entries)
      }
    })

    eventSource.addEventListener('usage_update', (e) => {
      if (sessionChanged()) return
      let data: any
      try { data = JSON.parse(e.data) } catch { appLog.w(TAG, 'SSE usage_update: invalid JSON, skipping'); return }
      if (data.size > 0) {
        updateUsageState(data.used ?? 0, data.size, data.cost, data.currency)
      }
    })

    // ── Queue drain — atomic replacement for old queue_done + queue_consume ──
    // Single event that atomically: finalizes current streaming, creates new
    // streaming placeholder. Pending messages are handled by pendingStore.
    eventSource.addEventListener('queue_drain', (e) => {
      resetStreamTimeout()
      let data: any
      try { data = JSON.parse(e.data) } catch { appLog.w(TAG, 'SSE queue_drain: invalid JSON, skipping'); return }

      // Sync pendingStore FIRST — remove the drained message from pending before
      // pushing the drain message into messages.value. This ordering ensures
      // renderedMessages computed never sees the same message in both sources
      // (drain + pending), preventing a brief duplicate flash.
      // Always update pendingStore — this is per-session so no cross-session contamination.
      pendingStore.syncFromBackendQueue(sessionId, data.queue || [])

      if (!sessionChanged()) {
        const drainText = data.text || ''
        const drainFiles = [...(data.filePaths || []), ...(data.files || [])]
        drainQueueMessage(
          messages.value, drainText, drainFiles, currentBackend.value,
          { onRenderNeeded, onExtractScheduledTasks },
          undefined,
          data.messageId || undefined
        )

        if (isOpen.value) {
          onRenderNeeded()
          onScrollBottom(true)
        }
      }
    })

    // queue_update: sent when a new message is enqueued while a session is running.
    // Syncs pendingStore with the authoritative backend queue state.
    eventSource.addEventListener('queue_update', (e) => {
      resetStreamTimeout()
      let data: any
      try { data = JSON.parse(e.data) } catch { appLog.w(TAG, 'SSE queue_update: invalid JSON, skipping'); return }

      // Always update pendingStore — per-session, no contamination possible.
      pendingStore.syncFromBackendQueue(sessionId, data.queue || [])

      if (sessionChanged()) return

      // Trigger render when pending messages are added/removed for the current session
      onRenderNeeded()
    })

    eventSource.addEventListener('error', (e) => {
      if (streamTimeout) { clearTimeout(streamTimeout); streamTimeout = null }
      if (sessionChanged()) return
      // Mark this connection as terminated by a server-sent error event.
      sseErrorHandled = true
      disconnectStream()
      let errorData: any
      try { errorData = JSON.parse((e as MessageEvent).data) } catch { /* ignore parse failure */ }
      if (errorData?.reason === 'sse_busy') {
        sseErrorHandled = false
        return
      }
      // Non-sse_busy errors — reload from DB for final state
      onLoadHistory().catch(() => {
        if (sessionChanged()) return
        const sm = findStreamingMsg(messages.value)
        if (sm) {
          const errorBlock: any = { type: 'error', text: errorData?.error || 'Unknown error' }
          if (errorData?.reason) errorBlock.reason = errorData.reason
          sm.blocks = [errorBlock]
        }
        _forceCleanupStreamingState(messages.value, { onRenderNeeded, onExtractScheduledTasks })
        loading.value = false
      })
      onStreamEnd?.('error')
    })

    // Flag to coordinate between the SSE 'error' named event and onerror.
    let sseErrorHandled = false

    eventSource.onerror = () => {
      if (streamTimeout) { clearTimeout(streamTimeout); streamTimeout = null }
      if (disconnectedByCleanup) {
        disconnectedByCleanup = false
        return
      }
      if (sseErrorHandled) {
        sseErrorHandled = false
        disconnectStream()
        reconnect.reset()
        return
      }
      const wasRecoverable = esRef.readyState !== EventSource.CLOSED
      disconnectStream()
      if (wasRecoverable && currentSessionId.value && loading.value && reconnect.shouldReconnect()) {
        reconnect.scheduleReconnect()
      } else {
        reconnect.reset()
        loading.value = true  // Keep loading true — session is still running
        pollUntilDone()
      }
    }
  }

  async function cancelStream() {
    if (!currentSessionId.value || !loading.value) return
    try {
      await cancelChat(currentSessionId.value)
    } catch (err) {
      appLog.e(TAG, 'Failed to cancel:', err)
      disconnectStream()
      forceCleanupStreamingState()
      onStreamEnd?.('cancelled')
    }
  }

  function handleOnline() {
    if (!loading.value || !currentSessionId.value) return
    if (eventSource) {
      appLog.i(TAG, 'Network recovered, reconnecting SSE stream')
      disconnectStream()
      connectStream(currentSessionId.value)
    }
  }
  window.addEventListener('online', handleOnline)

  function handleStreamVisibility() {
    if (document.visibilityState === 'hidden') {
      disconnectStream()
      stopPolling()
    }
  }

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
