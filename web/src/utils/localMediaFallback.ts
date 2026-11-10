/**
 * Load-state tracking + graceful fallback for content images.
 *
 * Why this exists
 * ---------------
 * Rendered markdown rewrites local image paths to `/api/fs/raw/…` (or a
 * `/api/fs/thumb` thumbnail) with NO existence check — `rewriteImageUrls` and
 * `createFixLocalImagePaths` emit the URL unconditionally. When the file does
 * not exist the endpoint answers 404 and the browser paints its default
 * broken-image glyph: a silent, unexplained gap in the conversation
 * (issue #501). Unlike file-path links, images are not covered by
 * `verifyFilePaths` (which only scans `[data-file-path]` elements), so nothing
 * else can surface the failure.
 *
 * What this does
 * --------------
 * Two document-level capture-phase listeners (resource events do NOT bubble, so
 * capture is required), because a content image has exactly three visual states
 * and the markup cannot express two of them:
 *
 *  - **pending** (no attribute) — nothing has arrived yet. `media-block.css`
 *    paints the loading placeholder for this state.
 *  - **settled** (`data-media-settled`) — the wait is over, one way or the
 *    other: `load` fired, or `error` fired for an image that is not content
 *    media (see below). Stamped by the listeners below.
 *  - **missing** (`data-media-missing`) — the `error` listener swapped the
 *    `<img>` for the shared MediaLoadError component, mounted imperatively with
 *    `h()` + `render()` — this module runs on v-html surfaces (chat messages,
 *    share pages) that have no Vue app instance, so it cannot rely on a
 *    component tree. The same component is used by every Vue media surface,
 *    which is what keeps a failed image looking identical in a chat bubble and
 *    in the file viewer.
 *
 * The stamp is what lets CSS distinguish "still arriving" from "arrived":
 * neither `load` nor `error` can be observed from a stylesheet, and the
 * pipelines emit the same `<img>` markup either way. It is stamped on EVERY
 * `<img>`, not on a whitelist of content surfaces — the selector list of the
 * placeholder lives in the stylesheet, and duplicating it here is how the two
 * drift apart.
 *
 * EVERY failing content image gets the placeholder, local or remote: a broken
 * remote URL used to keep the browser's glyph, which is the same ugly gap. The
 * file manager's own thumbnails and the attachment chips (which carry no
 * `lightbox-img` marker and degrade to a type icon on their own) are left
 * alone; they are still stamped as settled, which is harmless.
 *
 * The original `<img>` is kept in the DOM but hidden, and the `load` listener
 * restores it. That makes the fallback reversible: if the file is created
 * later, `useMediaWatch` bumps the `?t=` version, the browser re-fetches, and
 * the real image reappears without a re-render.
 *
 * Why streaming needs BOTH halves
 * -------------------------------
 * A streaming turn rewrites its text block through `v-html` on every chunk,
 * which replaces the block's children wholesale. Without help, that means a
 * fresh `<img>` per chunk, a fresh doomed request per chunk, and the
 * placeholder rebuilt only after each new error — a gap the browser may paint
 * in between (Android WebView is the reported case), i.e. the block collapses
 * and re-expands on every chunk. Two mechanisms cover the two reasons an image
 * has nothing to show:
 *
 *  - **unresolved local path** (the common case): the streaming render parks the
 *    destination in `UNRESOLVED_SRC_ATTR` instead of `src` (see
 *    `mediaSrc.ts`), so no request is ever made and the quiet card is painted
 *    on the spot. Nothing to remember — every chunk starts out parked.
 *  - **already failed** (remote 404, an absolute path the server rejects): the
 *    src is remembered below and re-applied by a MutationObserver, whose
 *    callback runs after Vue's `innerHTML` write and BEFORE the frame is
 *    painted, so the card never visibly disappears.
 *
 * Only images inside a still-streaming message consult the memory. A settled
 * message re-renders with rewritten `?target=` URLs — a different key — and
 * must be allowed to try them.
 */

import { h, render } from 'vue'
import { baseName } from '@/utils/path'
import { UNRESOLVED_SRC_ATTR } from '@/utils/mediaSrc.ts'
import MediaLoadError from '@/components/media/MediaLoadError.vue'

