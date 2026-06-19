/**
 * LCS diff algorithms for file content comparison.
 *
 * Uses jsdiff (Myers algorithm) for both line-level and char-level diff.
 * The old custom LCS implementation has been replaced for better accuracy
 * and performance (Myers O(ND) is faster than LCS O(MN) when edit distance D is small).
 *
 * Two-level diff:
 *   1. Line-level diff (diffLines) to identify added/deleted lines
 *   2. Char-level diff (diffChars) for modified line pairs (proximity-paired)
 */

import { diffLines, diffChars } from 'diff'
import type { Change } from 'diff'

export interface LineDiff {
    /** Lines deleted in old file (1-based line number in old text) */
    deletedInOld: number[]
    /** Lines added in new file (1-based line number in new text) */
    addedInNew: number[]
    /** For modified lines: old line → char-level ranges deleted */
    deletedChars: Map<number, { start: number; end: number }[]>
    /** For modified lines: new line → char-level ranges added */
    addedChars: Map<number, { start: number; end: number }[]>
}

/**
 * Full diff: line-level + char-level for changed lines.
 * Uses jsdiff's Myers algorithm with timeout support.
 */
export function computeDiff(oldText: string, newText: string): LineDiff {
    const result: LineDiff = {
        deletedInOld: [],
        addedInNew: [],
        deletedChars: new Map(),
        addedChars: new Map(),
    }

    // Line-level diff using jsdiff
    const changes = diffLines(oldText, newText, { timeout: 3 })

    // Track line numbers in old and new texts
    let oldLine = 1
    let newLine = 1

    // Collect deleted and added line groups for pairing
    const deleteGroups: Array<{ startOld: number; lines: string[] }> = []
    const addGroups: Array<{ startNew: number; lines: string[] }> = []

    for (const change of changes) {
        const lineCount = change.count || 0

        if (change.removed) {
            deleteGroups.push({
                startOld: oldLine,
                lines: change.value.replace(/\n$/, '').split('\n'),
            })
            oldLine += lineCount
        } else if (change.added) {
            addGroups.push({
                startNew: newLine,
                lines: change.value.replace(/\n$/, '').split('\n'),
            })
            newLine += lineCount
        } else {
            // Common lines — check if adjacent delete+add groups can be paired
            oldLine += lineCount
            newLine += lineCount
        }
    }

    // Pair adjacent delete+add groups as "modified" lines
    let di = 0, ai = 0
    while (di < deleteGroups.length && ai < addGroups.length) {
        const delGroup = deleteGroups[di]
        const addGroup = addGroups[ai]

        // Pair lines within the groups
        const pairCount = Math.min(delGroup.lines.length, addGroup.lines.length)

        for (let i = 0; i < pairCount; i++) {
            const oldLineNum = delGroup.startOld + i
            const newLineNum = addGroup.startNew + i
            charDiff(delGroup.lines[i], addGroup.lines[i], oldLineNum, newLineNum, result)
        }

        // Unpaired deleted lines
        for (let i = pairCount; i < delGroup.lines.length; i++) {
            result.deletedInOld.push(delGroup.startOld + i)
        }

        // Unpaired added lines
        for (let i = pairCount; i < addGroup.lines.length; i++) {
            result.addedInNew.push(addGroup.startNew + i)
        }

        di++
        ai++
    }

    // Remaining unpaired delete groups
    while (di < deleteGroups.length) {
        const delGroup = deleteGroups[di]
        for (let i = 0; i < delGroup.lines.length; i++) {
            result.deletedInOld.push(delGroup.startOld + i)
        }
        di++
    }

    // Remaining unpaired add groups
    while (ai < addGroups.length) {
        const addGroup = addGroups[ai]
        for (let i = 0; i < addGroup.lines.length; i++) {
            result.addedInNew.push(addGroup.startNew + i)
        }
        ai++
    }

    return result
}

/**
 * Char-level diff for a single modified line.
 * Populates result.deletedChars and result.addedChars.
 */
export function charDiff(oldLine: string, newLine: string, oldLineNum: number, newLineNum: number, result: LineDiff) {
    // Skip char-level diff for very long lines
    if (oldLine.length > 200 || newLine.length > 200) {
        result.deletedChars.set(oldLineNum, [{ start: 0, end: oldLine.length }])
        result.addedChars.set(newLineNum, [{ start: 0, end: newLine.length }])
        return
    }

    // If lines are identical, no char-level diff needed
    if (oldLine === newLine) return

    const changes = diffChars(oldLine, newLine, { timeout: 1 })

    // Convert char-level changes to offset ranges
    const deletedOffsets: number[] = []
    const addedOffsets: number[] = []
    let oldPos = 0
    let newPos = 0

    for (const change of changes) {
        const len = change.value.length
        if (change.removed) {
            for (let i = 0; i < len; i++) deletedOffsets.push(oldPos + i)
            oldPos += len
        } else if (change.added) {
            for (let i = 0; i < len; i++) addedOffsets.push(newPos + i)
            newPos += len
        } else {
            oldPos += len
            newPos += len
        }
    }

    if (deletedOffsets.length > 0) {
        result.deletedChars.set(oldLineNum, indicesToRanges(deletedOffsets))
    }
    if (addedOffsets.length > 0) {
        result.addedChars.set(newLineNum, indicesToRanges(addedOffsets))
    }
}

/**
 * Convert a set of character indices (0-based) into contiguous {start, end} ranges
 * using string offsets (for proper substring slicing with unicode).
 */
