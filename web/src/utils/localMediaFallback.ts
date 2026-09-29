/**
 * Graceful fallback for locally-served content images that fail to load.
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
 * A single document-level capture-phase `error` listener (resource errors do
 * NOT bubble, so capture is required) swaps a failed local `<img>` for the
 * shared MediaLoadError component, mounted imperatively with `h()` + `render()`
 * — this module runs on v-html surfaces (chat messages, share pages) that have
 * no Vue app instance, so it cannot rely on a component tree. The same
 * component is used by every Vue media surface, which is what keeps a failed
 * image looking identical in a chat bubble and in the file viewer.
 *
 * External URLs, `data:` URIs and the file manager's own thumbnails (which
 * already degrade to a type icon) are left alone.
 *
 * The original `<img>` is kept in the DOM but hidden, and a one-shot `load`
 * listener restores it. That makes the fallback reversible: if the file is
 * created later, `useMediaWatch` bumps the `?t=` version, the browser
 * re-fetches, and the real image reappears without a re-render.
 */

import { h, render } from 'vue'
import { baseName } from '@/utils/path'
import MediaLoadError from '@/components/media/MediaLoadError.vue'

/** Marks an <img> that already has a placeholder, so a retry cannot stack them. */
const MISSING_ATTR = 'data-media-missing'
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
        placeholder.dispose()
        wrapper?.classList.remove(WRAPPER_CLASS)
    }, { once: true })

    return true
}

let installed = false
let handler: ((e: Event) => void) | null = null

/**
 * Install the document-level capture-phase error listener. Idempotent; safe to
 * call from every app bootstrap. Returns a teardown function (tests / HMR).
 *
 * Scoped to `img.lightbox-img`: that marker is stamped on every content image
 * by the media-block factory (chat, file preview, share, table modal) and by
 * the image viewer. The file manager's own thumbnails deliberately carry it
 * NOT — they have a component-level `@error` that falls back to a type icon,
 * and two competing fallbacks would fight.
 */
export function installLocalMediaFallback(): () => void {
    if (installed || typeof document === 'undefined') return () => {}
    installed = true

    handler = (e: Event) => {
        const target = e.target as Element | null
        if (!target || target.tagName !== 'IMG') return
        const img = target as HTMLImageElement
        if (!img.classList.contains('lightbox-img')) return
        applyMediaMissingFallback(img)
    }

    document.addEventListener('error', handler, true)
    return uninstallLocalMediaFallback
}

/** Tear down the listener (tests / HMR). */
export function uninstallLocalMediaFallback(): void {
    if (handler) document.removeEventListener('error', handler, true)
    handler = null
    installed = false
}
