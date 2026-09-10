import { describe, expect, it } from 'vitest'
import {
    collectLineBlocks,
    findBlockAtOrBefore,
    renderedLineAnchor,
    renderedLineScrollTop,
} from '@/utils/scrollRenderedToLine'

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

describe('scrollRenderedToLine helpers', () => {
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

    it('renderedLineAnchor returns the owning line + in-block offset', () => {
        const container = makeContainer([
            { line: 1, top: 0 },
            { line: 5, top: 200 },
            { line: 9, top: 500 },
        ])
        container.scrollTop = 250
        expect(renderedLineAnchor(container)).toEqual({ line: 5, offset: 50 })
        container.scrollTop = 510
        expect(renderedLineAnchor(container)).toEqual({ line: 9, offset: 10 })
        // just before the first block (scrolled into leading whitespace)
        container.scrollTop = -5
        expect(renderedLineAnchor(container)).toEqual({ line: 1, offset: 0 })
    })

    it('renderedLineScrollTop returns the owning block top (unclamped)', () => {
        const container = makeContainer([
            { line: 1, top: 0 },
            { line: 5, top: 200 },
            { line: 9, top: 500 },
        ])
        // line 6 is owned by the block at line 5 (top 200)
        expect(renderedLineScrollTop(container, 6)).toBe(200)
        expect(renderedLineScrollTop(container, 3)).toBe(0)
        // not clamped to maxScroll (1000 − 200 = 800)
        const far = makeContainer([{ line: 1, top: 900 }])
        expect(renderedLineScrollTop(far, 1)).toBe(900)
    })

    it('returns null when no line blocks are present', () => {
        const container = makeContainer([])
        expect(renderedLineAnchor(container)).toBeNull()
        expect(renderedLineScrollTop(container, 3)).toBeNull()
    })

    it('ignores a block whose data-source-line is malformed', () => {
        const bad = {
            getAttribute: () => 'not-a-number',
            getBoundingClientRect: () => ({ top: 10 } as DOMRect),
        } as unknown as HTMLElement
        const content: any = { querySelectorAll: () => [bad] }
        const container: any = {
            scrollTop: 0,
            scrollHeight: 100,
            clientHeight: 50,
            getBoundingClientRect: () => ({ top: 0 } as DOMRect),
            querySelector: () => content,
        }
        expect(renderedLineAnchor(container)).toBeNull()
        expect(renderedLineScrollTop(container, 3)).toBeNull()
    })
})