/** Marks an <img> that already has a placeholder, so a retry cannot stack them. */
const MISSING_ATTR = 'data-media-missing'
/**
 * Marks an <img> whose wait is over — loaded, or failed (the failure path then
 * swaps it for the placeholder). Read by `media-block.css` to decide whether to
 * paint the loading placeholder — see the module doc.
 */
export const MEDIA_SETTLED_ATTR = 'data-media-settled'
/** Wrapper class hiding the figure's view/attach toolbar for missing media. */
const WRAPPER_CLASS = 'image-block-missing'

/**
 * Selector for "this image belongs to a message that is still streaming".
 *
 * Streaming is also what decides the quiet placeholder variant, so both reads
 * go through one selector rather than each spelling out the attribute.
 */
const STREAMING_SELECTOR = '[data-streaming="true"]'

/**
 * Srcs that failed while their turn was streaming, in insertion order.
 *
 * A streaming block is re-rendered on every chunk (see the module doc), so the
 * failure has to outlive the DOM node that discovered it: without this the
 * placeholder is rebuilt only after the next 404 round trip, and the gap
 * between the two is what flickers. Bounded — a long turn may reference many
 * broken files and this must not grow without limit.
 */
const FAILED_SOURCES = new Set<string>()
const FAILED_SOURCES_MAX = 64

/** Remember a streaming src that cannot be loaded (evicting the oldest). */
function rememberFailedSource(src: string): void {
    if (!src) return
    FAILED_SOURCES.add(src)
    while (FAILED_SOURCES.size > FAILED_SOURCES_MAX) {
        const oldest = FAILED_SOURCES.values().next()
        if (oldest.done) break
        FAILED_SOURCES.delete(oldest.value)
    }
}

/** Drop a src once it loads — the file exists now, so the card must stand down. */
function forgetFailedSource(src: string): void {
    if (src) FAILED_SOURCES.delete(src)
}

/**
 * The path a failed image was trying to render, for display.
 *
 * Served URLs carry the real path in `?target=` / `?path=` or in the path
 * suffix (`/api/fs/raw/<rel>`). Anything else — a relative path the pipeline
 * has not rewritten yet (streaming), or a remote URL — falls back to the URL's
 * own path, which is the only name available. `data:` / `blob:` URLs carry no
 * filename at all and yield ''.
 */
export function mediaPathFromSrc(src: string): string {
    if (!src || /^(data|blob):/i.test(src)) return ''
    try {
        const parsed = new URL(src, window.location.origin)
        const target = parsed.searchParams.get('target') || parsed.searchParams.get('path')
        if (target) return target
        const m = parsed.pathname.match(/\/api\/(?:fs\/raw|share\/[^/]+\/local)\/(.+)$/)
        return decodeOrRaw(m ? m[1] : parsed.pathname).replace(/^\/+/, '')
    } catch {
        return ''
    }
}

/** `decodeURIComponent`, tolerating malformed escapes (shows the raw text). */
function decodeOrRaw(value: string): string {
    try {
        return decodeURIComponent(value)
    } catch {
        return value
    }
}

/**
 * Record that an image's wait is over, which is what lets the stylesheet tell
 * "still arriving" from "arrived" (see the module doc). Idempotent.
 */
export function markMediaSettled(img: HTMLImageElement): void {
    img.setAttribute(MEDIA_SETTLED_ATTR, '1')
}

/**
 * Build the placeholder by mounting the shared MediaLoadError component into a
 * detached host.
 *
 * Mounting imperatively (rather than hand-writing DOM here) is what makes the
 * chat / share surfaces and the Vue media surfaces render the SAME element: one
 * component, one set of global classes, one place to change the wording or the
 * icon. `render()` is used with a bare host — the component is written against
 * the global i18n instance precisely so it works without an app context.
 *
 * Returns the host element (which the caller inserts) plus a teardown that
 * unmounts the component. The host carries no class of its own: the
 * placeholder's identity is the component's root class, so there is exactly one
 * element to find (and to count).
 */
