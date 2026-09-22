import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest'
import { useCompletionPopover } from '@/composables/useCompletionPopover'

// Module-level singleton state persists between tests within the file.
// Reset it before each test so each case starts from a clean queue.
// Date.now 也纳入 fake timers，保证自动关闭计时可被推进。
beforeEach(() => {
    vi.useFakeTimers()
    vi.setSystemTime(0)
    useCompletionPopover().reset()
})
afterEach(() => {
    vi.useRealTimers()
})

function makeItem(overrides = {}) {
    return {
        groupKey: 'session:s1',
        sessionId: 's1',
        eventLabel: '会话已完成',
        eventTone: 'success',
        title: '会话标题',
        body: '完成摘要',
        kind: 'session',
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
        p.push(makeItem({ groupKey: 'session:s1', sessionId: 's1' }))
        p.push(makeItem({ groupKey: 'session:s2', sessionId: 's2' }))
        p.push(makeItem({ groupKey: 'session:s3', sessionId: 's3' }))

        expect(p.active.value?.sessionId).toBe('s1')
        expect(p.queue.value.map((i) => i.sessionId)).toEqual(['s2', 's3'])
    })

    it('auto-dismisses the active item after 5s', () => {
        const p = useCompletionPopover()
        p.push(makeItem())

        // 4999ms 仍在展示
        vi.advanceTimersByTime(4999)
        expect(p.active.value?.sessionId).toBe('s1')

        // 到 5s 自动关闭
        vi.advanceTimersByTime(1)
        expect(p.active.value).toBeNull()
    })

    it('auto-dismiss advances to the next queued item', () => {
        const p = useCompletionPopover()
        p.push(makeItem({ groupKey: 'session:s1', sessionId: 's1' }))
        p.push(makeItem({ groupKey: 'session:s2', sessionId: 's2' }))

        vi.advanceTimersByTime(5000)
        expect(p.active.value?.sessionId).toBe('s2')
        expect(p.queue.value).toHaveLength(0)
    })

    it('the auto-dismiss timer restarts for each newly shown item', () => {
        const p = useCompletionPopover()
        p.push(makeItem({ groupKey: 'session:s1', sessionId: 's1' }))
        p.push(makeItem({ groupKey: 'session:s2', sessionId: 's2' }))

        // s1 展示 5s 后进入 s2 —— s2 必须重新计满 5s，不能沿用 s1 的剩余时间
        vi.advanceTimersByTime(5000)
        expect(p.active.value?.sessionId).toBe('s2')

        vi.advanceTimersByTime(4999)
        expect(p.active.value?.sessionId).toBe('s2')

        vi.advanceTimersByTime(1)
        expect(p.active.value).toBeNull()
    })

    it('dismiss() hides the active item and advances to the next one', () => {
        const p = useCompletionPopover()
        p.push(makeItem({ groupKey: 'session:s1', sessionId: 's1' }))
        p.push(makeItem({ groupKey: 'session:s2', sessionId: 's2' }))

        p.dismiss()

        expect(p.active.value?.sessionId).toBe('s2')
        expect(p.queue.value).toHaveLength(0)
    })

    it('dismiss() cancels the pending auto-dismiss timer', () => {
        const p = useCompletionPopover()
        p.push(makeItem({ groupKey: 'session:s1', sessionId: 's1' }))
        p.push(makeItem({ groupKey: 'session:s2', sessionId: 's2' }))

        p.dismiss()
        expect(p.active.value?.sessionId).toBe('s2')

        // 推进超过 s1 的计时窗口：若旧计时器未清掉，s2 会被提前关闭
        vi.advanceTimersByTime(4999)
        expect(p.active.value?.sessionId).toBe('s2')
    })

    it('dismiss() with an empty queue leaves active null', () => {
        const p = useCompletionPopover()
        p.push(makeItem())
        p.dismiss()
        p.dismiss()

        expect(p.active.value).toBeNull()
    })

    it('reset() clears the pending auto-dismiss timer', () => {
        const p = useCompletionPopover()
        p.push(makeItem({ groupKey: 'session:s1', sessionId: 's1' }))
        p.reset()
        p.push(makeItem({ groupKey: 'session:s2', sessionId: 's2' }))

        // s2 应在自己的 5s 窗口结束时关闭，而不是被 reset 前的计时器提前关掉
        vi.advanceTimersByTime(4999)
        expect(p.active.value?.sessionId).toBe('s2')
    })

    it('push() after everything was dismissed shows immediately again', () => {
        const p = useCompletionPopover()
        p.push(makeItem({ groupKey: 'session:s1', sessionId: 's1' }))
        p.dismiss()
        p.push(makeItem({ groupKey: 'session:s2', sessionId: 's2' }))

        expect(p.active.value?.sessionId).toBe('s2')
        expect(p.queue.value).toHaveLength(0)
    })

    it('push() while active keeps queue order FIFO', () => {
        const p = useCompletionPopover()
        p.push(makeItem({ groupKey: 'session:s1', sessionId: 's1' }))
        p.push(makeItem({ groupKey: 'session:s2', sessionId: 's2' }))
        p.push(makeItem({ groupKey: 'session:s3', sessionId: 's3' }))

        expect(p.active.value?.sessionId).toBe('s1')
        expect(p.queue.value.map((i) => i.sessionId)).toEqual(['s2', 's3'])

        p.dismiss()
        expect(p.active.value?.sessionId).toBe('s2')
        expect(p.queue.value.map((i) => i.sessionId)).toEqual(['s3'])

        p.dismiss()
        expect(p.active.value?.sessionId).toBe('s3')
        expect(p.queue.value).toHaveLength(0)
    })

    it('merges a same-group forge item into the queued one instead of adding a row', () => {
        const p = useCompletionPopover()
        p.push(makeItem({ groupKey: 'session:s1', sessionId: 's1' }))
        p.push(makeItem({
            groupKey: 'forge:acme/web', kind: 'forge', eventTone: 'info',
            eventLabel: '议题 #1 · 新开', title: 'acme/web',
        }))
        p.push(makeItem({
            groupKey: 'forge:acme/web', kind: 'forge', eventTone: 'info',
            eventLabel: '合并请求 #2 · 有新评论', title: 'acme/web', body: '新评论',
        }))

        // 两次 forge 事件合并成一条排队项
        expect(p.queue.value).toHaveLength(1)
        expect(p.queue.value[0].count).toBe(2)
        // 合并采用最新事件的标识与正文
        expect(p.queue.value[0].eventLabel).toBe('合并请求 #2 · 有新评论')
        expect(p.queue.value[0].body).toBe('新评论')
    })

    it('does not merge different repositories', () => {
        const p = useCompletionPopover()
        p.push(makeItem({ groupKey: 'session:s1', sessionId: 's1' }))
        p.push(makeItem({ groupKey: 'forge:acme/web', kind: 'forge', title: 'acme/web' }))
        p.push(makeItem({ groupKey: 'forge:acme/api', kind: 'forge', title: 'acme/api' }))

        expect(p.queue.value).toHaveLength(2)
        expect(p.queue.value.map(i => i.count)).toEqual([undefined, undefined])
    })

    it('merging does not mutate the active item on screen', () => {
        const p = useCompletionPopover()
        p.push(makeItem({ groupKey: 'forge:acme/web', kind: 'forge', title: 'acme/web', eventLabel: '议题 #1 · 新开' }))
        // active 就是首次那条；再来一条同仓库的应进 queue，而不是改动 active
        p.push(makeItem({ groupKey: 'forge:acme/web', kind: 'forge', title: 'acme/web', eventLabel: '合并请求 #2 · 已合并' }))

        expect(p.active.value?.eventLabel).toBe('议题 #1 · 新开')
        expect(p.active.value?.count).toBeUndefined()
        // 合并只作用于 queue；active 命中同键时不合并，而是作为独立排队项
        expect(p.queue.value).toHaveLength(1)
        expect(p.queue.value[0].eventLabel).toBe('合并请求 #2 · 已合并')
        expect(p.queue.value[0].count).toBeUndefined()
    })

    it('drops the oldest queued item when the queue exceeds 3', () => {
        const p = useCompletionPopover()
        // 第 1 条成为 active，其余进 queue
        p.push(makeItem({ groupKey: 'session:s1', sessionId: 's1' }))
        p.push(makeItem({ groupKey: 'session:s2', sessionId: 's2' }))
        p.push(makeItem({ groupKey: 'session:s3', sessionId: 's3' }))
        p.push(makeItem({ groupKey: 'session:s4', sessionId: 's4' }))
        // 第 4 条排队项进队时超限，丢最旧的排队项 s2
        p.push(makeItem({ groupKey: 'session:s5', sessionId: 's5' }))

        expect(p.active.value?.sessionId).toBe('s1')
        expect(p.queue.value.map(i => i.sessionId)).toEqual(['s3', 's4', 's5'])
    })

    it('queue cap never drops the item currently on screen', () => {
        const p = useCompletionPopover()
        p.push(makeItem({ groupKey: 'session:s1', sessionId: 's1' }))
        for (let i = 2; i <= 8; i++) {
            p.push(makeItem({ groupKey: `session:s${i}`, sessionId: `s${i}` }))
        }

        expect(p.active.value?.sessionId).toBe('s1')
        expect(p.queue.value).toHaveLength(3)
    })
})
