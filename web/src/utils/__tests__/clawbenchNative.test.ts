import { describe, it, expect, vi, afterEach } from 'vitest'
import { getNative, isNativeApp, reconnectTunnel, callNative } from '../clawbenchNative'

function setNative(obj: unknown) {
  ;(window as unknown as { ClawBenchNative?: unknown }).ClawBenchNative = obj
}

afterEach(() => {
  delete (window as unknown as { ClawBenchNative?: unknown }).ClawBenchNative
  vi.restoreAllMocks()
})

describe('clawbenchNative bridge wrapper', () => {
  it('getNative returns undefined when no bridge', () => {
    expect(getNative()).toBeUndefined()
  })

  it('getNative returns the bridge object', () => {
    const fake = { isNativeApp: () => true }
    setNative(fake)
    expect(getNative()).toBe(fake)
  })

  it('isNativeApp is true only when bridge reports true', () => {
    setNative({ isNativeApp: () => true })
    expect(isNativeApp()).toBe(true)
    setNative({ isNativeApp: () => false })
    expect(isNativeApp()).toBe(false)
    expect(isNativeApp()).toBe(false)
  })

  it('callNative awaits both sync and async bridge results', async () => {
    const syncNative = { getPassword: () => 'pwd' }
    setNative(syncNative)
    expect(await callNative(n => n.getPassword())).toBe('pwd')

    const asyncNative = { getPassword: () => Promise.resolve('pwd2') }
    setNative(asyncNative)
    expect(await callNative(n => n.getPassword())).toBe('pwd2')
  })

  it('callNative resolves undefined when bridge is missing', async () => {
    expect(await callNative(n => n.getPassword())).toBeUndefined()
  })

  it('reconnectTunnel resolves via Electron-style Promise', async () => {
    const native = { reconnectTunnelAsync: () => Promise.resolve(true) }
    setNative(native)
    expect(await reconnectTunnel()).toBe(true)
  })

  it('reconnectTunnel resolves via Android-style global callback', async () => {
    const native = {
      reconnectTunnelAsync: () => {
        setTimeout(() => {
          const cb = (window as unknown as { __clawbenchReconnectResult?: (v: boolean) => void }).__clawbenchReconnectResult
          cb?.(true)
        }, 5)
      },
    }
    setNative(native)
    expect(await reconnectTunnel()).toBe(true)
  })

  it('reconnectTunnel falls back to blocking reconnectTunnel', async () => {
    const native = { reconnectTunnel: () => false }
    setNative(native)
    expect(await reconnectTunnel()).toBe(false)
  })

  it('reconnectTunnel resolves false when nothing available', async () => {
    expect(await reconnectTunnel()).toBe(false)
  })
})

/**
 * The tunnel-transport bridge methods.
 *
 * Both are declared optional in the contract, because older hosts predate the
 * h2 transport entirely. The caller's contract is therefore: "call if present,
 * do nothing otherwise" — an absent method must never throw, and must never be
 * mistaken for a value. The two branches (Electron Promise / Android sync, and
 * legacy) are exercised below because a host that implements only one of them
 * is the realistic case, not a hypothetical.
 */
describe('clawbenchNative tunnel transport bridge', () => {
  it('getTunnelTransport resolves an Electron Promise and an Android sync value alike', async () => {
    setNative({ getTunnelTransport: () => Promise.resolve('both') })
    expect(await getNative()?.getTunnelTransport?.()).toBe('both')

    setNative({ getTunnelTransport: () => 'ssh' })
    expect(await getNative()?.getTunnelTransport?.()).toBe('ssh')
  })

  it('getTunnelTransport resolves undefined when the host predates it', async () => {
    setNative({ isNativeApp: () => true })
    expect(await getNative()?.getTunnelTransport?.()).toBeUndefined()
  })

  it('getActiveTunnelTransport reports the transport that carried the session', async () => {
    // Distinct from the configured preference: a 'both' client reports the one
    // that actually won, which is what a status display wants.
    setNative({ getActiveTunnelTransport: () => Promise.resolve('h2') })
    expect(await getNative()?.getActiveTunnelTransport?.()).toBe('h2')
  })

  it('getActiveTunnelTransport resolves undefined on a legacy host', async () => {
    setNative({ isNativeApp: () => true })
    expect(await getNative()?.getActiveTunnelTransport?.()).toBeUndefined()
  })

  it('setTunnelTransportH2Enabled forwards the boolean on an Android sync host', () => {
    // Android's bridge is synchronous: a void method, no Promise. Nothing in
    // the call path may assume a thenable.
    const setTunnelTransportH2Enabled = vi.fn()
    setNative({ setTunnelTransportH2Enabled })

    const result = getNative()?.setTunnelTransportH2Enabled?.(true)

    expect(setTunnelTransportH2Enabled).toHaveBeenCalledWith(true)
    expect(result).toBeUndefined()
  })

  it('setTunnelTransportH2Enabled degrades to a no-op on a host that predates it', () => {
    // A legacy Android build: the method is absent. Optional chaining is the
    // whole degradation story — a throw here would abort the toggle handler.
    setNative({ isNativeApp: () => true })

    expect(() => getNative()?.setTunnelTransportH2Enabled?.(true)).not.toThrow()
  })

  it('setTunnelTransportH2Enabled degrades to a no-op with no bridge at all (web mode)', () => {
    expect(() => getNative()?.setTunnelTransportH2Enabled?.(true)).not.toThrow()
  })

  it('getTunnelTransportH2Enabled resolves an Electron Promise and an Android sync value alike', async () => {
    setNative({ getTunnelTransportH2Enabled: () => Promise.resolve(true) })
    expect(await getNative()?.getTunnelTransportH2Enabled?.()).toBe(true)

    setNative({ getTunnelTransportH2Enabled: () => false })
    expect(await getNative()?.getTunnelTransportH2Enabled?.()).toBe(false)
  })

  it('getTunnelTransportH2Enabled resolves undefined on a legacy host', async () => {
    // The caller keys row visibility off this: undefined means "hide the row",
    // never "off".
    setNative({ isNativeApp: () => true })
    expect(await getNative()?.getTunnelTransportH2Enabled?.()).toBeUndefined()
  })
})
