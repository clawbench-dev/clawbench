/**
 * useFileScrollRestore — scroll-position save/restore for the file viewer.
 *
 * Owns every aspect of keeping the user's place when files/views switch:
 *
 *  - Cross-file / reopen restore uses pixel scrollTop, persisted per path in the
 *    module-level fileScrollCache (so it survives FileViewer unmount/remount).
 *  - rendered↔raw / edit-toggle restore uses { anchor, ratio } because the two
 *    panes have different heights and must align on content coordinates.
 *  - One shared 50ms poll drives both kinds of pending restore (previously two
 *    separate intervals in FileViewer).
 *
 * Guards that prevent losing the position (the historic bugs):
 *  - A hidden container (view panel left via v-show, e.g. switching to the file
 *    manager) has its CodeMirror scrollTop reset to 0 by display:none. We never
 *    write a scroll offset read from a hidden container (offsetParent === null).
 *  - Content renders asynchronously (CodeMirror viewport, markdown images), so
 *    the scroll container height grows over ticks. A target beyond the current
 *    max scroll is deferred until the content can actually hold it, otherwise
 *    the browser clamps it and it is never corrected.
 *
 * Instance state (per FileViewer) — the composable is NOT a singleton; only the
 * fileScrollCache it writes into is shared at module scope.
 */

import { extractToc } from '@/utils/toc'
import { EditorView } from '@codemirror/view'
import {
    pickPreviewAnchor,
    pickCmAnchor,
    relTopFor,
    scrollTopFor,
} from '@/utils/markdownScroll'
import {
    getFileScroll,
    getFileScrollEntry,
    setFileScroll,
    type FileScrollEntry,
    type ScrollAnchorState,
    type BlockAnchorState,
} from '@/utils/fileScrollCache'

export type { ScrollAnchorState, BlockAnchorState, FileScrollEntry } from '@/utils/fileScrollCache'

/** Structural element shape — tests inject plain objects, no real DOM needed. */
export interface ScrollContainerLike {
    scrollHeight: number
    clientHeight: number
    scrollTop: number
    offsetParent: Element | null
    classList?: { contains(c: string): boolean }
    addEventListener?: (t: string, fn: (e: Event) => void, o?: unknown) => void
    removeEventListener?: (t: string, fn: (e: Event) => void) => void
    querySelectorAll?: (s: string) => ArrayLike<Element>
    querySelector?: (s: string) => Element | null
    getBoundingClientRect?: () => DOMRect
}

export interface SavedScroll {
    scrollTop?: number
    anchor?: ScrollAnchorState | null
    blockAnchor?: BlockAnchorState | null
    ratio?: { ratio: number } | null
    /**
     * Restore by pixel offset first, treating the anchor as a fallback.
     *
     * Anchors exist so a rendered↔raw swap can align two panes of different
     * heights. Returning to a file in the *same* view mode needs no such
     * translation — the pixel offset is the exact place — and preferring a
     * possibly stale anchor there is what made back-navigation jump to an
     * unrelated heading.
     */
    preferPx?: boolean
}

export interface FileScrollContext {
    contentRoot: () => HTMLElement | null // the viewer's content element (contentRef.value)
    file: () => { path: string; content?: string | null; isExcalidraw?: boolean } | null
    markdownViewMode: () => string | undefined // 'rendered' | 'raw'
    editing: () => boolean
    loading: () => boolean
    isMarkdown: () => boolean
    isHtml: () => boolean
    isOpenapi: () => boolean
}

export interface UseFileScrollRestore {
    /** Register the window cancel-scroll-restore listener (component mounted). */
    start(): void
    /** Save current position if visible, detach, stop polling, drop listeners. */
    dispose(): void
    /** Resolve the scroll container for an explicit view mode + edit state. */
    scrollElFor(viewMode: string | undefined, isEditing: boolean): HTMLElement | null
    /** Resolve the scroll container for the current view mode + edit state. */
    currentScrollEl(): HTMLElement | null
    /** File switch is about to happen: save the outgoing file's position. */
    onFileWillChange(): void
    /** A file was selected (or cleared). Start restore when the path changed. */
    onFileChanged(file: { path: string } | null, pathChanged: boolean): void
    /** Content finished loading — nudge the restore poll once. */
    onContentReady(): void
    /** Capture { anchor, ratio } so a pane swap can restore the same place. */
    captureScroll(el: HTMLElement | null): SavedScroll | null
    /** After a rendered↔raw / edit toggle, restore the captured anchor/ratio. */
    restoreAfterContainerSwitch(saved: SavedScroll | null): void
    /** Cancel a pending pixel restore (scroll-to-line takes precedence). */
    cancelPendingRestore(): void
    /** Re-align to the active anchor if layout shifted (e.g. after Mermaid or image load). */
    realignAnchor(): void
}

