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

// NOTE: forgeEventLabels is deliberately NOT mocked. An earlier version mocked
// eventKindLabel/unreadReasonLabel to echo their inputs, which made the title
// assertions structurally clean but blind to a real defect: "pipeline" had no
// KIND_LABEL_KEYS entry and rendered as the raw English token in a Chinese
// title. The mock accepted anything, so the test passed. Assert against the
// real helpers and the real i18n keys.
//
// STORAGE_KEY is part of the module's public surface — useSettingsConfig reads
// it at import time — so the mock must expose it alongside the default export.
vi.mock('@/i18n', () => ({
    STORAGE_KEY: 'clawbench-locale',
    default: {
        global: {
            t: (key: string) => key,
            locale: { value: 'zh' },
        },
    },
}))

// Mock the native bridge — the Web frontend must keep the Android device cursor
// in sync with the in-memory cursor so background push doesn't re-deliver
// terminal events the user already saw in the foreground.
const mockUpdateLastSeenEventId = vi.fn()
vi.mock('@/utils/clawbenchNative', () => ({
    getNative: () => ({
        updateLastSeenEventId: (...args: unknown[]) => mockUpdateLastSeenEventId(...args),
    }),
}))

// Mutable flag for app mode — use plain object since vi.mock is hoisted
const appModeState = { value: false }
vi.mock('@/composables/useAppMode', () => ({
    useAppMode: () => ({
        isAppMode: { get value() { return appModeState.value }, set value(v: boolean) { appModeState.value = v } }
    }),
}))

