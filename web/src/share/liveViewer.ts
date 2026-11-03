/**
 * Viewer-side live share client: signalling socket + one media transport.
 *
 * Kept free of Vue so the state machine can be unit tested directly. The
 * component wraps it and mirrors `state` into reactive refs.
 *
 * Protocol (see internal/liveshare/signal.go for the authoritative shapes):
 *   server → joined | answer | ice | full | publisher-gone | talk-* | error
 *   client → join | offer | ice | talk-request | talk-start | talk-end
 *
 * Two topologies share this class, chosen by the `joined` frame:
 *
 *  - **p2p** (default): the socket carries only SDP/ICE and the media rides one
 *    RTCPeerConnection. `iceServers` is passed through verbatim, so a TURN
 *    rollout needs no change here.
 *  - **sfu**: `joined` also carries `mode: 'sfu'` and a `relay` block. No
 *    PeerConnection is created at all — media AND the control bus move to the
 *    LiveKit room, and the socket is left carrying room state only.
 *
 * The control-frame shapes are identical in both, so callers never branch.
 */
import { appLog } from '@/utils/appLog'
import { parseControlMessage, type ControlMessage } from './liveControl'
import {
  GUEST_SCREEN_BUDGET,
  applyContentHint,
  applyEncodeBudget,
  screenCaptureConstraints,
} from '@/composables/liveMediaPolicy'
import {
  closeRelay,
  decodeRelayData,
  openRelay,
  publishMic,
  publishVideo,
  sendRelayData,
} from '@/composables/useLiveRelay'
import type { Room, Track } from 'livekit-client'

const TAG = 'LiveViewer'

/**
 * LiveKit's `Track.Source` values, spelled out as strings.
 *
 * Imported as values they would drag the LiveKit runtime into this module (and
 * into every test that mounts the viewer). `useLiveRelay` already owns that
 * dependency, so the only thing needed here is the two source tags.
 */
const SOURCE_SCREEN = 'screen_share' as Track.Source.ScreenShare
const SOURCE_CAMERA = 'camera' as Track.Source.Camera

/** One health reading, as shown by the quality badge. */
export interface LiveStats {
  rttMs: number | null
  lossPct: number | null
  bitrateKbps: number | null
  jitterMs: number | null
}

export type LiveViewerState =
  | 'idle'
  | 'connecting' // socket opening, waiting for `joined`
  | 'negotiating' // offer sent, waiting for the answer
  | 'playing'
  | 'ended' // publisher gone or the share was closed
  | 'full' // room at capacity
  | 'name-taken' // somebody in the meeting already uses this display name
  | 'failed' // signalling or ICE failure

/**
 * Where the media goes when the room is relayed (SFU) rather than peer-to-peer.
 *
 * `token` is a short-lived credential the server signed for THIS viewer; it is
 * never stored, because a reconnect asks ClawBench for a fresh one.
 */
export interface RelayInfo {
  url: string
  token: string
  /** The LiveKit room name (the share token). */
  room?: string
}

/** Message shapes the server sends. Fields are optional by message type. */
interface ServerMessage {
  type?: string
  /**
   * Media topology. Absent (or 'p2p') means the classic PeerConnection path;
   * 'sfu' means `relay` is present and must be used instead.
   */
  mode?: string
  relay?: RelayInfo
  viewerId?: string
  title?: string
  sdp?: string
  candidate?: RTCIceCandidateInit
  iceServers?: RTCIceServer[]
  talkEnabled?: boolean
  /** Whether the publisher is running AI 听记 for this share. */
  notesEnabled?: boolean
  /** Whether the publisher sends a picture (false = voice meeting). */
  video?: boolean
  /** Whether the publisher's camera is on (video meetings). */
  camera?: boolean
  /** Whether the publisher's picture is a screen share (not a camera). */
  hostScreen?: boolean
  /** Whether the publisher can accept remote-control requests right now. */
  controlEnabled?: boolean
  /** Room-level switch for audience screen sharing. */
  screenEnabled?: boolean
  /** Viewer id currently sharing a screen ('' when nobody is). */
  screenSharer?: string
  /** Whether this frame is a screen-state update. */
  sharing?: boolean
  reason?: string
  message?: string
  /** Participant list, on `joined` and on every `roster` frame. */
  roster?: RosterMember[]
}

/**
 * One participant, as the server reports them.
 *
 * `id` is "host" for the publisher and the room's viewer id for everybody else;
 * comparing it with `viewer.viewerId` is how the page marks "you".
 */
export interface RosterMember {
  id: string
  name: string
  /** The publisher row: pinned first and labelled with the role. */
  host?: boolean
  /** That viewer's microphone is on. */
  talking?: boolean
  /** The publisher blocked that viewer's microphone. */
  muted?: boolean
}

