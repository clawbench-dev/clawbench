/**
 * useMarkdownDiff — Block-level diff for Markdown rendered mode.
 *
 * Replaces the old snippet-search approach with structured block diff:
 *   1. Extract block-level elements from rendered DOM
 *   2. Block-level diff (innerHTML) to detect structural + formatting changes
 *   3. Char-level diff (textContent) for modified blocks
 *   4. Generate marker data for Vue-rendered overlay
 *   5. Bottom drawer shows char-level diff on marker click
 *
 * P0 fixes from architecture review:
 *   - Old DOM snapshot via lastBlockList cache (timing-safe)
 *   - innerHTML for block diff (detects formatting changes)
 *   - Markers integrated into Vue render cycle (not imperative DOM)
 */

import { ref, shallowRef } from 'vue'
import { diffArrays } from 'diff'
import { diffChars } from 'diff'
import type { Change } from 'diff'
import { renderMarkdown } from '@/composables/useMarkdownRenderer.ts'
import type { DiffLine } from '@/utils/diff.ts'

// ─── Types ───

/** A block extracted from rendered Markdown DOM */
export interface BlockInfo {
    /** Tag name (uppercase, e.g. 'H1', 'P', 'LI') */
    tag: string
    /** textContent — used for char-level diff display */
    textContent: string
    /** innerHTML — used for block-level diff comparison */
    innerHTML: string
    /** CSS selector or path to locate this block in the live DOM */
    selector: string
    /** Mermaid source (only for mermaid blocks) */
    mermaidSource?: string
}

/** Marker types shown on the right side of changed blocks */
export type MarkerType = 'modified' | 'deleted' | 'added'

/** A marker displayed on the right side of a changed block/line */
export interface DiffMarker {
    /** Unique ID for Vue :key */
    id: string
    /** Marker type */
    type: MarkerType
    /** Label text: M / D / + */
    label: string
    /** CSS selector to locate the target block in the live DOM (Markdown only) */
    blockSelector: string
    /** 1-based line numbers for code markers (Code only) */
    lineNumbers?: number[]
    /** Char-level diff data for the drawer */
    charDiff: CharDiff | null
    /** Unified diff lines for drawer rendering */
    diffLines?: DiffLine[]
    /** aria-label for accessibility */
    ariaLabel: string
}

/** Marker info for code rendering — one per marker group */
export interface CodeDiffMarkerInfo {
    /** Marker type */
    type: MarkerType
    /** Label text: M / D / + */
    label: string
    /** Marker id — shared by grouped lines, used for click → drawer */
    id: string
    /** Number of lines this marker spans (for multi-line marker height) */
    lineCount?: number
}

/** Char-level diff result for a modified block */
export interface CharDiff {
    oldText: string
    newText: string
    changes: Change[]
}

/** Re-export DiffLine from diff.ts for consumers */
export type { DiffLine } from '@/utils/diff.ts'

/** Result of a full diff computation */
export interface DiffResult {
    markers: DiffMarker[]
    hasChanges: boolean
}

// ─── Block extraction ───

/** A block extracted from rendered Markdown DOM, with live element reference */
export interface BlockInfoWithEl extends BlockInfo {
    /** Live DOM element reference */
    el: Element
}

/** Tags that are considered block-level for diff purposes */
export const BLOCK_TAGS = new Set([
    'H1', 'H2', 'H3', 'H4', 'H5', 'H6',
    'P', 'LI', 'PRE', 'BLOCKQUOTE', 'HR',
])

/**
 * Extract block-level elements from a container element.
 * Outermost-first strategy: when a block is found, skip its children
 * (except for structural containers like LI and BLOCKQUOTE).
 */
export function extractBlocks(container: Element): BlockInfo[] {
    const blocks: BlockInfo[] = []

    const walk = (parent: Element, path: string) => {
        let childIndex = 0
        for (const child of parent.children) {
            const childPath = `${path} > :nth-child(${childIndex + 1})`
            const tag = child.tagName

            if (isDiffBlock(child)) {
                blocks.push(toBlockInfo(child, childPath, blocks.length))

                // Recurse into structural containers
                if (tag === 'LI' || tag === 'BLOCKQUOTE') {
                    walk(child, childPath)
                }
            } else if (tag === 'DIV' && child.classList.contains('katex-display')) {
                // Display math as independent block
                blocks.push(toBlockInfo(child, childPath, blocks.length))
            } else {
                // Non-block element: recurse to find blocks inside
                walk(child, childPath)
            }
            childIndex++
        }
    }

    walk(container, ':scope')
    return blocks
}

