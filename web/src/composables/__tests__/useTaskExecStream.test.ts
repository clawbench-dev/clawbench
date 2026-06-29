import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest'
import { ref, nextTick } from 'vue'
import { useTaskExecStream } from '@/composables/useTaskExecStream'

// Mock EventSource for SSE tests
class MockEventSource {
  static instances: MockEventSource[] = []
  url: string
  onerror: ((ev?: any) => void) | null = null
  private listeners: Map<string, Set<(e: any) => void>> = new Map()
  readyState = 0 // CONNECTING

  constructor(url: string) {
    this.url = url
    MockEventSource.instances.push(this)
  }

  addEventListener(type: string, handler: (e: any) => void) {
    if (!this.listeners.has(type)) this.listeners.set(type, new Set())
    this.listeners.get(type)!.add(handler)
  }

  close() {
    this.readyState = 2 // CLOSED
    const idx = MockEventSource.instances.indexOf(this)
    if (idx !== -1) MockEventSource.instances.splice(idx, 1)
  }

  // Test helper: simulate receiving an SSE event
  _emit(type: string, data: any) {
    const handlers = this.listeners.get(type)
    if (handlers) {
      const event = { data: JSON.stringify(data) }
      for (const h of handlers) h(event)
    }
  }
}

describe('useTaskExecStream', () => {
  let originalEventSource: typeof EventSource

  beforeEach(() => {
    originalEventSource = globalThis.EventSource
    // @ts-expect-error mock
    globalThis.EventSource = MockEventSource
    MockEventSource.instances = []
    vi.useFakeTimers()
    // Suppress Vue lifecycle warnings in test context
    vi.spyOn(console, 'warn').mockImplementation(() => {})
  })

  afterEach(() => {
    globalThis.EventSource = originalEventSource
    vi.useRealTimers()
    MockEventSource.instances = []
  })

  function createStream(overrides?: { sessionId?: string | null; status?: string }) {
    const sessionId = ref<string | null>(overrides?.sessionId !== undefined ? overrides.sessionId : 'test-session-123')
    const status = ref<string>(overrides?.status ?? 'running')
    const onRefresh = vi.fn().mockResolvedValue(undefined)
    const onComplete = vi.fn()

    const stream = useTaskExecStream({
      sessionId,
      status,
      onRefresh,
      onComplete,
    })

    return { stream, sessionId, status, onRefresh, onComplete }
  }

  describe('startPreview', () => {
    it('creates SSE connection when session is running', () => {
      const { stream } = createStream()
      stream.startPreview()

      expect(MockEventSource.instances.length).toBe(1)
      expect(MockEventSource.instances[0].url).toContain('session_id=test-session-123')
      expect(stream.isStreaming.value).toBe(true)
      expect(stream.isPolling.value).toBe(false)

      stream.stopPreview()
    })

    it('does nothing when session ID is null', () => {
      const { stream } = createStream({ sessionId: null as any })
      stream.startPreview()

      // No new EventSource should have been created for null session
      const hasSSEForNull = MockEventSource.instances.some(es => !es.url.includes('test-session'))
      expect(hasSSEForNull).toBe(false)
      expect(stream.isStreaming.value).toBe(false)
    })

    it('does nothing when status is not running', () => {
      const { stream } = createStream({ status: 'completed' })
      stream.startPreview()

      expect(MockEventSource.instances.length).toBe(0)
      expect(stream.isStreaming.value).toBe(false)
    })
  })

  describe('SSE event handling', () => {
    it('accumulates content events into streaming message', async () => {
      const { stream } = createStream()
      stream.startPreview()

      const es = MockEventSource.instances[0]

      es._emit('content', { content: 'Hello ' })
      es._emit('content', { content: 'World!' })

      const msg = stream.streamingMsg.value
      expect(msg).toBeTruthy()
      expect(msg.blocks).toHaveLength(1)
      expect(msg.blocks[0].type).toBe('text')
      expect(msg.blocks[0].text).toBe('Hello World!')

      stream.stopPreview()
    })

    it('handles thinking events', () => {
      const { stream } = createStream()
      stream.startPreview()

      const es = MockEventSource.instances[0]
      es._emit('thinking', { text: 'Let me think...' })
      es._emit('thinking_done', {})

      const msg = stream.streamingMsg.value
      expect(msg.blocks).toHaveLength(1)
      expect(msg.blocks[0].type).toBe('thinking')
      expect(msg.blocks[0].text).toBe('Let me think...')
      expect(msg.blocks[0].done).toBe(true)

      stream.stopPreview()
    })

    it('handles tool_use events', () => {
      const { stream } = createStream()
      stream.startPreview()

      const es = MockEventSource.instances[0]
      es._emit('tool_use', { name: 'ReadFile', id: 'tool-1', status: 'running', summary: 'Reading foo.go' })

      const msg = stream.streamingMsg.value
      expect(msg.blocks).toHaveLength(1)
      expect(msg.blocks[0].type).toBe('tool_use')
      expect(msg.blocks[0].name).toBe('ReadFile')
      expect(msg.blocks[0].done).toBe(false)
      expect(msg.blocks[0].summary).toBe('Reading foo.go')

      // Mark done
      es._emit('tool_use', { id: 'tool-1', done: true, status: 'completed' })
      expect(msg.blocks[0].done).toBe(true)

      stream.stopPreview()
    })

    it('handles done event and calls onComplete', () => {
      const { stream, onComplete } = createStream()
      stream.startPreview()

      const es = MockEventSource.instances[0]
      es._emit('done', {})

      expect(onComplete).toHaveBeenCalledTimes(1)
      expect(stream.isStreaming.value).toBe(false)
    })

    it('handles cancelled event and calls onComplete', () => {
      const { stream, onComplete } = createStream()
      stream.startPreview()

      const es = MockEventSource.instances[0]
      es._emit('cancelled', {})

      expect(onComplete).toHaveBeenCalledTimes(1)
      expect(stream.isStreaming.value).toBe(false)
    })

    it('falls back to polling on sse_busy error', () => {
      const { stream, onRefresh } = createStream()
      stream.startPreview()

      const es = MockEventSource.instances[0]
      // Simulate sse_busy error event
      const handlers = (es as any).listeners.get('error')
      if (handlers) {
        for (const h of handlers) h({ data: JSON.stringify({ reason: 'sse_busy' }) })
      }

      expect(stream.isPolling.value).toBe(true)
      expect(onRefresh).toHaveBeenCalledTimes(1) // immediate poll on fallback

      stream.stopPreview()
    })
  })

  describe('polling fallback', () => {
    it('polls onRefresh every 3 seconds', () => {
      const { stream, onRefresh, status } = createStream()
      stream.startPreview()

      // Simulate SSE failure → fallback to polling
      const es = MockEventSource.instances[0]
      es.onerror?.()

      expect(stream.isPolling.value).toBe(true)
      expect(onRefresh).toHaveBeenCalledTimes(1) // initial

      vi.advanceTimersByTime(3000)
      expect(onRefresh).toHaveBeenCalledTimes(2)

      vi.advanceTimersByTime(3000)
      expect(onRefresh).toHaveBeenCalledTimes(3)

      stream.stopPreview()
    })

    it('stops polling when status changes away from running', async () => {
      const { stream, onRefresh, status } = createStream()
      stream.startPreview()

      // Fallback to polling
      const es = MockEventSource.instances[0]
      es.onerror?.()

      expect(stream.isPolling.value).toBe(true)

      // Simulate execution completing
      status.value = 'completed'

      // Advance timers and flush the async poll callback
      vi.advanceTimersByTime(3000)
      await vi.runAllTimersAsync()

      expect(stream.isPolling.value).toBe(false)
      expect(stream.isStreaming.value).toBe(false)
    })
  })

  describe('stopPreview', () => {
    it('cleans up SSE connection', () => {
      const { stream } = createStream()
      stream.startPreview()

      expect(MockEventSource.instances.length).toBe(1)

      stream.stopPreview()

      expect(stream.isStreaming.value).toBe(false)
      // EventSource was closed
    })

    it('stops polling', () => {
      const { stream, onRefresh } = createStream()
      stream.startPreview()

      // Force into polling mode
      const es = MockEventSource.instances[0]
      es.onerror?.()

      stream.stopPreview()

      vi.advanceTimersByTime(3000)
      // No more polling calls after stop
      const callCount = onRefresh.mock.calls.length
      vi.advanceTimersByTime(3000)
      expect(onRefresh.mock.calls.length).toBe(callCount)
    })
  })

  describe('metadata', () => {
    it('captures metadata from SSE events', () => {
      const { stream } = createStream()
      stream.startPreview()

      const es = MockEventSource.instances[0]
      es._emit('metadata', { tokens: 100, cost: 0.05 })

      const msg = stream.streamingMsg.value
      expect(msg.metadata).toEqual({ tokens: 100, cost: 0.05 })

      stream.stopPreview()
    })
  })
})