export interface LiveViewerCallbacks {
  /** Fired once when the remote stream is attached (before playback starts). */
  onStream?: (stream: MediaStream) => void
  onStateChange?: (state: LiveViewerState, detail?: { reason?: string }) => void
  onTitle?: (title: string) => void
  /** Whether the room allows audience speech. Drives the "hold to talk" UI. */
  onTalkEnabled?: (enabled: boolean) => void
  /** Whether the publisher is running AI 听记 (shows the transcript panel). */
  onNotesEnabled?: (enabled: boolean) => void
  /**
   * Whether the publisher sends a picture. A voice meeting has none, so the
   * page renders a placeholder instead of waiting for a track that will never
   * arrive — and must not offer the remote-control button.
   */
  onVideoMode?: (video: boolean) => void
  /**
   * Whether the publisher's picture is a SCREEN. Remote control and annotation
   * only make sense then — a camera image has no desktop to inject into.
   */
  onHostScreen?: (screen: boolean) => void
  /**
   * Whether the publisher can accept a remote-control request (it has an input
   * injector and is sharing a whole screen). The viewer greys its button on
   * this; asking and being refused is the failure mode it prevents.
   */
  onControlEnabled?: (enabled: boolean) => void
  /** Whether the publisher's camera tile should be shown. */
  onCameraMode?: (camera: boolean) => void
  /** The publisher's camera picture (the picture-in-picture tile). */
  onCameraStream?: (stream: MediaStream | null) => void
  /** The room-level screen-sharing switch changed. */
  onScreenEnabled?: (enabled: boolean) => void
  /** Who is sharing a screen ('' = nobody). */
  onScreenSharer?: (viewerId: string) => void
  /** The screen a member is sharing, forwarded by the publisher. */
  onMemberScreenStream?: (stream: MediaStream | null) => void
  /** The room refused our screen share (switch off, or someone else is sharing). */
  onScreenDenied?: (reason: string) => void
  /** A control frame arrived (laser, ink, chat, presence, remote control). */
  onControl?: (msg: ControlMessage) => void
  /** The publisher muted us, or the room switch was turned off. */
  onTalkRevoked?: (reason: string) => void
  /** The publisher blocked (true) or released (false) our microphone. */
  onTalkMuted?: (muted: boolean) => void
  /** Our own viewer id, once the room has accepted us. */
  onJoined?: (viewerId: string) => void
  /** The full participant list, on join and on every change. */
  onRoster?: (roster: RosterMember[]) => void
}

export interface LiveViewerOptions {
  token: string
  /** The viewer's nickname (required by the server; shown to the publisher). */
  name: string
  callbacks?: LiveViewerCallbacks
  /** Injected for tests; defaults to the global WebSocket. */
  wsFactory?: (url: string) => WebSocket
  /** Injected for tests; defaults to the global RTCPeerConnection. */
  pcFactory?: (config: RTCConfiguration) => RTCPeerConnection
  /** Injected for tests; defaults to getUserMedia. */
  getUserMedia?: (constraints: MediaStreamConstraints) => Promise<MediaStream>
}

/** ws(s):// URL for the signalling socket of a share token. */
export function liveSignalUrl(
  token: string,
  origin?: { protocol: string; host: string },
  name?: string,
): string {
  const proto =
    (origin?.protocol ?? window.location.protocol) === 'https:' ? 'wss' : 'ws'
  const host = origin?.host ?? window.location.host
  const base = `${proto}://${host}/api/share/${encodeURIComponent(token)}/signal`
  // The nickname is REQUIRED by the server: it is what the publisher's audience
  // list and the persisted chat call this viewer by. Encoded because it is free
  // text (Chinese, emoji, spaces).
  return name ? `${base}?name=${encodeURIComponent(name)}` : base
}

export class LiveViewer {
  private readonly opts: LiveViewerOptions
  private readonly cb: LiveViewerCallbacks

  private ws: WebSocket | null = null
  private pc: RTCPeerConnection | null = null
  /** The audio transceiver reserved for our own speech (sendrecv, track=null). */
  private talkTx: RTCRtpTransceiver | null = null
  private micTrack: MediaStreamTrack | null = null
  /**
   * Video m-line we SEND our screen on (and receive the host's picture).
   *
   * Reserved at join time as sendrecv with no track, exactly like the
   * microphone: starting or stopping a share then only needs replaceTrack,
   * which does not renegotiate. Adding a track mid-call would require a second
   * offer and a stable-negotiation dance for a feature that is used rarely.
   */
  private screenTx: RTCRtpTransceiver | null = null
  /** Video m-line carrying ANOTHER viewer's screen, forwarded by the host. */
  private relayRxTx: RTCRtpTransceiver | null = null
  /**
   * Video m-line carrying the host's CAMERA (the picture-in-picture tile).
   *
   * Only present while the host shares a screen AND a camera. A camera-only
   * meeting puts the camera on `screenTx` instead, so this line carries
   * nothing and no tile is rendered.
   */
  private cameraRxTx: RTCRtpTransceiver | null = null

  private _state: LiveViewerState = 'idle'
  private _talkEnabled = false
  private _talking = false
  private _notesEnabled = false
  private _screenEnabled = false
  private _screenSharer = ''
  private _viewerId = ''
  private _roster: RosterMember[] = []
  /** Our own screen track while we are sharing (null when we are not). */
  private screenTrack: MediaStreamTrack | null = null
  /**
   * The video track on the relay m-line.
   *
   * Like the microphone line, this m-line is negotiated at join time and its
   * receiver exists immediately with a MUTED track — so `ontrack` fires even
   * when nobody is sharing. Only the server's screen-state decides whether it
   * is worth showing.
   */
  private relayTrack: MediaStreamTrack | null = null
  private memberScreenStream: MediaStream | null = null
  /** Control bus opened by the publisher. */
  private dc: RTCDataChannel | null = null
  /**
   * The track carrying the host's picture.
   *
   * Up to three video m-lines arrive (host picture, a forwarded member screen,
   * the camera tile) and `getStats()` reports one `inbound-rtp` entry per line.
   * Without an identity for the main one the quality badge would read whichever
   * entry happened to be iterated last — typically the camera tile.
   */
  private mainVideoTrack: MediaStreamTrack | null = null
  /** Previous byte counter, for turning cumulative bytes into a bitrate. */
  private lastStats: { at: number; bytes: number } | null = null
  private _closed = false
  /**
   * Whether a picture is expected. Defaults to true so a room that predates the
   * flag (or an older server) keeps behaving exactly as before.
   */
  private _videoEnabled = true
  /** Whether the host's picture is a screen (remote control / annotation). */
  private _hostScreen = false
  /**
   * Whether the publisher can be driven (input injector present AND a whole
   * screen shared). Defaults to false: the capability is the publisher's to
   * advertise, and a viewer that assumes it offers a button the host can only
   * refuse.
   */
  private _controlEnabled = false
  /** Whether the host's camera tile should be shown. */
  private _cameraEnabled = false
  /** Identities speaking right now, as the relay reports them (most recent first). */
  private _activeSpeakers: string[] = []

