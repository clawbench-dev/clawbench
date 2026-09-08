// Scroll-anchor helpers for keeping the markdown preview (rendered) and
// edit/raw (CodeMirror) panes aligned when toggling between them.
//
// The anchor is a single 1-based source line number: rendered blocks carry
// data-source-line (see markedConfig), CodeMirror has real line numbers, so a
// line is the shared coordinate between the two panes.

export interface ScrollAnchorState {
    /** 1-based source line the viewport should align to. */
    line: number
}

/** Clamp a target scrollTop into the container's scrollable range. */
export function clampScrollTop(
    container: { scrollHeight: number; clientHeight: number },
    target: number,
): number {
    const max = container.scrollHeight - container.clientHeight
    if (max <= 0) return 0
    return Math.min(max, Math.max(0, Math.round(target)))
}

/**
 * Binary-search an ascending list of line-annotated blocks for the last block
 * whose line is <= target (i.e. the block that owns a given source line).
 * Returns the matching index, or 0 when every block is after target, or -1 when
 * the list is empty.
 */
export function findBlockAtOrBefore(blocks: { line: number }[], target: number): number {
    if (blocks.length === 0) return -1
    let lo = 0
    let hi = blocks.length - 1
    let ans = 0
    while (lo <= hi) {
        const mid = (lo + hi) >> 1
        if (blocks[mid].line <= target) {
            ans = mid
            lo = mid + 1
        } else {
            hi = mid - 1
        }
    }
    return ans
}