function buildPlaceholder(src: string, streaming: boolean): { host: HTMLElement; dispose: () => void } {
    const host = document.createElement('span')
    const name = baseName(mediaPathFromSrc(src))
    render(h(MediaLoadError, { kind: 'image', name, streaming }), host)
    return {
        host,
        dispose: () => {
            render(null, host)
            host.remove()
        },
    }
}

/**
 * Swap an image with nothing to show for the placeholder, reversibly.
 *
 * Every failing content image gets one — local or not. A remote image that
 * 404s used to keep the browser's broken glyph (there was "no local file to
 * name"), which is exactly the ugly gap this placeholder exists to replace.
 *
 * The variant follows the message's state: inside a still-streaming message
 * the path has not been resolved yet, so the quiet variant is used; once the
 * turn settles the same failure renders as the labelled card.
 */
export function applyMediaMissingFallback(img: HTMLImageElement): boolean {
    if (img.hasAttribute(MISSING_ATTR)) return false
    // A streaming render parks a local destination here instead of `src`, so
    // inserting the markup cannot start a request (see mediaSrc.ts). The name
    // still comes from it, or the card would be anonymous.
    const parked = img.hasAttribute(UNRESOLVED_SRC_ATTR)
    const src = img.getAttribute('src') || (parked ? img.getAttribute(UNRESOLVED_SRC_ATTR) || '' : '')

    img.setAttribute(MISSING_ATTR, '1')
    img.classList.add('local-media-hidden')
    // The image never fires an event on the re-apply path (no request was
    // awaited), but its wait is over either way.
    markMediaSettled(img)

    const streaming = !!img.closest(STREAMING_SELECTOR)
    // A parked src never failed — it was never tried — so it must not enter the
    // memory (that would let it evict a real remote failure).
    if (streaming && !parked) rememberFailedSource(src)
    const placeholder = buildPlaceholder(src, streaming)
    img.insertAdjacentElement('afterend', placeholder.host)

    const wrapper = img.closest('.image-block-wrapper')
    wrapper?.classList.add(WRAPPER_CLASS)

    // Reversible: a later successful load (the file was created, or the
    // network recovered) removes the placeholder and restores the image.
    img.addEventListener('load', () => {
        forgetFailedSource(src)
        img.removeAttribute(MISSING_ATTR)
        img.classList.remove('local-media-hidden')
        markMediaSettled(img)
        placeholder.dispose()
        wrapper?.classList.remove(WRAPPER_CLASS)
    }, { once: true })

    return true
}

/**
 * Placeholder for one image the observer just saw inserted.
 *
 * Two reasons a freshly inserted image has nothing to show: the streaming
 * pipeline parked its destination (no request was ever made — it cannot load
 * before the turn settles), or it already failed earlier in this turn (the wave
 * of re-renders would otherwise rebuild the card after every 404).
 */
function handleInsertedImage(img: HTMLImageElement): void {
    if (img.hasAttribute(MISSING_ATTR)) return
    if (img.hasAttribute(UNRESOLVED_SRC_ATTR)) {
        // Parked on purpose: never requested, so there is nothing to wait for.
        applyMediaMissingFallback(img)
        return
    }
    if (FAILED_SOURCES.size === 0) return
    if (!img.closest(STREAMING_SELECTOR)) return
    if (!FAILED_SOURCES.has(img.getAttribute('src') || '')) return
    applyMediaMissingFallback(img)
}

/**
 * Re-apply the placeholder to images a patch just inserted into a streaming
 * message.
 *
 * Called by the MutationObserver below for every subtree a patch added, BEFORE
 * the frame is painted (observer callbacks are microtasks), which is what turns
 * "placeholder destroyed and rebuilt once per streaming chunk" into "the
 * placeholder never moves". Exported for direct testing; the behaviour that
 * matters is what the observer does with it.
 */
export function reapplyKnownMediaFailures(root: ParentNode): void {
    if (root instanceof Element && root.tagName === 'IMG') {
        handleInsertedImage(root as HTMLImageElement)
        return
    }
    const images = root.querySelectorAll?.('img')
    if (!images || images.length === 0) return
    for (const img of Array.from(images)) handleInsertedImage(img)
}

