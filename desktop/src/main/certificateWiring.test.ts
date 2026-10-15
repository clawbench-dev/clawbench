import { describe, it, expect } from 'vitest'
import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'

/**
 * Wiring guards for the desktop TLS-certificate prompt.
 *
 * `certificate.ts`, `index.ts`, `bridge.ts` and `window.ts` all import
 * `electron` at module scope, so no unit test can load them. The feature's
 * failure mode is silent — Chromium simply rejects the navigation and the user
 * is bounced back to the login page with no way to proceed — so the wiring is
 * pinned at the source, following the repo's convention for electron-only
 * modules (see `splashWiring.test.ts`).
 *
 * The pure decision logic is covered separately by `certificatePolicy.test.ts`.
 */

const repoRoot = resolve(__dirname, '../../..')

function readRepoFile(rel: string): string {
  return readFileSync(resolve(repoRoot, rel), 'utf8')
}

const INDEX = 'desktop/src/main/index.ts'
const BRIDGE = 'desktop/src/main/bridge.ts'
const CERT = 'desktop/src/main/certificate.ts'

describe('certificate handler is registered', () => {
  it('registers the app-level certificate-error handler at startup', () => {
    const src = readRepoFile(INDEX)
    expect(src).toContain("import { registerCertificateHandler } from './certificate'")
    expect(src).toContain('registerCertificateHandler()')
  })

  it('listens on certificate-error and answers through the callback', () => {
    const src = readRepoFile(CERT)
    expect(src).toContain("app.on('certificate-error'")
    // The navigation stays paused until the callback fires; a handler that never
    // calls it would hang the load forever.
    expect(src).toContain('callback(')
    // Must preventDefault before answering, or Chromium's own rejection wins.
    expect(src).toContain('event.preventDefault()')
  })

  it('prompts the user and trusts only on confirmation', () => {
    const src = readRepoFile(CERT)
    expect(src).toContain('dialog.showMessageBox')
    // Esc / window close must count as "cancel", never as trust.
    expect(src).toMatch(/cancelId:\s*1/)
    expect(src).toMatch(/response === 0/)
  })
})

describe('certificate trust is per-connection', () => {
  it('resets the trust set on every connect', () => {
    // The user asked to be re-prompted each time (matching Android), so a
    // persisted or process-lifetime trust would be wrong.
    const src = readRepoFile(BRIDGE)
    const connect = src.match(/native:connect-to-server[\s\S]*?\n  \}\)/)
    expect(connect, 'connect-to-server handler not found').not.toBeNull()
    expect(connect![0], 'connect must reset the certificate trust').toContain('resetCertificateTrust()')
  })

  it('clears the set rather than persisting it to the store', () => {
    const src = readRepoFile(CERT)
    expect(src).toContain('trustedOrigins.clear()')
    // A persisted trust would survive a restart, contradicting the per-connection
    // decision. Guard against a future "remember" change sneaking in silently.
    expect(src).not.toContain("getStore().set(")
  })
})
