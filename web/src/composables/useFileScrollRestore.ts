/**
 * useFileScrollRestore — scroll-position save/restore for the file viewer.
 *
 * Owns every aspect of keeping the user's place when files/views switch:
 *
 *  - Cross-file / reopen restore uses pixel scrollTop, persisted per path in the
 *    module-level fileScrollCache (so it survives FileViewer unmount/remount).
 *  - rendered↔raw / edit-toggle restore uses a source line anchor: rendered
 *    blocks carry data-source-line and CodeMirror has real line numbers, so a
 *    single 1-based line is the shared coordinate. Falls back to a scroll
 *    ratio when no line anchor is available.
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

import { EditorView } from '@codemirror/view'
import { ScrollAnchorState } from '@/utils/markdownScroll'
import { renderedTopLine, renderedLineScrollTop } from '@/utils/scrollRenderedToLine'
import { getFileScroll, setFileScroll } from '@/utils/fileScrollCache'

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
    anchor: ScrollAnchorState | null
    ratio: { ratio: number } | null
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
}

const POLL_MS = 50
const MAX_PX_ATTEMPTS = 100 // 100 * 50ms = 5s
const MAX_ANCHOR_ATTEMPTS = 60 // 60 * 50ms = 3s
const CANCEL_EVENT = 'cancel-scroll-restore'
const RESTORE_EVENT = 'restore-file-scroll'

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

export function useFileScrollRestore(ctx: FileScrollContext): UseFileScrollRestore {
    let attachedEl: HTMLElement | null = null
    let scrollHandler: ((e: Event) => void) | null = null
    let currentPath: string | null = null
    let pollTimer: ReturnType<typeof setInterval> | null = null
    let pendingPx: { path: string; scrollTop: number; attempts: number } | null = null
    let pendingAnchor: { saved: SavedScroll; attempts: number } | null = null

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

    function attachScrollListener(): void {
        detachScrollListener()
        const el = currentScrollEl()
        if (!el || !currentPath) return
        attachedEl = el
        scrollHandler = () => {
            // Ignore events fired while hidden: display:none resets CodeMirror's
            // scrollTop to 0 and later re-measuring can fire spurious scroll
            // events — writing 0 would overwrite the trusted position.
            if (el.offsetParent === null) return
            setFileScroll(currentPath!, el.scrollTop)
        }
        el.addEventListener('scroll', scrollHandler, { passive: true })
    }

    function detachScrollListener(): void {
        if (scrollHandler && attachedEl) {
            attachedEl.removeEventListener('scroll', scrollHandler)
        }
        scrollHandler = null
        attachedEl = null
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

        // Pixel restore (cross-file / reopen). Requires content loaded and the
        // container tall enough to actually hold the target (else deferred).
        if (pendingPx) {
            if (++pendingPx.attempts > MAX_PX_ATTEMPTS) {
                // Give up waiting. Original code attaches the listener here even
                // if the content is not scrollable yet — mirror that so future
                // user scrolls are tracked.
                pendingPx = null
                attachScrollListener()
            } else if (!ctx.loading() && el && isScrollable(el)) {
                if (pxCanApply(el, pendingPx.scrollTop)) {
                    el.scrollTop = pendingPx.scrollTop
                    pendingPx = null
                }
                // else: content not tall enough yet — wait for the next tick.
            }
        }

        // Anchor/ratio restore (rendered↔raw / edit toggle). No loading gate —
        // it only needs a laid-out scrollable container. When the target line
        // exists but the content is not tall enough yet (async images), defer
        // to the next tick instead of clamping to a wrong position.
        if (pendingAnchor) {
            if (++pendingAnchor.attempts > MAX_ANCHOR_ATTEMPTS) {
                pendingAnchor = null
            } else if (el && isScrollable(el)) {
                const { anchor, ratio } = pendingAnchor.saved
                let target: number | null = null
                if (anchor && ctx.isMarkdown()) {
                    if (el.classList?.contains('markdown-body')) target = renderedLineScrollTop(el, anchor.line)
                    else if (el.classList?.contains('cm-scroller')) target = cmLineScrollTop(el, anchor.line)
                }
                if (target == null) {
                    // No resolvable line anchor — fall back to ratio and stop.
                    if (ratio) {
                        const max = el.scrollHeight - el.clientHeight
                        if (max > 0) el.scrollTop = Math.round(ratio.ratio * max)
                    }
                    pendingAnchor = null
                } else if (pxCanApply(el, target)) {
                    el.scrollTop = target
                    pendingAnchor = null
                }
                // else: content not tall enough yet — wait for the next tick.
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

    // ── line-anchor capture & restore ─────────────────────────────────────
    // The anchor is a single 1-based source line — the shared coordinate
    // between rendered (data-source-line) and raw (CodeMirror doc) panes.
    //
    // CodeMirror's scrollDOM.scrollTop is treated as content coordinates
    // everywhere else in the app (CodeMirrorViewer.scrollToLine, the
    // viewport-line dispatcher) — the editor's `.cm-content` has no top
    // padding/offset. The `+1` on capture and matching `-1` on restore keep
    // the round trip on the same visual line.

    /** Top visible source line of a CodeMirror scroll container. */
    function cmTopLine(el: HTMLElement): number | null {
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
     * Ideal scrollTop that puts `line` at a CodeMirror container's viewport
     * top — NOT clamped. Returns null when the editor cannot resolve (no view
     * yet). Callers defer instead of clamping while async content grows.
     */
    function cmLineScrollTop(el: HTMLElement, line: number): number | null {
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

    function captureScroll(el: HTMLElement | null): SavedScroll | null {
        if (!el) return null
        const max = el.scrollHeight - el.clientHeight
        const ratio = max > 0 ? { ratio: el.scrollTop / max } : null
        let anchor: ScrollAnchorState | null = null
        if (ctx.isMarkdown()) {
            const line = el.classList?.contains('markdown-body')
                ? renderedTopLine(el)
                : el.classList?.contains('cm-scroller')
                    ? cmTopLine(el)
                    : null
            if (line != null) anchor = { line }
        }
        return { anchor, ratio }
    }

    // ── window cancel-scroll-restore (scroll-to-line takes precedence) ────

    function handleCancelScrollRestore(): void {
        cancelPendingRestore()
    }

    function handleRestoreFileScroll(e: Event): void {
        const ce = e as CustomEvent<{ scrollTop?: number }>
        if (typeof ce?.detail?.scrollTop === 'number') {
            const target = ce.detail.scrollTop
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
    }

    // ── public orchestration ──────────────────────────────────────────────

    function start(): void {
        window.addEventListener(CANCEL_EVENT, handleCancelScrollRestore)
        window.addEventListener(RESTORE_EVENT, handleRestoreFileScroll)
    }

    function dispose(): void {
        // Persist the current position before the viewer is torn down (overlay
        // v-if removal does not run the props.file watcher). Skip when hidden —
        // its scrollTop has been reset to 0 and must not overwrite the cache.
        if (currentPath && attachedEl && isVisiblyAttached(attachedEl)) {
            setFileScroll(currentPath, attachedEl.scrollTop)
        }
        detachScrollListener()
        stopPoll()
        window.removeEventListener(CANCEL_EVENT, handleCancelScrollRestore)
        window.removeEventListener(RESTORE_EVENT, handleRestoreFileScroll)
    }

    function onFileWillChange(): void {
        // Save the pane being left synchronously. Relying only on scroll events
        // can miss the final position when navigation follows a smooth scroll.
        if (currentPath && attachedEl && isVisiblyAttached(attachedEl)) {
            setFileScroll(currentPath, attachedEl.scrollTop)
        }
        detachScrollListener()
        stopPoll()
        // Do NOT clear pendingPx here: a same-path content refresh relies on the
        // content watcher (onContentReady) to re-kick the restore.
    }

    function onFileChanged(file: { path: string } | null, pathChanged: boolean): void {
        if (!file) {
            currentPath = null
            pendingPx = null
            return
        }
        currentPath = file.path
        if (pathChanged) {
            const savedScroll = getFileScroll(file.path)
            pendingPx = { path: file.path, scrollTop: savedScroll ?? 0, attempts: 0 }
            startPoll()
            kick()
        }
    }

    function onContentReady(): void {
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
    }
}
