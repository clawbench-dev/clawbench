import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest'
import { coalescedJson, resetCoalescedRequestsForTest } from '@/utils/inflightGet.ts'

const mockFetch = vi.fn()

describe('coalescedJson', () => {
    beforeEach(() => {
        resetCoalescedRequestsForTest()
        mockFetch.mockReset()
        vi.stubGlobal('fetch', mockFetch)
    })

    afterEach(() => {
        vi.unstubAllGlobals()
    })

    it('shares one request between concurrent callers of the same URL', async () => {
        let resolveFetch: (v: unknown) => void = () => {}
        mockFetch.mockReturnValue(new Promise((res) => { resolveFetch = res }))

        const a = coalescedJson('/api/x')
        const b = coalescedJson('/api/x')
        const c = coalescedJson('/api/x')
        resolveFetch({ ok: true, json: () => Promise.resolve({ n: 1 }) })

        await expect(Promise.all([a, b, c])).resolves.toEqual([{ n: 1 }, { n: 1 }, { n: 1 }])
        expect(mockFetch).toHaveBeenCalledTimes(1)
    })

    it('does not share between different URLs', async () => {
        mockFetch.mockResolvedValue({ ok: true, json: () => Promise.resolve({}) })

        await Promise.all([coalescedJson('/api/x'), coalescedJson('/api/y')])

        expect(mockFetch).toHaveBeenCalledTimes(2)
    })

    it('issues a fresh request once the previous one settled', async () => {
        mockFetch.mockResolvedValue({ ok: true, json: () => Promise.resolve({ n: 1 }) })

        await coalescedJson('/api/x')
        await coalescedJson('/api/x')

        // Deliberately NOT a response cache: a later caller must get fresh data.
        expect(mockFetch).toHaveBeenCalledTimes(2)
    })

    it('rejects on a non-2xx response', async () => {
        mockFetch.mockResolvedValue({ ok: false, status: 500, json: () => Promise.resolve({}) })

        await expect(coalescedJson('/api/x')).rejects.toThrow('HTTP 500')
    })

    it('lets a later caller retry after a failure', async () => {
        mockFetch.mockResolvedValueOnce({ ok: false, status: 500, json: () => Promise.resolve({}) })
        mockFetch.mockResolvedValueOnce({ ok: true, json: () => Promise.resolve({ n: 2 }) })

        await expect(coalescedJson('/api/x')).rejects.toThrow()
        // The failed entry must have been cleared, or this would replay the error.
        await expect(coalescedJson('/api/x')).resolves.toEqual({ n: 2 })
        expect(mockFetch).toHaveBeenCalledTimes(2)
    })

    it('clears the entry even when the request rejects', async () => {
        mockFetch.mockRejectedValueOnce(new Error('network down'))
        await expect(coalescedJson('/api/x')).rejects.toThrow('network down')

        mockFetch.mockResolvedValueOnce({ ok: true, json: () => Promise.resolve({ ok: 1 }) })
        await expect(coalescedJson('/api/x')).resolves.toEqual({ ok: 1 })
    })

    it('coalesces the request that a second caller joins mid-flight', async () => {
        let resolveFetch: (v: unknown) => void = () => {}
        mockFetch.mockReturnValueOnce(new Promise((res) => { resolveFetch = res }))

        const first = coalescedJson('/api/x')
        // Second caller arrives while the first is unresolved.
        const second = coalescedJson('/api/x')
        expect(mockFetch).toHaveBeenCalledTimes(1)

        resolveFetch({ ok: true, json: () => Promise.resolve({ n: 9 }) })
        await expect(second).resolves.toEqual({ n: 9 })
        await first
    })
})
