import { ref, onUnmounted, type Ref } from 'vue'
import { appLog } from '@/utils/appLog'
import { findLastBlockOfType } from '@/utils/chatStreamUtils.ts'

const TAG = 'TaskExecStream'

export interface UseTaskExecStreamOptions {
  /** Session ID of the running execution */
  sessionId: Ref<string | null>
  /** Current execution status — when 'running', preview is active */
  status: Ref<string>
  /** Callback to refresh execution detail from API (used by polling fallback) */
  onRefresh: () => Promise<void>
  /** Called when execution completes (status transitions away from 'running') */
  onComplete?: () => void
}

/**
 * Composable for live preview of a running task execution.
 *
 * Connects to the SSE stream for the session (same endpoint as interactive chat).
 * Falls back to 3-second polling if SSE fails (connection error, sse_busy, etc.).
 * Auto-cleanup on unmount or when execution completes.
 */
export function useTaskExecStream(options: UseTaskExecStreamOptions) {
  const { sessionId, status, onRefresh, onComplete } = options

  const streamingMsg = ref<any>(null)
  const isStreaming = ref(false)
  const isPolling = ref(false)

  let eventSource: EventSource | null = null
  let pollTimer: ReturnType<typeof setInterval> | null = null
  let streamTimeout: ReturnType<typeof setTimeout> | null = null
  const toolUseTimeouts: Map<string, ReturnType<typeof setTimeout>> = new Map()

  const STREAM_TIMEOUT_MS = 30000
  const TOOL_USE_TIMEOUT_MS = 30000
  const POLL_INTERVAL_MS = 3000

  function ensureStreamingMsg() {
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

  function resetStreamTimeout() {
    if (streamTimeout) clearTimeout(streamTimeout)
    streamTimeout = setTimeout(() => {
      appLog.w(TAG, 'SSE stream timeout - no events received, falling back to polling')
      disconnectSSE()
      startPolling()
    }, STREAM_TIMEOUT_MS)
  }

  function clearToolUseTimeouts() {
    for (const timer of toolUseTimeouts.values()) clearTimeout(timer)
    toolUseTimeouts.clear()
  }

  function disconnectSSE() {
    if (streamTimeout) { clearTimeout(streamTimeout); streamTimeout = null }
    clearToolUseTimeouts()
    if (eventSource) {
      eventSource.close()
      eventSource = null
    }
  }

  function stopPolling() {
    if (pollTimer) { clearInterval(pollTimer); pollTimer = null }
    isPolling.value = false
  }

  function stopPreview() {
    disconnectSSE()
    stopPolling()
    isStreaming.value = false
    // Finalize streaming message
    if (streamingMsg.value) {
      delete streamingMsg.value.streaming
    }
  }

  // ── SSE connection ──

  function connectSSE(sid: string) {
    disconnectSSE()
    stopPolling()

    const sm = ensureStreamingMsg()
    isStreaming.value = true

    const url = `/api/ai/chat/stream?session_id=${encodeURIComponent(sid)}`
    appLog.i(TAG, `connecting SSE: ${url}`)
    eventSource = new EventSource(url, { withCredentials: true })
    const esRef = eventSource

    resetStreamTimeout()

    // stream_start
    eventSource.addEventListener('stream_start', (e) => {
      let data: any
      try { data = JSON.parse(e.data) } catch { return }
      if (data.message_id) sm.id = data.message_id
    })

    // resume_split
    eventSource.addEventListener('resume_split', (e) => {
      resetStreamTimeout()
      delete sm.streaming
      const phase2 = {
        role: 'assistant',
        content: '',
        blocks: [],
        streaming: true,
        createdAt: new Date().toISOString(),
      }
      let data: any
      try { data = JSON.parse(e.data) } catch { /* empty */ }
      if (data?.message_id) (phase2 as any).id = data.message_id
      streamingMsg.value = phase2
    })

    // content
    eventSource.addEventListener('content', (e) => {
      resetStreamTimeout()
      const msg = streamingMsg.value
      if (!msg) return
      let data: any
      try { data = JSON.parse(e.data) } catch { return }
      const blocks = msg.blocks
      const existingText = findLastBlockOfType(blocks, 'text')
      if (existingText) {
        existingText.text += data.content
      } else {
        blocks.push({ type: 'text', text: data.content })
      }
    })

    // thinking
    eventSource.addEventListener('thinking', (e) => {
      resetStreamTimeout()
      const msg = streamingMsg.value
      if (!msg) return
      let data: any
      try { data = JSON.parse(e.data) } catch { return }
      const blocks = msg.blocks
      const existingThinking = findLastBlockOfType(blocks, 'thinking')
      if (existingThinking) {
        existingThinking.text += data.text
      } else {
        blocks.push({ type: 'thinking', text: data.text })
      }
    })

    // thinking_done
    eventSource.addEventListener('thinking_done', () => {
      const msg = streamingMsg.value
      if (!msg) return
      const blocks = msg.blocks
      for (let i = blocks.length - 1; i >= 0; i--) {
        if (blocks[i].type === 'thinking') {
          blocks[i].done = true
          break
        }
      }
    })

    // tool_use
    eventSource.addEventListener('tool_use', (e) => {
      resetStreamTimeout()
      const msg = streamingMsg.value
      if (!msg) return
      let data: any
      try { data = JSON.parse(e.data) } catch { return }
      const blocks = msg.blocks
      const existing = blocks.find((b: any) => b.type === 'tool_use' && b.id === data.id)
      if (data.done) {
        if (existing) {
          existing.done = true
          if (data.status !== undefined) existing.status = data.status
          if (data.summary !== undefined) existing.summary = data.summary
          if (data.display_name !== undefined) existing.display_name = data.display_name
          if (data.file_path !== undefined) existing.file_path = data.file_path
        }
        const timer = toolUseTimeouts.get(data.id)
        if (timer) { clearTimeout(timer); toolUseTimeouts.delete(data.id) }
      } else {
        if (existing) {
          if (data.name) existing.name = data.name
          if (data.status !== undefined) existing.status = data.status
          if (data.summary !== undefined) existing.summary = data.summary
          if (data.display_name !== undefined) existing.display_name = data.display_name
          if (data.file_path !== undefined) existing.file_path = data.file_path
        } else {
          const newBlock: any = {
            type: 'tool_use', name: data.name, id: data.id, done: false,
            status: data.status || '',
          }
          if (data.summary) newBlock.summary = data.summary
          if (data.display_name) newBlock.display_name = data.display_name
          if (data.file_path) newBlock.file_path = data.file_path
          blocks.push(newBlock)
          const timer = setTimeout(() => {
            if (!newBlock.done) {
              newBlock.done = true
            }
            toolUseTimeouts.delete(data.id)
          }, TOOL_USE_TIMEOUT_MS)
          toolUseTimeouts.set(data.id, timer)
        }
      }
    })

    // tool_result
    eventSource.addEventListener('tool_result', (e) => {
      resetStreamTimeout()
      const msg = streamingMsg.value
      if (!msg) return
      let data: any
      try { data = JSON.parse(e.data) } catch { return }
      const blocks = msg.blocks
      const existing = blocks.find((b: any) => b.type === 'tool_use' && b.id === data.id)
      if (existing) {
        if (data.name) existing.name = data.name
        if (data.status !== undefined) existing.status = data.status
        existing.done = true
      }
      const timer = toolUseTimeouts.get(data.id)
      if (timer) { clearTimeout(timer); toolUseTimeouts.delete(data.id) }
    })

    // metadata
    eventSource.addEventListener('metadata', (e) => {
      resetStreamTimeout()
      const msg = streamingMsg.value
      if (!msg) return
      let data: any
      try { data = JSON.parse(e.data) } catch { return }
      msg.metadata = data
    })

    // done
    eventSource.addEventListener('done', () => {
      if (streamTimeout) { clearTimeout(streamTimeout); streamTimeout = null }
      clearToolUseTimeouts()
      disconnectSSE()
      stopPreview()
      onComplete?.()
    })

    // cancelled
    eventSource.addEventListener('cancelled', () => {
      if (streamTimeout) { clearTimeout(streamTimeout); streamTimeout = null }
      const msg = streamingMsg.value
      if (msg) msg.cancelled = true
      disconnectSSE()
      stopPreview()
      onComplete?.()
    })

    // error
    eventSource.addEventListener('error', (e) => {
      if (streamTimeout) { clearTimeout(streamTimeout); streamTimeout = null }
      let errorData: any
      try { errorData = JSON.parse((e as MessageEvent).data) } catch { /* ignore */ }
      if (errorData?.reason === 'sse_busy') {
        // Another client is consuming the SSE stream — fall back to polling
        appLog.i(TAG, 'SSE busy, falling back to polling')
        disconnectSSE()
        startPolling()
        return
      }
      disconnectSSE()
      startPolling()
    })

    // onerror — connection-level error
    eventSource.onerror = () => {
      if (streamTimeout) { clearTimeout(streamTimeout); streamTimeout = null }
      appLog.w(TAG, `SSE onerror, falling back to polling (readyState=${esRef.readyState})`)
      disconnectSSE()
      // Always fall back to polling — EventSource reconnect is unreliable
      // for scheduled task preview; polling provides consistent 3s updates.
      startPolling()
    }
  }

  // ── Polling fallback ──

  function startPolling() {
    stopPolling()
    isPolling.value = true
    isStreaming.value = true
    appLog.i(TAG, 'starting 3s polling fallback')

    // Initial refresh immediately
    onRefresh()

    pollTimer = setInterval(async () => {
      try {
        await onRefresh()
        // If status is no longer running, stop polling
        if (status.value !== 'running') {
          stopPolling()
          isStreaming.value = false
          onComplete?.()
        }
      } catch {
        appLog.w(TAG, 'Polling refresh failed')
      }
    }, POLL_INTERVAL_MS)
  }

  // ── Start preview ──

  function startPreview() {
    const sid = sessionId.value
    if (!sid || status.value !== 'running') return
    connectSSE(sid)
  }

  // ── Cleanup ──

  onUnmounted(() => {
    stopPreview()
  })

  return {
    /** Reactive streaming message with blocks for ChatMessageItem rendering */
    streamingMsg,
    /** Whether preview is active (SSE or polling) */
    isStreaming,
    /** Whether currently in polling fallback mode */
    isPolling,
    /** Start live preview (SSE + polling fallback) */
    startPreview,
    /** Stop preview and clean up */
    stopPreview,
  }
}
