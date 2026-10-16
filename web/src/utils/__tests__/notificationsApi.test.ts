import { describe, expect, it, vi, afterEach } from 'vitest'
import {
    fetchNotifications,
    clearNotifications,
    NOTIFICATIONS_TIMEOUT_MS,
    type NotificationItem,
} from '@/utils/notificationsApi'

// notificationsApi reads i18n.global.locale.value for the X-Locale header.
vi.mock('@/i18n', () => ({
    default: { global: { locale: { value: 'en' } } },
}))

/** Build a Response-like stub whose body is the given text. */
function textResponse(body: string, init: { ok?: boolean; status?: number } = {}): Response {
    return {
        ok: init.ok ?? true,
        status: init.status ?? 200,
        text: () => Promise.resolve(body),
    } as unknown as Response
}

function item(over: Partial<NotificationItem> = {}): NotificationItem {
    return {
        id: 1,
        kind: 'session',
        eventType: 'completed',
        title: 't',
        body: 'b',
        projectId: 1,
        projectPath: '/p',
        createdAt: '2026-10-16T00:00:00Z',
        ...over,
    }
}

describe('fetchNotifications', () => {
    const originalFetch = globalThis.fetch

    afterEach(() => {
        vi.useRealTimers()
        globalThis.fetch = originalFetch
        vi.restoreAllMocks()
    })

    it('omits every query param when the query is empty', async () => {
        const fetchMock = vi.fn().mockResolvedValue(textResponse('{"items":[],"total":0,"hasMore":false}'))
        globalThis.fetch = fetchMock as unknown as typeof fetch

        await fetchNotifications()

        const [url] = fetchMock.mock.calls[0]
        expect(url).toBe('/api/notifications')
    })

    it('serializes the project/kind/range filters and the keyset cursor', async () => {
        const fetchMock = vi.fn().mockResolvedValue(textResponse('{"items":[],"total":0,"hasMore":false}'))
        globalThis.fetch = fetchMock as unknown as typeof fetch

        await fetchNotifications({
            project: '/proj a',
            kind: 'task',
            range: '7d',
            beforeAt: '2026-10-15T10:00:00Z',
            beforeId: 42,
            limit: 20,
        })

        const [url] = fetchMock.mock.calls[0]
        const qs = new URLSearchParams((url as string).split('?')[1])
        expect(qs.get('project')).toBe('/proj a')
        expect(qs.get('kind')).toBe('task')
        expect(qs.get('range')).toBe('7d')
        expect(qs.get('before_at')).toBe('2026-10-15T10:00:00Z')
        expect(qs.get('before_id')).toBe('42')
        expect(qs.get('limit')).toBe('20')
    })

    it('keeps beforeId=0 (a valid cursor) instead of dropping it', async () => {
        // The guard is `!= null`, not truthiness: id 0 would be silently dropped
        // by a falsy check and the page would repeat the first page forever.
        const fetchMock = vi.fn().mockResolvedValue(textResponse('{"items":[],"total":0,"hasMore":false}'))
        globalThis.fetch = fetchMock as unknown as typeof fetch

        await fetchNotifications({ beforeId: 0 })

        const [url] = fetchMock.mock.calls[0]
        expect(url).toContain('before_id=0')
    })

    it('returns the parsed page', async () => {
        const page = { items: [item({ id: 7 })], total: 1, hasMore: false }
        const fetchMock = vi.fn().mockResolvedValue(textResponse(JSON.stringify(page)))
        globalThis.fetch = fetchMock as unknown as typeof fetch

        await expect(fetchNotifications()).resolves.toEqual(page)
    })

    it('sends the X-Locale header', async () => {
        const fetchMock = vi.fn().mockResolvedValue(textResponse('{"items":[],"total":0,"hasMore":false}'))
        globalThis.fetch = fetchMock as unknown as typeof fetch

        await fetchNotifications()

        const [, init] = fetchMock.mock.calls[0]
        expect(init.headers).toMatchObject({ 'X-Locale': 'en' })
    })

    it('maps a structured error body to a NotificationsApiError with its code', async () => {
        const fetchMock = vi
            .fn()
            .mockResolvedValue(textResponse('{"error":"nope","code":"Forbidden"}', { ok: false, status: 403 }))
        globalThis.fetch = fetchMock as unknown as typeof fetch

        await expect(fetchNotifications()).rejects.toMatchObject({
            name: 'NotificationsApiError',
            code: 'Forbidden',
            status: 403,
            message: 'nope',
        })
    })

    it('falls back to a generic code and message when the error body is not JSON', async () => {
        const fetchMock = vi.fn().mockResolvedValue(textResponse('boom', { ok: false, status: 500 }))
        globalThis.fetch = fetchMock as unknown as typeof fetch

        await expect(fetchNotifications()).rejects.toMatchObject({
            code: 'NotificationsError',
            status: 500,
            message: 'notifications request failed (500)',
        })
    })

    it('rejects with a timeout error when the request outlives NOTIFICATIONS_TIMEOUT_MS', async () => {
        vi.useFakeTimers()
        const fetchMock = vi.fn((_url: string, init?: RequestInit) => {
            return new Promise((_resolve, reject) => {
                init?.signal?.addEventListener('abort', () => {
                    // jsdom's DOMException is NOT an Error subclass (browsers' is),
                    // which is exactly why the timeout must be classified by the
                    // timedOut flag and not by sniffing err.name.
                    reject(new DOMException('The operation was aborted.', 'AbortError'))
                })
            })
        })
        globalThis.fetch = fetchMock as unknown as typeof fetch

        const promise = fetchNotifications()
        const assertion = expect(promise).rejects.toMatchObject({
            code: 'NotificationsTimeout',
            status: 0,
        })
        await vi.advanceTimersByTimeAsync(NOTIFICATIONS_TIMEOUT_MS)
        await assertion
    })

    it('rejects with a network error code when fetch throws', async () => {
        const fetchMock = vi.fn().mockRejectedValue(new TypeError('Failed to fetch'))
        globalThis.fetch = fetchMock as unknown as typeof fetch

        await expect(fetchNotifications()).rejects.toMatchObject({
            code: 'NotificationsNetworkError',
            status: 0,
        })
    })

    it('does not misreport a caller-side abort as a timeout', async () => {
        // A caller cancelling (superseded request) is not a timeout. The two
        // abort sources must stay distinguishable, otherwise a deliberate
        // cancel would surface as "timed out" in the UI.
        const controller = new AbortController()
        controller.abort()
        let seenSignal: AbortSignal | undefined
        const fetchMock = vi.fn((_url: string, init?: RequestInit) => {
            seenSignal = init?.signal ?? undefined
            return Promise.reject(new DOMException('The operation was aborted.', 'AbortError'))
        })
        globalThis.fetch = fetchMock as unknown as typeof fetch

        await expect(fetchNotifications({}, controller.signal)).rejects.toMatchObject({
            code: 'NotificationsNetworkError',
        })
        expect(seenSignal?.aborted).toBe(true)
    })
})

describe('clearNotifications', () => {
    const originalFetch = globalThis.fetch

    afterEach(() => {
        globalThis.fetch = originalFetch
        vi.restoreAllMocks()
    })

    it('issues a DELETE to the collection endpoint and returns the count', async () => {
        const fetchMock = vi.fn().mockResolvedValue(textResponse('{"deleted":5}'))
        globalThis.fetch = fetchMock as unknown as typeof fetch

        await expect(clearNotifications()).resolves.toEqual({ deleted: 5 })

        const [url, init] = fetchMock.mock.calls[0]
        expect(url).toBe('/api/notifications')
        expect(init.method).toBe('DELETE')
    })
})
