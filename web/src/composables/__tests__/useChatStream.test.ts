import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest'
import { ref } from 'vue'
import { useChatStream } from '@/composables/useChatStream'
import { forceCleanupStreamingState, FILE_MODIFYING_TOOLS, chatMessageReducer } from '@/utils/chatStreamUtils'

// ── Timer leak prevention ──

const pendingTimers: ReturnType<typeof setTimeout>[] = []
const _origSetTimeout = setTimeout
globalThis.setTimeout = ((fn: TimerHandler, ms?: number, ...args: any[]) => {
  const id = _origSetTimeout(fn, ms, ...args)
  pendingTimers.push(id)
  return id
}) as typeof setTimeout

const pendingIntervals: ReturnType<typeof setInterval>[] = []
const _origSetInterval = setInterval
globalThis.setInterval = ((fn: TimerHandler, ms?: number, ...args: any[]) => {
  const id = _origSetInterval(fn, ms, ...args)
  pendingIntervals.push(id)
  return id
}) as typeof setInterval

// ── Mock useGlobalEvents (WS) ──

let registeredEventHandler: ((event: string, data: unknown) => void) | null = null
let mockSendWsMessage: ReturnType<typeof vi.fn>
let mockConnected: ReturnType<typeof ref<boolean>>

vi.mock('@/composables/useGlobalEvents', () => ({
  useGlobalEvents: () => ({
    onEvent: (handler: (event: string, data: unknown) => void) => {
      registeredEventHandler = handler
      return () => { registeredEventHandler = null }
    },
    sendWsMessage: mockSendWsMessage,
    connected: mockConnected,
  }),
}))

// ── Mocks ──

vi.mock('@/utils/chatStreamUtils', async (importOriginal) => {
  const actual = await importOriginal()
  return {
    ...actual,
    FILE_MODIFYING_TOOLS: new Set(),
    findLastBlockOfType: (blocks: any[], type: string) =>
      [...blocks].reverse().find(b => b.type === type),
  forceCleanupStreamingState: vi.fn((messages: any[]) => {
    const msg = messages.find((m: any) => m.role === 'assistant' && m.streaming)
    if (msg) delete msg.streaming
  }),
  findStreamingMsg: vi.fn((messages: any[]) => {
    return messages.find((m: any) => m.role === 'assistant' && m.streaming)
  }),
  drainQueueMessage: vi.fn((messages: any[], queueId: string, userContent: string, userFiles: any[], currentBackend: string, callbacks: any, _drainId?: string, _dbMessageId?: number) => {
    const streamingMsg = messages.find((m: any) => m.role === 'assistant' && m.streaming)
    if (streamingMsg) delete streamingMsg.streaming
    // Match by queueId first, then by content
    let pendingIdx = -1
    if (queueId) {
      pendingIdx = messages.findIndex((m: any) => m.role === 'user' && m.pending && m.id === queueId)
    }
    if (pendingIdx === -1 && userContent) {
      pendingIdx = messages.findIndex((m: any) => m.role === 'user' && m.pending && m.content === userContent)
    }
    if (pendingIdx !== -1) {
      delete messages[pendingIdx].pending
      // Keep the existing transient string id — the numeric _dbMessageId is
      // intentionally NOT adopted mid-stream (see drainQueueMessage).
      if (typeof messages[pendingIdx].id !== 'string') {
        messages[pendingIdx].id = _drainId || `drain-${Date.now()}-${Math.random().toString(36).slice(2, 8)}`
      }
    } else if (userContent) {
      const effectiveDrainId = _drainId || `drain-${Date.now()}-${Math.random().toString(36).slice(2, 8)}`
      messages.push({ role: 'user', id: effectiveDrainId, _drain: true, content: userContent, blocks: [{ type: 'text', text: userContent }], files: (userFiles || []).map((f: any) => typeof f === 'string' ? { path: f } : f), createdAt: new Date().toISOString() })
    }
    const newStreamingMsg = { role: 'assistant', content: '', blocks: [], streaming: true, createdAt: new Date().toISOString(), backend: currentBackend }
    messages.push(newStreamingMsg)
    return newStreamingMsg
  }),
  cancelPendingMessages: vi.fn((messages: any[], queueIds: string[]) => {
    let removed = 0
    for (let i = messages.length - 1; i >= 0; i--) {
      if (messages[i].pending && queueIds.includes(String(messages[i].id))) {
        messages.splice(i, 1)
        removed++
      }
    }
    return removed
  }),
}
})
vi.mock('@/composables/useLocale', () => ({
  gt: (key: string) => key,
}))

vi.mock('@/composables/useSessionIdentity', () => ({
  updateModeState: vi.fn(),
  updateAvailableModes: vi.fn(),
  updateCommandState: vi.fn(),
  updateThinkingEffortState: vi.fn(),
  updateAvailableThinkingEfforts: vi.fn(),
  updateUsageState: vi.fn(),
  currentAgentId: { value: 'test-agent-1' },
}))

vi.mock('@/composables/useAgents', () => ({
  updateACPModelList: vi.fn(),
}))

vi.mock('@/composables/usePlanProgress', async (importOriginal) => {
  const actual = await importOriginal() as any
  return {
    updatePlanEntries: vi.fn((entries: any[]) => { actual.updatePlanEntries(entries) }),
    clearPlanState: vi.fn(() => { actual.clearPlanState() }),
    usePlanProgress: actual.usePlanProgress,
  }
})

// ── Helpers ──

function createOptions(overrides: Record<string, any> = {}) {
  const messages = ref<any[]>([])
  return {
    messages,
    dispatch: (action: any) => { messages.value = chatMessageReducer(messages.value, action) },
    currentSessionId: ref('test-session-1'),
    currentBackend: ref('test-backend'),
    loading: ref(false),
    onRenderNeeded: vi.fn(),
    onScrollBottom: vi.fn(),
    onLoadHistory: vi.fn().mockResolvedValue(undefined),
    onMessage: vi.fn(),
    onOpen: vi.fn(),
    isOpen: ref(true),
    onParseAssistantContent: vi.fn().mockReturnValue({ blocks: [] }),
    onToast: vi.fn(),
    onNotification: vi.fn(),
    onStreamEnd: vi.fn(),
    onFileModified: vi.fn(),
    onExtractScheduledTasks: vi.fn(),
    onToolResult: vi.fn(),
    onToolUpdate: vi.fn(),
    ...overrides,
  }
}

/** Simulate a WS chat_stream event arriving via onEvent handler */
function simulateWsEvent(eventType: string, payload: Record<string, unknown>, sessionId = 'test-session-1') {
  if (!registeredEventHandler) throw new Error('No WS event handler registered')
  registeredEventHandler('chat_stream', {
    session_id: sessionId,
    event_type: eventType,
    payload,
  })
}

