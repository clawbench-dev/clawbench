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

/** Schemes that leave the app. */
const EXTERNAL_HREF_RE = /^(?:(?:f|ht)tps?:|mailto:|tel:|callto:|sms:|cid:|xmpp:|\/\/)/i

/** True when `href` points outside the app (http/https/mailto/…). */
export function isExternalHref(href: string): boolean {
    return EXTERNAL_HREF_RE.test(href.trim())
}

/** True when the URL points at a file the media converters turn into a player. */
export function isMediaHref(href: string): boolean {
    // Strip query/hash so `song.mp3?t=1` still matches.
    const path = href.trim().split(/[?#]/)[0].toLowerCase()
    return MEDIA_EXTENSIONS.some(ext => path.endsWith(ext))
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
    span.setAttribute('data-href', href)
    if (title) span.setAttribute('title', title)
    span.setAttribute('role', 'link')
    span.setAttribute('tabindex', '0')
    span.textContent = text
    return span
}
