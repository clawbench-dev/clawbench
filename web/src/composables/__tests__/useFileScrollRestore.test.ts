import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { getFileScroll, getFileScrollEntry, setFileScroll, _resetFileScrollCache } from '@/utils/fileScrollCache'
import { extractToc } from '@/utils/toc'
import {
    useFileScrollRestore,
    isScrollable,
    isVisiblyAttached,
    pxCanApply,
    getElementContentTop,
    captureBlockAnchor,
    restoreBlockAnchor,
    type FileScrollContext,
    type ScrollContainerLike,
} from '@/composables/useFileScrollRestore'

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

    describe('getElementContentTop', () => {
        it('walks the offsetTop chain up to the container', () => {
            const container: any = { scrollTop: 0 }
            const wrapper: any = { offsetTop: 40, offsetParent: container }
            const child: any = { offsetTop: 60, offsetParent: wrapper }
            expect(getElementContentTop(child, container)).toBe(100)
        })

        it('falls back to rect math and adds the scroll offset back', () => {
            const container: any = {
                scrollTop: 300,
                getBoundingClientRect: () => ({ top: 10 }),
            }
            // 570px below the container's top edge in the scrolled viewport.
            const below: any = { offsetTop: undefined, offsetParent: null, getBoundingClientRect: () => ({ top: 580 }) }
            expect(getElementContentTop(below, container)).toBe(870)
            // Scrolled past: above the container's top edge (diff = -70).
            const above: any = { offsetTop: undefined, offsetParent: null, getBoundingClientRect: () => ({ top: -60 }) }
            expect(getElementContentTop(above, container)).toBe(230)
        })
    })

    describe('blockAnchor capture & restore', () => {
        it('captures the topmost visible block and survives a layout shift', () => {
            const el = makeEl({ scrollHeight: 3000, clientHeight: 500, scrollTop: 1200 })
            const blocks = [100, 800, 1500, 2200].map((top) => ({ offsetTop: top, offsetParent: el, tagName: 'P' }))
            el.children = blocks

            const anchor = captureBlockAnchor(el)
            // 800 is the last block at or above scrollTop (+4px tolerance).
            expect(anchor?.index).toBe(1)
            expect(anchor?.relTop).toBe(-400)
            expect(anchor?.selector).toBe(':scope > :nth-child(2)')

            // An image above finished loading and pushed the block down.
            blocks[1].offsetTop = 1600
            expect(restoreBlockAnchor(el, anchor!)).toBe(true)
            expect(el.scrollTop).toBe(2000) // 1600 - (-400)
        })

        it('captures by id when the block has one', () => {
            const el = makeEl({ scrollHeight: 2000, clientHeight: 500, scrollTop: 600 })
            const blocks = [
                { offsetTop: 0, offsetParent: el, tagName: 'H1', id: 'intro' },
                { offsetTop: 900, offsetParent: el, tagName: 'P' },
            ]
            el.children = blocks

            expect(captureBlockAnchor(el)?.selector).toBe('#intro')
        })

        it('returns null without children', () => {
            const el = makeEl({ scrollHeight: 2000, clientHeight: 500 })
            expect(captureBlockAnchor(el)).toBeNull()
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
        it('captures a preview heading anchor from the markdown-body pane', () => {
            // Heading sits at content top 40, viewport scrolled past it (top 50):
            // its rect is 10px above the container's top edge, so the rect
            // fallback must add the scroll offset back (−10 + 50 = 40).
            const headingEl = { id: 'intro', getBoundingClientRect: () => ({ top: -10 } as DOMRect) }
            const el = makeEl({
                scrollHeight: 300,
                clientHeight: 50,
                scrollTop: 50,
                _classes: ['markdown-body'],
            })
            el.querySelectorAll = (sel: string) => (sel.startsWith('h1') ? [headingEl] : [])
            const ctx = makeContext({
                contentRoot: () => el,
                isMarkdown: () => true,
                file: () => ({ path: 'a.md', content: '# Intro\n\nbody\n\n# Section 2' }),
            })
            const s = useFileScrollRestore(ctx)

            const saved = s.captureScroll(el)
            expect(saved).not.toBeNull()
            // A heading is found at content top 40 (≤ scrollTop 50) → anchor.
            expect(saved!.anchor).not.toBeNull()
            expect(saved!.anchor!.id).toBe('intro')
            expect(saved!.ratio).not.toBeNull()
        })

        it('returns only ratio when the markdown has no headings', () => {
            const el = makeEl({ scrollHeight: 200, clientHeight: 50, scrollTop: 50, _classes: ['markdown-body'] })
            el.querySelectorAll = () => []
            const ctx = makeContext({
                contentRoot: () => el,
                isMarkdown: () => true,
                file: () => ({ path: 'a.md', content: 'plain text without headings' }),
            })
            const s = useFileScrollRestore(ctx)

            const saved = s.captureScroll(el)
            expect(saved!.anchor).toBeNull()
            expect(saved!.ratio).toEqual({ ratio: 50 / 150 })
        })
    })

    describe('anchor/ratio restore', () => {
        it('falls back to ratio when there is no heading anchor', () => {
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

        it('prefers the heading anchor over the ratio', () => {
            const headingEl = { id: 'sec-1', getBoundingClientRect: () => ({ top: 100 } as DOMRect) }
            const el = makeEl({
                scrollHeight: 300,
                clientHeight: 50,
                scrollTop: 0,
                _classes: ['markdown-body'],
            })
            el.querySelectorAll = (sel: string) => (sel.startsWith('h1') ? [headingEl] : [])
            el.querySelector = (sel: string) => {
                if (sel === '#sec-1') return headingEl
                if (sel === '.markdown-body' || sel === '.cm-scroller') return el
                return null
            }
            const ctx = makeContext({
                contentRoot: () => el,
                isMarkdown: () => true,
                file: () => ({ path: 'a.md', content: '# Section\n\nbody\n\n# Section 2' }),
            })
            const s = useFileScrollRestore(ctx)

            s.restoreAfterContainerSwitch({ anchor: { id: 'sec-1', line: 1, relTop: 0 }, ratio: { ratio: 0.9 } })
            vi.advanceTimersByTime(50)
            // heading top 100 relative to el, relTop 0 → scrollTop = 100 (not ratio 225)
            expect(el.scrollTop).toBe(100)
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

    describe('cached anchor freshness', () => {
        /**
         * Headings at 1000/2000/3000px inside a 5000px-tall document.
         * `offsetParent` points at the container so getElementContentTop() can
         * walk up to it the way real DOM does.
         */
        function makeMarkdownEl(toc: { id: string; line: number }[]) {
            const el = makeEl({ scrollHeight: 5000, clientHeight: 500, scrollTop: 0, _classes: ['markdown-body'] })
            const headings = toc.map((item, i) => ({
                id: item.id,
                offsetTop: (i + 1) * 1000,
                offsetParent: el as Element,
            }))
            el.querySelectorAll = () => headings as any
            return el
        }

        it('recomputes the cached anchor after a scroll, instead of leaving the old heading', () => {
            const md = '# One\n\nbody\n\n## Two\n\nbody\n\n## Three\n\nbody\n'
            const toc = extractToc(md, 'markdown')
            expect(toc.length).toBe(3)

            const el = makeMarkdownEl(toc)
            const ctx = makeContext({
                contentRoot: () => el,
                isMarkdown: () => true,
                file: () => ({ path: 'a.md', content: md }),
            })
            const s = useFileScrollRestore(ctx)
            s.start()

            // A previous visit banked an anchor at the very first heading.
            setFileScroll('a.md', { scrollTop: 0, anchor: { id: toc[0].id, line: toc[0].line, relTop: 0 } })
            s.onFileChanged({ path: 'a.md' }, true)
            vi.advanceTimersByTime(50) // attach the scroll listener

            // User scrolls to the middle of section two.
            el.scrollTop = 2500
            el.fire('scroll')
            expect(getFileScroll('a.md')).toBe(2500)
            // Pixels land immediately; the anchor is still the stale heading.
            expect(getFileScrollEntry('a.md')?.anchor?.id).toBe(toc[0].id)

            vi.advanceTimersByTime(150) // debounce settles
            const entry = getFileScrollEntry('a.md')
            expect(entry?.anchor?.id).toBe(toc[1].id)
            expect(entry?.anchor?.relTop).toBe(-500)
            s.dispose()
        })

        it('drops a pending anchor refresh when the file is switched away', () => {
            const md = '# One\n\nbody\n\n## Two\n'
            const toc = extractToc(md, 'markdown')
            const el = makeMarkdownEl(toc)
            const ctx = makeContext({
                contentRoot: () => el,
                isMarkdown: () => true,
                file: () => ({ path: 'a.md', content: md }),
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
        it('applies the pixel offset even when the entry carries an anchor', () => {
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
                    scrollEntry: { scrollTop: 1200, anchor: { id: 'stale', line: 1, relTop: 0 } },
                    preferPx: true,
                },
            }))

            expect(el.scrollTop).toBe(1200)
            s.dispose()
        })

        it('defers to the anchor when the content cannot hold the offset yet', () => {
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

        it('without preferPx the anchor still wins (rendered ↔ raw alignment)', () => {
            const el = makeEl({
                scrollHeight: 2000,
                clientHeight: 500,
                scrollTop: 0,
                _classes: ['markdown-body'],
            })
            // A heading the anchor can actually resolve to, at content-top 1000.
            const target: any = { id: 'sec-two', offsetTop: 1000, offsetParent: el }
            el.querySelector = (sel: string) =>
                sel === '#sec-two' ? target : (sel === '.markdown-body' || sel === '.cm-scroller' ? el : null)
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
                    scrollEntry: { scrollTop: 1200, anchor: { id: 'sec-two', line: 1, relTop: 0 } },
                },
            }))

            // Heading alignment (1000), not the pixel offset (1200).
            expect(el.scrollTop).toBe(1000)
            s.dispose()
        })
    })

    describe('cache entry replacement on scroll', () => {
        it('replaces the entry object instead of mutating a banked snapshot', () => {
            const el = makeEl({ scrollHeight: 2000, clientHeight: 500, scrollTop: 0 })
            const ctx = makeContext({ contentRoot: () => el, file: () => ({ path: 'a.md', content: '' }) })
            const s = useFileScrollRestore(ctx)
            const seeded = { scrollTop: 0, anchor: { id: 'x', line: 1, relTop: 0 }, blockAnchor: null, ratio: null }
            setFileScroll('a.md', seeded)
            s.onFileChanged({ path: 'a.md' }, true)
            vi.advanceTimersByTime(50) // listener attached

            el.scrollTop = 300
            el.fire('scroll')

            const after = getFileScrollEntry('a.md')
            // New object — a snapshot banked elsewhere must not move with it.
            expect(after).not.toBe(seeded)
            expect(getFileScroll('a.md')).toBe(300)
            expect(after?.anchor).toEqual(seeded.anchor)
            s.dispose()
        })
    })

    describe('realign stabilizer', () => {
        function makeAnchorEl() {
            const el = makeEl({ scrollHeight: 3000, clientHeight: 500, scrollTop: 0, _classes: ['markdown-body'] })
            const heading: any = { id: 'sec-two', offsetTop: 1000, offsetParent: el }
            el.querySelectorAll = () => [heading]
            el.querySelector = (sel: string) =>
                sel === '#sec-two' ? heading : (sel === '.markdown-body' || sel === '.cm-scroller' ? el : null)
            return { el, heading }
        }

        function setup(ctx: any) {
            const s = useFileScrollRestore(ctx)
            s.start()
            setFileScroll('a.md', { scrollTop: 960, anchor: { id: 'sec-two', line: 3, relTop: -40 }, blockAnchor: null, ratio: null })
            s.onFileChanged({ path: 'a.md' }, true)
            vi.advanceTimersByTime(50)
            return s
        }

        it('re-aligns to the anchor after a layout shift while armed', () => {
            const { el, heading } = makeAnchorEl()
            const ctx = makeContext({
                contentRoot: () => el,
                isMarkdown: () => true,
                file: () => ({ path: 'a.md', content: '# One\n\n## Two\n' }),
            })
            const s = setup(ctx)

            // Heading aligned: contentTop 1000, relTop -40 → scrollTop 1040.
            expect(el.scrollTop).toBe(1040)

            // A real browser fires a scroll event for that programmatic restore —
            // an echo carrying the armed offset; it must NOT disarm.
            el.fire('scroll')

            heading.offsetTop = 1400 // an image above finished loading
            window.dispatchEvent(new CustomEvent('realign-file-scroll'))
            expect(el.scrollTop).toBe(1440)

            // The correction's own echo must not be read as user scrolling.
            el.fire('scroll')

            // Re-armed: a second shift within the window realigns again.
            heading.offsetTop = 1800
            window.dispatchEvent(new CustomEvent('realign-file-scroll'))
            expect(el.scrollTop).toBe(1840)
            s.dispose()
        })

        it('a user scroll away from the armed offset disarms the stabilizer', () => {
            const { el, heading } = makeAnchorEl()
            const ctx = makeContext({
                contentRoot: () => el,
                isMarkdown: () => true,
                file: () => ({ path: 'a.md', content: '# One\n\n## Two\n' }),
            })
            const s = setup(ctx)
            expect(el.scrollTop).toBe(1040)

            el.scrollTop = 1600 // user scrolls on
            el.fire('scroll')

            heading.offsetTop = 1400
            window.dispatchEvent(new CustomEvent('realign-file-scroll'))
            // Disarmed — the realign must not fight the user's scroll.
            expect(el.scrollTop).toBe(1600)
            s.dispose()
        })

        it('stays armed long enough for a very late load', () => {
            // No time limit: an image that finishes 30s after the restore must
            // still be corrected.
            const { el, heading } = makeAnchorEl()
            const ctx = makeContext({
                contentRoot: () => el,
                isMarkdown: () => true,
                file: () => ({ path: 'a.md', content: '# One\n\n## Two\n' }),
            })
            const s = setup(ctx)
            expect(el.scrollTop).toBe(1040)

            vi.advanceTimersByTime(30000)
            heading.offsetTop = 1400
            window.dispatchEvent(new CustomEvent('realign-file-scroll'))
            expect(el.scrollTop).toBe(1440)
            s.dispose()
        })

        it('disarms when an explicit jump takes over positioning', () => {
            const { el, heading } = makeAnchorEl()
            const ctx = makeContext({
                contentRoot: () => el,
                isMarkdown: () => true,
                file: () => ({ path: 'a.md', content: '# One\n\n## Two\n' }),
            })
            const s = setup(ctx)
            expect(el.scrollTop).toBe(1040)

            // Every explicit jump (scroll-to-line, TOC, code link) cancels the
            // pending restore — the realign must not fight it afterwards.
            window.dispatchEvent(new CustomEvent('cancel-scroll-restore'))
            heading.offsetTop = 1400
            window.dispatchEvent(new CustomEvent('realign-file-scroll'))
            expect(el.scrollTop).toBe(1040)
            s.dispose()
        })

        it('arms stabilizer on preferPx restore when anchor is present', () => {
            const { el, heading } = makeAnchorEl()
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
                    scrollTop: 1040,
                    scrollEntry: { scrollTop: 1040, anchor: { id: 'sec-two', line: 3, relTop: -40 } },
                    preferPx: true,
                },
            }))

            expect(el.scrollTop).toBe(1040)
            // Programmatic scroll echo does not disarm
            el.fire('scroll')

            heading.offsetTop = 1500 // late image loaded
            window.dispatchEvent(new CustomEvent('realign-file-scroll'))
            expect(el.scrollTop).toBe(1540)
            s.dispose()
        })

        it('realigns for each successive late load', () => {
            // Image after image: every late load gets its own correction, with
            // no window to expire. Each correction moves scrollTop, so a real
            // browser hands it back as a scroll echo — that echo must be
            // tolerated, otherwise the first late load would be the last one
            // ever corrected.
            const { el, heading } = makeAnchorEl()
            const ctx = makeContext({
                contentRoot: () => el,
                isMarkdown: () => true,
                file: () => ({ path: 'a.md', content: '# One\n\n## Two\n' }),
            })
            const s = setup(ctx)
            expect(el.scrollTop).toBe(1040)

            vi.advanceTimersByTime(2000)
            heading.offsetTop = 1400
            window.dispatchEvent(new CustomEvent('realign-file-scroll'))
            expect(el.scrollTop).toBe(1440)
            el.fire('scroll') // echo of the correction itself

            vi.advanceTimersByTime(2000)
            heading.offsetTop = 1800
            window.dispatchEvent(new CustomEvent('realign-file-scroll'))
            expect(el.scrollTop).toBe(1840)
            el.fire('scroll')

            // Still armed after two echoes: a third load is corrected too.
            vi.advanceTimersByTime(2000)
            heading.offsetTop = 2200
            window.dispatchEvent(new CustomEvent('realign-file-scroll'))
            expect(el.scrollTop).toBe(2240)
            s.dispose()
        })

        it('arms the stabilizer when a polled pixel restore lands late', () => {
            // The content is still growing, so the pixel offset cannot be
            // applied on the first tick and lands through the poll instead.
            // That is exactly when images are still coming, so the anchor must
            // be armed to pull the position back afterwards.
            const { el, heading } = makeAnchorEl()
            el.scrollHeight = 600 // max scroll = 100, target 1000 does not fit
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
                    scrollTop: 1000,
                    scrollEntry: { scrollTop: 1000, anchor: { id: 'sec-two', line: 3, relTop: -40 } },
                    preferPx: true,
                },
            }))
            expect(el.scrollTop).toBe(0) // deferred, content too short

            el.scrollHeight = 3000
            vi.advanceTimersByTime(50)
            expect(el.scrollTop).toBe(1000)

            // Echo of the programmatic scroll must not disarm.
            el.fire('scroll')
            heading.offsetTop = 1400 // an image above finished loading
            window.dispatchEvent(new CustomEvent('realign-file-scroll'))
            expect(el.scrollTop).toBe(1440)
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