const POLL_MS = 50
const MAX_PX_ATTEMPTS = 100 // 100 * 50ms = 5s
const MAX_ANCHOR_ATTEMPTS = 60 // 60 * 50ms = 3s
/**
 * Delay after the last scroll event before the cached anchor is recomputed.
 *
 * Scroll events only carry a pixel offset, so a naive handler leaves the
 * cached anchor pointing at whatever heading was current when the file was
 * opened. Since anchors outrank pixels in restoreScroll(), returning to a file
 * later would snap back to that stale heading instead of where the user
 * actually stopped reading. Recomputing the whole snapshot once scrolling
 * settles keeps pixels and anchors describing the same place.
 */
const SNAPSHOT_DEBOUNCE_MS = 120
const CANCEL_EVENT = 'cancel-scroll-restore'
const RESTORE_EVENT = 'restore-file-scroll'
const REALIGN_EVENT = 'realign-file-scroll'

/** CSS.escape with a fallback for environments without it (jsdom in tests). */
function escapeCssIdentifier(id: string): string {
    if (typeof CSS !== 'undefined' && CSS.escape) return CSS.escape(id)
    return id.replace(/[^a-zA-Z0-9_-]/g, (c) => `\\${c}`)
}

// Pure decision helpers — unit-testable without DOM.

export function isScrollable(el: { scrollHeight: number; clientHeight: number }): boolean {
    return el.scrollHeight > el.clientHeight
}

export function isVisiblyAttached(el: { offsetParent: Element | null }): boolean {
    return el.offsetParent !== null
}

export function pxCanApply(
    el: { scrollHeight: number; clientHeight: number },
    target: number,
): boolean {
    return target <= el.scrollHeight - el.clientHeight
}

export function getElementContentTop(target: Element, container: HTMLElement): number {
    let top = 0
    let curr: HTMLElement | null = target as HTMLElement
    let reached = false
    while (curr && curr !== container) {
        if (typeof curr.offsetTop === 'number') {
            top += curr.offsetTop
            reached = true
        }
        curr = curr.offsetParent as HTMLElement | null
        if (curr === container) {
            reached = true
            break
        }
    }
    if (reached && curr === container) return top

    const tRect = (target as HTMLElement).getBoundingClientRect?.()
    const cRect = (container as HTMLElement).getBoundingClientRect?.()
    if (tRect && cRect) {
        // The rect diff already excludes the container's scroll offset, so add
        // it back: contentTop is where the target sits in the unscrolled
        // content (diff + scrollTop, regardless of the sign of diff).
        return tRect.top - cRect.top + (container.scrollTop || 0)
    }
    return 0
}

export function captureBlockAnchor(el: HTMLElement): BlockAnchorState | null {
    const root = (el.querySelector?.('.markdown-content') || el) as HTMLElement
    if (!root?.children) return null
    const children = Array.from(root.children) as HTMLElement[]
    if (children.length === 0) return null

    let bestBlock: HTMLElement | null = null
    let bestIndex = -1
    let bestContentTop = 0

    for (let i = 0; i < children.length; i++) {
        const child = children[i]
        const top = getElementContentTop(child, el)
        if (top <= el.scrollTop + 4) {
            bestBlock = child
            bestIndex = i
            bestContentTop = top
        } else {
            break
        }
    }

    if (!bestBlock) {
        bestBlock = children[0]
        bestIndex = 0
        bestContentTop = getElementContentTop(bestBlock, el)
    }

    const relTop = bestContentTop - el.scrollTop
    const id = bestBlock.id ? `#${escapeCssIdentifier(bestBlock.id)}` : undefined
    const selector = id || `:scope > :nth-child(${bestIndex + 1})`

    return {
        selector,
        index: bestIndex,
        tag: bestBlock.tagName ? bestBlock.tagName.toLowerCase() : 'div',
        relTop,
    }
}

