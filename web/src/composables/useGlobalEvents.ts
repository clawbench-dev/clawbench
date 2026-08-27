import { ref, computed } from 'vue'
import { useReconnect } from './useReconnect'
import { useAppMode } from './useAppMode'
import { showBrowserNotification } from './useNotification'
import { playNotificationSound } from './useNotificationSound'
import { gt } from './useLocale'
import { serverConfig } from './useSettingsConfig'
import { stripMarkdownPreview } from '@/utils/format'
import { getNative } from '@/utils/clawbenchNative'
import { appLog } from '@/utils/appLog'

// Event types from server
interface ServerEvent {
    type: string           // "event" | "ping"
    id?: string            // event ID for dedup
    event?: string         // "session_update" | "task_update"
    data?: {
        session_id?: string
        status?: string
        has_new_messages?: boolean
        task_id?: string
        execution_id?: string
        count?: number
        // Fields used for notification display
        session_title?: string
        response_preview?: string
        response_preview_plain?: string // Markdown-stripped preview for Android/browser notifications
        last_user_message?: string // plain-text preview of the most recent user message (completed only)
        agent_id?: string // agent that ran the session/execution (completed only)
        tool_name?: string
        project_path?: string
    }
}

// Client message types
type ClientMessage =
    | { type: 'ack'; id: string }
    | { type: 'pong' }
    | { type: 'subscribe'; session_id: string }
    | { type: 'unsubscribe'; session_id: string }
    | { type: 'cancel'; session_id: string }
    | { type: 'permission_respond'; session_id: string; tool_call_id: string; option_id: string; cancelled: boolean }

type EventHandler = (event: string, data: ServerEvent['data']) => void

// Module-level singleton state
const connected = ref(false)
// True once the WS connection has been established at least once.
// Used to suppress the reconnect overlay during initial page load.
const hasConnectedOnce = ref(false)
const handlers: EventHandler[] = []
const processedEventIds = new Set<string>()
const MAX_PROCESSED_IDS = 100
let ws: WebSocket | null = null
let heartbeatTimer: ReturnType<typeof setInterval> | null = null
// The server pings every 30s. The heartbeat watches the wall-clock time since
// the last received ping, instead of a fragile missed-count that can trip on a
// single delayed ping under load/network jitter.
const HEARTBEAT_CHECK_INTERVAL_MS = 15000   // how often we re-check staleness
const HEARTBEAT_STALE_MS = 90000            // no ping for 90s (3 missed pings) => dead
// Fallback stale-detection for the "socket alive but events silently dropped"
// case (e.g. a mobile WebView/Electron pauseTimers() freezes the event loop yet
// keeps the TCP socket open). The server pings every 30s, so 60s with NO WS
// message at all (ping or event) means the connection is effectively dead —
// much earlier than the 90s ping-only window. This forces a reconnect which
// re-runs the full state sync (clawbench-reconnect on open), correcting stale
// state even though the socket looked alive.
const EVENT_STALE_MS = 60000                // no message (ping or event) for 60s => silently dead
let lastPingAt = 0
let lastEventAt = 0

// Persistent client ID — identifies this browser/device across sessions.
// Stored in localStorage so the server can track multiple tabs/devices independently.
const CLIENT_ID_KEY = 'clawbench_client_id'
const LAST_SEEN_KEY = 'clawbench_last_seen_event_id'
let clientId = localStorage.getItem(CLIENT_ID_KEY)
if (!clientId) {
    // crypto.randomUUID() requires a secure context (HTTPS or localhost);
    // fallback to crypto.getRandomValues() for plain HTTP external access.
    clientId = crypto.randomUUID?.() ?? (() => {
        const bytes = crypto.getRandomValues(new Uint8Array(16))
        bytes[6] = (bytes[6] & 0x0f) | 0x40 // version 4
        bytes[8] = (bytes[8] & 0x3f) | 0x80 // variant 10
        const hex = Array.from(bytes, b => b.toString(16).padStart(2, '0')).join('')
        return `${hex.slice(0,8)}-${hex.slice(8,12)}-${hex.slice(12,16)}-${hex.slice(16,20)}-${hex.slice(20)}`
    })()
    localStorage.setItem(CLIENT_ID_KEY, clientId)
}

