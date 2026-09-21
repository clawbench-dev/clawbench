import { useAppMode } from '@/composables/useAppMode.ts'

/**
 * External-link annotation composable.
 *
 * Markdown links render through marked's default link renderer, which emits a
 * bare `<a href="…">` with no `target`. In a browser that means an external
 * link navigates the CURRENT page — the whole app UI is replaced by the target
 * site, and the user's session view is lost. Stamping `target="_blank"` lets
 * the browser open it in a new tab, which is what every other link surface in
 * the app already does (forge panels, upgrade dialogs, attachments).
 *
 * Only the browser (web) mode needs this. The native hosts already intercept
 * external links themselves and would be HARMED by the attribute:
 *   - Android: `MainActivity.shouldOverrideUrlLoading` hands non-server hosts
 *     to the system browser. The WebView has no multi-window support enabled
 *     (`setSupportMultipleWindows` is never called), so a `target="_blank"`
 *     link is silently swallowed — neither navigated nor dispatched to the
 *     override — turning a working link into a dead one.
 *   - Electron: `will-navigate` / `setWindowOpenHandler` in `window.ts` route
 *     external URLs to `shell.openExternal`.
 * Hence the `isAppMode` gate: same markup decision, host-appropriate.
 *
 * What is annotated (and what is not):
 *   - http:// and https:// links pointing OUTSIDE the current origin, plus
 *     protocol-relative `//host/…` links → annotated.
 *   - Same-origin absolute links → left alone. They are in-app navigation (the
 *     server serves the app itself), and opening them in a new tab would spawn
 *     a second app instance — which the single-tab guard then blocks.
 *   - Relative / `file://` links → left alone; they are local file references
 *     handled by the file-path annotator.
 *   - `#anchor` links → left alone (in-page jumps).
 *   - `mailto:` / `tel:` / other schemes → left alone. They have no "page" to
 *     replace, and `window.open`-style handling of them is browser-specific.
 *   - `/api/…` and `blob:` / `data:` → left alone (backend + resource URLs).
 */

/**
 * Schemes that should open in a new tab. `//host/…` (protocol-relative) is
 * included because it resolves to http(s) on the page's own protocol.
 */
const NEW_TAB_SCHEME_RE = /^(?:https?:)?\/\//i

/** True for a link that leaves the current origin (or is protocol-relative). */
export function isNewTabLink(href: string, origin: string): boolean {
    if (!href) return false
    // Protocol-relative and mailto:/tel:/file:/#/relative forms are excluded by
    // the scheme test below — only http(s) and `//` reach the origin check.
    if (!NEW_TAB_SCHEME_RE.test(href)) return false
    // Protocol-relative: resolves against the page's protocol, so it is
    // same-origin exactly when the host matches.
    if (href.startsWith('//')) {
        if (!origin) return true
        try {
            const url = new URL(`${window.location.protocol}${href}`)
            return url.origin !== origin
        } catch {
            return true
        }
    }
    if (!origin) return true
    try {
        return new URL(href).origin !== origin
    } catch {
        // Unparseable absolute URL — treat as external rather than silently
        // navigating away in place.
        return true
    }
}

/**
 * Merge `noopener noreferrer` into an existing rel value.
 *
 * The marked default renderer does not emit `rel`, but annotated markup can
 * already carry one (e.g. the localhost annotator adds `rel="noopener"`), and
 * re-rendering must not duplicate or drop those tokens.
 */
function mergeRel(existing: string | null): string {
    const tokens = new Set((existing || '').split(/\s+/).filter(Boolean))
    tokens.add('noopener')
    tokens.add('noreferrer')
    return Array.from(tokens).join(' ')
}

/**
 * Annotate external `<a href>` tags inside an already-parsed Document, mutating
 * it in place.
 *
 * Split out of `annotateExternalLinkTargets` so the markdown pipeline can run
 * it inside an EXISTING parse phase instead of paying its own
 * `parseFromString` + `body.innerHTML` round trip. Those round trips were the
 * dominant main-thread cost in the heavy-session freeze (see
 * useMarkdownRenderer's shared-Document note), so this step deliberately
 * piggybacks on a phase that already parses.
 *
 * @returns false when the step is a no-op (native app mode), so callers can
 *   skip serializing the document.
 */
export function annotateExternalLinkTargetsIn(doc: Document): boolean {
    const { isAppMode } = useAppMode()
    // Native hosts handle external links themselves and lack multi-window
    // support — see the module header.
    if (isAppMode.value) return false

    let origin = ''
    try {
        origin = window.location.origin
    } catch {
        // Non-browser environment (or opaque origin) — treat every http(s)
        // link as external.
    }

    for (const a of doc.querySelectorAll('a[href]')) {
        const href = a.getAttribute('href') || ''
        if (!isNewTabLink(href, origin)) continue
        a.setAttribute('target', '_blank')
        a.setAttribute('rel', mergeRel(a.getAttribute('rel')))
    }
    return true
}

/**
 * String wrapper around `annotateExternalLinkTargetsIn` for callers that hold
 * HTML rather than a Document. The markdown pipeline uses the `In` form.
 */
export function annotateExternalLinkTargets(html: string): string {
    if (!html) return html
    const doc = new DOMParser().parseFromString(html, 'text/html')
    if (!annotateExternalLinkTargetsIn(doc)) return html
    return doc.body.innerHTML
}
