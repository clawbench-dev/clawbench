import { getNative } from '@/utils/clawbenchNative'
import { appLog } from '@/utils/appLog'

const TAG = 'ExternalLink'

/**
 * Open an external URL (project homepage, issue tracker, …) outside the app.
 *
 * The three hosts disagree about what "open externally" means, and getting it
 * wrong is silent rather than loud:
 *   - Android WebView: the shell never calls `setSupportMultipleWindows`, so
 *     `window.open(url, '_blank')` is swallowed — no navigation, no error, the
 *     tap just does nothing. The bridge hands the URL to the system browser.
 *   - Electron: `setWindowOpenHandler` would route a new window to
 *     `shell.openExternal` anyway, but going through the bridge is explicit
 *     and lets the main process enforce its own scheme allow-list.
 *   - Plain browser: an anchor click, which the browser handles natively (and
 *     which keeps middle-click / "copy link" working if this ever becomes a
 *     real link).
 *
 * `rel="noopener noreferrer"` on the browser path mirrors the markdown
 * renderer's external-link annotation: without it the opened page gets a
 * handle on `window.opener` and can navigate this tab elsewhere.
 */
export function openExternalUrl(url: string): void {
    if (!url) return

    const native = getNative()
    if (native?.openExternalUrl) {
        Promise.resolve(native.openExternalUrl(url)).catch((err) => {
            appLog.w(TAG, `native openExternalUrl failed: ${url}`, err)
        })
        return
    }

    const a = document.createElement('a')
    a.href = url
    a.target = '_blank'
    a.rel = 'noopener noreferrer'
    document.body.appendChild(a)
    a.click()
    // Delay cleanup so the navigation has been initiated before the anchor is
    // detached (same pattern as download.ts).
    setTimeout(() => a.remove(), 1000)
}
