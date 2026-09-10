// DOM helpers for line-anchored scrolling inside a rendered markdown pane.
//
// Rendered markdown blocks carry a `data-source-line` attribute (1-based source
// line, emitted by markedConfig). These helpers map between the container's
// scroll position and that line number — the coordinate shared with the
// CodeMirror source view.

interface LineBlock {
    el: HTMLElement
    line: number
    /** Vertical position of the block top within the container's scroll area. */
    top: number
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

/**
 * Collect every `[data-source-line]` block inside the rendered markdown
 * container, in document order, with its top measured relative to the
 * container's scrollable content (rect difference + current scrollTop, which
 * cancels padding/borders).
 */
export function collectLineBlocks(container: HTMLElement): LineBlock[] {
    const blocks: LineBlock[] = []
    const root = container.querySelector('.markdown-content') ?? container
    const cRect = container.getBoundingClientRect()
    const cScroll = container.scrollTop
    for (const el of root.querySelectorAll<HTMLElement>('[data-source-line]')) {
        const line = parseInt(el.getAttribute('data-source-line') || '', 10)
        if (!Number.isFinite(line) || line <= 0) continue
        const top = el.getBoundingClientRect().top - cRect.top + cScroll
        blocks.push({ el, line, top })
    }
    return blocks
}

/**
 * Capture: the source line anchored at the container's current viewport top,
 * plus the pixel offset of the viewport top below that block's top (so a
 * rendered→rendered restore can land inside a tall block). Returns null when
 * the pane has no line blocks yet.
 */
export function renderedLineAnchor(container: HTMLElement): { line: number; offset: number } | null {
    const blocks = collectLineBlocks(container)
    if (blocks.length === 0) return null
    const scrollTop = container.scrollTop
    let idx = 0
    for (let i = 0; i < blocks.length; i++) {
        if (blocks[i].top <= scrollTop + 2) idx = i
        else break // blocks are in document order — stop past the viewport top
    }
    return { line: blocks[idx].line, offset: Math.max(0, scrollTop - blocks[idx].top) }
}

/**
 * The ideal scrollTop that places the block owning `line` at the container
 * top — NOT clamped to the current scroll range. Returns null when the pane
 * has no line blocks. Callers use this to defer a restore until the content
 * is tall enough (async images), instead of clamping to a wrong position.
 */
export function renderedLineScrollTop(container: HTMLElement, line: number): number | null {
    const blocks = collectLineBlocks(container)
    if (blocks.length === 0) return null
    const idx = findBlockAtOrBefore(blocks, line)
    return blocks[idx].top
}
