import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest'
import { useCompletionPopover } from '@/composables/useCompletionPopover'

// Module-level singleton state persists between tests within the file.
// Reset it before each test so each case starts from a clean queue.
beforeEach(() => {
    vi.useFakeTimers()
    useCompletionPopover().reset()
})
afterEach(() => {
    vi.useRealTimers()
})

function makeItem(overrides = {}) {
    return {
        sessionId: 's1',
        title: '会话标题',
        summary: '**加粗摘要**',
        kind: 'session',
        projectPath: '',
        ...overrides,
    }
}

describe('useCompletionPopover', () => {
    it('push() shows the item immediately when nothing is showing', () => {
        const p = useCompletionPopover()
        p.push(makeItem())

        expect(p.active.value).not.toBeNull()
        expect(p.active.value?.sessionId).toBe('s1')
        expect(p.active.value?.title).toBe('会话标题')
        expect(p.queue.value).toHaveLength(0)
    })

    it('push() queues subsequent items while one is showing', () => {
        const p = useCompletionPopover()
        p.push(makeItem({ sessionId: 's1' }))
        p.push(makeItem({ sessionId: 's2' }))
        p.push(makeItem({ sessionId: 's3' }))

        expect(p.active.value?.sessionId).toBe('s1')
        expect(p.queue.value.map((i) => i.sessionId)).toEqual(['s2', 's3'])
    })

    it('does NOT auto-hide or auto-advance over time — stays until dismissed', () => {
        const p = useCompletionPopover()
        p.push(makeItem({ sessionId: 's1' }))
        p.push(makeItem({ sessionId: 's2' }))

        // Far past any reasonable auto-dismiss duration
        vi.advanceTimersByTime(600000)
        expect(p.active.value?.sessionId).toBe('s1')
        expect(p.queue.value.map((i) => i.sessionId)).toEqual(['s2'])
    })

    it('dismiss() hides the active item and advances to the next one', () => {
        const p = useCompletionPopover()
        p.push(makeItem({ sessionId: 's1' }))
        p.push(makeItem({ sessionId: 's2' }))

        p.dismiss()

        expect(p.active.value?.sessionId).toBe('s2')
        expect(p.queue.value).toHaveLength(0)
    })

    it('dismiss() with an empty queue leaves active null', () => {
        const p = useCompletionPopover()
        p.push(makeItem())
        p.dismiss()
        p.dismiss()

        expect(p.active.value).toBeNull()
    })

    it('push() after everything was dismissed shows immediately again', () => {
        const p = useCompletionPopover()
        p.push(makeItem({ sessionId: 's1' }))
        p.dismiss()
        p.push(makeItem({ sessionId: 's2' }))

        expect(p.active.value?.sessionId).toBe('s2')
        expect(p.queue.value).toHaveLength(0)
    })

    it('push() while active replaces nothing and keeps queue order FIFO', () => {
        const p = useCompletionPopover()
        p.push(makeItem({ sessionId: 's1' }))
        p.push(makeItem({ sessionId: 's2' }))
        p.push(makeItem({ sessionId: 's3' }))

        expect(p.active.value?.sessionId).toBe('s1')
        expect(p.queue.value.map((i) => i.sessionId)).toEqual(['s2', 's3'])

        p.dismiss()
        expect(p.active.value?.sessionId).toBe('s2')
        expect(p.queue.value.map((i) => i.sessionId)).toEqual(['s3'])

        p.dismiss()
        expect(p.active.value?.sessionId).toBe('s3')
        expect(p.queue.value).toHaveLength(0)
    })
})
