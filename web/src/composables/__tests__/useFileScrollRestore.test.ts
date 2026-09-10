import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { getFileScroll, getFileScrollEntry, setFileScroll, _resetFileScrollCache } from '@/utils/fileScrollCache'
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
const renderedLineAnchorMock = vi.fn()
const renderedLineScrollTopMock = vi.fn()
vi.mock('@/utils/scrollRenderedToLine', () => ({
    renderedLineAnchor: (...a: unknown[]) => renderedLineAnchorMock(...a),
    renderedTopLine: (...a: unknown[]) => renderedLineAnchorMock(...a)?.line ?? null,
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
        renderedLineAnchorMock.mockReset()
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

        it('falls back to ratio at give-up when the line anchor is unreachable', () => {
            // The line anchor resolves but its target sits beyond the current
            // max scroll. The deferred restore must not drop the user at the
            // top — at give-up it falls through to the ratio ladder.
            const el = makeEl({ scrollHeight: 500, clientHeight: 50, scrollTop: 0, _classes: ['markdown-body'] })
            renderedLineScrollTopMock.mockReturnValue(4600) // max scroll = 450
            const ctx = makeContext({
                contentRoot: () => el,
                isMarkdown: () => true,
                file: () => ({ path: 'a.md', content: 'x' }),
            })
            const s = useFileScrollRestore(ctx)

            s.restoreAfterContainerSwitch({ scrollTop: 800, sourceLine: 900, ratio: { ratio: 0.4 } })
            vi.advanceTimersByTime(60 * 50 + 100) // past MAX_ANCHOR_ATTEMPTS
            expect(el.scrollTop).toBe(180) // 0.4 * (500 - 50)
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

    describe('captureScroll line-anchor branches', () => {
        it('captures the rendered source-line anchor + in-block offset', () => {
            const el = makeEl({ scrollHeight: 300, clientHeight: 50, scrollTop: 50, _classes: ['markdown-body'] })
            renderedLineAnchorMock.mockReturnValue({ line: 42, offset: 10 })
            const ctx = makeContext({
                contentRoot: () => el,
                isMarkdown: () => true,
                file: () => ({ path: 'a.md', content: '# Intro\n\nbody\n\n# Section 2' }),
            })
            const s = useFileScrollRestore(ctx)

            const saved = s.captureScroll(el)
            expect(saved).not.toBeNull()
            expect(saved!.sourceLine).toBe(42)
            expect(saved!.sourceOffset).toBe(10)
            expect(saved!.ratio).not.toBeNull()
        })

        it('captures the top source line from a CodeMirror pane (no headings involved)', () => {
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
            expect(saved!.sourceLine).toBe(12)
        })

        it('returns only ratio when the markdown pane exposes no source line', () => {
            const el = makeEl({ scrollHeight: 200, clientHeight: 50, scrollTop: 50, _classes: ['markdown-body'] })
            renderedLineAnchorMock.mockReturnValue(null)
            const ctx = makeContext({
                contentRoot: () => el,
                isMarkdown: () => true,
                file: () => ({ path: 'a.md', content: 'plain text' }),
            })
            const s = useFileScrollRestore(ctx)

            const saved = s.captureScroll(el)
            expect(saved!.sourceLine).toBeUndefined()
            expect(saved!.ratio).toEqual({ ratio: 50 / 150 })
        })
    })

    describe('source-line / ratio restore', () => {
        it('falls back to ratio when there is no line anchor', () => {
            const el = makeEl({ scrollHeight: 200, clientHeight: 50, scrollTop: 0 })
            const ctx = makeContext({
                contentRoot: () => el,
                isMarkdown: () => true,
                file: () => ({ path: 'a.md', content: '# hi' }),
            })
            const s = useFileScrollRestore(ctx)

            s.restoreAfterContainerSwitch({ ratio: { ratio: 0.5 } })
            vi.advanceTimersByTime(50)
            expect(el.scrollTop).toBe(75) // 0.5 * (200 - 50)
        })

        it('prefers the source-line anchor over the ratio', () => {
            const el = makeEl({ scrollHeight: 300, clientHeight: 50, scrollTop: 0, _classes: ['markdown-body'] })
            renderedLineScrollTopMock.mockReturnValue(40)
            const ctx = makeContext({
                contentRoot: () => el,
                isMarkdown: () => true,
                file: () => ({ path: 'a.md', content: '# Section\n\nbody' }),
            })
            const s = useFileScrollRestore(ctx)

            s.restoreAfterContainerSwitch({ sourceLine: 40, ratio: { ratio: 0.9 } })
            vi.advanceTimersByTime(50)
            // The line restore scrolls to the block top (40), NOT to the ratio
            // position (0.9 × 250 = 225).
            expect(el.scrollTop).toBe(40)
        })

        it('adds the in-block offset so a tall block restores mid-way, not at its top', () => {
            const el = makeEl({ scrollHeight: 3000, clientHeight: 500, scrollTop: 0, _classes: ['markdown-body'] })
            renderedLineScrollTopMock.mockReturnValue(100)
            const ctx = makeContext({
                contentRoot: () => el,
                isMarkdown: () => true,
                file: () => ({ path: 'a.md', content: 'x' }),
            })
            const s = useFileScrollRestore(ctx)

            s.restoreAfterContainerSwitch({ sourceLine: 20, sourceOffset: 600 })
            vi.advanceTimersByTime(50)
            expect(el.scrollTop).toBe(700) // 100 + 600
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

            s.restoreAfterContainerSwitch({ sourceLine: 40, ratio: { ratio: 0.5 } })
            vi.advanceTimersByTime(50)
            // Deferred: scrollTop untouched (not clamped to 10).
            expect(el.scrollTop).toBe(0)

            // Content grows enough to hold 40 → next tick applies it.
            el.scrollHeight = 200
            vi.advanceTimersByTime(50)
            expect(el.scrollTop).toBe(40)
        })

        it('restores a CodeMirror pane by scrolling the source line to the top', () => {
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

            s.restoreAfterContainerSwitch({ sourceLine: 20, ratio: { ratio: 0.9 } })
            vi.advanceTimersByTime(50)
            // block.top 250 − 1 = 249, within maxScroll (300−50=250).
            expect(el.scrollTop).toBe(249)
        })

        it('anchor restore is not gated by loading', () => {
            const el = makeEl({ scrollHeight: 200, clientHeight: 50, scrollTop: 0 })
            const ctx = makeContext({
                contentRoot: () => el,
                loading: () => true, // would block pixel restore
            })
            const s = useFileScrollRestore(ctx)

            s.restoreAfterContainerSwitch({ ratio: { ratio: 0.5 } })
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

    describe('cached anchor freshness', () => {
        it('recomputes the cached source line after a scroll settles', () => {
            const el = makeEl({ scrollHeight: 5000, clientHeight: 500, scrollTop: 0, _classes: ['markdown-body'] })
            const ctx = makeContext({
                contentRoot: () => el,
                isMarkdown: () => true,
                file: () => ({ path: 'a.md', content: '# One\n\nbody\n\n## Two\n\nbody\n' }),
            })
            const s = useFileScrollRestore(ctx)
            s.start()

            // A previous visit banked the first line.
            renderedLineAnchorMock.mockReturnValue({ line: 1, offset: 0 })
            setFileScroll('a.md', { scrollTop: 0, sourceLine: 1, sourceOffset: 0 })
            s.onFileChanged({ path: 'a.md' }, true)
            vi.advanceTimersByTime(50) // attach the scroll listener

            // User scrolls to the middle of section two.
            el.scrollTop = 2500
            el.fire('scroll')
            expect(getFileScroll('a.md')).toBe(2500)
            // Pixels land immediately; the line anchor is still the old one.
            expect(getFileScrollEntry('a.md')?.sourceLine).toBe(1)

            // Once the fling settles the snapshot is recomputed with the new line.
            renderedLineAnchorMock.mockReturnValue({ line: 12, offset: 40 })
            vi.advanceTimersByTime(150)
            const entry = getFileScrollEntry('a.md')
            expect(entry?.sourceLine).toBe(12)
            expect(entry?.sourceOffset).toBe(40)
            s.dispose()
        })

        it('drops a pending anchor refresh when the file is switched away', () => {
            const el = makeEl({ scrollHeight: 5000, clientHeight: 500, scrollTop: 0, _classes: ['markdown-body'] })
            const ctx = makeContext({
                contentRoot: () => el,
                isMarkdown: () => true,
                file: () => ({ path: 'a.md', content: '# One\n\nbody\n' }),
            })
            const s = useFileScrollRestore(ctx)
            s.start()
            s.onFileChanged({ path: 'a.md' }, true)
            vi.advanceTimersByTime(50)

            el.scrollTop = 1500
            el.fire('scroll')
            setFileScroll('b.md', 0)
            s.onFileWillChange()
            s.onFileChanged({ path: 'b.md' }, true)

            // The debounce must not resurrect a.md's position onto b.md.
            vi.advanceTimersByTime(200)
            expect(getFileScroll('b.md')).toBe(0)
            s.dispose()
        })
    })

    describe('preferPx restore', () => {
        it('applies the pixel offset even when the entry carries a line anchor', () => {
            const el = makeEl({ scrollHeight: 2000, clientHeight: 500, scrollTop: 0, _classes: ['markdown-body'] })
            const ctx = makeContext({
                contentRoot: () => el,
                isMarkdown: () => true,
                file: () => ({ path: 'a.md', content: '# One\n' }),
            })
            const s = useFileScrollRestore(ctx)
            s.start()
            s.onFileChanged({ path: 'a.md' }, true)
            vi.advanceTimersByTime(50)

            window.dispatchEvent(new CustomEvent('restore-file-scroll', {
                detail: {
                    scrollTop: 1200,
                    scrollEntry: { scrollTop: 1200, sourceLine: 1, sourceOffset: 0 },
                    preferPx: true,
                },
            }))

            expect(el.scrollTop).toBe(1200)
            s.dispose()
        })

        it('defers to the pixel poll when the content cannot hold the offset yet', () => {
            const el = makeEl({ scrollHeight: 600, clientHeight: 500, scrollTop: 0, _classes: ['markdown-body'] })
            const ctx = makeContext({
                contentRoot: () => el,
                isMarkdown: () => true,
                file: () => ({ path: 'a.md', content: '# One\n' }),
            })
            const s = useFileScrollRestore(ctx)
            s.start()
            s.onFileChanged({ path: 'a.md' }, true)
            vi.advanceTimersByTime(50)

            // 1200 is beyond maxScroll (100) → must not clamp to 100.
            window.dispatchEvent(new CustomEvent('restore-file-scroll', {
                detail: { scrollTop: 1200, preferPx: true },
            }))
            expect(el.scrollTop).toBe(0)

            el.scrollHeight = 2000
            vi.advanceTimersByTime(50)
            expect(el.scrollTop).toBe(1200)
            s.dispose()
        })

        it('without preferPx the line anchor still wins (rendered ↔ raw alignment)', () => {
            const el = makeEl({
                scrollHeight: 2000,
                clientHeight: 500,
                scrollTop: 0,
                _classes: ['markdown-body'],
            })
            renderedLineScrollTopMock.mockReturnValue(1000)
            const ctx = makeContext({
                contentRoot: () => el,
                isMarkdown: () => true,
                file: () => ({ path: 'a.md', content: '# One\n' }),
            })
            const s = useFileScrollRestore(ctx)
            s.start()
            s.onFileChanged({ path: 'a.md' }, true)
            vi.advanceTimersByTime(50)

            window.dispatchEvent(new CustomEvent('restore-file-scroll', {
                detail: {
                    scrollTop: 1200,
                    scrollEntry: { scrollTop: 1200, sourceLine: 3, sourceOffset: 0 },
                },
            }))

            // Line alignment (1000), not the pixel offset (1200).
            expect(el.scrollTop).toBe(1000)
            s.dispose()
        })
    })

    describe('cache entry replacement on scroll', () => {
        it('replaces the entry object instead of mutating a banked snapshot', () => {
            const el = makeEl({ scrollHeight: 2000, clientHeight: 500, scrollTop: 0 })
            const ctx = makeContext({ contentRoot: () => el, file: () => ({ path: 'a.md', content: '' }) })
            const s = useFileScrollRestore(ctx)
            const seeded = { scrollTop: 0, sourceLine: 1, sourceOffset: 0, ratio: null }
            setFileScroll('a.md', seeded)
            s.onFileChanged({ path: 'a.md' }, true)
            vi.advanceTimersByTime(50) // listener attached

            el.scrollTop = 300
            el.fire('scroll')

            const after = getFileScrollEntry('a.md')
            // New object — a snapshot banked elsewhere must not move with it.
            expect(after).not.toBe(seeded)
            expect(getFileScroll('a.md')).toBe(300)
            expect(after?.sourceLine).toBe(seeded.sourceLine)
            s.dispose()
        })
    })

    describe('realign stabilizer', () => {
        /** A markdown pane whose line-anchor target moves as images load. */
        function makeAnchorEl() {
            return makeEl({ scrollHeight: 3000, clientHeight: 500, scrollTop: 0, _classes: ['markdown-body'] })
        }

        function setup(ctx: any) {
            const s = useFileScrollRestore(ctx)
            s.start()
            renderedLineScrollTopMock.mockReturnValue(1000)
            setFileScroll('a.md', { scrollTop: 960, sourceLine: 3, sourceOffset: -40, ratio: null })
            s.onFileChanged({ path: 'a.md' }, true)
            vi.advanceTimersByTime(50)
            return s
        }

        it('re-aligns to the line anchor after a layout shift while armed', () => {
            const el = makeAnchorEl()
            const ctx = makeContext({
                contentRoot: () => el,
                isMarkdown: () => true,
                file: () => ({ path: 'a.md', content: '# One\n\n## Two\n' }),
            })
            const s = setup(ctx)

            // Block top 1000 + offset −40 → scrollTop 960.
            expect(el.scrollTop).toBe(960)

            // A real browser fires a scroll event for that programmatic restore —
            // an echo carrying the armed offset; it must NOT disarm.
            el.fire('scroll')

            renderedLineScrollTopMock.mockReturnValue(1400) // an image above finished loading
            window.dispatchEvent(new CustomEvent('realign-file-scroll'))
            expect(el.scrollTop).toBe(1360)

            // The correction's own echo must not be read as user scrolling.
            el.fire('scroll')

            // Re-armed: a second shift within the window realigns again.
            renderedLineScrollTopMock.mockReturnValue(1800)
            window.dispatchEvent(new CustomEvent('realign-file-scroll'))
            expect(el.scrollTop).toBe(1760)
            s.dispose()
        })

        it('a user scroll away from the armed offset disarms the stabilizer', () => {
            const el = makeAnchorEl()
            const ctx = makeContext({
                contentRoot: () => el,
                isMarkdown: () => true,
                file: () => ({ path: 'a.md', content: '# One\n\n## Two\n' }),
            })
            const s = setup(ctx)
            expect(el.scrollTop).toBe(960)

            el.scrollTop = 1600 // user scrolls on
            el.fire('scroll')

            renderedLineScrollTopMock.mockReturnValue(1400)
            window.dispatchEvent(new CustomEvent('realign-file-scroll'))
            // Disarmed — the realign must not fight the user's scroll.
            expect(el.scrollTop).toBe(1600)
            s.dispose()
        })

        it('stays armed long enough for a very late load', () => {
            // No time limit: an image that finishes 30s after the restore must
            // still be corrected.
            const el = makeAnchorEl()
            const ctx = makeContext({
                contentRoot: () => el,
                isMarkdown: () => true,
                file: () => ({ path: 'a.md', content: '# One\n\n## Two\n' }),
            })
            const s = setup(ctx)
            expect(el.scrollTop).toBe(960)

            vi.advanceTimersByTime(30000)
            renderedLineScrollTopMock.mockReturnValue(1400)
            window.dispatchEvent(new CustomEvent('realign-file-scroll'))
            expect(el.scrollTop).toBe(1360)
            s.dispose()
        })

        it('disarms when an explicit jump takes over positioning', () => {
            const el = makeAnchorEl()
            const ctx = makeContext({
                contentRoot: () => el,
                isMarkdown: () => true,
                file: () => ({ path: 'a.md', content: '# One\n\n## Two\n' }),
            })
            const s = setup(ctx)
            expect(el.scrollTop).toBe(960)

            // Every explicit jump (scroll-to-line, TOC, code link) cancels the
            // pending restore — the realign must not fight it afterwards.
            window.dispatchEvent(new CustomEvent('cancel-scroll-restore'))
            renderedLineScrollTopMock.mockReturnValue(1400)
            window.dispatchEvent(new CustomEvent('realign-file-scroll'))
            expect(el.scrollTop).toBe(960)
            s.dispose()
        })

        it('does not clamp when the line target is unreachable', () => {
            const el = makeAnchorEl()
            const ctx = makeContext({
                contentRoot: () => el,
                isMarkdown: () => true,
                file: () => ({ path: 'a.md', content: '# One\n\n## Two\n' }),
            })
            const s = setup(ctx)
            expect(el.scrollTop).toBe(960)

            el.scrollHeight = 1200 // max scroll = 700; target 4600 is unreachable
            renderedLineScrollTopMock.mockReturnValue(4600)
            window.dispatchEvent(new CustomEvent('realign-file-scroll'))
            // Must not write an out-of-range offset (browser would clamp to the
            // bottom and fight the user).
            expect(el.scrollTop).toBe(960)
            s.dispose()
        })

        it('arms the stabilizer on a preferPx restore when a line anchor is present', () => {
            const el = makeAnchorEl()
            const ctx = makeContext({
                contentRoot: () => el,
                isMarkdown: () => true,
                file: () => ({ path: 'a.md', content: '# One\n\n## Two\n' }),
            })
            const s = useFileScrollRestore(ctx)
            s.start()
            s.onFileChanged({ path: 'a.md' }, true)
            vi.advanceTimersByTime(50)

            window.dispatchEvent(new CustomEvent('restore-file-scroll', {
                detail: {
                    scrollTop: 960,
                    scrollEntry: { scrollTop: 960, sourceLine: 3, sourceOffset: -40 },
                    preferPx: true,
                },
            }))

            expect(el.scrollTop).toBe(960)
            // Programmatic scroll echo does not disarm
            el.fire('scroll')

            renderedLineScrollTopMock.mockReturnValue(1500) // late image loaded
            window.dispatchEvent(new CustomEvent('realign-file-scroll'))
            expect(el.scrollTop).toBe(1460)
            s.dispose()
        })

        it('realigns for each successive late load', () => {
            // Image after image: every late load gets its own correction, with
            // no window to expire. Each correction moves scrollTop, so a real
            // browser hands it back as a scroll echo — that echo must be
            // tolerated, otherwise the first late load would be the last one
            // ever corrected.
            const el = makeAnchorEl()
            const ctx = makeContext({
                contentRoot: () => el,
                isMarkdown: () => true,
                file: () => ({ path: 'a.md', content: '# One\n\n## Two\n' }),
            })
            const s = setup(ctx)
            expect(el.scrollTop).toBe(960)

            vi.advanceTimersByTime(2000)
            renderedLineScrollTopMock.mockReturnValue(1400)
            window.dispatchEvent(new CustomEvent('realign-file-scroll'))
            expect(el.scrollTop).toBe(1360)
            el.fire('scroll') // echo of the correction itself

            vi.advanceTimersByTime(2000)
            renderedLineScrollTopMock.mockReturnValue(1800)
            window.dispatchEvent(new CustomEvent('realign-file-scroll'))
            expect(el.scrollTop).toBe(1760)
            el.fire('scroll')

            // Still armed after two echoes: a third load is corrected too.
            vi.advanceTimersByTime(2000)
            renderedLineScrollTopMock.mockReturnValue(2200)
            window.dispatchEvent(new CustomEvent('realign-file-scroll'))
            expect(el.scrollTop).toBe(2160)
            s.dispose()
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
