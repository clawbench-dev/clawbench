/**
 * useFileScrollRestore — scroll-position save/restore for the file viewer.
 *
 * Owns every aspect of keeping the user's place when files/views switch:
 *
 *  - Cross-file / reopen restore uses pixel scrollTop, persisted per path in the
 *    module-level fileScrollCache (so it survives FileViewer unmount/remount).
 *  - rendered↔raw / edit-toggle restore uses a single SOURCE LINE anchor: the
 *    rendered pane annotates each block with `data-source-line` and CodeMirror
 *    exposes real line numbers, so a 1-based line is the shared coordinate
 *    between the two panes (with a pixel offset inside the owning block kept as
 *    `sourceOffset` for rendered→rendered precision).
 *  - One shared 50ms poll drives both kinds of pending restore.
 *
 * Guards that prevent losing the position (the historic bugs):
 *  - A hidden container (view panel left via v-show, e.g. switching to the file
 *    manager) has its CodeMirror scrollTop reset to 0 by display:none. We never
 *    write a scroll offset read from a hidden container (offsetParent === null).
 *  - Content renders asynchronously (CodeMirror viewport, markdown images), so
 *    the scroll container height grows over ticks. A target beyond the current
 *    max scroll is deferred until the content can actually hold it, otherwise
 *    the browser clamps it and it is never corrected. At give-up the deferred
 *    restore falls through to the pixel / ratio ladder rather than dropping the
 *    user at the top.
 *
 * Instance state (per FileViewer) — the composable is NOT a singleton; only the
 * fileScrollCache it writes into is shared at module scope.
 */

import { EditorView } from '@codemirror/view'
import { renderedLineAnchor, renderedLineScrollTop } from '@/utils/scrollRenderedToLine'
import {
    getFileScroll,
    getFileScrollEntry,
    setFileScroll,
    type FileScrollEntry,
} from '@/utils/fileScrollCache'

export type { FileScrollEntry } from '@/utils/fileScrollCache'

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
    /**
     * 1-based source line at the viewport top — the single content coordinate
     * shared by the rendered pane (`data-source-line`) and the raw pane
     * (CodeMirror line numbers). Works for markdown with or without headings.
     */
    sourceLine?: number
    /**
     * Pixels the viewport top sits BELOW the owning block's top. Lets a
     * rendered→rendered restore land inside a tall block instead of snapping to
     * its top. Ignored when restoring the raw pane (different line metrics).
     */
    sourceOffset?: number
    ratio?: { ratio: number } | null
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
    /** Capture { sourceLine, ratio } so a pane swap can restore the same place. */
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
 * cached anchor pointing at wherever the user was when scrolling started.
 * Recomputing the whole snapshot once scrolling settles keeps pixels and the
 * line anchor describing the same place.
 */
const SNAPSHOT_DEBOUNCE_MS = 120
const CANCEL_EVENT = 'cancel-scroll-restore'
const RESTORE_EVENT = 'restore-file-scroll'
const REALIGN_EVENT = 'realign-file-scroll'

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

/**
 * Top visible source line of a CodeMirror scroll container — the raw pane's
 * half of the shared line coordinate. Works without any TOC headings.
 */
export function cmTopLine(el: HTMLElement): number | null {
    const view = EditorView.findFromDOM(el)
    if (!view) return null
    try {
        const topBlock = view.lineBlockAtHeight(el.scrollTop + 1)
        return view.state.doc.lineAt(topBlock.from).number
    } catch {
        return null
    }
}

/**
 * Ideal scrollTop that puts `line` at a CodeMirror container's viewport top —
 * NOT clamped. Returns null when the editor cannot resolve (no view yet).
 * Callers defer instead of clamping while async content grows.
 *
 * The `-1` mirrors the `+1` on capture (cmTopLine) so a capture→restore round
 * trip lands on the same visual line.
 */
export function cmLineScrollTop(el: HTMLElement, line: number): number | null {
    const view = EditorView.findFromDOM(el)
    if (!view) return null
    const target = Math.min(Math.max(1, line), view.state.doc.lines)
    let block
    try {
        block = view.lineBlockAt(view.state.doc.line(target).from)
    } catch {
        return null
    }
    return Math.max(0, block.top - 1)
}

