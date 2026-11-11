import http from 'node:http'
import https from 'node:https'
import type { TLSSocket } from 'node:tls'
import { originOf } from './certificatePolicy'

/**
 * The HTTP GET used by the installer and the upgrade check.
 *
 * It lives in its own module, apart from `install.ts`, for one reason: the
 * certificate pinning below is the security boundary of the self-upgrade path,
 * and it must be testable without pulling in `electron` (which `install.ts`
 * imports at module scope). Splitting it out is what lets the pinning be
 * exercised against a real TLS handshake instead of a hand-written mock.
 */

/** Raised when the peer's certificate is not the one the user accepted. */
export class CertificatePinMismatchError extends Error {
  constructor(message: string) {
    super(message)
    this.name = 'CertificatePinMismatchError'
  }
}

export interface HttpGetOptions {
  /**
   * SHA-256 fingerprint of the certificate the user already accepted for this
   * origin (`trustedCertificates[origin]` in the store).
   *
   * Self-hosted servers are commonly self-signed, so the chain cannot be
   * verified at all; without this the upgrade check throws and reads as
   * "nothing to offer". Turning verification off entirely — the previous
   * approach — also accepted ANY certificate, which let anyone on the same
   * network answer `/api/desktop/latest` with a version and download URLs of
   * their choosing. Pinning keeps the self-signed case working while still
   * refusing every other certificate.
   *
   * An empty value means "verify normally" — the same behaviour a request to a
   * public host has always had.
   */
  pinnedFingerprint?: string
  /** Redirects left to follow before giving up. */
  redirectsLeft?: number
  /**
   * Abandon a transfer that is not making progress, so the caller can try the
   * next candidate URL.
   *
   * Without this a mirror that accepts the connection and then trickles never
   * fails — and the mirror walk only advances on FAILURE. Measured example
   * (2026-11-11): a proxy answered HTTP 206 at 38 KB/s, i.e. 55 minutes for one
   * 125MB desktop package, while the caller sat on it instead of trying the
   * next source.
   *
   * Two checks run every `idleMs`:
   *   - no bytes at all in that window → give up (dead connection);
   *   - after the first window, fewer than `minBytesPerSecond` → give up.
   * The first window is exempt from the rate check so a mirror that needs a
   * while to start streaming (it fetches from upstream first) is not mistaken
   * for a dead one.
   *
   * Omitted by default: the small JSON requests must keep their old behaviour.
   */
  progressGuard?: { idleMs: number; minBytesPerSecond: number }
}

/**
 * Normalize a certificate fingerprint to lowercase hex, or '' when unparseable.
 *
 * The two sides spell the same 32 bytes differently: Chromium (and therefore
 * Electron's `certificate.fingerprint`, which is what gets stored) writes
 * `sha256/<base64>`, while Node's `getPeerCertificate().fingerprint256` writes
 * `AA:BB:CC:…` hex. Comparing the raw strings would never match, so a pin would
 * silently stop working — the failure mode being "upgrades stop being offered".
 */
export function normalizeFingerprint(value: string): string {
  const raw = value.trim()
  if (!raw) return ''
  const slash = raw.indexOf('/')
  const body = slash >= 0 ? raw.slice(slash + 1) : raw
  if (/^[0-9a-f]{2}(:[0-9a-f]{2}){31}$/i.test(body)) return body.replace(/:/g, '').toLowerCase()
  if (/^[0-9a-f]{64}$/i.test(body)) return body.toLowerCase()
  if (/^[A-Za-z0-9+/]{43}=?$/.test(body)) {
    const decoded = Buffer.from(body, 'base64')
    if (decoded.length === 32) return decoded.toString('hex')
  }
  return ''
}

/** Whether `peer` is the certificate `pinned` names. An empty pin never matches. */
export function fingerprintMatches(pinned: string, peer: string): boolean {
  const a = normalizeFingerprint(pinned)
  return a.length === 64 && a === normalizeFingerprint(peer)
}

/**
 * GET `url` into a buffer.
 *
 * `pinnedFingerprint` switches the request to fingerprint pinning (see
 * `HttpGetOptions`). The handshake is accepted so the peer's certificate can be
 * read, and the request is destroyed the moment the fingerprint does not match
 * — before the response body is consumed.
 */
