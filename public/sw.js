// ClawBench Service Worker — minimal shell for PWA installability
// No offline caching; fetch always goes to network.
self.addEventListener('install', () => self.skipWaiting());
self.addEventListener('activate', () => self.clients.claim());
self.addEventListener('fetch', (e) => e.respondWith(fetch(e.request)));
