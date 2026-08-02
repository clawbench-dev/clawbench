import { onUnmounted, watch, type Ref } from 'vue'
import { appLog } from '@/utils/appLog'
import { useGlobalEvents } from './useGlobalEvents'
import { gt } from '@/composables/useLocale'
import { updateModeState, updateCommandState, updateThinkingEffortState, currentAgentId, updateUsageState } from './useSessionIdentity'
import { updateACPModelList } from './useAgents'
import { updatePlanEntries } from './usePlanProgress'
import { FILE_MODIFYING_TOOLS, findLastBlockOfType, forceCleanupStreamingState as _forceCleanupStreamingState, findStreamingMsg, drainQueueMessage, cancelPendingMessages, type ChatMessage, type ContentBlock, type ContentEventData, type ThinkingEventData, type ToolUseEventData, type QueueEventData, type ErrorEventData } from '@/utils/chatStreamUtils.ts'
import type { FileEntry } from '@/utils/fileAttachmentUtils'
import type { ChatStreamEventData } from '@/utils/chatStreamUtils.ts'

const TAG = 'ChatStream'

export interface UseChatStreamOptions {
  messages: Ref<ChatMessage[]>
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
    currentSessionId,
    currentBackend,
    loading,
    onRenderNeeded,
    onScrollBottom,
    onLoadHistory,
    onMessage,
    onOpen,
    isOpen,
    onToast,
    onNotification,
    onStreamEnd,
    onFileModified,
    onExtractScheduledTasks,
    onToolResult,
    onToolUpdate,
    onReplayDone,
  } = options

  let streamTimeout: ReturnType<typeof setTimeout> | null = null
  let renderTimer: number | null = null
  // Track tool_use timeout timers so we can clean them up
  const toolUseTimeouts: Map<string, ReturnType<typeof setTimeout>> = new Map()
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

  function connectStream(sessionId: string, options?: { subscribeOnly?: boolean }) {
    disconnectStream()
    isStreaming = true

    // Only create a streaming assistant message for actual AI generation,
    // not for replay waiting (subscribeOnly) where we just need WS events.
    if (!options?.subscribeOnly) {
      // Ensure a streaming assistant message exists — create one if needed
      const existingStreaming = findStreamingMsg(messages.value)
      if (!existingStreaming) {
        const newStreaming: ChatMessage = {
          role: 'assistant' as const,
          id: `drain-${Date.now()}-${Math.random().toString(36).slice(2, 8)}`,
          content: '',
          blocks: [] as ContentBlock[],
          streaming: true,
          createdAt: new Date().toISOString(),
          backend: currentBackend.value
        }
        // Insert after the last non-pending user message, not at the end.
        const lastUserIdx = messages.value.findLastIndex(
          (m) => m.role === 'user' && !m.pending
        )
        if (lastUserIdx !== -1) {
          messages.value.splice(lastUserIdx + 1, 0, newStreaming)
        } else {
          messages.value.push(newStreaming)
        }
        thinkingBlockCounter = 0
        onRenderNeeded()
      } else if ((existingStreaming as ChatMessage).fromDB) {
        delete (existingStreaming as ChatMessage).fromDB
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
        const sm = findStreamingMsg(messages.value)
        if (sm && payload.message_id) {
          sm.id = payload.message_id as number
        }
        break
      }



      case 'content': {
        if (sessionChanged()) return
        const sm = findStreamingMsg(messages.value)
        if (!sm) return
        resetStreamTimeout()
        const contentData = payload as unknown as ContentEventData
        const blocks = sm.blocks!
        const existingText = findLastBlockOfType(blocks, 'text')
        if (existingText) {
          existingText.text += contentData.content ?? ''
        } else {
          blocks.push({ type: 'text', text: contentData.content ?? '' })
        }
        debouncedRender()
        break
      }

      case 'thinking': {
        if (sessionChanged()) return
        const sm = findStreamingMsg(messages.value)
        if (!sm) return
        resetStreamTimeout()
        const thinkingData = payload as unknown as ThinkingEventData
        const blocks = sm.blocks!
        const existingThinking = findLastBlockOfType(blocks, 'thinking')
        if (existingThinking) {
          existingThinking.text += thinkingData.text ?? ''
        } else {
          blocks.push({ type: 'thinking', text: thinkingData.text ?? '', _key: `thinking-${thinkingBlockCounter++}` })
        }
        debouncedRender()
        if (isOpen.value) {
          onScrollBottom()
        }
        break
      }

      case 'thinking_done': {
        if (sessionChanged()) return
        const sm = findStreamingMsg(messages.value)
        if (!sm) return
        const blocks = sm.blocks!
        for (let i = blocks.length - 1; i >= 0; i--) {
          if (blocks[i].type === 'thinking') {
            blocks[i].done = true
            break
          }
        }
        onRenderNeeded()
        break
      }

      case 'tool_use': {
        if (sessionChanged()) return
        const sm = findStreamingMsg(messages.value)
        if (!sm) return
        resetStreamTimeout()
        const data = payload as unknown as ToolUseEventData
        const blocks = sm.blocks!
        const existing = blocks.find((b) => b.type === 'tool_use' && b.id === data.id)
        if (data.done) {
          if (existing) {
            if (data.input && Object.keys(data.input).length > 0) {
              existing.input = data.input
            }
            existing.done = true
            if (data.status !== undefined) existing.status = data.status
            if (data.summary !== undefined) existing.summary = data.summary
            if (data.display_name !== undefined) existing.display_name = data.display_name
            if (data.file_path !== undefined) existing.file_path = data.file_path
            if (data.duration_ms !== undefined) existing.duration_ms = data.duration_ms
          } else {
            const newBlock: ContentBlock = {
              type: 'tool_use', name: data.name!, id: data.id!, done: true,
              status: data.status || '',
            }
            if (data.input && Object.keys(data.input).length > 0) {
              newBlock.input = data.input
            }
            if (data.summary) newBlock.summary = data.summary
            if (data.display_name) newBlock.display_name = data.display_name
            if (data.file_path) newBlock.file_path = data.file_path
            if (data.duration_ms !== undefined) newBlock.duration_ms = data.duration_ms
            blocks.push(newBlock)
          }
          const timer = toolUseTimeouts.get(data.id!)
          if (timer) { clearTimeout(timer); toolUseTimeouts.delete(data.id!) }

          if (data.name && FILE_MODIFYING_TOOLS.has(data.name) && onFileModified) {
            const filePath = data.file_path || existing?.file_path
            if (filePath) {
              onFileModified(filePath)
            }
          }
        } else {
          if (existing) {
            if (data.input && Object.keys(data.input).length > 0) {
              existing.input = data.input
            }
            if (data.name) existing.name = data.name
            if (data.status !== undefined) existing.status = data.status
            if (data.summary !== undefined) existing.summary = data.summary
            if (data.display_name !== undefined) existing.display_name = data.display_name
            if (data.file_path !== undefined) existing.file_path = data.file_path
          } else {
            const newBlock: ContentBlock = {
              type: 'tool_use', name: data.name!, id: data.id!, done: false,
              status: data.status || '',
            }
            if (data.input && Object.keys(data.input).length > 0) {
              newBlock.input = data.input
            }
            if (data.summary) newBlock.summary = data.summary
            if (data.display_name) newBlock.display_name = data.display_name
            if (data.file_path) newBlock.file_path = data.file_path
            blocks.push(newBlock)
            if (data.name !== 'PermissionApproval' && !isSubagentToolName(data.name)) {
              const timer = setTimeout(() => {
                if (!newBlock.done) {
                  appLog.w(TAG, `tool_use block ${data.id} timed out without 'done', marking as done`)
                  newBlock.done = true
                  onRenderNeeded()
                }
                toolUseTimeouts.delete(data.id!)
              }, TOOL_USE_TIMEOUT_MS)
              toolUseTimeouts.set(data.id!, timer)
            }
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
        const sm = findStreamingMsg(messages.value)
        if (!sm) return
        resetStreamTimeout()
        const data = payload as unknown as ToolUseEventData
        const blocks = sm.blocks!
        const existing = blocks.find((b) => b.type === 'tool_use' && b.id === data.id)
        if (existing) {
          if (data.name) existing.name = data.name
          if (data.status !== undefined) existing.status = data.status
          existing.done = true
          if (data.duration_ms !== undefined) existing.duration_ms = data.duration_ms
        }
        const timer = toolUseTimeouts.get(data.id!)
        if (timer) { clearTimeout(timer); toolUseTimeouts.delete(data.id!) }
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
        const sm = findStreamingMsg(messages.value)
        if (!sm) return
        resetStreamTimeout()
        sm.metadata = payload as Record<string, unknown>
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
        onLoadHistory().then(() => {
          const afterSummary = messages.value.map((m, i: number) =>
            `[${i}] ${m.role}${m.id ? ` id=${m.id}` : ''}${m.streaming ? ' STREAMING' : ''} content="${(m.content || '').slice(0, 30)}" blocks=${m.blocks?.length || 0}`
          ).join(' | ')
          appLog.d(TAG, `[done→loadHistory] messages(${messages.value.length}): ${afterSummary}`)
        }).finally(() => {
          loading.value = false
          onMessage()
          if (isOpen.value) {
            onScrollBottom()
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
        onLoadHistory().then(() => {
          loading.value = false
          onRenderNeeded()
          if (isOpen.value) {
            onScrollBottom()
          }
        }).catch(() => {
          loading.value = false
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
        onLoadHistory().catch(() => {
          if (sessionChanged()) return
          const sm = findStreamingMsg(messages.value)
          if (sm) {
            const errorBlock: ContentBlock = { type: 'error', text: errorData?.error || 'Unknown error' }
            if (errorData?.reason) errorBlock.reason = errorData.reason
            sm.blocks = [errorBlock]
          }
          _forceCleanupStreamingState(messages.value, { onRenderNeeded, onExtractScheduledTasks })
          loading.value = false
        })
        onStreamEnd?.('error')
        break
      }

      case 'warning': {
        if (sessionChanged()) return
        const sm = findStreamingMsg(messages.value)
        if (!sm) return
        resetStreamTimeout()
        const warningData = payload as { text?: string; reason?: string }
        if (sm.streamingText) {
          sm.blocks!.push({ type: 'text', text: sm.streamingText as string })
          sm.streamingText = ''
        }
        const warningBlock: ContentBlock = { type: 'warning', text: warningData.text }
        if (warningData.reason) warningBlock.reason = warningData.reason
        sm.blocks!.push(warningBlock)
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
        const mlData = payload as { models?: unknown[] }
        if (Array.isArray(mlData.models) && mlData.models.length > 0) {
          const aid = currentAgentId.value
          if (aid) {
            updateACPModelList(aid, mlData.models as { id: string; name: string }[])
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
        const usageData = payload as { size?: number; used?: number; cost?: number; currency?: string; inputTokens?: number; outputTokens?: number }
        if ((usageData.size ?? 0) > 0) {
          updateUsageState(usageData.used ?? 0, usageData.size!, usageData.cost, usageData.currency, sessionId, usageData.inputTokens, usageData.outputTokens)
        }
        break
      }

      case 'user_message': {
        if (sessionChanged()) return
        const userData = payload as { messageId?: number; content?: string; files?: FileEntry[]; senderClientId?: string; queueId?: string }

        // Skip self-echo: if the sender is this device, we already have the optimistic message
        const myClientId = localStorage.getItem('clawbench_client_id')
        if (userData.senderClientId && userData.senderClientId === myClientId) break

        resetStreamTimeout()
        const userContent = userData.content || ''
        const userFiles: FileEntry[] = [
          ...(userData.files || []).map(f => typeof f === 'string' ? { path: f, isDir: false } : f),
        ]
        const msgId = userData.messageId || 0
        const remoteQueueId = userData.queueId || ''

        // Deduplicate: skip if a message with same DB ID or same content already exists
        // (e.g. loadHistory already loaded it from DB)
        const alreadyExists = messages.value.some((m) => {
          if (m.role !== 'user') return false
          if (msgId > 0 && m.id === msgId) return true
          if (m.content === userContent && !m.pending && !m._remote) return true
          return false
        })
        if (alreadyExists) break

        const newMsg: ChatMessage = {
          role: 'user',
          id: msgId > 0 ? msgId : `remote-${Date.now()}-${Math.random().toString(36).slice(2, 8)}`,
          content: userContent,
          blocks: userContent ? [{ type: 'text', text: userContent }] : [],
          files: userFiles,
          createdAt: new Date().toISOString(),
          _remote: true,
          backend: currentBackend.value,
          ...(remoteQueueId ? { _remoteQueueId: remoteQueueId } : {}),
        }

        // Insert before streaming assistant message (or at end)
        const streamingIdx = messages.value.findIndex(m => m.role === 'assistant' && m.streaming)
        if (streamingIdx !== -1) {
          messages.value.splice(streamingIdx, 0, newMsg)
        } else {
          messages.value.push(newMsg)
        }

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

        const beforeLen = messages.value.length
        const beforeStreamingCount = messages.value.filter((m) => m.streaming).length

        if (eventSessionId === currentSessionId.value) {
          const drainText = drainData.text || ''
          const drainFiles: FileEntry[] = [
            ...(drainData.files || []).map(f => typeof f === 'string' ? { path: f, isDir: false } : f),
            ...(drainData.filePaths || []).map(p => ({ path: p, isDir: false })),
          ]
          drainQueueMessage(
            messages.value, drainData.queueId || '', drainText, drainFiles, currentBackend.value,
            { onRenderNeeded, onExtractScheduledTasks },
            undefined,
            drainData.messageId || undefined
          )

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
        const removed = cancelPendingMessages(messages.value, cancelData.queueIds || [])
        appLog.d(TAG, `[queue_cancel] sid=${eventSessionId.slice(0,8)} removed ${removed} pending msgs with queueIds: ${cancelData.queueIds?.join(',') || 'none'}`)
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
