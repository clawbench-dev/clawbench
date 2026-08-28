import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest'

// Mock dependencies before importing useGlobalEvents
const mockShowBrowserNotification = vi.fn()
vi.mock('@/composables/useNotification', () => ({
    showBrowserNotification: (...args: unknown[]) => mockShowBrowserNotification(...args),
}))

const mockPlayNotificationSound = vi.fn()
vi.mock('@/composables/useNotificationSound', () => ({
    playNotificationSound: () => mockPlayNotificationSound(),
}))

vi.mock('@/composables/useLocale', () => ({
    gt: (key: string) => key, // Return key itself for test assertions
}))

// Mutable flag for app mode — use plain object since vi.mock is hoisted
const appModeState = { value: false }
vi.mock('@/composables/useAppMode', () => ({
    useAppMode: () => ({
        isAppMode: { get value() { return appModeState.value }, set value(v: boolean) { appModeState.value = v } }
    }),
}))

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
    let events: ReturnType<typeof useGlobalEvents>
    let events2: ReturnType<typeof useGlobalEvents>

    beforeEach(() => {
        mockWsInstances = []
        mockShowBrowserNotification.mockReset()
        mockPlayNotificationSound.mockReset()
        appModeState.value = false  // Default to browser mode
        originalWebSocket = globalThis.WebSocket
        globalThis.WebSocket = MockWebSocket as any
        events = useGlobalEvents()
        events2 = undefined as any
    })

    afterEach(() => {
        events.destroy()
        events2?.destroy()
        globalThis.WebSocket = originalWebSocket
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

    describe('heartbeat', () => {
        const CHECK = 15000
        const STALE = 90000
        const EVENT_STALE = 60000

        it('reconnects when no ping received within the stale window', () => {
            vi.useFakeTimers()
            const ws = connectAndGetWs()
            // Advance past the stale window with no ping delivered.
            vi.advanceTimersByTime(STALE + CHECK)
            // Should have force-disconnected and scheduled a reconnect.
            expect(ws.readyState).toBe(MockWebSocket.CLOSED)
            vi.useRealTimers()
        })

        it('does not disconnect while pings arrive regularly', () => {
            vi.useFakeTimers()
            const ws = connectAndGetWs()
            // Deliver pings at an interval below the stale window.
            for (let i = 0; i < 20; i++) {
                vi.advanceTimersByTime(CHECK)
                ws.receive({ type: 'ping' })
            }
            expect(ws.readyState).toBe(MockWebSocket.OPEN)
            vi.useRealTimers()
        })

        it('resets staleness on each message (a single delayed message does not trip it)', () => {
            vi.useFakeTimers()
            const ws = connectAndGetWs()
            // Advance near the event-stale threshold then deliver a ping — the timer restarts.
            vi.advanceTimersByTime(EVENT_STALE - 1000)
            ws.receive({ type: 'ping' })
            // Advancing past the original threshold must NOT disconnect now.
            vi.advanceTimersByTime(2000)
            expect(ws.readyState).toBe(MockWebSocket.OPEN)
            // But without further messages it eventually reconnects.
            vi.advanceTimersByTime(EVENT_STALE + CHECK)
            expect(ws.readyState).toBe(MockWebSocket.CLOSED)
            vi.useRealTimers()
        })

        it('reconnects when the socket is silently dead (no ping OR event) before the 90s ping window', () => {
            vi.useFakeTimers()
            const ws = connectAndGetWs()
            // No messages at all. The event-stale window (60s) must trigger a
            // reconnect BEFORE the ping-only window (90s) — closing the
            // "socket alive but events silently dropped" stale-state gap.
            // 60s + one 15s check interval puts us past EVENT_STALE but under STALE.
            vi.advanceTimersByTime(EVENT_STALE + CHECK)
            expect(ws.readyState).toBe(MockWebSocket.CLOSED)
            vi.useRealTimers()
        })

        it('does not disconnect when real events keep arriving within the event-stale window', () => {
            vi.useFakeTimers()
            const ws = connectAndGetWs()
            // Deliver real events every 15s (below the 60s event-stale window) —
            // even without pings the connection is treated as alive.
            for (let i = 0; i < 5; i++) {
                vi.advanceTimersByTime(CHECK)
                ws.receive({ type: 'event', id: nextId(), event: 'session_update', data: { status: 'running' } })
            }
            expect(ws.readyState).toBe(MockWebSocket.OPEN)
            vi.useRealTimers()
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
    })

    describe('visibility change', () => {
        it('should disconnect WebSocket on background in app mode', () => {
            appModeState.value = true  // App mode: disconnect on background
            // init() registers the visibility change handler
            events.init()
            const ws = connectAndGetWs()

            // Simulate going to background — should disconnect in app mode
            Object.defineProperty(document, 'visibilityState', {
                value: 'hidden',
                writable: true,
                configurable: true,
            })
            document.dispatchEvent(new Event('visibilitychange'))

            // WebSocket should be closed (app mode always disconnects on background)
            expect(ws.readyState).toBe(MockWebSocket.CLOSED)
        })

        it('should keep WebSocket alive on background in browser mode', () => {
            appModeState.value = false  // Browser mode: keep WS alive for notifications
            events.init()
            const ws = connectAndGetWs()

            // Simulate going to background
            Object.defineProperty(document, 'visibilityState', {
                value: 'hidden',
                writable: true,
                configurable: true,
            })
            document.dispatchEvent(new Event('visibilitychange'))

            // WebSocket should still be open (browser mode keeps WS alive)
            expect(ws.readyState).toBe(MockWebSocket.OPEN)
        })

        it('should reconnect on foreground after background', () => {
            appModeState.value = true  // App mode for disconnect behavior
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

        it('should reset reconnect state atomically on foreground (no stale disabled)', () => {
            appModeState.value = true
            events.init()
            const ws = connectAndGetWs()

            // Go to background — disconnect + disable reconnect
            Object.defineProperty(document, 'visibilityState', {
                value: 'hidden',
                writable: true,
                configurable: true,
            })
            document.dispatchEvent(new Event('visibilitychange'))
            expect(ws.readyState).toBe(MockWebSocket.CLOSED)

            // Simulate onclose firing after disconnect (normal behavior)
            // Before the fix, reconnect was disabled and the onclose handler
            // would not schedule reconnect. Now, on foreground we always reset.
            ws.onclose?.(new CloseEvent('close'))

            // Come back to foreground
            Object.defineProperty(document, 'visibilityState', {
                value: 'visible',
                writable: true,
                configurable: true,
            })
            document.dispatchEvent(new Event('visibilitychange'))

            // A new WebSocket should be created — reconnect.reset() was called
            // atomically in the visible branch, so disabled state is cleared.
            const newWs = getLatestWs()
            expect(newWs).not.toBe(ws)

            // Verify reconnect is not disabled by simulating a failed connect:
            // if the new WS closes, shouldReconnect() should return true
            newWs.onclose?.(new CloseEvent('close'))
            // With the old code, reconnect was disabled and would never retry.
            // With the new code, reconnect.reset() in the visible branch
            // ensures the reconnect system is always ready on foreground.
            expect(events.wsStatus.value).not.toBe('disconnected') // should be 'reconnecting'
        })

        it('foreground always resets reconnect even if WS was already connected', () => {
            appModeState.value = true
            events.init()
            const ws = connectAndGetWs()

            // Go to background
            Object.defineProperty(document, 'visibilityState', {
                value: 'hidden',
                writable: true,
                configurable: true,
            })
            document.dispatchEvent(new Event('visibilitychange'))

            // Come back to foreground
            Object.defineProperty(document, 'visibilityState', {
                value: 'visible',
                writable: true,
                configurable: true,
            })
            document.dispatchEvent(new Event('visibilitychange'))

            // A new WebSocket was created — verify reconnect is not stuck
            // in disabled state by checking that if the new WS fails,
            // reconnect.shouldReconnect() returns true (not disabled).
            const newWs = getLatestWs()
            expect(newWs).not.toBe(ws)
            // Simulate connection failure — onclose should schedule reconnect
            newWs.onclose?.(new CloseEvent('close'))
            // wsStatus should be 'reconnecting' (not stuck at 'disconnected'
            // with disabled=true as in the old code)
            expect(events.wsStatus.value).toBe('reconnecting')
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

    describe('hasConnectedOnce', () => {
        it('is false before any connection', () => {
            expect(events.hasConnectedOnce.value).toBe(false)
        })

        it('becomes true after first successful connection', () => {
            connectAndGetWs()
            expect(events.hasConnectedOnce.value).toBe(true)
        })

        it('resets to false on destroy', () => {
            connectAndGetWs()
            expect(events.hasConnectedOnce.value).toBe(true)
            events.destroy()
            expect(events.hasConnectedOnce.value).toBe(false)
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

    describe('reconnect state refresh event', () => {
        it('does NOT dispatch clawbench-reconnect on the first connect (startup already loads state)', () => {
            const dispatchSpy = vi.spyOn(window, 'dispatchEvent')
            connectAndGetWs()

            expect(dispatchSpy).not.toHaveBeenCalledWith(
                expect.objectContaining({ type: 'clawbench-reconnect' })
            )
            dispatchSpy.mockRestore()
        })

        it('dispatches clawbench-reconnect on a reconnect (after a disconnect)', () => {
            const dispatchSpy = vi.spyOn(window, 'dispatchEvent')

            // First connection — no event
            connectAndGetWs()
            expect(dispatchSpy).not.toHaveBeenCalledWith(
                expect.objectContaining({ type: 'clawbench-reconnect' })
            )

            // Disconnect, then reconnect — the event fires
            events.disconnect()
            connectAndGetWs()
            expect(dispatchSpy).toHaveBeenCalledWith(
                expect.objectContaining({ type: 'clawbench-reconnect' })
            )

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

    describe('browser notification on WS events', () => {
        it('shows browser notification for session completed when page not focused', () => {
            // Simulate page in background
            vi.spyOn(document, 'visibilityState', 'get').mockReturnValue('hidden')
            vi.spyOn(document, 'hasFocus').mockReturnValue(false)

            const ws = connectAndGetWs()
            ws.receive({
                type: 'event',
                id: nextId(),
                event: 'session_update',
                data: { session_id: 's1', status: 'completed', session_title: 'My Task', response_preview: 'Done!' },
            })

            expect(mockShowBrowserNotification).toHaveBeenCalledTimes(1)
            expect(mockShowBrowserNotification).toHaveBeenCalledWith(
                'chat.push.sessionCompleted',
                expect.objectContaining({
                    body: 'Done!',
                    tag: expect.stringContaining('clawbench-session_update-s1'),
                })
            )
            expect(mockPlayNotificationSound).toHaveBeenCalledTimes(1)
        })

        it('shows browser notification for session cancelled', () => {
            vi.spyOn(document, 'visibilityState', 'get').mockReturnValue('hidden')
            vi.spyOn(document, 'hasFocus').mockReturnValue(false)

            const ws = connectAndGetWs()
            ws.receive({
                type: 'event',
                id: nextId(),
                event: 'session_update',
                data: { session_id: 's2', status: 'cancelled' },
            })

            expect(mockShowBrowserNotification).toHaveBeenCalledTimes(1)
        })

        it('shows browser notification for permission_pending with tool name', () => {
            vi.spyOn(document, 'visibilityState', 'get').mockReturnValue('hidden')
            vi.spyOn(document, 'hasFocus').mockReturnValue(false)

            const ws = connectAndGetWs()
            ws.receive({
                type: 'event',
                id: nextId(),
                event: 'session_update',
                data: { session_id: 's3', status: 'permission_pending', tool_name: 'Bash' },
            })

            expect(mockShowBrowserNotification).toHaveBeenCalledTimes(1)
            expect(mockShowBrowserNotification).toHaveBeenCalledWith(
                'chat.push.actionRequired',
                expect.objectContaining({ body: 'Bash' })
            )
        })

        it('shows browser notification for task completed', () => {
            vi.spyOn(document, 'visibilityState', 'get').mockReturnValue('hidden')
            vi.spyOn(document, 'hasFocus').mockReturnValue(false)

            const ws = connectAndGetWs()
            ws.receive({
                type: 'event',
                id: nextId(),
                event: 'task_update',
                data: { task_id: '5', status: 'completed', session_title: 'Nightly build', response_preview: 'All tests pass' },
            })

            expect(mockShowBrowserNotification).toHaveBeenCalledTimes(1)
            expect(mockShowBrowserNotification).toHaveBeenCalledWith(
                'chat.push.taskCompleted',
                expect.objectContaining({ body: 'All tests pass' })
            )
        })

        it('shows browser notification for task failed', () => {
            vi.spyOn(document, 'visibilityState', 'get').mockReturnValue('hidden')
            vi.spyOn(document, 'hasFocus').mockReturnValue(false)

            const ws = connectAndGetWs()
            ws.receive({
                type: 'event',
                id: nextId(),
                event: 'task_update',
                data: { task_id: '6', status: 'failed' },
            })

            expect(mockShowBrowserNotification).toHaveBeenCalledTimes(1)
        })

        it('uses default i18n title when no session_title', () => {
            vi.spyOn(document, 'visibilityState', 'get').mockReturnValue('hidden')
            vi.spyOn(document, 'hasFocus').mockReturnValue(false)

            const ws = connectAndGetWs()
            ws.receive({
                type: 'event',
                id: nextId(),
                event: 'session_update',
                data: { session_id: 's1', status: 'completed' },
            })

            expect(mockShowBrowserNotification).toHaveBeenCalledTimes(1)
            // title = SessionCompleted, body = SessionCompleted (no preview)
            expect(mockShowBrowserNotification).toHaveBeenCalledWith(
                'chat.push.sessionCompleted',
                expect.objectContaining({ body: 'chat.push.sessionCompleted' })
            )
        })

        it('uses default i18n title for task_update without session_title', () => {
            vi.spyOn(document, 'visibilityState', 'get').mockReturnValue('hidden')
            vi.spyOn(document, 'hasFocus').mockReturnValue(false)

            const ws = connectAndGetWs()
            ws.receive({
                type: 'event',
                id: nextId(),
                event: 'task_update',
                data: { task_id: '5', status: 'completed' },
            })

            expect(mockShowBrowserNotification).toHaveBeenCalledTimes(1)
            // title = TaskCompleted, body = TaskCompleted (no preview)
            expect(mockShowBrowserNotification).toHaveBeenCalledWith(
                'chat.push.taskCompleted',
                expect.objectContaining({ body: 'chat.push.taskCompleted' })
            )
        })

        it('uses failed-specific i18n for task_update with status=failed', () => {
            vi.spyOn(document, 'visibilityState', 'get').mockReturnValue('hidden')
            vi.spyOn(document, 'hasFocus').mockReturnValue(false)

            const ws = connectAndGetWs()
            ws.receive({
                type: 'event',
                id: nextId(),
                event: 'task_update',
                data: { task_id: '6', status: 'failed' },
            })

            expect(mockShowBrowserNotification).toHaveBeenCalledTimes(1)
            expect(mockShowBrowserNotification).toHaveBeenCalledWith(
                'chat.push.taskFailed',
                expect.objectContaining({ body: 'chat.push.taskFailed' })
            )
        })

        it('uses default i18n body for permission_pending without tool_name', () => {
            vi.spyOn(document, 'visibilityState', 'get').mockReturnValue('hidden')
            vi.spyOn(document, 'hasFocus').mockReturnValue(false)

            const ws = connectAndGetWs()
            ws.receive({
                type: 'event',
                id: nextId(),
                event: 'session_update',
                data: { session_id: 's3', status: 'permission_pending' },
            })

            expect(mockShowBrowserNotification).toHaveBeenCalledTimes(1)
            // title = ActionRequired, body = ActionRequired (no tool_name)
            expect(mockShowBrowserNotification).toHaveBeenCalledWith(
                'chat.push.actionRequired',
                expect.objectContaining({ body: 'chat.push.actionRequired' })
            )
        })

        it('truncates long response_preview to 200 code points (aligned with backend)', () => {
            vi.spyOn(document, 'visibilityState', 'get').mockReturnValue('hidden')
            vi.spyOn(document, 'hasFocus').mockReturnValue(false)

            const longPreview = 'A'.repeat(300)
            const ws = connectAndGetWs()
            ws.receive({
                type: 'event',
                id: nextId(),
                event: 'session_update',
                data: { session_id: 's1', status: 'completed', response_preview: longPreview },
            })

            const body = mockShowBrowserNotification.mock.calls[0][1].body
            expect(body.length).toBeLessThan(300)
            expect(body.endsWith('…')).toBe(true)
        })

        it('truncates by Unicode code points not UTF-16 code units (emoji safety)', () => {
            vi.spyOn(document, 'visibilityState', 'get').mockReturnValue('hidden')
            vi.spyOn(document, 'hasFocus').mockReturnValue(false)

            // Each emoji is 2 UTF-16 code units but 1 code point
            const emoji = '🎉'
            const longPreview = emoji.repeat(300) // 300 code points, 600 UTF-16 units
            const ws = connectAndGetWs()
            ws.receive({
                type: 'event',
                id: nextId(),
                event: 'session_update',
                data: { session_id: 's1', status: 'completed', response_preview: longPreview },
            })

            const body = mockShowBrowserNotification.mock.calls[0][1].body
            // Should be 200 emoji + "…", NOT truncated at 200 UTF-16 units (100 emoji)
            const emojiCount = [...body.replace('…', '')].length
            expect(emojiCount).toBe(200)
        })

        it('does not show notification for non-terminal session statuses', () => {
            vi.spyOn(document, 'visibilityState', 'get').mockReturnValue('hidden')
            vi.spyOn(document, 'hasFocus').mockReturnValue(false)

            const ws = connectAndGetWs()
            ws.receive({ type: 'event', id: nextId(), event: 'session_update', data: { status: 'running' } })
            ws.receive({ type: 'event', id: nextId(), event: 'session_update', data: { status: 'permission_resolved' } })

            expect(mockShowBrowserNotification).not.toHaveBeenCalled()
        })

        it('does not show notification for non-notifiable task statuses', () => {
            vi.spyOn(document, 'visibilityState', 'get').mockReturnValue('hidden')
            vi.spyOn(document, 'hasFocus').mockReturnValue(false)

            const ws = connectAndGetWs()
            // "running" IS a notifiable status now (triggers "task started" notification)
            // Only truly non-notifiable statuses like "paused" should be filtered
            ws.receive({ type: 'event', id: nextId(), event: 'task_update', data: { status: 'paused' } })

            expect(mockShowBrowserNotification).not.toHaveBeenCalled()
        })

        it('shows browser notification for task started', () => {
            vi.spyOn(document, 'visibilityState', 'get').mockReturnValue('hidden')
            vi.spyOn(document, 'hasFocus').mockReturnValue(false)

            const ws = connectAndGetWs()
            ws.receive({
                type: 'event',
                id: nextId(),
                event: 'task_update',
                data: { task_id: '7', status: 'running', session_title: 'Nightly build' },
            })

            expect(mockShowBrowserNotification).toHaveBeenCalledTimes(1)
            expect(mockShowBrowserNotification).toHaveBeenCalledWith(
                'chat.push.taskStarted',
                expect.objectContaining({ body: 'Nightly build' })
            )
        })

        it('notification onClick dispatches clawbench-open-session for session_update', () => {
            vi.spyOn(document, 'visibilityState', 'get').mockReturnValue('hidden')
            vi.spyOn(document, 'hasFocus').mockReturnValue(false)
            const dispatchSpy = vi.spyOn(window, 'dispatchEvent')

            const ws = connectAndGetWs()
            ws.receive({
                type: 'event',
                id: nextId(),
                event: 'session_update',
                data: { session_id: 's1', status: 'completed', project_path: '/proj' },
            })

            // Extract onClick callback from the notification call
            const onClick = mockShowBrowserNotification.mock.calls[0][1].onClick
            expect(onClick).toBeDefined()
            onClick()

            expect(dispatchSpy).toHaveBeenCalledWith(
                expect.objectContaining({ type: 'clawbench-open-session' })
            )
            dispatchSpy.mockRestore()
        })

        it('notification onClick dispatches clawbench-open-task for task_update', () => {
            vi.spyOn(document, 'visibilityState', 'get').mockReturnValue('hidden')
            vi.spyOn(document, 'hasFocus').mockReturnValue(false)
            const dispatchSpy = vi.spyOn(window, 'dispatchEvent')

            const ws = connectAndGetWs()
            ws.receive({
                type: 'event',
                id: nextId(),
                event: 'task_update',
                data: { task_id: '5', execution_id: 'e1', status: 'completed', project_path: '/proj' },
            })

            const onClick = mockShowBrowserNotification.mock.calls[0][1].onClick
            expect(onClick).toBeDefined()
            onClick()

            expect(dispatchSpy).toHaveBeenCalledWith(
                expect.objectContaining({ type: 'clawbench-open-task' })
            )
            dispatchSpy.mockRestore()
        })
    })

    // ── fetchPendingEvents (断线补偿：pending 拉取的 user_message) ──
    // Phase 4: fetchPendingEvents must dispatch chat_stream user_message events
    // through the shared handlers (so useChatStream inserts the _remote bubble),
    // update the LAST_SEEN_KEY cursor, dedup by event id across the WS + pending
    // dual channels, and never trigger browser notifications for chat_stream.
    describe('fetchPendingEvents', () => {
        const LAST_SEEN_KEY = 'clawbench_last_seen_event_id'

        let originalFetch: typeof globalThis.fetch

        beforeEach(() => {
            originalFetch = globalThis.fetch
            localStorage.removeItem(LAST_SEEN_KEY)
        })

        afterEach(() => {
            globalThis.fetch = originalFetch
            localStorage.removeItem(LAST_SEEN_KEY)
        })

        /**
         * Mock fetch() for GET /api/ai/events/pending.
         * pending_events rows carry `event_type: 'chat_stream'` + a serialized
         * ServerMessage payload: `{type, id, event: 'chat_stream', data: ChatStreamData}`.
         */
        function mockPendingFetch(rows: Array<{ event_id: string; event_type?: string; payload?: unknown }>) {
            globalThis.fetch = vi.fn(async (input: RequestInfo | URL) => {
                expect(String(input)).toContain('/api/ai/events/pending')
                return {
                    ok: true,
                    json: async () => ({ events: rows }),
                } as Response
            }) as typeof fetch
        }

        /** Build a pending_events row for a chat_stream user_message. */
        function userMessageRow(eventId: string, msgId: number, opts: { content?: string; senderClientId?: string; queueId?: string } = {}) {
            return {
                event_id: eventId,
                event_type: 'chat_stream',
                payload: JSON.stringify({
                    type: 'event',
                    id: eventId,
                    event: 'chat_stream',
                    data: {
                        session_id: 's1',
                        event_type: 'user_message',
                        payload: {
                            messageId: msgId,
                            content: opts.content ?? 'hi',
                            ...(opts.senderClientId ? { senderClientId: opts.senderClientId } : {}),
                            ...(opts.queueId ? { queueId: opts.queueId } : {}),
                        },
                    },
                }),
            }
        }

        it('dispatches chat_stream user_message to registered handlers', async () => {
            const handler = vi.fn()
            events.onEvent(handler)
            // Install the pending-events mock BEFORE opening the WS — onopen
            // fires fetchPendingEvents() (fire-and-forget), which must see the mock.
            mockPendingFetch([userMessageRow('evt_user_1', 42, { content: 'hi', senderClientId: 'other-device' })])
            connectAndGetWs()

            await vi.waitFor(() => expect(handler).toHaveBeenCalledTimes(1))
            expect(handler).toHaveBeenCalledWith('chat_stream', {
                session_id: 's1',
                event_type: 'user_message',
                payload: { messageId: 42, content: 'hi', senderClientId: 'other-device' },
            })
        })

        it('updates LAST_SEEN_KEY cursor after dispatching user_message', async () => {
            const handler = vi.fn()
            events.onEvent(handler)
            mockPendingFetch([userMessageRow('evt_user_2', 43)])
            connectAndGetWs()

            await vi.waitFor(() => expect(localStorage.getItem(LAST_SEEN_KEY)).toBe('evt_user_2'))
        })

        it('dedups pending events by event id (already processed via WS)', async () => {
            const handler = vi.fn()
            events.onEvent(handler)
            const id = nextId()
            // Pull contains the already-seen event PLUS a fresh one — the fresh
            // event proves fetchPendingEvents actually ran (dispatched + cursor
            // advanced), while the seen event must be skipped (no re-dispatch).
            mockPendingFetch([userMessageRow(id, 44), userMessageRow('evt_user_new', 45, { content: 'fresh' })])
            const ws = connectAndGetWs()

            // The same user_message event arrives over WS first (dedup mark added)
            ws.receive({
                type: 'event',
                id,
                event: 'chat_stream',
                data: { session_id: 's1', event_type: 'user_message', payload: { messageId: 44, content: 'hi' } },
            })
            expect(handler).toHaveBeenCalledTimes(1)

            // fetchPendingEvents (fired on WS open) returns the same event — must
            // be skipped — plus a fresh one that IS dispatched.
            await vi.waitFor(() => expect(handler).toHaveBeenCalledTimes(2))
            expect(handler).toHaveBeenLastCalledWith('chat_stream', {
                session_id: 's1',
                event_type: 'user_message',
                payload: { messageId: 45, content: 'fresh' },
            })
            await vi.waitFor(() => expect(localStorage.getItem(LAST_SEEN_KEY)).toBe('evt_user_new'))
        })

        it('does not show browser notification for chat_stream events', async () => {
            vi.spyOn(document, 'visibilityState', 'get').mockReturnValue('hidden')
            vi.spyOn(document, 'hasFocus').mockReturnValue(false)

            mockPendingFetch([userMessageRow('evt_user_3', 45)])
            connectAndGetWs()

            // The event is processed (cursor advances), but chat_stream never
            // triggers a browser notification.
            await vi.waitFor(() => expect(localStorage.getItem(LAST_SEEN_KEY)).toBe('evt_user_3'))
            expect(mockShowBrowserNotification).not.toHaveBeenCalled()
        })
    })
})
