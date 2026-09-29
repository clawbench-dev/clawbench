import { describe, it, expect } from 'vitest'
import { isTransportError } from '../networkError'

describe('isTransportError', () => {
  it('classifies the browser TypeError for a failed fetch as a transport error', () => {
    // Chromium/Firefox: `fetch()` rejects with TypeError('Failed to fetch')
    // when the request never reached the server (offline, connection refused,
    // CORS). This is the exact error the Android resume path produces when the
    // radio has not come back yet.
    expect(isTransportError(new TypeError('Failed to fetch'))).toBe(true)
  })

  it('classifies Safari WebKit wording as a transport error', () => {
    // WebKit says "Load failed"; RN/older WebViews say "Network request failed".
    expect(isTransportError(new TypeError('Load failed'))).toBe(true)
    expect(isTransportError(new TypeError('Network request failed'))).toBe(true)
  })

  it('classifies the Android WebView NetworkError wording as a transport error', () => {
    expect(isTransportError(new Error('NetworkError when attempting to fetch resource.'))).toBe(true)
  })

  it('does NOT classify an HTTP-layer error (server returned non-2xx) as a transport error', () => {
    // apiGet/apiPost attach a status to HTTP-layer errors — the server WAS
    // reachable, so the user needs to see the failure.
    const httpErr = new Error('Internal Server Error') as Error & { status?: number }
    httpErr.status = 500
    expect(isTransportError(httpErr)).toBe(false)
  })

  it('does NOT classify an HTTP-layer error even when its message echoes transport wording', () => {
    // A reverse proxy answering 502 can put "failed to fetch upstream" in the
    // body, and apiGet copies `data.error` into `err.message`. Message sniffing
    // alone would misread that as a transport failure and swallow a real
    // server error — the `status` field is what makes the distinction reliable.
    const httpErr = new Error('failed to fetch upstream') as Error & { status?: number }
    httpErr.status = 502
    expect(isTransportError(httpErr)).toBe(false)
  })

  it('does NOT classify a plain Error with an unrelated message as a transport error', () => {
    expect(isTransportError(new Error('boom'))).toBe(false)
  })

  it('does NOT classify non-Error values as transport errors', () => {
    expect(isTransportError(undefined)).toBe(false)
    expect(isTransportError(null)).toBe(false)
    expect(isTransportError('Failed to fetch')).toBe(false)
  })

  it('does NOT classify an AbortError as a transport error', () => {
    // Aborts are deliberate (timeout / superseded request) and handled by the
    // callers' own signal.aborted checks; they must not be conflated here.
    const abortErr = new Error('The operation was aborted.')
    abortErr.name = 'AbortError'
    expect(isTransportError(abortErr)).toBe(false)
  })
})
