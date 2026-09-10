// Scroll-anchor helpers for keeping the markdown preview (rendered) and
// edit/raw (CodeMirror) panes aligned when toggling between them.
//
// Two anchor coordinate systems coexist here:
//  - Source-line anchor (`ScrollAnchorState`): a single 1-based source line.
//    Rendered blocks carry data-source-line (see markedConfig); CodeMirror has
//    real line numbers — so a line is the shared coordinate. Preferred when
//    available (works without any TOC headings).
//  - Heading anchor (`ScrollAnchor`): the current TOC heading + its viewport
//    offset. Used as the fallback when no source-line anchor resolves.

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

// ─── Heading anchor (TOC) — fallback coordinate ──────────────────────────────

export interface ScrollAnchor {
    id: string
    line: number
    /** heading content-top minus scrollTop (negative when scrolled past it) */
    relTop: number
}

/**
 * Pick the heading anchoring the current viewport from an ordered list of
 * headings (by document position). Returns the last heading whose content-top
 * sits at or above the viewport top (within `margin` px), i.e. the heading of
 * the section currently on screen.
 */
export function pickPreviewAnchor(
    headings: { id: string; line: number; contentTop: number }[],
    scrollTop: number,
    margin = 4,
): ScrollAnchor | null {
    let anchor: ScrollAnchor | null = null
    for (const h of headings) {
        if (h.contentTop <= scrollTop + margin) {
            anchor = { id: h.id, line: h.line, relTop: h.contentTop - scrollTop }
        } else {
            break // headings are in document order — once above viewport, stop
        }
    }
    return anchor
}

/**
 * Pick the last TOC item at or above the top visible source line. TOC items
 * must be ordered by line number ascending.
 */
export function pickCmAnchor(
    toc: { id: string; line: number }[],
    topLine: number,
): { id: string; line: number } | null {
    let anchor: { id: string; line: number } | null = null
    for (const item of toc) {
        if (item.line <= topLine) {
            anchor = item
        } else {
            break
        }
    }
    return anchor
}

/** Vertical offset of a heading relative to the viewport top. */
export function relTopFor(contentTop: number, scrollTop: number): number {
    return contentTop - scrollTop
}

/** ScrollTop that places a heading at the same viewport offset (`relTop`). */
export function scrollTopFor(contentTop: number, relTop: number): number {
    return Math.max(0, contentTop - relTop)
}