  // ── Relay (SFU) mode ──────────────────────────────────────────────────────
  /**
   * The LiveKit room, or null on the peer-to-peer path.
   *
   * Every relay-only operation keys off this handle rather than a separate
   * mode flag: there is nothing to publish to before the room resolves, and a
   * failed connect leaves it null (the session goes to `failed` instead of
   * silently falling back to a p2p room that may not exist).
   */
  private relayRoom: Room | null = null
  /**
   * Tracks of the host's picture, in arrival order.
   *
   * The relay delivers the screen and its audio as separate tracks (the p2p
   * path got one mixed audio track), but the page renders one `<video>` with a
   * single `srcObject` — so they are merged into one stream here.
   */
  private relayMainTracks: MediaStreamTrack[] = []
  private relayMainStream: MediaStream | null = null
  private relayMainVideoTrack: MediaStreamTrack | null = null
  /** The host's camera tile track (picture-in-picture). */
  private relayCameraTrack: MediaStreamTrack | null = null
  /** Another viewer's screen, relayed by the server. */
  private relayMemberTrack: MediaStreamTrack | null = null

  constructor(opts: LiveViewerOptions) {
    this.opts = opts
    this.cb = opts.callbacks ?? {}
  }

  get state(): LiveViewerState {
    return this._state
  }

  /** Whether a picture is expected from the publisher. */
  get videoEnabled(): boolean {
    return this._videoEnabled
  }

  /** Whether the publisher's picture is a screen share. */
  get hostScreen(): boolean {
    return this._hostScreen
  }

  /** Whether the publisher can accept a remote-control request. */
  get controlEnabled(): boolean {
    return this._controlEnabled
  }

  /** Whether the publisher's camera tile should be shown. */
  get cameraEnabled(): boolean {
    return this._cameraEnabled
  }

  get talkEnabled(): boolean {
    return this._talkEnabled
  }

  get talking(): boolean {
    return this._talking
  }

  /** Whether this share is transcribing (AI 听记). */
  get notesEnabled(): boolean {
    return this._notesEnabled
  }

  /** Our own viewer id, assigned by the server on `joined`. */
  get viewerId(): string {
    return this._viewerId
  }

  /** The participant list as the server last reported it. */
  get roster(): RosterMember[] {
    return this._roster
  }

  /**
   * Identities the relay currently reports as speaking.
   *
   * Only meaningful in relay mode: peer-to-peer mixes the audio on the
   * publisher's machine, so there is no per-speaker signal to report. The same
   * information is folded into `roster[].talking`.
   */
  get activeSpeakers(): string[] {
    return this._activeSpeakers
  }

  /** Whether the room allows audience screen sharing. */
  get screenEnabled(): boolean {
    return this._screenEnabled
  }

  /** Viewer id currently sharing a screen ('' = nobody). */
  get screenSharer(): string {
    return this._screenSharer
  }

  /** Whether we are the one sharing. */
  get isSharingScreen(): boolean {
    return this.screenTrack !== null
  }

  /** Open the socket and start negotiating. Safe to call once. */
  connect(): void {
    if (this._closed || this.ws) return
    this.setState('connecting')

    const factory =
      this.opts.wsFactory ?? ((url: string) => new WebSocket(url))
    let ws: WebSocket
    try {
      ws = factory(liveSignalUrl(this.opts.token, undefined, this.opts.name))
    } catch (e) {
      appLog.e(TAG, 'failed to open signalling socket', e)
      this.setState('failed', { reason: 'socket' })
      return
    }
    this.ws = ws
    ws.onmessage = (ev) => this.onMessage(ev.data)
    ws.onerror = () => {
      appLog.w(TAG, 'signalling socket error')
      if (this._state === 'connecting' || this._state === 'negotiating') {
        this.setState('failed', { reason: 'socket' })
      }
    }
    ws.onclose = () => {
      // A close after a terminal state is expected (the server closed us, or we
      // closed ourselves). Anything earlier means the link died before the
      // session could even start or while it was running.
      if (this._state === 'connecting' || this._state === 'negotiating' || this._state === 'playing') {
        this.setState('failed', { reason: 'closed' })
      }
    }
  }

