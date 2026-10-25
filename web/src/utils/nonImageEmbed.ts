/**
 * Repair of `![alt](src)` embeds whose destination is not an image.
 *
 * Why this exists
 * ---------------
 * The AI writes `![设备编排方案](/proj/docs/screenshots/_hero_layouts.html)`
 * when it means "look at this page I just rendered". A browser cannot decode
 * HTML as an image, so the `<img>` fires `error`, `localMediaFallback` swaps in
 * the generic "failed to load" placeholder, and the user sees a broken block
 * instead of the thing the AI meant to show. The system prompt now forbids it,
 * but a prompt is advisory — this is the deterministic half of the fix.
 *
 * The other half of the same failure mode is decoration: the AI copies the
 * prompt's placeholder literally and writes ``![x](`.clawbench/a.png`)``.
 * CommonMark keeps the backticks as part of the destination and marked
 * percent-encodes them (`%60`), so the request targets a filename that does
 * not exist. `parseFileUri` already strips such pairs for LINK targets; an
 * embed needs the same treatment or the extension check reads `.png%60` and
 * classifies a real image as foreign.
 *
 * Deliberately a leaf module (only `path.ts`): `chatRenderUtils` is
 * whitelist-mocked by several test files, so a new export there would be
 * `undefined` in those mocks.
 */

import { baseName } from '@/utils/path.ts'

/**
 * Extensions that actually render as a picture in an `![alt](src)` embed.
 *
 * Formats no browser decodes inline (.tiff/.psd/…) are excluded on purpose:
 * those degrade to the "failed to load" placeholder exactly like a .html does.
 */
export const IMAGE_EMBED_EXTENSIONS = ['.png', '.jpg', '.jpeg', '.gif', '.webp', '.svg', '.avif', '.bmp', '.ico']

/**
 * Matching decoration pairs an AI routinely wraps a path in: `` `path` ``,
 * `"path"`, `'path'`, plus their percent-encoded spellings (what marked emits).
 * Only a MATCHING pair is decoration — a lone backtick is a real filename
 * character.
 */
const WRAPPING_PAIRS: ReadonlyArray<readonly [string, string]> = [
    ['`', '`'],
    ['%60', '%60'],
    ["'", "'"],
    ['%27', '%27'],
    ['"', '"'],
    ['%22', '%22'],
]

/** Strip ONE matching wrapping decoration pair from a raw path. */
export function stripEmbedDecoration(raw: string): string {
    for (const [open, close] of WRAPPING_PAIRS) {
        if (raw.length > open.length + close.length && raw.startsWith(open) && raw.endsWith(close)) {
            return raw.slice(open.length, raw.length - close.length)
        }
    }
    return raw
}

/**
 * Where a served URL keeps the real filename.
 *
 * `?target=` / `?path=` carry it for the absolute forms, and the path suffix
 * does for `/api/fs/raw/<rel>` and `/api/share/{token}/local/<rel>`. Without
 * this, every src the render pipeline has already rewritten looks
 * extension-less and would pass as an image.
 */
interface ServedSrc {
    /** Query param holding the target, when the URL uses that form. */
    param: 'target' | 'path' | null
    /** Decoded target path. */
    path: string
    /** True for the thumbnail endpoint, which only serves decodable rasters. */
    thumb: boolean
    /** URL path prefix before the target, for rebuilding the suffix form. */
    prefix: string
}

