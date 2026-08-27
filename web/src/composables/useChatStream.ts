import { onUnmounted, watch, type Ref } from 'vue'
import { appLog } from '@/utils/appLog'
import { StreamFrameScheduler } from '@/utils/streamFrameScheduler'
import { useGlobalEvents } from './useGlobalEvents'
import { gt } from '@/composables/useLocale'
import { updateModeState, updateCommandState, updateThinkingEffortState, currentAgentId, updateUsageState } from './useSessionIdentity'
import { updateACPModelList } from './useAgents'
import { updatePlanEntries } from './usePlanProgress'
import { FILE_MODIFYING_TOOLS, forceCleanupStreamingState as _forceCleanupStreamingState, findStreamingMsg, nextClientSeq, type ChatMessage, type ChatMessageAction, type ContentBlock, type ContentEventData, type ThinkingEventData, type ToolUseEventData, type QueueEventData, type ErrorEventData } from '@/utils/chatStreamUtils.ts'
import type { FileEntry } from '@/utils/fileAttachmentUtils'
import type { ChatStreamEventData } from '@/utils/chatStreamUtils.ts'
import { ToolUseWatchdog } from '@/utils/toolUseWatchdog'

const TAG = 'ChatStream'

export interface UseChatStreamOptions {
  messages: Ref<ChatMessage[]>
  /** Single write channel for the messages array (chatMessageReducer). */
  dispatch: (action: ChatMessageAction) => void
  currentSessionId: Ref<string>
  currentBackend: Ref<string>
  loading: Ref<boolean>
  onRenderNeeded: (forceFull?: boolean) => void
  onScrollBottom: (force?: boolean) => void
  onLoadHistory: () => Promise<void>
  onMessage: () => void
  onOpen: () => void
  isOpen: Ref<boolean>
  onParseAssistantContent: (content: string) => { blocks: ContentBlock[]; metadata?: Record<string, unknown>; cancelled?: boolean }
  onToast: (msg: string, opts?: { icon?: string; type?: string; duration?: number; onClick?: () => void }) => void
  onNotification: (title: string, opts?: { body?: string; onClick?: () => void }) => void
  onStreamEnd?: (reason: 'done' | 'cancelled' | 'error') => void
  onFileModified?: (filePath: string) => void
  onExtractScheduledTasks?: (msgs: ChatMessage[]) => void
  onToolResult?: (toolId: string) => void
  onToolUpdate?: (toolId: string) => void
  onReplayDone?: () => void
}

