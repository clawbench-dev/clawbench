import { describe, it, expect, beforeEach, vi } from 'vitest'

/**
 * Certificate pinning for the upgrade path.
 *
 * The upgrade check asks the user's own server which version to run and where
 * to download it, so accepting ANY certificate there turns a self-signed
 * deployment into a remote-code-execution channel: whoever answers the request
 * chooses the version and the URL. The previous code did exactly that
 * (`rejectUnauthorized: false`).
 *
 * These tests pin the three properties that make the replacement safe:
 *   1. without a stored fingerprint the request is verified normally;
 *   2. with one, a different certificate is refused BEFORE the body is read;
 *   3. the fingerprint compares equal across the two spellings (Chromium's
 *      `sha256/<base64>` and Node's `AA:BB:…` hex), which is what makes the
 *      stored value usable at all.
 */

const state = vi.hoisted(() => ({
  /** Fingerprint the fake server presents, in Node's colon-hex form. */
  peer: '',
  requests: [] as Array<{ url: string; opts: unknown }>,
  body: Buffer.from('{}'),
  redirectTo: '',
  /** Set by destroy(): a real socket stops delivering a body too. */
  destroyed: false,
}))

/** 32 bytes (a SHA-256), spelled both ways so both formats are exercised. */
const BYTES = Buffer.from('0123456789abcdef'.repeat(4), 'hex')
const HEX = BYTES.toString('hex').match(/../g)!.join(':').toUpperCase()
const BASE64 = BYTES.toString('base64')
const OTHER_HEX = 'ff'.repeat(32).match(/../g)!.join(':').toUpperCase()

function makeGet() {
  const get = (url: string, optsOrCb: unknown, maybeCb?: unknown) => {
    const opts = typeof optsOrCb === 'function' ? undefined : optsOrCb
    const cb = (typeof optsOrCb === 'function' ? optsOrCb : maybeCb) as (res: unknown) => void
    state.requests.push({ url, opts })
    state.destroyed = false
    const listeners: Record<string, Array<(arg?: unknown) => void>> = {}
    const emit = (ev: string, arg?: unknown) => {
      for (const fn of listeners[ev] ?? []) fn(arg)
    }
    const req = {
      on(ev: string, fn: (arg?: unknown) => void) {
        ;(listeners[ev] ||= []).push(fn)
        if (ev === 'socket') {
          // The handshake completes asynchronously, after the caller has had a
          // chance to subscribe.
          queueMicrotask(() =>
            fn({
              encrypted: true,
              connecting: false,
              getPeerCertificate: () => ({ fingerprint256: state.peer }),
              once: (e: string, cb2: () => void) => { if (e === 'secureConnect') cb2() },
            }),
          )
        }
        return req
      },
      destroy(err: Error) {
        state.destroyed = true
        queueMicrotask(() => emit('error', err))
      },
    }

    const location = state.redirectTo
    state.redirectTo = ''
    const res = {
      statusCode: location ? 302 : 200,
      headers: location ? { location } : {},
      resume: () => {},
      on(ev: string, fn: (arg?: unknown) => void) {
        ;(listeners[ev] ||= []).push(fn)
        return res
      },
    }
    queueMicrotask(() => {
      cb(res)
      queueMicrotask(() => {
        if (state.destroyed) return
        emit('data', state.body)
        emit('end')
      })
    })
    return req
  }
  return { get, default: { get } }
}

vi.mock('node:https', makeGet)
vi.mock('node:http', makeGet)

import { httpGetBuffer, normalizeFingerprint, fingerprintMatches, CertificatePinMismatchError } from './httpGet'

beforeEach(() => {
  state.peer = HEX
  state.requests = []
  state.body = Buffer.from('{"version":"1.2.3"}')
  state.redirectTo = ''
  state.destroyed = false
})

