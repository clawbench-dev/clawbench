/**
 * The LiveKit client, wrapped down to the handful of operations this app needs.
 *
 * Two roles share it: the publisher connects once and publishes its tracks, a
 * viewer connects once and subscribes to whatever the host publishes. Both keep
 * their existing ClawBench WebSocket for room state (who joined, who may speak);
 * only MEDIA and the control bus move here.
 *
 * The control bus rides LiveKit's data channel instead of a server relay: both
 * ends are already in the room, the message shape (`ControlMessage`) is
 * unchanged, and it keeps the server out of a path it never had to understand.
 */
import type {
  Room,
  RemoteParticipant,
  RemoteTrack,
  RemoteTrackPublication,
  Track,
} from 'livekit-client'
import { appLog } from '@/utils/appLog'
import { cameraBudget, screenBudget } from './liveMediaPolicy'

const TAG = 'LiveRelay'

/**
 * The relay client is ~2.6 MB. It is loaded on demand instead of at import
 * time: this module is reachable from the chat screen (useLiveShare), so a
 * static import put the whole LiveKit SDK on the first screen — measured as a
 * 2.5 MB `mediaFullscreenGuard` chunk and a 4.2 MB first-screen payload, over
 * the 3.6 MB budget. Only a meeting that actually joins a relay pays for it.
 */
type LiveKitModule = typeof import('livekit-client')
let liveKitPromise: Promise<LiveKitModule> | null = null

function loadLiveKit(): Promise<LiveKitModule> {
  liveKitPromise ??= import('livekit-client')
  return liveKitPromise
}

/** What the caller wants to hear about. All optional: a publisher ignores most. */
export interface RelayHandlers {
  /** A track arrived (viewer side). `source` tells screen from camera from mic. */
  onTrack?: (track: MediaStreamTrack, source: Track.Source, identity: string) => void
  /** A track went away (unpublished, or the participant left). */
  onTrackGone?: (track: MediaStreamTrack) => void
  /** A control frame from another participant. */
  onData?: (payload: Uint8Array, from: string) => void
  /** Identities currently speaking, most recent first. */
  onActiveSpeakers?: (identities: string[]) => void
  /** The media connection ended; the room itself may still be alive. */
  onDisconnected?: (reason: string) => void
}

/**
 * The URL the browser connects to.
 *
 * Always this page's own origin. The relay itself is plain ws:// with no
 * certificate, and a page served over https cannot open a plain socket, so the
 * server proxies the signaling handshake under /livekit. The media path is
 * unaffected: picture and sound still go straight from the browser to LiveKit.
 */
export function relayConnectURL(): string {
  const scheme = window.location.protocol === 'https:' ? 'wss:' : 'ws:'
  return `${scheme}//${window.location.host}/livekit`
}

/**
 * The encode settings a relayed room publishes with.
 *
 * Exported so the numbers can be asserted without standing up the SDK: the
 * SDK's own defaults are a 1080p / 15 fps screen share at 2.5 Mbps, visibly
 * soft and choppy for shared text, and nothing else in this file would reveal
 * that a user's 画面质量 choice never reached the relay.
 */
export function relayPublishDefaults(): {
  screenShareEncoding: { maxBitrate: number; maxFramerate: number }
  videoEncoding: { maxBitrate: number; maxFramerate: number }
} {
  const screen = screenBudget()
  const camera = cameraBudget()
  return {
    screenShareEncoding: { maxBitrate: screen.maxBitrate, maxFramerate: screen.maxFramerate },
    videoEncoding: { maxBitrate: camera.maxBitrate, maxFramerate: camera.maxFramerate },
  }
}

/**
 * Connect to the relay.
 *
 * `url` and `token` come from the `joined` frame; neither is stored — a
 * reconnect asks ClawBench for a fresh one.
 */