/**
 * Extract block-level elements with live DOM references.
 * Same logic as extractBlocks, but returns { el, info } pairs
 * so callers don't need to re-walk the DOM to find elements.
 */
export function extractBlockElements(container: Element): BlockInfoWithEl[] {
    const blocks: BlockInfoWithEl[] = []

    const walk = (parent: Element, path: string) => {
        let childIndex = 0
        for (const child of parent.children) {
            const childPath = `${path} > :nth-child(${childIndex + 1})`
            const tag = child.tagName

            if (isDiffBlock(child)) {
                blocks.push({ el: child, ...toBlockInfo(child, childPath, blocks.length) })

                if (tag === 'LI' || tag === 'BLOCKQUOTE') {
                    walk(child, childPath)
                }
            } else if (tag === 'DIV' && child.classList.contains('katex-display')) {
                blocks.push({ el: child, ...toBlockInfo(child, childPath, blocks.length) })
            } else {
                walk(child, childPath)
            }
            childIndex++
        }
    }

    walk(container, ':scope')
    return blocks
}

/**
 * Check if an element is a diff-relevant block.
 * Single source of truth — used by extractBlocks, extractBlockElements, and MarkdownPreview.
 */
export function isDiffBlock(el: Element): boolean {
    const tag = el.tagName

    // Standard block tags
    if (BLOCK_TAGS.has(tag)) return true

    // div.table-wrap
    if (tag === 'DIV' && el.classList.contains('table-wrap')) return true

    // div.mermaid (already rendered)
    if (tag === 'DIV' && el.classList.contains('mermaid')) return true

    return false
}

function toBlockInfo(el: Element, selector: string, _index: number): BlockInfo {
    const tag = el.tagName
    const info: BlockInfo = {
        tag,
        textContent: el.textContent || '',
        innerHTML: el.innerHTML,
        selector,
    }

    // For pre/code blocks, use textContent for innerHTML comparison
    // (hljs syntax highlighting produces noisy innerHTML)
    if (tag === 'PRE') {
        info.innerHTML = el.textContent || ''
    }

    // For mermaid blocks, use data-mermaid source
    if (tag === 'DIV' && el.classList.contains('mermaid')) {
        info.mermaidSource = el.getAttribute('data-mermaid') || ''
        info.innerHTML = info.mermaidSource
    }

    return info
}

// ─── Block comparison key ───

/**
 * Get the comparison key for a block.
 * Uses textContent for semantic comparison — innerHTML is unreliable
 * because live DOM blocks may contain rendered artifacts (image timestamps,
 * file-path annotations, hljs classes) that differ from offscreen renders
 * even when the actual content is identical.
 */
function blockKey(block: BlockInfo): string {
    if (block.mermaidSource !== undefined) return block.mermaidSource
    return block.textContent
}

// ─── Diff computation ───

/**
 * Compute block-level diff between old and new block lists.
 * Returns markers for changed blocks.
 */
