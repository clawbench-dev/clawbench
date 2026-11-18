import { ref, computed } from 'vue'
import { useReconnect } from './useReconnect'
import { useForgeUnread } from './useForgeUnread'
import { useAppMode } from './useAppMode'
import { useAppForeground, onAppResume } from './useAppForeground'
import { showBrowserNotification } from './useNotification'
import { playNotificationSound } from './useNotificationSound'
import { useToast, type ToastIconKey, type ToastType } from './useToast'
import { shouldNotifyMeeting } from '@/utils/meetingVisibility'
import { gt } from './useLocale'
import { stripMarkdownPreview } from '@/utils/format'
import { eventKindLabel, unreadReasonLabel } from '@/utils/forgeEventLabels'
import { forgeTargetItemKey } from '@/composables/useForge'
import type { ForgeTarget } from '@/composables/useForgeNavigation'
import { getNative } from '@/utils/clawbenchNative'
import { appLog } from '@/utils/appLog'
import { localizedToolName } from '@/utils/toolNames'

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
        // chat_history id of the assistant message the preview was taken from.
        // Lets a notification click scroll to that message, not just the
        // session bottom (terminal statuses only; absent when none qualified).
        message_id?: number
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
        // usage_meter_update body: the server renders the wording (it carries
        // numbers) and ships it ready to display, so there is no status enum to
        // translate on the client.
        title?: string
        body?: string
        actual?: number
        expected?: number
        quota?: number
        metric?: string
        // live_event body (ws.MeetingEventData): one thing that happened in a
        // running meeting. `title` (the meeting name) and `body` (one localized
        // sentence) are shared with the usage-meter shape above — the producer
        // renders them server-side for the same reason (no request in scope).
        token?: string
        viewer_id?: string
        viewer_name?: string
        // mail_event body (ws.MailEventData): new inbox mail arrived.
        // `title`/`body` are rendered server-side like the usage/meeting
        // shapes above; mail_message_id is the cached row the click opens.
        // `count` (unread total) reuses the field declared above.
        mail_message_id?: number
        account_id?: number
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
    /**
     * The CI run id, for a pipeline event only (0 otherwise). A pipeline's
     * `number` is always 0, so this is the only value that names which run
     * changed — without it the frontend cannot deep-link to (or mark read) a
     * specific run.
     */
    run_id?: number
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

// ── Liveness ────────────────────────────────────────────────────────────────
// `lastMessageAt` is the single liveness signal: refreshed by pings AND by real
// events, so "no message for a while" covers both a half-open socket (the far
// end is gone, the client cannot tell) and a frozen event loop that kept the
// TCP connection open.
//
// There used to be a SECOND, ping-only threshold (90s) checked alongside it. It
// could never fire: every ping also refreshed this timestamp, so "90s with no
// ping" was always preceded by "60s with no message at all". A redundant
// detector is not free — it is one more thing to keep in sync, and it made the
// liveness story look more complicated than it is. Removed.
const MESSAGE_STALE_MS = 60000
// How often liveness is re-evaluated. This is the granularity of both bounds
// below, so it is deliberately much shorter than either: a 15s tick on top of a
// 15s handshake bound would let a stuck handshake sit unnoticed for 30s. The
// check is a couple of comparisons, so a short tick costs nothing.
const HEARTBEAT_CHECK_INTERVAL_MS = 5000
// A healthy handshake takes well under a second, so this only ever fires for a
// socket that is genuinely stuck. Kept separate from MESSAGE_STALE_MS because
// the two measure different things: one is "the far end stopped talking", the
// other is "we never got to talking at all".
const HANDSHAKE_TIMEOUT_MS = 15000

// ── Foreground resume (Android / HarmonyOS) ─────────────────────────────────
// Coming back from the background is a different regime from a mid-session
// blip: the OS froze the page, the radio re-attaches, and the socket we left
// behind is usually already dead at the far end even though it still reads
// OPEN. Waiting for evidence (a 60s silence, a failed write, a 15s handshake
// bound) is what made "back from background" feel like "stuck on 正在重连".
// The resume path therefore rebuilds the connection unconditionally and
// redials on a much tighter ladder — the user is looking at the screen.
//
// The dialed-on-resume socket also gets a shorter handshake bound and a 1s
// liveness tick, because a handshake that hangs right after a resume is the
// common mobile case (network stack still re-attaching) and waiting the normal
// 15s for it is exactly the delay being complained about. Both are restored to
// the normal values as soon as the socket opens.
const RESUME_HANDSHAKE_TIMEOUT_MS = 6000
const RESUME_CHECK_INTERVAL_MS = 1000
const RESUME_RECONNECT_BASE_DELAY_MS = 500