export async function openRelay(url: string, token: string, handlers: RelayHandlers = {}): Promise<Room> {
  const { Room, RoomEvent, ConnectionState } = await loadLiveKit()
  const room = new Room({
    // Screen content is the main picture; without this the adaptive stream
    // logic treats a mostly-static desktop as "not visible" and pauses it.
    adaptiveStream: false,
    dynacast: false,
    // Take the same budgets the peer-to-peer path uses, so a relayed meeting
    // looks like a direct one (see relayPublishDefaults).
    publishDefaults: relayPublishDefaults(),
  })

  room.on(RoomEvent.TrackSubscribed, (track: RemoteTrack, pub: RemoteTrackPublication, participant) => {
    handlers.onTrack?.(track.mediaStreamTrack, pub.source, participant.identity)
  })
  room.on(RoomEvent.TrackUnsubscribed, (track: RemoteTrack) => {
    handlers.onTrackGone?.(track.mediaStreamTrack)
  })
  room.on(RoomEvent.DataReceived, (payload: Uint8Array, participant?: RemoteParticipant) => {
    handlers.onData?.(payload, participant?.identity ?? '')
  })
  room.on(RoomEvent.ActiveSpeakersChanged, (speakers) => {
    handlers.onActiveSpeakers?.(speakers.map((p) => p.identity))
  })
  room.on(RoomEvent.Disconnected, (reason?: unknown) => {
    handlers.onDisconnected?.(String(reason ?? 'disconnected'))
  })
  room.on(RoomEvent.ConnectionStateChanged, (state) => {
    if (state === ConnectionState.Disconnected) handlers.onDisconnected?.('connection-state')
  })

  await room.connect(url, token)
  appLog.i(TAG, 'relay connected', room.name)
  return room
}

/** Disconnect and release everything. Safe to call twice. */
export function closeRelay(room: Room | null): void {
  if (!room) return
  try {
    room.disconnect()
  } catch (e) {
    appLog.d(TAG, 'disconnect failed', e)
  }
}

/**
 * Publish (or replace) the main picture.
 *
 * Screen share and camera both travel as a video track; `source` is what lets
 * the viewer tell them apart, so it must match what the picture actually is.
 */
export type VideoSource = 'screen_share' | 'camera'

export async function publishVideo(
  room: Room,
  track: MediaStreamTrack | null,
  source: VideoSource,
): Promise<void> {
  const { Track } = await loadLiveKit()
  const want = source === 'screen_share' ? Track.Source.ScreenShare : Track.Source.Camera
  const lp = room.localParticipant
  const existing = [...lp.videoTrackPublications.values()].find((p) => p.source === want)
  if (!track) {
    if (existing?.track) await lp.unpublishTrack(existing.track, true)
    return
  }
  if (existing?.track && existing.track.mediaStreamTrack === track) return
  if (existing?.track) await lp.unpublishTrack(existing.track, true)
  await lp.publishTrack(track, { source: want })
}

/** Publish or stop the microphone. */
export async function publishMic(room: Room, track: MediaStreamTrack | null): Promise<void> {
  const { Track } = await loadLiveKit()
  const lp = room.localParticipant
  const existing = [...lp.audioTrackPublications.values()].find(
    (p) => p.source === Track.Source.Microphone,
  )
  if (!track) {
    if (existing?.track) await lp.unpublishTrack(existing.track, true)
    return
  }
  if (existing?.track && existing.track.mediaStreamTrack === track) return
  if (existing?.track) await lp.unpublishTrack(existing.track, true)
  await lp.publishTrack(track, { source: Track.Source.Microphone })
}

/** Send one control frame. Reliable: a lost chat line or laser release matters. */
export async function sendRelayData(room: Room, message: unknown): Promise<void> {
  try {
    const payload = new TextEncoder().encode(JSON.stringify(message))
    await room.localParticipant.publishData(payload, { reliable: true })
  } catch (e) {
    appLog.d(TAG, 'publishData failed', e)
  }
}

/** Decode a control frame, or null when it is not ours. */
export function decodeRelayData(payload: Uint8Array): unknown {
  try {
    return JSON.parse(new TextDecoder().decode(payload))
  } catch {
    return null
  }
}