function splitServedSrc(src: string): ServedSrc | null {
    if (!/^\/api\/(?:fs|share)\//i.test(src)) return null
    let url: URL
    try {
        url = new URL(src, 'http://localhost')
    } catch {
        return null
    }
    const thumb = /\/thumb(?:\?|$)/i.test(url.pathname)
    const target = url.searchParams.get('target')
    const pathParam = url.searchParams.get('path')
    if (target !== null || pathParam !== null) {
        return { param: target !== null ? 'target' : 'path', path: target ?? pathParam ?? '', thumb, prefix: '' }
    }
    const m = url.pathname.match(/^(\/api\/(?:fs|share)\/[^/]+\/)(.*)$/)
    const raw = m ? m[2] : url.pathname
    return { param: null, path: decodeOrRaw(raw), thumb, prefix: m ? m[1] : '' }
}

function decodeOrRaw(s: string): string {
    try {
        return decodeURIComponent(s)
    } catch {
        return s
    }
}

/** The filesystem path an embed ultimately points at. */
function embedTargetPath(src: string): string {
    const served = splitServedSrc(src)
    if (served?.thumb) return 'thumb.png' // only ever serves decodable rasters
    const path = stripEmbedDecoration(served ? served.path : src)
    return path.split(/[?#]/)[0]
}

/**
 * Rewrite an embed src with its decoration removed.
 *
 * Decoration can sit at the ends of the whole src (chat pipeline: the raw
 * markdown destination) or inside the `target`/`path` param (file preview:
 * `fixImagePaths` has already moved the path into a served URL). Both are
 * handled, so a backtick-wrapped real image loads in either pipeline.
 */
export function cleanEmbedSrc(src: string): string {
    const served = splitServedSrc(src)
    if (!served) return stripEmbedDecoration(src)
    const bare = stripEmbedDecoration(served.path)
    if (bare === served.path) return src
    if (served.param === null) {
        // Suffix form (/api/fs/raw/<rel>): rebuild it from the cleaned path so
        // the request stops targeting a decorated filename.
        const encoded = bare.split('/').map(s => encodeURIComponent(s)).join('/')
        const query = src.includes('?') ? src.slice(src.indexOf('?')) : ''
        return served.prefix + encoded + query
    }
    const url = new URL(src, 'http://localhost')
    url.searchParams.set(served.param, bare)
    return url.pathname + url.search
}

/**
 * True when `src` may legitimately be the destination of an image embed.
 *
 * A src with NO extension counts as an image: plenty of legitimate media
 * endpoints serve extension-less URLs, so the extension is the only signal
 * acted on (never "unknown ⇒ broken").
 */
export function isImageEmbedPath(src: string): boolean {
    const name = embedTargetPath(src).split('/').pop() || ''
    const dot = name.lastIndexOf('.')
    if (dot <= 0) return true
    return IMAGE_EMBED_EXTENSIONS.includes(name.slice(dot).toLowerCase())
}

/**
 * Downgrade every non-image embed in the document to a plain link, and strip
 * decoration from the ones that are real images. Mutates in place.
 *
 * Runs on a parsed Document (not an HTML string) so the replacement is built
 * with DOM APIs and cannot inject markup. An embed already inside a link keeps
 * its outer anchor — an `<a>` cannot nest in an `<a>`, so the image becomes its
 * label text there instead.
 */
export function downgradeNonImageEmbedsIn(doc: Document): void {
    for (const img of Array.from(doc.querySelectorAll('img[src]'))) {
        const raw = img.getAttribute('src') || ''
        // data: URIs are inline bytes with no filename to judge.
        if (!raw || raw.startsWith('data:')) continue

        if (isImageEmbedPath(raw)) {
            const bare = cleanEmbedSrc(raw)
            if (bare !== raw) img.setAttribute('src', bare)
            continue
        }

        const alt = (img.getAttribute('alt') || '').trim()
        const label = alt || baseName(embedTargetPath(raw))

        if (img.closest('a')) {
            img.replaceWith(doc.createTextNode(label))
            continue
        }

        // An image already lifted into the unified figure (the file-preview
        // pipeline rewrites and wraps images BEFORE this runs) would leave an
        // empty shell behind: a header of view/attach buttons over nothing.
        const figure = img.closest('.image-block-wrapper')

        const link = doc.createElement('a')
        // The href must be the REAL path, not the serving URL: in the file
        // preview pipeline the src has already been rewritten to
        // /api/fs/raw/?target=…, and handing that to the file-path annotator
        // makes it parse the endpoint instead of the file ("glob pattern, not a
        // file path"), leaving a dead chip. embedTargetPath recovers the path.
        link.setAttribute('href', embedTargetPath(raw))
        link.textContent = label
        img.replaceWith(link)

        if (figure && !figure.querySelector('img, svg.lightbox-svg')) {
            figure.replaceWith(link) // moves the link out, drops the empty shell
        }
    }
}
