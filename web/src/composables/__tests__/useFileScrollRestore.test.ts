import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { getFileScroll, setFileScroll, _resetFileScrollCache } from '@/utils/fileScrollCache'
import {
    useFileScrollRestore,
    isScrollable,
    isVisiblyAttached,
    pxCanApply,
    type FileScrollContext,
    type ScrollContainerLike,
} from '@/composables/useFileScrollRestore'

// scrollRenderedToLine reads real layout (getBoundingClientRect), which jsdom
// cannot measure — inject controllable fakes per test.
const renderedTopLineMock = vi.fn()
const renderedLineScrollTopMock = vi.fn()
vi.mock('@/utils/scrollRenderedToLine', () => ({
    renderedTopLine: (...a: unknown[]) => renderedTopLineMock(...a),
    renderedLineScrollTop: (...a: unknown[]) => renderedLineScrollTopMock(...a),
}))

// CodeMirror EditorView.findFromDOM — faked to return a controllable view.
let fakeView: unknown = null
vi.mock('@codemirror/view', () => ({
    EditorView: { findFromDOM: () => fakeView },
}))

// A controllable fake scroll container. Tests drive scrollHeight/clientHeight
// and fire scroll events manually. offsetParent is settable so the visibility
// guard can be exercised (jsdom never computes real layout).
function makeEl(overrides: Partial<ScrollContainerLike> = {}) {
    const listeners: Record<string, Array<(e: Event) => void>> = {}
    const el: any = {
        scrollHeight: 100,
        clientHeight: 50,
        scrollTop: 0,
        offsetParent: {} as Element | null,
        classList: { contains: (c: string) => (el._classes || []).includes(c) },
        _classes: [] as string[],
        addEventListener: (t: string, fn: (e: Event) => void) => {
            ;(listeners[t] ||= []).push(fn)
        },
        removeEventListener: (t: string, fn: (e: Event) => void) => {
            listeners[t] = (listeners[t] || []).filter((f) => f !== fn)
        },
        querySelectorAll: () => [],
        querySelector: (sel: string) => (sel === '.cm-scroller' || sel === '.markdown-body' ? el : null),
        getBoundingClientRect: () => ({ top: 0, left: 0 } as DOMRect),
        fire(t: string) {
            for (const fn of listeners[t] || []) fn({} as Event)
        },
    }
    Object.assign(el, overrides)
    return el as any
}

function makeContext(overrides: Partial<FileScrollContext> = {}) {
    const ctx: any = {
        contentRoot: () => null,
        file: () => null,
        markdownViewMode: () => 'rendered',
        editing: () => false,
        loading: () => false,
        isMarkdown: () => false,
        isHtml: () => false,
        isOpenapi: () => false,
        ...overrides,
    }
    return ctx
}

