import { ref, computed } from 'vue'
import { useReconnect } from './useReconnect'
import { useAppMode } from './useAppMode'
import { showBrowserNotification } from './useNotification'
import { playNotificationSound } from './useNotificationSound'
import { gt } from './useLocale'

// Event types from server
interface ServerEvent {
    type: string           // "event" | "ping"
    id?: string            // event ID for dedup
    event?: string         // "session_update" | "task_update" | "queue_update"
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
        tool_name?: string
        project_path?: string
    }
}

// Client message types
type ClientMessage =
    | { type: 'ack'; id: string }
    | { type: 'pong' }

type EventHandler = (event: string, data: ServerEvent['data']) => void

// Module-level singleton state
const connected = ref(false)
const handlers: EventHandler[] = []
const processedEventIds = new Set<string>()
const MAX_PROCESSED_IDS = 100
let ws: WebSocket | null = null
let heartbeatTimer: ReturnType<typeof setInterval> | null = null
const MISSED_PONG_THRESHOLD = 2
let missedPongs = 0

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
    maxAttempts: 3,
    baseDelay: 2000,
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

// Same as backend model.ResponsePreviewMaxRunes = 512
const PUSH_ALERT_MAX_CODE_POINTS = 512

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
                ;(window as any).AndroidNative?.updateLastSeenEventId(latestId)
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
        missedPongs = 0
        reconnect.reset()

        // Fetch missed events that occurred while offline
        fetchPendingEvents()

        // Start heartbeat monitoring
        startHeartbeat()
    }

    ws.onmessage = (event) => {
        try {
            const msg: ServerEvent = JSON.parse(event.data)

            if (msg.type === 'ping') {
                send({ type: 'pong' })
                missedPongs = 0
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
                if (msg.event === 'summary_update' && (msg.data as any)?.targetType === 'chat_message') {
                    window.dispatchEvent(new CustomEvent('clawbench-summary-update', { detail: msg.data }))
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
                    const status = (msg.data as any)?.status as string | undefined
                    const isTerminal = (msg.event === 'session_update' && (status === 'completed' || status === 'cancelled' || status === 'permission_pending'))
                        || (msg.event === 'task_update' && (status === 'completed' || status === 'failed' || status === 'cancelled'))
                    if (isTerminal) {
                        localStorage.setItem(LAST_SEEN_KEY, msg.id)
                        // Sync cursor to Android SharedPreferences so that the native
                        // fetchPendingEvents() won't re-deliver these events when
                        // the app switches to background.
                        try {
                            ;(window as any).AndroidNative?.updateLastSeenEventId(msg.id)
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
    missedPongs = 0
    heartbeatTimer = setInterval(() => {
        if (ws && ws.readyState === WebSocket.OPEN) {
            missedPongs++
            if (missedPongs > MISSED_PONG_THRESHOLD) {
                // Connection seems dead, reconnect
                disconnect()
                if (reconnect.shouldReconnect()) {
                    reconnect.scheduleReconnect()
                }
            }
        }
    }, 35000) // Check every 35s (server pings every 30s)
}

function stopHeartbeat() {
    if (heartbeatTimer) {
        clearInterval(heartbeatTimer)
        heartbeatTimer = null
    }
}

/**
 * Show browser notification for WS events when page is in background.
 * - session_update: completed, cancelled, permission_pending
 * - task_update: completed, failed, cancelled
 * Title/alert formatting follows the same rules as the backend:
 *   1. Initial defaults: title=PushTaskCompleted, alert=PushSessionEnded
 *      (task_update: alert=PushScheduledTaskDone)
 *   2. permission_pending: title=PushPermissionPending, alert=toolName||PushPermissionPending
 *   3. If sessionTitle non-empty && not permission_pending: title="Done:"+sessionTitle
 *   4. If responsePreview non-empty: alert=truncate(responsePreview)
 */
function showEventBrowserNotification(event: string, data: ServerEvent['data']) {
    if (!data) return

    // Only show notification when page is not focused
    if (document.visibilityState === 'visible' && document.hasFocus()) return

    let title: string
    let alert_: string
    let onClick: (() => void) | undefined

    if (event === 'session_update') {
        const status = data.status
        if (status !== 'completed' && status !== 'cancelled' && status !== 'permission_pending') return

        // Default title/alert (same as backend, with cancelled-specific defaults)
        title = gt('chat.push.taskCompleted')
        alert_ = status === 'cancelled' ? gt('chat.push.sessionCancelled') : gt('chat.push.sessionEnded')

        const sessionTitle = data.session_title || ''
        const responsePreview = data.response_preview || ''

        // Permission pending: override title/alert
        if (status === 'permission_pending') {
            title = gt('chat.push.permissionPending')
            alert_ = data.tool_name || gt('chat.push.permissionPending')
        }

        // If sessionTitle non-empty and not permission_pending: "Done:"+sessionTitle
        if (sessionTitle && status !== 'permission_pending') {
            title = 'Done:' + sessionTitle
        }

        // If responsePreview non-empty: use as alert
        if (responsePreview) {
            alert_ = truncateForPush(responsePreview)
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
        if (status !== 'completed' && status !== 'failed' && status !== 'cancelled') return

        // Default title/alert (with status-specific defaults)
        title = gt('chat.push.taskCompleted')
        if (status === 'failed') {
            alert_ = gt('chat.push.taskFailed')
        } else if (status === 'cancelled') {
            alert_ = gt('chat.push.taskCancelled')
        } else {
            alert_ = gt('chat.push.scheduledTaskDone')
        }

        const sessionTitle = data.session_title || ''
        const responsePreview = data.response_preview || ''

        // If sessionTitle non-empty: "Done:"+sessionTitle
        if (sessionTitle) {
            title = 'Done:' + sessionTitle
        }

        // If responsePreview non-empty: use as alert
        if (responsePreview) {
            alert_ = truncateForPush(responsePreview)
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
        playNotificationSound()
        showBrowserNotification(title, {
            body: alert_,
            tag: `clawbench-${event}-${data.session_id || data.task_id || Date.now()}`,
            onClick,
        })
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
    function handleVisibilityChange() {
        if (document.visibilityState === 'visible') {
            // Returning to foreground — reconnect if disconnected and do full state pull
            if (!connected.value) connect()
            // Emit a custom event that other composables can listen to
            window.dispatchEvent(new CustomEvent('clawbench-foreground'))
        } else {
            if (isAppMode.value) {
                // App mode: always disconnect WebSocket on background
                disconnect()
                reconnect.disable() // prevent auto-reconnect while backgrounded
                // Re-enable reconnect for next foreground
                setTimeout(() => reconnect.reset(), 100)
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
        missedPongs = 0
        initialized = false
    }

    return {
        connected,
        wsStatus,
        connect,
        disconnect,
        onEvent,
        init,
        destroy,
    }
}
