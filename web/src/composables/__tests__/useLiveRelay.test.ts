import { afterEach, describe, expect, it, vi } from 'vitest'
import { openRelay, relayConnectURL, relayPublishDefaults } from '../useLiveRelay'
import { SETTINGS_KEY_PREFIX } from '@/utils/settingsStorage'

/**
 * A relayed meeting's data channel, reduced to the one event this file needs.
 *
 * `Room` is a stub: `openRelay` only constructs it, registers handlers on it and
 * connects, so the real SDK (and a real relay) is not part of what is being
 * tested here.
 */
vi.mock('livekit-client', () => {
  class Room {
    on = vi.fn()
    connect = vi.fn(async () => undefined)
    disconnect = vi.fn()
    name = 'room-1'
    localParticipant = {}
  }
  const RoomEvent = {
    TrackSubscribed: 'trackSubscribed',
    TrackUnsubscribed: 'trackUnsubscribed',
    DataReceived: 'dataReceived',
    ActiveSpeakersChanged: 'activeSpeakersChanged',
    Disconnected: 'disconnected',
    ConnectionStateChanged: 'connectionStateChanged',
  }
  const ConnectionState = { Disconnected: 'disconnected' }
  return { Room, RoomEvent, ConnectionState }
})

/** The handler `openRelay` registered for one event. */
function handlerFor(room: unknown, event: string): (...args: unknown[]) => void {
  const on = (room as { on: { mock: { calls: Array<[string, (...a: unknown[]) => void]> } } }).on
  const found = on.mock.calls.find(([name]) => name === event)
  if (!found) throw new Error(`no handler for ${event}`)
  return found[1]
}

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

/**
 * Who sent a data frame.
 *
 * The SDK's event carries the sender twice: the resolved `RemoteParticipant`
 * (absent until this client has processed that participant's join) and the
 * packet's own `participantIdentity` (always present). A viewer's FIRST frame is
 * the chat-history request it sends the moment its room connects — i.e. exactly
 * when the participant object may still be missing. Reading only the participant
 * dropped that request: the publisher refuses an unattributed frame, so a member
 * joining a relayed meeting late saw an empty chat panel.
 */
describe('relay data attribution', () => {
  const payload = new TextEncoder().encode('{"t":"chat-history-req"}')

  it('falls back to the packet identity while the participant is unknown', async () => {
    const onData = vi.fn()
    const room = await openRelay('ws://relay', 'token', { onData })
    const received = handlerFor(room, 'dataReceived')

    // What the SDK emits for a frame that arrived before the join update.
    received(payload, undefined, 0, undefined, 0, 'viewer:v7')

    expect(onData).toHaveBeenCalledWith(payload, 'viewer:v7')
  })

  it('prefers the resolved participant when it is there', async () => {
    const onData = vi.fn()
    const room = await openRelay('ws://relay', 'token', { onData })
    const received = handlerFor(room, 'dataReceived')

    received(payload, { identity: 'viewer:v7' }, 0, undefined, 0, 'viewer:v7')

    expect(onData).toHaveBeenCalledWith(payload, 'viewer:v7')
  })
})