export function computeMarkdownDiff(
    oldBlocks: BlockInfo[],
    newBlocks: BlockInfo[],
): DiffResult {
    if (oldBlocks.length === 0 && newBlocks.length === 0) {
        return { markers: [], hasChanges: false }
    }

    // Empty → non-empty: all added
    if (oldBlocks.length === 0) {
        return {
            markers: newBlocks.map((b, i) => toMarker('added', b, i, null)),
            hasChanges: true,
        }
    }

    // Non-empty → empty: all deleted
    if (newBlocks.length === 0) {
        // All deletions attach to the "previous block" — but there is none.
        // Return a single special marker.
        return {
            markers: [{
                id: 'del-all',
                type: 'deleted',
                label: 'D',
                blockSelector: ':scope',
                charDiff: { oldText: oldBlocks.map(b => b.textContent).join('\n'), newText: '', changes: [] },
                diffLines: oldBlocks.map(b => b.textContent).join('\n').split('\n').map((content, i) => ({ type: 'del' as const, content, oldLine: i + 1, newLine: null })),
                ariaLabel: `${oldBlocks.length} blocks deleted`,
            }],
            hasChanges: true,
        }
    }

    // Block-level diff using innerHTML keys
    const oldKeys = oldBlocks.map(blockKey)
    const newKeys = newBlocks.map(blockKey)

    const changes = diffArrays(oldKeys, newKeys)

    const markers: DiffMarker[] = []
    let oldIdx = 0
    let newIdx = 0
    let skipNext = false  // Skip paired additions that were already processed

    for (const change of changes) {
        if (skipNext) {
            skipNext = false
            continue
        }

        if (!change.added && !change.removed) {
            // Common blocks — no markers
            oldIdx += change.count
            newIdx += change.count
        } else if (change.removed) {
            const removedCount = change.count
            // Look ahead: is the next change an addition? (modified pair)
            const nextChangeIdx = changes.indexOf(change) + 1
            const nextChange = nextChangeIdx < changes.length ? changes[nextChangeIdx] : null

            if (nextChange && nextChange.added) {
                // Modified: pair removed with added blocks
                const addedCount = nextChange.count
                const pairCount = Math.min(removedCount, addedCount)

                // Paired blocks: modified (only if content actually changed)
                for (let i = 0; i < pairCount; i++) {
                    const oldBlock = oldBlocks[oldIdx + i]
                    const newBlock = newBlocks[newIdx + i]
                    if (oldBlock.textContent === newBlock.textContent) {
                        // innerHTML changed (e.g. link resolution, auto-numbering)
                        // but actual text is the same — not a real modification
                        continue
                    }
                    const charDiff = computeCharDiff(oldBlock.textContent, newBlock.textContent)
                    markers.push(toMarker('modified', newBlock, newIdx + i, charDiff, oldBlock.textContent))
                }

                // Extra removed blocks: deleted — merge consecutive into one marker
                if (removedCount > pairCount) {
                    const extraRemovedBlocks = oldBlocks.slice(oldIdx + pairCount, oldIdx + removedCount)
                    const prevNewIdx = newIdx + pairCount - 1
                    const targetBlock = prevNewIdx >= 0 ? newBlocks[prevNewIdx] : newBlocks[0]
                    const mergedText = extraRemovedBlocks.map(b => b.textContent).join('\n')
                    const charDiff: CharDiff = {
                        oldText: mergedText,
                        newText: '',
                        changes: [{ value: mergedText, removed: true, added: false, count: 1 }],
                    }
                    markers.push(toMarker('deleted', targetBlock, prevNewIdx, charDiff, undefined, oldIdx + pairCount, extraRemovedBlocks.length))
                }

                // Extra added blocks: added
                for (let i = pairCount; i < addedCount; i++) {
                    const newBlock = newBlocks[newIdx + i]
                    markers.push(toMarker('added', newBlock, newIdx + i, null))
                }

                oldIdx += removedCount
                newIdx += addedCount
                skipNext = true  // Skip the paired addition in the next iteration
            } else {
                // Pure deletion: no following addition — merge consecutive into one marker
                const removedBlocks = oldBlocks.slice(oldIdx, oldIdx + removedCount)
                const prevNewIdx = Math.max(0, newIdx - 1)
                const targetBlock = newBlocks[prevNewIdx]
                const mergedText = removedBlocks.map(b => b.textContent).join('\n')
                const charDiff: CharDiff = {
                    oldText: mergedText,
                    newText: '',
                    changes: [{ value: mergedText, removed: true, added: false, count: 1 }],
                }
                markers.push(toMarker('deleted', targetBlock, prevNewIdx, charDiff, undefined, oldIdx, removedBlocks.length))
                oldIdx += removedCount
            }
        } else if (change.added) {
            // Pure addition
            for (let i = 0; i < change.count; i++) {
                const newBlock = newBlocks[newIdx + i]
                const charDiff: CharDiff = {
                    oldText: '',
                    newText: newBlock.textContent,
                    changes: [{ value: newBlock.textContent, removed: false, added: true, count: 1 }],
                }
                markers.push(toMarker('added', newBlock, newIdx + i, charDiff))
            }
            newIdx += change.count
        }
    }

    return {
        markers,
        hasChanges: markers.length > 0,
    }
}

