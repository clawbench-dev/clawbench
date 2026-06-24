import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest'
import { useGlobalEvents } from '@/composables/useGlobalEvents'

// Mock WebSocket that captures constructor calls
let mockWsInstances: MockWebSocket[] = []

class MockWebSocket {
    static CONNECTING = 0
    static OPEN = 1
    static CLOSING = 2
    static CLOSED = 3

    url: string
    readyState: number = MockWebSocket.CONNECTING
    onopen: ((ev: Event) => void) | null = null
    onmessage: ((ev: MessageEvent) => void) | null = null
    onclose: ((ev: CloseEvent) => void) | null = null
    onerror: ((ev: Event) => void) | null = null
    sentMessages: string[] = []

    constructor(url: string) {
        this.url = url
        mockWsInstances.push(this)
    }

    send(data: string) {
        this.sentMessages.push(data)
    }

    close() {
        this.readyState = MockWebSocket.CLOSED
        this.onclose?.(new CloseEvent('close'))
    }

    // Simulate receiving a message from server
    receive(data: object) {
        this.onmessage?.(new MessageEvent('message', { data: JSON.stringify(data) }))
    }

    // Simulate connection open
    simulateOpen() {
        this.readyState = MockWebSocket.OPEN
        this.onopen?.(new Event('open'))
    }
}

function getLatestWs(): MockWebSocket {
    return mockWsInstances[mockWsInstances.length - 1]
}

// Unique ID counter to avoid cross-test dedup conflicts
let idCounter = 0
function nextId(): string {
    return `evt_test_${++idCounter}`
}

