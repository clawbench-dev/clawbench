import { ref, computed } from 'vue'
import { useReconnect } from './useReconnect'
import { useForgeUnread } from './useForgeUnread'
import { useAppMode } from './useAppMode'
import { showBrowserNotification } from './useNotification'
import { playNotificationSound } from './useNotificationSound'
import { gt } from './useLocale'
import { stripMarkdownPreview } from '@/utils/format'
import { eventKindLabel, unreadReasonLabel } from '@/utils/forgeEventLabels'
import { getNative } from '@/utils/clawbenchNative'
import { appLog } from '@/utils/appLog'

// Event types from server
export interface ServerEvent {
    type: string           // "event" | "ping"
    id?: string            // event ID for dedup
    event?: string         // "session_update" | "task_update"
    // True when the server replayed this event from the replay buffer right
    // after a WS reconnect (events.go GetBufferedEvents). Such events are
    // caught-up history, NOT live completions — consumers must suppress
    // completion popups / notifications for them.
    replayed?: boolean
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
        last_user_has_files?: boolean // whether that user message carried attachments (completed only)
        agent_id?: string // agent that ran the session/execution (completed only)
        tool_name?: string
        project_path?: string
        // chat_stream events (e.g. user_message) carry a ChatStreamData body:
        // { session_id, event_type, payload }. These are optional and loose —
        // existing notification/status fields above remain untouched. The
        // actual shape is validated downstream in useChatStream's onEvent
        // handler (it casts to ChatStreamEventData and guards session_id).
        event_type?: string
        payload?: unknown
        // forge_event body (ForgeEventDispatcher.HandleChange): the derived
        // change plus the item it belongs to. Optional/loose for the same
        // reason as above — only the forge branch reads them.
        event?: ForgeEventIdentity
        item?: ForgeItemIdentity
    }
}

/** Identity of a derived forge change (snake_case on the wire). */
export interface ForgeEventIdentity {
    platform?: string
    host?: string
    owner?: string
    repo?: string
    /** "issue" | "pr" | "pipeline" */
    item_type?: string
    number?: number
    /** "opened" | "closed" | "merged" | "reopened" | "commented" | "pipeline_done" */
    event_type?: string
    /**
     * Project that has this repository bound. The forge panel is project-scoped,
     * so a click must switch to this project before opening the tab — otherwise
     * it lands on a panel where the changed row does not exist.
     */
    project_path?: string
}

/** The issue/PR/pipeline a forge change happened on. */
export interface ForgeItemIdentity {
    type?: string
    number?: number
    title?: string
    url?: string
    state?: string
    author?: string
}

// Client message types
type ClientMessage =
    | { type: 'pong' }
    | { type: 'subscribe'; session_id: string }
    | { type: 'unsubscribe'; session_id: string }
    | { type: 'cancel'; session_id: string }
    | { type: 'permission_respond'; session_id: string; tool_call_id: string; option_id: string; cancelled: boolean }
    // Declares interest in server-pushed system-resource metrics. The server
    // only samples while at least one client declares `metrics_enabled`, and at
    // the fastest interval any of them asked for.
    | { type: 'metrics_preference'; metrics_enabled: boolean; metrics_interval_ms?: number }

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

// Client ID — identifies this browser/device across sessions.
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

// Last-seen event cursor — tracks the newest terminal event already seen.
// Session-only (in-memory, deliberately NOT persisted): when the page/app is
// reloaded the cursor starts empty, so fetchPendingEvents() skips to the newest
// event and never replays stale completion notifications from a previous run.
// Within a live session the cursor advances on every terminal event, so an
// in-page WS reconnect still recovers events missed while disconnected.
let lastSeenEventId = ''

// True while a replayed event is being dispatched — either fetched via
// fetchPendingEvents() (the WS was down when it was originally broadcast) or
// replayed from the server's WS replay buffer right after a reconnect (the
// server tags those messages with `replayed: true`). Consumers use this to
// distinguish replayed background events from live foreground ones (e.g.
// suppress completion popups / mark-read).
const isReplayingEvents = ref(false)

// Mirror the in-memory cursor to the host app (Android native background
// service). The Android cursor is persisted separately in SharedPreferences and
// survives process kills, so keeping it in sync prevents the background service
// from re-delivering terminal events the user already saw in the foreground.
function syncNativeCursor(eventId: string) {
    try {
        getNative()?.updateLastSeenEventId(eventId)
    } catch {
        // Non-critical
    }
}

