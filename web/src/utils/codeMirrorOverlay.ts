/**
 * codeMirrorOverlay — pure helpers to build CodeMirror 6 decorations that mirror
 * the code browse-mode overlays (diff markers, char-level flash).
 * Kept framework-free so the logic is unit-testable.
 */
import { RangeSetBuilder } from '@codemirror/state'
import { Decoration, type DecorationSet } from '@codemirror/view'
import type { EditorState } from '@codemirror/state'
import type { DiffMarker } from '@/composables/useMarkdownDiff.ts'
import type { FlashRange, FlashType } from '@/composables/useFileRefresh.ts'

/** Decoration class applied to changed lines, per marker type. */
export function diffLineClass(type: string): string {
    return `cm-diff-line-${type}`
}

/** A diff marker whose first line carries a clickable gutter label. */
export interface DiffLineEntry {
    marker: DiffMarker
    /** 1-based first line of the marker group (anchor for the gutter label) */
    line: number
}

/**
 * Build line decorations for diff markers plus char-flash marks in a single
 * ordered decoration set.
 *
 * @returns { decorations, diffLines } diffLines maps anchor line → marker for
 *          the gutter marker + click handling.
 */
export function buildOverlayDecorations(
    state: EditorState,
    markers: DiffMarker[],
    flashRanges: FlashRange[],
    flashType: FlashType,
): { decorations: DecorationSet; diffLines: Map<number, DiffMarker> } {
    const diffLines = new Map<number, DiffMarker>()
    const flashClass = flashType === 'delete' ? 'char-flash-delete' : 'char-flash-add'

    // Collect adds, sorted by position (RangeSetBuilder requires ascending order).
    const lineAdds: Array<{ from: number; to: number; dec: Decoration }> = []
    const markAdds: Array<{ from: number; to: number; dec: Decoration }> = []

    for (const m of markers) {
        if (!m.lineNumbers || !m.lineNumbers.length) continue
        const anchor = Math.min(m.lineNumbers[0], state.doc.lines)
        diffLines.set(anchor, m)
        for (const ln of m.lineNumbers) {
            const line = state.doc.line(Math.min(ln, state.doc.lines))
            lineAdds.push({ from: line.from, to: line.from, dec: Decoration.line({ class: diffLineClass(m.type) }) })
        }
    }

    for (const r of flashRanges) {
        const line = state.doc.line(Math.min(r.line, state.doc.lines))
        const from = line.from + Math.min(Math.max(r.start, 0), line.length)
        const to = r.end === Infinity ? line.to : line.from + Math.min(Math.max(r.end, 0), line.length)
        if (from < to) {
            markAdds.push({ from, to, dec: Decoration.mark({ class: flashClass }) })
        }
    }

    lineAdds.sort((a, b) => a.from - b.from)
    markAdds.sort((a, b) => a.from - b.from || a.to - b.to)

    const builder = new RangeSetBuilder<Decoration>()
    let i = 0
    let j = 0
    while (i < lineAdds.length || j < markAdds.length) {
        const l = lineAdds[i]
        const m = markAdds[j]
        if (!m || (l && l.from <= m.from)) {
            builder.add(l.from, l.to, l.dec)
            i++
        } else {
            builder.add(m.from, m.to, m.dec)
            j++
        }
    }

    return { decorations: builder.finish(), diffLines }
}