export function restoreBlockAnchor(el: HTMLElement, blockAnchor: BlockAnchorState): boolean {
    const root = (el.querySelector?.('.markdown-content') || el) as HTMLElement
    if (!root) return false
    let target: HTMLElement | null = null

    if (blockAnchor.selector && !blockAnchor.selector.startsWith(':scope')) {
        target = root.querySelector?.(blockAnchor.selector) as HTMLElement | null
    }
    if (!target && typeof blockAnchor.index === 'number' && root.children) {
        const children = root.children
        if (blockAnchor.index >= 0 && blockAnchor.index < children.length) {
            target = children[blockAnchor.index] as HTMLElement
        }
    }
    if (!target) return false

    const contentTop = getElementContentTop(target, el)
    el.scrollTop = scrollTopFor(contentTop, blockAnchor.relTop)
    return true
}

export function captureMarkdownScroll(el: HTMLElement, content: string): FileScrollEntry {
    const max = el.scrollHeight - el.clientHeight
    const ratio = max > 0 ? { ratio: el.scrollTop / max } : null
    let anchor: ScrollAnchorState | null = null
    const toc = extractToc(content, 'markdown')
    if (toc.length > 0 && el.querySelectorAll) {
        const idToMeta = new Map(toc.map((i) => [i.id, i]))
        const headings: { id: string; line: number; contentTop: number }[] = []
        for (const h of el.querySelectorAll('h1, h2, h3, h4, h5, h6')) {
            const id = h.id
            const meta = id ? idToMeta.get(id) : undefined
            if (!meta) continue
            const contentTop = getElementContentTop(h, el)
            headings.push({ id, line: meta.line, contentTop })
        }
        anchor = pickPreviewAnchor(headings, el.scrollTop)
    }
    const blockAnchor = captureBlockAnchor(el)
    return {
        scrollTop: el.scrollTop,
        anchor,
        blockAnchor,
        ratio,
    }
}

