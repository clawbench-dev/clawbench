import { describe, expect, it, vi } from 'vitest'
import { resolveRemoteControlSupport } from '../remoteControlSupport'

describe('resolveRemoteControlSupport', () => {
  it('reports true only when the bridge says so', async () => {
    expect(await resolveRemoteControlSupport({ remoteControlSupported: () => true })).toBe(true)
    expect(await resolveRemoteControlSupport({ remoteControlSupported: async () => true })).toBe(true)
  })

  it('reports false when the platform has no injector', async () => {
    expect(await resolveRemoteControlSupport({ remoteControlSupported: () => false })).toBe(false)
  })

  it('reports false when the bridge is absent (browser / older shell)', async () => {
    // The publisher UI is desktop-only, but an older preload that lacks the
    // method must not read as "supported": the allow button would promise
    // something the app cannot do.
    expect(await resolveRemoteControlSupport(null)).toBe(false)
    expect(await resolveRemoteControlSupport(undefined)).toBe(false)
    expect(await resolveRemoteControlSupport({})).toBe(false)
  })

  it('reports false when the probe throws', async () => {
    const bridge = {
      remoteControlSupported: vi.fn(async () => {
        throw new Error('ipc closed')
      }),
    }
    expect(await resolveRemoteControlSupport(bridge)).toBe(false)
  })

  it('reports false for a non-boolean answer', async () => {
    // A bridge that resolves with a truthy string is not a promise that the
    // helper exists; only a literal true counts.
    expect(
      await resolveRemoteControlSupport({
        remoteControlSupported: (() => 'yes') as unknown as () => boolean,
      }),
    ).toBe(false)
  })
})