let lastMessageAt = 0
// When the current socket began connecting — needed to time out a handshake that
// neither opens nor closes (see HANDSHAKE_TIMEOUT_MS). The bound itself is
// per-dial (a resume dial uses a tighter one), so it lives next to it.
let connectStartedAt = 0
let handshakeTimeoutMs = HANDSHAKE_TIMEOUT_MS
// True from a resume dial until that dial settles (opens or is dropped). Lets a
// second resume signal for the SAME physical resume leave the in-flight dial
// alone, while still rebuilding a socket that predates the resume (including a
// handshake stuck in CONNECTING since before the app was backgrounded — the
// case that must be killed, not preserved).
let resumeDialPending = false
// True from the first resume dial until a socket actually opens. Keeps the
// tighter resume pacing across the redials that follow a failed resume dial
// (connect() is re-entered with no arguments from the retry ladder), and is
// cleared in onopen so a later mid-session blip goes back to the normal pace.
let resumePacing = false

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

/**
 * Dismiss the host's native OS notification for a subject that was just read.
 *
 * The notification only auto-cancels on tap, so a user who opens the app and
 * reads the message leaves a stale notification that re-dispatches its deep
 * link when tapped later (the reported duplicate open/load). The server
 * broadcasts `status:"read"` when a session/task is marked read — from this or
 * any other client — which is the cue to clear it. Best-effort: the bridge
 * method is optional (older hosts) and a failure is non-critical.
 */
function dismissNativeNotification(taskId: string | undefined, sessionId: string | undefined) {
    try {
        const native = getNative()
        if (!native?.dismissEventNotification) return
        if (!taskId && !sessionId) return
        // The method is `void | Promise<void>` (Android sync, Electron invoke).
        // Swallow a rejection: ipcRenderer.invoke rejects if the handler throws,
        // and dismissal is best-effort — an unhandled rejection would surface
        // as noise. Mirrors useNotification.ts's nativeNotify handling.
        void Promise.resolve(native.dismissEventNotification(taskId ?? '', sessionId ?? '')).catch(() => {})
    } catch {
        // Non-critical
    }
}

/**
 * Tell the desktop shell that the session counters may have moved.
 *
 * The floating capsule is drawn by the main process, which has no WebSocket of
 * its own, so this is its only event-driven signal between poll ticks. Optional
 * on the bridge: Android drives its own refresh and older desktop builds have no
 * such channel, so an absent method is a no-op rather than an error.
 */
