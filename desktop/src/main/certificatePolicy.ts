/**
 * Pure decision logic for the desktop shell's TLS certificate handling.
 *
 * The Electron glue (`certificate.ts`) imports `electron` at module scope, so a
 * unit test cannot load it. Everything that is a *decision* rather than an
 * Electron call lives here instead, mirroring how `loadFailure.ts` and
 * `splashPolicy.ts` were split out for the same reason.
 *
 * Android already prompts on a failed certificate check and lets the user
 * "trust and continue" (see `MainActivity.showSslConfirmationDialog`); the
 * desktop shell had no such path and simply bounced back to the login page.
 * These helpers decide, per failed load, whether to prompt, trust silently, or
 * leave the default rejection in place.
 */

/** What the `certificate-error` handler should do for a failed load. */
export type CertificateAction =
  /** Not our concern (a subframe, or an unparseable URL): let Chromium reject. */
  | 'ignore'
  /** Trust without asking — localhost tunnels, whose cert never matches. */
  | 'auto-trust'
  /** Ask the user; trust only if they confirm. */
  | 'prompt'

/**
 * The origin used to key certificate trust for `url`, or '' when it cannot be
 * parsed.
 *
 * WebSocket URLs are folded onto their HTTP equivalent: a `wss://` handshake to
 * the same host:port reaches the same server and presents the same certificate
 * as `https://`. Keying them separately left the WebSocket rejected right after
 * the page itself had been trusted — the app then looked connected-but-dead.
 */
export function originOf(url: string): string {
  try {
    const parsed = new URL(url)
    if (parsed.protocol === 'wss:') parsed.protocol = 'https:'
    else if (parsed.protocol === 'ws:') parsed.protocol = 'http:'
    return parsed.origin
  } catch {
    return ''
  }
}

/**
 * Whether `hostname` denotes the local machine.
 *
 * `URL.hostname` keeps the brackets on an IPv6 literal (`[::1]`), so both forms
 * are matched. These are the addresses an SSH / h2 port-forward reaches the
 * server on, where the certificate can never match the host the client typed.
 */
export function isLocalhostHost(hostname: string): boolean {
  return (
    hostname === 'localhost' ||
    hostname === '127.0.0.1' ||
    hostname === '::1' ||
    hostname === '[::1]'
  )
}

/**
 * Whether a certificate the user accepted earlier still authorises this one.
 *
 * Bound to the FINGERPRINT, not just the origin: a server whose certificate
 * changed (reinstalled, rotated, or swapped by a man-in-the-middle) must raise
 * the prompt again. An empty fingerprint never matches — Electron reports one
 * for every real certificate, so an empty value means something is off and
 * asking is the safe answer.
 */
export function isRememberedCertificate(
  remembered: Record<string, string>,
  url: string,
  fingerprint: string,
): boolean {
  if (!fingerprint) return false
  const origin = originOf(url)
  if (!origin) return false
  return remembered[origin] === fingerprint
}

/**
 * Decide what to do about a certificate error for `url`.
 *
 * `trusted` says whether this origin's certificate is already accepted — the
 * same certificate the user confirmed earlier, or one this connection already
 * let through. That answer has to cover EVERY request to the origin, not just
 * the navigation that raised the prompt: certificate verification runs per
 * connection, so the WebSocket and every fetch that opens a fresh socket
 * (rather than reusing the navigation's keep-alive one) raises its own
 * `certificate-error` with `isMainFrame === false`. Scoping the trust to the
 * main frame left those rejected — the app rendered, then every later request
 * failed with `ERR_CERT_AUTHORITY_INVALID`.
 *
 * A localhost request is trusted silently whatever the frame type: an SSH / h2
 * tunnel reaches the server on loopback, where the certificate can never match
 * the host the client typed — the same carve-out Android makes. Only a
 * *third-party* subresource is left to Chromium's default rejection: prompting
 * there would block the page on an asset the user never asked about.
 */
export function decideCertificateAction(
  url: string,
  isMainFrame: boolean,
  trusted = false,
): CertificateAction {
  let hostname: string
  try {
    hostname = new URL(url).hostname
  } catch {
    return 'ignore'
  }
  if (trusted) return 'auto-trust'
  if (isLocalhostHost(hostname)) return 'auto-trust'
  if (!isMainFrame) return 'ignore'
  return 'prompt'
}
