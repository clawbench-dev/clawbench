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

/** The scheme://host:port origin of `url`, or '' when it cannot be parsed. */
export function originOf(url: string): string {
  try {
    return new URL(url).origin
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
 * Decide what to do about a certificate error for `url`.
 *
 * Only a main-frame navigation is actionable: a subresource failure must not
 * raise a blocking prompt (and Chromium rejects it on its own anyway). A
 * localhost main-frame navigation is trusted silently — the same carve-out
 * Android makes for its tunneled connections. Everything else prompts.
 */
export function decideCertificateAction(url: string, isMainFrame: boolean): CertificateAction {
  if (!isMainFrame) return 'ignore'
  let hostname: string
  try {
    hostname = new URL(url).hostname
  } catch {
    return 'ignore'
  }
  if (isLocalhostHost(hostname)) return 'auto-trust'
  return 'prompt'
}