  private onMessage(raw: unknown): void {
    let msg: ServerMessage
    try {
      msg = JSON.parse(String(raw)) as ServerMessage
    } catch {
      appLog.w(TAG, 'ignoring malformed frame')
      return
    }

    switch (msg.type) {
      case 'joined':
        this.onJoined(msg)
        break
      case 'answer':
        void this.onAnswer(msg)
        break
      case 'ice':
        void this.onRemoteICE(msg)
        break
      case 'full':
        this.setState('full')
        this.teardown()
        break
      case 'name-taken':
        // The room refused the name. Terminal for THIS attempt and nothing
        // more: the page puts the reader back on the name field, so this must
        // not look like a connection failure (which offers a retry that would
        // fail identically).
        this.setState('name-taken')
        this.teardown()
        break
      case 'publisher-gone':
        this.setState('ended', { reason: msg.reason })
        this.teardown()
        break
      case 'kicked':
        // Removed by the host. A distinct reason so the page can say "you were
        // removed" instead of the generic "the share ended".
        this.setState('ended', { reason: 'kicked' })
        this.teardown()
        break
      case 'roster':
        // Full snapshot, not a delta: the list is at most nine rows, and a
        // missed delta would leave the panel wrong until the page reloaded.
        this.applyRoster(msg.roster ?? [])
        break
      case 'talk-enabled':
        // The room-level switch changed after we joined. `joined` carries the
        // state at attach time, so this is the only way an already-connected
        // viewer learns the publisher enabled (or re-enabled) speaking.
        this._talkEnabled = msg.talkEnabled !== false
        this.cb.onTalkEnabled?.(this._talkEnabled)
        break
      case 'talk-revoked':
        // Three meanings share this frame: the publisher silenced THIS viewer
        // for the moment ('revoked'/'muted'), the publisher blocked them
        // outright ('host'), or the room switch went off ('disabled').
        // Only 'disabled' means the microphone toggle itself should disappear —
        // the other two are about the microphone, not about permission to have
        // one, and the viewer stays entitled to the button.
        this._talking = false
        void this.detachMic()
        if (msg.reason === 'disabled') {
          this._talkEnabled = false
          this.cb.onTalkEnabled?.(false)
        }
        if (msg.reason === 'host') this.cb.onTalkMuted?.(true)
        this.cb.onTalkRevoked?.(msg.reason ?? '')
        break
      case 'talk-unmuted':
        // The block is gone; the microphone stays off until the viewer turns it
        // back on themselves.
        this.cb.onTalkMuted?.(false)
        break
      case 'talk-disabled':
        // We asked to speak while the room switch was off. Flip the toggle back
        // instead of leaving it on and silently sending nothing.
        this._talking = false
        this._talkEnabled = false
        void this.detachMic()
        this.cb.onTalkEnabled?.(false)
        this.cb.onTalkRevoked?.(msg.reason ?? 'disabled')
        break
      case 'screen-state':
        this._screenEnabled = Boolean(msg.screenEnabled)
        this._screenSharer = msg.screenSharer ?? msg.viewerId ?? ''
        this.cb.onScreenEnabled?.(this._screenEnabled)
        this.cb.onScreenSharer?.(this._screenSharer)
        if (this._screenSharer) this.emitMemberScreen()
        else this.cb.onMemberScreenStream?.(null)
        break
      case 'picture-state':
        // Meetings have no type: the host can start or stop a screen share and
        // turn the camera on or off at any moment. All three flags arrive
        // together because they describe one picture — applying them one at a
        // time would render a combination that never existed.
        this._videoEnabled = Boolean(msg.video)
        this._hostScreen = Boolean(msg.hostScreen)
        this._cameraEnabled = Boolean(msg.camera)
        this.cb.onVideoMode?.(this._videoEnabled)
        this.cb.onHostScreen?.(this._hostScreen)
        this.cb.onCameraMode?.(this._cameraEnabled)
        break
      case 'control-enabled':
        // The publisher's own capability (injector + whole-screen picture), not
        // a viewer setting: it flips whenever the host swaps between a screen
        // and a window, or starts/stops sharing.
        this._controlEnabled = Boolean(msg.controlEnabled)
        this.cb.onControlEnabled?.(this._controlEnabled)
        break
      case 'notes-enabled':
        // The publisher turned AI 听记 on (or off) after we joined. `joined`
        // only carries the state at attach time, so without this frame a
        // viewer that arrived earlier would never learn about a mid-meeting
        // toggle — the transcript pane would stay hidden for the whole meeting.
        this._notesEnabled = Boolean(msg.notesEnabled)
        this.cb.onNotesEnabled?.(this._notesEnabled)
        break
      case 'screen-denied':
        // The room refused: either the switch is off or somebody else got
        // there first. Release the local capture immediately — leaving it live
        // would keep the browser's "sharing" indicator on with nothing sent.
        this.abortLocalScreenShare()
        this.cb.onScreenDenied?.(msg.reason ?? 'disabled')
        break
      case 'error':
        appLog.w(TAG, 'server rejected a control frame', msg.message)
        break
      default:
        appLog.w(TAG, 'unknown frame type', msg.type)
    }
  }

  /** Store one roster snapshot and hand it to the page. */
  private applyRoster(list: RosterMember[]): void {
    this._roster = list
    this.cb.onRoster?.(list)
  }

