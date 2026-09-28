import { describe, it, expect, vi } from 'vitest'

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

import { evaluateGate, beginNavigation } from './versionGate'
import type { DesktopLatest } from './updater'

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