// ─── Marker label mapping ───

const MARKER_LABELS: Record<MarkerType, string> = { modified: 'M', deleted: 'D', added: '+' }

/**
 * Shared factory for constructing DiffMarker objects.
 * Centralizes the label mapping and ensures consistent construction.
 */
function buildDiffMarker(opts: {
    id: string
    type: MarkerType
    blockSelector?: string
    lineNumbers?: number[]
    charDiff: CharDiff | null
    diffLines?: DiffLine[]
    ariaLabel: string
}): DiffMarker {
    return {
        id: opts.id,
        type: opts.type,
        label: MARKER_LABELS[opts.type],
        blockSelector: opts.blockSelector ?? '',
        lineNumbers: opts.lineNumbers,
        charDiff: opts.charDiff,
        diffLines: opts.diffLines,
        ariaLabel: opts.ariaLabel,
    }
}

function toMarker(type: MarkerType, block: BlockInfo, blockIndex: number, charDiff: CharDiff | null, oldBlockText?: string, oldBlockStartIdx?: number, blockCount?: number): DiffMarker {
    const countLabel = blockCount && blockCount > 1 ? ` (${blockCount} blocks)` : ''
    const ariaLabel = `${type.charAt(0).toUpperCase() + type.slice(1)}: ${block.tag}${countLabel}`
    // For modified/deleted blocks with old content, use line-level diff
    let diffLines: DiffLine[] | undefined
    if (type === 'modified' && oldBlockText !== undefined && oldBlockText !== block.textContent) {
        diffLines = contentToDiffLines(oldBlockText, block.textContent)
    } else if (type === 'deleted' && charDiff) {
        diffLines = charDiffToLines(charDiff)
    } else if (type === 'added') {
        // Pure addition: show all lines as added
        const lines = block.textContent ? block.textContent.split('\n') : []
        diffLines = lines.map((content, i) => ({ type: 'add' as const, content, oldLine: null, newLine: i + 1 }))
    } else if (charDiff) {
        diffLines = charDiffToLines(charDiff)
    }
    // Use oldBlockStartIdx to make deleted marker IDs unique when multiple
    // old blocks are merged into one marker (avoids duplicate keys in v-for)
    const idSuffix = type === 'deleted' && oldBlockStartIdx !== undefined
        ? `${blockIndex}-old${oldBlockStartIdx}-${block.tag}`
        : `${blockIndex}-${block.tag}`
    return buildDiffMarker({
        id: `${type}-${idSuffix}`,
        type,
        blockSelector: block.selector,
        charDiff,
        diffLines,
        ariaLabel,
    })
}

/**
 * Compute char-level diff between two text strings.
 */
export function computeCharDiff(oldText: string, newText: string): CharDiff {
    const changes = diffChars(oldText, newText, { timeout: 3 })
    return { oldText, newText, changes: changes || [] }
}

/**
 * Convert a CharDiff into DiffLine[] for unified diff rendering.
 *
 * Strategy: split oldText/newText by \n, walk the changes array,
 * and track old/new line numbers to produce line-level DiffLine entries.
 *
 * For each change:
 *   - removed → 'del' lines with old line numbers
 *   - added   → 'add' lines with new line numbers
 *   - common  → 'ctx' lines with both line numbers
 *
 * Multi-line change values are split by \n and each sub-line becomes
 * its own DiffLine entry.
 */
export function charDiffToLines(charDiff: CharDiff): DiffLine[] {
    if (!charDiff.changes || charDiff.changes.length === 0) return []

    let oldLineNum = 1
    let newLineNum = 1
    const result: DiffLine[] = []

    for (const change of charDiff.changes) {
        const lines = change.value.split('\n')
        // If the value ends with \n, split produces a trailing empty string — remove it
        if (change.value.endsWith('\n')) lines.pop()

        if (change.removed) {
            for (const line of lines) {
                result.push({ type: 'del', content: line, oldLine: oldLineNum++, newLine: null })
            }
        } else if (change.added) {
            for (const line of lines) {
                result.push({ type: 'add', content: line, oldLine: null, newLine: newLineNum++ })
            }
        } else {
            for (const line of lines) {
                result.push({ type: 'ctx', content: line, oldLine: oldLineNum++, newLine: newLineNum++ })
            }
        }
    }

    return result
}

