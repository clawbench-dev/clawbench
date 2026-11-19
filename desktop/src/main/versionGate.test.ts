import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'

// versionGate imports electron + install at module scope (they do Electron/fs
// work), so they are mocked here to load the pure `evaluateGate` decision.
vi.mock('electron', () => ({
  app: { getVersion: () => '1.0.0', isPackaged: true, getLocale: () => 'en' },
  dialog: { showMessageBox: vi.fn() },
}))
vi.mock('./install', () => ({
  httpGetBuffer: vi.fn(),
  downloadAndInstall: vi.fn(),
  restartInto: vi.fn(),
}))
vi.mock('./clientLog', () => ({ recordError: vi.fn() }))
// `labels.ts` reads the persisted language through the store; the gate tests
// exercise the English wording, so pin it to 'en'. The same store holds the
// accepted certificate fingerprints, which the gate hands to the release-info
// request.
const trust = vi.hoisted(() => ({ fingerprints: {} as Record<string, string> }))
vi.mock('./store', () => ({
  getStore: () => ({
    get: (key: string) => (key === 'trustedCertificates' ? trust.fingerprints : 'en'),
  }),
}))
vi.mock('./updater', () => ({
  fetchDesktopLatest: vi.fn(async () => null),
}))

import { evaluateGate, beginNavigation, checkVersionGate, installServerVersion } from './versionGate'
import type { VersionGateInfo } from './versionGate'
import type { DesktopLatest } from './updater'
import { fetchDesktopLatest } from './updater'
import { dialog } from 'electron'
import { downloadAndInstall } from './install'

const latest = (over: Partial<DesktopLatest> = {}): DesktopLatest => ({
  version: 'v1.1.0',
  // Ordinary release: the package really is this tag's build, and the server
  // predates the protocol field (0) so these cases exercise the version gate.
  clientVersion: 'v1.1.0',
  protocol: 0,
  tag: 'v1.1.0',
  urls: ['https://example/full.zip'],
  checksums: {},
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

  it('carries the download URLs and checksums through for the install action', () => {
    const info = evaluateGate('1.0.0', latest({ checksums: { 'full.zip': 'a'.repeat(64) } }), true)
    expect(info!.urls).toEqual(['https://example/full.zip'])
    expect(info!.checksums).toEqual({ 'full.zip': 'a'.repeat(64) })
  })

  it('does not gate when the protocols agree, even though the versions differ', () => {
    // A release that reused the previous desktop build: the version strings
    // differ (the server moved on) but the contract did not, so forcing a
    // ~150MB download of an identical app would be pointless.
    const info = evaluateGate('0.129.0', latest({ version: 'v0.130.0', protocol: 3 }), true, 3)
    expect(info).toBeNull()
  })

  it('gates when the protocols differ, even with equal versions', () => {
    // A breaking contract change must be caught regardless of the version
    // string — including two builds of the same tag.
    const info = evaluateGate('1.1.0', latest({ protocol: 4 }), true, 3)
    expect(info).not.toBeNull()
    expect(info!.direction).toBe('older')
  })

  it('falls back to the version gate when the server reports no protocol', () => {
    // Rolling upgrade: an older server has no protocol field.
    expect(evaluateGate('1.0.0', latest({ protocol: 0 }), true, 3)).not.toBeNull()
    expect(evaluateGate('1.1.0', latest({ protocol: 0 }), true, 3)).toBeNull()
  })

  it('never gates on protocol for an unpackaged dev build', () => {
    expect(evaluateGate('0.1.0', latest({ protocol: 4 }), false, 3)).toBeNull()
  })

  it('installs the version the package actually carries, not the server version', () => {
    // The reused build reports its own (older) version; installing it under the
    // server's version makes app.getVersion() disagree with the pointer, and
    // the startup handoff then deletes the pointer and reverts the upgrade.
    const info = evaluateGate(
      '0.128.0',
      latest({ version: 'v0.130.0', clientVersion: 'v0.129.0', protocol: 4 }),
      true,
      3,
    )
    expect(info!.serverVersion).toBe('v0.130.0')
    expect(info!.installVersion).toBe('v0.129.0')
  })
})

describe('beginNavigation', () => {
  it('advances monotonically so an in-flight check can be recognised as stale', () => {
    const a = beginNavigation()
    const b = beginNavigation()
    expect(b).toBeGreaterThan(a)
  })
})

describe('checkVersionGate', () => {
  afterEach(() => {
    trust.fingerprints = {}
    vi.mocked(fetchDesktopLatest).mockReset()
  })

  it('asks the server with the certificate fingerprint the user accepted', async () => {
    // The release-info answer decides which binary is downloaded, so the main
    // process has to know the certificate the window was told to trust.
    trust.fingerprints['https://192.168.1.50:20000'] = 'sha256/AAAA'
    vi.mocked(fetchDesktopLatest).mockResolvedValue(latest())

    const gen = beginNavigation()
    const info = await checkVersionGate('https://192.168.1.50:20000/app', gen)

    expect(fetchDesktopLatest).toHaveBeenCalledWith('https://192.168.1.50:20000/app', 'sha256/AAAA')
    expect(info).not.toBeNull()
  })

  it('passes no fingerprint for a server the user never accepted a certificate from', async () => {
    vi.mocked(fetchDesktopLatest).mockResolvedValue(latest())

    await checkVersionGate('https://example.com/app', beginNavigation())

    // Empty means "verify normally", never "accept anything".
    expect(fetchDesktopLatest).toHaveBeenCalledWith('https://example.com/app', '')
  })
})

const gateInfo = (over: Partial<VersionGateInfo> = {}): VersionGateInfo => ({
  clientVersion: '1.0.0',
  serverVersion: '1.1.0',
  installVersion: '1.1.0',
  direction: 'older',
  urls: ['https://example/full.zip'],
  checksums: {},
  ...over,
})

describe('installServerVersion', () => {
  const dialogDetail = (): string => {
    const args = vi.mocked(dialog.showMessageBox).mock.calls[0] as unknown as unknown[]
    const opts = args[args.length - 1] as { detail?: string }
    return opts?.detail ?? ''
  }

  beforeEach(() => {
    vi.mocked(dialog.showMessageBox).mockReset()
    vi.mocked(downloadAndInstall).mockReset()
  })
  afterEach(() => vi.restoreAllMocks())

  it('passes the checksum map to the full-package installer', async () => {
    vi.mocked(downloadAndInstall).mockResolvedValue('/tmp/app-1.1.0')
    // "Later" on the restart prompt, so nothing tries to launch a process.
    vi.mocked(dialog.showMessageBox).mockResolvedValue({ response: 1, checkboxChecked: false })

    const checksums = { 'full.zip': 'a'.repeat(64) }
    const ok = await installServerVersion(gateInfo({ checksums }), null)

    expect(ok).toBe(true)
    expect(downloadAndInstall).toHaveBeenCalledWith(
      ['https://example/full.zip'],
      '1.1.0',
      checksums,
    )
  })

  it('reports the download failure and keeps the gate up', async () => {
    vi.mocked(downloadAndInstall).mockRejectedValue(new Error('all download sources failed'))

    const ok = await installServerVersion(gateInfo(), null)
    expect(ok).toBe(false)
    expect(dialogDetail()).toContain('all download sources failed')
  })
})