describe('useChatStream', () => {
  beforeEach(() => {
    registeredEventHandler = null
    mockSendWsMessage = vi.fn()
    mockConnected = ref(true)
  })

  afterEach(() => {
    for (const id of pendingTimers) {
      clearTimeout(id)
    }
    pendingTimers.length = 0
    for (const id of pendingIntervals) {
      clearInterval(id)
    }
    pendingIntervals.length = 0
  })

  // ── Basic streaming ──

  describe('connectStream', () => {
    it('should send subscribe message and create streaming placeholder', () => {
      const options = createOptions()
      const { connectStream } = useChatStream(options)

      connectStream('test-session-1')

      expect(mockSendWsMessage).toHaveBeenCalledWith({ type: 'subscribe', session_id: 'test-session-1' })
      const assistantMsg = options.messages.value.find(
        (m: any) => m.role === 'assistant' && m.streaming
      )
      expect(assistantMsg).toBeDefined()
      expect(assistantMsg.blocks).toEqual([])
    })

    it('should create assistant message with current backend', () => {
      const options = createOptions()
      options.currentBackend.value = 'claude-code'
      const { connectStream } = useChatStream(options)

      connectStream('test-session-1')

      const assistantMsg = options.messages.value.find(
        (m: any) => m.role === 'assistant' && m.streaming
      )
      expect(assistantMsg).toBeDefined()
      expect(assistantMsg.backend).toBe('claude-code')
    })

    it('should disconnect previous stream before connecting new one', () => {
      const options = createOptions()
      const { connectStream } = useChatStream(options)

      connectStream('session-1')
      mockSendWsMessage.mockClear()

      // disconnectStream sends unsubscribe for currentSessionId ('test-session-1')
      // because isStreaming is set, then connectStream sends subscribe for session-2
      connectStream('session-2')

      // Should unsubscribe from test-session-1 (currentSessionId at disconnect time)
      // and subscribe to session-2
      expect(mockSendWsMessage).toHaveBeenCalledWith({ type: 'unsubscribe', session_id: 'test-session-1' })
      expect(mockSendWsMessage).toHaveBeenCalledWith({ type: 'subscribe', session_id: 'session-2' })
    })

    it('should insert streaming assistant AFTER the newest non-pending user message', () => {
      const options = createOptions()
      options.messages.value.push(
        { role: 'user', id: 1, content: 'A', blocks: [{ type: 'text', text: 'A' }] },
        { role: 'user', id: 'queue-B', content: 'B', blocks: [{ type: 'text', text: 'B' }], pending: true },
      )

      const { connectStream } = useChatStream(options)
      connectStream('test-session-1')

      // The streaming reply answers A (the last non-pending user message).
      // Queued message B (pending) is a later turn for the drain loop — the
      // reply must NOT be pushed below it, otherwise the order becomes
      // A, B, streaming (the queued-message reply swap).
      expect(options.messages.value[0].role).toBe('user')
      expect(options.messages.value[0].content).toBe('A')
      expect(options.messages.value[1].role).toBe('assistant')
      expect(options.messages.value[1].streaming).toBe(true)
      expect(options.messages.value[2].role).toBe('user')
      expect(options.messages.value[2].pending).toBe(true)
    })

    it('anchors the reply to the newest SENT user (max seq), not a physical-last history message', () => {
      // Regression: msg1 (freshly sent, has a client seq) must be the anchor —
      // NOT the physical-last history message. History messages lack seq, so
      // the max-seq scan finds msg1 even though it may not be the last element.
      const options = createOptions()
      options.messages.value.push(
        // history loaded from DB (no seq)
        { role: 'user', id: 38348, content: 'old', blocks: [{ type: 'text', text: 'old' }] },
        { role: 'assistant', id: 38349, content: 'old reply', blocks: [{ type: 'text', text: 'old reply' }] },
      )
      options.dispatch({ type: 'optimistic_push', msg: {
        role: 'user', id: 'pending-msg1', content: '1', blocks: [{ type: 'text', text: '1' }],
        pending: false, seq: 99,
      } })

      const { connectStream } = useChatStream(options)
      connectStream('test-session-1')

      const placeholder = options.messages.value.find((m: any) => m.role === 'assistant' && m.streaming)
      expect(placeholder).toBeDefined()
      // Anchor must be msg1 (the just-sent message), not the history question.
      expect(String(placeholder.parentQueueId)).toBe('pending-msg1')
      // Order: history, msg1, reply (msg1 is the newest message → after history).
      const order = options.messages.value.map((m: any) => m.role === 'user' ? `u:${m.content}` : `a:${m.content}`)
      expect(order[0]).toBe('u:old')
      expect(order[1]).toBe('a:old reply')
      expect(order[2]).toBe('u:1')
      expect(order[3]).toBe('a:')
    })

    it('should reuse existing streaming message only when reuseExistingStreaming is set', () => {
      const options = createOptions()
      const { connectStream } = useChatStream(options)

      // First connect creates a streaming message
      connectStream('test-session-1')
      const firstAssistant = options.messages.value.find(
        (m: any) => m.role === 'assistant' && m.streaming
      )

      // Second connect (e.g. enqueue while running) explicitly opts into reuse
      mockSendWsMessage.mockClear()
      connectStream('test-session-1', { reuseExistingStreaming: true })

      // Should still have exactly one streaming assistant (the same one)
      const streamingMsgs = options.messages.value.filter(
        (m: any) => m.role === 'assistant' && m.streaming
      )
      expect(streamingMsgs.length).toBe(1)
      expect(streamingMsgs[0]).toBe(firstAssistant)
    })

    it('starts a fresh streaming message for a new turn instead of reusing a stale one (no reply accumulation)', () => {
      const options = createOptions()
      const { connectStream } = useChatStream(options)

      // Turn 1: stream a reply
      connectStream('test-session-1')
      simulateWsEvent('content', { content: 'first reply' })
      const firstStreaming = options.messages.value.find(
        (m: any) => m.role === 'assistant' && m.streaming
      )

      // Simulate a missed 'done': the turn-1 message is still flagged streaming.
      expect(firstStreaming.streaming).toBe(true)

      // Turn 2: fresh connect (session not running) must NOT reuse the stale
      // streaming message — otherwise turn-2 content would accumulate into it.
      connectStream('test-session-1')
      simulateWsEvent('content', { content: 'second reply' })

      const assistants = options.messages.value.filter((m: any) => m.role === 'assistant')
      expect(assistants.length).toBe(2)

      const texts = assistants.map((a: any) =>
        (a.blocks || []).filter((b: any) => b.type === 'text').map((b: any) => b.text).join('')
      )
      expect(texts).toContain('first reply')
      expect(texts).toContain('second reply')
      // The two replies must live in separate messages, never concatenated.
      expect(texts.some((t: string) => t.includes('first reply') && t.includes('second reply'))).toBe(false)
    })

    it('reconnecting a LIVE stream with reuse keeps one streaming message (no split reply)', () => {
      const options = createOptions()
      const { connectStream } = useChatStream(options)

      // A reply is already streaming with partial content (live turn).
      connectStream('test-session-1')
      simulateWsEvent('content', { content: 'partial ' })
      const live = options.messages.value.find((m: any) => m.role === 'assistant' && m.streaming)

      // loadHistory reconnects to the SAME live stream after a WS reconnect /
      // tab-visibility change — passes reuseExistingStreaming:true (see
      // syncSessionState). Must NOT finalize the live message or open a second
      // empty "outputting" segment.
      mockSendWsMessage.mockClear()
      connectStream('test-session-1', { reuseExistingStreaming: true })
      simulateWsEvent('content', { content: 'rest' })

      const streaming = options.messages.value.filter((m: any) => m.role === 'assistant' && m.streaming)
      expect(streaming.length).toBe(1)
      expect(streaming[0]).toBe(live)
      const text = (live.blocks || []).filter((b: any) => b.type === 'text').map((b: any) => b.text).join('')
      expect(text).toBe('partial rest')
    })

    it('reconnect after loadHistory replaces messages with a fromDB streaming message keeps ONE streaming message (no split reply)', () => {
      const options = createOptions()
      const { connectStream } = useChatStream(options)

      // Live turn: [user, assistant streaming with partial content]
      options.messages.value.push(
        { role: 'user', id: 1, content: 'question', blocks: [{ type: 'text', text: 'question' }] },
        { role: 'assistant', id: 100, content: '', blocks: [{ type: 'text', text: 'partial ' }], streaming: true, fromDB: true },
      )

      // loadHistory → syncSessionState (isRunning) reconnects to the SAME live
      // stream with reuseExistingStreaming:true. Must NOT finalize the fromDB
      // streaming message and open a second empty "outputting" segment above it.
      mockSendWsMessage.mockClear()
      connectStream('test-session-1', { reuseExistingStreaming: true })
      simulateWsEvent('content', { content: 'rest' })

      const streaming = options.messages.value.filter((m: any) => m.role === 'assistant' && m.streaming)
      expect(streaming.length).toBe(1)
      const text = (streaming[0].blocks || []).filter((b: any) => b.type === 'text').map((b: any) => b.text).join('')
      expect(text).toBe('partial rest')
      // No finalized duplicate left behind.
      const assistants = options.messages.value.filter((m: any) => m.role === 'assistant')
      expect(assistants.length).toBe(1)
    })

    it('new-turn connect still finalizes a stale streaming message instead of reusing it', () => {
      // The inverse case: a fresh connect for a NEW turn (session not running)
      // must NOT reuse a stale streaming message — otherwise the new reply's
      // content accumulates into the old one.
      const options = createOptions()
      const { connectStream } = useChatStream(options)

      options.messages.value.push(
        { role: 'user', id: 1, content: 'question', blocks: [{ type: 'text', text: 'question' }] },
        { role: 'assistant', id: 100, content: '', blocks: [{ type: 'text', text: 'stale reply' }], streaming: true },
      )

      connectStream('test-session-1')
      simulateWsEvent('content', { content: 'fresh reply' })

      const assistants = options.messages.value.filter((m: any) => m.role === 'assistant')
      expect(assistants.length).toBe(2)
      const texts = assistants.map((a: any) =>
        (a.blocks || []).filter((b: any) => b.type === 'text').map((b: any) => b.text).join('')
      )
      expect(texts).toContain('stale reply')
      expect(texts).toContain('fresh reply')
      expect(texts.some((t: string) => t.includes('stale reply') && t.includes('fresh reply'))).toBe(false)
    })
  })

  describe('disconnectStream', () => {
    it('should send unsubscribe message', () => {
      const options = createOptions()
      const { connectStream, disconnectStream } = useChatStream(options)

      connectStream('test-session-1')
      mockSendWsMessage.mockClear()

      disconnectStream()

      expect(mockSendWsMessage).toHaveBeenCalledWith({ type: 'unsubscribe', session_id: 'test-session-1' })
    })

    it('should be safe to call when no stream is active', () => {
      const { disconnectStream } = useChatStream(createOptions())
      expect(() => disconnectStream()).not.toThrow()
    })

    it('should not send unsubscribe when not streaming', () => {
      const options = createOptions()
      const { disconnectStream } = useChatStream(options)

      // Never called connectStream, so isStreaming is false
      disconnectStream()

      expect(mockSendWsMessage).not.toHaveBeenCalled()
    })
  })

  describe('cancelStream', () => {
    it('should send cancel message via WS when loading is true', async () => {
      const options = createOptions()
      options.loading.value = true
      const { cancelStream } = useChatStream(options)

      await cancelStream()

      expect(mockSendWsMessage).toHaveBeenCalledWith({ type: 'cancel', session_id: 'test-session-1' })
    })

    it('should not send cancel when loading is false (early return)', async () => {
      const options = createOptions()
      options.loading.value = false
      const { cancelStream } = useChatStream(options)

      await cancelStream()

      expect(mockSendWsMessage).not.toHaveBeenCalledWith(expect.objectContaining({ type: 'cancel' }))
    })

    it('should not send cancel when no sessionId (early return)', async () => {
      const options = createOptions({ currentSessionId: ref('') })
      options.loading.value = true
      const { cancelStream } = useChatStream(options)

      await cancelStream()

      expect(mockSendWsMessage).not.toHaveBeenCalled()
    })
  })

  // ── Content events ──

  describe('WS event handling — content', () => {
    it('should coalesce content events into text blocks', () => {
      const options = createOptions()
      const { connectStream } = useChatStream(options)

      connectStream('test-session-1')

      simulateWsEvent('content', { content: 'Hello ' })
      simulateWsEvent('content', { content: 'World' })

      const assistantMsg = options.messages.value.find(
        (m: any) => m.role === 'assistant' && m.streaming
      )
      const textBlocks = assistantMsg.blocks.filter((b: any) => b.type === 'text')
      expect(textBlocks.length).toBe(1)
      expect(textBlocks[0].text).toBe('Hello World')
    })

    it('should drop content events when streaming message was removed', () => {
      const options = createOptions()
      const { connectStream } = useChatStream(options)

      connectStream('test-session-1')

      // Remove the streaming message
      const idx = options.messages.value.findIndex(
        (m: any) => m.role === 'assistant' && m.streaming
      )
      options.messages.value.splice(idx, 1)

      const prevLength = options.messages.value.length
      simulateWsEvent('content', { content: 'should be dropped' })

      expect(options.messages.value.length).toBe(prevLength)
    })

    it('should ignore content events for different session', () => {
      const options = createOptions()
      const { connectStream } = useChatStream(options)

      connectStream('test-session-1')
      options.currentSessionId.value = 'other-session'

      simulateWsEvent('content', { content: 'ignored content' })

      const assistantMsg = options.messages.value.find(
        (m: any) => m.role === 'assistant' && m.streaming
      )
      const textBlocks = assistantMsg?.blocks?.filter((b: any) => b.type === 'text') || []
      expect(textBlocks.length).toBe(0)
    })
  })

  describe('WS event handling — thinking', () => {
    it('should coalesce thinking events into thinking blocks', () => {
      const options = createOptions()
      const { connectStream } = useChatStream(options)

      connectStream('test-session-1')

      simulateWsEvent('thinking', { text: 'Let me think...' })
      simulateWsEvent('thinking', { text: ' about this.' })

      const assistantMsg = options.messages.value.find(
        (m: any) => m.role === 'assistant' && m.streaming
      )
      const thinkingBlocks = assistantMsg.blocks.filter((b: any) => b.type === 'thinking')
      expect(thinkingBlocks.length).toBe(1)
      expect(thinkingBlocks[0].text).toBe('Let me think... about this.')
    })

    it('thinking_done → should mark last thinking block as done', () => {
      const options = createOptions()
      const { connectStream } = useChatStream(options)

      connectStream('test-session-1')

      simulateWsEvent('thinking', { text: 'Deep thought' })
      simulateWsEvent('thinking_done', {})

      const assistantMsg = options.messages.value.find(
        (m: any) => m.role === 'assistant' && m.streaming
      )
      const thinkingBlock = assistantMsg.blocks.find((b: any) => b.type === 'thinking')
      expect(thinkingBlock.done).toBe(true)
    })

    it('thinking_done → should mark only the LAST thinking block as done', () => {
      const options = createOptions()
      const { connectStream } = useChatStream(options)

      connectStream('test-session-1')

      // Manually push blocks to test the reverse iteration
      const assistantMsg = options.messages.value.find(
        (m: any) => m.role === 'assistant' && m.streaming
      )
      assistantMsg.blocks.push({ type: 'thinking', text: 'First thought' })
      assistantMsg.blocks.push({ type: 'text', text: 'Some content' })
      assistantMsg.blocks.push({ type: 'thinking', text: 'Second thought' })

      simulateWsEvent('thinking_done', {})

      expect(assistantMsg.blocks[0].done).toBeUndefined()
      expect(assistantMsg.blocks[2].done).toBe(true)
    })

    it('thinking_done → should do nothing when no thinking block exists', () => {
      const options = createOptions()
      const { connectStream } = useChatStream(options)

      connectStream('test-session-1')
      simulateWsEvent('content', { content: 'Hello' })

      const assistantMsg = options.messages.value.find(
        (m: any) => m.role === 'assistant' && m.streaming
      )
      const blocksLengthBefore = assistantMsg.blocks.length

      simulateWsEvent('thinking_done', {})

      expect(assistantMsg.blocks.length).toBe(blocksLengthBefore)
    })

    it('thinking_done → should call onRenderNeeded', () => {
      const options = createOptions()
      const { connectStream } = useChatStream(options)

      connectStream('test-session-1')
      simulateWsEvent('thinking', { text: 'Thinking...' })
      options.onRenderNeeded.mockClear()

      simulateWsEvent('thinking_done', {})

      expect(options.onRenderNeeded).toHaveBeenCalled()
    })

    it('thinking_done → should skip when guard fails', () => {
      const options = createOptions()
      const { connectStream } = useChatStream(options)

      connectStream('test-session-1')
      simulateWsEvent('thinking', { text: 'Thinking...' })
      options.currentSessionId.value = 'different-session'
      options.onRenderNeeded.mockClear()

      simulateWsEvent('thinking_done', {})

      expect(options.onRenderNeeded).not.toHaveBeenCalled()
    })
  })

  // ── Tool events ──

  describe('WS event handling — tool_use', () => {
    it('should handle tool_use start and done events', () => {
      const options = createOptions()
      const { connectStream } = useChatStream(options)

      connectStream('test-session-1')

      simulateWsEvent('tool_use', {
        name: 'Read',
        id: 'tool-1',
        input: { file_path: '/tmp/test.txt' },
      })

      const assistantMsg = options.messages.value.find(
        (m: any) => m.role === 'assistant' && m.streaming
      )
      const toolBlock = assistantMsg.blocks.find(
        (b: any) => b.type === 'tool_use' && b.id === 'tool-1'
      )
      expect(toolBlock).toBeDefined()
      expect(toolBlock.done).toBe(false)

      simulateWsEvent('tool_use', {
        name: 'Read',
        id: 'tool-1',
        done: true,
        status: 'success',
        summary: 'main.go',
      })

      expect(toolBlock.done).toBe(true)
      expect(toolBlock.status).toBe('success')
      expect(toolBlock.summary).toBe('main.go')
    })

    it('should update input on existing same-id block when not done', () => {
      const options = createOptions()
      const { connectStream } = useChatStream(options)

      connectStream('test-session-1')

      simulateWsEvent('tool_use', {
        name: 'Edit',
        id: 'tool-same',
        input: { file_path: '/tmp/old.txt' },
      })
      simulateWsEvent('tool_use', {
        name: 'Edit',
        id: 'tool-same',
        input: { file_path: '/tmp/new.txt', old_text: 'foo', new_text: 'bar' },
      })

      const assistantMsg = options.messages.value.find(
        (m: any) => m.role === 'assistant' && m.streaming
      )
      const toolBlock = assistantMsg.blocks.find(
        (b: any) => b.type === 'tool_use' && b.id === 'tool-same'
      )
      expect(toolBlock.input).toEqual({ file_path: '/tmp/new.txt', old_text: 'foo', new_text: 'bar' })
      expect(toolBlock.done).toBe(false)
    })

    it('should call onFileModified for FILE_MODIFYING_TOOLS when done', () => {
      FILE_MODIFYING_TOOLS.add('Write')

      const options = createOptions()
      const { connectStream } = useChatStream(options)

      connectStream('test-session-1')

      simulateWsEvent('tool_use', { name: 'Write', id: 'tool-write' })
      simulateWsEvent('tool_use', {
        name: 'Write',
        id: 'tool-write',
        done: true,
        status: 'success',
        file_path: '/tmp/newfile.txt',
      })

      expect(options.onFileModified).toHaveBeenCalledWith('/tmp/newfile.txt')

      FILE_MODIFYING_TOOLS.delete('Write')
    })

    it('should call onToolUpdate when onToolUpdate callback provided', () => {
      const options = createOptions()
      const { connectStream } = useChatStream(options)

      connectStream('test-session-1')

      simulateWsEvent('tool_use', { name: 'Read', id: 'tool-upd-1' })

      expect(options.onToolUpdate).toHaveBeenCalledWith('tool-upd-1')
    })
  })

  describe('WS event handling — tool_result', () => {
    it('should update existing tool_use block with matching id', () => {
      const options = createOptions()
      const { connectStream } = useChatStream(options)

      connectStream('test-session-1')

      simulateWsEvent('tool_use', { name: 'Read', id: 'tool-1', input: { file_path: '/tmp/test.txt' } })
      simulateWsEvent('tool_result', { id: 'tool-1', status: 'success' })

      const assistantMsg = options.messages.value.find(
        (m: any) => m.role === 'assistant' && m.streaming
      )
      const toolBlock = assistantMsg.blocks.find(
        (b: any) => b.type === 'tool_use' && b.id === 'tool-1'
      )
      expect(toolBlock.done).toBe(true)
      expect(toolBlock.status).toBe('success')
    })

    it('should copy duration_ms from tool_result onto the existing block', () => {
      const options = createOptions()
      const { connectStream } = useChatStream(options)

      connectStream('test-session-1')

      simulateWsEvent('tool_use', { name: 'Read', id: 'tool-dur', input: { file_path: '/tmp/test.txt' } })
      simulateWsEvent('tool_result', { id: 'tool-dur', status: 'success', duration_ms: 4200 })

      const assistantMsg = options.messages.value.find(
        (m: any) => m.role === 'assistant' && m.streaming
      )
      const toolBlock = assistantMsg.blocks.find(
        (b: any) => b.type === 'tool_use' && b.id === 'tool-dur'
      )
      expect(toolBlock.duration_ms).toBe(4200)
    })

    it('should copy duration_ms from a done tool_use event', () => {
      const options = createOptions()
      const { connectStream } = useChatStream(options)

      connectStream('test-session-1')

      simulateWsEvent('tool_use', { name: 'Read', id: 'tool-dur-2', input: { file_path: '/tmp/test.txt' } })
      simulateWsEvent('tool_use', { name: 'Read', id: 'tool-dur-2', done: true, status: 'success', duration_ms: 1500 })

      const assistantMsg = options.messages.value.find(
        (m: any) => m.role === 'assistant' && m.streaming
      )
      const toolBlock = assistantMsg.blocks.find(
        (b: any) => b.type === 'tool_use' && b.id === 'tool-dur-2'
      )
      expect(toolBlock.duration_ms).toBe(1500)
    })

    it('should do nothing if no matching tool_use block exists', () => {
      const options = createOptions()
      const { connectStream } = useChatStream(options)

      connectStream('test-session-1')

      simulateWsEvent('tool_result', { id: 'nonexistent-tool', output: 'orphan result', status: 'success' })

      const assistantMsg = options.messages.value.find(
        (m: any) => m.role === 'assistant' && m.streaming
      )
      expect(assistantMsg.blocks.length).toBe(0)
    })

    it('should call onScrollBottom and onToolResult after update', () => {
      const options = createOptions()
      const { connectStream } = useChatStream(options)

      connectStream('test-session-1')

      simulateWsEvent('tool_use', { name: 'Read', id: 'tool-3', input: { file_path: '/tmp/test.txt' } })

      const scrollCallsBefore = options.onScrollBottom.mock.calls.length
      simulateWsEvent('tool_result', { id: 'tool-3', output: 'result' })

      expect(options.onScrollBottom.mock.calls.length).toBeGreaterThan(scrollCallsBefore)
      expect(options.onToolResult).toHaveBeenCalledWith('tool-3')
    })
  })

  describe('WS event handling — metadata', () => {
    it('should set metadata on streaming message', () => {
      const options = createOptions()
      const { connectStream } = useChatStream(options)

      connectStream('test-session-1')

      simulateWsEvent('metadata', { model: 'gpt-4', tokens: 42 })

      const assistantMsg = options.messages.value.find(
        (m: any) => m.role === 'assistant' && m.streaming
      )
      expect(assistantMsg.metadata).toEqual({ model: 'gpt-4', tokens: 42 })
    })

    it('should not set metadata when guard fails (session changed)', () => {
      const options = createOptions()
      const { connectStream } = useChatStream(options)

      connectStream('test-session-1')
      options.currentSessionId.value = 'different-session'

      simulateWsEvent('metadata', { model: 'gpt-4', tokens: 42 })

      const assistantMsg = options.messages.value.find(
        (m: any) => m.role === 'assistant' && m.streaming
      )
      expect(assistantMsg.metadata).toBeUndefined()
    })
  })

  // ── Done / Cancelled / Error ──

  describe('WS event handling — done', () => {
    it('should disconnect and load history on done', async () => {
      const options = createOptions()
      const { connectStream } = useChatStream(options)

      options.loading.value = true
      connectStream('test-session-1')
      mockSendWsMessage.mockClear()

      simulateWsEvent('done', {})

      // Should send unsubscribe
      expect(mockSendWsMessage).toHaveBeenCalledWith({ type: 'unsubscribe', session_id: 'test-session-1' })
      expect(options.onLoadHistory).toHaveBeenCalled()
    })

    it('should set loading to false after done', async () => {
      const options = createOptions()
      const { connectStream } = useChatStream(options)

      options.loading.value = true
      connectStream('test-session-1')

      simulateWsEvent('done', {})

      await vi.waitFor(() => {
        expect(options.loading.value).toBe(false)
      })
    })

    it('should call onStreamEnd with done', async () => {
      const options = createOptions()
      const { connectStream } = useChatStream(options)

      options.loading.value = true
      connectStream('test-session-1')

      simulateWsEvent('done', {})

      await vi.waitFor(() => {
        expect(options.onStreamEnd).toHaveBeenCalledWith('done')
      })
    })

    it('should call onMessage after done', async () => {
      const options = createOptions()
      const { connectStream } = useChatStream(options)

      options.loading.value = true
      connectStream('test-session-1')

      simulateWsEvent('done', {})

      await vi.waitFor(() => {
        expect(options.onMessage).toHaveBeenCalled()
      })
    })

    it('should not modify loading when session changed before done', async () => {
      const options = createOptions()
      const { connectStream } = useChatStream(options)

      options.loading.value = true
      connectStream('test-session-1')
      options.currentSessionId.value = 'different-session'

      simulateWsEvent('done', {})

      // Guard rejected the event — loading stays true
      expect(options.loading.value).toBe(true)
      expect(options.onLoadHistory).not.toHaveBeenCalled()
    })

    it('should not call onStreamEnd when guard fails on done', () => {
      const options = createOptions()
      const { connectStream } = useChatStream(options)

      options.loading.value = true
      connectStream('test-session-1')
      options.currentSessionId.value = 'different-session'

      simulateWsEvent('done', {})

      expect(options.onStreamEnd).not.toHaveBeenCalled()
    })

    it('should call onToast and onNotification when isOpen=false on done', async () => {
      const options = createOptions({ isOpen: ref(false) })
      const { connectStream } = useChatStream(options)

      options.loading.value = true
      connectStream('test-session-1')

      simulateWsEvent('content', { content: 'Done reply' })
      simulateWsEvent('done', {})

      await vi.waitFor(() => {
        expect(options.onToast).toHaveBeenCalled()
      })
      expect(options.onNotification).toHaveBeenCalled()
    })

    it('should call onRenderNeeded(true) after loadHistory resolves to re-render Mermaid on final DOM', async () => {
      const options = createOptions()
      const { connectStream } = useChatStream(options)

      options.loading.value = true
      connectStream('test-session-1')
      // Clear calls from connectStream
      options.onRenderNeeded.mockClear()

      simulateWsEvent('done', {})

      await vi.waitFor(() => {
        expect(options.onLoadHistory).toHaveBeenCalled()
      })
      // Wait for the .then() callback to execute after loadHistory resolves
      await vi.waitFor(() => {
        const forceFullCalls = options.onRenderNeeded.mock.calls.filter((c: any[]) => c[0] === true)
        // Fix for #387: onRenderNeeded(true) called from onLoadHistory().then()
        // (the mocked _forceCleanupStreamingState doesn't call onRenderNeeded)
        expect(forceFullCalls.length).toBeGreaterThanOrEqual(1)
      })
    })
  })

  describe('WS event handling — replay_done', () => {
    it('should call onRenderNeeded(true) after loadHistory resolves to re-render Mermaid', async () => {
      const options = createOptions()
      const { connectStream } = useChatStream(options)

      options.loading.value = true
      connectStream('test-session-1')

      simulateWsEvent('replay_done', {})

      await vi.waitFor(() => {
        expect(options.onLoadHistory).toHaveBeenCalled()
      })
      await vi.waitFor(() => {
        const forceFullCalls = options.onRenderNeeded.mock.calls.filter((c: any[]) => c[0] === true)
        // Fix for #387: onRenderNeeded(true) after loadHistory resolves
        expect(forceFullCalls.length).toBeGreaterThanOrEqual(1)
      })
    })
  })

  describe('WS event handling — cancelled', () => {
    it('should disconnect and mark message as cancelled', () => {
      const options = createOptions()
      const { connectStream } = useChatStream(options)

      options.loading.value = true
      connectStream('test-session-1')
      mockSendWsMessage.mockClear()

      simulateWsEvent('cancelled', {})

      expect(mockSendWsMessage).toHaveBeenCalledWith({ type: 'unsubscribe', session_id: 'test-session-1' })
      const assistantMsg = options.messages.value.find(
        (m: any) => m.role === 'assistant'
      )
      expect(assistantMsg.cancelled).toBe(true)
      expect(options.loading.value).toBe(false)
      expect(options.onStreamEnd).toHaveBeenCalledWith('cancelled')
    })

    it('should not affect new session when cancelled arrives for old session', () => {
      const options = createOptions()
      const stream = useChatStream(options)

      options.loading.value = true
      stream.connectStream('test-session-1')
      mockSendWsMessage.mockClear()

      // Switch to new session
      options.currentSessionId.value = 'session-2'
      stream.connectStream('session-2')
      mockSendWsMessage.mockClear()

      // Stale cancelled event for old session (session_id doesn't match)
      simulateWsEvent('cancelled', {}, 'test-session-1')

      // New session should not be affected
      expect(options.loading.value).toBe(true)
      expect(options.onStreamEnd).not.toHaveBeenCalledWith('cancelled')
    })

    it('should not call onStreamEnd when guard fails on cancelled', () => {
      const options = createOptions()
      const { connectStream } = useChatStream(options)

      options.loading.value = true
      connectStream('test-session-1')
      options.currentSessionId.value = 'different-session'

      simulateWsEvent('cancelled', {})

      expect(options.onStreamEnd).not.toHaveBeenCalled()
    })
  })

  describe('WS event handling — error', () => {
    it('should disconnect stream and call onStreamEnd with error', () => {
      const options = createOptions()
      const { connectStream } = useChatStream(options)

      options.loading.value = true
      connectStream('test-session-1')
      mockSendWsMessage.mockClear()

      simulateWsEvent('error', { error: 'session not running' })

      expect(mockSendWsMessage).toHaveBeenCalledWith({ type: 'unsubscribe', session_id: 'test-session-1' })
      expect(options.onStreamEnd).toHaveBeenCalledWith('error')
    })

    it('should call onLoadHistory on error', () => {
      const options = createOptions()
      const { connectStream } = useChatStream(options)

      options.loading.value = true
      connectStream('test-session-1')

      simulateWsEvent('error', { error: 'session not running' })

      expect(options.onLoadHistory).toHaveBeenCalled()
    })

    it('should not call onStreamEnd when guard fails on error', () => {
      const options = createOptions()
      const { connectStream } = useChatStream(options)

      options.loading.value = true
      connectStream('test-session-1')
      options.currentSessionId.value = 'different-session'

      simulateWsEvent('error', { error: 'session not running' })

      // Error disconnects before guard check, but guard prevents onLoadHistory/onStreamEnd
      expect(options.onStreamEnd).not.toHaveBeenCalledWith('error')
    })

    it('should call onRenderNeeded(true) after loadHistory resolves to re-render Mermaid', async () => {
      const options = createOptions()
      const { connectStream } = useChatStream(options)

      options.loading.value = true
      connectStream('test-session-1')

      simulateWsEvent('error', { error: 'session not running' })

      await vi.waitFor(() => {
        expect(options.onLoadHistory).toHaveBeenCalled()
      })
      await vi.waitFor(() => {
        const forceFullCalls = options.onRenderNeeded.mock.calls.filter((c: any[]) => c[0] === true)
        // Fix for #387: onRenderNeeded(true) after loadHistory resolves
        expect(forceFullCalls.length).toBeGreaterThanOrEqual(1)
      })
    })
  })

  // ── Warning event ──

  describe('WS event handling — warning', () => {
    it('should add warning block to streaming message', () => {
      const options = createOptions()
      const { connectStream } = useChatStream(options)

      connectStream('test-session-1')

      simulateWsEvent('warning', { text: 'Rate limited', reason: 'too_many_requests' })

      const assistantMsg = options.messages.value.find(
        (m: any) => m.role === 'assistant' && m.streaming
      )
      const warningBlock = assistantMsg.blocks.find((b: any) => b.type === 'warning')
      expect(warningBlock).toBeDefined()
      expect(warningBlock.text).toBe('Rate limited')
      expect(warningBlock.reason).toBe('too_many_requests')
    })

    it('should skip onRenderNeeded when isOpen=false', () => {
      const options = createOptions({ isOpen: ref(false) })
      const { connectStream } = useChatStream(options)

      connectStream('test-session-1')
      options.onRenderNeeded.mockClear()

      simulateWsEvent('warning', { text: 'Rate limited' })

      const assistantMsg = options.messages.value.find(
        (m: any) => m.role === 'assistant' && m.streaming
      )
      expect(assistantMsg.blocks.some((b: any) => b.type === 'warning')).toBe(true)
      expect(options.onRenderNeeded).not.toHaveBeenCalled()
    })
  })

  // ── Queue events ──

  describe('WS event handling — queue_drain', () => {
    it('should create new streaming assistant placeholder', () => {
      const options = createOptions()
      const { connectStream } = useChatStream(options)

      connectStream('test-session-1')

      simulateWsEvent('queue_drain', { queueId: '', text: 'drain msg', filePaths: [], files: [], queue: [] })

      const lastMsg = options.messages.value[options.messages.value.length - 1]
      expect(lastMsg.role).toBe('assistant')
      expect(lastMsg.streaming).toBe(true)
      expect(lastMsg.blocks).toEqual([])
    })

    it('should match pending message by queueId and clear pending flag', () => {
      const options = createOptions()
      options.messages.value.push({
        role: 'user', id: 'pending-abc', content: 'queued msg', pending: true,
        blocks: [{ type: 'text', text: 'queued msg' }],
        createdAt: new Date().toISOString(),
      })

      const { connectStream } = useChatStream(options)
      connectStream('test-session-1')

      simulateWsEvent('queue_drain', { queueId: 'pending-abc', text: 'queued msg', filePaths: [], files: [], queue: [] })

      const userMsg = options.messages.value.find((m: any) => m.role === 'user' && m.content === 'queued msg')
      expect(userMsg).toBeDefined()
      expect(userMsg.pending).toBeUndefined()
    })

    it('should not modify messages when session changed', () => {
      const options = createOptions()
      const { connectStream } = useChatStream(options)

      connectStream('test-session-1')
      options.messages.value.push({
        role: 'user', content: 'existing msg', blocks: [{ type: 'text', text: 'existing msg' }],
        createdAt: new Date().toISOString(),
      })

      options.currentSessionId.value = 'different-session'
      const msgCountBefore = options.messages.value.length

      simulateWsEvent('queue_drain', { queueId: '', text: 'another queued msg', filePaths: [], files: [], queue: [] })

      expect(options.messages.value.length).toBe(msgCountBefore)
    })

    it('should finalize old streaming and create new streaming assistant', () => {
      const options = createOptions()
      const { connectStream } = useChatStream(options)

      connectStream('test-session-1')

      const streamingMsg = options.messages.value.find((m: any) => m.role === 'assistant' && m.streaming)
      streamingMsg.content = ''
      streamingMsg.blocks = [{ type: 'text', text: 'A reply content' }]

      simulateWsEvent('queue_drain', { queueId: '', text: 'next msg', filePaths: [], files: [], queue: [] })

      const finalizedMsg = options.messages.value.find((m: any) => m.blocks?.[0]?.text === 'A reply content')
      expect(finalizedMsg).toBeDefined()
      expect(finalizedMsg.streaming).toBeUndefined()

      const newStreaming = options.messages.value.find((m: any) => m.role === 'assistant' && m.streaming)
      expect(newStreaming).toBeDefined()
      expect(newStreaming.blocks).toEqual([])
    })

    it('should create new streaming assistant with correct backend', () => {
      const options = createOptions()
      options.currentBackend.value = 'claude-code'
      const { connectStream } = useChatStream(options)

      connectStream('test-session-1')

      simulateWsEvent('queue_drain', { queueId: '', text: 'next msg', filePaths: [], files: [], queue: [] })

      const newStreaming = options.messages.value.find((m: any) => m.role === 'assistant' && m.streaming)
      expect(newStreaming).toBeDefined()
      expect(newStreaming.backend).toBe('claude-code')
    })
  })

  describe('WS event handling — queue_cancel', () => {
    it('should remove pending messages matching queueIds', () => {
      const options = createOptions()
      options.messages.value.push({
        role: 'user', id: 'pending-1', content: 'A', pending: true,
        blocks: [{ type: 'text', text: 'A' }],
        createdAt: new Date().toISOString(),
      })
      options.messages.value.push({
        role: 'user', id: 'pending-2', content: 'B', pending: true,
        blocks: [{ type: 'text', text: 'B' }],
        createdAt: new Date().toISOString(),
      })

      const { connectStream } = useChatStream(options)
      connectStream('test-session-1')

      simulateWsEvent('queue_cancel', { sessionId: 'test-session-1', queueIds: ['pending-1', 'pending-2'] })

      const pendingMsgs = options.messages.value.filter((m: any) => m.pending)
      expect(pendingMsgs).toHaveLength(0)
    })

    it('should ignore event for different session', () => {
      const options = createOptions()
      options.messages.value.push({
        role: 'user', id: 'pending-1', content: 'A', pending: true,
        blocks: [{ type: 'text', text: 'A' }],
        createdAt: new Date().toISOString(),
      })

      const { connectStream } = useChatStream(options)
      connectStream('test-session-1')

      simulateWsEvent('queue_cancel', { sessionId: 'different-session', queueIds: ['pending-1'] })

      const pendingMsgs = options.messages.value.filter((m: any) => m.pending)
      expect(pendingMsgs).toHaveLength(1)
    })

    it('should call onRenderNeeded after removing', () => {
      const options = createOptions()
      options.messages.value.push({
        role: 'user', id: 'pending-1', content: 'A', pending: true,
        blocks: [{ type: 'text', text: 'A' }],
        createdAt: new Date().toISOString(),
      })

      const { connectStream } = useChatStream(options)
      connectStream('test-session-1')
      options.onRenderNeeded.mockClear()

      simulateWsEvent('queue_cancel', { sessionId: 'test-session-1', queueIds: ['pending-1'] })

      expect(options.onRenderNeeded).toHaveBeenCalled()
    })
  })

  describe('WS event handling — user_message', () => {
    it('should insert user message from another device', () => {
      const options = createOptions()
      options.messages.value.push({
        role: 'assistant', content: '', blocks: [], streaming: true,
        createdAt: new Date().toISOString(),
      })

      const { connectStream } = useChatStream(options)
      // This is a live-stream scenario: the pre-pushed streaming assistant is the
      // current active reply, so reuse it.
      connectStream('test-session-1', { reuseExistingStreaming: true })
      options.onRenderNeeded.mockClear()

      simulateWsEvent('user_message', { messageId: 42, content: 'hello from phone' })

      // User message inserted before streaming assistant
      expect(options.messages.value).toHaveLength(2)
      expect(options.messages.value[0].role).toBe('user')
      expect(options.messages.value[0].content).toBe('hello from phone')
      expect(options.messages.value[0].id).toBe(42)
      expect(options.messages.value[0]._remote).toBe(true)
      expect(options.messages.value[1].role).toBe('assistant')
    })

    it('should deduplicate by DB message ID', () => {
      const options = createOptions()
      options.messages.value.push({
        role: 'user', id: 42, content: 'existing msg',
        blocks: [{ type: 'text', text: 'existing msg' }],
        createdAt: new Date().toISOString(),
      })

      const { connectStream } = useChatStream(options)
      connectStream('test-session-1')

      simulateWsEvent('user_message', { messageId: 42, content: 'existing msg' })

      // No duplicate — same DB ID
      const userMsgs = options.messages.value.filter((m: any) => m.role === 'user')
      expect(userMsgs).toHaveLength(1)
    })

    it('should deduplicate by content for non-pending messages', () => {
      const options = createOptions()
      options.messages.value.push({
        role: 'user', id: 10, content: 'hello',
        blocks: [{ type: 'text', text: 'hello' }],
        createdAt: new Date().toISOString(),
      })

      const { connectStream } = useChatStream(options)
      connectStream('test-session-1')

      simulateWsEvent('user_message', { messageId: 0, content: 'hello' })

      // No duplicate — same content already present (not pending)
      const userMsgs = options.messages.value.filter((m: any) => m.role === 'user')
      expect(userMsgs).toHaveLength(1)
    })

    it('should allow same content if existing message is pending (optimistic)', () => {
      const options = createOptions()
      options.messages.value.push({
        role: 'user', id: 'pending-1', content: 'hello', pending: true,
        blocks: [{ type: 'text', text: 'hello' }],
        createdAt: new Date().toISOString(),
      })

      const { connectStream } = useChatStream(options)
      connectStream('test-session-1')

      simulateWsEvent('user_message', { messageId: 0, content: 'hello' })

      // Pending message is from this device, remote message still added
      const userMsgs = options.messages.value.filter((m: any) => m.role === 'user')
      expect(userMsgs).toHaveLength(2)
    })

    it('should deduplicate by queueId when the pending bubble matches (self-echo)', () => {
      // Regression: when this device enqueues a message, the backend broadcasts
      // user_message carrying the same queueId. If the echo is not skipped, the
      // queued message is rendered twice — once as the pending bubble and once
      // as a remote duplicate.
      const options = createOptions()
      options.messages.value.push({
        role: 'user', id: 'pending-abc', content: 'hello', pending: true,
        blocks: [{ type: 'text', text: 'hello' }],
        createdAt: new Date().toISOString(),
      })

      const { connectStream } = useChatStream(options)
      connectStream('test-session-1')

      simulateWsEvent('user_message', { messageId: 0, content: 'hello', queueId: 'pending-abc' })

      // Same queueId → the remote echo is the pending bubble itself; no duplicate.
      const userMsgs = options.messages.value.filter((m: any) => m.role === 'user')
      expect(userMsgs).toHaveLength(1)
      expect(userMsgs[0].id).toBe('pending-abc')
    })

    it('should push to end when no streaming assistant message exists', () => {
      const options = createOptions()

      const { connectStream } = useChatStream(options)
      connectStream('test-session-1')

      // connectStream creates a streaming placeholder, so there's already 1 assistant message
      // Clear messages to simulate a scenario with no streaming assistant
      options.messages.value.length = 0

      simulateWsEvent('user_message', { messageId: 0, content: 'from phone' })

      expect(options.messages.value).toHaveLength(1)
      expect(options.messages.value[0].role).toBe('user')
      expect(options.messages.value[0].content).toBe('from phone')
      expect(options.messages.value[0]._remote).toBe(true)
      // Temporary ID when messageId is 0
      expect(typeof options.messages.value[0].id).toBe('string')
      expect(options.messages.value[0].id.startsWith('remote-')).toBe(true)
    })

    it('should include file attachments', () => {
      const options = createOptions()

      const { connectStream } = useChatStream(options)
      connectStream('test-session-1')

      simulateWsEvent('user_message', {
        messageId: 55,
        content: 'check this file',
        files: [{ path: '/tmp/a.go', isDir: false }],
      })

      expect(options.messages.value[0].files).toEqual([{ path: '/tmp/a.go', isDir: false }])
    })

    it('should ignore event for different session', () => {
      const options = createOptions()

      const { connectStream } = useChatStream(options)
      connectStream('test-session-1')

      simulateWsEvent('user_message', { messageId: 1, content: 'hello' }, 'different-session')

      // Only the streaming placeholder from connectStream, no user message added
      const userMsgs = options.messages.value.filter((m: any) => m.role === 'user')
      expect(userMsgs).toHaveLength(0)
    })

    it('should skip self-echo when senderClientId matches own clientId', () => {
      // Set a known clientId in localStorage
      localStorage.setItem('clawbench_client_id', 'my-device-123')

      const options = createOptions()
      options.messages.value.push({
        role: 'assistant', content: '', blocks: [], streaming: true,
        createdAt: new Date().toISOString(),
      })

      const { connectStream } = useChatStream(options)
      connectStream('test-session-1')

      // Simulate a user_message from this same device
      simulateWsEvent('user_message', { messageId: 42, content: 'my own msg', senderClientId: 'my-device-123' })

      // No user message inserted — self-echo skipped
      const userMsgs = options.messages.value.filter((m: any) => m.role === 'user')
      expect(userMsgs).toHaveLength(0)

      localStorage.removeItem('clawbench_client_id')
    })

    it('should not skip when senderClientId is different from own clientId', () => {
      localStorage.setItem('clawbench_client_id', 'my-device-123')

      const options = createOptions()
      options.messages.value.push({
        role: 'assistant', content: '', blocks: [], streaming: true,
        createdAt: new Date().toISOString(),
      })

      const { connectStream } = useChatStream(options)
      connectStream('test-session-1')

      // Simulate a user_message from a different device
      simulateWsEvent('user_message', { messageId: 42, content: 'other device msg', senderClientId: 'other-device-456' })

      const userMsgs = options.messages.value.filter((m: any) => m.role === 'user')
      expect(userMsgs).toHaveLength(1)
      expect(userMsgs[0].content).toBe('other device msg')

      localStorage.removeItem('clawbench_client_id')
    })

    it('should store queueId for precise drain matching', () => {
      const options = createOptions()
      options.messages.value.push({
        role: 'assistant', content: '', blocks: [], streaming: true,
        createdAt: new Date().toISOString(),
      })

      const { connectStream } = useChatStream(options)
      connectStream('test-session-1', { reuseExistingStreaming: true })

      simulateWsEvent('user_message', { messageId: 0, content: 'queued msg', queueId: 'pending-abc123' })

      const userMsg = options.messages.value.find((m: any) => m.role === 'user')
      expect(userMsg._remoteQueueId).toBe('pending-abc123')
    })
  })

  // ── ACP state events ──

  describe('WS event handling — ACP state events', () => {
    describe('mode_update', () => {
      it('should call updateModeState with currentModeId and availableModes', async () => {
        const { updateModeState } = await import('@/composables/useSessionIdentity')
        ;(updateModeState as any).mockClear()

        const options = createOptions()
        const { connectStream } = useChatStream(options)
        connectStream('test-session-1')

        simulateWsEvent('mode_update', {
          currentModeId: 'code',
          availableModes: [
            { id: 'ask', name: 'Ask' },
            { id: 'code', name: 'Code' },
          ],
        })

        expect(updateModeState).toHaveBeenCalledWith('code', [
          { id: 'ask', name: 'Ask' },
          { id: 'code', name: 'Code' },
        ])
      })

      it('should skip when no currentModeId and no availableModes', async () => {
        const { updateModeState } = await import('@/composables/useSessionIdentity')
        ;(updateModeState as any).mockClear()

        const options = createOptions()
        const { connectStream } = useChatStream(options)
        connectStream('test-session-1')

        simulateWsEvent('mode_update', { currentModeId: '', availableModes: [] })

        expect(updateModeState).not.toHaveBeenCalled()
      })

      it('should skip when guard fails', async () => {
        const { updateModeState } = await import('@/composables/useSessionIdentity')
        ;(updateModeState as any).mockClear()

        const options = createOptions()
        const { connectStream } = useChatStream(options)
        connectStream('test-session-1')
        options.currentSessionId.value = 'different-session'

        simulateWsEvent('mode_update', { currentModeId: 'code', availableModes: [{ id: 'code', name: 'Code' }] })

        expect(updateModeState).not.toHaveBeenCalled()
      })
    })

    describe('config_update', () => {
      it('with category=mode → should call updateModeState', async () => {
        const { updateModeState } = await import('@/composables/useSessionIdentity')
        ;(updateModeState as any).mockClear()

        const options = createOptions()
        const { connectStream } = useChatStream(options)
        connectStream('test-session-1')

        simulateWsEvent('config_update', {
          currentValueId: 'architect',
          options: [{
            category: 'mode',
            values: [{ id: 'ask', name: 'Ask' }, { id: 'architect', name: 'Architect' }],
          }],
        })

        expect(updateModeState).toHaveBeenCalledWith('architect', [
          { id: 'ask', name: 'Ask' },
          { id: 'architect', name: 'Architect' },
        ])
      })

      it('with id=mode → should call updateModeState', async () => {
        const { updateModeState } = await import('@/composables/useSessionIdentity')
        ;(updateModeState as any).mockClear()

        const options = createOptions()
        const { connectStream } = useChatStream(options)
        connectStream('test-session-1')

        simulateWsEvent('config_update', {
          currentValueId: 'code',
          options: [{
            id: 'mode',
            values: [{ id: 'code', name: 'Code' }],
          }],
        })

        expect(updateModeState).toHaveBeenCalledWith('code', [{ id: 'code', name: 'Code' }])
      })

      it('with non-mode category → should not call updateModeState', async () => {
        const { updateModeState } = await import('@/composables/useSessionIdentity')
        ;(updateModeState as any).mockClear()

        const options = createOptions()
        const { connectStream } = useChatStream(options)
        connectStream('test-session-1')

        simulateWsEvent('config_update', {
          options: [{ category: 'other', values: [{ id: 'x', name: 'X' }] }],
        })

        expect(updateModeState).not.toHaveBeenCalled()
      })

      it('should use value id as name fallback', async () => {
        const { updateModeState } = await import('@/composables/useSessionIdentity')
        ;(updateModeState as any).mockClear()

        const options = createOptions()
        const { connectStream } = useChatStream(options)
        connectStream('test-session-1')

        simulateWsEvent('config_update', {
          currentValueId: 'ask',
          options: [{
            category: 'mode',
            values: [{ id: 'ask' }], // no name field
          }],
        })

        expect(updateModeState).toHaveBeenCalledWith('ask', [{ id: 'ask', name: 'ask' }])
      })

      it('with thought_level category → should call updateThinkingEffortState', async () => {
        const { updateThinkingEffortState } = await import('@/composables/useSessionIdentity')
        ;(updateThinkingEffortState as any).mockClear()

        const options = createOptions()
        const { connectStream } = useChatStream(options)
        connectStream('test-session-1')

        simulateWsEvent('config_update', {
          currentValueId: 'high',
          options: [{
            category: 'thought_level',
            values: [
              { id: 'low', name: 'Low' },
              { id: 'high', name: 'High' },
            ],
          }],
        })

        expect(updateThinkingEffortState).toHaveBeenCalledWith('high', [
          { id: 'low', name: 'Low' },
          { id: 'high', name: 'High' },
        ])
      })
    })

    describe('thinking_effort_update', () => {
      it('should call updateThinkingEffortState with currentId and levels', async () => {
        const { updateThinkingEffortState } = await import('@/composables/useSessionIdentity')
        ;(updateThinkingEffortState as any).mockClear()

        const options = createOptions()
        const { connectStream } = useChatStream(options)
        connectStream('test-session-1')

        simulateWsEvent('thinking_effort_update', {
          currentId: 'medium',
          availableLevels: [
            { id: 'low', name: 'Low' },
            { id: 'medium', name: 'Medium' },
            { id: 'high', name: 'High' },
          ],
        })

        expect(updateThinkingEffortState).toHaveBeenCalledWith('medium', [
          { id: 'low', name: 'Low' },
          { id: 'medium', name: 'Medium' },
          { id: 'high', name: 'High' },
        ])
      })

      it('should call updateThinkingEffortState when only currentId present (bug fix: symmetric with mode_update)', async () => {
        const { updateThinkingEffortState } = await import('@/composables/useSessionIdentity')
        ;(updateThinkingEffortState as any).mockClear()

        const options = createOptions()
        const { connectStream } = useChatStream(options)
        connectStream('test-session-1')

        simulateWsEvent('thinking_effort_update', { currentId: 'low' })

        expect(updateThinkingEffortState).toHaveBeenCalledWith('low', [])
      })

      it('should use id as name fallback', async () => {
        const { updateThinkingEffortState } = await import('@/composables/useSessionIdentity')
        ;(updateThinkingEffortState as any).mockClear()

        const options = createOptions()
        const { connectStream } = useChatStream(options)
        connectStream('test-session-1')

        simulateWsEvent('thinking_effort_update', {
          currentId: 'high',
          availableLevels: [{ id: 'high' }],
        })

        expect(updateThinkingEffortState).toHaveBeenCalledWith('high', [{ id: 'high', name: 'high' }])
      })

      it('should skip when no currentId and no availableLevels', async () => {
        const { updateThinkingEffortState } = await import('@/composables/useSessionIdentity')
        ;(updateThinkingEffortState as any).mockClear()

        const options = createOptions()
        const { connectStream } = useChatStream(options)
        connectStream('test-session-1')

        simulateWsEvent('thinking_effort_update', { currentId: '', availableLevels: [] })

        expect(updateThinkingEffortState).not.toHaveBeenCalled()
      })

      it('should skip when guard fails', async () => {
        const { updateThinkingEffortState } = await import('@/composables/useSessionIdentity')
        ;(updateThinkingEffortState as any).mockClear()

        const options = createOptions()
        const { connectStream } = useChatStream(options)
        connectStream('test-session-1')
        options.currentSessionId.value = 'different-session'

        simulateWsEvent('thinking_effort_update', { currentId: 'high', availableLevels: [{ id: 'high', name: 'High' }] })

        expect(updateThinkingEffortState).not.toHaveBeenCalled()
      })
    })

    describe('commands_update', () => {
      it('should call updateCommandState with commands array', async () => {
        const { updateCommandState } = await import('@/composables/useSessionIdentity')
        ;(updateCommandState as any).mockClear()

        const options = createOptions()
        const { connectStream } = useChatStream(options)
        connectStream('test-session-1')

        simulateWsEvent('commands_update', {
          commands: [
            { name: '/help', description: 'Show help' },
            { name: '/compact', description: 'Compact context', inputHint: '[instructions]' },
          ],
        })

        expect(updateCommandState).toHaveBeenCalledWith([
          { name: '/help', description: 'Show help' },
          { name: '/compact', description: 'Compact context', inputHint: '[instructions]' },
        ])
      })

      it('should not call when commands is not an array', async () => {
        const { updateCommandState } = await import('@/composables/useSessionIdentity')
        ;(updateCommandState as any).mockClear()

        const options = createOptions()
        const { connectStream } = useChatStream(options)
        connectStream('test-session-1')

        simulateWsEvent('commands_update', { commands: 'not-an-array' })

        expect(updateCommandState).not.toHaveBeenCalled()
      })
    })

    describe('model_list_update', () => {
      it('should call updateACPModelList with agent ID and models', async () => {
        const { updateACPModelList } = await import('@/composables/useAgents')
        ;(updateACPModelList as any).mockClear()

        const options = createOptions()
        const { connectStream } = useChatStream(options)
        connectStream('test-session-1')

        simulateWsEvent('model_list_update', {
          models: [{ id: 'gpt-4', name: 'GPT-4' }, { id: 'gpt-3.5', name: 'GPT-3.5' }],
          currentModelId: 'gpt-4',
        })

        expect(updateACPModelList).toHaveBeenCalledWith('test-agent-1', [
          { id: 'gpt-4', name: 'GPT-4' },
          { id: 'gpt-3.5', name: 'GPT-3.5' },
        ], 'gpt-4')
      })

      it('should not call when models is empty', async () => {
        const { updateACPModelList } = await import('@/composables/useAgents')
        ;(updateACPModelList as any).mockClear()

        const options = createOptions()
        const { connectStream } = useChatStream(options)
        connectStream('test-session-1')

        simulateWsEvent('model_list_update', { models: [], currentModelId: '' })

        expect(updateACPModelList).not.toHaveBeenCalled()
      })

      it('should not call when agentId is empty', async () => {
        const { updateACPModelList } = await import('@/composables/useAgents')
        const { currentAgentId } = await import('@/composables/useSessionIdentity')
        ;(updateACPModelList as any).mockClear()
        const origAgentId = currentAgentId.value
        currentAgentId.value = ''

        const options = createOptions()
        const { connectStream } = useChatStream(options)
        connectStream('test-session-1')

        simulateWsEvent('model_list_update', {
          models: [{ id: 'gpt-4', name: 'GPT-4' }],
          currentModelId: 'gpt-4',
        })

        expect(updateACPModelList).not.toHaveBeenCalled()
        currentAgentId.value = origAgentId
      })
    })

    describe('plan_update', () => {
      it('should update plan entries', async () => {
        const { clearPlanState } = await import('@/composables/usePlanProgress')
        clearPlanState()

        const options = createOptions()
        const { connectStream } = useChatStream(options)
        connectStream('test-session-1')

        simulateWsEvent('plan_update', {
          entries: [
            { content: 'Analyze code', priority: 'high', status: 'completed' },
            { content: 'Refactor', priority: 'high', status: 'in_progress' },
            { content: 'Test', priority: 'medium', status: 'pending' },
          ],
        })

        const { usePlanProgress } = await import('@/composables/usePlanProgress')
        const { planEntries, hasPlan } = usePlanProgress()
        expect(hasPlan.value).toBe(true)
        expect(planEntries.value).toHaveLength(3)
        expect(planEntries.value[1].content).toBe('Refactor')
      })

      it('should ignore plan_update when guard fails', async () => {
        const { clearPlanState } = await import('@/composables/usePlanProgress')
        clearPlanState()

        const options = createOptions()
        const { connectStream } = useChatStream(options)
        connectStream('test-session-1')
        options.currentSessionId.value = 'different-session'

        simulateWsEvent('plan_update', {
          entries: [{ content: 'Task', priority: 'high', status: 'pending' }],
        })

        const { usePlanProgress } = await import('@/composables/usePlanProgress')
        const { hasPlan } = usePlanProgress()
        expect(hasPlan.value).toBe(false)
      })

      it('should skip plan_update when entries is not an array', async () => {
        const { clearPlanState } = await import('@/composables/usePlanProgress')
        clearPlanState()

        const options = createOptions()
        const { connectStream } = useChatStream(options)
        connectStream('test-session-1')

        simulateWsEvent('plan_update', { entries: 'not-an-array' })

        const { usePlanProgress } = await import('@/composables/usePlanProgress')
        const { hasPlan } = usePlanProgress()
        expect(hasPlan.value).toBe(false)
      })
    })

    describe('usage_update', () => {
      it('should call updateUsageState when size > 0', async () => {
        const { updateUsageState } = await import('@/composables/useSessionIdentity')
        ;(updateUsageState as any).mockClear()

        const options = createOptions()
        const { connectStream } = useChatStream(options)
        connectStream('test-session-1')

        simulateWsEvent('usage_update', { size: 200000, used: 50000, cost: 0.5, currency: 'USD', inputTokens: 100, outputTokens: 200 })

        expect(updateUsageState).toHaveBeenCalledWith(50000, 200000, 0.5, 'USD', 'test-session-1', 100, 200, undefined, undefined, undefined, undefined)
      })

      it('should skip when size is 0', async () => {
        const { updateUsageState } = await import('@/composables/useSessionIdentity')
        ;(updateUsageState as any).mockClear()

        const options = createOptions()
        const { connectStream } = useChatStream(options)
        connectStream('test-session-1')

        simulateWsEvent('usage_update', { size: 0, used: 0 })

        expect(updateUsageState).not.toHaveBeenCalled()
      })
    })
  })

  // ── Stream start event ──

  describe('WS event handling — stream_start', () => {
    it('should set message id on streaming message', () => {
      const options = createOptions()
      const { connectStream } = useChatStream(options)

      connectStream('test-session-1')

      simulateWsEvent('stream_start', { message_id: 42 })

      const assistantMsg = options.messages.value.find(
        (m: any) => m.role === 'assistant' && m.streaming
      )
      expect(assistantMsg.id).toBe(42)
    })

    it('should skip when guard fails', () => {
      const options = createOptions()
      const { connectStream } = useChatStream(options)

      connectStream('test-session-1')
      options.currentSessionId.value = 'different-session'

      simulateWsEvent('stream_start', { message_id: 99 })

      const assistantMsg = options.messages.value.find(
        (m: any) => m.role === 'assistant' && m.streaming
      )
      expect(assistantMsg.id).not.toBe(99)
    })
  })



  // ── Stream timeout ──

  describe('stream timeout', () => {
    it('should disconnect and reload from DB on stream timeout (session done)', async () => {
      vi.useFakeTimers()
      const options = createOptions()
      // Simulate loadHistory finding the session is no longer running
      options.onLoadHistory = vi.fn().mockImplementation(() => {
        options.loading.value = false
        return Promise.resolve()
      })
      const { connectStream } = useChatStream(options)

      options.loading.value = true
      connectStream('test-session-1')
      mockSendWsMessage.mockClear()

      // Advance past STREAM_TIMEOUT_MS (30000)
      await vi.advanceTimersByTimeAsync(31000)

      expect(options.onLoadHistory).toHaveBeenCalled()
      expect(options.onStreamEnd).toHaveBeenCalledWith('error')
      vi.advanceTimersByTime(10000)
      vi.useRealTimers()
    })

    it('should keep loading=true on stream timeout when session is still running', async () => {
      vi.useFakeTimers()
      const options = createOptions()
      // Simulate loadHistory finding the session is still running
      options.onLoadHistory = vi.fn().mockImplementation(() => {
        // loadHistory internally sets loading=true when data.running=true
        options.loading.value = true
        return Promise.resolve()
      })
      const { connectStream } = useChatStream(options)

      options.loading.value = true
      connectStream('test-session-1')
      mockSendWsMessage.mockClear()

      // Advance past STREAM_TIMEOUT_MS (30000)
      await vi.advanceTimersByTimeAsync(31000)

      expect(options.onLoadHistory).toHaveBeenCalled()
      // Session is still running — loading must stay true
      expect(options.loading.value).toBe(true)
      // onStreamEnd should NOT be called because the session is still active
      expect(options.onStreamEnd).not.toHaveBeenCalled()
      vi.advanceTimersByTime(10000)
      vi.useRealTimers()
    })

    it('should set loading=false and call onStreamEnd when loadHistory fails on timeout', async () => {
      vi.useFakeTimers()
      const options = createOptions()
      options.onLoadHistory = vi.fn().mockRejectedValue(new Error('network error'))
      const { connectStream } = useChatStream(options)

      options.loading.value = true
      connectStream('test-session-1')

      // Advance past STREAM_TIMEOUT_MS (30000)
      await vi.advanceTimersByTimeAsync(31000)

      expect(options.loading.value).toBe(false)
      expect(options.onStreamEnd).toHaveBeenCalledWith('error')
      vi.advanceTimersByTime(10000)
      vi.useRealTimers()
    })

    it('should reset timeout on content event', async () => {
      vi.useFakeTimers()
      const options = createOptions()
      const { connectStream } = useChatStream(options)

      options.loading.value = true
      connectStream('test-session-1')
      options.onLoadHistory.mockClear()

      // Advance 20s — still within timeout
      await vi.advanceTimersByTimeAsync(20000)

      // Receive a content event — resets timeout
      simulateWsEvent('content', { content: 'still alive' })

      // Advance another 20s — should NOT have timed out yet
      await vi.advanceTimersByTimeAsync(20000)
      expect(options.onLoadHistory).not.toHaveBeenCalled()

      vi.advanceTimersByTime(10000)
      vi.useRealTimers()
    })
  })

  // ── isOpen guard ──

  describe('isOpen guard — skip render and scroll when panel not visible', () => {
    it('should skip debouncedRender when isOpen=false', async () => {
      vi.useFakeTimers()
      const options = createOptions({ isOpen: ref(false) })
      const { connectStream } = useChatStream(options)

      connectStream('test-session-1')
      options.onRenderNeeded.mockClear()
      options.onScrollBottom.mockClear()

      simulateWsEvent('content', { content: 'Hello' })

      await vi.advanceTimersByTimeAsync(200)

      // Data accumulated but render/scroll skipped
      const assistantMsg = options.messages.value.find(
        (m: any) => m.role === 'assistant' && m.streaming
      )
      expect(assistantMsg.blocks[0].text).toBe('Hello')
      expect(options.onRenderNeeded).not.toHaveBeenCalled()
      expect(options.onScrollBottom).not.toHaveBeenCalled()

      vi.advanceTimersByTime(10000)
      vi.useRealTimers()
    })

    it('should call debouncedRender when isOpen=true', async () => {
      vi.useFakeTimers()
      const options = createOptions({ isOpen: ref(true) })
      const { connectStream } = useChatStream(options)

      connectStream('test-session-1')
      options.onRenderNeeded.mockClear()
      options.onScrollBottom.mockClear()

      simulateWsEvent('content', { content: 'Hello' })

      await vi.advanceTimersByTimeAsync(100)

      expect(options.onRenderNeeded).toHaveBeenCalled()
      expect(options.onScrollBottom).toHaveBeenCalled()

      vi.advanceTimersByTime(10000)
      vi.useRealTimers()
    })

    it('should skip onScrollBottom on thinking event when isOpen=false', () => {
      const options = createOptions({ isOpen: ref(false) })
      const { connectStream } = useChatStream(options)

      connectStream('test-session-1')
      options.onScrollBottom.mockClear()

      simulateWsEvent('thinking', { text: 'Deep thought' })

      const assistantMsg = options.messages.value.find(
        (m: any) => m.role === 'assistant' && m.streaming
      )
      expect(assistantMsg.blocks[0].text).toBe('Deep thought')
      expect(options.onScrollBottom).not.toHaveBeenCalled()
    })

    it('should skip onScrollBottom on tool_use event when isOpen=false', () => {
      const options = createOptions({ isOpen: ref(false) })
      const { connectStream } = useChatStream(options)

      connectStream('test-session-1')
      options.onScrollBottom.mockClear()

      simulateWsEvent('tool_use', { name: 'Read', id: 'tool-guard-1', input: { file_path: '/tmp/test.txt' } })

      expect(options.onScrollBottom).not.toHaveBeenCalled()
    })

    it('should skip onScrollBottom on done event when isOpen=false', async () => {
      const options = createOptions({ isOpen: ref(false) })
      const { connectStream } = useChatStream(options)

      options.loading.value = true
      connectStream('test-session-1')
      options.onScrollBottom.mockClear()

      simulateWsEvent('done', {})

      await vi.waitFor(() => {
        expect(options.onLoadHistory).toHaveBeenCalled()
      })
      expect(options.onScrollBottom).not.toHaveBeenCalled()
    })

    it('should call onScrollBottom on done event when isOpen=true', async () => {
      const options = createOptions({ isOpen: ref(true) })
      const { connectStream } = useChatStream(options)

      options.loading.value = true
      connectStream('test-session-1')
      options.onScrollBottom.mockClear()

      simulateWsEvent('done', {})

      await vi.waitFor(() => {
        expect(options.onScrollBottom).toHaveBeenCalledWith()
      })
    })
  })

  // ── WS reconnect re-subscription ──

  describe('WS reconnect re-subscription', () => {
    it('should re-subscribe when WS reconnects while streaming', async () => {
      const options = createOptions()
      const { connectStream } = useChatStream(options)

      options.loading.value = true
      connectStream('test-session-1')
      mockSendWsMessage.mockClear()

      // Simulate WS disconnect
      mockConnected.value = false
      await new Promise(r => setTimeout(r, 0))

      // Simulate WS reconnect
      mockSendWsMessage.mockClear()
      mockConnected.value = true
      await new Promise(r => setTimeout(r, 0))

      expect(mockSendWsMessage).toHaveBeenCalledWith({ type: 'subscribe', session_id: 'test-session-1' })
    })

    it('should not re-subscribe when not streaming', async () => {
      const options = createOptions()
      useChatStream(options)

      mockSendWsMessage.mockClear()

      // Never called connectStream, so isStreaming is false
      mockConnected.value = false
      await new Promise(r => setTimeout(r, 0))
      mockConnected.value = true
      await new Promise(r => setTimeout(r, 50))

      expect(mockSendWsMessage).not.toHaveBeenCalledWith(expect.objectContaining({ type: 'subscribe' }))
    })

    it('should reset stream timeout on re-subscription', async () => {
      vi.useFakeTimers()
      const options = createOptions()
      const { connectStream } = useChatStream(options)

      options.loading.value = true
      connectStream('test-session-1')
      options.onLoadHistory.mockClear()

      // Advance 20s
      await vi.advanceTimersByTimeAsync(20000)

      // WS reconnects — resets timeout
      mockConnected.value = false
      await vi.advanceTimersByTimeAsync(0)
      mockSendWsMessage.mockClear()
      mockConnected.value = true
      await vi.advanceTimersByTimeAsync(0)

      // Verify re-subscribe was sent (timeout was reset)
      expect(mockSendWsMessage).toHaveBeenCalledWith({ type: 'subscribe', session_id: 'test-session-1' })

      // Advance another 20s — should NOT have timed out (timeout was reset)
      await vi.advanceTimersByTimeAsync(20000)
      expect(options.onLoadHistory).not.toHaveBeenCalled()

      vi.advanceTimersByTime(10000)
      vi.useRealTimers()
    })
  })

  // ── Non-chat_stream events should be ignored ──

  describe('event filtering', () => {
    it('should ignore non-chat_stream events', () => {
      const options = createOptions()
      const { connectStream } = useChatStream(options)

      connectStream('test-session-1')

      // Simulate a non-chat_stream event
      if (registeredEventHandler) {
        registeredEventHandler('session_update', { session_id: 'test-session-1' })
      }

      const assistantMsg = options.messages.value.find(
        (m: any) => m.role === 'assistant' && m.streaming
      )
      // Streaming message should still exist with no blocks
      expect(assistantMsg).toBeDefined()
      expect(assistantMsg.blocks.length).toBe(0)
    })

    it('should ignore chat_stream events for different session_id', () => {
      const options = createOptions()
      const { connectStream } = useChatStream(options)

      connectStream('test-session-1')

      // Event for a different session
      simulateWsEvent('content', { content: 'wrong session' }, 'other-session')

      const assistantMsg = options.messages.value.find(
        (m: any) => m.role === 'assistant' && m.streaming
      )
      const textBlocks = assistantMsg?.blocks?.filter((b: any) => b.type === 'text') || []
      expect(textBlocks.length).toBe(0)
    })
  })

  // ── tool_use block timeout ──

  describe('tool_use block timeout', () => {
    it('keeps subagent (task) tool block running past the 30s tool timeout', () => {
      vi.useFakeTimers()
      const options = createOptions()
      const { connectStream } = useChatStream(options)
      connectStream('test-session-1')

      simulateWsEvent('tool_use', { id: 'tool-task-1', name: 'task', done: false, status: '' })
      vi.advanceTimersByTime(5000)
      // Reset the stream timeout so only the per-tool timeout fires.
      simulateWsEvent('content', { content: 'working' })
      vi.advanceTimersByTime(26000) // 31s total since the tool started

      const streamingMsg = options.messages.value.find((m: any) => m.role === 'assistant')
      const taskBlock = streamingMsg.blocks.find((b: any) => b.id === 'tool-task-1')
      expect(taskBlock.done).toBe(false)
      vi.useRealTimers()
    })

    it('marks a non-subagent tool done after the 30s tool timeout (unchanged)', () => {
      vi.useFakeTimers()
      const options = createOptions()
      const { connectStream } = useChatStream(options)
      connectStream('test-session-1')

      simulateWsEvent('tool_use', { id: 'tool-read-1', name: 'Read', done: false, status: '' })
      vi.advanceTimersByTime(5000)
      simulateWsEvent('content', { content: 'working' })
      vi.advanceTimersByTime(26000)

      const streamingMsg = options.messages.value.find((m: any) => m.role === 'assistant')
      const readBlock = streamingMsg.blocks.find((b: any) => b.id === 'tool-read-1')
      expect(readBlock.done).toBe(true)
      vi.useRealTimers()
    })

    it('keeps a long-running tool running when progress events keep arriving (watchdog re-armed)', () => {
      vi.useFakeTimers()
      const options = createOptions()
      const { connectStream } = useChatStream(options)
      connectStream('test-session-1')

      // First sight of the tool call arms the watchdog.
      simulateWsEvent('tool_use', { id: 'tool-bash-1', name: 'Bash', done: false, status: '' })
      // Progress events every 25s keep re-arming the 30s watchdog — the tool
      // runs for well past the original 30s but must NOT be marked done.
      for (let t = 25; t <= 100; t += 25) {
        vi.advanceTimersByTime(25000)
        simulateWsEvent('tool_use', { id: 'tool-bash-1', name: 'Bash', done: false, status: 'running', summary: `progress ${t}s` })
      }

      const streamingMsg = options.messages.value.find((m: any) => m.role === 'assistant')
      const bashBlock = streamingMsg.blocks.find((b: any) => b.id === 'tool-bash-1')
      expect(bashBlock.done).toBe(false)
      expect(bashBlock.summary).toBe('progress 100s')
      vi.useRealTimers()
    })

    it('marks a tool done when it goes silent for 30s even after progress events (stall fallback)', () => {
      vi.useFakeTimers()
      const options = createOptions()
      const { connectStream } = useChatStream(options)
      connectStream('test-session-1')

      // Two progress events keep the tool alive...
      simulateWsEvent('tool_use', { id: 'tool-grep-1', name: 'Grep', done: false, status: '' })
      vi.advanceTimersByTime(20000)
      simulateWsEvent('tool_use', { id: 'tool-grep-1', name: 'Grep', done: false, status: 'running' })
      // Re-arm the stream watchdog separately (content event) so the per-tool
      // watchdog at 50s fires without racing the stream timeout.
      vi.advanceTimersByTime(25000)
      simulateWsEvent('content', { content: 'still working' })
      vi.advanceTimersByTime(10000)

      // ...then it goes silent — the per-tool watchdog (last armed at 20s) fires at 50s.
      const streamingMsg = options.messages.value.find((m: any) => m.role === 'assistant')
      const grepBlock = streamingMsg.blocks.find((b: any) => b.id === 'tool-grep-1')
      expect(grepBlock.done).toBe(true)
      vi.useRealTimers()
    })
  })

  describe('queue events (queue_drain / queue_cancel)', () => {
    it('queue_drain: finalizes the previous streaming reply and starts a new one for the queued message', () => {
      const options = createOptions()
      const { connectStream } = useChatStream(options)
      options.messages.value.push(
        { role: 'user', id: 1, content: 'A', blocks: [{ type: 'text', text: 'A' }] },
        { role: 'assistant', id: 2, content: 'A reply', blocks: [{ type: 'text', text: 'A reply' }], streaming: true },
        { role: 'user', id: 'queue-B', content: 'B', blocks: [{ type: 'text', text: 'B' }], pending: true },
      )
      connectStream('test-session-1')

      simulateWsEvent('queue_drain', { sessionId: 'test-session-1', queueId: 'queue-B', text: 'B', messageId: 3 })

      // Previous assistant finalized, queued message adopts its DB id (parent
      // message A is DB-backed → parentIsDB=true).
      const aReply = options.messages.value.find((m: any) => m.content === 'A reply')
      expect(aReply.streaming).toBeUndefined()
      const b = options.messages.value.find((m: any) => m.content === 'B')
      expect(b.pending).toBeUndefined()
      expect(b.id).toBe(3)
      // A new streaming placeholder exists for B's reply.
      const streaming = options.messages.value.filter((m: any) => m.role === 'assistant' && m.streaming)
      expect(streaming.length).toBe(1)
    })

    it('queue_cancel: removes only the cancelled pending messages, leaving others queued', () => {
      const options = createOptions()
      const { connectStream } = useChatStream(options)
      options.messages.value.push(
        { role: 'user', id: 1, content: 'A', blocks: [{ type: 'text', text: 'A' }] },
        { role: 'assistant', id: 2, content: 'A reply', blocks: [{ type: 'text', text: 'A reply' }], streaming: true },
        { role: 'user', id: 'queue-B', content: 'B', blocks: [{ type: 'text', text: 'B' }], pending: true },
        { role: 'user', id: 'queue-C', content: 'C', blocks: [{ type: 'text', text: 'C' }], pending: true },
      )
      connectStream('test-session-1')

      simulateWsEvent('queue_cancel', { sessionId: 'test-session-1', queueIds: ['queue-B'] })

      expect(options.messages.value.some((m: any) => m.content === 'B')).toBe(false)
      expect(options.messages.value.some((m: any) => m.content === 'C')).toBe(true)
    })

    it('queue_drain for a different session is ignored', () => {
      const options = createOptions()
      const { connectStream } = useChatStream(options)
      options.messages.value.push(
        { role: 'user', id: 1, content: 'A', blocks: [{ type: 'text', text: 'A' }] },
        { role: 'assistant', id: 2, content: 'A reply', blocks: [{ type: 'text', text: 'A reply' }], streaming: true },
        { role: 'user', id: 'queue-B', content: 'B', blocks: [{ type: 'text', text: 'B' }], pending: true },
      )
      connectStream('test-session-1')

      simulateWsEvent('queue_drain', { sessionId: 'OTHER-SESSION', queueId: 'queue-B', text: 'B', messageId: 3 })

      // Nothing changed for the current session.
      const b = options.messages.value.find((m: any) => m.content === 'B')
      expect(b.pending).toBe(true)
      const streaming = options.messages.value.filter((m: any) => m.role === 'assistant' && m.streaming)
      expect(streaming.length).toBe(1)
    })
  })
})