export function charIndicesToRanges(indices: Set<number>, text: string): { start: number; end: number }[] {
    if (indices.size === 0) return []
    const sorted = [...indices].sort((a, b) => a - b)
    const chars = [...text]
    // Build char-index → string-offset map
    const offsets: number[] = []
    let offset = 0
    for (const ch of chars) {
        offsets.push(offset)
        offset += ch.length // ch might be multi-byte in JS string
    }
    offsets.push(offset) // end offset for last char + 1

    const ranges: { start: number; end: number }[] = []
    let rangeStart = sorted[0]
    for (let k = 1; k <= sorted.length; k++) {
        const isEnd = k === sorted.length || sorted[k] !== sorted[k - 1] + 1
        if (isEnd) {
            const rangeEnd = sorted[k - 1]
            ranges.push({ start: offsets[rangeStart], end: offsets[rangeEnd + 1] })
            if (k < sorted.length) rangeStart = sorted[k]
        }
    }
    return ranges
}

/**
 * Simple line-by-line diff (no LCS). Used as fallback for large files.
 */
export function simpleLineDiff(oldLines: string[], newLines: string[], result: LineDiff) {
    const maxLen = Math.max(oldLines.length, newLines.length)
    for (let i = 0; i < maxLen; i++) {
        if (i >= oldLines.length) {
            result.addedInNew.push(i + 1)
        } else if (i >= newLines.length) {
            result.deletedInOld.push(i + 1)
        } else if (oldLines[i] !== newLines[i]) {
            charDiff(oldLines[i], newLines[i], i + 1, i + 1, result)
        }
    }
}

/**
 * LCS-based line diff with proximity pairing for char-level diff.
 */
export function lcsLineDiff(oldLines: string[], newLines: string[], result: LineDiff) {
    const m = oldLines.length
    const n = newLines.length

    // Build LCS table
    const dp: number[][] = Array.from({ length: m + 1 }, () => Array(n + 1).fill(0))
    for (let i = 1; i <= m; i++) {
        for (let j = 1; j <= n; j++) {
            if (oldLines[i - 1] === newLines[j - 1]) {
                dp[i][j] = dp[i - 1][j - 1] + 1
            } else {
                dp[i][j] = Math.max(dp[i - 1][j], dp[i][j - 1])
            }
        }
    }

    // Backtrack to classify lines
    const oldMatched = new Set<number>()
    const newMatched = new Set<number>()
    let i = m, j = n
    while (i > 0 && j > 0) {
        if (oldLines[i - 1] === newLines[j - 1]) {
            oldMatched.add(i - 1)
            newMatched.add(j - 1)
            i--; j--
        } else if (dp[i - 1][j] >= dp[i][j - 1]) {
            i--
        } else {
            j--
        }
    }

    for (let oi = 0; oi < m; oi++) {
        if (!oldMatched.has(oi)) result.deletedInOld.push(oi + 1)
    }
    for (let nj = 0; nj < n; nj++) {
        if (!newMatched.has(nj)) result.addedInNew.push(nj + 1)
    }

    // Pair by proximity for char-level diff
    const deletedSorted = result.deletedInOld.slice().sort((a, b) => a - b)
    const addedSorted = result.addedInNew.slice().sort((a, b) => a - b)
    const pairedOld = new Set<number>()
    const pairedNew = new Set<number>()

    for (const oi of deletedSorted) {
        let bestJ = -1, bestDist = Infinity
        for (const nj of addedSorted) {
            if (pairedNew.has(nj)) continue
            const dist = Math.abs(oi - nj)
            if (dist < bestDist) { bestDist = dist; bestJ = nj }
        }
        if (bestJ >= 0 && bestDist <= 3) {
            pairedOld.add(oi)
            pairedNew.add(bestJ)
            charDiff(oldLines[oi - 1], newLines[bestJ - 1], oi, bestJ, result)
        }
    }
    result.deletedInOld = result.deletedInOld.filter(l => !pairedOld.has(l))
    result.addedInNew = result.addedInNew.filter(l => !pairedNew.has(l))
}

/**
 * Convert a sorted array of char indices into contiguous {start, end} ranges.
 */
function indicesToRanges(indices: number[]): { start: number; end: number }[] {
    if (indices.length === 0) return []
    const ranges: { start: number; end: number }[] = []
    let rangeStart = indices[0]

    for (let i = 1; i <= indices.length; i++) {
        if (i === indices.length || indices[i] !== indices[i - 1] + 1) {
            ranges.push({ start: rangeStart, end: indices[i - 1] + 1 })
            if (i < indices.length) rangeStart = indices[i]
        }
    }

    return ranges
}

/**
 * Convert line-level deletions to whole-line FlashRange.
 * end=Infinity is clamped to rawLine.length in CodePreview's applyFlashToLine.
 */
export function wholeLineRanges(lineNums: number[]): { line: number; start: number; end: number }[] {
    return lineNums.map(line => ({ line, start: 0, end: Infinity }))
}

/**
 * Convert char-level diff maps to FlashRange arrays.
 */
export function charMapToRanges(map: Map<number, { start: number; end: number }[]>): { line: number; start: number; end: number }[] {
    const ranges: { line: number; start: number; end: number }[] = []
    for (const [line, chars] of map) {
        for (const { start, end } of chars) {
            ranges.push({ line, start, end })
        }
    }
    return ranges
}
