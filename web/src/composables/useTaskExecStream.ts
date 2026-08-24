import { ref, onUnmounted, watch, type Ref } from 'vue'
import { appLog } from '@/utils/appLog'
import { useGlobalEvents } from './useGlobalEvents'
import { findLastBlockOfType, type ContentBlock } from '@/utils/chatStreamUtils.ts'
import type { ChatStreamEventData } from '@/utils/chatStreamUtils.ts'
import { ToolUseWatchdog } from '@/utils/toolUseWatchdog'

const TAG = 'TaskExecStream'

interface StreamingMsg {
  id?: number
  role: string
  content: string
  blocks: Array<Record<string, unknown>>
  streaming?: boolean
  cancelled?: boolean
  metadata?: unknown
  createdAt: string
}

export interface UseTaskExecStreamOptions {
  /** Session ID of the running execution */
  sessionId: Ref<string | null>
  /** Current execution status — when 'running', preview is active */
  status: Ref<string>
  /** Called when execution completes (status transitions away from 'running') */
  onComplete?: () => void
}

/**
 * Composable for live preview of a running task execution.
 *
 * Connects to the WS chat_stream channel for the session.
 * Auto-cleanup on unmount or when execution completes.
 */
export function useTaskExecStream(options: UseTaskExecStreamOptions) {
  const { sessionId, status, onComplete } = options

  const streamingMsg = ref<StreamingMsg | null>(null)
  const isStreaming = ref(false)

  const TOOL_USE_TIMEOUT_MS = 30000
  // Watchdog for tool_use blocks: reset on every tool_use progress event so
  // long-running tools aren't falsely marked done after a fixed 30s.
  const toolUseWatchdog = new ToolUseWatchdog()

  // Subagent (task/Agent) tool calls run for minutes inside a child session whose
  // inner events aren't forwarded over ACP, so the outer call legitimately exceeds
  // TOOL_USE_TIMEOUT_MS. Don't kill their spinner with the 30s fallback, otherwise
  // a long-running subagent looks like it already finished. Same for
  // PermissionApproval, which waits on user interaction.
  const SUBAGENT_TOOL_NAMES = new Set(['task', 'agent'])
  function isSubagentToolName(name?: unknown): boolean {
    return typeof name === 'string' && SUBAGENT_TOOL_NAMES.has(name.toLowerCase())
  }
  function shouldSkipWatchdog(name?: unknown): boolean {
    return name === 'PermissionApproval' || isSubagentToolName(name)
  }

  const { onEvent, sendWsMessage, connected } = useGlobalEvents()

  function ensureStreamingMsg(): StreamingMsg {
    if (!streamingMsg.value) {
      streamingMsg.value = {
        role: 'assistant',
        content: '',
        blocks: [],
        streaming: true,
        createdAt: new Date().toISOString(),
      }
    }
    return streamingMsg.value
  }

  function clearToolUseTimeouts() {
    toolUseWatchdog.clearAll()
  }

  function stopPreview() {
    const sid = sessionId.value
    if (sid) sendWsMessage({ type: 'unsubscribe', session_id: sid })
    clearToolUseTimeouts()
    isStreaming.value = false
    if (streamingMsg.value) {
      delete streamingMsg.value.streaming
    }
  }

  // ── WS event handler for chat_stream events ──

  const unsubscribeFromWs = onEvent((event: string, data: unknown) => {
    if (event !== 'chat_stream') return
    const csData = data as ChatStreamEventData
    if (csData.session_id !== sessionId.value) return

    const payload = csData.payload as Record<string, unknown>

    switch (csData.event_type) {
      case 'stream_start': {
        const sm = streamingMsg.value
        if (sm && payload.message_id) sm.id = payload.message_id as number
        break
      }

      case 'content_reset': {
        const sm = streamingMsg.value
        if (!sm) return
        sm.blocks = []
        sm.metadata = undefined
        break
      }

      case 'content': {
        const msg = streamingMsg.value
        if (!msg) return
        const blocks = msg.blocks as ContentBlock[]
        const existingText = findLastBlockOfType(blocks, 'text')
        if (existingText) {
          existingText.text += (payload.content as string) ?? ''
        } else {
          blocks.push({ type: 'text', text: (payload.content as string) ?? '' })
        }
        break
      }

      case 'thinking': {
        const msg = streamingMsg.value
        if (!msg) return
        const blocks = msg.blocks as ContentBlock[]
        const existingThinking = findLastBlockOfType(blocks, 'thinking')
        if (existingThinking) {
          existingThinking.text += (payload.text as string) ?? ''
        } else {
          blocks.push({ type: 'thinking', text: (payload.text as string) ?? '' })
        }
        break
      }

      case 'thinking_done': {
        const msg = streamingMsg.value
        if (!msg) return
        const blocks = msg.blocks
        for (let i = blocks.length - 1; i >= 0; i--) {
          if (blocks[i].type === 'thinking') {
            blocks[i].done = true
            break
          }
        }
        break
      }

      case 'tool_use': {
        const msg = streamingMsg.value
        if (!msg) return
        // eslint-disable-next-line @typescript-eslint/no-explicit-any -- tool_use block shape is dynamic
        const data = payload as any
        const blocks = msg.blocks
        const existing = blocks.find((b: Record<string, unknown>) => b.type === 'tool_use' && b.id === data.id)
        if (data.done) {
          if (existing) {
            existing.done = true
            if (data.status !== undefined) existing.status = data.status
            if (data.summary !== undefined) existing.summary = data.summary
            if (data.display_name !== undefined) existing.display_name = data.display_name
            if (data.file_path !== undefined) existing.file_path = data.file_path
            if (data.duration_ms !== undefined) existing.duration_ms = data.duration_ms
          }
          toolUseWatchdog.clear(data.id)
        } else {
          if (existing) {
            if (data.name) existing.name = data.name
            if (data.status !== undefined) existing.status = data.status
            if (data.summary !== undefined) existing.summary = data.summary
            if (data.display_name !== undefined) existing.display_name = data.display_name
            if (data.file_path !== undefined) existing.file_path = data.file_path
            // Progress event: reset the stall watchdog so long-running tools
            // that keep emitting updates are never falsely marked done.
            if (!shouldSkipWatchdog(existing.name)) {
              toolUseWatchdog.start(data.id, TOOL_USE_TIMEOUT_MS, () => {
                if (!existing.done) {
                  appLog.w(TAG, `tool_use block ${data.id} stalled without 'done' for ${TOOL_USE_TIMEOUT_MS}ms, marking as done`)
                  existing.done = true
                }
              })
            }
          } else {
            // eslint-disable-next-line @typescript-eslint/no-explicit-any -- tool_use block shape is dynamic
            const newBlock: any = {
              type: 'tool_use', name: data.name, id: data.id, done: false,
              status: data.status || '',
            }
            if (data.summary) newBlock.summary = data.summary
            if (data.display_name) newBlock.display_name = data.display_name
            if (data.file_path) newBlock.file_path = data.file_path
            blocks.push(newBlock)
            if (!shouldSkipWatchdog(data.name)) {
              toolUseWatchdog.start(data.id, TOOL_USE_TIMEOUT_MS, () => {
                if (!newBlock.done) {
                  appLog.w(TAG, `tool_use block ${data.id} stalled without 'done' for ${TOOL_USE_TIMEOUT_MS}ms, marking as done`)
                  newBlock.done = true
                }
              })
            }
          }
        }
        break
      }

      case 'tool_result': {
        const msg = streamingMsg.value
        if (!msg) return
        // eslint-disable-next-line @typescript-eslint/no-explicit-any -- tool_result block shape is dynamic
        const data = payload as any
        const blocks = msg.blocks
        const existing = blocks.find((b: Record<string, unknown>) => b.type === 'tool_use' && b.id === data.id)
        if (existing) {
          if (data.name) existing.name = data.name
          if (data.status !== undefined) existing.status = data.status
          existing.done = true
          if (data.duration_ms !== undefined) existing.duration_ms = data.duration_ms
        }
        toolUseWatchdog.clear(data.id)
        break
      }

      case 'metadata': {
        const msg = streamingMsg.value
        if (!msg) return
        msg.metadata = payload
        break
      }

      case 'done': {
        clearToolUseTimeouts()
        stopPreview()
        onComplete?.()
        break
      }

      case 'cancelled': {
        const msg = streamingMsg.value
        if (msg) msg.cancelled = true
        stopPreview()
        onComplete?.()
        break
      }

      case 'error': {
        appLog.e(TAG, 'Stream error:', payload)
        stopPreview()
        onComplete?.()
        break
      }
    }
  })

  // Re-subscribe on WS reconnect
  const stopConnectedWatch = watch(connected, (isConnected) => {
    if (isConnected && isStreaming.value && sessionId.value) {
      appLog.i(TAG, 'WS reconnected, re-subscribing to session stream')
      sendWsMessage({ type: 'subscribe', session_id: sessionId.value })
    }
  })

  // ── Start preview ──

  function startPreview() {
    const sid = sessionId.value
    if (!sid || status.value !== 'running') return
    ensureStreamingMsg()
    isStreaming.value = true
    sendWsMessage({ type: 'subscribe', session_id: sid })
    appLog.i(TAG, `Subscribing to WS stream for session: ${sid.slice(0, 8)}`)
  }

  // ── Cleanup ──

  onUnmounted(() => {
    stopPreview()
    unsubscribeFromWs()
    stopConnectedWatch()
  })

  return {
    /** Reactive streaming message with blocks for ChatMessageItem rendering */
    streamingMsg,
    /** Whether preview is active */
    isStreaming,
    /** Start live preview (WS subscribe) */
    startPreview,
    /** Stop preview and clean up (WS unsubscribe) */
    stopPreview,
  }
}