  private onJoined(msg: ServerMessage): void {
    this._talkEnabled = Boolean(msg.talkEnabled)
    this._videoEnabled = msg.video !== false
    this.cb.onVideoMode?.(this._videoEnabled)
    // Absent means "an older publisher": its picture was always a screen, and
    // it never had a camera. Both defaults keep such a share fully usable.
    this._hostScreen = msg.hostScreen !== false
    this.cb.onHostScreen?.(this._hostScreen)
    this._cameraEnabled = Boolean(msg.camera)
    this.cb.onCameraMode?.(this._cameraEnabled)
    this._controlEnabled = Boolean(msg.controlEnabled)
    this.cb.onControlEnabled?.(this._controlEnabled)
    this.cb.onTalkEnabled?.(this._talkEnabled)
    this._viewerId = msg.viewerId ?? ''
    this.cb.onJoined?.(this._viewerId)
    if (msg.roster) this.applyRoster(msg.roster)
    this._notesEnabled = Boolean(msg.notesEnabled)
    this.cb.onNotesEnabled?.(this._notesEnabled)
    this._screenEnabled = Boolean(msg.screenEnabled)
    this._screenSharer = msg.screenSharer ?? ''
    this.cb.onScreenEnabled?.(this._screenEnabled)
    this.cb.onScreenSharer?.(this._screenSharer)
    if (msg.title) this.cb.onTitle?.(msg.title)

    if (msg.mode === 'sfu' && msg.relay) {
      // Relayed room: media and the control bus live in LiveKit. Everything
      // below belongs to the peer-to-peer path (offer, transceivers, data
      // channel) and must NOT run — creating a PeerConnection here would open
      // a second, unused transport and send an offer nobody answers.
      void this.startRelay(msg.relay)
      return
    }

    const pcFactory =
      this.opts.pcFactory ??
      ((config: RTCConfiguration) => new RTCPeerConnection(config))
    const pc = pcFactory({ iceServers: msg.iceServers ?? [] })
    this.pc = pc

    // Control bus. The OFFERER creates it: an answer may not introduce an
    // m-line the offer did not have, so a channel created by the publisher
    // stays `connecting` forever and every chat/control frame is dropped —
    // which is indistinguishable from the buttons being dead. The publisher
    // adopts it through its own `ondatachannel`.
    const dc = pc.createDataChannel('cb-live', { ordered: true })
    this.dc = dc
    dc.onmessage = (e) => {
      const msg = parseControlMessage(String(e.data))
      if (msg) this.cb.onControl?.(msg)
    }
    dc.onopen = () => {
      // Ask for the transcript so far. Chat is never stored on this side, so
      // without the replay a viewer that joins late — or drops out and comes
      // back — sees an empty panel while the publisher still holds every line.
      // Sent on open, not at construction: the reply comes back on this same
      // channel, which is only usable once both ends agree it is open.
      this.sendControl({ t: 'chat-history-req' })
    }

    pc.ontrack = (ev) => {
      // Two video m-lines arrive here: the host's picture (with the mixed
      // audio, one stream) and — only while somebody is presenting — another
      // viewer's screen forwarded by the host. They are told apart by the
      // transceiver, not by kind.
      const stream = ev.streams[0] ?? new MediaStream([ev.track])
      if (ev.transceiver === this.cameraRxTx) {
        // The picture-in-picture tile. It must NOT reach onStream: that assigns
        // the main <video>, so the camera would replace the screen — and it
        // carries no audio, which would also clear the "tap to unmute" prompt.
        this.cb.onCameraStream?.(stream)
        ev.track.addEventListener('ended', () => this.cb.onCameraStream?.(null))
        return
      }
      if (ev.transceiver === this.relayRxTx) {
        this.relayTrack = ev.track
        ev.track.addEventListener('ended', () => {
          this.memberScreenStream = null
          this.cb.onMemberScreenStream?.(null)
        })
        // Muted until somebody presents; surfacing it now would black out the
        // host's picture the moment a viewer joins.
        if (this._screenSharer) this.emitMemberScreen()
        return
      }
      // The publisher sends video plus one mixed audio track. Wrap whatever
      // arrives into a single stream so the <video> element has one srcObject.
      this.cb.onStream?.(stream)
      if (ev.track.kind === 'video') this.mainVideoTrack = ev.track
      // A voice meeting never sends video: the audio track IS the connection.
      // Without this the page would sit on "connecting" forever.
      if (ev.track.kind === 'video' || (ev.track.kind === 'audio' && !this._videoEnabled)) {
        this.setState('playing')
      }
    }
    pc.onicecandidate = (ev) => {
      if (ev.candidate) this.send({ type: 'ice', candidate: ev.candidate.toJSON() })
    }
    pc.onconnectionstatechange = () => {
      if (pc.connectionState === 'failed') {
        appLog.w(TAG, 'peer connection failed')
        // Tear down, not just flag it: if the viewer was speaking, its
        // microphone would otherwise stay live (and the OS mic indicator on)
        // after the connection is already dead.
        this.teardown()
        this.setState('failed', { reason: 'ice' })
      }
    }

    // Reserve our own audio m-line up front as sendrecv with no track. Speaking
    // then only needs replaceTrack, which does NOT renegotiate — verified
    // against Electron 44 / Chromium (see the design doc §8.3.4). Adding the
    // transceiver later would require a second offer.
    this.talkTx = pc.addTransceiver('audio', { direction: 'sendrecv' })

    // m-line order is a contract with the publisher (it wires the same lines in
    // the same order): 1 audio, 2 video (host picture down + our screen up),
    // 3 video (a forwarded member screen, down only), 4 video (the host's
    // camera tile, down only).
    this.screenTx = pc.addTransceiver('video', { direction: 'sendrecv' })
    this.relayRxTx = pc.addTransceiver('video', { direction: 'recvonly' })
    // Added here, at join, and never later: a transceiver created mid-call
    // needs a second offer, and the publisher is the answerer. An older
    // publisher simply ignores the extra line.
    this.cameraRxTx = pc.addTransceiver('video', { direction: 'recvonly' })

    void this.sendOffer()
  }

  // ── Relay (SFU) mode ──────────────────────────────────────────────────────

  /**
   * Join the LiveKit room that carries this meeting's media.
   *
   * The socket stays open: it is still the authority on room state (who may
   * speak, who is sharing a screen, the participant list). Only media and the
   * control bus move.
   */
  private async startRelay(relay: RelayInfo): Promise<void> {
    // Assigned inside the try, read by the disconnect handler: a room that was
    // never handed to us must not be able to fail the session.
    let room: Room | null = null
    try {
      room = await openRelay(relay.url, relay.token, {
        onTrack: (track, source, identity) => this.onRelayTrack(track, source, identity),
        onTrackGone: (track) => this.onRelayTrackGone(track),
        onData: (payload) => this.onRelayData(payload),
        onActiveSpeakers: (identities) => this.onRelaySpeakers(identities),
        onDisconnected: (reason) => this.onRelayDisconnected(room, reason),
      })
    } catch (e) {
      appLog.e(TAG, 'relay connect failed', e)
      this.setState('failed', { reason: 'relay' })
      return
    }
    if (!room) return
    if (this._closed) {
      // The page was closed while we were connecting.
      closeRelay(room)
      return
    }
    this.relayRoom = room
    // Ask for the transcript so far. Peer-to-peer does this in the data
    // channel's `open` handler; LiveKit's channel is usable as soon as the room
    // is connected, so this is the equivalent moment — without it a viewer that
    // joins late sees an empty chat panel.
    void sendRelayData(room, { t: 'chat-history-req' })
  }