/**
 * Convert old/new text content directly to DiffLine[] using line-level diff.
 * Used when we have raw old/new content (e.g., code diff) rather than CharDiff.
 */
export function contentToDiffLines(oldContent: string, newContent: string): DiffLine[] {
    const oldLines = oldContent ? oldContent.split('\n') : []
    const newLines = newContent ? newContent.split('\n') : []

    if (oldLines.length === 0 && newLines.length === 0) return []

    const changes = diffArrays(oldLines, newLines)

    let oldLineNum = 1
    let newLineNum = 1
    const result: DiffLine[] = []

    for (const change of changes) {
        if (change.removed) {
            for (let i = 0; i < change.count; i++) {
                result.push({ type: 'del', content: oldLines[oldLineNum - 1], oldLine: oldLineNum++, newLine: null })
            }
        } else if (change.added) {
            for (let i = 0; i < change.count; i++) {
                result.push({ type: 'add', content: newLines[newLineNum - 1], oldLine: null, newLine: newLineNum++ })
            }
        } else {
            for (let i = 0; i < change.count; i++) {
                result.push({ type: 'ctx', content: oldLines[oldLineNum - 1], oldLine: oldLineNum++, newLine: newLineNum++ })
            }
        }
    }

    return result
}

/**
 * Like contentToDiffLines, but only returns a window around the specified
 * center lines with N lines of context. Collapses long context gaps into
 * a single ellipsis marker.
 *
 * @param centerNewLines  1-based new-file line numbers to center on
 * @param contextLines    number of unchanged context lines on each side (default 3)
 */
export function contentToDiffLinesWithCtx(
    oldContent: string,
    newContent: string,
    centerNewLines: number[],
    contextLines: number = 3,
): DiffLine[] {
    const allLines = contentToDiffLines(oldContent, newContent)
    if (allLines.length === 0) return []
    if (centerNewLines.length === 0) return allLines

    // Find the index range in allLines that covers the center lines ± context
    const centerSet = new Set(centerNewLines)

    // Mark each line as "center" (changed) or not
    const isCenter = allLines.map((dl, idx) => {
        if (dl.type !== 'ctx' && dl.newLine !== null) return centerSet.has(dl.newLine)
        // For deleted lines, check if they are adjacent to any center new line
        if (dl.type === 'del' && dl.oldLine !== null) {
            const prev = idx > 0 ? allLines[idx - 1] : null
            const next = idx < allLines.length - 1 ? allLines[idx + 1] : null
            if (prev && prev.newLine !== null && centerSet.has(prev.newLine)) return true
            if (next && next.newLine !== null && centerSet.has(next.newLine)) return true
        }
        return false
    })

    // Find ranges to keep: center indices ± contextLines
    const keep = new Set<number>()
    for (let i = 0; i < allLines.length; i++) {
        if (isCenter[i]) {
            const lo = Math.max(0, i - contextLines)
            const hi = Math.min(allLines.length - 1, i + contextLines)
            for (let j = lo; j <= hi; j++) keep.add(j)
        }
    }

    // Build result with ellipsis for gaps
    const result: DiffLine[] = []
    const sortedKeep = [...keep].sort((a, b) => a - b)

    // Leading ellipsis if we're not starting from the beginning
    if (sortedKeep.length > 0 && sortedKeep[0] > 0) {
        result.push({ type: 'ctx', content: '⋯', oldLine: null, newLine: null, isEllipsis: true })
    }

    let lastKept = -1
    for (const i of sortedKeep) {
        if (lastKept >= 0 && i > lastKept + 1) {
            // Insert ellipsis separator
            result.push({ type: 'ctx', content: '⋯', oldLine: null, newLine: null, isEllipsis: true })
        }
        result.push(allLines[i])
        lastKept = i
    }

    // Trailing ellipsis if we're not ending at the last line
    if (sortedKeep.length > 0 && sortedKeep[sortedKeep.length - 1] < allLines.length - 1) {
        result.push({ type: 'ctx', content: '⋯', oldLine: null, newLine: null, isEllipsis: true })
    }

    return result
}

