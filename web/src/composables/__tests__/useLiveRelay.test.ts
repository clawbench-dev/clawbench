import { afterEach, describe, expect, it, vi } from 'vitest'
import { relayConnectURL, relayPublishDefaults } from '../useLiveRelay'
import { SETTINGS_KEY_PREFIX } from '@/utils/settingsStorage'

/**
 * Where the page points the relay client.
 *
 * Always this page's own origin: the relay is plain ws:// with no certificate,
 * and an https page may not open a plain socket. The server proxies the
 * handshake under /livekit; media never comes through here.
 */

afterEach(() => {
  vi.unstubAllGlobals()
  localStorage.clear()
})

/**
 * The SDK's defaults are 1080p / 15 fps / 2.5 Mbps for a screen share. A relayed
 * meeting must publish at the quality the user chose instead, or the relay path
 * silently stays the worst-looking one.
 */
describe('relayPublishDefaults', () => {
  it('publishes the screen at the chosen quality', () => {
    localStorage.setItem(SETTINGS_KEY_PREFIX + 'liveScreenBitrate', JSON.stringify('8'))
    localStorage.setItem(SETTINGS_KEY_PREFIX + 'liveScreenFps', JSON.stringify('24'))

    expect(relayPublishDefaults().screenShareEncoding).toEqual({
      maxBitrate: 8_000_000,
      maxFramerate: 24,
    })
  })

  it('publishes the camera with the camera settings, not the screen ones', () => {
    localStorage.setItem(SETTINGS_KEY_PREFIX + 'liveScreenBitrate', JSON.stringify('12'))
    localStorage.setItem(SETTINGS_KEY_PREFIX + 'liveCameraBitrate', JSON.stringify('1.5'))
    localStorage.setItem(SETTINGS_KEY_PREFIX + 'liveCameraFps', JSON.stringify('15'))

    expect(relayPublishDefaults().videoEncoding).toEqual({ maxBitrate: 1_500_000, maxFramerate: 15 })
  })
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
