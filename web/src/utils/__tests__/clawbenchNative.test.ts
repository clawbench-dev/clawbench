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
