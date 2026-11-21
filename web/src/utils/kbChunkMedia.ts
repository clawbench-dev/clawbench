/**
 * Serve the images a knowledge-base document references.
 *
 * A chunk is the text that came out of a file, so its markdown still contains
 * the ORIGINAL relative srcs (`![](../images/stats.png)`). Rendered raw, those
 * resolve against the web app's own URL — the request 404s and the browser
 * paints its broken-image glyph, which is what made the document drawer look
 * broken: no frame, no label, a stray alt text.
 *
 * Two things happen here instead:
 *
 *  - a local src is resolved against the SOURCE FILE's directory and rewritten
 *    to the same served URL the rest of the app uses (`/api/fs/raw/?target=…`,
 *    with the thumbnail form for raster formats).
 *  - every <img> gets the `lightbox-img` marker, so `localMediaFallback` treats
 *    it as content media: an image that still cannot be fetched is swapped for
 *    the shared placeholder card ("could not load" + file name) instead of the
 *    browser's glyph.
 *  - it also gets `data-attach-src`, the marker the media-block factory reads as
 *    "this is a local file" — that is what puts the attach / open-file buttons
 *    on the figure's toolbar, like a picture in the file preview has.
 *
 * Deliberately NOT the chat pipeline (`rewriteImageUrls`): that one resolves
 * against the project root and stamps `chat-img` (chat sizing). A passage is a
 * fragment of a document, not a message.
 */

import { isLocalMediaSrc } from '@/utils/mediaSrc.ts'
import { dirName, isAbsolutePath, normalizeSlashes } from '@/utils/path.ts'
import { escapeHtml as escapeAttr } from '@/utils/html.ts'

/** Extensions `/api/fs/thumb` can rasterize (see chatRenderUtils.THUMB_EXTENSIONS). */
const THUMB_EXTENSIONS = ['.png', '.jpg', '.jpeg']

/** Same widths the chat pipeline asks for; a drawer is never wider than a message. */
const THUMB_WIDTH_WIDE = 1200
const THUMB_WIDTH_NARROW = 640

/**
 * Rewrite every `<img src>` inside a rendered chunk.
 *
 * @param html         Rendered HTML of one passage.
 * @param sourcePath   The ORIGINAL document's path (empty when unknown); a
 *                     relative src resolves against its directory.
 * @param opts.isWideScreen  Viewport width class — picks the thumbnail width.
 * @param opts.stamp   Cache-buster, so a re-parsed document does not reuse a
 *                     stale thumbnail.
 */
export function resolveChunkImages(
    html: string,
    sourcePath: string,
    opts: { isWideScreen: boolean; stamp?: number },
): string {
    if (!html.includes('<img')) return html
    const baseDir = sourcePath ? dirName(sourcePath) : ''
    const thumbWidth = opts.isWideScreen ? THUMB_WIDTH_WIDE : THUMB_WIDTH_NARROW
    const stamp = String(opts.stamp ?? Date.now())

    return html.replace(/<img\b([^>]*)>/gi, (match: string, attrs: string) => {
        const srcMatch = attrs.match(/\bsrc\s*=\s*"([^"]*)"/i)
        if (!srcMatch) return mark(attrs)
        const rawSrc = srcMatch[1]
        if (!rawSrc || !isLocalMediaSrc(rawSrc)) return mark(attrs)

        // Marked percent-encodes what it writes into `src`, so a CJK filename
        // arrives as `%E5%9B%BE…`. The served forms carry the REAL path; only
        // their own query encoding applies afterwards.
        const src = decodeOnce(rawSrc)
        const target = isAbsolutePath(src) ? normalizeSlashes(src) : joinUnder(baseDir, src)

        const full = `/api/fs/raw/?target=${encodeURIComponent(target)}&t=${stamp}`
        const attach = ` data-attach-src="${escapeAttr(target)}"`
        if (!isThumbExtension(src)) {
            return mark(attrs.replace(srcMatch[0], `src="${escapeAttr(full)}" data-full-src="${escapeAttr(full)}"${attach}`))
        }
        const thumb = `/api/fs/thumb?target=${encodeURIComponent(target)}&w=${thumbWidth}&t=${stamp}`
        return mark(attrs.replace(srcMatch[0], `src="${escapeAttr(thumb)}" data-full-src="${escapeAttr(full)}"${attach}`))
    })
}

/** Add the content-media marker without dropping the classes already present. */
function mark(attrs: string): string {
    if (/\bclass\s*=\s*"[^"]*\blightbox-img\b/i.test(attrs)) return `<img${attrs}>`
    if (/\bclass\s*=\s*"/i.test(attrs)) {
        return `<img${attrs.replace(/\bclass\s*=\s*"/i, 'class="lightbox-img ')}>`
    }
    return `<img${attrs} class="lightbox-img">`
}

function decodeOnce(value: string): string {
    try {
        return decodeURIComponent(value)
    } catch {
        return value
    }
}

function isThumbExtension(path: string): boolean {
    const lower = path.toLowerCase()
    return THUMB_EXTENSIONS.some((ext) => lower.endsWith(ext))
}

/**
 * `baseDir/src` with the slashes normalized. `..` segments are kept: the server
 * collapses them when it resolves `?target=`, and collapsing them here would
 * turn a path outside the base into a wrong in-base one.
 */
function joinUnder(baseDir: string, src: string): string {
    if (!baseDir) return normalizeSlashes(src)
    return normalizeSlashes(`${baseDir.replace(/\/+$/, '')}/${src}`)
}
