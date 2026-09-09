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

export interface ScrollAnchorState {
    id: string
    line: number
    relTop: number
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
        // it only needs a laid-out scrollable container.
        if (pendingAnchor) {
            if (++pendingAnchor.attempts > MAX_ANCHOR_ATTEMPTS) {
                pendingAnchor = null
            } else if (el && isScrollable(el)) {
                restoreScroll(pendingAnchor.saved, el)
                pendingAnchor = null
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

    function capturePreviewAnchor(el: HTMLElement): ScrollAnchorState | null {
        const content = ctx.file()?.content || ''
        const toc = extractToc(content, 'markdown')
        if (toc.length === 0) return null
        const idToMeta = new Map(toc.map((i) => [i.id, i]))
        const headings: { id: string; line: number; contentTop: number }[] = []
        if (el.querySelectorAll) {
            for (const h of el.querySelectorAll('h1, h2, h3, h4, h5, h6')) {
                const id = h.id
                const meta = id ? idToMeta.get(id) : undefined
                if (!meta) continue
                const contentTop =
                    (h as Element).getBoundingClientRect?.().top -
                    (el as Element).getBoundingClientRect?.().top
                headings.push({ id, line: meta.line, contentTop })
            }
        }
        return pickPreviewAnchor(headings, el.scrollTop)
    }

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
        const max = el.scrollHeight - el.clientHeight
        const ratio = max > 0 ? { ratio: el.scrollTop / max } : null
        let anchor: ScrollAnchorState | null = null
        if (ctx.isMarkdown()) {
            if (el.classList?.contains('markdown-body')) {
                anchor = capturePreviewAnchor(el)
            } else if (el.classList?.contains('cm-scroller')) {
                anchor = captureCmAnchor(el)
            }
        }
        return { anchor, ratio }
    }

    function restorePreviewAnchor(el: HTMLElement, anchor: ScrollAnchorState): boolean {
        const sel = `#${escapeCssIdentifier(anchor.id)}`
        const target = el.querySelector?.(sel)
        if (!target) return false
        const contentTop =
            (target as Element).getBoundingClientRect?.().top -
            (el as Element).getBoundingClientRect?.().top
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

    function restoreScroll(saved: SavedScroll, el: HTMLElement): void {
        // TOC heading alignment first, percentage ratio as fallback.
        if (saved.anchor && ctx.isMarkdown()) {
            if (el.classList?.contains('markdown-body') && restorePreviewAnchor(el, saved.anchor)) return
            if (el.classList?.contains('cm-scroller') && restoreCmAnchor(el, saved.anchor)) return
        }
        if (saved.ratio) {
            const max = el.scrollHeight - el.clientHeight
            if (max > 0) el.scrollTop = Math.round(saved.ratio.ratio * max)
        }
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