describe('normalizeFingerprint', () => {
  it('reduces both spellings of the same certificate to one value', () => {
    // Electron stores `sha256/<base64>`; Node reports colon-separated hex.
    // Comparing them raw would never match, and the visible symptom is simply
    // that the server stops offering upgrades.
    expect(normalizeFingerprint(`sha256/${BASE64}`)).toBe(BYTES.toString('hex'))
    expect(normalizeFingerprint(HEX)).toBe(BYTES.toString('hex'))
    expect(normalizeFingerprint(HEX.toLowerCase().replace(/:/g, ''))).toBe(BYTES.toString('hex'))
  })

  it('returns empty for values it cannot interpret', () => {
    expect(normalizeFingerprint('')).toBe('')
    expect(normalizeFingerprint('sha256/')).toBe('')
    expect(normalizeFingerprint('not-a-fingerprint')).toBe('')
  })
})

describe('fingerprintMatches', () => {
  it('matches the same certificate across formats and rejects any other', () => {
    expect(fingerprintMatches(`sha256/${BASE64}`, HEX)).toBe(true)
    expect(fingerprintMatches(HEX, HEX)).toBe(true)
    expect(fingerprintMatches(HEX, OTHER_HEX)).toBe(false)
    // An empty pin must never authorise anything: "no fingerprint remembered"
    // means "verify normally", not "trust everything".
    expect(fingerprintMatches('', HEX)).toBe(false)
    expect(fingerprintMatches(HEX, '')).toBe(false)
  })
})

describe('httpGetBuffer', () => {
  it('verifies the certificate normally when nothing is pinned', async () => {
    const body = await httpGetBuffer('https://example.com/api/desktop/latest')
    expect(body.toString()).toBe('{"version":"1.2.3"}')
    // No `rejectUnauthorized: false`: a public host keeps full chain
    // verification, exactly as before the pinning existed.
    expect(state.requests[0].opts).toEqual({})
  })

  it('accepts the pinned certificate', async () => {
    const body = await httpGetBuffer('https://192.168.1.50:20000/api/desktop/latest', {
      pinnedFingerprint: `sha256/${BASE64}`,
    })
    expect(body.toString()).toBe('{"version":"1.2.3"}')
    expect(state.requests[0].opts).toEqual({ rejectUnauthorized: false })
  })

  it('refuses a certificate that is not the pinned one, before reading the body', async () => {
    state.peer = OTHER_HEX
    const err = await httpGetBuffer('https://192.168.1.50:20000/api/desktop/latest', {
      pinnedFingerprint: `sha256/${BASE64}`,
    }).then(
      () => null,
      (e: unknown) => e,
    )
    // The distinction that matters: a request carrying a pin must NOT succeed
    // against an arbitrary certificate. Accepting any certificate is precisely
    // the bug this replaced.
    expect(err).toBeInstanceOf(CertificatePinMismatchError)
  })

  it('keeps the pin across a same-origin redirect and drops it on a cross-origin one', async () => {
    state.redirectTo = 'https://192.168.1.50:20000/login'
    await httpGetBuffer('https://192.168.1.50:20000/api/desktop/latest', { pinnedFingerprint: HEX })
    expect(state.requests.at(-1)!.url).toBe('https://192.168.1.50:20000/login')
    expect(state.requests.at(-1)!.opts).toEqual({ rejectUnauthorized: false })

    state.requests = []
    state.redirectTo = 'https://cdn.example.com/x.zip'
    await httpGetBuffer('https://192.168.1.50:20000/x.zip', { pinnedFingerprint: HEX })
    // A different origin presents a different certificate; keeping the pin
    // there would fail every redirect, and trusting it would defeat the point.
    expect(state.requests.at(-1)!.opts).toEqual({})
  })

  it('cannot pin a plaintext request, so it is left unverified-by-pin', async () => {
    // There is no certificate on http, so a pin cannot apply; the caller that
    // needs transport security must use https (see downloadFirstAvailable).
    await httpGetBuffer('http://127.0.0.1:9/x.zip', { pinnedFingerprint: HEX })
    expect(state.requests[0].opts).toBeUndefined()
  })
})
