import { onMounted, onUnmounted, type Ref } from 'vue'
import { cancelChat } from '@/utils/api'
import { appLog } from '@/utils/appLog'
import { useReconnect } from './useReconnect'
import { gt } from '@/composables/useLocale'
import { updateModeState, updateCommandState, updateAvailableThinkingEfforts, currentAgentId, updateUsageState } from './useSessionIdentity'
import { updateACPModelList } from './useAgents'
import { updatePlanEntries } from './usePlanProgress'
import { FILE_MODIFYING_TOOLS, findLastBlockOfType, forceCleanupStreamingState as _forceCleanupStreamingState, findStreamingMsg, drainQueueMessage, mergeStreamingAssistantBlocks, isLocalOptimisticUserMessage, findLastIndexCompat, findLastCompat } from '@/utils/chatStreamUtils.ts'
import { isAndroidAppMode, postWebDiagLog } from '@/utils/androidNetwork.ts'

const TAG = 'ChatStream'

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
    onFileModified,
    onExtractScheduledTasks,
  } = options

  let eventSource: EventSource | null = null
  let streamTimeout: ReturnType<typeof setTimeout> | null = null
  let renderTimer: number | null = null
  let pollingInterval: number | null = null
  /** Bumps on every stopPolling — stale in-flight poll ticks must ignore results. */
  let pollGeneration = 0
  let pollLoopActive = false
  /** Session id captured at poll start — survives brief identity ref gaps on Android. */
  let pollSessionId = ''
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
  const POLL_HISTORY_LIMIT = 12 // Newest row may be user message when limit=1
  const POLL_INTERVAL_MS = 2000
  const POLL_INTERVAL_ANDROID_MS = 800
  const POLL_FETCH_TIMEOUT_MS = 8000
  const VISIBILITY_HIDDEN_DEBOUNCE_MS = 400 // Ignore keyboard flicker on Android

  // Secondary client (sse_busy / instant SSE close): poll DB instead of SSE reconnect loop.
  let preferPollingOnly = false
  let visibilityHiddenTimer: ReturnType<typeof setTimeout> | null = null
  let outgoingSendInFlight = false

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
      if (preferPollingOnly) {
        pollUntilDone()
        return
      }
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
    pollGeneration++
    pollLoopActive = false
    pollSessionId = ''
    if (pollingInterval) { clearInterval(pollingInterval); pollingInterval = null }
  }

  function activePollSessionId(): string {
    return currentSessionId.value || pollSessionId
  }

  function clearVisibilityHiddenTimer() {
    if (visibilityHiddenTimer) {
      clearTimeout(visibilityHiddenTimer)
      visibilityHiddenTimer = null
    }
  }

  function pollDelayMs() {
    return isAndroidAppMode() ? POLL_INTERVAL_ANDROID_MS : POLL_INTERVAL_MS
  }

  /** Ensure a local streaming assistant placeholder exists for dots / poll merge target. */
  function ensureStreamingPlaceholder() {
    const existingStreaming = findStreamingMsg(messages.value)
    if (!existingStreaming) {
      const newStreaming = {
        role: 'assistant' as const,
        id: `drain-${Date.now()}-${Math.random().toString(36).slice(2, 8)}`,
        content: '',
        blocks: [] as any[],
        streaming: true,
        createdAt: new Date().toISOString(),
        backend: currentBackend.value,
      }
      const lastUserIdx = findLastIndexCompat(
        messages.value,
        (m: any) => m.role === 'user' && !m.pending
      )
      if (lastUserIdx !== -1) {
        messages.value.splice(lastUserIdx + 1, 0, newStreaming)
      } else {
        messages.value.push(newStreaming)
      }
      thinkingBlockCounter = 0
      // Force array reassignment — Android WebView may not paint deep mutations on send.
      messages.value = [...messages.value]
      onRenderNeeded(true)
    } else if ((existingStreaming as any).fromDB) {
      delete (existingStreaming as any).fromDB
    }
    onScrollBottom(true)
  }

  function enterPollPrimaryMode(reason: string) {
    appLog.d(TAG, `Poll-primary mode (${reason})`)
    preferPollingOnly = true
    reconnect.reset()
    disconnectStream()
    ensureStreamingPlaceholder()
    pollUntilDone()
  }

  /** DB poll while POST is in flight — desktop only. Android WebView starves
   *  concurrent fetch/EventSource when a poll starts before POST completes. */
  function ensureOutboundPoll() {
    if (isAndroidAppMode()) {
      appLog.d(TAG, 'ensureOutboundPoll skipped on Android (post-first)')
      return
    }
    if (!loading.value || !currentSessionId.value) {
      appLog.w(TAG, `ensureOutboundPoll skipped loading=${loading.value} sid=${!!currentSessionId.value}`)
      return
    }
    ensureStreamingPlaceholder()
    pollUntilDone()
  }

  async function recoverFromPollingFailures() {
    try {
      await onLoadHistory()
      if (loading.value && currentSessionId.value) {
        appLog.w(TAG, 'Polling failed but session still running — resuming poll-primary')
        enterPollPrimaryMode('poll_recover')
        return
      }
    } catch {
      // fall through to error UI
    }
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

  async function pollOnce(
    counters: { jsonParseFailures: number; httpFailures: number },
    limits: { maxJsonParseFailures: number; maxHttpFailures: number },
    signal?: AbortSignal
  ): Promise<'continue' | 'done' | 'failed'> {
    try {
      const sid = activePollSessionId()
      appLog.d(TAG, `poll tick session=${sid?.slice(0, 8)} loading=${loading.value}`)
      // Match the working /api/ai/chat/count fetch style exactly (no custom
      // Cache-Control headers / credentials opts). Those extras correlated with
      // zero mid-turn GETs on Android WebView while count polls succeeded.
      const url =
        `/api/ai/chat?session_id=${encodeURIComponent(sid)}` +
        `&limit=${POLL_HISTORY_LIMIT}&_t=${Date.now()}`
      const resp = await fetch(url, signal ? { signal } : undefined)
      if (!resp.ok) {
        throw new Error(`HTTP ${resp.status}`)
      }
      counters.httpFailures = 0
      let data
      try {
        data = await resp.json()
        counters.jsonParseFailures = 0
      } catch {
        counters.jsonParseFailures++
        if (counters.jsonParseFailures >= limits.maxJsonParseFailures) {
          appLog.e(TAG, 'Polling: too many invalid JSON responses, giving up')
          throw new Error('Invalid JSON response')
        }
        appLog.e(TAG, 'Polling: invalid JSON response')
        return 'continue'
      }
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
        // POST may still be in flight — server has not marked session running yet.
        if (outgoingSendInFlight) {
          return 'continue'
        }
        const localStreaming = findStreamingMsg(messages.value)
        if (loading.value && localStreaming && !localStreaming.fromDB) {
          return 'continue'
        }
        onLoadHistory().finally(() => {
          loading.value = false
          preferPollingOnly = false
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
        return 'done'
      }

      const lastAssistant = findLastCompat(latestMsgs, (m: any) => m.role === 'assistant')
      const existingStreaming = findStreamingMsg(messages.value)

      if (lastAssistant && existingStreaming) {
        const pollBlocks = lastAssistant.blocks
        const prevLen = existingStreaming.blocks?.length || 0
        const merged = mergeStreamingAssistantBlocks(existingStreaming.blocks, pollBlocks)
        if (merged !== pollBlocks) {
          appLog.d(TAG, `[poll] raw-text fallback — preserving ${existingStreaming.blocks.filter((b: any) => b.type === 'thinking').length} thinking blocks`)
        }
        // New array ref so Android WebView / ContentBlocks pick up thinking chips.
        existingStreaming.blocks = [...merged]
        if (lastAssistant.id && !existingStreaming.fromDB) {
          existingStreaming.id = lastAssistant.id
        }
        if (lastAssistant.metadata) existingStreaming.metadata = lastAssistant.metadata
        if (lastAssistant.cancelled) existingStreaming.cancelled = lastAssistant.cancelled
        if (merged.length !== prevLen || merged !== pollBlocks) {
          messages.value = [...messages.value]
        }
      } else if (lastAssistant && !existingStreaming) {
        const existingById = lastAssistant.id
          ? messages.value.find((m: any) => m.id === lastAssistant.id)
          : null
        if (existingById) {
          existingById.streaming = true
          const pollBlocks = lastAssistant.blocks
          existingById.blocks = [...mergeStreamingAssistantBlocks(existingById.blocks || [], pollBlocks)]
          if (lastAssistant.metadata) existingById.metadata = lastAssistant.metadata
          if (lastAssistant.cancelled) existingById.cancelled = lastAssistant.cancelled
          messages.value = [...messages.value]
        } else {
          lastAssistant.streaming = true
          messages.value.push(lastAssistant)
          messages.value = [...messages.value]
        }
      }

      currentSessionId.value = data.sessionId || currentSessionId.value
      if (data.sessionId) pollSessionId = data.sessionId
      if (isOpen.value) {
        debouncedRender()
      } else {
        onRenderNeeded()
      }
      return 'continue'
    } catch (err: any) {
      if (err?.name === 'AbortError') {
        appLog.w(TAG, 'Polling: fetch timeout/abort')
        return 'continue'
      }
      appLog.e(TAG, 'Polling error:', err)
      counters.httpFailures++
      if (counters.httpFailures < limits.maxHttpFailures) {
        return 'continue'
      }
      appLog.e(TAG, 'Polling: too many HTTP failures, giving up')
      return 'failed'
    }
  }

  function pollUntilDone(explicitSessionId?: string) {
    // setInterval (not async while+await): Android WebView reliably fires
    // intervals (msg-count poll works) but concurrent await-fetch loops started
    // mid-POST often never reach the network until B/F.
    stopPolling()
    pollLoopActive = true
    pollSessionId = explicitSessionId || currentSessionId.value || ''
    const gen = pollGeneration
    const counters = { jsonParseFailures: 0, httpFailures: 0 }
    const limits = { maxJsonParseFailures: 5, maxHttpFailures: 5 }
    let tickInFlight = false
    appLog.i(TAG, `poll interval start gen=${gen} android=${isAndroidAppMode()} sid=${pollSessionId.slice(0, 8)} every=${pollDelayMs()}ms`)

    const runTick = async () => {
      if (tickInFlight) return
      if (gen !== pollGeneration || !pollLoopActive) return
      if (!activePollSessionId() || !loading.value) {
        appLog.w(TAG, `poll tick exit sid=${!!activePollSessionId()} loading=${loading.value}`)
        stopPolling()
        return
      }
      tickInFlight = true
      const ac = new AbortController()
      const abortTimer = window.setTimeout(() => ac.abort(), POLL_FETCH_TIMEOUT_MS)
      let result: 'continue' | 'done' | 'failed' = 'continue'
      try {
        result = await pollOnce(counters, limits, ac.signal)
      } catch (err: any) {
        if (err?.name === 'AbortError') {
          appLog.w(TAG, 'poll fetch aborted (timeout) — retrying')
          result = 'continue'
        } else {
          appLog.e(TAG, 'poll tick error:', err)
          result = 'continue'
        }
      } finally {
        clearTimeout(abortTimer)
        tickInFlight = false
      }
      if (gen !== pollGeneration || !pollLoopActive) return
      if (result === 'done') {
        stopPolling()
        return
      }
      if (result === 'failed') {
        stopPolling()
        await recoverFromPollingFailures()
      }
    }

    void runTick()
    pollingInterval = window.setInterval(() => { void runTick() }, pollDelayMs())
  }

  function setOutgoingSendInFlight(inFlight: boolean) {
    outgoingSendInFlight = inFlight
  }

  /**
   * Apply a WS chat_stream_update snapshot (throttled DB flush from server).
   * Primary live path for Android WebView where EventSource and poll GETs
   * never reach the server mid-turn, but /api/ai/events/ws stays connected.
   */
  function applyChatStreamUpdate(data: { session_id?: string; blocks?: any[] } | undefined) {
    try {
      if (!data?.session_id || data.session_id !== currentSessionId.value) {
        postWebDiagLog(TAG, `ws-stream skip sid mismatch got=${data?.session_id?.slice?.(0, 8)} cur=${currentSessionId.value?.slice?.(0, 8)}`, 'D')
        return
      }
      if (!Array.isArray(data.blocks)) {
        postWebDiagLog(TAG, `ws-stream skip blocks not array type=${typeof data.blocks}`, 'W')
        return
      }
      if (!loading.value && !findStreamingMsg(messages.value)) return

      ensureStreamingPlaceholder()
      const existing = findStreamingMsg(messages.value)
      if (!existing) {
        postWebDiagLog(TAG, 'ws-stream skip no streaming placeholder', 'W')
        return
      }

      const merged = mergeStreamingAssistantBlocks(existing.blocks || [], data.blocks)
      existing.blocks = [...merged]
      messages.value = [...messages.value]
      postWebDiagLog(TAG, `ws-stream applied blocks=${merged.length} thinking=${merged.filter((b: any) => b.type === 'thinking').length}`)
      if (isOpen.value) {
        debouncedRender()
      } else {
        onRenderNeeded()
      }
    } catch (err: any) {
      postWebDiagLog(TAG, `ws-stream FAIL ${err?.name || 'Error'}: ${err?.message || err}`, 'E')
      appLog.e(TAG, 'applyChatStreamUpdate failed', err)
    }
  }

  /**
   * Apply a /api/ai/chat poll JSON payload (used by Android count-timer direct fetch).
   * Same merge path as pollOnce after a successful GET.
   */
  function applyPollPayload(data: any): 'continue' | 'done' {
    if (!data) return 'continue'
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
      if (outgoingSendInFlight) return 'continue'
      const localStreaming = findStreamingMsg(messages.value)
      if (loading.value && localStreaming && !localStreaming.fromDB) return 'continue'
      onLoadHistory().finally(() => {
        loading.value = false
        preferPollingOnly = false
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
      return 'done'
    }

    ensureStreamingPlaceholder()
    const lastAssistant = findLastCompat(latestMsgs, (m: any) => m.role === 'assistant')
    const existingStreaming = findStreamingMsg(messages.value)

    if (lastAssistant && existingStreaming) {
      const pollBlocks = lastAssistant.blocks
      const prevLen = existingStreaming.blocks?.length || 0
      const merged = mergeStreamingAssistantBlocks(existingStreaming.blocks, pollBlocks)
      existingStreaming.blocks = [...merged]
      if (lastAssistant.id && !existingStreaming.fromDB) {
        existingStreaming.id = lastAssistant.id
      }
      if (lastAssistant.metadata) existingStreaming.metadata = lastAssistant.metadata
      if (lastAssistant.cancelled) existingStreaming.cancelled = lastAssistant.cancelled
      if (merged.length !== prevLen || merged !== pollBlocks) {
        messages.value = [...messages.value]
      }
    } else if (lastAssistant && !existingStreaming) {
      const existingById = lastAssistant.id
        ? messages.value.find((m: any) => m.id === lastAssistant.id)
        : null
      if (existingById) {
        existingById.streaming = true
        existingById.blocks = [...mergeStreamingAssistantBlocks(existingById.blocks || [], lastAssistant.blocks)]
        if (lastAssistant.metadata) existingById.metadata = lastAssistant.metadata
        if (lastAssistant.cancelled) existingById.cancelled = lastAssistant.cancelled
        messages.value = [...messages.value]
      } else {
        lastAssistant.streaming = true
        messages.value.push(lastAssistant)
        messages.value = [...messages.value]
      }
    }

    currentSessionId.value = data.sessionId || currentSessionId.value
    if (data.sessionId) pollSessionId = data.sessionId
    if (isOpen.value) {
      debouncedRender()
    } else {
      onRenderNeeded()
    }
    return 'continue'
  }

  function beginOutgoingTurn() {
    // Fresh turn: clear leftover poll-primary from a prior sse_busy.
    preferPollingOnly = false
    try {
      ensureStreamingPlaceholder()
    } catch (err: any) {
      postWebDiagLog(TAG, `beginOutgoingTurn FAIL ${err?.name || 'Error'}: ${err?.message || err}`, 'E')
      // Last-resort placeholder so turn-loading dots are replaced by a real streaming msg.
      if (!findStreamingMsg(messages.value)) {
        messages.value.push({
          role: 'assistant',
          id: `drain-${Date.now()}`,
          content: '',
          blocks: [],
          streaming: true,
          createdAt: new Date().toISOString(),
          backend: currentBackend.value,
        })
        messages.value = [...messages.value]
        onRenderNeeded(true)
      }
    }
  }

  function connectStream(sessionId: string, isRetry = false) {
    // Android WebView: EventSource often never hits the server during an active
    // turn (0 stream requests in logs). Use interval DB poll only — same timer
    // class as /api/ai/chat/count which does work on device.
    if (isAndroidAppMode()) {
      try {
        preferPollingOnly = true
        disconnectedByCleanup = false
        if (!isRetry) reconnect.reset()
        if (sessionId && !currentSessionId.value) {
          currentSessionId.value = sessionId
        }
        ensureStreamingPlaceholder()
        appLog.i(TAG, `Android poll-primary session=${sessionId.slice(0, 8)}`)
        postWebDiagLog(TAG, `poll-primary start sid=${sessionId.slice(0, 8)} loading=${loading.value}`)
        if (!pollLoopActive) {
          pollUntilDone(sessionId)
        }
      } catch (err: any) {
        postWebDiagLog(TAG, `poll-primary FAIL ${err?.name || 'Error'}: ${err?.message || err}`, 'E')
        appLog.e(TAG, 'Android poll-primary failed', err)
      }
      return
    }

    // Desktop / preferPollingOnly (sse_busy): poll DB instead of SSE reconnect loop.
    if (preferPollingOnly) {
      ensureStreamingPlaceholder()
      if (!pollLoopActive) {
        pollUntilDone()
      }
      return
    }

    disconnectStream()
    // Keep parallel DB poll running as backup for dropped thinking events.
    disconnectedByCleanup = false
    if (!isRetry) {
      reconnect.reset()
    }

    ensureStreamingPlaceholder()

    appLog.i(TAG, `EventSource open session=${sessionId.slice(0, 8)}`)
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

    // Track whether we've seen the first content event for the current streaming
    // message — used for diagnostic logging on queue_drain timing
    let firstContentSeen = false

    eventSource.addEventListener('content', (e) => {
      if (sessionChanged()) return
      const sm = findStreamingMsg(messages.value)
      if (!sm) return
      resetStreamTimeout()
      let data: any
      try { data = JSON.parse(e.data) } catch { appLog.w(TAG, 'SSE content: invalid JSON, skipping'); return }
      if (!firstContentSeen) {
        firstContentSeen = true
        appLog.d(TAG, `[content: first] streamingMsg.id=${sm.id ?? 'none'} len=${(data.content || '').length} totalMsgs=${messages.value.length}`)
      }
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
        appLog.d(TAG, `[thinking] new block _key=${blocks[blocks.length - 1]._key} textLen=${data.text.length} blocks=${blocks.length} isLoading=${loading.value}`)
        // Reassign blocks array so Vue re-renders on Android WebView where deep
        // mutations alone may not trigger ContentBlocks updates during SSE.
        sm.blocks = [...blocks]
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
      const pendingCount = messages.value.filter((m: any) => m.pending).length
      appLog.d(TAG, `[done] pending msgs: ${pendingCount}; messages: ${doneSummary}`)

      disconnectStream()
      reconnect.reset()
      preferPollingOnly = false
      onLoadHistory().then(() => {
        // Diagnostic: log message state after loadHistory replaces the array
        const afterSummary = messages.value.map((m: any, i: number) =>
          `[${i}] ${m.role}${m.id ? ` id=${m.id}` : ''}${m.streaming ? ' STREAMING' : ''} content="${(m.content || '').slice(0, 30)}" blocks=${m.blocks?.length || 0}`
        ).join(' | ')
        appLog.d(TAG, `[done→loadHistory] messages(${messages.value.length}): ${afterSummary}`)
      }).finally(() => {
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
      // No error block needed — sm.cancelled already shows the neutral "已中断" marker.
      // User cancellation is intentional, not an error.
      _forceCleanupStreamingState(messages.value, { onRenderNeeded, onExtractScheduledTasks })
      loading.value = false
      preferPollingOnly = false
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
        updateUsageState(data.used ?? 0, data.size, data.cost, data.currency, sessionId)
      }
    })

    // ── Queue queued — new message was enqueued while session is running ──
    // Pushes a pending user message into messages.value. Deduplicates against
    // optimistically pushed pending messages (by content text match).
    eventSource.addEventListener('queue_queued', (e) => {
      resetStreamTimeout()
      let data: any
      try { data = JSON.parse(e.data) } catch { appLog.w(TAG, 'SSE queue_queued: invalid JSON, skipping'); return }

      const eventSessionId = data.sessionId || sessionId
      if (eventSessionId !== currentSessionId.value) return

      const queueText = data.text || ''
      // Dedup: if a pending message with this content already exists, skip
      const alreadyPending = messages.value.some(
        (m: any) => m.role === 'user' && m.pending && m.content === queueText
      )
      if (!alreadyPending && queueText) {
        const queueFiles = [...(data.filePaths || []), ...(data.files || [])]
        messages.value.push({
          role: 'user',
          id: `queue-${Date.now()}-${Math.random().toString(36).slice(2, 8)}`,
          content: queueText,
          blocks: queueText ? [{ type: 'text', text: queueText }] : [],
          files: queueFiles.map((p: string) => ({ path: p })),
          createdAt: new Date().toISOString(),
          pending: true,
        })
        appLog.d(TAG, `[queue_queued] text="${queueText.slice(0,40)}" pushed to messages`)
        onRenderNeeded()
        if (isOpen.value) onScrollBottom(true)
      } else {
        appLog.d(TAG, `[queue_queued] text="${queueText.slice(0,40)}" dedup (already pending)`)
      }
    })

    // ── Queue drain — atomic replacement for old queue_done + queue_consume ──
    // Single event that atomically: finalizes current streaming, creates new
    // streaming placeholder. Pending messages are in messages.value with pending: true.
    eventSource.addEventListener('queue_drain', (e) => {
      resetStreamTimeout()
      let data: any
      try { data = JSON.parse(e.data) } catch { appLog.w(TAG, 'SSE queue_drain: invalid JSON, skipping'); return }

      const eventSessionId = data.sessionId || sessionId

      // Diagnostic: snapshot messages BEFORE drain
      const beforeLen = messages.value.length
      const beforeStreamingCount = messages.value.filter((m: any) => m.streaming).length

      // Only update messages.value if this event is for the currently viewed session.
      if (eventSessionId === currentSessionId.value) {
        const drainText = data.text || ''
        const drainFiles = [...(data.filePaths || []), ...(data.files || [])]
        drainQueueMessage(
          messages.value, drainText, drainFiles, currentBackend.value,
          { onRenderNeeded, onExtractScheduledTasks },
          undefined,
          data.messageId || undefined
        )

        // Diagnostic: snapshot messages AFTER drain
        const afterLen = messages.value.length
        const afterStreamingCount = messages.value.filter((m: any) => m.streaming).length
        appLog.d(TAG, `[queue_drain] sid=${eventSessionId.slice(0,8)} msgId=${data.messageId || 'none'} text="${drainText.slice(0,40)}" | before(${beforeLen},streaming=${beforeStreamingCount}) after(${afterLen},streaming=${afterStreamingCount})`)

        if (isOpen.value) {
          onRenderNeeded()
          onScrollBottom(true)
        }
      } else {
        // Event for a background session — skip. Pending messages in
        // messages.value belong to the current session; background session's
        // pending messages will be synced via fetchQueue when user switches.
        appLog.d(TAG, `[queue_drain] sid=${eventSessionId.slice(0,8)} background session, skipped`)
      }
    })

    // queue_update: sent when a new message is enqueued while a session is running.
    // queue_update: sent when queue state changes (e.g. another client enqueues).
    // Replaces pending portion of messages.value with backend queue state.
    eventSource.addEventListener('queue_update', (e) => {
      resetStreamTimeout()
      let data: any
      try { data = JSON.parse(e.data) } catch { appLog.w(TAG, 'SSE queue_update: invalid JSON, skipping'); return }

      // Route by event's explicit sessionId (fallback to captured sessionId)
      const eventSessionId = data.sessionId || sessionId

      // Replace pending portion of messages.value with backend queue state.
      // The backend queue is the source of truth for pending messages.
      if (eventSessionId === currentSessionId.value) {
        const backendQueue = data.queue || []
        // Remove all existing pending messages from messages.value
        for (let i = messages.value.length - 1; i >= 0; i--) {
          if (messages.value[i].pending) messages.value.splice(i, 1)
        }
        // Push backend queue items as pending messages
        for (const item of backendQueue) {
          const itemFiles = [...(item.files || []), ...(item.filePaths || [])]
          messages.value.push({
            role: 'user',
            id: `queue-${Date.now()}-${Math.random().toString(36).slice(2, 8)}`,
            content: item.text || '',
            blocks: item.text ? [{ type: 'text', text: item.text }] : [],
            files: itemFiles.map((p: string) => ({ path: p })),
            createdAt: item.createdAt || new Date().toISOString(),
            pending: true,
          })
        }
        appLog.d(TAG, `[queue_update] sid=${eventSessionId.slice(0,8)} synced ${backendQueue.length} pending msgs from backend`)
        onRenderNeeded()
      }

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
        enterPollPrimaryMode('sse_busy')
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
        // Server error already handled — start polling if still loading
        if (loading.value && currentSessionId.value) {
          pollUntilDone()
        }
        return
      }
      const wasRecoverable = esRef.readyState !== EventSource.CLOSED
      disconnectStream()
      if (preferPollingOnly) {
        pollUntilDone()
        return
      }
      if (wasRecoverable && currentSessionId.value && loading.value && reconnect.shouldReconnect()) {
        reconnect.scheduleReconnect()
      } else {
        reconnect.reset()
        loading.value = true  // Keep loading true — session is still running
        pollUntilDone()
      }
    }

    // Parallel DB poll — Android WebView may defer EventSource until visible; poll
    // also covers sse_busy and dropped SSE thinking events on desktop.
    if (loading.value && currentSessionId.value && !pollLoopActive) {
      pollUntilDone()
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
    if (preferPollingOnly) {
      appLog.i(TAG, 'Network recovered in poll-primary mode — resuming DB poll')
      pollUntilDone()
      return
    }
    if (eventSource) {
      appLog.i(TAG, 'Network recovered, reconnecting SSE stream')
      disconnectStream()
      connectStream(currentSessionId.value)
    }
  }
  window.addEventListener('online', handleOnline)

  function handleStreamVisibility() {
    if (document.visibilityState === 'hidden') {
      // Keyboard close on Android fires hidden briefly during send — never kill an
      // active turn; that prevents SSE/poll from ever reaching the server.
      if (loading.value) {
        clearVisibilityHiddenTimer()
        return
      }
      if (visibilityHiddenTimer) clearTimeout(visibilityHiddenTimer)
      visibilityHiddenTimer = setTimeout(() => {
        visibilityHiddenTimer = null
        if (document.visibilityState !== 'hidden' || loading.value) return
        disconnectStream()
        stopPolling()
      }, VISIBILITY_HIDDEN_DEBOUNCE_MS)
      return
    }
    clearVisibilityHiddenTimer()
    if (!currentSessionId.value) return
    const hasStreamingMsg = messages.value.some((m: any) => m.streaming)
    const hasUnsentLocalUser = messages.value.some(isLocalOptimisticUserMessage)
    if (!loading.value && !hasStreamingMsg) return
    if (hasUnsentLocalUser) {
      appLog.d(TAG, 'Page visible while POST in flight — skip loadHistory')
      return
    }
    appLog.d(TAG, 'Page visible while streaming — recovering SSE or reloading history')
    reconnect.reset()
    // Poll-primary (Android / sse_busy): resume DB interval poll only.
    if (preferPollingOnly || isAndroidAppMode()) {
      enterPollPrimaryMode(isAndroidAppMode() ? 'android_visible' : 'visible_poll')
      return
    }
    // Desktop: reconnect EventSource on B/F.
    if (loading.value && !eventSource) {
      if (reconnect.shouldReconnect()) {
        reconnect.scheduleReconnect()
      } else {
        onLoadHistory().catch(() => {
          loading.value = false
        })
      }
    } else if (hasStreamingMsg) {
      onLoadHistory().catch(() => {})
    }
    if (loading.value && !eventSource) {
      pollUntilDone()
    }
  }

  onMounted(() => {
    document.addEventListener('visibilitychange', handleStreamVisibility)
  })

  onUnmounted(() => {
    disconnectStream()
    stopPolling()
    clearVisibilityHiddenTimer()
    clearToolUseTimeouts()
    window.removeEventListener('online', handleOnline)
    document.removeEventListener('visibilitychange', handleStreamVisibility)
  })

  return {
    beginOutgoingTurn,
    setOutgoingSendInFlight,
    ensureOutboundPoll,
    connectStream,
    disconnectStream,
    cancelStream,
    stopPolling,
    applyChatStreamUpdate,
    applyPollPayload,
  }
}