// ─── Code diff markers ───

/**
 * Convert a LineDiff (from diffUtils.computeDiff) into DiffMarker[] for code files.
 *
 * - Modified lines (paired by computeDiff): marker on the NEW line number
 * - Added lines: marker on the new line number
 * - Deleted lines: marker anchored to nearest surviving new line
 *
 * Groups consecutive same-type lines into single markers with shared CharDiff.
 */
export function computeCodeDiffMarkers(
    lineDiff: import('@/utils/diffUtils.ts').LineDiff,
    oldContent: string,
    newContent: string,
): DiffMarker[] {
    const oldLines = oldContent.split('\n')
    const newLines = newContent.split('\n')
    const markers: DiffMarker[] = []

    // ── Modified lines ──
    // Use modifiedPairs (explicit old↔new pairing) instead of trying to
    // reconstruct the pairing from deletedChars/addedChars keys, which
    // fails when only one side has entries (e.g. pure deletion or addition
    // within a modified line).
    const modifiedNewLineSet = new Set(lineDiff.modifiedPairs.map(([, nl]) => nl))

    // Group consecutive modified new lines, preserving old→new mapping
    const sortedPairs = [...lineDiff.modifiedPairs].sort((a, b) => a[1] - b[1])
    for (const group of groupConsecutivePairs(sortedPairs)) {
        const newLineNums = group.map(([, nl]) => nl)
        const oldLineNums = group.map(([ol]) => ol)
        const oldText = oldLineNums.map(ol => oldLines[ol - 1] || '').join('\n')
        const newText = newLineNums.map(nl => newLines[nl - 1] || '').join('\n')
        markers.push(makeCodeMarker('modified', newLineNums, computeCharDiff(oldText, newText), oldContent, newContent))
    }

    // ── Pure added lines ──
    // Lines in addedInNew that are NOT in modifiedNewLineSet
    const pureAdded = lineDiff.addedInNew.filter(l => !modifiedNewLineSet.has(l))
    const sortedAdded = [...pureAdded].sort((a, b) => a - b)
    for (const group of groupConsecutive(sortedAdded)) {
        const newText = group.map(nl => newLines[nl - 1] || '').join('\n')
        const charDiff: CharDiff = {
            oldText: '',
            newText,
            changes: [{ value: newText, removed: false, added: true, count: 1 }],
        }
        markers.push(makeCodeMarker('added', group, charDiff, oldContent, newContent))
    }

    // ── Deleted lines ──
    // Anchored to the nearest surviving new-content line above the deletion.
    if (lineDiff.deletedInOld.length > 0) {
        const anchors = buildDeletedAnchors(lineDiff.deletedInOld, oldContent, newContent)
        // Group by anchor line
        const anchorGroups = new Map<number, number[]>()
        for (const { oldLine, anchorLine } of anchors) {
            if (!anchorGroups.has(anchorLine)) anchorGroups.set(anchorLine, [])
            anchorGroups.get(anchorLine)!.push(oldLine)
        }
        for (const [anchorLine, oldLineNums] of anchorGroups) {
            const oldText = oldLineNums.map(ol => oldLines[ol - 1] || '').join('\n')
            const charDiff: CharDiff = {
                oldText,
                newText: '',
                changes: [{ value: oldText, removed: true, added: false, count: 1 }],
            }
            markers.push(makeCodeMarker('deleted', [anchorLine], charDiff, oldContent, newContent))
        }
    }

    return markers
}

function makeCodeMarker(
    type: MarkerType,
    newLines: number[],
    charDiff: CharDiff | null,
    oldContent: string,
    newContent: string,
): DiffMarker {
    const startLine = newLines[0]
    const endLine = newLines[newLines.length - 1]
    return buildDiffMarker({
        id: `code-${type}-${startLine}-${endLine}`,
        type,
        lineNumbers: newLines,
        charDiff,
        diffLines: contentToDiffLinesWithCtx(oldContent, newContent, newLines),
        ariaLabel: newLines.length === 1
            ? `${type} line ${startLine}`
            : `${type} lines ${startLine}-${endLine}`,
    })
}

