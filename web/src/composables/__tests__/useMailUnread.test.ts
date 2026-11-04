import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

const apiGet = vi.fn()

vi.mock('@/utils/api', () => ({
    apiGet: (...args: unknown[]) => apiGet(...args),
}))

/**
 * The badge state is module-level (one poller per app, not per component), so
 * every case re-imports the module to start from a clean slate.
 */
let current: typeof import('../useMailUnread') | null = null

async function loadModule() {
    vi.resetModules()
    current = await import('../useMailUnread')
    return current
}

describe('useMailUnread', () => {
    beforeEach(() => {
        apiGet.mockReset()
        vi.useFakeTimers()
    })

    afterEach(() => {
        current?.stopMailUnreadPolling()
        current = null
        vi.useRealTimers()
    })

    it('stores the unread total and per-account breakdown', async () => {
        apiGet.mockResolvedValueOnce({
            total: 7,
            accounts: [{ accountId: 1, name: '工作', email: 'w@x.com', unread: 7 }],
        })
        const mod = await loadModule()
        await mod.refreshMailUnread()

        expect(apiGet).toHaveBeenCalledWith('/api/mail/unread')
        expect(mod.useMailUnread().total.value).toBe(7)
        expect(mod.useMailUnread().accounts.value).toHaveLength(1)
    })

    it('normalises a malformed payload instead of rendering undefined', async () => {
        apiGet.mockResolvedValueOnce({ total: '3', accounts: 'nope' })
        const mod = await loadModule()
        await mod.refreshMailUnread()

        expect(mod.useMailUnread().total.value).toBe(0)
        expect(mod.useMailUnread().accounts.value).toEqual([])
    })

    it('keeps the last known count when a refresh fails', async () => {
        apiGet.mockResolvedValueOnce({ total: 4, accounts: [] })
        const mod = await loadModule()
        await mod.refreshMailUnread()

        apiGet.mockRejectedValueOnce(new Error('offline'))
        await mod.refreshMailUnread()

        // A badge is not worth an error banner; the stale number is better
        // than flashing 0 every time the network hiccups.
        expect(mod.useMailUnread().total.value).toBe(4)
    })

    it('collapses concurrent refreshes into one request', async () => {
        let resolveFirst: (v: unknown) => void = () => {}
        apiGet.mockReturnValueOnce(new Promise((r) => { resolveFirst = r }))
        const mod = await loadModule()

        const first = mod.refreshMailUnread()
        const second = mod.refreshMailUnread()
        resolveFirst({ total: 1, accounts: [] })
        await Promise.all([first, second])

        expect(apiGet).toHaveBeenCalledTimes(1)
    })

    it('starts the poller once no matter how many mount sites call it', async () => {
        apiGet.mockResolvedValue({ total: 2, accounts: [] })
        const mod = await loadModule()

        mod.startMailUnreadPolling()
        mod.startMailUnreadPolling()
        await vi.advanceTimersByTimeAsync(60_000)

        // One immediate refresh plus exactly one tick — a second interval
        // would double the request rate for every extra mount.
        expect(apiGet).toHaveBeenCalledTimes(2)
    })

    it('stops polling when the last consumer goes away', async () => {
        apiGet.mockResolvedValue({ total: 2, accounts: [] })
        const mod = await loadModule()

        mod.startMailUnreadPolling()
        await vi.advanceTimersByTimeAsync(0)
        mod.stopMailUnreadPolling()
        await vi.advanceTimersByTimeAsync(180_000)

        expect(apiGet).toHaveBeenCalledTimes(1)
    })
})
