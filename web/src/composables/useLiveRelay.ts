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
import {
  ConnectionState,
  Room,
  RoomEvent,
  Track,
  type RemoteParticipant,
  type RemoteTrack,
  type RemoteTrackPublication,
} from 'livekit-client'
import { appLog } from '@/utils/appLog'

const TAG = 'LiveRelay'

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
 * Connect to the relay.
 *
 * `url` is the ws/wss endpoint from the `joined` frame; `token` is the
 * short-lived credential the server signed. Neither is stored anywhere — a
 * reconnect asks ClawBench for a fresh one.
 */
export async function openRelay(url: string, token: string, handlers: RelayHandlers = {}): Promise<Room> {
  const room = new Room({
    // Screen content is the main picture; without this the adaptive stream
    // logic treats a mostly-static desktop as "not visible" and pauses it.
    adaptiveStream: false,
    dynacast: false,
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
export async function publishVideo(
  room: Room,
  track: MediaStreamTrack | null,
  source: Track.Source.ScreenShare | Track.Source.Camera,
): Promise<void> {
  const lp = room.localParticipant
  const existing = [...lp.videoTrackPublications.values()].find((p) => p.source === source)
  if (!track) {
    if (existing?.track) await lp.unpublishTrack(existing.track, true)
    return
  }
  if (existing?.track && existing.track.mediaStreamTrack === track) return
  if (existing?.track) await lp.unpublishTrack(existing.track, true)
  await lp.publishTrack(track, { source })
}

/** Publish or stop the microphone. */
export async function publishMic(room: Room, track: MediaStreamTrack | null): Promise<void> {
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
