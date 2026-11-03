import { getStore } from './store'
import { originOf } from './certificatePolicy'

/**
 * Read side of the remembered-certificate store.
 *
 * Split from `certificate.ts` because that module imports `electron` at module
 * scope (so nothing that touches it can be unit-tested), while the upgrade
 * check needs the same trust decision in a plain Node context: the main
 * process's own TLS stack knows nothing about the certificate the user accepted
 * in the window.
 */

/** Certificates the user has accepted, `origin -> fingerprint`. */
export function rememberedCertificates(): Record<string, string> {
  return getStore().get('trustedCertificates') ?? {}
}

/**
 * The fingerprint accepted for `url`'s origin, or '' when none is remembered.
 *
 * '' means "verify normally" — never "trust anything". A server whose
 * certificate the user has not confirmed therefore keeps behaving exactly as it
 * did before certificate pinning existed.
 */
export function trustedFingerprintFor(url: string): string {
  const origin = originOf(url)
  if (!origin) return ''
  return rememberedCertificates()[origin] ?? ''
}
