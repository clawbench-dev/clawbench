import { describe, expect, it } from 'vitest'
import { clampScrollTop, findBlockAtOrBefore } from '@/utils/markdownScroll'
import { collectLineBlocks, renderedTopLine, scrollRenderedToLine } from '@/utils/scrollRenderedToLine'

describe('findBlockAtOrBefore', () => {
    const blocks = [
        { line: 1, top: 0 },
        { line: 3, top: 120 },
        { line: 7, top: 340 },
    ]

    it('returns -1 for an empty list', () => {
        expect(findBlockAtOrBefore([], 5)).toBe(-1)
    })

    it('finds the last block owning a source line', () => {
        expect(findBlockAtOrBefore(blocks, 1)).toBe(0)
        expect(findBlockAtOrBefore(blocks, 2)).toBe(0)
        expect(findBlockAtOrBefore(blocks, 3)).toBe(1)
        expect(findBlockAtOrBefore(blocks, 6)).toBe(1)
        expect(findBlockAtOrBefore(blocks, 7)).toBe(2)
        expect(findBlockAtOrBefore(blocks, 999)).toBe(2)
    })

    it('returns the first block when the target precedes every block', () => {
        expect(findBlockAtOrBefore(blocks, 0)).toBe(0)
    })
})

describe('clampScrollTop', () => {
    it('clamps to the scrollable range and rounds', () => {
        expect(clampScrollTop({ scrollHeight: 100, clientHeight: 50 }, 200)).toBe(50)
        expect(clampScrollTop({ scrollHeight: 100, clientHeight: 50 }, -5)).toBe(0)
        expect(clampScrollTop({ scrollHeight: 100, clientHeight: 50 }, 30.6)).toBe(31)
    })

    it('returns 0 when content does not overflow', () => {
        expect(clampScrollTop({ scrollHeight: 50, clientHeight: 50 }, 10)).toBe(0)
        expect(clampScrollTop({ scrollHeight: 40, clientHeight: 50 }, 10)).toBe(0)
    })
})

describe('scrollRenderedToLine / renderedTopLine', () => {
    /** Fake scroll container whose [data-source-line] blocks have fixed content tops. */
    function makeContainer(tops: { line: number; top: number }[]) {
        // In a real browser, an element's getBoundingClientRect().top is a
        // viewport coordinate that decreases as the container scrolls. Mirror
        // that here so collectLineBlocks' rect-diff recovers the content top.
        const container: any = {
            scrollTop: 0,
            scrollHeight: 1000,
            clientHeight: 200,
            getBoundingClientRect: () => ({ top: 0 } as DOMRect),
        }
        const els = tops.map((b) => {
            const el: any = {
                getAttribute: () => String(b.line),
                getBoundingClientRect: () => ({ top: b.top - container.scrollTop } as DOMRect),
            }
            return el
        })
        const content: any = { querySelectorAll: () => els }
        container.querySelector = () => content
        return container
    }

    it('collectLineBlocks measures tops relative to the container scroll area', () => {
        const container = makeContainer([
            { line: 1, top: 0 },
            { line: 5, top: 200 },
        ])
        container.scrollTop = 30
        const blocks = collectLineBlocks(container)
        // rect-diff cancels the scroll offset, recovering content coordinates.
        expect(blocks.map((b) => ({ line: b.line, top: b.top }))).toEqual([
            { line: 1, top: 0 },
            { line: 5, top: 200 },
        ])
    })

    it('renderedTopLine returns the block at/above the viewport top', () => {
        const container = makeContainer([
            { line: 1, top: 0 },
            { line: 5, top: 200 },
            { line: 9, top: 500 },
        ])
        container.scrollTop = 250
        expect(renderedTopLine(container)).toBe(5)
        container.scrollTop = 510
        expect(renderedTopLine(container)).toBe(9)
        // just before the first block (scrolled into leading whitespace)
        container.scrollTop = -5
        expect(renderedTopLine(container)).toBe(1)
    })

    it('returns null when no line blocks are present', () => {
        const container = makeContainer([])
        expect(renderedTopLine(container)).toBeNull()
        expect(scrollRenderedToLine(container, 3)).toBe(false)
    })

    it('scrollRenderedToLine aligns the owning block top to the viewport top', () => {
        const container = makeContainer([
            { line: 1, top: 0 },
            { line: 5, top: 200 },
            { line: 9, top: 500 },
        ])
        expect(scrollRenderedToLine(container, 6)).toBe(true)
        // line 6 is owned by the block at line 5 (top 200)
        expect(container.scrollTop).toBe(200)
        expect(scrollRenderedToLine(container, 3)).toBe(true)
        expect(container.scrollTop).toBe(0)
    })

    it('clamps scrollTop to the scrollable range', () => {
        const container = makeContainer([{ line: 1, top: 900 }])
        container.scrollHeight = 1000
        container.clientHeight = 200
        expect(scrollRenderedToLine(container, 1)).toBe(true)
        expect(container.scrollTop).toBe(800) // max = 1000 − 200
    })

    it('survives a block whose data-source-line is malformed', () => {
        const bad = {
            el: {
                getAttribute: () => 'not-a-number',
                getBoundingClientRect: () => ({ top: 10 } as DOMRect),
            } as unknown as HTMLElement,
        }
        const content: any = { querySelectorAll: () => [bad.el] }
        const container: any = {
            scrollTop: 0,
            scrollHeight: 100,
            clientHeight: 50,
            getBoundingClientRect: () => ({ top: 0 } as DOMRect),
            querySelector: () => content,
        }
        expect(renderedTopLine(container)).toBeNull()
        expect(scrollRenderedToLine(container, 3)).toBe(false)
    })
})