import {
    useGlobalEvents,
    _seedLastSeenEventIdForTesting,
    _getLastSeenEventIdForTesting,
} from '@/composables/useGlobalEvents'

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
        mockUpdateLastSeenEventId.mockReset()
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

    describe('event cursor', () => {
        it('收到终态事件时同步安卓设备游标，非终态不同步', () => {
            const ws = connectAndGetWs()

            // Terminal event (session completed) → native cursor must advance.
            const terminalId = nextId()
            ws.receive({ type: 'event', id: terminalId, event: 'session_update', data: { status: 'completed' } })
            expect(mockUpdateLastSeenEventId).toHaveBeenLastCalledWith(terminalId)

            // Non-terminal event → no native sync.
            mockUpdateLastSeenEventId.mockClear()
            const runningId = nextId()
            ws.receive({ type: 'event', id: runningId, event: 'session_update', data: { status: 'running' } })
            expect(mockUpdateLastSeenEventId).not.toHaveBeenCalled()
        })

        it('task 终态事件同样同步安卓设备游标', () => {
            const ws = connectAndGetWs()

            const taskId = nextId()
            ws.receive({ type: 'event', id: taskId, event: 'task_update', data: { status: 'failed' } })
            expect(mockUpdateLastSeenEventId).toHaveBeenLastCalledWith(taskId)
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

    // ── forge_event browser notification ──
    //
    // Forge changes used to update the dock badge only; the IM robots were the
    // sole out-of-app surface. These pin the browser/system path.
    describe('browser notification for forge_event', () => {
        function forgeEventData(overrides: Record<string, unknown> = {}) {
            return {
                event: {
                    platform: 'github', host: 'github.com', owner: 'acme', repo: 'widgets',
                    item_type: 'pr', number: 42, event_type: 'merged',
                },
                item: {
                    type: 'pr', number: 42, title: 'Fix the thing',
                    url: 'https://github.com/acme/widgets/pull/42',
                },
                ...overrides,
            }
        }

        it('shows a notification composed from repo, kind, number and reason', () => {
            vi.spyOn(document, 'visibilityState', 'get').mockReturnValue('hidden')
            vi.spyOn(document, 'hasFocus').mockReturnValue(false)

            const ws = connectAndGetWs()
            ws.receive({ type: 'event', id: nextId(), event: 'forge_event', data: forgeEventData() })

            expect(mockShowBrowserNotification).toHaveBeenCalledTimes(1)
            // Real helpers + real i18n keys (the i18n mock echoes keys), so a
            // missing KIND_LABEL_KEYS entry surfaces as a raw token here.
            expect(mockShowBrowserNotification).toHaveBeenCalledWith(
                'acme/widgets · task.form.eventKindPr #42 · forge.overview.reason.merged',
                expect.objectContaining({
                    body: 'Fix the thing',
                    tag: expect.stringContaining('clawbench-forge_event-widgets'),
                })
            )
        })

        it('omits the item number for a pipeline (a "#0" reads as a broken reference)', () => {
            vi.spyOn(document, 'visibilityState', 'get').mockReturnValue('hidden')
            vi.spyOn(document, 'hasFocus').mockReturnValue(false)

            const ws = connectAndGetWs()
            ws.receive({
                type: 'event',
                id: nextId(),
                event: 'forge_event',
                data: {
                    event: {
                        platform: 'github', host: 'github.com', owner: 'acme', repo: 'widgets',
                        item_type: 'pipeline', number: 0, event_type: 'pipeline_done',
                    },
                    item: { type: 'pipeline', number: 0, title: 'CI', url: 'https://github.com/acme/widgets/actions/runs/7' },
                },
            })

            const title = mockShowBrowserNotification.mock.calls[0][0]
            // "pipeline" must resolve to the repository-pipeline label, not leak
            // the raw wire token into a localized title.
            expect(title).toBe('acme/widgets · task.form.eventKindRepo · forge.overview.reason.pipeline_done')
            expect(title).not.toContain('#0')
            expect(title).not.toContain('pipeline ·')
        })

        it('falls back to the reason when the item carries no title', () => {
            vi.spyOn(document, 'visibilityState', 'get').mockReturnValue('hidden')
            vi.spyOn(document, 'hasFocus').mockReturnValue(false)

            const ws = connectAndGetWs()
            ws.receive({
                type: 'event',
                id: nextId(),
                event: 'forge_event',
                data: {
                    event: { owner: 'acme', repo: 'widgets', item_type: 'issue', number: 1, event_type: 'commented' },
                    item: { type: 'issue', number: 1, url: 'https://github.com/acme/widgets/issues/1' },
                },
            })

            expect(mockShowBrowserNotification).toHaveBeenCalledWith(
                'acme/widgets · task.form.eventKindIssue #1 · forge.overview.reason.commented',
                expect.objectContaining({ body: 'forge.overview.reason.commented' })
            )
        })

        it('does not notify when the event identity is missing', () => {
            vi.spyOn(document, 'visibilityState', 'get').mockReturnValue('hidden')
            vi.spyOn(document, 'hasFocus').mockReturnValue(false)

            const ws = connectAndGetWs()
            ws.receive({ type: 'event', id: nextId(), event: 'forge_event', data: {} })

            expect(mockShowBrowserNotification).not.toHaveBeenCalled()
        })

        it('notification onClick dispatches clawbench-open-forge with the project path', () => {
            vi.spyOn(document, 'visibilityState', 'get').mockReturnValue('hidden')
            vi.spyOn(document, 'hasFocus').mockReturnValue(false)
            const dispatchSpy = vi.spyOn(window, 'dispatchEvent')

            const ws = connectAndGetWs()
            ws.receive({
                type: 'event',
                id: nextId(),
                event: 'forge_event',
                data: forgeEventData({
                    event: {
                        platform: 'github', host: 'github.com', owner: 'acme', repo: 'widgets',
                        item_type: 'pr', number: 42, event_type: 'merged',
                        project_path: '/home/u/proj-b',
                    },
                }),
            })

            const onClick = mockShowBrowserNotification.mock.calls[0][1].onClick
            expect(onClick).toBeDefined()
            onClick()

            // The panel is project-scoped, so the destination project must travel
            // with the event — without it the click opens the ACTIVE project's
            // panel, where this repository's row does not exist.
            expect(dispatchSpy).toHaveBeenCalledWith(
                expect.objectContaining({
                    type: 'clawbench-open-forge',
                    detail: { projectPath: '/home/u/proj-b' },
                })
            )
            dispatchSpy.mockRestore()
        })

        it('dispatches clawbench-open-forge with an undefined project path when unattributed', () => {
            vi.spyOn(document, 'visibilityState', 'get').mockReturnValue('hidden')
            vi.spyOn(document, 'hasFocus').mockReturnValue(false)
            const dispatchSpy = vi.spyOn(window, 'dispatchEvent')

            const ws = connectAndGetWs()
            // forgeEventData() has no project_path on the event.
            ws.receive({ type: 'event', id: nextId(), event: 'forge_event', data: forgeEventData() })

            mockShowBrowserNotification.mock.calls[0][1].onClick()

            // The listener falls back to the current project when it is absent.
            expect(dispatchSpy).toHaveBeenCalledWith(
                expect.objectContaining({
                    type: 'clawbench-open-forge',
                    detail: { projectPath: undefined },
                })
            )
            dispatchSpy.mockRestore()
        })

        it('does not notify for a replayed forge_event (caught-up history)', () => {
            vi.spyOn(document, 'visibilityState', 'get').mockReturnValue('hidden')
            vi.spyOn(document, 'hasFocus').mockReturnValue(false)

            const ws = connectAndGetWs()
            ws.receive({
                type: 'event',
                id: nextId(),
                event: 'forge_event',
                replayed: true,
                data: forgeEventData(),
            })

            expect(mockShowBrowserNotification).not.toHaveBeenCalled()
        })
    })

    // ── push_mode is not a gate ──
    //
    // The system-notification decision belongs to the local `browserNotification`
    // setting (checked inside showBrowserNotification) and page focus. Gating it
    // here on the server-side push_mode made the switch unreachable for anyone
    // who had picked DingTalk/飞书, and silenced the desktop tab of users whose
    // push_mode was changed for their phone.
    describe('browser notification is independent of push_mode', () => {
        it('still notifies when push_mode is dingtalk', async () => {
            vi.spyOn(document, 'visibilityState', 'get').mockReturnValue('hidden')
            vi.spyOn(document, 'hasFocus').mockReturnValue(false)

            const settings = await import('@/composables/useSettingsConfig')
            settings.serverConfig.value = { push_mode: 'dingtalk' }

            try {
                const ws = connectAndGetWs()
                ws.receive({
                    type: 'event',
                    id: nextId(),
                    event: 'session_update',
                    data: { session_id: 's1', status: 'completed', response_preview: 'Done!' },
                })

                expect(mockShowBrowserNotification).toHaveBeenCalledTimes(1)
            } finally {
                settings.serverConfig.value = {}
            }
        })

        it('still notifies when push_mode is disabled', async () => {
            vi.spyOn(document, 'visibilityState', 'get').mockReturnValue('hidden')
            vi.spyOn(document, 'hasFocus').mockReturnValue(false)

            const settings = await import('@/composables/useSettingsConfig')
            settings.serverConfig.value = { push_mode: 'disabled' }

            try {
                const ws = connectAndGetWs()
                ws.receive({
                    type: 'event',
                    id: nextId(),
                    event: 'task_update',
                    data: { task_id: '5', status: 'completed', response_preview: 'ok' },
                })

                expect(mockShowBrowserNotification).toHaveBeenCalledTimes(1)
            } finally {
                settings.serverConfig.value = {}
            }
        })
    })

    // ── fetchPendingEvents (断线补偿：pending 拉取的 user_message) ──
    // Phase 4: fetchPendingEvents must dispatch chat_stream user_message events
    // through the shared handlers (so useChatStream inserts the _remote bubble),
    // advance the in-memory last-seen cursor, dedup by event id across the WS +
    // pending dual channels, and never trigger browser notifications for
    // chat_stream. The cursor is session-only: it must NOT be read from or
    // written to localStorage.
    describe('fetchPendingEvents', () => {
        let originalFetch: typeof globalThis.fetch

        beforeEach(() => {
            originalFetch = globalThis.fetch
            // 默认模拟"同一运行周期内断线重连"：游标已在内存中，拉取游标之后
            // 的事件做补偿。游标存在性由服务端校验，前端 mock 不涉及。
            _seedLastSeenEventIdForTesting('evt_seen_old')
        })

        afterEach(() => {
            globalThis.fetch = originalFetch
            _seedLastSeenEventIdForTesting('')
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

        it('advances the in-memory cursor after dispatching user_message', async () => {
            const handler = vi.fn()
            events.onEvent(handler)
            mockPendingFetch([userMessageRow('evt_user_2', 43)])
            connectAndGetWs()

            await vi.waitFor(() => expect(_getLastSeenEventIdForTesting()).toBe('evt_user_2'))
        })

        it('同步安卓设备游标到最新事件位置（防止后台重复推送前台已看过的完成事件）', async () => {
            // The native bridge must be told where the in-memory cursor ended up
            // so the Android background service won't re-deliver terminal events
            // the user already saw while the app was in the foreground.
            const handler = vi.fn()
            events.onEvent(handler)
            mockPendingFetch([userMessageRow('evt_user_native', 46)])
            connectAndGetWs()

            await vi.waitFor(() => expect(mockUpdateLastSeenEventId).toHaveBeenCalledWith('evt_user_native'))
        })

        it('游标为空时（页面重开/APP 重启）同步安卓设备游标到最新，不重放历史事件', async () => {
            // Fresh page load: in-memory cursor is empty, so nothing is
            // dispatched and the cursor jumps to the newest event. The native
            // cursor must follow, otherwise the Android background service would
            // re-deliver the whole 24h backlog next time the app goes to
            // background.
            _seedLastSeenEventIdForTesting('')
            const handler = vi.fn()
            events.onEvent(handler)

            mockPendingFetch([
                userMessageRow('evt_old_1', 1, { content: 'old' }),
                userMessageRow('evt_old_2', 2, { content: 'older' }),
                userMessageRow('evt_latest_native', 3, { content: 'newest' }),
            ])
            connectAndGetWs()

            await vi.waitFor(() => expect(mockUpdateLastSeenEventId).toHaveBeenCalledWith('evt_latest_native'))
            expect(handler).not.toHaveBeenCalled()
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
            await vi.waitFor(() => expect(_getLastSeenEventIdForTesting()).toBe('evt_user_new'))
        })

        it('does not show browser notification for chat_stream events', async () => {
            vi.spyOn(document, 'visibilityState', 'get').mockReturnValue('hidden')
            vi.spyOn(document, 'hasFocus').mockReturnValue(false)

            mockPendingFetch([userMessageRow('evt_user_3', 45)])
            connectAndGetWs()

            // The event is processed (cursor advances), but chat_stream never
            // triggers a browser notification.
            await vi.waitFor(() => expect(_getLastSeenEventIdForTesting()).toBe('evt_user_3'))
            expect(mockShowBrowserNotification).not.toHaveBeenCalled()
        })

        it('断线期间 pending user_message 与流式 content 事件按到达顺序交错分发', async () => {
            // Scenario: device B was offline while device A sent a message. On
            // reconnect, fetchPendingEvents returns the missed user_message
            // (write-ahead stored while disconnected), while the live WS stream
            // for that same turn delivers stream_start/content events. Both
            // channels flow through the same shared handlers, so the reducer
            // (in useChatStream) sees them in arrival order and can build the
            // _remote bubble BEFORE the streaming content lands.
            const received: Array<{ event: string; data: any }> = []
            events.onEvent((event, data) => received.push({ event, data }))

            // The pending pull carries the user_message for the turn that
            // happened while offline.
            mockPendingFetch([userMessageRow('evt_pending_user', 200, { content: 'hi while offline', senderClientId: 'device-a', queueId: 'remote-q-1' })])
            const ws = connectAndGetWs()

            // Wait until the pending user_message was dispatched through the
            // handler (fetchPendingEvents is async, fired on WS open).
            await vi.waitFor(() => {
                expect(received.some((r) => r.event === 'chat_stream' &&
                    r.data?.event_type === 'user_message' &&
                    r.data?.payload?.messageId === 200)).toBe(true)
            })

            // Now the live streaming events for the same turn arrive over WS.
            ws.receive({
                type: 'event',
                id: nextId(),
                event: 'chat_stream',
                data: { session_id: 's1', event_type: 'stream_start', payload: { message_id: 201 } },
            })
            ws.receive({
                type: 'event',
                id: nextId(),
                event: 'chat_stream',
                data: { session_id: 's1', event_type: 'content', payload: { content: 'reply to your message' } },
            })

            await vi.waitFor(() => {
                expect(received.some((r) => r.event === 'chat_stream' && r.data?.event_type === 'content')).toBe(true)
            })

            // Ordering: the offline-compensated user_message arrives BEFORE the
            // live stream events — so a reducer that inserts the _remote bubble
            // first and then finds the streaming placeholder is never starved.
            const types = received.map((r) => `${r.event}:${r.data?.event_type}`)
            const umIdx = types.indexOf('chat_stream:user_message')
            const ssIdx = types.indexOf('chat_stream:stream_start')
            expect(umIdx).toBeGreaterThanOrEqual(0)
            expect(ssIdx).toBeGreaterThan(umIdx)

            // Cursor advanced past the pending user_message so it won't be
            // re-delivered on the next reconnect.
            expect(_getLastSeenEventIdForTesting()).toBe('evt_pending_user')
        })

        it('游标为空时（页面重开/APP 重启）不派发历史事件，仅推进游标到最新', async () => {
            // Fresh page load / app restart: the in-memory cursor is empty. The
            // server's pending_events table holds up to 24h of terminal events;
            // replaying them all would flood the client with dozens of stale
            // completion popups/notifications. Instead the cursor must advance
            // to the newest event and nothing must be dispatched.
            _seedLastSeenEventIdForTesting('')
            const handler = vi.fn()
            events.onEvent(handler)

            mockPendingFetch([
                userMessageRow('evt_old_1', 1, { content: 'old' }),
                userMessageRow('evt_old_2', 2, { content: 'older' }),
                userMessageRow('evt_latest', 3, { content: 'newest' }),
            ])
            connectAndGetWs()

            // History must NOT be dispatched...
            await vi.waitFor(() => expect(_getLastSeenEventIdForTesting()).toBe('evt_latest'))
            expect(handler).not.toHaveBeenCalled()

            // ...and the cursor is now at the newest event so future reconnects
            // pull from here instead of replaying the whole history.
            expect(handler).toHaveBeenCalledTimes(0)
        })

        it('整批 pending 事件分发期间 isReplayingEvents 全程为 true，结束后复位', async () => {
            // 断线补偿批内每个事件都必须被标记为重放（App.vue 弹窗据此抑制），
            // 不能因为循环内提前复位导致批内后续事件丢失标记。
            _seedLastSeenEventIdForTesting('evt_seen')
            const seenFlags: boolean[] = []
            events.onEvent(() => seenFlags.push(events.isReplayingEvents.value))

            mockPendingFetch([
                userMessageRow('evt_b1', 1, { content: 'a' }),
                userMessageRow('evt_b2', 2, { content: 'b' }),
                userMessageRow('evt_b3', 3, { content: 'c' }),
            ])
            connectAndGetWs()

            await vi.waitFor(() => expect(seenFlags.length).toBe(3))
            // 批内每个事件分发时 isReplayingEvents 都为 true
            expect(seenFlags).toEqual([true, true, true])
            // 批结束后复位
            expect(events.isReplayingEvents.value).toBe(false)
        })

        it('pending 回放的 session_update completed 不弹浏览器通知（ISS-253）', async () => {
            // 断线期间（游标已存在）会话完成 → 重连后 fetchPendingEvents 拉回
            // 该终态事件。这是"用户没看到的历史完成"，即使页面在后台也绝不弹
            // 浏览器通知——与 WS replay 路径行为一致。
            vi.spyOn(document, 'visibilityState', 'get').mockReturnValue('hidden')
            vi.spyOn(document, 'hasFocus').mockReturnValue(false)
            _seedLastSeenEventIdForTesting('evt_seen_old')

            mockPendingFetch([{
                event_id: 'evt_pending_completed',
                event_type: 'session_update',
                payload: JSON.stringify({
                    type: 'event',
                    id: 'evt_pending_completed',
                    event: 'session_update',
                    data: { session_id: 's1', status: 'completed', project_path: '/p' },
                }),
            }])
            connectAndGetWs()

            // 事件被处理（游标推进）但不弹通知
            await vi.waitFor(() => expect(_getLastSeenEventIdForTesting()).toBe('evt_pending_completed'))
            expect(mockShowBrowserNotification).not.toHaveBeenCalled()
            vi.restoreAllMocks()
        })
    })

    // ── WS replay buffer（断线后服务器握手即重放缓冲事件）──
    // Bug：页面刷新后仍弹出历史会话完成通知。fetchPendingEvents 的空游标跳过
    // 只保护了 pending 拉取路径；服务器 WS 握手后的 replay buffer（events.go
    // GetBufferedEvents）把断线期间缓冲的 session_update completed 直接推送，
    // 前端未区分 replay 与 live，导致 completion popover 弹出历史通知。
    // 修复：后端给重放消息打 `replayed: true` 标记（events.go），前端按标记
    // 置 isReplayingEvents，消费者（App.vue completion handler / 浏览器通知）
    // 据此抑制。
    describe('WS replay buffer (handshake replay)', () => {
        let originalFetch: typeof globalThis.fetch

        beforeEach(() => {
            originalFetch = globalThis.fetch
            // No pending events — replay comes purely from the WS buffer.
            globalThis.fetch = vi.fn(async () => ({
                ok: true,
                json: async () => ({ events: [] }),
            }) as Response)
        })

        afterEach(() => {
            globalThis.fetch = originalFetch
            _seedLastSeenEventIdForTesting('')
        })

        function replayedEvent(id: string, event: string, data: object): object {
            // Mirrors the server's tagged replay (events.go sets Replayed=true).
            return { type: 'event', id, event, replayed: true, data }
        }

        it('页面刷新后 WS 握手重放的历史 completed 事件被标记为重放', async () => {
            _seedLastSeenEventIdForTesting('')
            const handler = vi.fn()
            events.onEvent(handler)
            const ws = connectAndGetWs()

            await vi.waitFor(() => expect(_getLastSeenEventIdForTesting()).toBe(''))

            // 服务器握手后重放断线期间缓冲的历史 completed 事件（带 replayed 标记）
            ws.receive(replayedEvent('evt_hist_completed', 'session_update', {
                session_id: 'old-sess', status: 'completed',
            }))

            // replay 期间：isReplayingEvents 必须为 true（App.vue 弹窗据此抑制）
            expect(events.isReplayingEvents.value).toBe(true)
            // 浏览器通知被抑制（replay 是历史事件，即使页面未聚焦也不弹）
            expect(mockShowBrowserNotification).not.toHaveBeenCalled()
            // 游标仍正常推进（终态事件路径不受影响）
            expect(_getLastSeenEventIdForTesting()).toBe('evt_hist_completed')

            // 下一条 live 消息到达：标志复位，不再抑制
            ws.receive({ type: 'event', id: nextId(), event: 'session_update', data: { status: 'running' } })
            expect(events.isReplayingEvents.value).toBe(false)
        })

        it('刷新后重放窗口内的多个历史 completed 事件全部抑制', async () => {
            _seedLastSeenEventIdForTesting('')
            const handler = vi.fn()
            events.onEvent(handler)
            const ws = connectAndGetWs()

            await vi.waitFor(() => expect(_getLastSeenEventIdForTesting()).toBe(''))

            // 多个历史事件在握手重放中批量到达（全部带 replayed 标记）
            ws.receive(replayedEvent('evt_h1', 'session_update', { session_id: 'a', status: 'completed' }))
            ws.receive(replayedEvent('evt_h2', 'session_update', { session_id: 'b', status: 'completed' }))
            ws.receive(replayedEvent('evt_h3', 'session_update', { session_id: 'c', status: 'completed' }))

            expect(mockShowBrowserNotification).not.toHaveBeenCalled()
            // 游标推进到最后一条历史事件
            expect(_getLastSeenEventIdForTesting()).toBe('evt_h3')
        })

        it('replay 结束后（下一条 live 消息）后续 live 事件正常通知', async () => {
            _seedLastSeenEventIdForTesting('')
            const handler = vi.fn()
            events.onEvent(handler)
            const ws = connectAndGetWs()

            await vi.waitFor(() => expect(_getLastSeenEventIdForTesting()).toBe(''))

            // 握手重放：历史 completed（抑制）
            ws.receive(replayedEvent('evt_hist', 'session_update', { session_id: 'old', status: 'completed' }))
            expect(mockShowBrowserNotification).not.toHaveBeenCalled()

            // 之后的 live 事件：标志已复位，正常弹浏览器通知（页面未聚焦）
            ws.receive({ type: 'event', id: nextId(), event: 'session_update', data: { session_id: 'new', status: 'completed' } })
            expect(events.isReplayingEvents.value).toBe(false)
            expect(mockShowBrowserNotification).toHaveBeenCalledTimes(1)
        })

        it('页面刷新后不弹出历史完成通知（App.vue 弹窗依赖 isReplayingEvents）', async () => {
            // 模拟 App.vue 的 completion popover handler：replay 阶段必须抑制弹窗。
            _seedLastSeenEventIdForTesting('')
            const popoverPush = vi.fn()
            events.onEvent((event, data) => {
                if (!data || data.status !== 'completed') return
                if (event !== 'session_update' && event !== 'task_update') return
                if (events.isReplayingEvents.value) return // App.vue 同款抑制逻辑
                popoverPush(data)
            })
            const ws = connectAndGetWs()

            await vi.waitFor(() => expect(_getLastSeenEventIdForTesting()).toBe(''))

            // 握手重放历史 completed → 抑制
            ws.receive(replayedEvent('evt_hist', 'session_update', { session_id: 'old', status: 'completed' }))
            expect(popoverPush).not.toHaveBeenCalled()

            // 之后的 live completed → 正常弹窗
            ws.receive({ type: 'event', id: nextId(), event: 'session_update', data: { session_id: 'live', status: 'completed' } })
            expect(popoverPush).toHaveBeenCalledTimes(1)
        })

        it('断线期间在页面停留（游标已存在）→ 握手重放补发的 completed 也抑制弹窗', async () => {
            // 与"页面刷新"不同的场景：同一运行周期内 WS 断开又重连。
            // 游标已在内存（已看过 evt_seen），断线期间产生的新 completed 事件
            // 由服务器 replay buffer 在握手后补发——同样是"用户没看到的历史通知"，
            // 必须抑制，不能因为游标非空就当作 live 事件弹出。
            _seedLastSeenEventIdForTesting('evt_seen')
            const popoverPush = vi.fn()
            events.onEvent((event, data) => {
                if (!data || data.status !== 'completed') return
                if (event !== 'session_update' && event !== 'task_update') return
                if (events.isReplayingEvents.value) return
                popoverPush(data)
            })
            const ws = connectAndGetWs()

            // 无 pending 事件，fetchPendingEvents 不置 isReplayingEvents
            await vi.waitFor(() => expect(events.isReplayingEvents.value).toBe(false))

            // 服务器 replay buffer 在握手后把断线期间缓冲的 completed 推来（带标记）
            ws.receive(replayedEvent('evt_offline_completed', 'session_update', { session_id: 'offline', status: 'completed' }))
            // 带 replayed 标记 → 抑制弹窗
            expect(popoverPush).not.toHaveBeenCalled()
            expect(events.isReplayingEvents.value).toBe(true)
            expect(_getLastSeenEventIdForTesting()).toBe('evt_offline_completed')

            // 同一批次的下一条继续抑制
            ws.receive(replayedEvent('evt_offline2', 'session_update', { session_id: 'offline2', status: 'completed' }))
            expect(popoverPush).not.toHaveBeenCalled()
            expect(_getLastSeenEventIdForTesting()).toBe('evt_offline2')
        })
    })
})
