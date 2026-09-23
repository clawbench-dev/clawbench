import { describe, it, expect } from 'vitest'
import { shouldFallBackToLogin, buildConnectErrorScript } from './loadFailure'

/**
 * Guards the fallback decision on the main window's `did-fail-load`.
 *
 * The regression: the login-page fallback was silent, so an unreachable server
 * looked like the app randomly dropping the user back to the login screen.
 * Equally important is what must NOT fall back — a subframe failure or a
 * cancelled navigation is not an unreachable server, and treating either as one
 * would hijack a working window to the login page.
 */

const LOGIN = 'file:///app/resources/login.html'
const SERVER = 'https://192.168.1.100:20000'

function failure(over: Partial<Parameters<typeof shouldFallBackToLogin>[0]> = {}) {
  return { errorCode: -102, failedUrl: SERVER, loginUrl: LOGIN, isMainFrame: true, ...over }
}

describe('shouldFallBackToLogin', () => {
  it('falls back when the main frame fails to load the server', () => {
    expect(shouldFallBackToLogin(failure())).toBe(true)
  })

  it('ignores subframe failures', () => {
    // An iframe that fails does not mean the app page is unreachable.
    expect(shouldFallBackToLogin(failure({ isMainFrame: false }))).toBe(false)
  })

  it('ignores ERR_ABORTED (-3)', () => {
    // A cancelled navigation (e.g. a redirect we prevented) says nothing about
    // reachability and would otherwise hijack the window.
    expect(shouldFallBackToLogin(failure({ errorCode: -3 }))).toBe(false)
  })

  it('ignores a failure of the login page itself', () => {
    // Nothing further to fall back to; reloading it would loop.
    expect(shouldFallBackToLogin(failure({ failedUrl: LOGIN }))).toBe(false)
  })

  it('ignores a failure with no URL', () => {
    expect(shouldFallBackToLogin(failure({ failedUrl: '' }))).toBe(false)
  })
})

describe('buildConnectErrorScript', () => {
  it('calls onConnectError with the description', () => {
    expect(buildConnectErrorScript('ERR_CONNECTION_REFUSED'))
      .toBe("if (typeof onConnectError === 'function') { onConnectError(\"ERR_CONNECTION_REFUSED\") }")
  })

  it('guards against the login page not having defined onConnectError', () => {
    // The page may not have finished its script when this runs.
    expect(buildConnectErrorScript('x')).toContain("typeof onConnectError === 'function'")
  })

  it('cannot be broken out of by a description containing quotes', () => {
    // Evaluate the generated script against a spy. A substring check would be
    // wrong here: the injected text still APPEARS inside the string literal —
    // what matters is that it is not executed.
    const script = buildConnectErrorScript('bad"); globalThis.__pwned = true; ("')
    const received: string[] = []
    // eslint-disable-next-line no-new-func
    new Function('onConnectError', script)((m: string) => received.push(m))

    expect(received).toEqual(['bad"); globalThis.__pwned = true; ("'])
    expect((globalThis as Record<string, unknown>).__pwned).toBeUndefined()
  })

  it('handles a backslash without producing an invalid literal', () => {
    const script = buildConnectErrorScript('a\\b')
    expect(script).toBe("if (typeof onConnectError === 'function') { onConnectError(\"a\\\\b\") }")
  })

  it('is a no-op when onConnectError is absent', () => {
    // The page may not be the login page (or its script not yet run).
    expect(() => {
      // eslint-disable-next-line no-new-func
      new Function(buildConnectErrorScript('x'))()
    }).not.toThrow()
  })

  it('passes an empty string when the description is missing', () => {
    expect(buildConnectErrorScript('')).toBe("if (typeof onConnectError === 'function') { onConnectError(\"\") }")
  })
})
