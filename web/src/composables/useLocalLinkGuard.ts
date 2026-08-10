import { isExternalLink, isAnchorLink } from '@/utils/doubleClickUtils'

/**
 * Decide whether a link should be intercepted as an in-app file reference.
 * Downloads, backend endpoints (/api/…), and resource URLs (blob:/data:) are
 * intentionally left to the browser; anchors and external links pass through.
 */
function shouldIntercept(anchor: HTMLAnchorElement, href: string): boolean {
    // Downloads are handled by the browser or native bridge — never intercept.
    if (anchor.hasAttribute('download')) return false
    // Backend endpoints and resource URLs are not file paths.
    if (href.startsWith('/api/') || href.startsWith('blob:') || href.startsWith('data:')) return false
    // Anchor (#section) and external (http/https/mailto/…) links pass through.
    if (isAnchorLink(href) || isExternalLink(href)) return false
    // Local / relative / file link.
    return true
}

/**
 * Document-level bubble-phase guard for local/relative/file links.
 *
 * Once DOMPurify allows the `file:` scheme, any `<a href="file://…">` in
 * rendered markdown could be followed by the browser (which cannot load a
 * file: URL from a web context and is a security concern). Render sites that
 * use useDoubleClickCopy already preventDefault local links, but sites that
 * don't (e.g. tool details, table modals) would let the browser navigate.
 *
 * This guard runs on the document in the bubble phase and only acts as a
 * last-resort fallback: it skips any event already defaultPrevented by a
 * site-specific handler, modified clicks (ctrl/cmd to open in a new tab),
 * downloads, backend endpoints, and external/anchor links.
 */
export function initLocalLinkGuard(onOpenLocal: (href: string) => void): () => void {
    function handler(e: MouseEvent) {
        if (e.defaultPrevented) return
        if (e.button !== 0 || e.metaKey || e.ctrlKey || e.shiftKey || e.altKey) return
        const target = e.target as Element | null
        const anchor = target?.closest?.<HTMLAnchorElement>('a[href]')
        if (!anchor) return
        const href = anchor.getAttribute('href')
        if (!href) return
        if (!shouldIntercept(anchor, href)) return
        // Local / relative / file link that no site handler opened.
        e.preventDefault()
        onOpenLocal(href)
    }

    document.addEventListener('click', handler)
    return () => document.removeEventListener('click', handler)
}
