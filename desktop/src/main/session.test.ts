import { describe, it, expect, vi, beforeEach } from 'vitest'
import { clearCacheAndReload } from './session'

/**
 * Behaviour of the shared reload primitive (issue #559).
 *
 * This is the seam the fix lives in: `storagesToClear()` is trivially correct
 * on its own, but the bug was in *what `session.ts` passes it*. Asserting the
 * policy table alone would leave "ignore the scope argument" (the original
 * defect, in one line) undetected, so these tests drive the real
 * `clearCacheAndReload` and inspect the arguments Electron actually received.
 *
 * `electron` and `./window` are mocked exactly as `bridge.test.ts` does —
 * `session.ts` imports Electron at module scope, so it cannot load unmocked.
 */

const electronMock = vi.hoisted(() => ({
  clearCache: vi.fn(async () => undefined),
  clearStorageData: vi.fn(async (_opts?: { storages?: string[] }) => undefined),
  reload: vi.fn(),
}))

vi.mock('electron', () => ({
  session: {
    defaultSession: {
      clearCache: electronMock.clearCache,
      clearStorageData: electronMock.clearStorageData,
    },
  },
}))

vi.mock('./window', () => ({
  getMainWindow: () => ({ webContents: { reload: electronMock.reload } }),
}))

// `vi.mock` is hoisted above the import, so `session` loads with the stubs in
// place even though the import sits at the top of the file.

function lastStorages(): string[] {
  const calls = electronMock.clearStorageData.mock.calls
  expect(calls.length, 'clearStorageData must be called exactly once').toBe(1)
  return calls[0][0]?.storages ?? []
}

describe('clearCacheAndReload', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('keeps localStorage and cookies for the post-upgrade scope', async () => {
    await clearCacheAndReload('cache')
    const storages = lastStorages()
    // The whole point of #559: these hold every per-device preference, and the
    // server upgrade does not invalidate any of them.
    expect(storages).not.toContain('localstorage')
    expect(storages).not.toContain('indexdb')
    expect(storages).not.toContain('cookies')
  })

  it('still drops cached app-shell copies for the post-upgrade scope', async () => {
    // Otherwise the reload could keep serving chunks from the previous build,
    // which is the failure the reload exists to prevent. Exact set, so a later
    // "just add one more storage" edit cannot widen it silently.
    await clearCacheAndReload('cache')
    expect(new Set(lastStorages())).toEqual(new Set(['cachestorage', 'serviceworkers']))
  })

  it('clears everything for the explicit hard reload', async () => {
    // The documented Ctrl+Shift+R contract: signs out and resets preferences.
    await clearCacheAndReload('everything')
    expect(new Set(lastStorages())).toEqual(
      new Set(['localstorage', 'indexdb', 'cookies', 'cachestorage', 'serviceworkers']),
    )
  })

  it('always drops the HTTP cache and reloads the window', async () => {
    await clearCacheAndReload('cache')
    expect(electronMock.clearCache).toHaveBeenCalledTimes(1)
    expect(electronMock.reload).toHaveBeenCalledTimes(1)
  })

  it('reloads after storage is cleared, not before', async () => {
    // Reloading first would let the old page write its state back into the
    // storage this call is trying to clear.
    const order: string[] = []
    electronMock.clearStorageData.mockImplementationOnce(async () => { order.push('clearStorageData') })
    electronMock.reload.mockImplementationOnce(() => { order.push('reload') })
    await clearCacheAndReload('everything')
    expect(order).toEqual(['clearStorageData', 'reload'])
  })
})