  /**
   * One track arrived from the relay.
   *
   * The relay delivers every participant's tracks separately, so the routing
   * that the peer-to-peer path does with transceivers is done with `source` and
   * `identity` here. `identity` is 'host' for the publisher and the room's
   * viewer id for everybody else — the same ids the roster uses.
   */
  private onRelayTrack(track: MediaStreamTrack, source: string, identity: string): void {
    // The publisher is 'host'; everybody else is their viewer id. An empty one
    // means the relay could not name the sender — the picture then has no
    // owner, so say so instead of silently routing it as a member's screen.
    if (!identity) appLog.w(TAG, 'relay track without an identity', source, track.kind)

    if (track.kind === 'audio') {
      // EVERY audio track goes into the main stream, not just the host's.
      // A viewer who raised their hand and was approved publishes here, and the
      // rest of the audience has to hear them — dropping non-host audio would
      // make "举手发言" a silent feature. The relay never delivers our own
      // track back, so there is no echo to guard against; the identity check is
      // only a belt-and-braces guard for a relay that misroutes.
      if (identity === `viewer:${this._viewerId}`) return
      this.addRelayMainTrack(track)
      // A voice meeting has no video: the audio track IS the picture arriving.
      if (!this._videoEnabled) this.setState('playing')
      return
    }

    if (source === SOURCE_CAMERA) {
      // The picture-in-picture tile. It must NOT reach onStream: that assigns
      // the main <video>, so the camera would replace the screen.
      this.relayCameraTrack = track
      this.cb.onCameraStream?.(new MediaStream([track]))
      track.addEventListener('ended', () => this.onRelayTrackGone(track))
      return
    }

    if (source !== SOURCE_SCREEN) return

    if (identity === 'host') {
      this.relayMainVideoTrack = track
      this.addRelayMainTrack(track)
      this.setState('playing')
      return
    }

    // Another viewer's screen. Surfaced through the same callback the p2p path
    // uses; a build without member sharing simply ignores it.
    this.relayMemberTrack = track
    this.cb.onMemberScreenStream?.(new MediaStream([track]))
    track.addEventListener('ended', () => this.onRelayTrackGone(track))
  }

  /** Merge one host track into the single stream the main <video> renders. */
  private addRelayMainTrack(track: MediaStreamTrack): void {
    if (this.relayMainTracks.includes(track)) return
    this.relayMainTracks = [...this.relayMainTracks, track]
    if (track.kind === 'video') this.mainVideoTrack = track
    // A fresh MediaStream rather than addTrack: the component re-assigns
    // `srcObject` on every onStream, so a mutated stream would not be noticed.
    this.relayMainStream = new MediaStream(this.relayMainTracks)
    this.cb.onStream?.(this.relayMainStream)
  }

  private onRelayTrackGone(track: MediaStreamTrack): void {
    if (track === this.relayCameraTrack) {
      this.relayCameraTrack = null
      this.cb.onCameraStream?.(null)
      return
    }
    if (track === this.relayMemberTrack) {
      this.relayMemberTrack = null
      this.cb.onMemberScreenStream?.(null)
      return
    }
    if (!this.relayMainTracks.includes(track)) return
    this.relayMainTracks = this.relayMainTracks.filter((t) => t !== track)
    if (this.relayMainVideoTrack === track) this.relayMainVideoTrack = null
    if (this.relayMainTracks.length === 0) {
      // Nothing left to render. The page learns about the missing picture from
      // the server's `picture-state` frame, exactly as on the p2p path, so
      // there is nothing to report here.
      this.relayMainStream = null
      return
    }
    this.relayMainStream = new MediaStream(this.relayMainTracks)
    this.cb.onStream?.(this.relayMainStream)
  }

  /** A control frame arrived on LiveKit's data channel. */
  private onRelayData(payload: Uint8Array): void {
    const decoded = decodeRelayData(payload)
    if (decoded === null) return
    // Re-encode and run the SAME parser the data channel uses: one place
    // decides what a valid control frame is, whatever carried it.
    const msg = parseControlMessage(JSON.stringify(decoded))
    if (msg) this.cb.onControl?.(msg)
  }

  /**
   * Who is speaking, per the relay.
   *
   * The server cannot report this in relay mode (it never sees the media), so
   * the roster's `talking` flags are derived here. The roster ids ARE the
   * relay identities, so the existing callback is enough — no new API and no
   * second list for the panel to keep in step.
   */
  private onRelaySpeakers(identities: string[]): void {
    this._activeSpeakers = identities
    const active = new Set(identities)
    this.applyRoster(this._roster.map((m) => ({ ...m, talking: active.has(m.id) })))
  }

  private onRelayDisconnected(room: Room | null, reason: string): void {
    // Ignore a room we already released (or never adopted). `teardown()` closes
    // the room on purpose and LiveKit reports that close through this very
    // callback — acting on it would turn every clean exit into `failed`.
    if (!room || this.relayRoom !== room) return
    appLog.w(TAG, 'relay disconnected', reason)
    this.relayRoom = null
    closeRelay(room)
    this.setState('failed', { reason: 'relay' })
  }

  private async sendOffer(): Promise<void> {
    if (!this.pc) return
    try {
      const offer = await this.pc.createOffer()
      await this.pc.setLocalDescription(offer)
      this.send({ type: 'offer', sdp: offer.sdp ?? '' })
      this.setState('negotiating')
    } catch (e) {
      appLog.e(TAG, 'createOffer failed', e)
      this.setState('failed', { reason: 'offer' })
    }
  }

  private async onAnswer(msg: ServerMessage): Promise<void> {
    if (!this.pc || !msg.sdp) return
    try {
      await this.pc.setRemoteDescription({ type: 'answer', sdp: msg.sdp })
    } catch (e) {
      appLog.e(TAG, 'setRemoteDescription(answer) failed', e)
      this.setState('failed', { reason: 'answer' })
    }
  }

  private async onRemoteICE(msg: ServerMessage): Promise<void> {
    if (!this.pc || !msg.candidate) return
    try {
      await this.pc.addIceCandidate(msg.candidate)
    } catch (e) {
      // A candidate that arrives before the remote description is normal and
      // recoverable; log at debug level rather than failing the session.
      appLog.d(TAG, 'addIceCandidate ignored', e)
    }
  }