export function useChatStream(options: UseChatStreamOptions) {
  const {
    messages,
    dispatch,
    currentSessionId,
    currentBackend,
    loading,
    onRenderNeeded,
    onScrollBottom,
    onLoadHistory,
    onMessage,
    onOpen,
    isOpen,
    onNotification,
    onStreamEnd,
    onFileModified,
    onExtractScheduledTasks,
    onToolResult,
    onToolUpdate,
    onReplayDone,
  } = options

  let streamTimeout: ReturnType<typeof setTimeout> | null = null
  const renderScheduler = new StreamFrameScheduler()
  // Watchdog for tool_use blocks: a tool that goes silent for TOOL_USE_TIMEOUT_MS
  // is considered stalled and marked done. Restarted on every tool_use progress
  // event so long-running tools aren't falsely marked finished.
  const toolUseWatchdog = new ToolUseWatchdog()
  // Counter for assigning stable _key to thinking blocks during streaming
  let thinkingBlockCounter = 0
  // Whether we are currently streaming (subscribed to a session)
  let isStreaming = false

  const STREAM_TIMEOUT_MS = 30000 // 30 seconds without any WS event = try reconnect
  const PERMISSION_STREAM_TIMEOUT_MS = 300000 // 5 min when permission approval is pending (user deciding)
  const TOOL_USE_TIMEOUT_MS = 30000 // 30 seconds without 'done' event = mark as done

  // Subagent (task/Agent) tool calls run for minutes inside a child session whose
  // inner events aren't forwarded over ACP, so the outer call legitimately exceeds
  // TOOL_USE_TIMEOUT_MS. Don't kill their spinner with the 30s fallback, otherwise a
  // long-running subagent looks like it already finished.
  const SUBAGENT_TOOL_NAMES = new Set(['task', 'agent'])
  function isSubagentToolName(name?: string): boolean {
    return !!name && SUBAGENT_TOOL_NAMES.has(name.toLowerCase())
  }

  const { onEvent, sendWsMessage, connected } = useGlobalEvents()

  function debouncedRender() {
    renderScheduler.cancel('render')
    renderScheduler.cancel('scroll')
    // Panel not visible: skip rendering and scrolling — data still accumulates,
    // rendering will catch up when the tab becomes active (loadHistory on re-activate)
    if (!isOpen.value) {
      return
    }
    renderScheduler.schedule('render', onRenderNeeded)
    renderScheduler.schedule('scroll', () => onScrollBottom())
  }

  function hasPendingPermissionApproval(): boolean {
    const sm = findStreamingMsg(messages.value)
    if (!sm?.blocks) return false
    return sm.blocks.some(
      (b) =>
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
      appLog.w(TAG, 'Stream timeout - no events received, reloading from DB')
      // No WS event received for too long — reload from DB
      disconnectStream()
      onLoadHistory().then(() => {
        // loadHistory sets loading based on data.running — if the session
        // is still active, it will reconnect the stream and keep loading=true.
        // Only trigger onStreamEnd if the session is truly done (loading=false).
        if (!loading.value) {
          onStreamEnd?.('error')
        }
      }).catch(() => {
        // loadHistory failed — reset loading state so user isn't stuck
        loading.value = false
        onStreamEnd?.('error')
      })
    }, timeoutMs)
  }

  function disconnectStream() {
    if (streamTimeout) { clearTimeout(streamTimeout); streamTimeout = null }
    clearToolUseTimeouts()
    if (isStreaming && currentSessionId.value) {
      sendWsMessage({ type: 'unsubscribe', session_id: currentSessionId.value })
    }
    isStreaming = false
  }

  function clearToolUseTimeouts() {
    toolUseWatchdog.clearAll()
  }

  /**
   * Clean up streaming state for the current assistant message.
   * Delegates to the extracted pure function, then handles composable-specific
   * cleanup (tool_use timeouts, loading state).
   */

  function connectStream(sessionId: string, options?: { subscribeOnly?: boolean; reuseExistingStreaming?: boolean }) {
    disconnectStream()
    isStreaming = true

    // Only create a streaming assistant message for actual AI generation,
    // not for replay waiting (subscribeOnly) where we just need WS events.
    if (!options?.subscribeOnly) {
      const existingStreaming = findStreamingMsg(messages.value)

      // A stale streaming message left over from a previous turn (e.g. its
      // 'done' event was missed) must be finalized before we start a new one.
      // Otherwise connectStream would reuse it and keep appending content,
      // echoing all earlier replies into the current reply. Reuse is only
      // legitimate when explicitly opted in (enqueue/reconnect to a live stream).
      if (existingStreaming && !options?.reuseExistingStreaming) {
        _forceCleanupStreamingState(messages.value, { onRenderNeeded, onExtractScheduledTasks })
      }

      // Ensure a streaming assistant message exists — create one if needed
      const streaming = findStreamingMsg(messages.value)
      if (!streaming) {
        // Anchor the new placeholder right after its question so it can never
        // sort above an earlier reply. The question is the newest NON-pending
        // user message: queued messages (pending=true) are later turns waiting
        // for the drain loop — anchoring to one of them would push this reply
        // (and everything before it) below the queued bubbles, producing the
        // wrong order (msg2, msg3 above msg1, reply1). Fall back to the last
        // user message when every user message is pending.
        // NOTE: prefer the user message with the largest seq (monotonic send
        // order) — sorting moves an unadopted msg1 bubble to the front, so the
        // newest user is not necessarily the last physical element. Messages
        // without a seq (DB-loaded history) are excluded unless nothing else.
        let parentUserIdx = -1
        let parentUserSeq = -1
        messages.value.forEach((m, i) => {
          if (m.role !== 'user') return
          if (m.pending) return
          if (m.seq == null) return
          if (m.seq > parentUserSeq) { parentUserSeq = m.seq; parentUserIdx = i }
        })
        if (parentUserIdx === -1) {
          parentUserIdx = messages.value.findLastIndex((m) => m.role === 'user' && !m.pending)
        }
        if (parentUserIdx === -1) {
          parentUserIdx = messages.value.findLastIndex((m) => m.role === 'user')
        }
        const newStreaming: ChatMessage = {
          role: 'assistant' as const,
          id: `drain-${Date.now()}-${Math.random().toString(36).slice(2, 8)}`,
          content: '',
          blocks: [] as ContentBlock[],
          streaming: true,
          createdAt: new Date().toISOString(),
          backend: currentBackend.value,
          seq: nextClientSeq(),
          parentQueueId: parentUserIdx !== -1 ? String(messages.value[parentUserIdx].id) : undefined,
        }
        appLog.d(TAG, `[connectStream] create placeholder id=${newStreaming.id} parentQueueId=${newStreaming.parentQueueId} reuse=${!!options?.reuseExistingStreaming} subscribeOnly=${!!options?.subscribeOnly}`)
        // Single write channel: the reducer pushes + re-sorts.
        dispatch({ type: 'stream_placeholder', msg: newStreaming })
        thinkingBlockCounter = 0
        onRenderNeeded()
      } else if ((streaming as ChatMessage).fromDB) {
        delete (streaming as ChatMessage).fromDB
      }
      onScrollBottom()
    }

    // Subscribe to session's streaming events via WS
    sendWsMessage({ type: 'subscribe', session_id: sessionId })

    // Start stream timeout
    resetStreamTimeout()
  }

  // ── WS event handler for chat_stream events ──
  // All 21+ event types from the backend are dispatched through this single handler.
  const unsubscribeFromWs = onEvent((event: string, data: unknown) => {
    if (event !== 'chat_stream') return
    const csData = data as ChatStreamEventData
    if (csData.session_id !== currentSessionId.value) return

    const sessionId = csData.session_id
    const payload = csData.payload as Record<string, unknown>
    const sessionChanged = () => currentSessionId.value !== sessionId

    switch (csData.event_type) {
      case 'stream_start': {
        if (sessionChanged()) return
        if (payload.message_id) {
          dispatch({ type: 'ws_stream_start', messageId: payload.message_id as number })
        }
        break
      }

      case 'content_reset': {
        if (sessionChanged()) return
        if (!findStreamingMsg(messages.value)) return
        dispatch({ type: 'ws_content_reset' })
        onRenderNeeded()
        break
      }

      case 'content': {
        if (sessionChanged()) return
        if (!findStreamingMsg(messages.value)) return
        resetStreamTimeout()
        const contentData = payload as unknown as ContentEventData
        dispatch({ type: 'ws_content', text: contentData.content ?? '' })
        debouncedRender()
        break
      }

      case 'thinking': {
        if (sessionChanged()) return
        if (!findStreamingMsg(messages.value)) return
        resetStreamTimeout()
        const thinkingData = payload as unknown as ThinkingEventData
        dispatch({ type: 'ws_thinking', text: thinkingData.text ?? '', key: `thinking-${thinkingBlockCounter++}` })
        debouncedRender()
        if (isOpen.value) {
          onScrollBottom()
        }
        break
      }

      case 'thinking_done': {
        if (sessionChanged()) return
        if (!findStreamingMsg(messages.value)) return
        dispatch({ type: 'ws_thinking_done' })
        onRenderNeeded()
        break
      }

      case 'tool_use': {
        if (sessionChanged()) return
        if (!findStreamingMsg(messages.value)) return
        resetStreamTimeout()
        const data = payload as unknown as ToolUseEventData
        dispatch({ type: 'ws_tool_use', data })
        // Side effects that depend on the block's updated state.
        const smAfter = findStreamingMsg(messages.value)
        const blocksAfter = smAfter?.blocks || []
        const existing = blocksAfter.find((b) => b.type === 'tool_use' && b.id === data.id)
        if (data.done) {
          toolUseWatchdog.clear(data.id!)
          if (data.name && FILE_MODIFYING_TOOLS.has(data.name) && onFileModified) {
            const filePath = data.file_path || existing?.file_path
            if (filePath) {
              onFileModified(filePath)
            }
          }
        } else if (!data.done) {
          // Progress event: reset the stall watchdog so long-running tools
          // that keep emitting updates are never falsely marked done.
          if (data.name !== 'PermissionApproval' && !isSubagentToolName(data.name)) {
            const block = existing || blocksAfter[blocksAfter.length - 1]
            toolUseWatchdog.start(data.id!, TOOL_USE_TIMEOUT_MS, () => {
              if (block && !block.done) {
                appLog.w(TAG, `tool_use block ${data.id} stalled without 'done' for ${TOOL_USE_TIMEOUT_MS}ms, marking as done`)
                block.done = true
                onRenderNeeded()
              }
            })
          }
        }
        if (onToolUpdate && data.id) {
          onToolUpdate(data.id)
        }
        if (isOpen.value) {
          onScrollBottom()
        }
        break
      }

      case 'tool_result': {
        if (sessionChanged()) return
        if (!findStreamingMsg(messages.value)) return
        resetStreamTimeout()
        const data = payload as unknown as ToolUseEventData
        dispatch({ type: 'ws_tool_result', data })
        toolUseWatchdog.clear(data.id!)
        onRenderNeeded()
        if (onToolResult && data.id) {
          onToolResult(data.id)
        }
        if (isOpen.value) {
          onScrollBottom()
        }
        break
      }

      case 'metadata': {
        if (sessionChanged()) return
        if (!findStreamingMsg(messages.value)) return
        resetStreamTimeout()
        dispatch({ type: 'ws_metadata', metadata: payload as Record<string, unknown> })
        break
      }

      case 'done': {
        if (sessionChanged()) return
        if (streamTimeout) { clearTimeout(streamTimeout); streamTimeout = null }
        clearToolUseTimeouts()
        thinkingBlockCounter = 0

        _forceCleanupStreamingState(messages.value, { onRenderNeeded, onExtractScheduledTasks })

        const doneSummary = messages.value.map((m, i: number) =>
          `[${i}] ${m.role}${m.id ? ` id=${m.id}` : ''}${m.streaming ? ' STREAMING' : ''} content="${(m.content || '').slice(0, 30)}" blocks=${m.blocks?.length || 0}`
        ).join(' | ')
        const pendingCount = messages.value.filter((m) => m.pending).length
        appLog.d(TAG, `[done] pending msgs: ${pendingCount}; messages: ${doneSummary}`)

        disconnectStream()

        // Unlock input bar and fire stream-end callbacks immediately so the user
        // sees the final state (meta bar, file-changes banner, summary toggle)
        // without waiting for the loadHistory REST round-trip. Previously these
        // were in the .finally() of onLoadHistory(), which meant the UI stayed
        // in a limbo state (streaming indicator gone but meta bar not yet shown)
        // for 50-500ms+ while the REST call completed and the message array was
        // replaced. Moving them here eliminates that perceived lag.
        loading.value = false
        onMessage()
        if (isOpen.value) {
          onScrollBottom()
        }
        onStreamEnd?.('done')
        if (!isOpen.value) {
          const lastMsg = messages.value[messages.value.length - 1]
          if (lastMsg?.role === 'assistant') {
            // In-app toast bubble removed — the completion popover now covers
            // this case (shown when the chat view is not in the foreground).
            // Keep the system notification for when the app is backgrounded.
            onNotification(gt('chat.stream.aiReplied'), {
              body: gt('chat.stream.clickToViewReply'),
              onClick: () => onOpen()
            })
          }
        }

        // Sync messages from DB in the background. loadHistory replaces the
        // entire messages array (DB IDs replace drain-* keys, summary is
        // populated, etc.) but this is a non-urgent consistency refresh — the
        // user already sees the correct final state from forceCleanupStreamingState.
        onLoadHistory().then(() => {
          const afterSummary = messages.value.map((m, i: number) =>
            `[${i}] ${m.role}${m.id ? ` id=${m.id}` : ''}${m.streaming ? ' STREAMING' : ''} content="${(m.content || '').slice(0, 30)}" blocks=${m.blocks?.length || 0}`
          ).join(' | ')
          appLog.d(TAG, `[done→loadHistory] messages(${messages.value.length}): ${afterSummary}`)
          // Re-render Mermaid on the final DOM — loadHistory replaced messages
          // and Vue rebuilt the DOM, destroying any Mermaid SVGs rendered by the
          // earlier forceCleanupStreamingState onRenderNeeded(true) call.
          onRenderNeeded(true)
        }).catch(() => {
          // Non-critical: loadHistory has its own error handling (toast).
          // UI already finalized by forceCleanupStreamingState above.
        })
        break
      }

      case 'replay_done': {
        if (sessionChanged()) return
        appLog.i(TAG, '[replay_done] LoadSession replay completed, reloading history from DB')
        if (streamTimeout) { clearTimeout(streamTimeout); streamTimeout = null }
        clearToolUseTimeouts()
        thinkingBlockCounter = 0
        disconnectStream()
        onReplayDone?.()
        // Unlock input immediately — don't wait for loadHistory REST round-trip.
        loading.value = false
        if (isOpen.value) {
          onScrollBottom()
        }
        // Sync from DB in the background.
        onLoadHistory().then(() => {
          // Force full render — loadHistory replaced DOM, Mermaid needs re-render
          onRenderNeeded(true)
        }).catch(() => {
          // Non-critical: UI already finalized.
        })
        break
      }

      case 'cancelled': {
        if (streamTimeout) { clearTimeout(streamTimeout); streamTimeout = null }
        if (sessionChanged()) return
        const sm = findStreamingMsg(messages.value)
        if (!sm) return
        disconnectStream()
        sm.cancelled = true
        _forceCleanupStreamingState(messages.value, { onRenderNeeded, onExtractScheduledTasks })
        loading.value = false
        onStreamEnd?.('cancelled')
        break
      }

      case 'error': {
        if (streamTimeout) { clearTimeout(streamTimeout); streamTimeout = null }
        if (sessionChanged()) return
        disconnectStream()
        const errorData = payload as unknown as ErrorEventData
        // Set the error block via the reducer's single write channel so the UI
        // updates immediately. The reducer attaches it to the live streaming
        // assistant, or to the last assistant when the stream already ended
        // (backend crash after done) — so the user never needs a reload to see
        // it.
        dispatch({ type: 'ws_error', text: errorData?.error || 'Unknown error', reason: errorData?.reason })
        _forceCleanupStreamingState(messages.value, { onRenderNeeded, onExtractScheduledTasks })
        loading.value = false
        onStreamEnd?.('error')
        // Sync from DB in the background (same pattern as 'done' handler).
        onLoadHistory().then(() => {
          onRenderNeeded(true)
        }).catch(() => {
          // Non-critical: error block already displayed.
        })
        break
      }

      case 'warning': {
        if (sessionChanged()) return
        if (!findStreamingMsg(messages.value)) return
        resetStreamTimeout()
        const warningData = payload as { text?: string; reason?: string }
        dispatch({ type: 'ws_warning', text: warningData.text || '', reason: warningData.reason })
        if (isOpen.value) {
          onRenderNeeded()
        }
        break
      }

      case 'mode_update': {
        if (sessionChanged()) return
        const modeData = payload as Record<string, unknown>
        if (modeData.currentModeId || (modeData.availableModes as unknown[])?.length > 0) {
          updateModeState(modeData.currentModeId as string || '', (modeData.availableModes || []) as { id: string; name: string }[])
        }
        break
      }

      case 'config_update': {
        if (sessionChanged()) return
        const configData = payload as Record<string, unknown>
        for (const opt of (configData.options as Record<string, unknown>[] || [])) {
          if ((opt.category as string) === 'mode' || (opt.id as string) === 'mode') {
            const modes = ((opt.values as Record<string, string>[]) || []).map((v) => ({ id: v.id, name: v.name || v.id }))
            const currentModeId = (configData.currentValueId as string) || ''
            if (currentModeId || modes.length > 0) {
              updateModeState(currentModeId, modes)
            }
          }
          if ((opt.category as string) === 'thought_level' || (opt.id as string) === 'thought_level') {
            const levels = ((opt.values as Record<string, string>[]) || []).map((v) => ({ id: v.id, name: v.name || v.id }))
            const currentId = (configData.currentValueId as string) || ''
            if (currentId || levels.length > 0) {
              updateThinkingEffortState(currentId, levels)
            }
          }
        }
        break
      }

      case 'thinking_effort_update': {
        if (sessionChanged()) return
        const effortData = payload as Record<string, unknown>
        if (effortData.currentId || (effortData.availableLevels as unknown[])?.length > 0) {
          const levels = ((effortData.availableLevels as Record<string, string>[]) || []).map((l) => ({ id: l.id, name: l.name || l.id }))
          const currentId = (effortData.currentId as string) || ''
          updateThinkingEffortState(currentId, levels)
        }
        break
      }

      case 'commands_update': {
        if (sessionChanged()) return
        const cmdData = payload as { commands?: unknown[] }
        if (Array.isArray(cmdData.commands)) {
          updateCommandState(cmdData.commands as { name: string; description: string; inputHint?: string }[])
        }
        break
      }

      case 'model_list_update': {
        if (sessionChanged()) return
        const mlData = payload as { models?: unknown[]; currentModelId?: string }
        if (Array.isArray(mlData.models) && mlData.models.length > 0) {
          const aid = currentAgentId.value
          if (aid) {
            updateACPModelList(aid, mlData.models as { id: string; name: string }[], mlData.currentModelId)
          }
        }
        break
      }

      case 'plan_update': {
        if (sessionChanged()) return
        const planData = payload as { entries?: unknown[] }
        if (Array.isArray(planData.entries)) {
          updatePlanEntries(planData.entries as import('@/composables/usePlanProgress').PlanEntry[])
        }
        break
      }

      case 'usage_update': {
        if (sessionChanged()) return
        const usageData = payload as { size?: number; used?: number; cost?: number; currency?: string; inputTokens?: number; outputTokens?: number; totalTokens?: number; cachedReadTokens?: number; cachedWriteTokens?: number; thoughtTokens?: number }
        if ((usageData.size ?? 0) > 0) {
          updateUsageState(usageData.used ?? 0, usageData.size!, usageData.cost, usageData.currency, sessionId, usageData.inputTokens, usageData.outputTokens, usageData.totalTokens, usageData.cachedReadTokens, usageData.cachedWriteTokens, usageData.thoughtTokens)
        }
        break
      }

      case 'user_message': {
        if (sessionChanged()) return
        const userData = payload as { messageId?: number; content?: string; files?: FileEntry[]; senderClientId?: string; queueId?: string }

        // Skip self-echo: if the sender is this device, we already have the
        // optimistic message. Still adopt its DB id from messageId — this is
        // the ONLY reliable way to learn a directly-sent message's DB id
        // without a backend that echoes msgId in the POST response. Without
        // it the unadopted bubble sorts as a transient (after later queued
        // messages that already adopted DB ids) — the ordering mess.
        const myClientId = localStorage.getItem('clawbench_client_id')
        if (userData.senderClientId && userData.senderClientId === myClientId) {
          if (userData.messageId && userData.queueId) {
            dispatch({ type: 'optimistic_adopt_id', id: userData.queueId, dbId: userData.messageId })
          }
          break
        }

        resetStreamTimeout()
        dispatch({ type: 'ws_user_message', data: { ...userData, backend: currentBackend.value } })

        debouncedRender()
        if (isOpen.value) {
          onScrollBottom()
        }
        break
      }

      case 'queue_drain': {
        resetStreamTimeout()
        const drainData = payload as unknown as QueueEventData
        const eventSessionId = drainData.sessionId || sessionId

        if (eventSessionId === currentSessionId.value) {
          const drainText = drainData.text || ''
          const drainFiles: FileEntry[] = [
            ...(drainData.files || []).map(f => typeof f === 'string' ? { path: f, isDir: false } : f),
            ...(drainData.filePaths || []).map(p => ({ path: p, isDir: false })),
          ]
          const beforeLen = messages.value.length
          const beforeStreamingCount = messages.value.filter((m) => m.streaming).length
          // Diagnostic: what pending/string-id bubbles exist before this drain.
          const bubbleSummary = messages.value
            .filter((m) => m.pending || typeof m.id === 'string')
            .map((m) => `[${String(m.id)}${m.queueId ? '/q=' + m.queueId : ''}${m.pending ? '/P' : ''}]`)
            .join(' ')
          appLog.d(TAG, `[queue_drain] before: ${bubbleSummary}`)
          dispatch({ type: 'ws_queue_drain', queueId: drainData.queueId || '', text: drainText, files: drainFiles, dbMessageId: drainData.messageId || undefined, backend: currentBackend.value })
          // Extract scheduled tasks from the newly added message(s).
          onExtractScheduledTasks?.(messages.value)

          const afterLen = messages.value.length
          const afterStreamingCount = messages.value.filter((m) => m.streaming).length
          appLog.d(TAG, `[queue_drain] sid=${eventSessionId.slice(0,8)} queueId=${drainData.queueId || 'none'} msgId=${drainData.messageId || 'none'} text="${drainText.slice(0,40)}" | before(${beforeLen},streaming=${beforeStreamingCount}) after(${afterLen},streaming=${afterStreamingCount})`)

          if (isOpen.value) {
            onRenderNeeded()
            onScrollBottom()
          }
        }
        break
      }

      case 'queue_cancel': {
        const cancelData = payload as { sessionId?: string; queueIds?: string[] }
        const eventSessionId = cancelData.sessionId || sessionId
        if (eventSessionId !== currentSessionId.value) break
        const ids = cancelData.queueIds || []
        const before = messages.value.length
        dispatch({ type: 'ws_queue_cancel', queueIds: ids })
        const removed = before - messages.value.length
        appLog.d(TAG, `[queue_cancel] sid=${eventSessionId.slice(0,8)} removed ${removed} pending msgs with queueIds: ${ids.join(',') || 'none'}`)
        onRenderNeeded()
        break
      }
    }
  })

  async function cancelStream() {
    if (!currentSessionId.value || !loading.value) return
    // Send cancel via WS
    sendWsMessage({ type: 'cancel', session_id: currentSessionId.value })
  }

  // Re-subscribe on WS reconnect
  // NOTE: After this watch fires, App.vue's handleReconnect runs
  // loadSessionsOnce() which refreshes runningSessions. If the session
  // finished during disconnection, handleReconnect (via useChatSession)
  // will detect the stale loading state and clean it up. The re-subscribe
  // here is a fallback for the case where the session IS still running.
  const stopConnectedWatch = watch(connected, (isConnected) => {
    if (isConnected && isStreaming && currentSessionId.value) {
      appLog.i(TAG, 'WS reconnected, re-subscribing to session stream')
      sendWsMessage({ type: 'subscribe', session_id: currentSessionId.value })
      resetStreamTimeout()
    }
  })

  function handleOnline() {
    if (!loading.value || !currentSessionId.value) return
    if (isStreaming) {
      appLog.i(TAG, 'Network recovered, stream will re-subscribe via WS')
    }
  }
  window.addEventListener('online', handleOnline)

  onUnmounted(() => {
    disconnectStream()
    clearToolUseTimeouts()
    renderScheduler.cancelAll()
    unsubscribeFromWs()
    stopConnectedWatch()
    window.removeEventListener('online', handleOnline)
  })

  return {
    connectStream,
    disconnectStream,
    cancelStream,
  }
}
