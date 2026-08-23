import { describe, expect, it } from 'vitest'
import { createSeqGuard } from '@/utils/gitApi'

describe('createSeqGuard — git history race guard', () => {
    it('token() returns a fresh token on every call', () => {
        const guard = createSeqGuard()
        const t1 = guard.token()
        const t2 = guard.token()
        const t3 = guard.token()
        expect(t1).not.toBe(t2)
        expect(t2).not.toBe(t3)
        expect(t1).not.toBe(t3)
    })

    it('isCurrent is true only for the most recently issued token', () => {
        const guard = createSeqGuard()
        const t1 = guard.token()
        expect(guard.isCurrent(t1)).toBe(true)
        const t2 = guard.token()
        expect(guard.isCurrent(t1)).toBe(false) // t1 superseded by t2
        expect(guard.isCurrent(t2)).toBe(true)
    })

    it('a stale token stays stale after further loads (loading flag guard)', () => {
        const guard = createSeqGuard()
        // Simulate: first load starts, then refresh starts, then third load starts.
        const firstLoad = guard.token()
        const secondLoad = guard.token()
        const thirdLoad = guard.token()

        // The first load completes late — it must not be considered current.
        expect(guard.isCurrent(firstLoad)).toBe(false)
        // Neither may the second.
        expect(guard.isCurrent(secondLoad)).toBe(false)
        // Only the latest load may reset the loading flag.
        expect(guard.isCurrent(thirdLoad)).toBe(true)
    })

    it('two independent guards do not interfere', () => {
        const a = createSeqGuard()
        const b = createSeqGuard()
        const a1 = a.token()
        const b1 = b.token()
        expect(a.isCurrent(a1)).toBe(true)
        expect(b.isCurrent(b1)).toBe(true)
        // A token issued by one guard is never current in the other.
        expect(a.isCurrent(b1)).toBe(false)
        expect(b.isCurrent(a1)).toBe(false)
    })

    it('a guard reports no token as current before any token is issued', () => {
        const guard = createSeqGuard()
        expect(guard.isCurrent({ __seqToken: true })).toBe(false)
    })
})
