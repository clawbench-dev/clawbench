/*
 * ClawBench Service Worker — installability shim only.
 *
 * WHY THIS FILE IS SO CAREFUL
 *
 * A service worker is only needed here because Chrome's automatic install
 * prompt (`beforeinstallprompt`) still requires a registered fetch handler.
 * Everything a service worker can do to a site is also a way to break it, and
 * this app has already been broken by one:
 *
 *   - the previous worker cached successful non-API responses, so a rebuilt
 *     index.html kept being served from cache while pointing at chunk hashes
 *     that no longer existed;
 *   - it intercepted /api/* (including the login flow) and answered with a
 *     synthetic 503 when offline, which dropped session cookies and turned
 *     every request into a 403.
 *
 * So this worker deliberately does NOT touch Cache Storage at all:
 * no caches.open, no cache.put, no cache.addAll, anywhere. With no cache
 * writes there is no such thing as a stale cached asset, and nothing here can
 * serve a response the server would not have served.
 *
 * The Static Routing API (Chrome 132+) removes even the interception: routed
 * paths are fetched straight from the network by the browser, without booting
 * the worker. Browsers without the API ignore the registration and fall back
 * to the pass-through fetch handler below, which is behaviourally equivalent.
 */

// Bump on every behavioural change. The browser compares the script
// byte-for-byte, so any edit here already triggers an update; this constant
// exists to make the intent explicit and to give the log line something to say.
const SW_VERSION = 'clawbench-sw-1';

/**
 * Paths that must never be served by the worker.
 *
 * `source: 'network'` means the browser fetches these directly and the worker
 * is not started at all — no boot cost, and structurally no chance of the
 * worker answering for them. Anything carrying credentials or the app shell
 * belongs here:
 *   - /api/*    authenticated JSON + streaming; a worker in this path is how
 *               the cookie/403 incident happened.
 *   - /login    auth entry point.
 *   - /share/*  token-scoped public pages; must always be fresh.
 *   - /sw.js    the worker script itself, so an update check is never answered
 *               by a running worker.
 *   - /manifest.json, /assets/*, /, /index.html  the installable app shell.
 */
const NETWORK_ONLY_PATHS = [
  '/',
  '/index.html',
  '/api/*',
  '/login',
  '/share/*',
  '/sw.js',
  '/manifest.json',
  '/assets/*',
];

self.addEventListener('install', (event) => {
  // Static routing is Chromium-only and still behind a feature check on older
  // builds, so the guard is required rather than defensive noise.
  if (typeof event.registerRouter === 'function') {
    try {
      event.registerRouter(
        NETWORK_ONLY_PATHS.map((pathname) => ({
          condition: { urlPattern: { pathname } },
          source: 'network',
        })),
      );
    } catch (err) {
      // A rejected routing table must not fail the install: the fetch handler
      // below is the fallback and produces the same responses.
      console.warn('[SW] registerRouter failed, falling back to fetch handler', err);
    }
  }
  self.skipWaiting();
});

self.addEventListener('activate', (event) => {
  // One-time cleanup of whatever an older worker cached. This worker never
  // writes to Cache Storage, so afterwards the list is empty for good.
  event.waitUntil(
    (async () => {
      try {
        const keys = await caches.keys();
        await Promise.all(keys.map((key) => caches.delete(key)));
        if (keys.length > 0) {
          console.log('[SW]', SW_VERSION, 'cleared', keys.length, 'legacy cache(s)');
        }
      } catch (err) {
        console.warn('[SW] cache cleanup failed', err);
      }
      await self.clients.claim();
    })(),
  );
});

// Transparent pass-through. No caching, no offline fallback, no synthetic
// responses — the request goes to the network exactly as if no worker existed.
// Kept non-empty on purpose: Chrome ignores an empty fetch handler when
// deciding whether the site is installable.
self.addEventListener('fetch', (event) => {
  event.respondWith(fetch(event.request));
});
