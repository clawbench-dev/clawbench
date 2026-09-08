// DOM helpers for line-anchored scrolling inside a rendered markdown pane.
//
// Rendered markdown blocks carry a `data-source-line` attribute (1-based source
// line, emitted by markedConfig). These helpers map between the container's
// scroll position and that line number — the coordinate shared with the
// CodeMirror source view.

import { clampScrollTop, findBlockAtOrBefore } from '@/utils/markdownScroll'

interface LineBlock {
    el: HTMLElement
    line: number
    /** Vertical position of the block top within the container's scroll area. */
    top: number
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
 * Capture: the source line anchored at the container's current viewport top.
 * Returns the line of the last block whose top is at/above the viewport top
 * (with a small tolerance), or null when the pane has no line blocks yet.
 */
export function renderedTopLine(container: HTMLElement): number | null {
    const blocks = collectLineBlocks(container)
    if (blocks.length === 0) return null
    const scrollTop = container.scrollTop
    let idx = 0
    for (let i = 0; i < blocks.length; i++) {
        if (blocks[i].top <= scrollTop + 2) idx = i
        else break // blocks are in document order — stop past the viewport top
    }
    return blocks[idx].line
}

/**
 * Restore: scroll the container so the block owning `line` sits at the top.
 * Falls back to the first block when `line` is before the first annotation.
 * Returns false when the pane has no line blocks (caller falls back to ratio).
 */
export function scrollRenderedToLine(container: HTMLElement, line: number): boolean {
    const target = renderedLineScrollTop(container, line)
    if (target == null) return false
    container.scrollTop = clampScrollTop(container, target)
    return true
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
