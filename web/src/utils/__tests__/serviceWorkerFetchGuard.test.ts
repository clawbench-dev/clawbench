import { afterEach, describe, expect, it, vi } from 'vitest'
import { readWebFile } from '@/testUtils/readWebFile'

// ────────────────────────────────────────────────────────────
// sw.js fetch-handler behaviour, executed for real.
//
// The sibling serviceWorkerContract.test.ts is source-sniffing: it asserts on
// the *text* of sw.js, which is the right tool for "no caches.open anywhere".
// It is the wrong tool for this bug, because the regression it guards is a
// control-flow one — a guard clause that must run *before* respondWith. A
// text assertion would have been satisfied by the broken version too
// (`respondWith(fetch(event.request))` was present the whole time).
//
// So this file evaluates the worker with a fake `self` and drives the real
// fetch handler.
//
// WHY THIS MATTERS (the incident):
// A fetch handler that unconditionally calls respondWith(fetch(event.request))
// re-issues every request through fetch(). For a multipart/form-data POST,
// WebKit drops the body on that path (WebKit bug 319396; earlier variants
// 187461) and sends Content-Length: 0. The server's ParseMultipartForm then
// fails immediately and answers 400 "FileTooLargeOrInvalid" — sub-millisecond,
// which is the signature. Every iOS Safari upload broke this way.
//
// The fix is that non-GET requests must NOT be passed to respondWith at all:
// returning without calling it lets the browser take its own network path,
// body intact.
//
// jsdom has no Service Worker runtime, so `self` is stubbed. That is
// sufficient here because the worker is a plain script: it only registers
// listeners at load time, and the handler under test touches nothing else.
// ────────────────────────────────────────────────────────────

const swSource = readWebFile('sw.js')

type FetchHandler = (event: { request: { method: string }; respondWith: (r: unknown) => void }) => void

/**
 * Evaluate sw.js against a fake `self` and return its registered handlers.
 *
 * The install/activate bodies are never invoked, so the stubs they would need
 * (`event.registerRouter`, `caches.*`, `self.clients`) only have to exist for
 * the registration calls to succeed.
 */
function loadServiceWorker(): Record<string, FetchHandler> {
  const handlers: Record<string, FetchHandler> = {}
  const fakeSelf = {
    addEventListener: (type: string, fn: FetchHandler) => {
      handlers[type] = fn
    },
    skipWaiting: () => {},
    clients: { claim: () => Promise.resolve() },
  }
  const fakeCaches = {
    keys: () => Promise.resolve<string[]>([]),
    delete: () => Promise.resolve(true),
  }
  // The worker is a plain script, not a module, so it has to be evaluated.
  new Function('self', 'caches', 'console', swSource)(fakeSelf, fakeCaches, console)
  return handlers
}

/** Build an event object and record whether respondWith was reached. */
function dispatch(method: string): { respondWith: ReturnType<typeof vi.fn>; result: unknown } {
  const handler = loadServiceWorker().fetch
  expect(handler, 'sw.js must register a fetch handler').toBeTypeOf('function')
  const respondWith = vi.fn()
  const result = handler({ request: { method }, respondWith })
  return { respondWith, result }
}

afterEach(() => {
  vi.unstubAllGlobals()
})

describe('sw.js fetch handler — non-GET requests bypass the worker', () => {
  // The regression: any of these routed through respondWith(fetch(...)) loses
  // its body on WebKit, which is how iOS Safari uploads became 400s.
  for (const method of ['POST', 'PUT', 'PATCH', 'DELETE']) {
    it(`does not call respondWith for ${method}`, () => {
      vi.stubGlobal('fetch', vi.fn())
      const { respondWith } = dispatch(method)
      expect(
        respondWith,
        `${method} must fall through to the browser's own network path, ` +
          'or WebKit drops the request body and uploads arrive empty',
      ).not.toHaveBeenCalled()
    })
  }

  it('does not touch fetch() for a non-GET request', () => {
    // Returning before respondWith is the whole fix. If fetch() were still
    // called (e.g. as an argument evaluated before the guard), the body would
    // already have been re-issued by the time we returned.
    const fetchStub = vi.fn()
    vi.stubGlobal('fetch', fetchStub)
    dispatch('POST')
    expect(fetchStub).not.toHaveBeenCalled()
  })

  it('returns without a response so the browser handles it', () => {
    vi.stubGlobal('fetch', vi.fn())
    const { result } = dispatch('POST')
    // Returning undefined is what makes the browser fall back to its default
    // network behaviour. Returning a value here would require respondWith.
    expect(result).toBeUndefined()
  })
})

describe('sw.js fetch handler — GET still passes through', () => {
  it('calls respondWith with the network fetch for GET', () => {
    const response = { ok: true }
    const fetchStub = vi.fn().mockResolvedValue(response)
    vi.stubGlobal('fetch', fetchStub)
    const { respondWith } = dispatch('GET')
    expect(respondWith).toHaveBeenCalledTimes(1)
    expect(fetchStub).toHaveBeenCalledTimes(1)
    // The handler must stay a pure pass-through: the promise handed to
    // respondWith is exactly the fetch result, not a synthetic response.
    expect(respondWith).toHaveBeenCalledWith(expect.anything())
  })

  it('forwards the request object unchanged', () => {
    const fetchStub = vi.fn().mockResolvedValue({})
    vi.stubGlobal('fetch', fetchStub)
    const handler = loadServiceWorker().fetch
    const request = { method: 'GET' }
    handler({ request, respondWith: vi.fn() })
    expect(fetchStub).toHaveBeenCalledWith(request)
  })

  it('keeps the handler non-empty so the app stays installable', () => {
    // Chrome ignores an EMPTY fetch handler when deciding installability, so
    // the GET branch must still call respondWith — dropping the whole handler
    // to fix the POST bug would silently remove the install prompt.
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({}))
    expect(dispatch('GET').respondWith).toHaveBeenCalled()
  })
})