  /**
   * Turn our microphone on.
   *
   * There is no approval step: the room-level switch is the whole authorisation
   * model. The server rejects it (with `talk-disabled`) if that switch is off,
   * which also covers a stale UI whose toggle was enabled by an older `joined`.
   */
  async startTalking(): Promise<boolean> {
    // `talkTx` exists only on the p2p path; a relayed room has a LiveKit room
    // instead, and the microphone is published there.
    if (!this.talkTx && !this.relayRoom) return false
    if (!this.micTrack) {
      try {
        const gum =
          this.opts.getUserMedia ??
          ((c: MediaStreamConstraints) => navigator.mediaDevices.getUserMedia(c))
        const mic = await gum({
          audio: { echoCancellation: true, noiseSuppression: true, autoGainControl: true },
        })
        this.micTrack = mic.getAudioTracks()[0] ?? null
      } catch (e) {
        appLog.e(TAG, 'microphone unavailable', e)
        return false
      }
    }
    if (!this.micTrack) return false
    if (this.relayRoom) {
      try {
        await publishMic(this.relayRoom, this.micTrack)
      } catch (e) {
        appLog.e(TAG, 'publish microphone failed', e)
        return false
      }
    } else {
      await this.talkTx!.sender.replaceTrack(this.micTrack)
    }
    this._talking = true
    this.send({ type: 'talk-start' })
    return true
  }

  /**
   * Turn our microphone off. Always safe to call, including when the publisher
   * already muted us: leaving the track attached would keep the mic live.
   */
  async stopTalking(): Promise<void> {
    if (!this._talking && !this.talkTx?.sender.track) return
    this._talking = false
    await this.detachMic()
    this.send({ type: 'talk-end' })
  }

  /**
   * Start sharing this browser's screen.
   *
   * The browser shows its own picker; we only receive the chosen track. A
   * failure (no permission, insecure origin, unsupported browser) is reported
   * as `false` rather than thrown — the caller renders a hint, not an error
   * page.
   */
  async startScreenShare(): Promise<boolean> {
    // `screenTx` exists only on the p2p path; a relayed room publishes instead.
    if ((!this.screenTx && !this.relayRoom) || this.screenTrack) return false
    const gdm = navigator.mediaDevices?.getDisplayMedia
    if (!gdm) return false
    try {
      const stream = await gdm.call(navigator.mediaDevices, {
        // Capped for the same reason the host caps its own capture: this picture
        // is decoded and re-encoded on the host once per other viewer. 15 fps is
        // the ceiling because a member's screen is a secondary picture here.
        video: { ...screenCaptureConstraints(), frameRate: { max: 15 } },
        // No audio: the audio m-line already carries our microphone, and a
        // second audio track would need another transceiver (another
        // renegotiation) for a marginal gain.
        audio: false,
      })
      const track = stream.getVideoTracks()[0]
      if (!track) {
        // Nothing to send: release the capture the browser just granted.
        for (const t of stream.getTracks()) t.stop()
        return false
      }
      applyContentHint(track, 'text')
      this.screenTrack = track
      if (this.relayRoom) {
        // Relay mode: publish the capture; the SFU owns the encoder and the
        // per-viewer fan-out, so the p2p uplink budget below does not apply.
        try {
          await publishVideo(this.relayRoom, track, SOURCE_SCREEN)
        } catch (e) {
          appLog.w(TAG, 'publish screen failed', e)
          this.releaseScreenTrack()
          return false
        }
      } else {
        await this.screenTx!.sender.replaceTrack(track)
        // The host re-encodes this, so the uplink has to be bounded on the side
        // that owns the encoder. Best effort: a rejected budget still leaves a
        // working share.
        void applyEncodeBudget(this.screenTx!.sender, GUEST_SCREEN_BUDGET)
      }
      // The browser's own "stop sharing" bar ends the track without telling us
      // through a socket, so it has to be handled here.
      track.addEventListener('ended', () => {
        void this.stopScreenShare()
      })
      this.send({ type: 'screen-start' })
      return true
    } catch (e) {
      appLog.w(TAG, 'screen capture unavailable', e)
      // screenTrack may already be set (replaceTrack failed): it must be
      // STOPPED, not just forgotten — a dangling track keeps the browser's
      // "sharing your screen" indicator on and keeps capturing.
      this.releaseScreenTrack()
      return false
    }
  }

  /**
   * Pick the stat entry that belongs to the host's picture.
   *
   * A meeting can carry three video m-lines at once (host picture, a forwarded
   * member screen, the camera tile), and `getStats()` reports one entry per
   * line. The badge must describe the picture the viewer is actually watching:
   * reading the camera tile's numbers would report a stuttering screen as
   * "good" — or the reverse.
   *
   * Matched by `mid` first (the m-line's identity, which we know from the
   * transceiver we reserved for the picture), then by `trackIdentifier` (the
   * track `ontrack` handed us). Both are optional in the spec, so a browser
   * that reports neither falls back to the FIRST entry — arbitrary, but stable,
   * unlike "last one wins".
   */
  private pickMainVideoStat(
    videos: Array<Record<string, unknown>>,
  ): Record<string, unknown> | null {
    if (videos.length === 0) return null
    if (videos.length === 1) return videos[0]
    const mid = this.screenTx?.mid
    if (mid) {
      const byMid = videos.find((v) => v.mid === mid)
      if (byMid) return byMid
    }
    const trackId = this.mainVideoTrack?.id
    if (trackId) {
      const byTrack = videos.find((v) => v.trackIdentifier === trackId)
      if (byTrack) return byTrack
    }
    return videos[0]
  }