describe('useFileScrollRestore', () => {
    beforeEach(() => {
        _resetFileScrollCache()
        vi.useFakeTimers()
        renderedTopLineMock.mockReset()
        renderedLineScrollTopMock.mockReset()
        fakeView = null
    })
    afterEach(() => {
        vi.useRealTimers()
    })

    describe('pure decision helpers', () => {
        it('isScrollable', () => {
            expect(isScrollable({ scrollHeight: 100, clientHeight: 50 })).toBe(true)
            expect(isScrollable({ scrollHeight: 50, clientHeight: 50 })).toBe(false)
            expect(isScrollable({ scrollHeight: 40, clientHeight: 50 })).toBe(false)
        })

        it('isVisiblyAttached', () => {
            expect(isVisiblyAttached({ offsetParent: {} as Element })).toBe(true)
            expect(isVisiblyAttached({ offsetParent: null })).toBe(false)
        })

        it('pxCanApply', () => {
            const el = { scrollHeight: 100, clientHeight: 50 }
            expect(pxCanApply(el, 50)).toBe(true)
            expect(pxCanApply(el, 49)).toBe(true)
            expect(pxCanApply(el, 51)).toBe(false)
        })
    })

    describe('cross-file pixel restore', () => {
        it('restores the cached scrollTop once the content can hold it', () => {
            const el = makeEl({ scrollHeight: 200, clientHeight: 50, scrollTop: 0 })
            const ctx = makeContext({ contentRoot: () => el })
            const s = useFileScrollRestore(ctx)

            // Cached position from a previous visit.
            setFileScroll('a.go', 120)

            // File switches in; el is initially short (can't hold 120).
            el.scrollHeight = 100 // maxScroll 50
            s.onFileWillChange()
            s.onFileChanged({ path: 'a.go' }, true)

            // First ticks: content too short → must not clamp early.
            vi.advanceTimersByTime(50)
            expect(el.scrollTop).toBe(0)

            // Content finishes growing; restore applies exactly 120.
            el.scrollHeight = 200
            vi.advanceTimersByTime(50)
            expect(el.scrollTop).toBe(120)
        })

        it('does not overwrite the cache when the outgoing container is hidden', () => {
            const el = makeEl({ scrollTop: 523 })
            const ctx = makeContext({ contentRoot: () => el })
            const s = useFileScrollRestore(ctx)

            // User scrolled while visible → cache has the trusted value.
            setFileScroll('a.go', 523)

            // Switch away while the panel is hidden (scrollTop already reset to 0).
            el.scrollTop = 0
            el.offsetParent = null
            s.onFileWillChange()

            expect(getFileScroll('a.go')).toBe(523)
        })

        it('saves the outgoing file when the container is visible', () => {
            const el = makeEl({ scrollTop: 441 })
            const ctx = makeContext({ contentRoot: () => el })
            const s = useFileScrollRestore(ctx)

            s.onFileChanged({ path: 'a.go' }, true)
            s.onContentReady() // attach listener
            el.scrollTop = 441
            el.fire('scroll') // user scrolls
            s.onFileWillChange()

            expect(getFileScroll('a.go')).toBe(441)
        })
    })

    describe('scroll listener visibility guard', () => {
        it('records scrolls while visible and ignores them while hidden', () => {
            const el = makeEl()
            const ctx = makeContext({ contentRoot: () => el })
            const s = useFileScrollRestore(ctx)

            s.onFileChanged({ path: 'a.go' }, true)
            vi.advanceTimersByTime(50) // attach
            el.scrollTop = 200
            el.fire('scroll')
            expect(getFileScroll('a.go')).toBe(200)

            el.scrollTop = 0 // display:none reset
            el.offsetParent = null
            el.fire('scroll')
            expect(getFileScroll('a.go')).toBe(200) // unchanged
        })
    })

    describe('max-attempt give-up', () => {
        it('stops polling and attaches when content never grows tall enough', () => {
            const el = makeEl({ scrollHeight: 60, clientHeight: 50, scrollTop: 0 }) // maxScroll 10
            const ctx = makeContext({ contentRoot: () => el })
            const s = useFileScrollRestore(ctx)
            setFileScroll('a.go', 500)

            s.onFileChanged({ path: 'a.go' }, true)
            // 100 ticks (5s) of insufficient height.
            vi.advanceTimersByTime(100 * 50)
            expect(el.scrollTop).toBe(0)

            // Subsequent user scrolls are still tracked (listener attached).
            el.scrollTop = 30
            el.fire('scroll')
        })
    })

    describe('cancel-scroll-restore event', () => {
        it('clears the pending pixel restore so scroll-to-line wins', () => {
            const el = makeEl({ scrollHeight: 60, clientHeight: 50, scrollTop: 0 }) // can't hold 120 yet
            const ctx = makeContext({ contentRoot: () => el })
            const s = useFileScrollRestore(ctx)
            setFileScroll('a.go', 120)

            s.start()
            s.onFileChanged({ path: 'a.go' }, true)
            window.dispatchEvent(new CustomEvent('cancel-scroll-restore'))

            // Even after the content grows tall enough, the cancelled restore
            // must not fire (scroll-to-line has taken over).
            el.scrollHeight = 200
            vi.advanceTimersByTime(200)
            expect(el.scrollTop).toBe(0)
            s.dispose()
        })
    })

    describe('onFileChanged null / clear', () => {
        it('clears the current path and pending restore when the file is cleared', () => {
            const el = makeEl({ scrollHeight: 200, clientHeight: 50, scrollTop: 0 })
            const ctx = makeContext({ contentRoot: () => el })
            const s = useFileScrollRestore(ctx)
            setFileScroll('a.go', 120)

            s.onFileChanged({ path: 'a.go' }, true)
            // Clear the file: path and pending restore reset.
            s.onFileChanged(null, false)

            // Simulate a stale scroll event on the old container: no cache write.
            el.scrollTop = 77
            el.fire('scroll')
            expect(getFileScroll('a.go')).toBe(120)
        })
    })

    describe('scrollElFor view-mode resolution', () => {
        it('returns null for iframe-based viewers (html/openapi/excalidraw rendered)', () => {
            const el = makeEl()
            const mk = (isHtml: boolean, isOpenapi: boolean, isExcalidraw: boolean) =>
                makeContext({
                    contentRoot: () => el,
                    isHtml: () => isHtml,
                    isOpenapi: () => isOpenapi,
                    file: () => (isExcalidraw ? { path: 'd.excalidraw', isExcalidraw: true } : { path: 'x' }),
                })
            const htmlCtx = mk(true, false, false)
            const apiCtx = mk(false, true, false)
            const drawCtx = mk(false, false, true)

            const sHtml = useFileScrollRestore(htmlCtx)
            const sApi = useFileScrollRestore(apiCtx)
            const sDraw = useFileScrollRestore(drawCtx)

            expect(sHtml.scrollElFor('rendered', false)).toBeNull()
            expect(sApi.scrollElFor('rendered', false)).toBeNull()
            expect(sDraw.scrollElFor('rendered', false)).toBeNull()
            // html/openapi resolve the CM scroller in source mode.
            expect(sHtml.scrollElFor('source', false)).toBe(el)
            expect(sApi.scrollElFor('source', false)).toBe(el)
        })

        it('resolves markdown-body for rendered markdown and cm-scroller for raw', () => {
            const previewEl = makeEl({ _classes: ['markdown-body'] })
            const cmEl = makeEl({ _classes: ['cm-scroller'] })
            const root: any = {
                querySelector: (sel: string) => (sel === '.markdown-body' ? previewEl : cmEl),
            }
            const ctx = makeContext({
                contentRoot: () => root,
                isMarkdown: () => true,
            })
            const s = useFileScrollRestore(ctx)

            expect(s.scrollElFor('rendered', false)).toBe(previewEl)
            expect(s.scrollElFor('raw', false)).toBe(cmEl)
            // Edit mode always uses the CM scroller.
            expect(s.scrollElFor('rendered', true)).toBe(cmEl)
        })
    })

    describe('captureScroll markdown anchor branches', () => {
        it('captures the rendered source-line anchor from the markdown-body pane', () => {
            const el = makeEl({ scrollHeight: 300, clientHeight: 50, scrollTop: 50, _classes: ['markdown-body'] })
            renderedTopLineMock.mockReturnValue(42)
            const ctx = makeContext({
                contentRoot: () => el,
                isMarkdown: () => true,
                file: () => ({ path: 'a.md', content: '# Intro\n\nbody\n\n# Section 2' }),
            })
            const s = useFileScrollRestore(ctx)

            const saved = s.captureScroll(el)
            expect(saved).not.toBeNull()
            // Viewport top maps to source line 42 → anchor { line }.
            expect(saved!.anchor).toEqual({ line: 42 })
            expect(saved!.ratio).not.toBeNull()
        })

        it('captures the top source line from a CodeMirror pane (no TOC involved)', () => {
            const el = makeEl({ scrollHeight: 300, clientHeight: 50, scrollTop: 0, _classes: ['cm-scroller'] })
            fakeView = {
                lineBlockAtHeight: () => ({ from: 0 }),
                state: { doc: { lineAt: () => ({ number: 12 }) } },
            }
            const ctx = makeContext({
                contentRoot: () => el,
                isMarkdown: () => true,
                file: () => ({ path: 'a.md', content: 'x' }),
            })
            const s = useFileScrollRestore(ctx)

            const saved = s.captureScroll(el)
            expect(saved!.anchor).toEqual({ line: 12 })
        })

        it('returns only ratio when the markdown pane exposes no source line', () => {
            const el = makeEl({ scrollHeight: 200, clientHeight: 50, scrollTop: 50, _classes: ['markdown-body'] })
            renderedTopLineMock.mockReturnValue(null)
            const ctx = makeContext({
                contentRoot: () => el,
                isMarkdown: () => true,
                file: () => ({ path: 'a.md', content: 'plain text' }),
            })
            const s = useFileScrollRestore(ctx)

            const saved = s.captureScroll(el)
            expect(saved!.anchor).toBeNull()
            expect(saved!.ratio).toEqual({ ratio: 50 / 150 })
        })
    })

    describe('anchor/ratio restore', () => {
        it('falls back to ratio when there is no line anchor', () => {
            const el = makeEl({ scrollHeight: 200, clientHeight: 50, scrollTop: 0 })
            const ctx = makeContext({
                contentRoot: () => el,
                isMarkdown: () => true,
                file: () => ({ path: 'a.md', content: '# hi' }),
            })
            const s = useFileScrollRestore(ctx)

            s.restoreAfterContainerSwitch({ anchor: null, ratio: { ratio: 0.5 } })
            vi.advanceTimersByTime(50)
            expect(el.scrollTop).toBe(75) // 0.5 * (200 - 50)
        })

        it('prefers the line anchor over the ratio', () => {
            const el = makeEl({ scrollHeight: 300, clientHeight: 50, scrollTop: 0, _classes: ['markdown-body'] })
            renderedLineScrollTopMock.mockReturnValue(40)
            const ctx = makeContext({
                contentRoot: () => el,
                isMarkdown: () => true,
                file: () => ({ path: 'a.md', content: '# Section\n\nbody' }),
            })
            const s = useFileScrollRestore(ctx)

            s.restoreAfterContainerSwitch({ anchor: { line: 40 }, ratio: { ratio: 0.9 } })
            vi.advanceTimersByTime(50)
            // The line restore scrolls to the anchor's content top (40), NOT to
            // the ratio position (0.9 × 250 = 225).
            expect(el.scrollTop).toBe(40)
        })

        it('defers the line restore while the content cannot yet hold the target line', () => {
            const el = makeEl({ scrollHeight: 60, clientHeight: 50, scrollTop: 0, _classes: ['markdown-body'] })
            // Target scrollTop 40 needs maxScroll >= 40; the container max is 10,
            // so the first tick must NOT clamp to 10 — it defers.
            renderedLineScrollTopMock.mockReturnValue(40)
            const ctx = makeContext({
                contentRoot: () => el,
                isMarkdown: () => true,
                file: () => ({ path: 'a.md', content: 'x' }),
            })
            const s = useFileScrollRestore(ctx)

            s.restoreAfterContainerSwitch({ anchor: { line: 40 }, ratio: { ratio: 0.5 } })
            vi.advanceTimersByTime(50)
            // Deferred: scrollTop untouched (not clamped to 10).
            expect(el.scrollTop).toBe(0)

            // Content grows enough to hold 40 → next tick applies it.
            el.scrollHeight = 200
            vi.advanceTimersByTime(50)
            expect(el.scrollTop).toBe(40)
        })

        it('restores a CodeMirror pane by scrolling the anchor line to the top', () => {
            const el = makeEl({ scrollHeight: 300, clientHeight: 50, scrollTop: 0, _classes: ['cm-scroller'] })
            fakeView = {
                state: { doc: { lines: 100, line: () => ({ from: 200 }) } },
                lineBlockAt: () => ({ top: 250 }),
            }
            const ctx = makeContext({
                contentRoot: () => el,
                isMarkdown: () => true,
                file: () => ({ path: 'a.md', content: 'x' }),
            })
            const s = useFileScrollRestore(ctx)

            s.restoreAfterContainerSwitch({ anchor: { line: 20 }, ratio: { ratio: 0.9 } })
            vi.advanceTimersByTime(50)
            // block.top 250 − 1, clamped to maxScroll (300−50=250) → 249? no:
            // clamp(250-1=249) → min(250, max(0, 249)) = 249.
            expect(el.scrollTop).toBe(249)
        })

        it('anchor restore is not gated by loading', () => {
            const el = makeEl({ scrollHeight: 200, clientHeight: 50, scrollTop: 0 })
            const ctx = makeContext({
                contentRoot: () => el,
                loading: () => true, // would block pixel restore
            })
            const s = useFileScrollRestore(ctx)

            s.restoreAfterContainerSwitch({ anchor: null, ratio: { ratio: 0.5 } })
            vi.advanceTimersByTime(50)
            expect(el.scrollTop).toBe(75)
        })
    })

    describe('dispose', () => {
        it('saves the current visible position on dispose', () => {
            const el = makeEl({ scrollTop: 700 })
            const ctx = makeContext({ contentRoot: () => el })
            const s = useFileScrollRestore(ctx)

            s.onFileChanged({ path: 'a.go' }, true)
            s.onContentReady()
            el.scrollTop = 700
            s.dispose()

            expect(getFileScroll('a.go')).toBe(700)
        })

        it('does not overwrite the cache when disposed while hidden', () => {
            const el = makeEl({ scrollTop: 0 })
            const ctx = makeContext({ contentRoot: () => el })
            const s = useFileScrollRestore(ctx)
            setFileScroll('a.go', 700)

            s.onFileChanged({ path: 'a.go' }, true)
            el.offsetParent = null
            s.dispose()

            expect(getFileScroll('a.go')).toBe(700)
        })
    })

    describe('restore-file-scroll event', () => {
        it('restores scrollTop when event is dispatched', () => {
            const el = makeEl({ scrollHeight: 1000, clientHeight: 100, scrollTop: 0 })
            const ctx = makeContext({ contentRoot: () => el })
            const s = useFileScrollRestore(ctx)
            s.start()
            s.onFileChanged({ path: 'a.go' }, true)
            s.onContentReady()

            window.dispatchEvent(new CustomEvent('restore-file-scroll', { detail: { scrollTop: 350 } }))
            expect(el.scrollTop).toBe(350)
            s.dispose()
        })

        it('does not restore scrollTop after dispose', () => {
            const el = makeEl({ scrollHeight: 1000, clientHeight: 100, scrollTop: 0 })
            const ctx = makeContext({ contentRoot: () => el })
            const s = useFileScrollRestore(ctx)
            s.start()
            s.onFileChanged({ path: 'a.go' }, true)
            s.onContentReady()
            s.dispose()

            window.dispatchEvent(new CustomEvent('restore-file-scroll', { detail: { scrollTop: 350 } }))
            expect(el.scrollTop).toBe(0)
        })
    })
})
