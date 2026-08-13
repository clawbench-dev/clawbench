// ClawBench Service Worker — generated at build time.
// VERSION and PRECACHE are injected by scripts/sw-plugin.ts.
const VERSION = '__VERSION__'
const PRECACHE = __PRECACHE__

const CACHE_NAME = 'clawbench-' + VERSION
const RUNTIME_CACHE = 'clawbench-runtime-' + VERSION

self.addEventListener('install', (event) => {
  event.waitUntil(
    caches
      .open(CACHE_NAME)
      .then((cache) => cache.addAll(PRECACHE))
      .then(() => self.skipWaiting())
  )
})

self.addEventListener('activate', (event) => {
  event.waitUntil(
    caches
      .keys()
      .then((keys) =>
        Promise.all(
          keys
            .filter(
              (k) => /^clawbench-/.test(k) && k !== CACHE_NAME && k !== RUNTIME_CACHE
            )
            .map((k) => caches.delete(k))
        )
      )
      .then(() => self.clients.claim())
  )
})

self.addEventListener('fetch', (event) => {
  const { request } = event
  if (request.method !== 'GET') return
  const url = new URL(request.url)
  if (url.origin !== self.location.origin) return

  // Navigation (index.html): network-first so redeploys are picked up
  // immediately; fall back to cache only when offline.
  if (request.mode === 'navigate') {
    event.respondWith(
      fetch(request)
        .then((res) => {
          const copy = res.clone()
          caches.open(RUNTIME_CACHE).then((c) => c.put(request, copy))
          return res
        })
        .catch(() => caches.match(request).then((hit) => hit || caches.match('/index.html')))
    )
    return
  }

  // Hashed static assets (immutable): cache-first, fetch-and-store on miss.
  event.respondWith(
    caches.match(request).then((cached) => {
      if (cached) return cached
      return fetch(request).then((res) => {
        if (res && res.ok) {
          const copy = res.clone()
          caches.open(RUNTIME_CACHE).then((c) => c.put(request, copy))
        }
        return res
      })
    })
  )
})
