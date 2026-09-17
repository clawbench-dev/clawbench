import { describe, expect, it } from 'vitest'
import { readWebFile } from '@/testUtils/readWebFile'

// ────────────────────────────────────────────────────────────
// Contract for the PWA service worker and its registration path.
//
// The worker exists ONLY to keep the app installable. Everything it can do
// beyond that is a way to break the app, and it already has: the previous
// worker cached successful responses, so a rebuilt index.html kept being served
// from cache while pointing at chunk hashes that no longer existed, and it
// answered /api/* with a synthetic 503 which dropped session cookies and turned
// every request into a 403.
//
// jsdom has no Service Worker implementation and no way to run sw.js, so these
// are source-sniffing checks — the same pattern as countBadge.css.test.ts. They
// assert the *absence* of the dangerous primitives, which is exactly the kind of
// regression that would otherwise ship silently: re-adding one `caches.open`
// call is invisible in review and only shows up as a stale-asset bug weeks
// later.
// ────────────────────────────────────────────────────────────

const swSource = readWebFile('sw.js')
const indexHtml = readWebFile('index.html')
const mainTs = readWebFile('src/main.ts')

/** Strip comments so prose explaining a hazard is not mistaken for the hazard. */
function stripComments(src: string): string {
  return src.replace(/\/\*[\s\S]*?\*\//g, '').replace(/(^|[^:])\/\/.*$/gm, '$1')
}

const swCode = stripComments(swSource)

describe('sw.js never touches Cache Storage', () => {
  it('has no cache write calls', () => {
    for (const forbidden of ['caches.open', 'cache.put', 'cache.addAll', 'cache.add(']) {
      expect(swCode, `${forbidden} would reintroduce stale-asset serving`).not.toContain(forbidden)
    }
  })

  it('only reads Cache Storage in activate, to delete legacy caches', () => {
    // caches.keys/caches.delete are allowed, but only inside the activate
    // handler — a delete during fetch would mean the worker is doing cache
    // bookkeeping on the request path.
    const activateStart = swCode.indexOf("addEventListener('activate'")
    const fetchStart = swCode.indexOf("addEventListener('fetch'")
    expect(activateStart, 'activate handler must exist').toBeGreaterThan(-1)
    expect(fetchStart, 'fetch handler must exist').toBeGreaterThan(-1)

    const activateBody = swCode.slice(activateStart, fetchStart)
    expect(activateBody).toContain('caches.keys')
    expect(activateBody).toContain('caches.delete')
    expect(swCode.slice(fetchStart)).not.toContain('caches.')
  })
})

describe('sw.js routes sensitive paths away from the worker', () => {
  it('declares a network-only router table', () => {
    expect(swCode).toContain('registerRouter')
    expect(swCode).toContain("source: 'network'")
  })

  it('routes the authenticated and shell paths through the network', () => {
    for (const path of ["'/api/*'", "'/login'", "'/share/*'", "'/sw.js'", "'/manifest.json'", "'/assets/*'"]) {
      expect(swCode, `${path} must bypass the worker`).toContain(path)
    }
  })

  it('guards registerRouter for browsers without the API', () => {
    // Firefox and Safari have no registerRouter; calling it unguarded throws
    // during install and the worker never activates.
    expect(swCode).toMatch(/typeof event\.registerRouter === 'function'/)
  })
})

describe('sw.js fetch handler is a non-empty pass-through', () => {
  it('responds with the plain network request', () => {
    // Chrome ignores an EMPTY fetch handler when deciding installability, so
    // removing respondWith would silently kill the install prompt. It must also
    // stay a pure pass-through: no synthetic responses.
    expect(swCode).toContain('respondWith(fetch(event.request))')
  })

  it('never fabricates a response', () => {
    expect(swCode).not.toContain('new Response(')
  })
})

describe('nothing unregisters the worker on every page load', () => {
  // The pre-PWA cleanup path called getRegistrations() and unregistered
  // everything. Left in place, it would tear down the worker registered by
  // registerPwaServiceWorker() on the very next load.
  it('index.html has no unregister script', () => {
    expect(indexHtml).not.toContain('getRegistrations')
    expect(indexHtml).not.toContain('unregister')
    expect(indexHtml).not.toContain('serviceWorker')
  })

  it('main.ts has no unregister block', () => {
    expect(stripComments(mainTs)).not.toContain('getRegistrations')
    expect(stripComments(mainTs)).not.toContain('unregister')
  })

  it('main.ts registers the worker instead', () => {
    expect(mainTs).toContain('registerPwaServiceWorker')
  })
})

describe('manifest reaches the build root', () => {
  it('index.html links the manifest at the site root', () => {
    // Not /assets/manifest.json: the manifest URL is the installed app's
    // identity and must be stable and match the manifest's own `scope`.
    expect(indexHtml).toContain('<link rel="manifest" href="/manifest.json">')
  })

  it('manifest lives in publicDir so Vite copies it verbatim', () => {
    // Regression: while manifest.json sat in the Vite project root (web/), Vite
    // treated the <link href="/manifest.json"> as a hashable asset and rewrote
    // it to /manifest-<hash>.json in the build output. Assets under publicDir
    // are copied untouched, which is what a stable manifest URL requires.
    const manifest = JSON.parse(readWebFile('../assets/manifest.json'))
    expect(manifest.start_url).toBe('/')
    expect(manifest.scope).toBe('/')
    expect(manifest.display).toBe('standalone')
    // Chrome requires a 192px and a 512px icon.
    const sizes = manifest.icons.map((i: { sizes: string }) => i.sizes)
    expect(sizes).toContain('192x192')
    expect(sizes).toContain('512x512')
  })

  it('vite copies the service worker to the output root', () => {
    const viteConfig = readWebFile('../vite.config.ts')
    expect(viteConfig).toMatch(/PWA_ROOT_FILES\s*=\s*\[[^\]]*'sw\.js'/)
  })
})