function notifyFloatingActivity() {
    try {
        getNative()?.notifyFloatingActivity?.()
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

function connect(opts?: { resumed?: boolean }) {
    disconnect()

    // Resume pacing: a shorter handshake bound and a 1s liveness tick, for the
    // dial made on returning to the foreground AND for the redials that follow
    // it while they keep failing (the network stack is still re-attaching; a
    // 15s handshake bound per attempt is exactly the delay being complained
    // about). Cleared the moment a socket opens, so the normal cadence resumes.
    const resumed = opts?.resumed === true || resumePacing
    resumePacing = resumed
    resumeDialPending = resumed
    handshakeTimeoutMs = resumed ? RESUME_HANDSHAKE_TIMEOUT_MS : HANDSHAKE_TIMEOUT_MS
    const checkInterval = resumed ? RESUME_CHECK_INTERVAL_MS : HEARTBEAT_CHECK_INTERVAL_MS

    const protocol = location.protocol === 'https:' ? 'wss:' : 'ws:'
    const url = `${protocol}//${location.host}/api/ai/events/ws?client_id=${clientId}`

    ws = new WebSocket(url)
    connectStartedAt = Date.now()
    // Start monitoring at dial time, not at open time. A socket that never opens
    // is exactly the case that needs watching, so the tick has to be running
    // while the handshake is still in flight. One timer covers every state.
    startHeartbeat(checkInterval)

    ws.onopen = () => {
        connected.value = true
        // True reconnect only if we've connected before — the app-startup first
        // connect already loads sessions/tasks/git, so dispatching here too
        // would duplicate those requests.
        const isReconnect = hasConnectedOnce.value
        hasConnectedOnce.value = true
        lastMessageAt = Date.now()
        // Back to the normal cadence: the resume-specific pacing was only for
        // the dials above.
        resumePacing = false
        resumeDialPending = false
        handshakeTimeoutMs = HANDSHAKE_TIMEOUT_MS
        reconnect.reset()

        // Fetch missed events that occurred while offline
        fetchPendingEvents()

        // Notify other composables to refresh stale state
        // (sessions, tasks, git — WS-push state that may have changed
        // while disconnected beyond the 10s buffer window)
        if (isReconnect) {
            window.dispatchEvent(new CustomEvent('clawbench-reconnect'))
        }
    }

    ws.onmessage = (event) => {
        try {
            const msg: ServerEvent = JSON.parse(event.data)
            lastMessageAt = Date.now()

            if (msg.type === 'ping') {
                send({ type: 'pong' })
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

                // Dismiss the host's native notification when the subject is
                // read. "read" is not a terminal/notifiable state, so it is not
                // handled by showEventBrowserNotification below — but it IS the
                // cue to clear a notification the user already acted on (the
                // reported stale-notification / duplicate deep-link defect).
                if ((msg.event === 'session_update' || msg.event === 'task_update')
                    && (msg.data as Record<string, unknown>)?.status === 'read') {
                    const d = msg.data as Record<string, unknown>
                    dismissNativeNotification(
                        d.task_id as string | undefined,
                        d.session_id as string | undefined,
                    )
                }

                // Dispatch summary_update as a custom event for ChatPanelContent
                if (msg.event === 'summary_update' && (msg.data as Record<string, unknown>)?.targetType === 'chat_message') {
                    window.dispatchEvent(new CustomEvent('clawbench-summary-update', { detail: msg.data }))
                }

                // Dispatch chat_recommendation for the chat input bar to auto-fill / show a suggestion chip
                if (msg.event === 'chat_recommendation') {
                    window.dispatchEvent(new CustomEvent('clawbench-recommendation', { detail: msg.data }))
                }

                // A session title changed outside the rename flow (automatic AI
                // rename). The client that renamed learns the title from its own
                // response; this event exists so OTHER clients — and this one,
                // when it did not initiate the rename — converge. Dispatched as a
                // window event so App.vue can update the open chat header and
                // refresh a mounted session list.
                if (msg.event === 'session_title_update') {
                    window.dispatchEvent(new CustomEvent('clawbench-session-title-update', { detail: msg.data }))
                }

                // Forge (GitHub/GitLab) change events: re-derive the unread
                // badge on LIVE events only. Replayed events are caught-up
                // history, so they must not inflate the badge after a reconnect.
                // The count is refetched (debounced) rather than incremented
                // locally so it stays correct after the tab has been opened.
                if (msg.event === 'forge_event' && !msg.replayed) {
                    useForgeUnread().onForgeEvent()
                }

                // Meeting events (remote-control request, someone joining, a
                // member starting to share, the minutes finishing). Deliberately
                // NOT routed through showEventBrowserNotification: its gate is
                // "is the page focused", while these must reach a host who is
                // using the app on another tab. See handleMeetingEvent.
                if (msg.event === 'live_event' && !msg.replayed) {
                    handleMeetingEvent(msg.data)
                }

                // Browser notification: when page is not focused, show browser
                // notification for terminal events (completed/cancelled/failed/
                // permission_pending). Replayed events are caught-up history
                // and never notify.
                showEventBrowserNotification(msg.event!, msg.data, true)

                // The desktop floating capsule keeps its own copy of the session
                // counters (it polls /api/ai/sessions/overview from the main
                // process, with no WebSocket of its own). Tell it when something
                // here could have changed them, so it does not wait out its 15s
                // poll. Reported on the same conditions as the notification
                // above, but through a SEPARATE bridge call: showBrowserNotification
                // is gated by the desktop-notification setting and by page focus,
                // and the capsule must track the counters either way.
                if (!msg.replayed) notifyFloatingActivity()

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

/**
 * Classify the current socket. `null` means healthy — anything else is a
 * reason to dial again.
 *
 * ── Why this is ONE function (do not split it up again) ─────────────────────
 * Deciding "is this connection usable" used to be spread over four places, each
 * asking a slightly different question:
 *
 *   1. the close handler            — "did it close?"
 *   2. a periodic tick (60s)        — "OPEN but no message?"
 *   3. a periodic tick (90s)        — "OPEN but no ping?"   (never fired)
 *   4. the foreground resume        — its own copy of all of the above
 *
 * Every one of them was written to plug a hole the others left, and each new
 * hole got a new detector instead of a better shared question. That is how the
 * "back from background, never reconnects, only a force-quit fixes it" bug
 * appeared: a fix on 2026-10-15 taught the resume path to leave a CONNECTING
 * socket alone (correct — do not tear down a healthy in-flight handshake), but
 * no existing detector covered a handshake that is stuck in CONNECTING forever,
 * so the one state that needed killing became the one state nothing looked at.
 *
 * So: adding a new "looks alive but is not" case means adding a branch HERE.
 * Do not hang a fifth timer or a second resume-path check off the side — that
 * is exactly the shape that produced the bug above. The test for whether this
 * refactor is still intact: `grep -n readyState useGlobalEvents.ts` should show
 * this function and `send()`, nothing else.
 *
 * ── The two clocks are not redundant ────────────────────────────────────────
 *
 *  - `handshake-timeout`: a socket stuck in CONNECTING. Mobile clients land
 *    here after returning from the background — the handshake was started while
 *    the OS had the page frozen, the network stack dropped it, and NEITHER
 *    onopen NOR onclose ever fires. A CONNECTING socket is not OPEN (so the
 *    silence check below never looks at it) and never closes (so no reconnect
 *    is scheduled). Left alone the client sits on "连接断开，正在重连…" until the
 *    app is force-quit and relaunched. A healthy handshake takes well under a
 *    second, so a generous bound is still short.
 *
 *  - `silent`: OPEN but no message for MESSAGE_STALE_MS. The server pings every
 *    30s, so silence means the other end is gone (half-open socket) or our own
 *    event loop was frozen while the TCP connection stayed open. This bound
 *    must stay above the ping interval, which is why it is its own constant
 *    rather than reusing HANDSHAKE_TIMEOUT_MS.
 *
 * They measure different things — "the far end stopped talking" versus "we
 * never got to talking at all" — so they cannot be merged into one number. A
 * ping-only threshold that was ALSO checked here (90s) was removed: every ping
 * refreshed the same timestamp the silence check reads, so it could never fire
 * first. One redundant detector is cheap to write and expensive to reason
 * about later.
 */
type SocketFailure = 'absent' | 'handshake-timeout' | 'silent' | null

function socketFailure(now: number): SocketFailure {
    if (!ws) return 'absent'
    if (ws.readyState === WebSocket.CONNECTING) {
        return now - connectStartedAt > handshakeTimeoutMs ? 'handshake-timeout' : null
    }
    if (ws.readyState === WebSocket.OPEN) {
        return now - lastMessageAt > MESSAGE_STALE_MS ? 'silent' : null
    }
    // CLOSING / CLOSED: the close handler owns the retry.
    return 'absent'
}

/** Drop the current socket and schedule one retry, whatever killed it. */
function recycle(failure: Exclude<SocketFailure, null>) {
    appLog.w('GlobalEvents', `connection ${failure} — reconnecting`)
    // disconnect() detaches onclose, so the abandoned socket cannot schedule a
    // competing reconnect; the explicit schedule below owns the retry.
    disconnect()
    if (reconnect.shouldReconnect()) {
        reconnect.scheduleReconnect()
    }
}

function disconnect() {
    stopHeartbeat()
    resumeDialPending = false
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

function startHeartbeat(intervalMs: number = HEARTBEAT_CHECK_INTERVAL_MS) {
    stopHeartbeat()
    heartbeatTimer = setInterval(() => {
        const failure = socketFailure(Date.now())
        if (failure) recycle(failure)
    }, intervalMs)
}

function stopHeartbeat() {
    if (heartbeatTimer) {
        clearInterval(heartbeatTimer)
        heartbeatTimer = null
    }
}

/** How each meeting event is presented in-app: glyph, tone, and whether it is
 *  worth a sound. Joins and screen shares are ambient (a tone for every one of
 *  them would make a five-person meeting unlivable); a control request and a
 *  finished/failed summary are things the host must act on or look at. */
const MEETING_EVENT_PRESENTATION: Record<string, { icon: ToastIconKey; type: ToastType; sound: boolean }> = {
    control_request: { icon: 'hand', type: 'info', sound: true },
    viewer_joined: { icon: 'chat', type: 'info', sound: false },
    screen_share: { icon: 'eye', type: 'info', sound: false },
    minutes_ready: { icon: 'file', type: 'success', sound: true },
    minutes_failed: { icon: 'error', type: 'error', sound: true },
    transcript_ready: { icon: 'file', type: 'success', sound: true },
    transcript_failed: { icon: 'error', type: 'error', sound: true },
    revised_ready: { icon: 'file', type: 'success', sound: true },
    revised_failed: { icon: 'error', type: 'error', sound: true },
}

/**
 * Announce one meeting event to the host.
 *
 * Three gates, in order:
 *
 *   1. only the HOST is told. The server broadcasts to every client of the
 *      project — viewers' devices included — and a viewer has nothing to
 *      answer (their request is the one being announced);
 *   2. never while the meeting page is on screen: the participants dock already
 *      shows the request, so anything more is noise;
 *   3. a hidden document counts as "not looking at the meeting" (that is the
 *      page-visibility rule), which is exactly when the OS notification is
 *      wanted.
 *
 * Two surfaces, because neither alone covers the cases:
 *
 *   - the in-app toast is the only one that survives a FOCUSED window, which is
 *     precisely the situation this exists for (host using the app on another
 *     tab);
 *   - the OS notification is the only one that survives a backgrounded app.
 *     Its existing gates (page focus / window on screen / app foreground)
 *     suppress it whenever the app is already in front, so the two never
 *     double up in practice.
 */
function handleMeetingEvent(data: ServerEvent['data']): void {
    const token = data?.token || ''
    const body = data?.body || ''
    if (!token || !body) return
    if (!shouldNotifyMeeting(token)) return

    const eventType = data?.event_type || ''
    const presentation = MEETING_EVENT_PRESENTATION[eventType] ?? {
        icon: 'info' as ToastIconKey,
        type: 'info' as ToastType,
        sound: false,
    }
    const title = data?.title || gt('nav.meeting')
    const open = () => {
        window.dispatchEvent(new CustomEvent('clawbench-open-meeting', {
            detail: { token, projectPath: data?.project_path },
        }))
    }

    useToast().show(`${title} · ${body}`, {
        icon: presentation.icon,
        type: presentation.type,
        duration: 6000,
        onClick: open,
    })
    if (presentation.sound) playNotificationSound()

    showBrowserNotification(title, {
        body,
        tag: `clawbench-live-${token}`,
        onClick: open,
        nav: { meetingToken: token, projectPath: data?.project_path },
    })
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
    // The assistant message the notification is about, when the server could
    // identify it. Lets the click land on that message instead of the bottom of
    // the session — the preview walk skips text-less messages, so the newest
    // assistant row is not always the one the body describes.
    let messageId: number | undefined
    // The forge item this event is about, when it is a forge event with a
    // usable identity. Hoisted out of the branch so the notification payload
    // below (built after the branch) can carry it to the native shell.
    let forgeTarget: ForgeTarget | undefined

    if (event === 'session_update') {
        const status = data.status
        if (status !== 'completed' && status !== 'cancelled' && status !== 'permission_pending') return

        const toolName = data.tool_name || ''
        // Same localized label as the chat card strip — the notification must
        // not show a different (raw wire) name for the same tool.
        const toolLabel = localizedToolName(toolName)

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
            alert_ = toolLabel || gt('chat.push.actionRequired')
        }

        // Click: navigate to the session
        const sessionId = data.session_id
        const projectPath = data.project_path
        messageId = data.message_id
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
        messageId = data.message_id
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

        // Click: open THIS item's detail, switching projects first when the
        // change belongs to a repository bound by another project (the panel is
        // project-scoped, so the row only exists there).
        //
        // The target carries the opaque read key rather than (type, number): a
        // pipeline's number is always 0, so its identity is the run id
        // ("pipeline/run:<id>") — which the backend now sends as `run_id`
        // precisely so a pipeline click can land on the run that changed.
        const projectPath = ev.project_path
        const itemType = (ev.item_type || item?.type) as ForgeTarget['type'] | undefined
        const runId = ev.run_id || 0
        forgeTarget = itemType === 'issue' || itemType === 'pr' || itemType === 'pipeline'
            ? {
                projectPath,
                type: itemType,
                number: num || 0,
                runId,
                itemKey: forgeTargetItemKey(itemType, num || 0, runId),
            }
            : undefined
        onClick = () => {
            window.dispatchEvent(new CustomEvent('clawbench-open-forge', {
                detail: { projectPath, target: forgeTarget },
            }))
        }
    } else if (event === 'usage_meter_update') {
        // The wording is rendered server-side (it carries numbers such as
        // "used $9.20 vs $6.67 expected") and arrives in the payload; there is
        // no status enum to translate here. A missing title means a malformed
        // event, which must not produce an empty notification.
        title = String(data.title || '')
        alert_ = String(data.body || '')
        if (!title) return
        onClick = () => {
            window.dispatchEvent(new CustomEvent('clawbench-open-usage-meter'))
        }
    } else if (event === 'mail_event') {
        // New inbox mail. Same server-rendered wording as usage_meter: the
        // sync pass owns the subject/sender and localizes once, so there is
        // nothing to translate here.
        title = String(data.title || '')
        alert_ = String(data.body || '')
        const mailMessageId = data.mail_message_id
        if (!title || !mailMessageId) return
        onClick = () => {
            window.dispatchEvent(new CustomEvent('clawbench-open-mail', {
                detail: { messageId: mailMessageId },
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
                // The assistant message this notification is about, so the
                // click can scroll to it rather than just opening the session.
                messageId,
                // Forge notifications carry no session/task id, so the native
                // shell needs an explicit discriminator — otherwise its
                // sessionId/taskId branches both miss and the click is dropped.
                forge: event === 'forge_event',
                // Same reason as `forge`: a usage event has no session/task id,
                // so the native shell needs its own discriminator or the click
                // is silently dropped.
                usageMeter: event === 'usage_meter_update',
                // Mail notifications carry only a cached mail_messages id.
                mailMessageId: event === 'mail_event' ? data.mail_message_id : undefined,
                // The item to open, so the desktop shell's click deep-links
                // instead of only raising the tab. Spread (not referenced) so
                // the object crosses the IPC boundary as plain data.
                ...(forgeTarget ? { forgeTarget } : {}),
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
    // Design principle: the foreground branch is self-contained — it resets
    // reconnect state and reconnects without depending on any timer that may
    // have been scheduled during the background ('hidden') branch. This
    // eliminates the old race where setTimeout(reset, 100) was frozen by
    // Android's pauseTimers() and fired unpredictably (or never) on resume.
    //
    // "Returning to foreground" must be driven by a RELIABLE signal. The
    // background branch disables reconnect, and `disabled` is STICKY — only
    // reset() clears it. Relying on `visibilitychange === 'visible'` alone
    // strands the client: Android's WebView does not reliably deliver
    // visibilitychange (the repo documents this and added an unconditional
    // native resume hook for exactly this reason — see useAppForeground), so
    // when the 'visible' event never arrives the socket stays dead forever
    // (onclose and the heartbeat both gate on shouldReconnect(), which is false
    // while disabled). The user then sees the "连接断开，正在重连…" overlay until
    // they force-quit and relaunch the app, which re-runs init() -> connect().
    // Both sources therefore funnel into resyncForeground().
    //
    // ── Why the mobile path rebuilds unconditionally ────────────────────────
    // On Android/HarmonyOS the OS freezes the page while paused. The socket we
    // come back to is usually already dead at the far end while still reading
    // OPEN, and the handshake we start right after the resume is the one most
    // likely to hang (the radio is still re-attaching). Asking "does this look
    // usable?" therefore answered "yes" often enough to matter, and every
    // "yes" cost a full silence window (60s) before anything happened — which
    // is exactly the reported "back from background, stuck on 正在重连 for a
    // long time". There is nothing worth preserving across that freeze (the
    // events raised while hidden are lost either way and the resync pulls them
    // back), so the app path tears the socket down and dials a fresh one
    // immediately, on a tighter retry ladder.
    //
    // The browser/Electron path keeps the health check: those hosts never
    // freeze the page, the socket genuinely survives, and tearing it down on
    // every tab switch would drop live stream events and re-run the whole
    // state sync for nothing.
    //
    // The rebuild is skipped while a dial made by this same resume is still in
    // flight: ONE physical resume can reach this function twice (the document
    // visibilitychange handler below is a separate listener chain from
    // onAppResume, so useAppForeground's dedupe does not collapse this pair),
    // and the second call would close the socket the first one just dialled.
    //
    // The guard is "that dial is still pending", NOT a bare time window: the
    // socket is torn down whenever the app goes to the background, so a quick
    // background→foreground round trip inside any such window must still
    // rebuild. A time-only guard would skip it and leave the client with no
    // socket at all (the heartbeat that would have noticed was stopped by the
    // teardown). It is also not "is the socket young" — a handshake that was
    // already stuck in CONNECTING before the app was frozen is exactly what has
    // to be killed, not preserved.
    function resyncForeground() {
        const now = Date.now()
        const mobileApp = isAppMode.value && !isDesktopApp.value
        // Always clear the sticky disabled flag (set by the background branch)
        // and any stale attempt count left by backgrounded reconnect attempts
        // the OS killed. Must run on every foreground entry.
        reconnect.reset(mobileApp ? { baseDelay: RESUME_RECONNECT_BASE_DELAY_MS } : undefined)
        if (mobileApp) {
            // A dial made by this same resume is still in flight: leave it
            // alone (see resumeDialPending). Anything else — no socket (the
            // background branch tore it down), or a socket that predates the
            // resume and may well be half-open or stuck in CONNECTING — is
            // rebuilt.
            if (resumeDialPending) return
            appLog.i('GlobalEvents', 'resume: rebuilding the event socket')
            connect({ resumed: true })
        } else {
            // One question, the same one the liveness tick asks: is this socket
            // usable? A healthy connection is left untouched — tearing it down
            // would drop live stream events and re-run the whole state sync for
            // nothing. Anything else is replaced immediately rather than waiting
            // up to a full tick, because on resume the user is looking at the
            // screen.
            const failure = socketFailure(now)
            if (failure) {
                appLog.w('GlobalEvents', `resume: ${failure} — reconnecting`)
                connect()
            }
        }
        // Emit a custom event that other composables can listen to
        window.dispatchEvent(new CustomEvent('clawbench-foreground'))
    }

    function handleVisibilityChange() {
        if (document.visibilityState === 'visible') {
            resyncForeground()
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
    // Unsubscribe handle for the native resume hook; cleared in destroy().
    let removeResumeListener: (() => void) | null = null
    function init() {
        if (initialized) return
        initialized = true
        // Ensure the native foreground bridges are installed. Idempotent — the
        // composable installs them once per module instance.
        useAppForeground()
        document.addEventListener('visibilitychange', handleVisibilityChange)
        // Reliable resume signal (Android native onResume). On Android the
        // WebView may never deliver a 'visible' visibilitychange, so without
        // this the client could stay permanently disconnected after a resume.
        removeResumeListener = onAppResume(resyncForeground)
        // Initial connect
        connect()
    }

    function destroy() {
        document.removeEventListener('visibilitychange', handleVisibilityChange)
        removeResumeListener?.()
        removeResumeListener = null
        disconnect()
        // ISS-192: Clear handlers and state on destroy to prevent stale closures
        // from firing after SPA hot project switch.
        handlers.length = 0
        processedEventIds.clear()
        lastSeenEventId = ''
        lastMessageAt = 0
        connectStartedAt = 0
        // The resume pacing is per-connection state; a re-init must not inherit
        // it from whatever the previous instance was doing.
        resumePacing = false
        resumeDialPending = false
        handshakeTimeoutMs = HANDSHAKE_TIMEOUT_MS
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

