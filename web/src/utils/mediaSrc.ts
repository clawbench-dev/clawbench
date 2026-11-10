/**
 * Which media srcs point at a local file, and how a streaming turn defers them.
 *
 * Why this is one module
 * ----------------------
 * Two independent callers must agree on exactly the same set of srcs:
 *
 *  - `chatRenderUtils.resolveMediaSrc` rewrites "this is a local file" into a
 *    served `/api/fs/raw/…` URL at the END of a turn.
 *  - the STREAMING render cannot do that rewrite yet (it is deliberately the
 *    cheap path), so it must instead refuse to fetch the src at all.
 *
 * If the two disagree, either the stream fetches a path that the rewrite will
 * never produce (a guaranteed 404 per chunk), or a real image is parked and
 * stays invisible until the turn ends. Keeping the classifier in one place is
 * what makes them agree by construction. Leaf module: no internal imports.
 *
 * Why parking exists
 * ------------------
 * A streaming block is re-rendered on every chunk, so an `<img src="docs/a.png">`
 * would fire the same doomed request dozens of times per second and paint the
 * browser's broken glyph before the placeholder can replace it. During a turn a
 * local path can NEVER resolve — the rewrite only runs when the turn settles —
 * so the request is not "early", it is wrong.
 *
 * `localMediaFallback.ts` turns a parked image into the quiet placeholder
 * immediately (no request, no failure, no round trip). The destination is kept
 * in `data-local-src` so the placeholder can still name the file.
 */

/**
 * Media src forms that are NOT local files and must never be rewritten (or
 * parked): remote URLs, protocol-relative URLs, inline bytes, and URLs already
 * served by our own file endpoints (a re-render can see those).
 *
 * `blob:` is inline bytes too — it has no file behind it, so treating it as a
 * relative path (the old behaviour without `blob:` in this list) rewrote it to
 * `/api/fs/raw/blob:…`, which cannot exist.
 */
export const NON_LOCAL_MEDIA_SRC_RE = /^(https?:|blob:|\/\/|data:|\/api\/fs\/)/i

/** True when `src` names a file the render pipeline has to serve itself. */
export function isLocalMediaSrc(src: string): boolean {
    return !!src && !NON_LOCAL_MEDIA_SRC_RE.test(src)
}

/**
 * Attribute holding an image destination that the streaming render deliberately
 * did NOT put in `src`. Read by `localMediaFallback.ts` (name for the
 * placeholder) and by nothing else.
 */
export const UNRESOLVED_SRC_ATTR = 'data-local-src'

/**
 * Move every local image destination out of `src` into `UNRESOLVED_SRC_ATTR`,
 * so inserting the markup cannot start a request. Mutates the Document in place.
 *
 * Only for a still-streaming render: once the turn settles the pipeline rewrites
 * these paths into served URLs and the images load normally.
 *
 * Runs after `downgradeNonImageEmbedsIn` (a `.html` embed has become a link by
 * then and is no longer an image) and after DOMPurify (a `data-*` attribute
 * added before sanitizing would have to be allow-listed).
 */
export function parkUnresolvedImagesIn(doc: Document): void {
    for (const img of Array.from(doc.querySelectorAll('img[src]'))) {
        const src = img.getAttribute('src') || ''
        if (!isLocalMediaSrc(src)) continue
        img.setAttribute(UNRESOLVED_SRC_ATTR, src)
        img.removeAttribute('src')
    }
}