const { isAppMode } = useAppMode()

const reconnect = useReconnect({
    baseDelay: 2000,
    maxDelay: 15000,
    onReconnect: () => connect(),
})

function addProcessedId(id: string) {
    processedEventIds.add(id)
    // Evict oldest entries when set exceeds limit
    if (processedEventIds.size > MAX_PROCESSED_IDS) {
        const toRemove = processedEventIds.size - MAX_PROCESSED_IDS
        const iter = processedEventIds.values()
        for (let i = 0; i < toRemove; i++) {
            const val = iter.next().value
            if (val !== undefined) processedEventIds.delete(val)
        }
    }
}

function isDuplicate(id: string): boolean {
    return processedEventIds.has(id)
}

// Aligned with backend model.ResponsePreviewMaxRunes = 200
const PUSH_ALERT_MAX_CODE_POINTS = 200

/**
 * Truncate text for notification alert.
 * Max N Unicode code points + "…".
 * Uses [...str] to count code points (not UTF-16 code units).
 */
function truncateForPush(s: string): string {
    const chars = [...s]
    if (chars.length <= PUSH_ALERT_MAX_CODE_POINTS) return s
    return chars.slice(0, PUSH_ALERT_MAX_CODE_POINTS).join('') + '…'
}

/**
 * Get plain-text notification body from response preview data.
 * Prefers response_preview_plain (server-stripped), falls back to
 * stripMarkdownPreview on response_preview for older server versions.
 */
function plainPreview(data: ServerEvent['data']): string {
    if (!data) return ''
    if (data.response_preview_plain) return truncateForPush(data.response_preview_plain)
    if (data.response_preview) return stripMarkdownPreview(data.response_preview, PUSH_ALERT_MAX_CODE_POINTS)
    return ''
}

async function fetchPendingEvents() {
    try {
        const lastSeenId = localStorage.getItem(LAST_SEEN_KEY) || ''
        const url = lastSeenId
            ? `/api/ai/events/pending?after=${encodeURIComponent(lastSeenId)}`
            : '/api/ai/events/pending'

        const resp = await fetch(url, { credentials: 'same-origin' })
        if (!resp.ok) return

        const data = await resp.json()
        const events: Array<{ event_id: string; event_type: string; payload: string }> = data.events || []
        if (events.length === 0) return

        let latestId = lastSeenId
        for (const event of events) {
            const msg: ServerEvent = JSON.parse(event.payload)
            if (!msg.event || !msg.data) continue

            // Dedup check
            if (msg.id && isDuplicate(msg.id)) continue
            if (msg.id) addProcessedId(msg.id)

            // Dispatch to handlers
            for (const handler of handlers) {
                handler(msg.event!, msg.data)
            }

            // Show browser notification
            showEventBrowserNotification(msg.event!, msg.data)

            if (msg.id) latestId = msg.id
        }

        // Update cursor
        if (latestId !== lastSeenId) {
            localStorage.setItem(LAST_SEEN_KEY, latestId)
            // Sync cursor to Android SharedPreferences
            try {
                getNative()?.updateLastSeenEventId(latestId)
            } catch {}
        }
    } catch {
        // Non-critical
    }
}

