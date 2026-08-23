import { describe, expect, it, vi, afterEach } from 'vitest'
import { gitFetch, GitTimeoutError, GIT_TIMEOUT_MS } from '@/utils/gitApi'

// gitApi reads i18n.global.locale.value for the X-Locale header.
vi.mock('@/i18n', () => ({
    default: { global: { locale: { value: 'en' } } },
}))

/**
 * A fetch stub that never resolves unless its AbortSignal fires — mimics the
 * real fetch behaviour where abort() makes the in-flight request reject.
 */
function hangingFetch(): { fetchMock: ReturnType<typeof vi.fn>; signalOf: () => AbortSignal | null } {
    let lastSignal: AbortSignal | null = null
    const fetchMock = vi.fn((_url: string, init?: RequestInit) => {
        lastSignal = init?.signal ?? null
        return new Promise((_resolve, reject) => {
            const onAbort = () => {
                reject(new DOMException('The operation was aborted.', 'AbortError'))
            }
            // If the signal is already aborted, reject immediately.
            if (lastSignal?.aborted) {
                onAbort()
                return
            }
            lastSignal?.addEventListener('abort', onAbort, { once: true })
        })
    })
    return {
        fetchMock: fetchMock as unknown as typeof fetch,
        signalOf: () => lastSignal,
    }
}

describe('gitFetch', () => {
    const originalFetch = globalThis.fetch

    afterEach(() => {
        vi.useRealTimers()
        globalThis.fetch = originalFetch
        vi.restoreAllMocks()
    })

    it('resolves with the Response on success and sends the X-Locale header', async () => {
        // Use a plain object shaped like a Response — avoids leaking a real
        // Response's internal promise handles into Vitest's async-leak check.
        const mockResp = { ok: true, status: 200 } as unknown as Response
        const fetchMock = vi.fn().mockResolvedValue(mockResp)
        globalThis.fetch = fetchMock as unknown as typeof fetch

        const resp = await gitFetch('/api/git/project-history')

        expect(resp).toBe(mockResp)
        expect(fetchMock).toHaveBeenCalledTimes(1)
        const [url, init] = fetchMock.mock.calls[0]
        expect(url).toBe('/api/git/project-history')
        expect(init.headers).toMatchObject({ 'X-Locale': 'en' })
        // Request must carry a non-aborted signal
        expect(init.signal.aborted).toBe(false)
    })

    it('rejects with GitTimeoutError when the request hangs past the timeout', async () => {
        const { fetchMock } = hangingFetch()
        globalThis.fetch = fetchMock

        const promise = gitFetch('/api/git/project-history', { timeoutMs: 50 })

        await expect(promise).rejects.toBeInstanceOf(GitTimeoutError)
    })

    it('uses GIT_TIMEOUT_MS by default', async () => {
        // The default timeout is the module constant.
        expect(GIT_TIMEOUT_MS).toBe(10_000)
    })

    it('rejects with AbortError (not GitTimeoutError) when an external signal aborts', async () => {
        const controller = new AbortController()
        const { fetchMock } = hangingFetch()
        globalThis.fetch = fetchMock

        const promise = gitFetch('/api/git/project-history', { signal: controller.signal, timeoutMs: 10_000 })

        controller.abort()

        await expect(promise).rejects.toMatchObject({ name: 'AbortError' })
    })

    it('aborts immediately when the external signal is already aborted', async () => {
        const controller = new AbortController()
        controller.abort()
        const { fetchMock, signalOf } = hangingFetch()
        globalThis.fetch = fetchMock

        const promise = gitFetch('/api/git/project-history', { signal: controller.signal })

        await expect(promise).rejects.toMatchObject({ name: 'AbortError' })
        // fetch must have been called with an already-aborted signal
        expect(signalOf()?.aborted).toBe(true)
    })

    it('forwards external abort to the request signal while in flight', async () => {
        const { fetchMock, signalOf } = hangingFetch()
        globalThis.fetch = fetchMock

        const controller = new AbortController()
        const promise = gitFetch('/api/git/project-history', { signal: controller.signal, timeoutMs: 10_000 })

        // The internal signal must be linked to the external one.
        expect(signalOf()?.aborted).toBe(false)
        controller.abort()
        expect(signalOf()?.aborted).toBe(true)

        await expect(promise).rejects.toMatchObject({ name: 'AbortError' })
    })

    it('does not throw GitTimeoutError when the external signal wins the race', async () => {
        const controller = new AbortController()
        const { fetchMock } = hangingFetch()
        globalThis.fetch = fetchMock

        const promise = gitFetch('/api/git/project-history', { signal: controller.signal, timeoutMs: 5 })

        // External abort fires first (before the 5ms timeout).
        controller.abort()

        await expect(promise).rejects.toMatchObject({ name: 'AbortError' })
    })

    it('surfaces the original network error instead of misreporting it as a timeout', async () => {
        // A network-level failure (e.g. TypeError "Failed to fetch") has no
        // signal involvement — it must propagate unchanged.
        globalThis.fetch = vi.fn(() => Promise.reject(new TypeError('Failed to fetch'))) as unknown as typeof fetch

        const promise = gitFetch('/api/git/project-history', { timeoutMs: 10_000 })

        await expect(promise).rejects.toBeInstanceOf(TypeError)
        await expect(promise).rejects.not.toBeInstanceOf(GitTimeoutError)
    })
})