/** Capture the rendered pane's line anchor + in-block pixel offset. */
export function captureMarkdownScroll(el: HTMLElement): FileScrollEntry {
    const max = el.scrollHeight - el.clientHeight
    const ratio = max > 0 ? { ratio: el.scrollTop / max } : null
    const anchor = renderedLineAnchor(el)
    return {
        scrollTop: el.scrollTop,
        sourceLine: anchor?.line,
        sourceOffset: anchor?.offset,
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
    let activeSourceLine: number | null = null
    let activeSourceOffset = 0
    let stabilizeArmedPx: number | null = null

    function disarmStabilizer(): void {
        activeSourceLine = null
        activeSourceOffset = 0
        stabilizeArmedPx = null
    }

    /**
     * Remember the source line a restore just landed on so late layout shifts
     * can be corrected (see realignAnchor).
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
    function armStabilizer(el: HTMLElement, sourceLine: number, sourceOffset = 0): void {
        activeSourceLine = sourceLine
        activeSourceOffset = sourceOffset
        // The programmatic scroll that positions the anchor fires a scroll
        // event asynchronously. Remember the applied offset so that echo is
        // not mistaken for user scrolling (which must disarm the stabilizer).
        stabilizeArmedPx = el.scrollTop
    }

    /**
     * Arm the anchor after a *pixel* restore, so a late layout shift can still
     * be corrected.
     *
     * Pixels and the line anchor describe the same place only while the layout
     * is stable. A markdown pane that is still growing (images, Mermaid) shifts
     * the content under a pixel offset, so the anchor has to be able to pull
     * the position back. Restores that land through the poll (content not tall
     * enough yet) are the ones most likely to be followed by late loads, so
     * they arm too.
     */
    function armStabilizerForEntry(el: HTMLElement, entry?: SavedScroll | null): void {
        if (!entry || typeof entry.sourceLine !== 'number') return
        if (!ctx.isMarkdown() || !el.classList?.contains('markdown-body')) return
        armStabilizer(el, entry.sourceLine, entry.sourceOffset ?? 0)
    }

    function realignAnchor(): void {
        if (activeSourceLine == null) return
        const el = currentScrollEl()
        if (!el || !isScrollable(el)) return
        if (!ctx.isMarkdown() || !el.classList?.contains('markdown-body')) return

        // Recompute the owning block's top and re-apply the in-block offset. If
        // the target is not reachable yet (content still growing) do nothing —
        // clamping here would fight the user's own scrolling.
        //
        // A correction moves scrollTop, and that move comes back through the
        // scroll handler as an echo. Re-arm on the new offset, otherwise the
        // echo reads as "the user scrolled" and disarms the stabilizer — the
        // first late image would then be the last one ever corrected, which is
        // exactly what dropping the timeout was meant to avoid.
        const blockTop = renderedLineScrollTop(el, activeSourceLine)
        if (blockTop == null) return
        const target = blockTop + activeSourceOffset
        if (!pxCanApply(el, target)) return
        el.scrollTop = target
        stabilizeArmedPx = el.scrollTop
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

    /** Write a full { scrollTop, sourceLine, ratio } snapshot now. */
    function refreshSnapshot(): void {
        if (!currentPath || !attachedEl || !isVisiblyAttached(attachedEl)) return
        const saved = captureScroll(attachedEl)
        if (!saved) return
        setFileScroll(currentPath, {
            scrollTop: attachedEl.scrollTop,
            sourceLine: saved.sourceLine,
            sourceOffset: saved.sourceOffset,
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
                    sourceLine: existing.sourceLine,
                    sourceOffset: existing.sourceOffset,
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
            // the fling settles so the cache never holds a stale line.
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
                // Content never grew enough to hold the line — fall through to
                // the pixel / ratio ladder rather than leaving the user at 0.
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

    // ── capture & restore ─────────────────────────────────────────────────

    function captureScroll(el: HTMLElement | null): SavedScroll | null {
        if (!el) return null
        if (ctx.isMarkdown() && el.classList?.contains('markdown-body')) {
            return captureMarkdownScroll(el)
        }
        const max = el.scrollHeight - el.clientHeight
        const ratio = max > 0 ? { ratio: el.scrollTop / max } : null
        let sourceLine: number | undefined
        if (ctx.isMarkdown() && el.classList?.contains('cm-scroller')) {
            // Raw pane: record the exact source line so the round trip back to
            // the rendered pane (or a reopen) aligns on the same line, even for
            // markdown without any headings.
            sourceLine = cmTopLine(el) ?? undefined
        }
        return { scrollTop: el.scrollTop, sourceLine, ratio }
    }

    function restoreScroll(saved: SavedScroll, el: HTMLElement, allowFallback = true): boolean {
        // 1. Source-line anchor — the single content coordinate. Preferred over
        //    pixels because the two panes have different heights and must align
        //    on the source line.
        if (ctx.isMarkdown() && typeof saved.sourceLine === 'number' && saved.sourceLine > 0) {
            if (el.classList?.contains('markdown-body')) {
                const blockTop = renderedLineScrollTop(el, saved.sourceLine)
                if (blockTop != null) {
                    const target = blockTop + (saved.sourceOffset ?? 0)
                    if (pxCanApply(el, target)) {
                        el.scrollTop = target
                        armStabilizer(el, saved.sourceLine, saved.sourceOffset ?? 0)
                        return true
                    }
                    // Content not tall enough yet → defer to the next tick
                    // instead of clamping to a wrong position. At give-up
                    // (allowFallback) fall through to the pixel / ratio ladder.
                    if (!allowFallback) return false
                }
            } else if (el.classList?.contains('cm-scroller')) {
                const target = cmLineScrollTop(el, saved.sourceLine)
                if (target != null) {
                    if (pxCanApply(el, target)) {
                        el.scrollTop = target
                        return true
                    }
                    if (!allowFallback) return false
                }
            }
        }
        // 2. Pixel restore
        if (typeof saved.scrollTop === 'number' && pxCanApply(el, saved.scrollTop)) {
            el.scrollTop = saved.scrollTop
            return true
        }
        // 3. Percentage ratio as fallback
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
        // and keep the line anchor purely as a fallback for when the content
        // can no longer hold that offset.
        if (preferPx && typeof target === 'number') {
            const el = currentScrollEl()
            if (el && isScrollable(el) && pxCanApply(el, target)) {
                el.scrollTop = target
                armStabilizerForEntry(el, entry)
                return
            }
            pendingAnchor = null
            pendingPx = { path: currentPath || '', scrollTop: target, attempts: 0, entry: entry ?? null }
            startPoll()
            kick()
            return
        }

        if (entry && typeof entry.sourceLine === 'number') {
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
                    sourceLine: saved.sourceLine,
                    sourceOffset: saved.sourceOffset,
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
                    sourceLine: saved.sourceLine,
                    sourceOffset: saved.sourceOffset,
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
            if (entry && typeof entry.sourceLine === 'number') {
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
