// Notification-center API client.
//
// The notification center is a browsable, append-only log of session/task/forge
// events. It has NO read/unread state and NO badge — it answers "what happened",
// not "what is pending". See docs/plans/2026-10-15-notification-center-design.md.
import i18n from '@/i18n'
import { appLog } from '@/utils/appLog'

const TAG = 'NotificationsApi'

export const NOTIFICATIONS_TIMEOUT_MS = 20_000

export type NotificationKind = 'session' | 'task' | 'forge'

/** One row of the notification log. */
export interface NotificationItem {
    id: number
    kind: NotificationKind
    /** completed / cancelled / permission_pending / failed / running / merged / … */
    eventType: string
    title: string
    body: string
    projectId: number
    projectPath: string
    sessionId?: string
    taskId?: string
    executionId?: string
    forgeItemKey?: string
    agentId?: string
    createdAt: string
    /**
     * True when the session/task this row points at no longer exists. Clicking
     * such a row must NOT navigate — the source's own fallback (a session
     * silently redirects to the newest one) would drop the user somewhere
     * unrelated. Forge rows are never marked: their items live upstream.
     */
    sourceMissing?: boolean
}

/** The time-range filter tokens the server understands. */
export type NotificationRange = 'today' | '7d' | '30d'

export interface NotificationQuery {
    /** Project PATH. Omitted means all projects. */
    project?: string
    /** Event kind. Omitted means all kinds. */
    kind?: NotificationKind
    /** Time range. Omitted means no lower bound. */
    range?: NotificationRange
    /** Keyset cursor from the previous page's last row. */
    beforeAt?: string
    beforeId?: number
    limit?: number
}

export interface NotificationPage {
    items: NotificationItem[]
    total: number
    hasMore: boolean
}

class NotificationsApiError extends Error {
    code: string
    status: number
    constructor(message: string, code: string, status: number) {
        super(message)
        this.name = 'NotificationsApiError'
        this.code = code
        this.status = status
    }
}

async function notificationsFetch<T>(path: string, opts: { method?: string; signal?: AbortSignal } = {}): Promise<T> {
    const controller = new AbortController()
    // Track the timeout by flag rather than by inspecting the thrown error.
    // Sniffing `err.name === 'AbortError'` is not portable: jsdom's DOMException
    // is NOT an `Error` subclass (real browsers' is), so the check silently
    // misses and a timeout is misreported as a network failure. A flag also
    // avoids the race where the caller's abort and the internal timeout fire in
    // the same tick — same reasoning as gitApi.ts's `timedOut`.
    let timedOut = false
    const timer = setTimeout(() => {
        timedOut = true
        controller.abort()
    }, NOTIFICATIONS_TIMEOUT_MS)
    if (opts.signal?.aborted) controller.abort()
    opts.signal?.addEventListener('abort', () => controller.abort(), { once: true })

    try {
        const res = await fetch(path, {
            method: opts.method ?? 'GET',
            headers: { 'X-Locale': i18n.global.locale.value as string },
            signal: controller.signal,
        })
        const text = await res.text()
        let payload: unknown = null
        if (text) {
            try {
                payload = JSON.parse(text)
            } catch {
                payload = null
            }
        }
        if (!res.ok) {
            const p = (payload ?? {}) as Record<string, unknown>
            const code = typeof p.code === 'string' ? p.code : 'NotificationsError'
            const message = typeof p.error === 'string' ? p.error : `notifications request failed (${res.status})`
            throw new NotificationsApiError(message, code, res.status)
        }
        return payload as T
    } catch (err) {
        if (err instanceof NotificationsApiError) throw err
        if (timedOut) {
            throw new NotificationsApiError('notifications request timed out', 'NotificationsTimeout', 0)
        }
        appLog.w(TAG, 'request failed', path, err)
        throw new NotificationsApiError(String(err), 'NotificationsNetworkError', 0)
    } finally {
        clearTimeout(timer)
    }
}

/** List a page of the notification log, newest first. */
export function fetchNotifications(q: NotificationQuery = {}, signal?: AbortSignal): Promise<NotificationPage> {
    const params = new URLSearchParams()
    if (q.project) params.set('project', q.project)
    if (q.kind) params.set('kind', q.kind)
    if (q.range) params.set('range', q.range)
    if (q.beforeAt) params.set('before_at', q.beforeAt)
    if (q.beforeId != null) params.set('before_id', String(q.beforeId))
    if (q.limit != null) params.set('limit', String(q.limit))
    const qs = params.toString()
    return notificationsFetch<NotificationPage>(`/api/notifications${qs ? `?${qs}` : ''}`, { signal })
}

/** Delete the whole notification log. Returns the number of rows removed. */
export function clearNotifications(): Promise<{ deleted: number }> {
    return notificationsFetch<{ deleted: number }>('/api/notifications', { method: 'DELETE' })
}
