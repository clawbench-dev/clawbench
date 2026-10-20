import { useDialog } from '@/composables/useDialog'
import { openExternalUrl } from '@/utils/externalLink'
import { isExternalHref, LOCALHOST_LINK_ATTR } from '@/utils/externalLinkNode'
import { appLog } from '@/utils/appLog'

const TAG = 'ExternalLinkClick'
import i18n from '@/i18n'

const t = (key: string) => i18n.global.t(key)

/**
 * Document-level click layer that turns a rendered external link into a
 * confirmed navigation.
 *
 * ## Why the link is not an <a> any more
 *
 * On Android, long-pressing an <a> makes WebView start its own link gesture:
 * the anchor gets selected as a LINK, and the system context menu never appears
 * — so the URL cannot be copied with the ordinary long-press flow. The markdown
 * renderer therefore emits `<span class="md-external-link" data-href="…">` for
 * external URLs (see `markedConfig.ts`). As plain text the characters are
 * selectable, so long-press behaves like on any other text.
 *
 * ## Why a tap asks first
 *
 * A span no longer navigates by itself, and navigating on a single tap is what
 * we must NOT restore: a stray tap while scrolling a chat message would yank
 * the user out to a browser. So a tap opens a confirm dialog showing the exact
 * URL, and only an explicit confirm leaves the app.
 *
 * ## Why one global listener
 *
 * The span can be rendered by every host that renders markdown (chat, task,
 * forge, file preview, share page, tool drawers). A per-host handler would have
 * to be added to each — the same mistake `inertPathClick.ts` documents. One
 * capture-phase listener covers all present and future hosts.
 *
 * Capture phase is deliberate: it runs before a host's own click handler, so
 * `stopPropagation()` keeps a host that still expects an anchor from acting on
 * the same tap (e.g. the local-link guard resolving a relative path).
 */
export function installExternalLinkClick(): () => void {
    function handler(e: MouseEvent) {
        // A drag that selected text ends in a click too. dragClickGuard (installed
        // before this layer — see App.vue) suppresses those with preventDefault.
        if (e.defaultPrevented) return
        // Primary button only; modified clicks keep their usual meaning.
        if (e.button !== 0 || e.metaKey || e.ctrlKey || e.shiftKey || e.altKey) return

        const target = e.target as Element | null
        const span = target?.closest?.<HTMLElement>('.md-external-link')
        if (!span) return

        // Localhost URLs are NOT opened externally. In the App they point at a
        // port on the SERVER, reachable only through the SSH/h2 tunnel; handing
        // them to the system browser lands on the device's own localhost (a
        // dead page). The localhost annotator marks them and the host's own
        // bubble-phase handler opens them in the sandboxed browser — so this
        // capture layer must let the event through untouched.
        //
        // Returning (not stopPropagation) is the whole point: capture runs
        // first, so intercepting here would make the host handler unreachable.
        if (span.hasAttribute(LOCALHOST_LINK_ATTR)) return

        const href = span.getAttribute('data-href')
        if (!href) return

        e.preventDefault()
        e.stopPropagation()
        void confirmAndOpen(href)
    }

    // Keyboard activation, since the span carries role="link" tabindex="0".
    function keyHandler(e: KeyboardEvent) {
        if (e.key !== 'Enter' && e.key !== ' ') return
        // Holding the key would otherwise queue one dialog per repeat.
        if (e.repeat) return
        // Something closer already handled this key.
        if (e.defaultPrevented) return

        const target = e.target as Element | null
        const span = target?.closest?.<HTMLElement>('.md-external-link')
        if (!span) return
        // A converted span can contain a real control — the localhost "open"
        // button, or (before the annotators learned to skip spans) a file chip
        // and its open button. Those handle Enter/Space themselves; firing the
        // link dialog too would open two things at once.
        if (target?.closest?.('button, a[href], input, select, textarea, [contenteditable]')) return

        // Same rule as the click handler: a localhost URL belongs to the
        // tunnel, not the system browser. The host's own keydown path handles
        // it (or nothing happens — far better than a dead page).
        if (span.hasAttribute(LOCALHOST_LINK_ATTR)) return

        const href = span.getAttribute('data-href')
        if (!href) return
        e.preventDefault()
        void confirmAndOpen(href)
    }

    document.addEventListener('click', handler, true)
    document.addEventListener('keydown', keyHandler, true)
    return () => {
        document.removeEventListener('click', handler, true)
        document.removeEventListener('keydown', keyHandler, true)
    }
}

/**
 * Ask before leaving the app, then open the URL externally.
 *
 * The URL is shown in full (not just the host) so the user can tell where they
 * are about to go — this is also the moment they may realise they wanted to
 * copy it instead, which is why the dialog carries the URL as plain text.
 *
 * `href` is RE-VALIDATED here even though the renderer only emits allowed
 * schemes: it arrives from a DOM attribute, and `data-href` is not covered by
 * DOMPurify's URI allowlist (that only inspects `href`/`src`). Raw HTML in an
 * AI message could therefore plant a `javascript:` value. Validating at the
 * sink keeps the guarantee independent of how the element got there.
 */
async function confirmAndOpen(href: string): Promise<void> {
    if (!isExternalHref(href)) {
        appLog.w(TAG, 'refusing to open non-external href', href)
        return
    }
    const ok = await useDialog().confirm(href, {
        title: t('chat.externalLink.title'),
        confirmText: t('chat.externalLink.open'),
        cancelText: t('common.cancel'),
    })
    if (ok) openExternalUrl(href)
}
