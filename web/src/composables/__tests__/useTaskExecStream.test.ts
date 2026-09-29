import { describe, expect, it, vi, beforeEach } from 'vitest'
import { ref, nextTick } from 'vue'
import { useTaskExecStream } from '@/composables/useTaskExecStream'

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

vi.mock('@/utils/appLog', () => ({
  appLog: { i: vi.fn(), w: vi.fn(), e: vi.fn(), d: vi.fn() },
}))

vi.mock('@/utils/chatStreamUtils', async (importOriginal) => {
  // Spread the REAL module so the code under test runs the real helpers. A
  // hand-written copy here would make this suite prove only that the copy agrees
  // with itself — and it did: the previous mock reimplemented findLastBlockOfType
  // without the tool_use boundary, so the suite could not see the duplicate
  // think_id that boundary causes.
  const actual = await importOriginal<typeof import('@/utils/chatStreamUtils')>()
  return { ...actual }
})

vi.mock('vue', async () => {
  const actual = await vi.importActual('vue')
  return {
    ...actual,
    onUnmounted: vi.fn(),
  }
})

// ── Helpers ──

function simulateWsEvent(eventType: string, payload: unknown, sessionId = 'test-session-123') {
  if (!registeredEventHandler) throw new Error('No event handler registered')
  registeredEventHandler('chat_stream', {
    session_id: sessionId,
    event_type: eventType,
    payload,
  })
}

