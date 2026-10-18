/**
 * Single source of truth for how an EXTERNAL link is rendered.
 *
 * ## Why external links are not <a>
 *
 * On Android, long-pressing an <a> makes WebView start its own link gesture:
 * the anchor is selected as a LINK and the system context menu never appears,
 * so the URL cannot be copied with the ordinary long-press flow. Rendering the
 * URL as plain text restores that — the characters are selectable like any
 * other text.
 *
 * ## Why one module
 *
 * External links reach the DOM through SEVERAL independent paths:
 *
 *   1. marked's `link` renderer (markdown `[x](https://…)`, autolinks)
 *   2. `annotateBareUrlsIn` (schemeless `github.com/foo/bar`)
 *   3. `annotateLocalhostIn` (a bare `localhost:5173` / `<code>` URL)
 *
 * Each used to build its own element. Converting only one of them produced a
 * half-migrated UI where some URLs were still anchors — clickable and
 * long-press-hostile — while others were spans. Every path must therefore go
 * through {@link createExternalLinkNode}.
 *
 * ## The element
 *
 * A `<span class="md-external-link" data-href="…">` carrying:
 *   - `role="link"` + `tabindex="0"` so keyboard users can still reach it
 *     (the click layer handles Enter/Space);
 *   - the visible text unchanged, so the URL stays copyable and searchable;
 *   - `data-href` instead of `href` — nothing should treat it as navigation
 *     except our own handler.
 *
 * The click layer that turns a tap into a confirmed navigation lives in
 * `utils/externalLinkClick.ts`.
 */

/** Class name shared by the renderer, the click layer and the CSS. */
export const EXTERNAL_LINK_CLASS = 'md-external-link'

/**
 * Media files must keep their <a> even when the URL is external.
 *
 * `convertAudioLinks` / `convertVideoLinks` (chatRenderUtils) rewrite
 * `<a href="…mp3">` into an inline <audio>/<video> player, matching on the
 * ANCHOR. Turning those into spans silently degrades every media link back to
 * plain text. The list mirrors the extensions those converters accept.
 */
const MEDIA_EXTENSIONS = [
    '.mp3', '.wav', '.ogg', '.m4a', '.aac', '.flac', '.wma', '.opus',
    '.mp4', '.mkv', '.avi', '.mov', '.webm', '.flv', '.wmv', '.m4v', '.3gp', '.m3u8',
]

/**
 * Schemes that leave the app.
 *
 * Deliberately NARROWER than it once was: `cid:` and `xmpp:` were inherited
 * from DOMPurify's list but mean nothing to `openExternalUrl`, and every scheme
 * listed here widens the `data-href` sink (see below). Only schemes the OS can
 * actually hand to a browser or dialer belong here.
 *
 * ## Why this list is a security boundary, not a convenience
 *
 * `data-href` is a plain `data-*` attribute, so DOMPurify's
 * `ALLOWED_URI_REGEXP` does NOT apply to it — that regexp only inspects URI
 * attributes (`href`, `src`, …). Raw HTML in an AI message is a legal markdown
 * token, so without a check here a message could carry
 * `<span class="md-external-link" data-href="javascript:alert(1)">`, which the
 * click layer would hand to `openExternalUrl` → `a.click()` in browser mode.
 * The renderer only emits spans for URLs that pass this test, and the click
 * layer re-validates (a DOM attribute can be set by anything).
 */
const EXTERNAL_HREF_RE = /^(?:(?:f|ht)tps?:|mailto:|tel:|callto:|sms:|\/\/)/i

/** True when `href` points outside the app (http/https/mailto/…). */
export function isExternalHref(href: string): boolean {
    return EXTERNAL_HREF_RE.test(href.trim())
}

/**
 * True when the URL points at a file the media converters turn into a player.
 *
 * Deliberately does NOT strip a query/hash: `convertAudioLinks` /
 * `convertVideoLinks` compare the RAW href with `endsWith(ext)`, so
 * `song.mp3?t=1` is NOT converted — a deliberate rule pinned by their tests
 * ("handles links with query parameters"). If this function stripped the query
 * it would classify such a URL as media, keep the `<a>`, and then no converter
 * would match it: neither a player nor the long-press fix. The two must agree.
 */
export function isMediaHref(href: string): boolean {
    const lower = href.trim().toLowerCase()
    return MEDIA_EXTENSIONS.some(ext => lower.endsWith(ext))
}

/**
 * Whether this URL should be rendered as text rather than an anchor.
 *
 * Media URLs are excluded because the player converters need the anchor; every
 * other external URL is converted.
 */
export function shouldRenderAsText(href: string): boolean {
    return isExternalHref(href) && !isMediaHref(href)
}

/**
 * Normalise a URL the way marked does for anchors.
 *
 * `encodeURI` percent-encodes non-ASCII and spaces, then `%25` is restored to
 * `%` so an already-encoded path is not double-encoded. Kept identical to
 * `defaultLinkRender` / marked v18 so a URL reads the same whether it ended up
 * as an anchor or as one of our spans.
 *
 * Returns null when the input cannot be encoded (unpaired surrogate), matching
 * marked's behaviour of falling back to the link text.
 */
export function cleanExternalHref(href: string): string | null {
    try {
        return encodeURI(href).replace(/%25/g, '%')
    } catch {
        return null
    }
}

/**
 * Build the text element for an external URL.
 *
 * `text` defaults to the URL itself (bare-URL annotations show the URL). A
 * markdown link passes its label instead, so `[site](https://…)` keeps reading
 * as "site".
 *
 * `doc` is required rather than defaulting to `document`: the annotation passes
 * work on a DETACHED document (DOMParser output), and creating a node from the
 * live document would make `replaceChild` throw a WRONG_DOCUMENT_ERR.
 */
export function createExternalLinkNode(
    doc: Document,
    href: string,
    text: string = href,
    title?: string,
): HTMLElement {
    const span = doc.createElement('span')
    span.className = EXTERNAL_LINK_CLASS
    span.setAttribute('data-href', cleanExternalHref(href) ?? href)
    if (title) span.setAttribute('title', title)
    span.setAttribute('role', 'link')
    span.setAttribute('tabindex', '0')
    span.textContent = text
    return span
}
