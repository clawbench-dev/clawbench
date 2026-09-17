import { appLog } from './appLog'
import { isNativeApp } from './clawbenchNative'

const TAG = 'PWA'
const SW_URL = '/sw.js'

/**
 * Register the PWA service worker — but only where registering one can help and
 * cannot hurt.
 *
 * The worker exists solely to satisfy Chrome's install criteria: the automatic
 * `beforeinstallprompt` event still requires a registered fetch handler. It
 * never writes to Cache Storage (see web/sw.js), so the failure mode that got
 * the previous worker deleted — a cached app shell pointing at chunk hashes
 * that no longer exist, plus /api/* responses answered from the worker — is not
 * reachable from here.
 *
 * Registration is skipped unless every gate passes:
 *
 *   1. the API exists at all;
 *   2. the page is in a secure context. Service workers only run on https or
 *      localhost, and this is what keeps the Android WebView (which loads
 *      http://localhost:{port}) out of the picture without a UA check;
 *   3. this is the top-level document — an embedded iframe must not register a
 *      worker for its host page;
 *   4. this is not the native app, which already has its own install path and
 *      must not show web install affordances;
 *   5. /sw.js is actually served as JavaScript. In Vite dev mode the backend
 *      proxy may be absent, and the SPA fallback then answers /sw.js with
 *      index.html; registering that HTML as a worker script fails with a
 *      confusing syntax error, so probe first.
 *
 * Callers get a boolean rather than a thrown error: a missing PWA install
 * prompt is a degraded feature, never a reason to break app startup.
 */
export async function registerPwaServiceWorker(): Promise<boolean> {
  try {
    if (!('serviceWorker' in navigator)) return false

    // `isSecureContext` is true for https and for localhost/127.0.0.1. Using it
    // instead of parsing the protocol also covers browsers that treat other
    // origins as trustworthy.
    if (!window.isSecureContext) return false

    // Cross-origin `window.top` access throws; that case is not top-level.
    if (window !== window.top) return false

    if (isNativeApp()) return false

    if (!(await isServiceWorkerScript(SW_URL))) {
      appLog.i(TAG, 'Service Worker skipped: not served as JavaScript')
      return false
    }

    // updateViaCache: 'none' keeps the HTTP cache out of the update path. The
    // browser revalidates sw.js on every navigation, so a worker fix ships as
    // soon as the file changes — without it a long max-age could pin clients to
    // a broken worker with no way to update them (a worker cannot be replaced
    // by one that is never fetched).
    const registration = await navigator.serviceWorker.register(SW_URL, {
      scope: '/',
      updateViaCache: 'none',
    })
    appLog.i(TAG, 'Service Worker registered:', registration.scope)
    return true
  } catch (err) {
    appLog.w(TAG, 'Service Worker registration failed:', err)
    return false
  }
}

/**
 * HEAD the worker URL and require a JavaScript content type.
 *
 * Mirrors the guard the project used before the worker was removed: in dev the
 * dev-server SPA fallback answers unknown paths with HTML, and a worker
 * registered from an HTML body throws at install time.
 */
async function isServiceWorkerScript(url: string): Promise<boolean> {
  try {
    const res = await fetch(url, { method: 'HEAD', cache: 'no-store' })
    if (!res.ok) return false
    const contentType = (res.headers.get('content-type') || '').toLowerCase()
    return contentType.includes('javascript') || contentType.includes('ecmascript')
  } catch {
    return false
  }
}