describe('useTaskExecStream', () => {
  beforeEach(() => {
    registeredEventHandler = null
    mockSendWsMessage = vi.fn()
    mockConnected = ref(true)
    vi.spyOn(console, 'warn').mockImplementation(() => {})
  })

  function createStream(overrides?: { sessionId?: string | null; status?: string }) {
    const sessionId = ref<string | null>(overrides?.sessionId !== undefined ? overrides.sessionId : 'test-session-123')
    const status = ref<string>(overrides?.status ?? 'running')
    const onComplete = vi.fn()

    const stream = useTaskExecStream({
      sessionId,
      status,
      onComplete,
    })

    return { stream, sessionId, status, onComplete }
  }

  describe('startPreview', () => {
    it('subscribes to WS when session is running', () => {
      const { stream } = createStream()
      stream.startPreview()

      expect(mockSendWsMessage).toHaveBeenCalledWith({ type: 'subscribe', session_id: 'test-session-123' })
      expect(stream.isStreaming.value).toBe(true)
      expect(stream.streamingMsg.value).toBeTruthy()
    })

    it('does nothing when session ID is null', () => {
      const { stream } = createStream({ sessionId: null })
      stream.startPreview()

      expect(mockSendWsMessage).not.toHaveBeenCalled()
      expect(stream.isStreaming.value).toBe(false)
    })

    it('does nothing when status is not running', () => {
      const { stream } = createStream({ status: 'completed' })
      stream.startPreview()

      expect(mockSendWsMessage).not.toHaveBeenCalled()
      expect(stream.isStreaming.value).toBe(false)
    })
  })

  describe('stopPreview', () => {
    it('unsubscribes from WS and resets streaming state', () => {
      const { stream } = createStream()
      stream.startPreview()
      mockSendWsMessage.mockClear()

      stream.stopPreview()

      expect(mockSendWsMessage).toHaveBeenCalledWith({ type: 'unsubscribe', session_id: 'test-session-123' })
      expect(stream.isStreaming.value).toBe(false)
    })
  })

  describe('WS event handling', () => {
    it('ignores events for different sessions', () => {
      const { stream } = createStream()
      stream.startPreview()

      simulateWsEvent('content', { content: 'Hello' }, 'other-session')

      const msg = stream.streamingMsg.value
      expect(msg!.blocks).toHaveLength(0)

      stream.stopPreview()
    })

    it('accumulates content events into streaming message', () => {
      const { stream } = createStream()
      stream.startPreview()

      simulateWsEvent('content', { content: 'Hello ' })
      simulateWsEvent('content', { content: 'World!' })

      const msg = stream.streamingMsg.value
      expect(msg).toBeTruthy()
      expect(msg!.blocks).toHaveLength(1)
      expect(msg!.blocks[0].type).toBe('text')
      expect(msg!.blocks[0].text).toBe('Hello World!')

      stream.stopPreview()
    })

    it('handles thinking events', () => {
      const { stream } = createStream()
      stream.startPreview()

      simulateWsEvent('thinking', { text: 'Let me think...' })
      simulateWsEvent('thinking_done', {})

      const msg = stream.streamingMsg.value
      expect(msg!.blocks).toHaveLength(1)
      expect(msg!.blocks[0].type).toBe('thinking')
      expect(msg!.blocks[0].text).toBe('Let me think...')
      expect(msg!.blocks[0].done).toBe(true)

      stream.stopPreview()
    })

    it('keys a thinking block with the think_id from the payload', () => {
      // The task-exec stream reads the same WS payload as the main chat, which
      // now carries the block's identity. Without picking it up, the block is
      // keyed by position and cannot be linked to its chat_thinking prefix.
      const { stream } = createStream()
      stream.startPreview()

      simulateWsEvent('thinking', { text: 'first', think_id: 'th_task1' })

      // Assert on the OPENING delta, before any later delta's backfill could
      // mask a missing id: the block must carry its identity from the push that
      // created it, not acquire one later.
      expect(stream.streamingMsg.value!.blocks[0].think_id).toBe('th_task1')

      simulateWsEvent('thinking', { text: ' second', think_id: 'th_task1' })

      const msg = stream.streamingMsg.value
      expect(msg!.blocks).toHaveLength(1)
      expect(msg!.blocks[0].think_id).toBe('th_task1')
      expect(msg!.blocks[0].text).toBe('first second')

      stream.stopPreview()
    })

    it('tolerates a thinking payload with no think_id', () => {
      // Version skew: a server older than the field must not break the stream.
      const { stream } = createStream()
      stream.startPreview()

      simulateWsEvent('thinking', { text: 'reasoning' })

      const msg = stream.streamingMsg.value
      expect(msg!.blocks).toHaveLength(1)
      expect(msg!.blocks[0].think_id).toBeUndefined()
      expect(msg!.blocks[0].text).toBe('reasoning')

      stream.stopPreview()
    })

    it('backfills a missing think_id on a later delta', () => {
      // VERSION-SKEW GUARD: a block that got an id-less start and later receives
      // an id picks it up from the next delta.
      const { stream } = createStream()
      stream.startPreview()

      simulateWsEvent('thinking', { text: 'a' })
      expect(stream.streamingMsg.value!.blocks[0].think_id).toBeUndefined()

      simulateWsEvent('thinking', { text: 'b', think_id: 'th_first' })
      expect(stream.streamingMsg.value!.blocks).toHaveLength(1)
      expect(stream.streamingMsg.value!.blocks[0].think_id).toBe('th_first')
      expect(stream.streamingMsg.value!.blocks[0].text).toBe('ab')

      stream.stopPreview()
    })

    it('starts a NEW block when the delta carries a different think_id', () => {
      // A different id is a different block by the backend's own definition;
      // merging them would put two blocks' text under one v-for key.
      const { stream } = createStream()
      stream.startPreview()

      simulateWsEvent('thinking', { text: 'first', think_id: 'th_one' })
      simulateWsEvent('thinking', { text: 'second', think_id: 'th_two' })

      const blocks = stream.streamingMsg.value!.blocks
      expect(blocks, 'two distinct blocks').toHaveLength(2)
      expect(blocks[0]).toMatchObject({ think_id: 'th_one', text: 'first' })
      expect(blocks[1]).toMatchObject({ think_id: 'th_two', text: 'second' })

      stream.stopPreview()
    })

    it('appends to its own block even when a tool_use sits after it', () => {
      // The reported "top thinking block frozen forever", in the TASK stream.
      // A sub-agent's tool_use interleaves on the wire, so the positional scan
      // stops at it, finds nothing, and used to open a SECOND block carrying the
      // SAME think_id — two blocks, one v-for key, head frozen.
      const { stream } = createStream()
      stream.startPreview()

      simulateWsEvent('thinking', { text: 'TOP', think_id: 'th_X' })
      // done:false CREATES the block (a done:true for an unknown tool only
      // clears a watchdog and appends nothing — using it here left the array
      // without the boundary and made the test pass against the buggy code).
      simulateWsEvent('tool_use', { id: 'sub1', name: 'Bash', input: { cmd: 'ls' }, done: false })
      simulateWsEvent('thinking', { text: 'MORE', think_id: 'th_X' })

      const blocks = stream.streamingMsg.value!.blocks
      const thinks = blocks.filter((b) => b.type === 'thinking')
      expect(thinks, 'extends the existing block, does not open another').toHaveLength(1)
      expect(thinks[0].text).toBe('TOPMORE')

      const ids = thinks.map((b) => b.think_id)
      expect(new Set(ids).size, 'think_id must not repeat').toBe(ids.length)

      stream.stopPreview()
    })

    it('handles tool_use events', () => {
      const { stream } = createStream()
      stream.startPreview()

      simulateWsEvent('tool_use', { name: 'ReadFile', id: 'tool-1', status: 'running', summary: 'Reading foo.go' })

      const msg = stream.streamingMsg.value
      expect(msg!.blocks).toHaveLength(1)
      expect(msg!.blocks[0].type).toBe('tool_use')
      expect(msg!.blocks[0].name).toBe('ReadFile')
      expect(msg!.blocks[0].done).toBe(false)
      expect(msg!.blocks[0].summary).toBe('Reading foo.go')

      // Mark done
      simulateWsEvent('tool_use', { id: 'tool-1', done: true, status: 'completed' })
      expect(msg!.blocks[0].done).toBe(true)

      stream.stopPreview()
    })

    it('copies input onto a new tool_use block so interactive cards (PermissionApproval) can render', () => {
      const { stream } = createStream()
      stream.startPreview()

      const input = {
        session_id: 'test-session-123',
        toolCallId: 'tool-call-1',
        permissionId: 'perm_tool-call-1',
        toolName: 'ReadFile',
        toolInput: '{"file_path":"/tmp/x"}',
        options: [{ name: '允许一次', kind: 'allow_once', optionId: 'opt-1' }],
      }
      simulateWsEvent('tool_use', { name: 'PermissionApproval', id: 'perm_tool-call-1', done: false, input })

      const msg = stream.streamingMsg.value
      const toolBlock = msg!.blocks.find((b: any) => b.id === 'perm_tool-call-1')
      expect(toolBlock).toBeTruthy()
      expect(toolBlock.input).toEqual(input)

      stream.stopPreview()
    })

    it('updates input on an existing tool_use block when a later event carries it', () => {
      const { stream } = createStream()
      stream.startPreview()

      // First event without input (e.g. an early tool_use notification)
      simulateWsEvent('tool_use', { name: 'PermissionApproval', id: 'perm_1', done: false })

      // Follow-up event with the full input
      const input = { toolName: 'Bash', options: [{ name: '允许', kind: 'allow_once', optionId: 'opt-1' }] }
      simulateWsEvent('tool_use', { name: 'PermissionApproval', id: 'perm_1', done: false, input })

      const msg = stream.streamingMsg.value
      const toolBlock = msg!.blocks.find((b: any) => b.id === 'perm_1')
      expect(toolBlock.input).toEqual(input)

      stream.stopPreview()
    })

    it('handles tool_result events', () => {
      const { stream } = createStream()
      stream.startPreview()

      simulateWsEvent('tool_use', { name: 'ReadFile', id: 'tool-1', done: false })
      simulateWsEvent('tool_result', { id: 'tool-1', name: 'ReadFile', status: 'completed' })

      const msg = stream.streamingMsg.value
      const toolBlock = msg!.blocks.find((b: any) => b.id === 'tool-1')
      expect(toolBlock.done).toBe(true)

      stream.stopPreview()
    })

    it('copies duration_ms from tool_result onto the block', () => {
      const { stream } = createStream()
      stream.startPreview()

      simulateWsEvent('tool_use', { name: 'Bash', id: 'tool-dur', done: false })
      simulateWsEvent('tool_result', { id: 'tool-dur', status: 'success', duration_ms: 6700 })

      const msg = stream.streamingMsg.value
      const toolBlock = msg!.blocks.find((b: any) => b.id === 'tool-dur')
      expect(toolBlock.done).toBe(true)
      expect(toolBlock.duration_ms).toBe(6700)

      stream.stopPreview()
    })

    it('handles stream_start event', () => {
      const { stream } = createStream()
      stream.startPreview()

      simulateWsEvent('stream_start', { message_id: 42 })

      const msg = stream.streamingMsg.value
      expect(msg!.id).toBe(42)

      stream.stopPreview()
    })



    it('handles metadata event', () => {
      const { stream } = createStream()
      stream.startPreview()

      simulateWsEvent('metadata', { tokens: 100, cost: 0.05 })

      const msg = stream.streamingMsg.value
      expect(msg!.metadata).toEqual({ tokens: 100, cost: 0.05 })

      stream.stopPreview()
    })

    it('handles done event and calls onComplete', () => {
      const { stream, onComplete } = createStream()
      stream.startPreview()

      simulateWsEvent('done', {})

      expect(onComplete).toHaveBeenCalledTimes(1)
      expect(stream.isStreaming.value).toBe(false)
    })

    it('handles cancelled event and calls onComplete', () => {
      const { stream, onComplete } = createStream()
      stream.startPreview()

      simulateWsEvent('cancelled', {})

      expect(onComplete).toHaveBeenCalledTimes(1)
      expect(stream.isStreaming.value).toBe(false)
      expect(stream.streamingMsg.value?.cancelled).toBe(true)
    })

    it('handles error event and calls onComplete', () => {
      const { stream, onComplete } = createStream()
      stream.startPreview()

      simulateWsEvent('error', { error: 'Something went wrong' })

      expect(onComplete).toHaveBeenCalledTimes(1)
      expect(stream.isStreaming.value).toBe(false)
    })

    it('tracks tool_use timeout timers', () => {
      const { stream } = createStream()
      stream.startPreview()

      simulateWsEvent('tool_use', { name: 'ReadFile', id: 'tool-1', done: false })

      const msg = stream.streamingMsg.value
      expect(msg!.blocks[0].done).toBe(false)

      // Verify tool_use timeout is set up by checking that the block
      // can be marked done when the timer fires (tested via tool_result)
      simulateWsEvent('tool_result', { id: 'tool-1', name: 'ReadFile', status: 'completed' })
      expect(msg!.blocks[0].done).toBe(true)

      stream.stopPreview()
    })
  })

  describe('WS reconnect', () => {
    it('re-subscribes when WS reconnects during streaming', async () => {
      const { stream } = createStream()
      stream.startPreview()
      mockSendWsMessage.mockClear()

      // Simulate disconnect
      mockConnected.value = false
      await nextTick()

      // Simulate reconnect
      mockSendWsMessage.mockClear()
      mockConnected.value = true
      await nextTick()

      expect(mockSendWsMessage).toHaveBeenCalledWith({ type: 'subscribe', session_id: 'test-session-123' })
    })

    it('does not re-subscribe when not streaming', async () => {
      const { stream } = createStream()
      // Not started, so reconnect should not subscribe
      mockConnected.value = false
      await nextTick()
      mockConnected.value = true
      await nextTick()

      expect(mockSendWsMessage).not.toHaveBeenCalledWith(expect.objectContaining({ type: 'subscribe' }))
    })
  })
})