function connect() {
    disconnect()

    const protocol = location.protocol === 'https:' ? 'wss:' : 'ws:'
    const url = `${protocol}//${location.host}/api/ai/events/ws?client_id=${clientId}`

    ws = new WebSocket(url)

    ws.onopen = () => {
        connected.value = true
        // True reconnect only if we've connected before — the app-startup first
        // connect already loads sessions/tasks/git, so dispatching here too
        // would duplicate those requests.
        const isReconnect = hasConnectedOnce.value
        hasConnectedOnce.value = true
        lastPingAt = Date.now()
        lastEventAt = Date.now()
        reconnect.reset()

        // Fetch missed events that occurred while offline
        fetchPendingEvents()

        // Notify other composables to refresh stale state
        // (sessions, tasks, git — WS-push state that may have changed
        // while disconnected beyond the 10s buffer window)
        if (isReconnect) {
            window.dispatchEvent(new CustomEvent('clawbench-reconnect'))
        }

        // Start heartbeat monitoring
        startHeartbeat()
    }

    ws.onmessage = (event) => {
        try {
            const msg: ServerEvent = JSON.parse(event.data)
            lastEventAt = Date.now()

            if (msg.type === 'ping') {
                send({ type: 'pong' })
                lastPingAt = Date.now()
                return
            }

            if (msg.type === 'event' && msg.event) {
                // Dedup check
                if (msg.id && isDuplicate(msg.id)) {
                    return
                }
                if (msg.id) {
                    addProcessedId(msg.id)
                }

                // Dispatch to handlers
                for (const handler of handlers) {
                    handler(msg.event!, msg.data)
                }

                // Dispatch summary_update as a custom event for ChatPanelContent
                if (msg.event === 'summary_update' && (msg.data as Record<string, unknown>)?.targetType === 'chat_message') {
                    window.dispatchEvent(new CustomEvent('clawbench-summary-update', { detail: msg.data }))
                }

                // Dispatch chat_recommendation for the chat input bar to auto-fill / show a suggestion chip
                if (msg.event === 'chat_recommendation') {
                    window.dispatchEvent(new CustomEvent('clawbench-recommendation', { detail: msg.data }))
                }

                // Browser notification: when page is not focused, show browser
                // notification for terminal events (completed/cancelled/failed/
                // permission_pending).
                showEventBrowserNotification(msg.event!, msg.data)

                // Send ack
                if (msg.id) {
                    send({ type: 'ack', id: msg.id })
                    // Update last seen event cursor for offline recovery
                    // Only update for terminal-state events that are persisted server-side
                    const status = (msg.data as Record<string, unknown>)?.status as string | undefined
                    const isTerminal = (msg.event === 'session_update' && (status === 'completed' || status === 'cancelled' || status === 'permission_pending'))
                        || (msg.event === 'task_update' && (status === 'completed' || status === 'failed' || status === 'cancelled'))
                    if (isTerminal) {
                        localStorage.setItem(LAST_SEEN_KEY, msg.id)
                        // Sync cursor to Android SharedPreferences so that the native
                        // fetchPendingEvents() won't re-deliver these events when
                        // the app switches to background.
                        try {
                            getNative()?.updateLastSeenEventId(msg.id)
                        } catch {}
                    }
                }
            }
        } catch {
            // Ignore malformed messages
        }
    }

    ws.onclose = () => {
        connected.value = false
        stopHeartbeat()

        if (reconnect.shouldReconnect()) {
            reconnect.scheduleReconnect()
        }
    }

    ws.onerror = () => {
        // onclose will fire after this
    }
}

function disconnect() {
    stopHeartbeat()
    if (ws) {
        ws.onclose = null // prevent reconnect
        ws.close()
        ws = null
    }
    connected.value = false
}

function send(msg: ClientMessage) {
    if (ws && ws.readyState === WebSocket.OPEN) {
        ws.send(JSON.stringify(msg))
    }
}

