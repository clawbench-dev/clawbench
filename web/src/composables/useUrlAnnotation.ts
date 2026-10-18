/**
 * Bare-URL annotation.
 *
 * marked's GFM autolinker already turns `https://…`, `http://…` and `www.…`
 * into <a> tags, so those never reach the file-path annotator. A SCHEMELESS
 * domain does not: `github.com/foo/bar` stays plain text, and the file-path
 * annotator then reads the tail after the first dot as a path — rendering a
 * dead `com/foo/bar` chip that can never resolve.
 *
 * This step runs BEFORE the file-path annotator and wraps such domains in <a>,
 * which both makes them clickable and removes them from file-path detection
 * (that annotator skips any text inside an <a>).
 *
 * WHAT MATCHES: a domain-like first segment (a dot plus a known TLD) followed by
 * AT LEAST ONE path segment. The path requirement is what separates a URL from
 * a filename: `build.sh`, `cfg.FR` and `useSessionIdentity.app` all look like
 * `name.tld` but carry no path, and all three occur as real identifiers in this
 * codebase — linkifying them would break working file annotations.
 *
 * NO LOOKBEHIND: the left boundary is a captured prefix character rather than
 * `(?<!…)`, because lookbehind is a *syntax* error on Safari/iPadOS < 16.4 that
 * takes the entire bundle down (the same constraint documented on
 * INLINE_MATH_RE in useMarkdownRenderer.ts). The prefix is captured and
 * re-emitted instead.
 *
 * BOUNDED: both the path-segment length and the segment count are capped. An
 * unbounded segment run is quadratic on long slash-heavy input and would trip
 * the renderer's own 1 s hang guard; the caps sit far above any real URL.
 */

import { annotateOneExternalLink } from '@/composables/useExternalLinkAnnotation'

/**
 * TLDs accepted as the domain suffix. A fixed list rather than `[a-z]{2,}`:
 * an open suffix would match every `name.ext` pair, which is exactly the
 * filename collision this rule exists to avoid.
 */
const TLDS = [
    'com', 'org', 'net', 'io', 'dev', 'cn', 'ai', 'edu', 'gov', 'app',
    'co', 'me', 'xyz', 'top', 'info', 'gg', 'tv', 'cc', 'tech', 'site',
    'online', 'sh', 'de', 'fr', 'jp', 'uk', 'us', 'eu', 'ru', 'br',
    'in', 'au', 'ca', 'nl', 'se', 'pl', 'it', 'es', 'ch', 'be',
]

/** Characters allowed in a bare-URL path segment. */
const PATH_SEG = '[a-zA-Z0-9_.\\-~%]'
/** Max characters per path segment, and max segments. Real URLs are far smaller. */
const BARE_URL_SEG_MAX = 128
const BARE_URL_SEG_COUNT_MAX = 16

/**
 * Trailing punctuation that ends the sentence, not the URL: `See
 * github.com/foo/bar.` must link `github.com/foo/bar`, not the period.
 *
 * Only ASCII `.` is in `PATH_SEG` (the other terminators already stop the match),
 * and it is also a legal path character — `README.md`, `v1.2`. So a trailing dot
 * is stripped only when it is NOT followed by more path text, which the regex
 * cannot express without lookbehind (banned: see the module header). The scan
 * below trims it and leaves it as text instead.
 */
const TRAILING_DOT_RE = /\.$/

/**
 * `(prefix)(domain)(path…)`. The prefix is `^` or one non-path character, so a
 * match never begins mid-token; it is captured (not looked behind) and
 * re-emitted. The path group requires at least one segment — see the header.
 */
const BARE_URL_RE = new RegExp(
    '(^|[^\\w./@-])' +
    '((?:[a-z0-9-]+\\.)+(?:' + TLDS.join('|') + '))' +
    '(?:/' + PATH_SEG + '{1,' + BARE_URL_SEG_MAX + '}){1,' + BARE_URL_SEG_COUNT_MAX + '}',
    'gi',
)

/**
 * Annotate bare URLs inside an already-parsed Document, mutating it in place.
 *
 * The `In` form lets the markdown pipeline run this inside an EXISTING parse
 * phase instead of paying its own `parseFromString` + `body.innerHTML` round
 * trip (those round trips were the dominant main-thread cost in the
 * heavy-session freeze — see the shared-Document note in useMarkdownRenderer).
 *
 * Text inside <a> is skipped: it is already a link (marked's autolink, or an
 * earlier pass). Text inside <code>/<pre> IS annotated, matching the localhost
 * annotator — a URL in a code sample is normally meant to be clickable, and
 * leaving it alone would keep the dead-chip bug there.
 *
 * @returns true when anything was annotated, so callers can skip serializing.
 */
export function annotateBareUrlsIn(doc: Document): boolean {
    const nodes: Text[] = []
    const walker = doc.createTreeWalker(doc.body, NodeFilter.SHOW_TEXT, {
        acceptNode(node: Text) {
            const parent = node.parentElement
            if (!parent) return NodeFilter.FILTER_REJECT
            if (parent.closest('a')) return NodeFilter.FILTER_REJECT
            return NodeFilter.FILTER_ACCEPT
        },
    })
    while (walker.nextNode()) nodes.push(walker.currentNode as Text)

    let origin = ''
    try {
        origin = window.location.origin
    } catch {
        // Non-browser environment — treat every link as external.
    }

    let annotated = false
    // Reverse order so DOM insertions cannot invalidate nodes still to process.
    for (let i = nodes.length - 1; i >= 0; i--) {
        const textNode = nodes[i]
        const text = textNode.textContent || ''
        BARE_URL_RE.lastIndex = 0
        if (!BARE_URL_RE.test(text)) continue

        BARE_URL_RE.lastIndex = 0
        const frag = doc.createDocumentFragment()
        let last = 0
        let match: RegExpExecArray | null
        let found = false
        while ((match = BARE_URL_RE.exec(text)) !== null) {
            const prefix = match[1]
            // match[2] is the domain only; the path lives in a non-capturing
            // group, so take the whole remainder of the match.
            let url = match[0].slice(prefix.length)
            // A sentence-final period is not part of the URL (see
            // TRAILING_DOT_RE). Give it back to the surrounding text.
            const trailing = TRAILING_DOT_RE.exec(url)
            if (trailing) url = url.slice(0, -trailing[0].length)
            const urlStart = match.index + prefix.length
            if (urlStart > last) frag.appendChild(doc.createTextNode(text.slice(last, urlStart)))
            const a = doc.createElement('a')
            a.setAttribute('href', 'https://' + url)
            a.textContent = url
            annotateOneExternalLink(a, origin)
            frag.appendChild(a)
            found = true
            last = urlStart + url.length
        }
        if (!found) continue
        if (last < text.length) frag.appendChild(doc.createTextNode(text.slice(last)))
        textNode.parentNode?.replaceChild(frag, textNode)
        annotated = true
    }

    return annotated
}

/**
 * String wrapper around `annotateBareUrlsIn` for callers that hold HTML rather
 * than a Document. The markdown pipeline uses the `In` form.
 */
export function annotateBareUrls(html: string): string {
    if (!html) return html
    const doc = new DOMParser().parseFromString(html, 'text/html')
    if (!annotateBareUrlsIn(doc)) return html
    return doc.body.innerHTML
}
