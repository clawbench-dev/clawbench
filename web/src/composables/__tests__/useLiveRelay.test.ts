import { afterEach, describe, expect, it, vi } from 'vitest'
import { relayConnectURL } from '../useLiveRelay'

/**
 * Where the page points the relay client.
 *
 * Always this page's own origin: the relay is plain ws:// with no certificate,
 * and an https page may not open a plain socket. The server proxies the
 * handshake under /livekit; media never comes through here.
 */

afterEach(() => {
  vi.unstubAllGlobals()
})

describe('relayConnectURL', () => {
  it('uses this origin’s /livekit path over https', () => {
    vi.stubGlobal('location', { protocol: 'https:', host: 'clawbench.example.com:20000' })

    expect(relayConnectURL()).toBe('wss://clawbench.example.com:20000/livekit')
  })

  it('follows a plain-http page to a plain socket', () => {
    vi.stubGlobal('location', { protocol: 'http:', host: '10.0.0.5:20000' })

    expect(relayConnectURL()).toBe('ws://10.0.0.5:20000/livekit')
  })
})