let installed = false
let errorHandler: ((e: Event) => void) | null = null
let loadHandler: ((e: Event) => void) | null = null
let mutationObserver: MutationObserver | null = null

/**
 * Install the document-level capture-phase load/error listeners. Idempotent;
 * safe to call from every app bootstrap. Returns a teardown (tests / HMR).
 *
 * `load` stamps `data-media-settled` on EVERY `<img>` — the stylesheet decides
 * which images show a placeholder, so filtering here would need a second copy
 * of that selector list. `error` is scoped to CONTENT media, which is either
 * `img.lightbox-img` (stamped by the media-block factory for chat, file
 * preview, share and the table modal) or any `<img>` inside a message that is
 * still streaming — those come from the bare markdown render, which skips the
 * enhancements that stamp the marker (deliberately: streaming must stay cheap).
 * The file manager's own thumbnails match neither — they have a component-level
 * `@error` that falls back to a type icon, and two competing fallbacks would
 * fight.
 *
 * The initial sweep matters as much as the listener: `load` fires ONCE, so an
 * image that finished decoding before this module ran (a cached image painted
 * during the first render, or any image when the module is installed lazily)
 * would never be stamped and would keep the pending placeholder forever.
 * `complete` + `naturalWidth` is the only way to ask "did this already load" —
 * `complete` alone is also true for a src-less image, and in jsdom it is true
 * for a never-loaded one, so the width is the real signal.
 */
export function installLocalMediaFallback(): () => void {
    if (installed || typeof document === 'undefined') return () => {}
    installed = true

    loadHandler = (e: Event) => {
        const target = e.target as Element | null
        if (!target || target.tagName !== 'IMG') return
        markMediaSettled(target as HTMLImageElement)
    }

    errorHandler = (e: Event) => {
        const target = e.target as Element | null
        if (!target || target.tagName !== 'IMG') return
        const img = target as HTMLImageElement
        // ANY error ends the pending state: leaving the shimmer running would
        // promise a load that is never coming.
        markMediaSettled(img)
        // See the doc above: a streaming message renders bare <img> without the
        // `lightbox-img` marker, and every one of them is content.
        if (!img.classList.contains('lightbox-img') && !img.closest(STREAMING_SELECTOR)) return
        applyMediaMissingFallback(img)
    }

    // Capture phase: resource events do not bubble, so a bubbling listener
    // never fires for them.
    document.addEventListener('load', loadHandler, true)
    document.addEventListener('error', errorHandler, true)

    // A streaming block is re-rendered through `v-html` on every chunk, which
    // destroys the placeholder built for the previous frame. Re-applying it
    // from this observer (a microtask — after the patch, before the paint) is
    // what keeps a known-missing image from blinking once per chunk, and what
    // turns a parked destination into a card without any request. See the
    // module doc.
    if (typeof MutationObserver !== 'undefined' && document.body) {
        mutationObserver = new MutationObserver((records) => {
            for (const record of records) {
                for (const node of Array.from(record.addedNodes)) {
                    if (node.nodeType !== Node.ELEMENT_NODE) continue
                    reapplyKnownMediaFailures(node as Element)
                }
            }
        })
        mutationObserver.observe(document.body, { childList: true, subtree: true })
    }

    for (const img of Array.from(document.querySelectorAll('img'))) {
        if (img.complete && img.naturalWidth > 0) markMediaSettled(img)
    }

    return uninstallLocalMediaFallback
}

/** Tear down the listeners (tests / HMR). */
export function uninstallLocalMediaFallback(): void {
    if (loadHandler) document.removeEventListener('load', loadHandler, true)
    if (errorHandler) document.removeEventListener('error', errorHandler, true)
    mutationObserver?.disconnect()
    mutationObserver = null
    loadHandler = null
    errorHandler = null
    installed = false
    // The memory describes the DOM that was just torn down; a fresh install
    // (HMR, the next test) starts from "nothing has failed".
    FAILED_SOURCES.clear()
}
