import { describe, expect, it } from 'vitest'
import { parseGoToLine } from '../goToLine'

/**
 * parseGoToLine drives the Ctrl+G prompt (issue #547). The forms below are the
 * ones the prompt's own message advertises, so each one is pinned: an input the
 * UI promises to accept must not silently fall back to line 1.
 */
describe('parseGoToLine', () => {
    it('accepts an absolute line number', () => {
        expect(parseGoToLine('42', 1, 100)).toBe(42)
        expect(parseGoToLine('  7  ', 1, 100)).toBe(7)
    })

    it('accepts relative offsets from the current line', () => {
        expect(parseGoToLine('+7', 10, 100)).toBe(17)
        expect(parseGoToLine('-7', 10, 100)).toBe(3)
    })

    it('accepts a percentage of the document', () => {
        expect(parseGoToLine('50%', 1, 100)).toBe(50)
        expect(parseGoToLine('0%', 1, 100)).toBe(1)
        expect(parseGoToLine('100%', 1, 100)).toBe(100)
    })

    it('treats a signed percentage as relative to the current line', () => {
        // +10% of 100 lines from line 50 = 60; -10% = 40.
        expect(parseGoToLine('+10%', 50, 100)).toBe(60)
        expect(parseGoToLine('-10%', 50, 100)).toBe(40)
    })

    it('clamps out-of-range results instead of rejecting them', () => {
        expect(parseGoToLine('9999', 1, 100)).toBe(100)
        expect(parseGoToLine('0', 1, 100)).toBe(1)
        expect(parseGoToLine('-9999', 10, 100)).toBe(1)
        expect(parseGoToLine('+9999', 10, 100)).toBe(100)
    })

    it('returns null for input that is not a line reference', () => {
        // A typo must not silently jump to line 1 — the caller shows an error.
        expect(parseGoToLine('', 1, 100)).toBeNull()
        expect(parseGoToLine('   ', 1, 100)).toBeNull()
        expect(parseGoToLine('abc', 1, 100)).toBeNull()
        expect(parseGoToLine('1:5', 1, 100)).toBeNull()
        expect(parseGoToLine('50%%', 1, 100)).toBeNull()
        expect(parseGoToLine('+', 1, 100)).toBeNull()
    })

    it('tolerates a single-line document', () => {
        expect(parseGoToLine('5', 1, 1)).toBe(1)
        expect(parseGoToLine('50%', 1, 1)).toBe(1)
    })
})
