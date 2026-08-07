import { describe, expect, it } from 'vitest'
import { pickPreviewAnchor, pickCmAnchor, relTopFor, scrollTopFor } from '../markdownScroll.ts'

describe('pickPreviewAnchor', () => {
    it('picks the last heading at/above the viewport top (section on screen)', () => {
        const headings = [
            { id: 'intro', line: 1, contentTop: 0 },
            { id: 'install', line: 5, contentTop: 600 },
            { id: 'usage', line: 12, contentTop: 1200 },
        ]
        const anchor = pickPreviewAnchor(headings, 700)
        expect(anchor).toEqual({ id: 'install', line: 5, relTop: 600 - 700 })
    })

    it('anchors on the first heading when scrolled into its section', () => {
        const headings = [
            { id: 'intro', line: 1, contentTop: 0 },
            { id: 'install', line: 5, contentTop: 600 },
        ]
        const anchor = pickPreviewAnchor(headings, 650)
        expect(anchor?.id).toBe('install')
        expect(anchor?.relTop).toBe(600 - 650)
    })

    it('returns the last heading even when scrolled far past several', () => {
        const headings = [
            { id: 'a', line: 1, contentTop: 0 },
            { id: 'b', line: 3, contentTop: 100 },
            { id: 'c', line: 6, contentTop: 300 },
            { id: 'd', line: 9, contentTop: 900 },
        ]
        const anchor = pickPreviewAnchor(headings, 500)
        expect(anchor?.id).toBe('c')
    })

    it('returns null when no heading is at/above the viewport (preamble before first heading)', () => {
        const headings = [{ id: 'first', line: 1, contentTop: 50 }]
        expect(pickPreviewAnchor(headings, 0)).toBeNull()
    })

    it('returns null for empty headings', () => {
        expect(pickPreviewAnchor([], 100)).toBeNull()
    })
})

describe('pickCmAnchor', () => {
    it('picks the last heading at or above the top visible line', () => {
        const toc = [
            { id: 'a', line: 1 },
            { id: 'b', line: 5 },
            { id: 'c', line: 12 },
        ]
        expect(pickCmAnchor(toc, 8)?.id).toBe('b')
        expect(pickCmAnchor(toc, 12)?.id).toBe('c')
        expect(pickCmAnchor(toc, 1)?.id).toBe('a')
    })

    it('returns null when top line precedes the first heading', () => {
        const toc = [{ id: 'a', line: 5 }]
        expect(pickCmAnchor(toc, 3)).toBeNull()
    })

    it('returns null for empty toc', () => {
        expect(pickCmAnchor([], 10)).toBeNull()
    })
})

describe('relTopFor / scrollTopFor', () => {
    it('relTopFor is contentTop minus scrollTop', () => {
        expect(relTopFor(600, 700)).toBe(-100)
        expect(relTopFor(100, 50)).toBe(50)
    })

    it('scrollTopFor places the heading at the given relTop and never goes negative', () => {
        expect(scrollTopFor(600, -100)).toBe(700)
        expect(scrollTopFor(200, 50)).toBe(150)
        expect(scrollTopFor(100, 200)).toBe(0)
    })

    it('round-trips: relTopFor then scrollTopFor reproduces contentTop', () => {
        const scrollTop = 750
        const contentTop = 620
        const relTop = relTopFor(contentTop, scrollTop)
        expect(scrollTopFor(contentTop, relTop)).toBe(scrollTop)
    })
})