export function useFileScrollRestore(ctx: FileScrollContext): UseFileScrollRestore {
    let attachedEl: HTMLElement | null = null
    let attachedPath: string | null = null
    let scrollHandler: ((e: Event) => void) | null = null
    let snapshotTimer: ReturnType<typeof setTimeout> | null = null
    let currentPath: string | null = null
    let pollTimer: ReturnType<typeof setInterval> | null = null
    let pendingPx: { path: string; scrollTop: number; attempts: number; entry?: SavedScroll | null } | null = null
    let pendingAnchor: { saved: SavedScroll; attempts: number } | null = null
    let activeAnchor: ScrollAnchorState | null = null
    let activeBlockAnchor: BlockAnchorState | null = null
    let stabilizeArmedPx: number | null = null

    function disarmStabilizer(): void {
        activeAnchor = null
        activeBlockAnchor = null
        stabilizeArmedPx = null
    }

    /**
     * Remember the anchor a restore just landed on so late layout shifts can be
     * corrected (see realignAnchor).
     *
     * Stays armed until one of the three things that mean "the restore's
     * position is no longer where the user is":
     *  - the user scrolls (scroll handler, below);
     *  - the file changes (onFileWillChange / onFileChanged / dispose);
     *  - something else takes over positioning — every explicit jump
     *    (scroll-to-line, TOC, code link) dispatches 'cancel-scroll-restore'.
     *
     * There is deliberately no timeout: images and Mermaid diagrams can finish
     * long after the restore, and a time limit would silently drop the
     * correction exactly when it is still needed.
     */
    function armStabilizer(anchor: ScrollAnchorState | null, blockAnchor: BlockAnchorState | null, el: HTMLElement): void {
        activeAnchor = anchor
        activeBlockAnchor = blockAnchor
        // The programmatic scroll that positions the anchor fires a scroll
        // event asynchronously. Remember the applied offset so that echo is
        // not mistaken for user scrolling (which must disarm the stabilizer).
        stabilizeArmedPx = el.scrollTop
    }

    /**
     * Arm the anchor after a *pixel* restore, so a late layout shift can still
     * be corrected.
     *
     * Pixels and anchors describe the same place only while the layout is
     * stable. A markdown pane that is still growing (images, Mermaid) shifts
     * the content under a pixel offset, so the anchor has to be able to pull
     * the position back. Restores that land through the poll (content not tall
     * enough yet) are the ones most likely to be followed by late loads, so
     * they arm too.
     */
    function armStabilizerForEntry(el: HTMLElement, entry?: SavedScroll | null): void {
        if (!entry) return
        if (!(entry.anchor || entry.blockAnchor)) return
        if (!ctx.isMarkdown() || !el.classList?.contains('markdown-body')) return
        armStabilizer(entry.anchor || null, entry.blockAnchor || null, el)
    }

    function realignAnchor(): void {
        if (!activeAnchor && !activeBlockAnchor) return
        const el = currentScrollEl()
        if (!el || !isScrollable(el)) return
        if (!ctx.isMarkdown() || !el.classList?.contains('markdown-body')) return

        // Heading anchor first; the block anchor is the fallback for markdown
        // without TOC headings.
        //
        // A correction moves scrollTop, and that move comes back through the
        // scroll handler as an echo. Re-arm on the new offset, otherwise the
        // echo reads as "the user scrolled" and disarms the stabilizer — the
        // first late image would then be the last one ever corrected, which is
        // exactly what dropping the timeout was meant to avoid.
        if (activeAnchor && restorePreviewAnchor(el, activeAnchor)) {
            stabilizeArmedPx = el.scrollTop
            return
        }
        if (activeBlockAnchor && restoreBlockAnchor(el, activeBlockAnchor)) {
            stabilizeArmedPx = el.scrollTop
        }
    }

    // ── container resolution ──────────────────────────────────────────────

    function scrollElFor(viewMode: string | undefined, isEditing: boolean): HTMLElement | null {
        const el = ctx.contentRoot()
        if (!el) return null
        // Edit mode always renders the CodeMirror editor
        if (isEditing) return el.querySelector('.cm-scroller')
        if (ctx.isMarkdown()) {
            // Rendered markdown scrolls in .markdown-body; source view uses CM
            return viewMode === 'rendered'
                ? el.querySelector('.markdown-body')
                : el.querySelector('.cm-scroller')
        }
        if (ctx.isHtml() && viewMode === 'rendered') return null // iframe scrolls itself
        if (ctx.isOpenapi() && viewMode === 'rendered') return null // ReDoc iframe scrolls itself
        if (ctx.file()?.isExcalidraw) return null // Excalidraw iframe scrolls itself
        // CodeMirror-based viewers scroll inside .cm-scroller
        return el.querySelector('.cm-scroller')
    }

    function currentScrollEl(): HTMLElement | null {
        return scrollElFor(ctx.markdownViewMode(), ctx.editing())
    }

    // ── scroll listener (writes px positions to the cache) ───────────────

    /** Write a full { scrollTop, anchor, blockAnchor, ratio } snapshot now. */
    function refreshSnapshot(): void {
        if (!currentPath || !attachedEl || !isVisiblyAttached(attachedEl)) return
        const saved = captureScroll(attachedEl)
        if (!saved) return
        setFileScroll(currentPath, {
            scrollTop: attachedEl.scrollTop,
            anchor: saved.anchor,
            blockAnchor: saved.blockAnchor,
            ratio: saved.ratio,
        })
    }

    function cancelSnapshotRefresh(): void {
        if (snapshotTimer) {
            clearTimeout(snapshotTimer)
            snapshotTimer = null
        }
    }

    function scheduleSnapshotRefresh(): void {
        if (snapshotTimer) clearTimeout(snapshotTimer)
        snapshotTimer = setTimeout(() => {
            snapshotTimer = null
            refreshSnapshot()
        }, SNAPSHOT_DEBOUNCE_MS)
    }

    function attachScrollListener(): void {
        const el = currentScrollEl()
        if (!el || !currentPath) return
        // Idempotent on purpose: the poll calls this on every tick, and a
        // blind re-attach would tear down the pending anchor refresh together
        // with the previous listener, so the debounce would never fire.
        if (attachedEl === el && scrollHandler && attachedPath === currentPath) return
        detachScrollListener()
        attachedEl = el
        attachedPath = currentPath
        scrollHandler = () => {
            // Ignore events fired while hidden: display:none resets CodeMirror's
            // scrollTop to 0 and later re-measuring can fire spurious scroll
            // events — writing 0 would overwrite the trusted position.
            if (el.offsetParent === null) return
            const existing = getFileScrollEntry(currentPath!)
            // Replace the entry wholesale instead of mutating it: the cached
            // object is shared with the navigation history / origin, and an
            // in-place scrollTop write would retroactively move snapshots
            // those visitors banked earlier.
            if (existing) {
                setFileScroll(currentPath!, {
                    scrollTop: el.scrollTop,
                    anchor: existing.anchor,
                    blockAnchor: existing.blockAnchor,
                    ratio: existing.ratio,
                })
            } else {
                setFileScroll(currentPath!, el.scrollTop)
            }
            // The anchor restore's own programmatic scroll arrives here as an
            // echo carrying the armed offset — keep the realign window open for
            // that one; only a real user scroll (different offset) disarms it.
            if (stabilizeArmedPx === null || Math.abs(el.scrollTop - stabilizeArmedPx) > 1) {
                disarmStabilizer()
            }
            // Pixels are authoritative right away; the anchor catches up once
            // the fling settles so the cache never holds a stale heading.
            scheduleSnapshotRefresh()
        }
        el.addEventListener('scroll', scrollHandler, { passive: true })
    }

    function detachScrollListener(): void {
        cancelSnapshotRefresh()
        if (scrollHandler && attachedEl) {
            attachedEl.removeEventListener('scroll', scrollHandler)
        }
        scrollHandler = null
        attachedEl = null
        attachedPath = null
    }

    // ── shared poll ───────────────────────────────────────────────────────

    function stopPoll(): void {
        if (pollTimer) {
            clearInterval(pollTimer)
            pollTimer = null
        }
    }

    function startPoll(): void {
        if (!pollTimer) pollTimer = setInterval(tick, POLL_MS)
    }

    function tick(): void {
        const el = currentScrollEl()

        // Anchor/ratio restore (rendered↔raw / edit toggle / cross-file anchor).
        if (pendingAnchor) {
            if (++pendingAnchor.attempts > MAX_ANCHOR_ATTEMPTS) {
                if (el && isScrollable(el)) {
                    restoreScroll(pendingAnchor.saved, el, true)
                }
                pendingAnchor = null
            } else if (el && isScrollable(el)) {
                const ok = restoreScroll(pendingAnchor.saved, el, false)
                if (ok) {
                    pendingAnchor = null
                    pendingPx = null
                }
            }
        }

        // Pixel restore (cross-file / reopen). Requires content loaded and the
        // container tall enough to actually hold the target (else deferred).
        if (pendingPx) {
            if (++pendingPx.attempts > MAX_PX_ATTEMPTS) {
                // Content never grew enough to hold the offset (file shrunk?).
                // Fall back to the anchor rather than dropping the user at 0.
                if (pendingPx.entry && el && isScrollable(el)) {
                    restoreScroll(pendingPx.entry, el, true)
                }
                pendingPx = null
                attachScrollListener()
            } else if (!ctx.loading() && el && isScrollable(el)) {
                if (pxCanApply(el, pendingPx.scrollTop)) {
                    el.scrollTop = pendingPx.scrollTop
                    armStabilizerForEntry(el, pendingPx.entry)
                    pendingPx = null
                }
            }
        }

        // Attach the scroll listener once content is ready (idempotent).
        if (el && !ctx.loading() && isScrollable(el)) {
            attachScrollListener()
        }
        if (!pendingPx && !pendingAnchor) stopPoll()
    }

    function kick(): void {
        tick()
        if (pendingPx || pendingAnchor) startPoll()
    }

    // ── anchor/ratio capture & restore ────────────────────────────────────

    function captureCmAnchor(el: HTMLElement): ScrollAnchorState | null {
        const view = el instanceof Element ? EditorView.findFromDOM(el) : null
        if (!view) return null
        const content = ctx.file()?.content || ''
        const toc = extractToc(content, 'markdown')
        if (toc.length === 0) return null
        let topBlock
        try {
            topBlock = view.lineBlockAtHeight(el.scrollTop + 1)
        } catch {
            return null
        }
        const topLine = view.state.doc.lineAt(topBlock.from).number
        const item = pickCmAnchor(toc, topLine)
        if (!item) return null
        const line = view.state.doc.line(Math.min(Math.max(1, item.line), view.state.doc.lines))
        let block
        try {
            block = view.lineBlockAt(line.from)
        } catch {
            return null
        }
        return { id: item.id, line: item.line, relTop: relTopFor(block.top, el.scrollTop) }
    }

    function captureScroll(el: HTMLElement | null): SavedScroll | null {
        if (!el) return null
        if (ctx.isMarkdown() && el.classList?.contains('markdown-body')) {
            return captureMarkdownScroll(el, ctx.file()?.content || '')
        }
        const max = el.scrollHeight - el.clientHeight
        const ratio = max > 0 ? { ratio: el.scrollTop / max } : null
        let anchor: ScrollAnchorState | null = null
        if (ctx.isMarkdown() && el.classList?.contains('cm-scroller')) {
            anchor = captureCmAnchor(el)
        }
        return { scrollTop: el.scrollTop, anchor, ratio }
    }

    function restorePreviewAnchor(el: HTMLElement, anchor: ScrollAnchorState): boolean {
        const sel = `#${escapeCssIdentifier(anchor.id)}`
        const target = el.querySelector?.(sel)
        if (!target) return false
        const contentTop = getElementContentTop(target, el)
        el.scrollTop = scrollTopFor(contentTop, anchor.relTop)
        return true
    }

    function restoreCmAnchor(el: HTMLElement, anchor: ScrollAnchorState): boolean {
        const view = el instanceof Element ? EditorView.findFromDOM(el) : null
        if (!view) return false
        const line = view.state.doc.line(Math.min(Math.max(1, anchor.line), view.state.doc.lines))
        let block
        try {
            block = view.lineBlockAt(line.from)
        } catch {
            return false
        }
        el.scrollTop = scrollTopFor(block.top, anchor.relTop)
        return true
    }

    function restoreScroll(saved: SavedScroll, el: HTMLElement, allowFallback = true): boolean {
        // 0. Pixel restore when the caller knows the offset is directly
        //    comparable (same file, same view mode).
        if (saved.preferPx && typeof saved.scrollTop === 'number' && pxCanApply(el, saved.scrollTop)) {
            el.scrollTop = saved.scrollTop
            if (ctx.isMarkdown() && el.classList?.contains('markdown-body') && (saved.anchor || saved.blockAnchor)) {
                armStabilizer(saved.anchor || null, saved.blockAnchor || null, el)
            }
            return true
        }
        // 1. TOC heading alignment first
        if (saved.anchor && ctx.isMarkdown()) {
            if (el.classList?.contains('markdown-body') && restorePreviewAnchor(el, saved.anchor)) {
                armStabilizer(saved.anchor, saved.blockAnchor || null, el)
                return true
            }
            if (el.classList?.contains('cm-scroller') && restoreCmAnchor(el, saved.anchor)) {
                return true
            }
        }
        // 2. Block anchor alignment
        if (saved.blockAnchor && ctx.isMarkdown() && el.classList?.contains('markdown-body')) {
            if (restoreBlockAnchor(el, saved.blockAnchor)) {
                armStabilizer(saved.anchor || null, saved.blockAnchor, el)
                return true
            }
        }
        // If an anchor was provided but not found yet, don't fall back until timeout
        const hasAnchor = !!(saved.anchor || saved.blockAnchor)
        if (hasAnchor && !allowFallback) return false

        // 3. Pixel restore
        if (typeof saved.scrollTop === 'number' && pxCanApply(el, saved.scrollTop)) {
            el.scrollTop = saved.scrollTop
            return true
        }
        // 4. Percentage ratio as fallback
        if (saved.ratio) {
            const max = el.scrollHeight - el.clientHeight
            if (max > 0) {
                el.scrollTop = Math.round(saved.ratio.ratio * max)
                return true
            }
        }
        return false
    }

    // ── window cancel-scroll-restore (scroll-to-line takes precedence) ────

    function handleCancelScrollRestore(): void {
        cancelPendingRestore()
    }

    function handleRestoreFileScroll(e: Event): void {
        const ce = e as CustomEvent<{ scrollTop?: number; scrollEntry?: FileScrollEntry; preferPx?: boolean }>
        const entry = ce?.detail?.scrollEntry
        const preferPx = ce?.detail?.preferPx === true
        const target = typeof ce?.detail?.scrollTop === 'number' ? ce.detail.scrollTop : entry?.scrollTop

        // Same view mode: the pixel offset is the exact place, so lead with it
        // and keep the anchor purely as a fallback for when the content can no
        // longer hold that offset.
        if (preferPx && typeof target === 'number') {
            const el = currentScrollEl()
            if (el && isScrollable(el) && pxCanApply(el, target)) {
                el.scrollTop = target
                if (ctx.isMarkdown() && el.classList?.contains('markdown-body') && (entry?.anchor || entry?.blockAnchor)) {
                    armStabilizer(entry.anchor || null, entry.blockAnchor || null, el)
                }
                return
            }
            pendingAnchor = null
            pendingPx = { path: currentPath || '', scrollTop: target, attempts: 0, entry: entry ?? null }
            startPoll()
            kick()
            return
        }

        if (entry && (entry.anchor || entry.blockAnchor)) {
            pendingAnchor = { saved: entry, attempts: 0 }
            pendingPx = typeof target === 'number' ? { path: currentPath || '', scrollTop: target, attempts: 0, entry } : null
            startPoll()
            kick()
            return
        }
        if (typeof target === 'number') {
            const el = currentScrollEl()
            if (el && isScrollable(el) && pxCanApply(el, target)) {
                el.scrollTop = target
            } else {
                pendingPx = { path: currentPath || '', scrollTop: target, attempts: 0 }
                startPoll()
                kick()
            }
        }
    }

    function cancelPendingRestore(): void {
        pendingPx = null
        pendingAnchor = null
        disarmStabilizer()
    }

    // ── public orchestration ──────────────────────────────────────────────

    function start(): void {
        window.addEventListener(CANCEL_EVENT, handleCancelScrollRestore)
        window.addEventListener(RESTORE_EVENT, handleRestoreFileScroll)
        window.addEventListener(REALIGN_EVENT, realignAnchor)
    }

    function dispose(): void {
        // Persist the current position before the viewer is torn down (overlay
        // v-if removal does not run the props.file watcher). Skip when hidden —
        // its scrollTop has been reset to 0 and must not overwrite the cache.
        if (currentPath && attachedEl && isVisiblyAttached(attachedEl)) {
            const saved = captureScroll(attachedEl)
            if (saved) {
                setFileScroll(currentPath, {
                    scrollTop: attachedEl.scrollTop,
                    anchor: saved.anchor,
                    blockAnchor: saved.blockAnchor,
                    ratio: saved.ratio,
                })
            } else {
                setFileScroll(currentPath, attachedEl.scrollTop)
            }
        }
        detachScrollListener()
        stopPoll()
        disarmStabilizer()
        window.removeEventListener(CANCEL_EVENT, handleCancelScrollRestore)
        window.removeEventListener(RESTORE_EVENT, handleRestoreFileScroll)
        window.removeEventListener(REALIGN_EVENT, realignAnchor)
    }

    function onFileWillChange(): void {
        // Save the pane being left synchronously. Relying only on scroll events
        // can miss the final position when navigation follows a smooth scroll.
        if (currentPath && attachedEl && isVisiblyAttached(attachedEl)) {
            const saved = captureScroll(attachedEl)
            if (saved) {
                setFileScroll(currentPath, {
                    scrollTop: attachedEl.scrollTop,
                    anchor: saved.anchor,
                    blockAnchor: saved.blockAnchor,
                    ratio: saved.ratio,
                })
            } else {
                setFileScroll(currentPath, attachedEl.scrollTop)
            }
        }
        detachScrollListener()
        stopPoll()
        disarmStabilizer()
        // Do NOT clear pendingPx here: a same-path content refresh relies on the
        // content watcher (onContentReady) to re-kick the restore.
    }

    function onFileChanged(file: { path: string } | null, pathChanged: boolean): void {
        if (!file) {
            currentPath = null
            pendingPx = null
            pendingAnchor = null
            disarmStabilizer()
            return
        }
        currentPath = file.path
        if (pathChanged) {
            const entry = getFileScrollEntry(file.path)
            const savedScroll = entry?.scrollTop ?? getFileScroll(file.path)
            if (entry && (entry.anchor || entry.blockAnchor)) {
                pendingAnchor = { saved: entry, attempts: 0 }
            } else {
                pendingAnchor = null
            }
            pendingPx = { path: file.path, scrollTop: savedScroll ?? 0, attempts: 0, entry: entry ?? null }
            startPoll()
            kick()
        }
    }

    function onContentReady(): void {
        realignAnchor()
        kick()
    }

    function restoreAfterContainerSwitch(saved: SavedScroll | null): void {
        if (!saved) return
        pendingAnchor = { saved, attempts: 0 }
        startPoll()
        kick()
    }

    return {
        start,
        dispose,
        scrollElFor,
        currentScrollEl,
        onFileWillChange,
        onFileChanged,
        onContentReady,
        captureScroll,
        restoreAfterContainerSwitch,
        cancelPendingRestore,
        realignAnchor,
    }
}
