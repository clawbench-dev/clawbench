import { ref, computed } from 'vue'
import { appLog } from '@/utils/appLog'
import { useReconnect } from './useReconnect'
import { useAppMode } from './useAppMode'

const TAG = 'GlobalEvents'

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
    }
}

// Client message types
type ClientMessage =
    | { type: 'ack'; id: string }
    | { type: 'pong' }
    | { type: 'register'; push_reg_id: string }

type EventHandler = (event: string, data: ServerEvent['data']) => void

// Module-level singleton state
const connected = ref(false)
const pushAvailable = ref(false)
const pushRegistered = ref(false)
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

/**
 * Check whether push notifications are available.
 * In app mode: query AndroidNative.isPushAvailable() (set by MainActivity after fetchPushConfig).
 * In web mode: fetch from /api/push/config endpoint.
 * Also sets pushRegistered if JPush is available AND has a registration ID.
 */
async function checkPushAvailable() {
    if (isAppMode.value) {
        // Android native bridge — already fetched by MainActivity.fetchPushConfig()
        const native = (window as any).AndroidNative
        pushAvailable.value = !!native?.isPushAvailable?.()
        // JPush registered = available + has registration ID from SDK
        const regId = native?.getPushRegistrationId?.()
        pushRegistered.value = pushAvailable.value && !!regId
    } else {
        // Web mode — check server config
        try {
            const resp = await fetch('/api/push/config')
            if (resp.ok) {
                const data = await resp.json()
                pushAvailable.value = !!data.jpush_enabled && !!data.jpush_app_key
            }
        } catch {
            pushAvailable.value = false
        }
        // Web mode doesn't run JPush SDK, pushRegistered is always false
        pushRegistered.value = false
    }
}

/**
 * Register JPush Registration ID with the server via WS "register" message.
 * This is the preferred path — pushRegID is tied to the WS session.
 * If WS is not connected, falls back to connecting first (register will be sent on open).
 */
function registerPushId() {
    if (!isAppMode.value) return
    const regId = (window as any).AndroidNative?.getPushRegistrationId?.()
    if (!regId) {
        appLog.d(TAG, 'registerPushId: no registration ID available yet (JPush SDK may still be initializing)')
        return
    }
    send({ type: 'register', push_reg_id: regId })
    pushRegistered.value = true
    appLog.d(TAG, 'registerPushId: sent via WS, regId:', regId)
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

        // Check push availability (non-blocking)
        checkPushAvailable()

        // Register push ID via WS message (JPush SDK may have obtained
        // the registration ID since last connect). This is idempotent.
        registerPushId()

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

                // Send ack
                if (msg.id) {
                    send({ type: 'ack', id: msg.id })
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

    // Visibility change: always disconnect WebSocket on background.
    // Mobile OS throttles/kills background connections, so keeping WS alive
    // is unreliable and wastes resources. The heartbeat monitor may keep
    // reconnecting a connection that the OS will just kill again.
    // On foreground, we reconnect and do a full state pull.
    function handleVisibilityChange() {
        if (document.visibilityState === 'visible') {
            // Returning to foreground — reconnect and do full state pull
            connect()
            // Emit a custom event that other composables can listen to
            window.dispatchEvent(new CustomEvent('clawbench-foreground'))
        } else {
            // Going to background — always disconnect WebSocket
            disconnect()
            reconnect.disable() // prevent auto-reconnect while backgrounded
            // Re-enable reconnect for next foreground
            setTimeout(() => reconnect.reset(), 100)
        }
    }

    // Handle JPush registration event from native layer.
    // This fires when JPushReceiver.onRegister() is called — typically a few
    // seconds after app startup, when the JPush SDK has finished registering
    // with JPush servers and obtained a Registration ID.
    function handlePushRegistered(e: Event) {
        const detail = (e as CustomEvent).detail
        const regId = detail?.registrationId
        if (!regId) return

        appLog.d(TAG, 'JPush registration ID received from native:', regId)
        // Update push availability state
        pushAvailable.value = true
        // Register via WS — this is the primary registration path now.
        // If WS is connected, send immediately. If not, it'll be sent on next connect.
        registerPushId()
    }

    let initialized = false
    function init() {
        if (initialized) return
        initialized = true
        document.addEventListener('visibilitychange', handleVisibilityChange)
        // Listen for JPush registration event from native layer.
        // This fires when JPushReceiver.onRegister() is called, which may happen
        // seconds after app startup (JPush SDK registration is async).
        window.addEventListener('clawbench-push-registered', handlePushRegistered)
        // Initial connect
        connect()
        // Register push ID via WS (may return empty if JPush hasn't initialized yet —
        // the clawbench-push-registered event will re-trigger registration).
        registerPushId()
    }

    function destroy() {
        document.removeEventListener('visibilitychange', handleVisibilityChange)
        window.removeEventListener('clawbench-push-registered', handlePushRegistered)
        disconnect()
        // ISS-192: Clear handlers and state on destroy to prevent stale closures
        // from firing after SPA hot project switch.
        handlers.length = 0
        processedEventIds.clear()
        missedPongs = 0
        pushAvailable.value = false
        pushRegistered.value = false
        initialized = false
    }

    return {
        connected,
        wsStatus,
        pushAvailable,
        pushRegistered,
        connect,
        disconnect,
        onEvent,
        init,
        destroy,
    }
}