  /**
   * One reading of the connection's health.
   *
   * Bitrate needs two samples, so the first call returns null for it and the
   * second one is meaningful — callers poll, they do not call this once.
   */
  async getStats(): Promise<LiveStats> {
    const pc = this.pc
    if (!pc) return { rttMs: null, lossPct: null, bitrateKbps: null, jitterMs: null }
    try {
      const report = await pc.getStats()
      let rttMs: number | null = null
      let lossPct: number | null = null
      let jitterMs: number | null = null
      let bytes = 0
      let lost = 0
      let received = 0

      const videos: Array<Record<string, unknown>> = []
      report.forEach((s: Record<string, unknown>) => {
        if (s.type === 'candidate-pair' && s.state === 'succeeded') {
          const rtt = s.currentRoundTripTime
          if (typeof rtt === 'number') rttMs = Math.round(rtt * 1000)
        }
        if (s.type === 'inbound-rtp' && s.kind === 'video') videos.push(s)
      })

      const main = this.pickMainVideoStat(videos)
      if (main) {
        if (typeof main.bytesReceived === 'number') bytes = main.bytesReceived
        if (typeof main.jitter === 'number') jitterMs = Math.round(main.jitter * 1000)
        if (typeof main.packetsLost === 'number') lost = main.packetsLost
        if (typeof main.packetsReceived === 'number') received = main.packetsReceived
      }

      if (received + lost > 0) lossPct = Math.round((lost / (received + lost)) * 1000) / 10

      const now = Date.now()
      let bitrateKbps: number | null = null
      if (this.lastStats && now > this.lastStats.at && bytes > this.lastStats.bytes) {
        const seconds = (now - this.lastStats.at) / 1000
        bitrateKbps = Math.round(((bytes - this.lastStats.bytes) * 8) / seconds / 1000)
      }
      this.lastStats = { at: now, bytes }
      return { rttMs, lossPct, bitrateKbps, jitterMs }
    } catch (e) {
      appLog.d(TAG, 'getStats failed', e)
      return { rttMs: null, lossPct: null, bitrateKbps: null, jitterMs: null }
    }
  }

  /** Send one control frame (laser, ink, chat, remote-control event). */
  sendControl(msg: ControlMessage): boolean {
    if (this.relayRoom) {
      // Relay mode: the control bus is LiveKit's data channel. The frame shape
      // is unchanged, so every caller stays topology-agnostic.
      void sendRelayData(this.relayRoom, msg)
      return true
    }
    if (this.dc?.readyState !== 'open') return false
    try {
      this.dc.send(JSON.stringify(msg))
      return true
    } catch (e) {
      appLog.d(TAG, 'control send failed', e)
      return false
    }
  }

  /**
   * Tell the SERVER that we asked for remote control.
   *
   * The request itself travels over the control channel (`ctrl-req`) — that is
   * what the publisher's UI answers, and it must not wait for a round trip.
   * This is the server's copy, and it exists for one reason: the server is the
   * only party that can notify a host who is not looking at the meeting page.
   * It changes no room state, so it is safe to send even when the host has the
   * capability switched off (the publisher still sees the request and refuses).
   */
  requestControl(): void {
    this.send({ type: 'control-request' })
  }

  /** Stop sharing and tell the room. */
  async stopScreenShare(): Promise<void> {
    if (!this.screenTrack) return
    this.releaseScreenTrack()
    await this.detachScreen()
    this.send({ type: 'screen-stop' })
  }

  /** Drop the local capture without reporting it (the room already refused). */
  private abortLocalScreenShare(): void {
    this.releaseScreenTrack()
    void this.detachScreen()
  }

  private releaseScreenTrack(): void {
    const track = this.screenTrack
    this.screenTrack = null
    try {
      track?.stop()
    } catch {
      /* already stopped */
    }
  }

  /** Surface the relayed track once the room says somebody is presenting. */
  private emitMemberScreen(): void {
    if (!this.relayTrack) return
    if (
      !this.memberScreenStream ||
      this.memberScreenStream.getVideoTracks()[0] !== this.relayTrack
    ) {
      this.memberScreenStream = new MediaStream([this.relayTrack])
    }
    this.cb.onMemberScreenStream?.(this.memberScreenStream)
  }

  private async detachScreen(): Promise<void> {
    if (this.relayRoom) {
      try {
        await publishVideo(this.relayRoom, null, SOURCE_SCREEN)
      } catch (e) {
        appLog.d(TAG, 'unpublish screen failed', e)
      }
    }
    try {
      await this.screenTx?.sender.replaceTrack(null)
    } catch (e) {
      appLog.d(TAG, 'screen replaceTrack(null) failed', e)
    }
  }

  /** Detach the microphone track without reporting it (used on mute). */
  private async detachMic(): Promise<void> {
    if (this.relayRoom) {
      try {
        await publishMic(this.relayRoom, null)
      } catch (e) {
        appLog.d(TAG, 'unpublish microphone failed', e)
      }
    }
    try {
      await this.talkTx?.sender.replaceTrack(null)
    } catch (e) {
      appLog.d(TAG, 'replaceTrack(null) failed', e)
    }
  }

  private send(payload: Record<string, unknown>): void {
    if (!this.ws || this.ws.readyState !== 1 /* OPEN */) return
    try {
      this.ws.send(JSON.stringify(payload))
    } catch (e) {
      appLog.w(TAG, 'send failed', e)
    }
  }

  private setState(state: LiveViewerState, detail?: { reason?: string }): void {
    if (this._state === state) return
    this._state = state
    this.cb.onStateChange?.(state, detail)
  }

  /** Release every resource. Idempotent. */
  close(): void {
    if (this._closed) return
    this._closed = true
    this.teardown()
    this.setState('idle')
  }

  private teardown(): void {
    void this.stopTalking()
    this.releaseScreenTrack()
    // Clear the handle BEFORE closing: LiveKit reports the close through
    // `onDisconnected`, which must not turn a clean exit into `failed`.
    const relay = this.relayRoom
    this.relayRoom = null
    if (relay) closeRelay(relay)
    this.relayMainTracks = []
    this.relayMainStream = null
    this.relayMainVideoTrack = null
    this.relayCameraTrack = null
    this.relayMemberTrack = null
    this._activeSpeakers = []
    this.dc = null
    this.lastStats = null
    this.mainVideoTrack = null
    this.relayTrack = null
    this.memberScreenStream = null
    if (this.micTrack) {
      this.micTrack.stop()
      this.micTrack = null
    }
    if (this.pc) {
      try {
        this.pc.close()
      } catch {
        /* already closed */
      }
      this.pc = null
    }
    this.talkTx = null
    this.screenTx = null
    this.relayRxTx = null
    this.cameraRxTx = null
    if (this.ws) {
      try {
        this.ws.close()
      } catch {
        /* already closed */
      }
      this.ws = null
    }
  }
}
