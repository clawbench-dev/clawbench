// Scroll-anchor helpers for keeping markdown preview (rendered) and
// edit/raw (source) panes positionally aligned when toggling between them.
//
// Primary strategy: align to the current heading (TOC entry). Fallback:
// percentage ratio. These helpers are pure (operate on content coordinates,
// not DOM) so they are unit-testable without layout.

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