export function httpGetBuffer(url: string, opts: HttpGetOptions = {}): Promise<Buffer> {
  const { pinnedFingerprint = '', redirectsLeft = 5, progressGuard } = opts
  const isHttps = url.startsWith('https:')
  // A pin is a statement about a TLS peer; on a plaintext URL there is no
  // certificate to compare, so the pin cannot apply.
  const pin = isHttps ? pinnedFingerprint : ''

  return new Promise((resolve, reject) => {
    const startedAt = Date.now()
    let received = 0
    let windowBytes = 0
    let guardTimer: ReturnType<typeof setTimeout> | null = null

    const stopGuard = () => {
      if (guardTimer) {
        clearTimeout(guardTimer)
        guardTimer = null
      }
    }

    // Re-armed every window rather than on each chunk: a mirror sending one
    // byte every few seconds keeps an idle timer alive forever, which is
    // exactly the case this has to catch.
    const armGuard = () => {
      if (!progressGuard) return
      const { idleMs, minBytesPerSecond } = progressGuard
      guardTimer = setTimeout(() => {
        guardTimer = null
        const rate = (windowBytes * 1000) / idleMs
        if (received === 0) {
          req.destroy(new Error(`download abandoned: no data for ${idleMs}ms`))
          return
        }
        // The first window is exempt from the rate check: a proxy has to fetch
        // the object from upstream before it can stream, and calling that
        // "too slow" would abandon a mirror that is about to be fine.
        if (Date.now() - startedAt >= 2 * idleMs && rate < minBytesPerSecond) {
          req.destroy(new Error(`download abandoned: ${Math.round(rate / 1024)} KB/s is below the ${Math.round(minBytesPerSecond / 1024)} KB/s floor`))
          return
        }
        windowBytes = 0
        armGuard()
      }, idleMs)
    }

    const onResponse = (res: http.IncomingMessage) => {
      // Registries and CDNs redirect tarball URLs; without following them the
      // download would silently produce a body of zero bytes.
      const { statusCode, headers } = res
      if (statusCode && statusCode >= 300 && statusCode < 400 && headers.location) {
        stopGuard()
        if (redirectsLeft <= 0) { reject(new Error('too many redirects')); return }
        res.resume()
        const next = new URL(headers.location, url).toString()
        // The pin belongs to one origin. A redirect that leaves it is a
        // different server with a different certificate, so the pin is dropped
        // and that hop is verified normally.
        const nextPin = pin && originOf(next) === originOf(url) ? pin : ''
        resolve(httpGetBuffer(next, { pinnedFingerprint: nextPin, redirectsLeft: redirectsLeft - 1, progressGuard }))
        return
      }
      if (statusCode !== 200) {
        stopGuard()
        reject(new Error(`download failed: HTTP ${statusCode}`))
        return
      }
      const chunks: Buffer[] = []
      res.on('data', (c: Buffer) => {
        received += c.length
        windowBytes += c.length
        chunks.push(c)
      })
      res.on('end', () => {
        stopGuard()
        resolve(Buffer.concat(chunks))
      })
      armGuard()
    }

    // A pinned certificate is usually self-signed, so the chain cannot be
    // verified; accept the handshake and check the fingerprint instead.
    const req = isHttps
      ? https.get(url, pin ? { rejectUnauthorized: false } : {}, onResponse)
      : http.get(url, onResponse)

    if (pin) {
      req.on('socket', (socket) => {
        const tls = socket as TLSSocket
        const verify = () => {
          const peer = tls.getPeerCertificate?.() ?? ({} as ReturnType<TLSSocket['getPeerCertificate']>)
          if (!fingerprintMatches(pin, peer.fingerprint256 ?? '')) {
            req.destroy(new CertificatePinMismatchError(`certificate does not match the pinned one for ${originOf(url)}`))
          }
        }
        // A reused keep-alive socket has already completed its handshake, so
        // `secureConnect` will not fire again — check it right away. Node's
        // default agent keeps connections alive, so this is the common case for
        // a second request to the same origin.
        if (tls.encrypted && !tls.connecting) verify()
        else tls.once('secureConnect', verify)
      })
    }

    req.on('error', (err) => {
      stopGuard()
      reject(err)
    })
  })
}