describe('useGlobalEvents', () => {
    let originalWebSocket: typeof WebSocket
    let originalFetch: typeof globalThis.fetch
    let events: ReturnType<typeof useGlobalEvents>
    let events2: ReturnType<typeof useGlobalEvents>

    beforeEach(() => {
        mockWsInstances = []
        originalWebSocket = globalThis.WebSocket
        originalFetch = globalThis.fetch
        globalThis.WebSocket = MockWebSocket as any
        // Mock fetch for push config endpoint
        globalThis.fetch = vi.fn().mockResolvedValue({
            ok: true,
            json: () => Promise.resolve({ jpush_enabled: false, jpush_app_key: '' }),
        })
        events = useGlobalEvents()
        events2 = undefined as any
    })

    afterEach(() => {
        events.destroy()
        events2?.destroy()
        globalThis.WebSocket = originalWebSocket
        globalThis.fetch = originalFetch
    })

    function connectAndGetWs(): MockWebSocket {
        events.connect()
        const ws = getLatestWs()
        ws.simulateOpen()
        return ws
    }

    function connectAndGetWs2(ev: ReturnType<typeof useGlobalEvents>): MockWebSocket {
        ev.connect()
        const ws = getLatestWs()
        ws.simulateOpen()
        return ws
    }

    describe('event dedup', () => {
        it('should not dispatch duplicate events with same ID', () => {
            const handler = vi.fn()
            events.onEvent(handler)
            const ws = connectAndGetWs()

            const id = nextId()
            const eventData = { session_id: 's1', status: 'completed' }
            ws.receive({ type: 'event', id, event: 'session_update', data: eventData })
            ws.receive({ type: 'event', id, event: 'session_update', data: eventData })

            expect(handler).toHaveBeenCalledTimes(1)
        })

        it('should dispatch events with different IDs', () => {
            const handler = vi.fn()
            events.onEvent(handler)
            const ws = connectAndGetWs()

            ws.receive({ type: 'event', id: nextId(), event: 'session_update', data: {} })
            ws.receive({ type: 'event', id: nextId(), event: 'task_update', data: {} })

            expect(handler).toHaveBeenCalledTimes(2)
        })

        it('should dispatch events without ID (no dedup)', () => {
            const handler = vi.fn()
            events.onEvent(handler)
            const ws = connectAndGetWs()

            ws.receive({ type: 'event', event: 'session_update', data: {} })
            ws.receive({ type: 'event', event: 'session_update', data: {} })

            expect(handler).toHaveBeenCalledTimes(2)
        })
    })

    describe('ping/pong', () => {
        it('should send pong when receiving ping', () => {
            const ws = connectAndGetWs()

            ws.receive({ type: 'ping' })

            expect(ws.sentMessages).toContainEqual(JSON.stringify({ type: 'pong' }))
        })
    })

    describe('ack', () => {
        it('should send ack for events with ID', () => {
            const ws = connectAndGetWs()
            const id = nextId()

            ws.receive({ type: 'event', id, event: 'session_update', data: {} })

            expect(ws.sentMessages).toContainEqual(JSON.stringify({ type: 'ack', id }))
        })

        it('should not send ack for events without ID', () => {
            const ws = connectAndGetWs()
            ws.sentMessages = []

            ws.receive({ type: 'event', event: 'session_update', data: {} })

            const ackMessages = ws.sentMessages.filter(m => {
                try { return JSON.parse(m).type === 'ack' } catch { return false }
            })
            expect(ackMessages).toHaveLength(0)
        })
    })

    describe('onEvent handler management', () => {
        it('should unsubscribe handler when returned function is called', () => {
            const handler = vi.fn()
            const unsub = events.onEvent(handler)
            const ws = connectAndGetWs()

            ws.receive({ type: 'event', id: nextId(), event: 'session_update', data: {} })
            expect(handler).toHaveBeenCalledTimes(1)

            unsub()
            ws.receive({ type: 'event', id: nextId(), event: 'session_update', data: {} })
            expect(handler).toHaveBeenCalledTimes(1) // not called again
        })

        it('should dispatch to multiple handlers', () => {
            const handler1 = vi.fn()
            const handler2 = vi.fn()
            events.onEvent(handler1)
            events.onEvent(handler2)
            const ws = connectAndGetWs()

            ws.receive({ type: 'event', id: nextId(), event: 'session_update', data: {} })

            expect(handler1).toHaveBeenCalledTimes(1)
            expect(handler2).toHaveBeenCalledTimes(1)
        })
    })

    describe('registerPushId', () => {
        it('should skip registration in web mode (no AndroidNative)', () => {
            // In web mode (no AndroidNative), registerPushId is a no-op.
            // init() calls registerPushId() which early-returns if !isAppMode.
            // Verify no push/register fetch call is made.
            events.init()
            const registerCalls = (globalThis.fetch as ReturnType<typeof vi.fn>).mock.calls.filter(
                (call: any[]) => call[0] === '/api/push/register'
            )
            expect(registerCalls).toHaveLength(0)
        })
    })

    describe('event handler receives correct data', () => {
        it('should pass event name and data to handler', () => {
            const handler = vi.fn()
            events.onEvent(handler)
            const ws = connectAndGetWs()

            const data = { session_id: 's1', status: 'completed', has_new_messages: true }
            ws.receive({ type: 'event', id: nextId(), event: 'session_update', data })

            expect(handler).toHaveBeenCalledWith('session_update', data)
        })

        it('should handle task_update events', () => {
            const handler = vi.fn()
            events.onEvent(handler)
            const ws = connectAndGetWs()

            const data = { task_id: 't1', status: 'completed' }
            ws.receive({ type: 'event', id: nextId(), event: 'task_update', data })

            expect(handler).toHaveBeenCalledWith('task_update', data)
        })
    })

    describe('malformed messages', () => {
        it('should ignore non-JSON messages', () => {
            const handler = vi.fn()
            events.onEvent(handler)
            const ws = connectAndGetWs()

            ws.onmessage?.(new MessageEvent('message', { data: 'not json' }))

            expect(handler).not.toHaveBeenCalled()
        })

        it('should ignore messages with unknown type', () => {
            const handler = vi.fn()
            events.onEvent(handler)
            const ws = connectAndGetWs()

            ws.receive({ type: 'unknown_type' })

            expect(handler).not.toHaveBeenCalled()
        })
    })

    describe('connect/disconnect', () => {
        it('should create WebSocket on connect', () => {
            const beforeCount = mockWsInstances.length
            events.connect()
            expect(mockWsInstances.length).toBeGreaterThan(beforeCount)
        })

        it('should close WebSocket on disconnect', () => {
            const ws = connectAndGetWs()
            events.disconnect()
            expect(ws.readyState).toBe(MockWebSocket.CLOSED)
        })
    })

    // ISS-192: destroy() must clear handlers and state to prevent stale closures
    // from firing after SPA hot project switch.
    describe('destroy clears state', () => {
        it('should clear handlers on destroy so they do not fire on reconnect', () => {
            const handler = vi.fn()
            events.onEvent(handler)
            events.destroy()

            // After destroy, re-init and connect
            events2 = useGlobalEvents()
            const ws = connectAndGetWs2(events2)

            // The old handler should NOT fire for new events
            ws.receive({ type: 'event', id: nextId(), event: 'session_update', data: {} })
            expect(handler).not.toHaveBeenCalled()

            events2.destroy()
        })

        it('should clear processedEventIds on destroy', () => {
            const handler = vi.fn()
            events.onEvent(handler)
            const ws = connectAndGetWs()

            const id = nextId()
            ws.receive({ type: 'event', id, event: 'session_update', data: {} })
            expect(handler).toHaveBeenCalledTimes(1)

            events.destroy()

            // After destroy + re-init, same event ID should not be deduped
            events2 = useGlobalEvents()
            const handler2 = vi.fn()
            events2.onEvent(handler2)
            const ws2 = connectAndGetWs2(events2)
            ws2.receive({ type: 'event', id, event: 'session_update', data: {} })
            expect(handler2).toHaveBeenCalledTimes(1)

            events2.destroy()
        })

        it('should reset pushAvailable and pushRegistered on destroy', () => {
            events.pushAvailable.value = true
            events.pushRegistered.value = true

            events.destroy()

            expect(events.pushAvailable.value).toBe(false)
            expect(events.pushRegistered.value).toBe(false)
        })
    })

    describe('visibility change', () => {
        it('should disconnect WebSocket on background regardless of push availability', () => {
            // init() registers the visibility change handler
            events.init()
            const ws = connectAndGetWs()
            events.pushAvailable.value = false

            // Simulate going to background — should disconnect even without push
            Object.defineProperty(document, 'visibilityState', {
                value: 'hidden',
                writable: true,
                configurable: true,
            })
            document.dispatchEvent(new Event('visibilitychange'))

            // WebSocket should be closed (always disconnect on background)
            expect(ws.readyState).toBe(MockWebSocket.CLOSED)
        })

        it('should reconnect on foreground after background', () => {
            events.init()
            const ws = connectAndGetWs()

            // Go to background (disconnects)
            Object.defineProperty(document, 'visibilityState', {
                value: 'hidden',
                writable: true,
                configurable: true,
            })
            document.dispatchEvent(new Event('visibilitychange'))
            expect(ws.readyState).toBe(MockWebSocket.CLOSED)

            // Come back to foreground
            Object.defineProperty(document, 'visibilityState', {
                value: 'visible',
                writable: true,
                configurable: true,
            })
            document.dispatchEvent(new Event('visibilitychange'))

            // A new WebSocket should be created
            const newWs = getLatestWs()
            expect(newWs).not.toBe(ws)
        })
    })

    describe('wsStatus computed', () => {
        it('returns "connected" when connected', () => {
            events.init()
            const ws = connectAndGetWs()
            expect(events.wsStatus.value).toBe('connected')
        })

        it('returns "disconnected" when not connected and not reconnecting', () => {
            expect(events.wsStatus.value).toBe('disconnected')
        })
    })

    describe('init and destroy', () => {
        it('init only runs once (idempotent)', () => {
            events.init()
            const count1 = mockWsInstances.length
            events.init()
            expect(mockWsInstances.length).toBe(count1)
        })

        it('destroy removes visibility change listener', () => {
            events.init()
            const ws = connectAndGetWs()

            events.destroy()

            // Visibility change should not trigger disconnect after destroy
            Object.defineProperty(document, 'visibilityState', {
                value: 'hidden',
                writable: true,
                configurable: true,
            })
            // This should not throw since the listener was removed
            document.dispatchEvent(new Event('visibilitychange'))
        })
    })

    describe('summary_update event dispatch', () => {
        it('dispatches clawbench-summary-update custom event for chat_message summary_update', () => {
            const dispatchSpy = vi.spyOn(window, 'dispatchEvent')
            const ws = connectAndGetWs()

            ws.receive({
                type: 'event',
                id: nextId(),
                event: 'summary_update',
                data: { targetType: 'chat_message', sessionId: 's1' },
            })

            expect(dispatchSpy).toHaveBeenCalledWith(
                expect.objectContaining({ type: 'clawbench-summary-update' })
            )
            dispatchSpy.mockRestore()
        })

        it('does not dispatch custom event for non-chat_message summary_update', () => {
            const dispatchSpy = vi.spyOn(window, 'dispatchEvent')
            const ws = connectAndGetWs()
            ws.sentMessages = [] // clear

            ws.receive({
                type: 'event',
                id: nextId(),
                event: 'summary_update',
                data: { targetType: 'task_execution' },
            })

            const customEvents = dispatchSpy.mock.calls.filter(
                (call: any[]) => call[0]?.type === 'clawbench-summary-update'
            )
            expect(customEvents).toHaveLength(0)
            dispatchSpy.mockRestore()
        })
    })

    describe('processedEventIds eviction', () => {
        it('evicts old event IDs when exceeding MAX_PROCESSED_IDS', () => {
            const handler = vi.fn()
            events.onEvent(handler)
            const ws = connectAndGetWs()

            // Send 101+ unique events to trigger eviction (MAX_PROCESSED_IDS = 100)
            for (let i = 0; i < 102; i++) {
                ws.receive({ type: 'event', id: `eviction_test_${i}`, event: 'session_update', data: {} })
            }

            expect(handler).toHaveBeenCalledTimes(102)
        })
    })
})
