import { describe, it, expect, vi, beforeEach } from 'vitest'

const h = vi.hoisted(() => ({
  serverUrl: '',
  servers: [] as Array<{ url: string; name?: string }>,
  switches: [] as Array<[string, string]>,
  logs: [] as string[],
}))

vi.mock('electron', () => ({
  app: {
    commandLine: {
      appendSwitch: (name: string, value: string) => h.switches.push([name, value]),
    },
  },
}))

vi.mock('./store', () => ({
  getStore: () => ({
    get: (k: string) => {
      if (k === 'serverUrl') return h.serverUrl
      if (k === 'servers') return h.servers
      return undefined
    },
  }),
}))

vi.mock('./clientLog', () => ({
  record: (_level: string, _tag: string, msg: string) => h.logs.push(msg),
}))

import {
  applySecureContextSwitch,
  needsSecureContextRelaunch,
  promotableOrigins,
  _setPromotedForTests,
} from './secureContext'

const SWITCH = 'unsafely-treat-insecure-origin-as-secure'

beforeEach(() => {
  h.serverUrl = ''
  h.servers = []
  h.switches = []
  h.logs = []
  _setPromotedForTests([])
})

describe('promotableOrigins', () => {
  it('covers the active server and every saved one, deduplicated', () => {
    h.serverUrl = 'http://192.168.1.5:20000/chat'
    h.servers = [
      { url: 'http://192.168.1.5:20000' },
      { url: 'http://192.168.1.6:20314' },
      { url: 'http://192.168.1.6:20314' },
    ]
    expect(promotableOrigins()).toEqual([
      'http://192.168.1.5:20000',
      'http://192.168.1.6:20314',
    ])
  })

  it('skips https (already secure) and unparseable URLs', () => {
    h.serverUrl = 'https://nas.example.com:8443'
    h.servers = [{ url: 'not a url' }, { url: '' }, { url: 'http://10.0.0.2:20000' }]
    expect(promotableOrigins()).toEqual(['http://10.0.0.2:20000'])
  })

  it('returns nothing when no server is configured', () => {
    expect(promotableOrigins()).toEqual([])
  })
})

describe('applySecureContextSwitch', () => {
  it('registers every origin in one comma-separated switch', () => {
    h.serverUrl = 'http://192.168.1.5:20000'
    h.servers = [{ url: 'http://192.168.1.6:20314' }]
    applySecureContextSwitch()
    expect(h.switches).toEqual([
      [SWITCH, 'http://192.168.1.5:20000,http://192.168.1.6:20314'],
    ])
    expect(h.logs.join(' ')).toContain('192.168.1.5:20000')
  })

  it('adds no switch when there is nothing to promote', () => {
    h.serverUrl = 'https://nas.example.com:8443'
    applySecureContextSwitch()
    expect(h.switches).toEqual([])
  })
})

describe('needsSecureContextRelaunch', () => {
  it('is true for an http origin this process did not promote', () => {
    _setPromotedForTests(['http://192.168.1.5:20000'])
    expect(needsSecureContextRelaunch('http://192.168.1.7:20777')).toBe(true)
  })

  it('is false for an origin that was promoted at startup', () => {
    _setPromotedForTests(['http://192.168.1.5:20000'])
    expect(needsSecureContextRelaunch('http://192.168.1.5:20000/login')).toBe(false)
  })

  it('is false for https (already a secure context)', () => {
    expect(needsSecureContextRelaunch('https://nas.example.com:8443')).toBe(false)
  })

  it('is false for a URL it cannot parse — the connect flow reports that instead', () => {
    expect(needsSecureContextRelaunch('not a url')).toBe(false)
  })
})
