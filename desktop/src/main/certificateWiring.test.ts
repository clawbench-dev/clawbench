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
const PRELOAD = 'desktop/src/preload/index.ts'
const STORE = 'desktop/src/main/store.ts'
const FIELD_MAP = 'web/src/components/settings/settingsFieldMap.ts'

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

  it('answers subresource failures from the trusted-origin set', () => {
    // Trust is granted for one navigation but must cover every request to that
    // origin: each new socket (the WebSocket, most fetches) re-runs certificate
    // verification with isMainFrame=false. Forgetting to pass the trust set
    // here is invisible at build time and breaks the whole app at runtime.
    const src = readRepoFile(CERT)
    expect(src).toContain('isRememberedCertificate(rememberedCertificates()')
    expect(src).toContain('decideCertificateAction(url, isMainFrame, remembered)')
  })

  it('prompts the user and trusts only on confirmation', () => {
    const src = readRepoFile(CERT)
    expect(src).toContain('dialog.showMessageBox')
    // Esc / window close must count as "cancel", never as trust.
    expect(src).toMatch(/cancelId:\s*1/)
    expect(src).toMatch(/response === 0/)
  })
})

describe('certificate trust is remembered by fingerprint', () => {
  it('does NOT clear the remembered certificates on connect', () => {
    // Remembering is the point: reconnecting to the same server must not ask
    // again. A changed certificate still does, because the entry is keyed by
    // fingerprint (see the policy test).
    const src = readRepoFile(BRIDGE)
    const connect = src.match(/native:connect-to-server[\s\S]*?\n  \}\)/)
    expect(connect, 'connect-to-server handler not found').not.toBeNull()
    expect(connect![0], 'connect must not reset the certificate trust').not.toContain('resetCertificateTrust')
  })

  it('persists the accepted fingerprint and matches on it', () => {
    const src = readRepoFile(CERT)
    expect(src).toContain("getStore().set('trustedCertificates'")
    expect(src).toContain('certificate.fingerprint')
    expect(src).toContain('isRememberedCertificate(')
  })

  it('declares the store key so an existing install gets a default', () => {
    const src = readRepoFile(STORE)
    expect(src).toContain('trustedCertificates: Record<string, string>')
    expect(src).toContain('trustedCertificates: {},')
  })

  it('exposes an undo through the settings UI', () => {
    // Without a way to forget, a mistaken "trust and continue" is permanent.
    expect(readRepoFile(BRIDGE)).toContain("ipcMain.handle('native:forget-trusted-certs'")
    expect(readRepoFile(PRELOAD)).toContain("invoke('native:forget-trusted-certs')")
    expect(readRepoFile(FIELD_MAP)).toContain("key: 'trustedCerts'")
  })
})