const { isAppMode, isDesktopApp } = useAppMode()

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
 *
 * Exported because the in-app completion notification needs the exact same
 * "title + one plain line" body as the system notification — if the two
 * diverged, the same completion would read differently depending on whether
 * the page happened to be focused.
 */
export function plainPreview(data: ServerEvent['data']): string {
    if (!data) return ''
    if (data.response_preview_plain) return truncateForPush(data.response_preview_plain)
    if (data.response_preview) return stripMarkdownPreview(data.response_preview, PUSH_ALERT_MAX_CODE_POINTS)
    return ''
}

async function fetchPendingEvents() {
    try {
        const lastSeenId = lastSeenEventId
        const url = lastSeenId
            ? `/api/ai/events/pending?after=${encodeURIComponent(lastSeenId)}`
            : '/api/ai/events/pending'

        const resp = await fetch(url, { credentials: 'same-origin' })
        if (!resp.ok) return

        const data = await resp.json()
        const events: Array<{ event_id: string; event_type: string; payload: string }> = data.events || []
        if (events.length === 0) return

        // No cursor (fresh page load / app restart — the in-memory cursor is
        // empty): the client has never seen any event in this run, so there is
        // nothing to replay. The server's pending_events table keeps up to 24h
        // of terminal events (completed/cancelled/failed); replaying them all
        // would flood a fresh client with dozens of stale completion
        // popups/notifications. Instead, advance the cursor to the newest
        // event so future reconnects start from here.
        if (!lastSeenId) {
            const latest = events[events.length - 1]
            if (latest.event_id) {
                lastSeenEventId = latest.event_id
                // Also advance the native cursor so the Android background
                // service won't replay the 24h backlog after a restart.
                syncNativeCursor(latest.event_id)
            }
            return
        }

        let latestId = lastSeenId
        // Flag replayed events so consumers can distinguish a background
        // completion that is being caught up (WS was down while it happened)
        // from a live event. Consumers like the chat completion path use this
        // to avoid auto-marking a session read for a completion the user never
        // saw happen — the unread badge must survive until the user opens it.
        isReplayingEvents.value = true
        try {
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

                // Show browser notification. skipReplay=true: these events are
                // caught-up history (WS was down while they happened), never
                // live completions — the user didn't watch the session/task run,
                // so no notification. Mirrors the WS replay path below.
                showEventBrowserNotification(msg.event!, msg.data, true)

                if (msg.id) latestId = msg.id
            }
        } finally {
            isReplayingEvents.value = false
        }

        // Update cursor
        if (latestId !== lastSeenId) {
            lastSeenEventId = latestId
            syncNativeCursor(latestId)
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

                // Reset the per-message replay flag first so it never leaks from
                // the previous message (e.g. a replayed burst followed by a live
                // event must not have the live event suppressed).
                isReplayingEvents.value = false

                // Replay-buffer events (tagged `replayed: true` by the server
                // when it replays the buffered history right after a reconnect)
                // are caught-up history, not live completions — flag them so
                // consumers can suppress completion popups / notifications.
                if (msg.replayed) {
                    isReplayingEvents.value = true
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

                // Forge (GitHub/GitLab) change events: re-derive the unread
                // badge on LIVE events only. Replayed events are caught-up
                // history, so they must not inflate the badge after a reconnect.
                // The count is refetched (debounced) rather than incremented
                // locally so it stays correct after the tab has been opened.
                if (msg.event === 'forge_event' && !msg.replayed) {
                    useForgeUnread().onForgeEvent()
                }

                // Browser notification: when page is not focused, show browser
                // notification for terminal events (completed/cancelled/failed/
                // permission_pending). Replayed events are caught-up history
                // and never notify.
                showEventBrowserNotification(msg.event!, msg.data, true)

                // Update last seen event cursor for offline recovery
                // Only update for terminal-state events that are persisted server-side
                if (msg.id) {
                    const status = (msg.data as Record<string, unknown>)?.status as string | undefined
                    const isTerminal = (msg.event === 'session_update' && (status === 'completed' || status === 'cancelled' || status === 'permission_pending'))
                        || (msg.event === 'task_update' && (status === 'completed' || status === 'failed' || status === 'cancelled'))
                    if (isTerminal) {
                        lastSeenEventId = msg.id
                        // Keep the Android native cursor in sync so background
                        // push won't re-deliver an event already seen here.
                        syncNativeCursor(msg.id)
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
 *
 * forge_event:
 *   title = "owner/repo 议题 #12 · 合并" (kind + item + reason)
 *   alert = the item title (the one field the event does not otherwise carry)
 */
/**
 * Show a browser notification for a terminal event.
 *
 * The system-notification decision is gated by the local `desktopNotification`
 * setting (inside showBrowserNotification) and by page focus — NOT by the
 * server-side `push_mode`. push_mode selects the mobile/IM channel; a user on
 * DingTalk push still wants their desktop tab to notify them. Gating on it here
 * also made the switch unreachable for anyone who had picked DingTalk/飞书.
 *
 * @param skipReplay when true, a replay-phase (caught-up history) event does
 *   NOT produce a notification — it is suppressed like a background event
 *   would be. Callers use this for events that arrive via fetchPendingEvents()
 *   or the WS replay buffer after a reconnect.
 */
function showEventBrowserNotification(event: string, data: ServerEvent['data'], skipReplay = false) {
    if (!data) return

    // A replay-phase event is caught-up history, not a live completion — never
    // notify for it, even if the page is in the background right now.
    if (skipReplay && isReplayingEvents.value) return

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
    } else if (event === 'forge_event') {
        const ev = data.event
        const item = data.item
        // Without an identity there is nothing meaningful to render (and no
        // reason to interrupt the user), so bail rather than emit a bare title.
        if (!ev?.event_type) return

        const slug = [ev.owner, ev.repo].filter(Boolean).join('/')
        const kind = eventKindLabel(ev.item_type || item?.type || '')
        const reason = unreadReasonLabel(ev.event_type)
        // A pipeline carries no item number (the syncer builds it with Number 0),
        // so the falsy check below already drops it — rendering "#0" would look
        // like a broken reference.
        const num = ev.number || item?.number
        const ref = num ? ` #${num}` : ''
        title = [slug, `${kind}${ref}`.trim(), reason].filter(Boolean).join(' · ')
        alert_ = item?.title || reason

        // Click: open the Issues & PRs tab, switching projects first when the
        // change belongs to a repository bound by another project. The panel has
        // no item-level deep link, so the destination is the tab where the row
        // and its unread badge live.
        const projectPath = ev.project_path
        onClick = () => {
            window.dispatchEvent(new CustomEvent('clawbench-open-forge', {
                detail: { projectPath },
            }))
        }
    } else {
        return
    }

    try {
        playNotificationSound()
        showBrowserNotification(title, {
            body: alert_,
            tag: `clawbench-${event}-${data.session_id || data.task_id || data.event?.repo || Date.now()}`,
            nav: {
                sessionId: data.session_id,
                taskId: data.task_id,
                executionId: data.execution_id,
                projectPath: data.project_path,
                // Forge notifications carry no session/task id, so the native
                // shell needs an explicit discriminator — otherwise its
                // sessionId/taskId branches both miss and the click is dropped.
                forge: event === 'forge_event',
            },
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
    //
    // The Electron shell reports isNativeApp() === true (it is a native host),
    // but it must be treated like a desktop browser, NOT like Android: its
    // window is merely minimized and the process keeps running, so the socket
    // is never killed by an OS. Dropping it here would mean no event ever
    // reaches showBrowserNotification while minimized — i.e. no notifications
    // at all, defeating the whole purpose of the desktop shell. Hence the
    // isDesktopApp guard.
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
            if (isAppMode.value && !isDesktopApp.value) {
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
            // Browser mode (and the Electron shell): keep WS alive so
            // background/minimized notifications still arrive.
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
        lastSeenEventId = ''
        lastPingAt = 0
        lastEventAt = 0
        hasConnectedOnce.value = false
        initialized = false
    }

    return {
        connected,
        hasConnectedOnce,
        wsStatus,
        isReplayingEvents,
        connect,
        disconnect,
        onEvent,
        sendWsMessage: send,
        init,
        destroy,
    }
}

// ── Test-only helpers ────────────────────────────────────────────────────────
// The last-seen cursor is intentionally session-only (in-memory). These hooks
// let tests seed/read it since localStorage is no longer the backing store.
/** Seed the in-memory last-seen cursor (simulates an established session). */
export function _seedLastSeenEventIdForTesting(id: string) {
    lastSeenEventId = id
}

/** Read the current in-memory last-seen cursor. */
export function _getLastSeenEventIdForTesting(): string {
    return lastSeenEventId
}

