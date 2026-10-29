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
 *    other: `load` fired, or `error` fired for media that gets no placeholder
 *    (an external URL). Stamped by the listeners below.
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
 * External URLs, `data:` URIs and the file manager's own thumbnails (which
 * already degrade to a type icon) are left alone by the FALLBACK; they are
 * still stamped as settled, which is harmless (nothing styles them by it).
 *
 * The original `<img>` is kept in the DOM but hidden, and the `load` listener
 * restores it. That makes the fallback reversible: if the file is created
 * later, `useMediaWatch` bumps the `?t=` version, the browser re-fetches, and
 * the real image reappears without a re-render.
 */

import { h, render } from 'vue'
import { baseName } from '@/utils/path'
import MediaLoadError from '@/components/media/MediaLoadError.vue'

/** Marks an <img> that already has a placeholder, so a retry cannot stack them. */
const MISSING_ATTR = 'data-media-missing'
/**
 * Marks an <img> whose wait is over — loaded, or failed with no placeholder
 * substituted. Read by `media-block.css` to decide whether to paint the loading
 * placeholder — see the module doc.
 */
export const MEDIA_SETTLED_ATTR = 'data-media-settled'
/** Wrapper class hiding the figure's view/attach toolbar for missing media. */
const WRAPPER_CLASS = 'image-block-missing'

/**
 * True when the URL is served by one of our own local-file endpoints and can
 * therefore legitimately 404 (as opposed to an external image or inline data).
 */
export function isLocalMediaUrl(url: string): boolean {
    return /^\/api\/(fs|share)\//.test(url)
}

/**
 * The path a failed image was trying to render, for display. Derived from the
 * query param when present (`/api/fs/thumb?target=…`, `/api/fs/raw/?target=…`)
 * and from the path suffix otherwise (`/api/fs/raw/<rel>`). Returns '' when
 * nothing usable can be extracted.
 */
export function mediaPathFromSrc(src: string): string {
    try {
        const parsed = new URL(src, window.location.origin)
        const target = parsed.searchParams.get('target') || parsed.searchParams.get('path')
        if (target) return target
        const m = parsed.pathname.match(/\/api\/(?:fs\/raw|share\/[^/]+\/local)\/(.+)$/)
        return m ? decodeURIComponent(m[1]) : ''
    } catch {
        return ''
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
function buildPlaceholder(src: string): { host: HTMLElement; dispose: () => void } {
    const host = document.createElement('span')
    const name = baseName(mediaPathFromSrc(src))
    render(h(MediaLoadError, { kind: 'image', name }), host)
    return {
        host,
        dispose: () => {
            render(null, host)
            host.remove()
        },
    }
}

/** Swap a failed local image for the placeholder, reversibly. */
export function applyMediaMissingFallback(img: HTMLImageElement): boolean {
    if (img.hasAttribute(MISSING_ATTR)) return false
    const src = img.getAttribute('src') || ''
    if (!isLocalMediaUrl(src)) return false

    img.setAttribute(MISSING_ATTR, '1')
    img.classList.add('local-media-hidden')

    const placeholder = buildPlaceholder(src)
    img.insertAdjacentElement('afterend', placeholder.host)

    const wrapper = img.closest('.image-block-wrapper')
    wrapper?.classList.add(WRAPPER_CLASS)

    // Reversible: a later successful load (the file was created, or the
    // network recovered) removes the placeholder and restores the image.
    img.addEventListener('load', () => {
        img.removeAttribute(MISSING_ATTR)
        img.classList.remove('local-media-hidden')
        markMediaSettled(img)
        placeholder.dispose()
        wrapper?.classList.remove(WRAPPER_CLASS)
    }, { once: true })

    return true
}

let installed = false
let errorHandler: ((e: Event) => void) | null = null
let loadHandler: ((e: Event) => void) | null = null

/**
 * Install the document-level capture-phase load/error listeners. Idempotent;
 * safe to call from every app bootstrap. Returns a teardown (tests / HMR).
 *
 * `load` stamps `data-media-settled` on EVERY `<img>` — the stylesheet decides
 * which images show a placeholder, so filtering here would need a second copy
 * of that selector list. `error` is scoped to `img.lightbox-img`: that marker is
 * stamped on every content image by the media-block factory (chat, file
 * preview, share, table modal) and by the image viewer. The file manager's own
 * thumbnails deliberately carry it NOT — they have a component-level `@error`
 * that falls back to a type icon, and two competing fallbacks would fight.
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
        // ANY error ends the pending state, whether or not a placeholder is
        // substituted: a failed external image gets no card (there is no local
        // file to name), and leaving the shimmer running would promise a load
        // that is never coming. The broken glyph it degrades to is the same
        // behaviour this module has always left for non-local media.
        markMediaSettled(img)
        if (!img.classList.contains('lightbox-img')) return
        applyMediaMissingFallback(img)
    }

    // Capture phase: resource events do not bubble, so a bubbling listener
    // never fires for them.
    document.addEventListener('load', loadHandler, true)
    document.addEventListener('error', errorHandler, true)

    for (const img of Array.from(document.querySelectorAll('img'))) {
        if (img.complete && img.naturalWidth > 0) markMediaSettled(img)
    }

    return uninstallLocalMediaFallback
}

/** Tear down the listeners (tests / HMR). */
export function uninstallLocalMediaFallback(): void {
    if (loadHandler) document.removeEventListener('load', loadHandler, true)
    if (errorHandler) document.removeEventListener('error', errorHandler, true)
    loadHandler = null
    errorHandler = null
    installed = false
}
