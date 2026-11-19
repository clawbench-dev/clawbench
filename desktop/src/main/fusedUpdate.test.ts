import { describe, it, expect, vi, beforeEach } from 'vitest'

// The module is Electron/filesystem glue around `downloadAndInstall`; the
// decisions worth pinning are which package it accepts and what it does with
// the user's restart answer.
vi.mock('electron', () => ({
  app: { getVersion: () => '1.0.0', getLocale: () => 'en' },
  dialog: { showMessageBox: vi.fn(async () => ({ response: 1 })) },
}))
vi.mock('./install', () => ({
  downloadAndInstall: vi.fn(async () => '/tmp/app-1.1.0'),
  restartInto: vi.fn(),
}))
vi.mock('./localServer', () => ({ hasEmbeddedServerBinary: vi.fn(() => true) }))
vi.mock('./clientLog', () => ({ recordError: vi.fn() }))
vi.mock('./store', () => ({
  getStore: () => ({ get: () => 'en' }),
}))

import { installFusedUpdate } from './fusedUpdate'
import { downloadAndInstall, restartInto } from './install'
import { hasEmbeddedServerBinary } from './localServer'
import { dialog } from 'electron'
import { platformKey } from './updater'

// Keyed by the platform the test process runs on, because that is what the
// module picks out of the server's map. Hard-coding "win32-x64" made these
// tests pass only on Windows.
const key = platformKey(process.platform, process.arch)
const downloads = { [key]: ['https://example.test/fused.zip'] }

beforeEach(() => {
  vi.clearAllMocks()
  vi.mocked(hasEmbeddedServerBinary).mockReturnValue(true)
  vi.mocked(dialog.showMessageBox).mockResolvedValue({ response: 1 } as never)
})

describe('installFusedUpdate', () => {
  it('refuses on a thin build, which has no bundled server to update', async () => {
    vi.mocked(hasEmbeddedServerBinary).mockReturnValue(false)

    const res = await installFusedUpdate('1.1.0', downloads, {}, null)

    expect(res.ok).toBe(false)
    expect(downloadAndInstall).not.toHaveBeenCalled()
    expect(restartInto).not.toHaveBeenCalled()
  })

  it('installs the all-in-one package for the platform, never a thin/payload URL', async () => {
    // The whole point of this path: the archive carries client AND server.
    // Anything else (the thin package deletes `server/`, the payload leaves it
    // stale) must never reach the installer from here.
    const urls = ['https://example.test/fused.zip', 'https://mirror.test/fused.zip']

    const res = await installFusedUpdate('1.1.0', { [key]: urls }, {}, null)

    expect(res.ok).toBe(true)
    expect(downloadAndInstall).toHaveBeenCalledWith(urls, '1.1.0', {})
  })

  it('restarts into the new version only when the user asks', async () => {
    vi.mocked(dialog.showMessageBox).mockResolvedValue({ response: 0 } as never)

    await installFusedUpdate('1.1.0', downloads, {}, null)

    expect(restartInto).toHaveBeenCalledWith('1.1.0')
  })

  it('leaves the app running when the user defers the restart', async () => {
    // The pointer is already flipped, so "later" still lands on the new version
    // at the next launch.
    const res = await installFusedUpdate('1.1.0', downloads, {}, null)

    expect(res.ok).toBe(true)
    expect(restartInto).not.toHaveBeenCalled()
  })

  it('reports a release with no package for this platform instead of downloading', async () => {
    const other = key === 'win32-x64' ? 'darwin-arm64' : 'win32-x64'
    const res = await installFusedUpdate('1.1.0', { [other]: ['https://example.test/other.zip'] }, {}, null)

    expect(res.ok).toBe(false)
    expect(downloadAndInstall).not.toHaveBeenCalled()
    expect(dialog.showMessageBox).toHaveBeenCalled()
  })

  it('keeps the current version when the download or install fails', async () => {
    vi.mocked(downloadAndInstall).mockRejectedValueOnce(new Error('mirror unreachable'))

    const res = await installFusedUpdate('1.1.0', downloads, {}, null)

    expect(res.ok).toBe(false)
    expect(res.error).toContain('mirror unreachable')
    expect(restartInto).not.toHaveBeenCalled()
    // The call is (parent, options); the tuple type of the mocked overload only
    // lists one element, so read the options through a cast.
    const calls = vi.mocked(dialog.showMessageBox).mock.calls as unknown as Array<
      [unknown, { detail?: string }]
    >
    expect(String(calls[calls.length - 1][1]?.detail)).toContain('mirror unreachable')
  })
})