function startHeartbeat() {
    stopHeartbeat()
    lastPingAt = Date.now()
    lastEventAt = Date.now()
    heartbeatTimer = setInterval(() => {
        if (ws && ws.readyState === WebSocket.OPEN) {
            // If no message at all (ping or event) was received within the
            // event-stale window, the connection is silently dead even though
            // the socket looks alive (half-open socket / frozen event loop that
            // kept the TCP socket open). Force a reconnect so the UI reflects
            // the real state, the client re-subscribes, and the onopen handler
            // re-runs the full state sync (clawbench-reconnect). This closes
            // the "WS alive but state stale" window earlier than the ping-only
            // check below.
            if (Date.now() - lastEventAt > EVENT_STALE_MS) {
                appLog.w('GlobalEvents', `No WS message for ${EVENT_STALE_MS}ms — connection silently dead, forcing reconnect`)
                disconnect()
                if (reconnect.shouldReconnect()) {
                    reconnect.scheduleReconnect()
                }
                return
            }
            // If the server hasn't pinged within the stale window, the
            // connection is effectively dead (half-open socket / server closed
            // without a close frame). Force a reconnect so the UI reflects the
            // real state and the client re-subscribes promptly.
            if (Date.now() - lastPingAt > HEARTBEAT_STALE_MS) {
                disconnect()
                if (reconnect.shouldReconnect()) {
                    reconnect.scheduleReconnect()
                }
            }
        }
    }, HEARTBEAT_CHECK_INTERVAL_MS)
}

function stopHeartbeat() {
    if (heartbeatTimer) {
        clearInterval(heartbeatTimer)
        heartbeatTimer = null
    }
}

/**
 * Push notification title/alert formatting.
 * Aligned with DingTalk templates (the canonical source of truth).
 *
 * session_update:
 *   completed:          title=SessionCompleted, alert=responsePreview || SessionCompleted
 *   cancelled:          title=SessionCancelled, alert=responsePreview || SessionCancelled
 *   permission_pending: title=ActionRequired,  alert=toolName || ActionRequired
 *
 * task_update:
 *   running:            title=TaskStarted,     alert=taskName
 *   completed:          title=TaskCompleted,   alert=responsePreview || TaskCompleted
 *   failed:             title=TaskFailed,      alert=responsePreview || TaskFailed
 *   cancelled:          title=TaskCancelled,   alert=responsePreview || TaskCancelled
 */
function showEventBrowserNotification(event: string, data: ServerEvent['data']) {
    if (!data) return

    // Only show native browser notifications when push_mode is "native"
    const pushMode = serverConfig.value?.push_mode as string || 'native'
    if (pushMode !== 'native') return

    // Only show notification when page is not focused
    if (document.visibilityState === 'visible' && document.hasFocus()) return

    let title: string
    let alert_: string
    let onClick: (() => void) | undefined

    if (event === 'session_update') {
        const status = data.status
        if (status !== 'completed' && status !== 'cancelled' && status !== 'permission_pending') return

        const toolName = data.tool_name || ''

        // Default title/alert per status
        if (status === 'completed') {
            title = gt('chat.push.sessionCompleted')
            alert_ = plainPreview(data) || gt('chat.push.sessionCompleted')
        } else if (status === 'cancelled') {
            title = gt('chat.push.sessionCancelled')
            alert_ = plainPreview(data) || gt('chat.push.sessionCancelled')
        } else {
            // permission_pending
            title = gt('chat.push.actionRequired')
            alert_ = toolName || gt('chat.push.actionRequired')
        }

        // Click: navigate to the session
        const sessionId = data.session_id
        const projectPath = data.project_path
        if (sessionId) {
            onClick = () => {
                window.dispatchEvent(new CustomEvent('clawbench-open-session', {
                    detail: { sessionId, projectPath },
                }))
            }
        }
    } else if (event === 'task_update') {
        const status = data.status
        if (status !== 'running' && status !== 'completed' && status !== 'failed' && status !== 'cancelled') return

        const sessionTitle = data.session_title || ''

        if (status === 'running') {
            title = gt('chat.push.taskStarted')
            alert_ = sessionTitle || gt('chat.push.taskStarted')
        } else if (status === 'completed') {
            title = gt('chat.push.taskCompleted')
            alert_ = plainPreview(data) || gt('chat.push.taskCompleted')
        } else if (status === 'failed') {
            title = gt('chat.push.taskFailed')
            alert_ = plainPreview(data) || gt('chat.push.taskFailed')
        } else {
            // cancelled
            title = gt('chat.push.taskCancelled')
            alert_ = plainPreview(data) || gt('chat.push.taskCancelled')
        }

        // Click: navigate to the task
        const taskId = data.task_id
        const executionId = data.execution_id
        const projectPath = data.project_path
        if (taskId) {
            onClick = () => {
                window.dispatchEvent(new CustomEvent('clawbench-open-task', {
                    detail: { taskId, executionId, projectPath },
                }))
            }
        }
    } else {
        return
    }

    try {
        if (pushMode === 'native') {
            playNotificationSound()
            showBrowserNotification(title, {
                body: alert_,
                tag: `clawbench-${event}-${data.session_id || data.task_id || Date.now()}`,
                nav: {
                    sessionId: data.session_id,
                    taskId: data.task_id,
                    executionId: data.execution_id,
                    projectPath: data.project_path,
                },
                onClick,
            })
        }
    } catch {
        // Non-critical
    }
}