/**
 * For each pure-deleted old line, find the nearest surviving new line above it.
 * Walks diffArrays(old, new) to track old→new line correspondence.
 */
function buildDeletedAnchors(
    deletedInOld: number[],
    oldContent: string,
    newContent: string,
): Array<{ oldLine: number; anchorLine: number }> {
    const deletedSet = new Set(deletedInOld)
    const changes = diffArrays(oldContent.split('\n'), newContent.split('\n'))

    let oldLine = 1
    let newLine = 1
    let lastNewLine = 0  // last new line seen before current position

    const result: Array<{ oldLine: number; anchorLine: number }> = []

    for (const change of changes) {
        if (!change.added && !change.removed) {
            // Common lines
            oldLine += change.count
            lastNewLine = newLine + change.count - 1
            newLine += change.count
        } else if (change.removed) {
            for (let i = 0; i < change.count; i++) {
                if (deletedSet.has(oldLine + i)) {
                    result.push({ oldLine: oldLine + i, anchorLine: lastNewLine > 0 ? lastNewLine : 1 })
                }
            }
            oldLine += change.count
        } else if (change.added) {
            lastNewLine = newLine + change.count - 1
            newLine += change.count
        }
    }

    return result
}

/** Group consecutive numbers into arrays of consecutive sequences */
function groupConsecutive(nums: number[]): number[][] {
    if (nums.length === 0) return []
    const groups: number[][] = [[nums[0]]]
    for (let i = 1; i < nums.length; i++) {
        if (nums[i] === nums[i - 1] + 1) {
            groups[groups.length - 1].push(nums[i])
        } else {
            groups.push([nums[i]])
        }
    }
    return groups
}

/** Group consecutive pairs by their new-line numbers being adjacent */
function groupConsecutivePairs(pairs: [number, number][]): [number, number][][] {
    if (pairs.length === 0) return []
    const groups: [number, number][][] = [[pairs[0]]]
    for (let i = 1; i < pairs.length; i++) {
        if (pairs[i][1] === pairs[i - 1][1] + 1) {
            groups[groups.length - 1].push(pairs[i])
        } else {
            groups.push([pairs[i]])
        }
    }
    return groups
}

// ─── Off-screen rendering ───

const domParser = new DOMParser()

/**
 * Render markdown content off-screen and extract blocks.
 * Uses renderMarkdown + DOMParser (no Mermaid/KaTeX DOM rendering).
 * Simulates the mermaid transformation: <pre class="mermaid"> → <div class="mermaid">
 * so the block structure matches the live DOM.
 */
export function offscreenExtractBlocks(content: string): BlockInfo[] {
    const html = renderMarkdown(content, { sanitize: false })
    const doc = domParser.parseFromString(html, 'text/html')

    // Simulate mermaid transformation: <pre class="mermaid"> → <div class="mermaid" data-mermaid="source">
    const mermaidPres = doc.body.querySelectorAll('pre.mermaid')
    for (const pre of mermaidPres) {
        const container = doc.createElement('div')
        container.className = 'mermaid'
        const source = pre.textContent?.trim() || ''
        container.setAttribute('data-mermaid', source)
        pre.replaceWith(container)
    }

    return extractBlocks(doc.body)
}

// ─── Composable ───

/**
 * Reactive state for Markdown diff markers.
 */
export const diffMarkers = ref<DiffMarker[]>([])
export const diffDrawerVisible = ref(false)
export const diffDrawerMarker = shallowRef<DiffMarker | null>(null)
/** Full file content before changes (for undo) */
export const diffOldContent = ref<string | null>(null)
/** File path when diff was computed (for undo path validation) */
export const diffOldFilePath = ref<string | null>(null)

export function openDiffDrawer(marker: DiffMarker) {
    diffDrawerMarker.value = marker
    diffDrawerVisible.value = true
}

export function closeDiffDrawer() {
    diffDrawerVisible.value = false
    diffDrawerMarker.value = null
}

export function clearDiffMarkers() {
    diffMarkers.value = []
    diffOldContent.value = null
    diffOldFilePath.value = null
    closeDiffDrawer()
}
