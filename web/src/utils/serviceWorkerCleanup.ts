import { appLog } from './appLog.ts'

const TAG = 'PWA'

// Manage the optional PWA Service Worker.
//
// This build does not ship an sw.js, but older builds did and a registered
// Service Worker persists in the browser. A stale SW keeps serving a cached
// old index.html whose hashed chunk names (e.g. CodeMirrorViewer-*.js) no
// longer exist after a redeploy, causing 404s (broken code previews, etc.).
//
// Strategy: probe /sw.js. If the server serves a valid worker, register it.
// Otherwise unregister any leftover worker and clear its caches so the app
// always fetches fresh assets.
export function manageServiceWorker(): Promise<void> {
  if (!('serviceWorker' in navigator)) return Promise.resolve()
  if (document.readyState === 'complete') {
    return checkAndRegister()
  }
  return new Promise((resolve) => {
    window.addEventListener('load', () => resolve(checkAndRegister()), { once: true })
  })
}

async function checkAndRegister(): Promise<void> {
  try {
    const res = await fetch('/sw.js', { method: 'HEAD' })
    const ct = res.headers.get('content-type') || ''
    if (res.ok && (ct.includes('javascript') || ct.includes('application/octet-stream'))) {
      try {
        const registration = await navigator.serviceWorker.register('/sw.js')
        appLog.i(TAG, 'Service Worker registered:', registration.scope)
      } catch (error) {
        appLog.w(TAG, 'Service Worker registration failed:', error)
      }
    } else {
      appLog.i(TAG, 'Service Worker skipped: invalid response', res.status, ct)
      await unregisterStaleServiceWorker()
    }
  } catch (error) {
    appLog.w(TAG, 'Service Worker fetch check failed:', error)
    await unregisterStaleServiceWorker()
  }
}

export async function unregisterStaleServiceWorker(): Promise<void> {
  if (!('serviceWorker' in navigator)) return
  try {
    const registrations = await navigator.serviceWorker.getRegistrations()
    for (const reg of registrations) {
      const ok = await reg.unregister()
      appLog.i(TAG, 'Unregistered stale Service Worker' + (ok ? '' : ' (failed)'), reg.scope)
    }
  } catch (error) {
    appLog.w(TAG, 'Failed to unregister stale Service Worker:', error)
  }
  if (window.caches && window.caches.keys) {
    try {
      const keys = await window.caches.keys()
      await Promise.all(keys.map((key) => window.caches.delete(key)))
    } catch (error) {
      appLog.w(TAG, 'Failed to clear caches:', error)
    }
  }
}