export function useGlobalEvents() {
    // WebSocket connection status: 'connected' | 'reconnecting' | 'disconnected'
    const wsStatus = computed(() => {
        if (connected.value) return 'connected'
        if (reconnect.reconnecting.value) return 'reconnecting'
        return 'disconnected'
    })

    function onEvent(handler: EventHandler) {
        handlers.push(handler)
        return () => {
            const idx = handlers.indexOf(handler)
            if (idx !== -1) handlers.splice(idx, 1)
        }
    }

    // Visibility change: disconnect WebSocket on background in app mode.
    // Mobile OS throttles/kills background connections, so keeping WS alive
    // is unreliable and wastes resources. The heartbeat monitor may keep
    // reconnecting a connection that the OS will just kill again.
    // In browser mode, keep WS alive on background so that browser
    // notifications can be shown for terminal events (completed/cancelled/
    // permission_pending/failed). Desktop browsers keep WS alive in background.
    //
    // Design principle: the foreground ('visible') branch is self-contained —
    // it resets reconnect state and reconnects without depending on any timer
    // that may have been scheduled during the background ('hidden') branch.
    // This eliminates the old race where setTimeout(reset, 100) was frozen by
    // Android's pauseTimers() and fired unpredictably (or never) on resume.
    function handleVisibilityChange() {
        if (document.visibilityState === 'visible') {
            // Returning to foreground — self-contained state reset + reconnect.
            // Always reset reconnect state first (it may be disabled from
            // background or have stale attempt counts from backgrounded
            // reconnect attempts that the OS killed).
            reconnect.reset()
            // Reconnect if disconnected
            if (!connected.value) connect()
            // Emit a custom event that other composables can listen to
            window.dispatchEvent(new CustomEvent('clawbench-foreground'))
        } else {
            if (isAppMode.value) {
                // App mode: disconnect WebSocket on background.
                // Disable reconnect to prevent the onclose handler from
                // scheduling reconnects while backgrounded (the OS will
                // just kill them again, wasting resources and battery).
                disconnect()
                reconnect.disable()
                // No setTimeout(reset, 100) here — the foreground branch
                // handles the reset atomically. The old setTimeout approach
                // was fragile: Android pauseTimers() froze it, and even
                // without pauseTimers it created a 100ms window where
                // reconnect was disabled but no foreground event had fired.
            }
            // Browser mode: keep WS alive for background notifications
        }
    }

    let initialized = false
    function init() {
        if (initialized) return
        initialized = true
        document.addEventListener('visibilitychange', handleVisibilityChange)
        // Initial connect
        connect()
    }

    function destroy() {
        document.removeEventListener('visibilitychange', handleVisibilityChange)
        disconnect()
        // ISS-192: Clear handlers and state on destroy to prevent stale closures
        // from firing after SPA hot project switch.
        handlers.length = 0
        processedEventIds.clear()
        lastPingAt = 0
        lastEventAt = 0
        hasConnectedOnce.value = false
        initialized = false
    }

    return {
        connected,
        hasConnectedOnce,
        wsStatus,
        connect,
        disconnect,
        onEvent,
        sendWsMessage: send,
        init,
        destroy,
    }
}
