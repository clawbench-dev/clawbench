/**
 * Regression: a diff that could not be computed must never look like
 * "nothing changed".
 *
 * jsdiff's `timeout` option aborts on wall-clock time and returns `undefined`.
 * The old code did `diffLines(...) ?? []` / `diffChars(...) ?? []`, so an abort
 * produced an empty change list — i.e. the UI silently showed a modified file
 * as identical. That surfaced as a flake in the ">500 lines" test under load,
 * but the user-visible effect was a wrong diff, not a slow one.
 *
 * The tests below drive the abort path directly (mocked `diff` returning
 * undefined) instead of racing a real timer, so they are deterministic.
 */
import { beforeEach, describe, expect, it, vi } from 'vitest'

const diffLinesMock = vi.fn()
const diffCharsMock = vi.fn()

vi.mock('diff', () => ({
    diffLines: (...args: unknown[]) => diffLinesMock(...args),
    diffChars: (...args: unknown[]) => diffCharsMock(...args),
}))

const { computeDiff, charDiff } = await import('@/utils/diffUtils')

beforeEach(() => {
    diffLinesMock.mockReset()
    diffCharsMock.mockReset()
})

describe('computeDiff when jsdiff aborts', () => {
    it('reports every line as changed rather than as unchanged', () => {
        diffLinesMock.mockReturnValue(undefined)

        const result = computeDiff('a\nb\nc', 'a\nX\nc')

        expect(result.deletedInOld).toEqual([1, 2, 3])
        expect(result.addedInNew).toEqual([1, 2, 3])
        expect(result.modifiedPairs).toEqual([])
    })

    it('does not crash on empty input and reports nothing to diff', () => {
        diffLinesMock.mockReturnValue(undefined)

        const result = computeDiff('', '')

        expect(result.deletedInOld).toEqual([])
        expect(result.addedInNew).toEqual([])
    })

    it('counts a trailing newline as a terminator, not an extra line', () => {
        diffLinesMock.mockReturnValue(undefined)

        const result = computeDiff('a\nb\n', 'a\nb\n')

        expect(result.deletedInOld).toEqual([1, 2])
        expect(result.addedInNew).toEqual([1, 2])
    })

    it('does not pass a wall-clock timeout to jsdiff', () => {
        // The timeout is what makes the abort possible; the fix is to stop
        // asking for one. Guard the call shape so a reintroduced
        // `{ timeout: N }` fails here instead of silently degrading diffs.
        diffLinesMock.mockReturnValue([])
        computeDiff('a', 'b')
        expect(diffLinesMock.mock.calls[0]).toHaveLength(2)
        expect(diffLinesMock.mock.calls[0][2]).toBeUndefined()
    })
})

describe('charDiff when jsdiff aborts', () => {
    it('marks the whole line as replaced', () => {
        diffCharsMock.mockReturnValue(undefined)

        const result = {
            deletedInOld: [] as number[],
            addedInNew: [] as number[],
            deletedChars: new Map<number, { start: number; end: number }[]>(),
            addedChars: new Map<number, { start: number; end: number }[]>(),
            modifiedPairs: [] as [number, number][],
        }
        charDiff('abc', 'xyz', 4, 5, result)

        expect(result.deletedChars.get(4)).toEqual([{ start: 0, end: 3 }])
        expect(result.addedChars.get(5)).toEqual([{ start: 0, end: 3 }])
    })

    it('still leaves identical lines untouched', () => {
        const result = {
            deletedInOld: [] as number[],
            addedInNew: [] as number[],
            deletedChars: new Map<number, { start: number; end: number }[]>(),
            addedChars: new Map<number, { start: number; end: number }[]>(),
            modifiedPairs: [] as [number, number][],
        }
        charDiff('same', 'same', 1, 1, result)

        expect(result.deletedChars.size).toBe(0)
        expect(result.addedChars.size).toBe(0)
        expect(diffCharsMock).not.toHaveBeenCalled()
    })

    it('does not pass a wall-clock timeout to jsdiff', () => {
        diffCharsMock.mockReturnValue([])
        charDiff('a', 'b', 1, 1, {
            deletedInOld: [], addedInNew: [],
            deletedChars: new Map(), addedChars: new Map(), modifiedPairs: [],
        })
        expect(diffCharsMock.mock.calls[0]).toHaveLength(2)
        expect(diffCharsMock.mock.calls[0][2]).toBeUndefined()
    })
})
