import path from 'node:path'
import { describe, expect, it } from 'vitest'
import {
  EMBEDDED_DEFAULT_PORT,
  chooseEmbeddedPort,
  embeddedServerArgs,
  embeddedServerBinaryPath,
  embeddedServerUrl,
  isClawBenchHealth,
  isLocalServerUrl,
  reservedPortsForEmbedded,
  sameServerUrl,
  sessionCookieName,
  shouldAdoptEmbeddedServer,
} from './localServerPolicy'

describe('chooseEmbeddedPort', () => {
  it('keeps the preferred port when it is free — browser storage is keyed by origin', () => {
    expect(chooseEmbeddedPort({ preferred: 23456, inUse: [], reserved: [] })).toBe(23456)
  })

  it('walks up from the default when the preferred port is taken', () => {
    expect(chooseEmbeddedPort({ preferred: 20000, inUse: [20000], reserved: [] })).toBe(20001)
  })

  it('skips reserved ports even when they are free', () => {
    // 20001 belongs to a saved remote server: binding it would make the app's
    // saved URL for that machine resolve to our embedded server instead.
    expect(chooseEmbeddedPort({ preferred: 20000, inUse: [20000], reserved: [20001] })).toBe(20002)
  })

  it('skips the whole in-use window in one go', () => {
    expect(chooseEmbeddedPort({ preferred: 20000, inUse: [20000, 20001, 20002], reserved: [] })).toBe(20003)
  })

  it('rejects a preferred port below 1024 (bind would need privilege)', () => {
    expect(chooseEmbeddedPort({ preferred: 80, inUse: [], reserved: [] })).toBe(EMBEDDED_DEFAULT_PORT)
  })

  it('falls back below the default when nothing above the preferred port is free', () => {
    // A preferred port near the ceiling: the upward scan has room but the test
    // pins the wrap-around, which is the branch that would otherwise return 0
    // and fail the launch outright.
    const inUse = Array.from({ length: 20000 }, (_, i) => 45536 + i)
    expect(chooseEmbeddedPort({ preferred: 45536, inUse, reserved: [] })).toBe(20000)
  })
})

describe('reservedPortsForEmbedded', () => {
  it('lists the ports of saved servers, including implicit ones', () => {
    const ports = reservedPortsForEmbedded([
      'https://192.168.1.10:8443',
      'http://nas.local',
      'https://example.com',
      'not a url',
    ])
    expect(ports.sort((a, b) => a - b)).toEqual([80, 443, 8443])
  })
})

describe('embeddedServerArgs', () => {
  it('pins the port and data directory and asks for the managed lifecycle', () => {
    expect(embeddedServerArgs({ port: 20123, dataDir: 'C:\\Users\\me\\AppData\\ClawBench\\server' })).toEqual([
      '--port', '20123',
      '--data-dir', 'C:\\Users\\me\\AppData\\ClawBench\\server',
      '--host', '0.0.0.0',
      '--exit-on-stdin-close',
      '--managed',
    ])
  })

  it('honours an explicit host', () => {
    const args = embeddedServerArgs({ port: 1, dataDir: '/d', host: '127.0.0.1' })
    expect(args[args.indexOf('--host') + 1]).toBe('127.0.0.1')
  })
})

describe('isClawBenchHealth', () => {
  it('accepts only the app marker', () => {
    expect(isClawBenchHealth({ app: 'clawbench', version: 'v1' })).toBe(true)
    expect(isClawBenchHealth({ app: 'something-else' })).toBe(false)
    expect(isClawBenchHealth({})).toBe(false)
    expect(isClawBenchHealth('clawbench')).toBe(false)
    expect(isClawBenchHealth(null)).toBe(false)
  })
})

describe('sessionCookieName', () => {
  it('mirrors the server scoping rule', () => {
    expect(sessionCookieName(20000)).toBe('clawbench_session')
    expect(sessionCookieName(20123)).toBe('cb20123_clawbench_session')
  })
})

describe('isLocalServerUrl / embeddedServerUrl', () => {
  it('recognises the loopback forms', () => {
    expect(isLocalServerUrl(embeddedServerUrl(20000))).toBe(true)
    expect(isLocalServerUrl('http://localhost:20000')).toBe(true)
    expect(isLocalServerUrl('https://192.168.1.5:20000')).toBe(false)
    expect(isLocalServerUrl('file:///tmp/login.html')).toBe(false)
    expect(isLocalServerUrl('garbage')).toBe(false)
  })
})

describe('sameServerUrl', () => {
  it('identifies a server by its origin', () => {
    expect(sameServerUrl('http://127.0.0.1:20000', 'HTTP://127.0.0.1:20000/')).toBe(true)
    expect(sameServerUrl('https://nas.local:8443/', 'https://nas.local:8443')).toBe(true)
  })

  it('does not confuse a different port, scheme or host', () => {
    expect(sameServerUrl('http://127.0.0.1:20000', 'http://127.0.0.1:20001')).toBe(false)
    expect(sameServerUrl('http://nas.local:20000', 'https://nas.local:20000')).toBe(false)
    // The built-in server keeps its own port while an unrelated one is saved.
    expect(sameServerUrl('http://127.0.0.1:20000', 'http://192.168.1.5:20000')).toBe(false)
    expect(sameServerUrl('', 'http://127.0.0.1:20000')).toBe(false)
  })
})

describe('shouldAdoptEmbeddedServer', () => {
  it('opens the built-in server when nothing has been chosen yet', () => {
    expect(shouldAdoptEmbeddedServer('', '')).toBe(true)
    expect(shouldAdoptEmbeddedServer('', 'http://127.0.0.1:20000')).toBe(true)
  })

  it('keeps a server the user chose, across restarts', () => {
    // The bug this pins: every launch used to rewrite serverUrl to the built-in
    // address, so "connect to my own machine" silently reverted on restart.
    expect(shouldAdoptEmbeddedServer('https://nas.local:20000', 'http://127.0.0.1:20000')).toBe(false)
  })

  it('follows the built-in server across a port change', () => {
    // Same server, new port (the preferred one was taken this time): the app
    // must reopen the new address rather than a dead one.
    expect(shouldAdoptEmbeddedServer('http://127.0.0.1:20000', 'http://127.0.0.1:20000')).toBe(true)
    expect(shouldAdoptEmbeddedServer('http://127.0.0.1:20000/', 'http://127.0.0.1:20000')).toBe(true)
  })
})

describe('embeddedServerBinaryPath', () => {
  it('puts the server next to the app executable, in server/', () => {
    // electron-builder's extraFiles places it at <app root>/server/ — NOT under
    // resources/, which a desktop payload update replaces wholesale. Getting
    // this wrong makes a fused install look like a thin one (and disables the
    // version gate's "reinstall the all-in-one package" guard).
    expect(embeddedServerBinaryPath('/opt/ClawBench/clawbench-desktop')).toBe(
      path.join('/opt/ClawBench', 'server', 'clawbench.exe'),
    )
  })

  it('never looks inside resources/', () => {
    expect(embeddedServerBinaryPath('/opt/ClawBench/clawbench-desktop')).not.toContain('resources')
  })
})
