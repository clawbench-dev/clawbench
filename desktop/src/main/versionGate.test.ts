import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'

// versionGate imports electron + install at module scope (they do Electron/fs
// work), so they are mocked here to load the pure `evaluateGate` decision.
vi.mock('electron', () => ({
  app: { getVersion: () => '1.0.0', isPackaged: true },
  dialog: { showMessageBox: vi.fn() },
}))
vi.mock('./install', () => ({
  httpGetBuffer: vi.fn(),
  downloadAndInstall: vi.fn(),
  installPayload: vi.fn(),
  restartInto: vi.fn(),
  appRoot: () => '/tmp/app',
}))
vi.mock('./clientLog', () => ({ recordError: vi.fn() }))

import { evaluateGate, beginNavigation, installServerVersion, formatInstallFailure } from './versionGate'
import type { VersionGateInfo } from './versionGate'
import type { DesktopLatest } from './updater'
import { dialog } from 'electron'
import { downloadAndInstall, installPayload } from './install'

const latest = (over: Partial<DesktopLatest> = {}): DesktopLatest => ({
  version: 'v1.1.0',
  tag: 'v1.1.0',
  urls: ['https://example/full.zip'],
  payloadUrls: [],
  ...over,
})

describe('evaluateGate', () => {
  it('gates when the client is older than the server, marking direction older', () => {
    const info = evaluateGate('1.0.0', latest(), true)
    expect(info).not.toBeNull()
    expect(info!.direction).toBe('older')
    expect(info!.clientVersion).toBe('1.0.0')
    expect(info!.serverVersion).toBe('v1.1.0')
  })

  it('gates when the client is NEWER than the server, marking direction newer', () => {
    // The behaviour change: a client ahead of its server is flagged too, and
    // the overlay must say "align down", not "update".
    const info = evaluateGate('1.2.0', latest(), true)
    expect(info).not.toBeNull()
    expect(info!.direction).toBe('newer')
  })

  it('does not gate when the versions agree, across spellings', () => {
    expect(evaluateGate('1.1.0', latest(), true)).toBeNull()
    // Same release, one side carrying build metadata.
    expect(evaluateGate('1.1.0', latest({ version: 'v1.1.0-07291030' }), true)).toBeNull()
  })

  it('never gates an unpackaged dev build', () => {
    expect(evaluateGate('0.1.0', latest(), false)).toBeNull()
  })

  it('fails open on unparseable versions', () => {
    expect(evaluateGate('dev', latest(), true)).toBeNull()
    expect(evaluateGate('1.0.0', latest({ version: 'dev' }), true)).toBeNull()
  })

  it('carries the download URLs through for the install action', () => {
    const info = evaluateGate('1.0.0', latest({ payloadUrls: ['https://example/payload.tgz'] }), true)
    expect(info!.urls).toEqual(['https://example/full.zip'])
    expect(info!.payloadUrls).toEqual(['https://example/payload.tgz'])
  })
})

describe('beginNavigation', () => {
  it('advances monotonically so an in-flight check can be recognised as stale', () => {
    const a = beginNavigation()
    const b = beginNavigation()
    expect(b).toBeGreaterThan(a)
  })
})

const gateInfo = (over: Partial<VersionGateInfo> = {}): VersionGateInfo => ({
  clientVersion: '1.0.0',
  serverVersion: '1.1.0',
  direction: 'older',
  urls: ['https://example/full.zip'],
  payloadUrls: [],
  ...over,
})

describe('installServerVersion failure reporting', () => {
  // showMessageBox is overloaded (with/without a parent window); the mock is
  // typed against the 1-arg overload, so read the options object out of the raw
  // call args. At runtime the parent is passed explicitly (as `undefined` when
  // there is none), so the options object is the LAST argument.
  const dialogDetail = (): string => {
    const args = vi.mocked(dialog.showMessageBox).mock.calls[0] as unknown as unknown[]
    const opts = args[args.length - 1] as { detail?: string }
    return opts?.detail ?? ''
  }

  beforeEach(() => {
    vi.mocked(dialog.showMessageBox).mockReset()
    vi.mocked(downloadAndInstall).mockReset()
    vi.mocked(installPayload).mockReset()
  })
  afterEach(() => vi.restoreAllMocks())

  it('reports the payload (npm) error too when the full download also fails', async () => {
    // The regression: the payload path failed silently (only console.error), so
    // the dialog showed only the github.com full-package error — making a
    // missing/failing npm mirror look like the client never tried npm at all.
    vi.mocked(installPayload).mockRejectedValue(new Error('payload 404: npm mirror unreachable'))
    vi.mocked(downloadAndInstall).mockRejectedValue(
      new Error('all download sources failed:\nhttps://gh-proxy.com/...: read ECONNRESET'),
    )

    const ok = await installServerVersion(
      gateInfo({ payloadUrls: ['https://example/payload.tgz'] }),
      null,
    )
    expect(ok).toBe(false)

    expect(dialogDetail()).toContain('read ECONNRESET') // the full-download error
    expect(dialogDetail()).toContain('npm mirror unreachable') // AND the payload error
  })

  it('still reports the full-package error when there is no payload path', async () => {
    vi.mocked(downloadAndInstall).mockRejectedValue(new Error('all download sources failed'))

    const ok = await installServerVersion(gateInfo({ payloadUrls: [] }), null)
    expect(ok).toBe(false)

    expect(dialogDetail()).toContain('all download sources failed')
    expect(installPayload).not.toHaveBeenCalled()
  })
})

describe('formatInstallFailure', () => {
  it('shows only the full-download error when the payload path was not attempted', () => {
    expect(formatInstallFailure(new Error('boom'), null)).toBe('boom')
  })

  it('labels both attempts so the user can tell npm from github', () => {
    const s = formatInstallFailure(new Error('github ECONNRESET'), new Error('npm 404'))
    expect(s).toContain('npm 404')
    expect(s).toContain('github ECONNRESET')
  })
})
