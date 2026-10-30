/**
 * Viewer-side live share client: signalling socket + one RTCPeerConnection.
 *
 * Kept free of Vue so the state machine can be unit tested directly. The
 * component wraps it and mirrors `state` into reactive refs.
 *
 * Protocol (see internal/liveshare/signal.go for the authoritative shapes):
 *   server → joined | answer | ice | full | publisher-gone | talk-* | error
 *   client → join | offer | ice | talk-request | talk-start | talk-end
 *
 * Media is P2P: the socket carries only SDP/ICE. `iceServers` comes from the
 * `joined` frame and is passed to RTCPeerConnection verbatim, so enabling a
 * TURN relay later needs no change here.
 */
import { appLog } from '@/utils/appLog'

const TAG = 'LiveViewer'

export type LiveViewerState =
  | 'idle'
  | 'connecting' // socket opening, waiting for `joined`
  | 'negotiating' // offer sent, waiting for the answer
  | 'playing'
  | 'ended' // publisher gone or the share was closed
  | 'full' // room at capacity
  | 'failed' // signalling or ICE failure

/** Message shapes the server sends. Fields are optional by message type. */
interface ServerMessage {
  type?: string
  viewerId?: string
  title?: string
  sdp?: string
  candidate?: RTCIceCandidateInit
  iceServers?: RTCIceServer[]
  talkEnabled?: boolean
  /** Whether the publisher is running AI 听记 for this share. */
  notesEnabled?: boolean
  /** Room-level switch for audience screen sharing. */
  screenEnabled?: boolean
  /** Viewer id currently sharing a screen ('' when nobody is). */
  screenSharer?: string
  /** Whether this frame is a screen-state update. */
  sharing?: boolean
  reason?: string
  message?: string
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
  /** The room-level screen-sharing switch changed. */
  onScreenEnabled?: (enabled: boolean) => void
  /** Who is sharing a screen ('' = nobody). */
  onScreenSharer?: (viewerId: string) => void
  /** The screen a member is sharing, forwarded by the publisher. */
  onMemberScreenStream?: (stream: MediaStream | null) => void
  /** The room refused our screen share (switch off, or someone else is sharing). */
  onScreenDenied?: (reason: string) => void
  /** The publisher muted us, or the room switch was turned off. */
  onTalkRevoked?: (reason: string) => void
}

export interface LiveViewerOptions {
  token: string
  callbacks?: LiveViewerCallbacks
  /** Injected for tests; defaults to the global WebSocket. */
  wsFactory?: (url: string) => WebSocket
  /** Injected for tests; defaults to the global RTCPeerConnection. */
  pcFactory?: (config: RTCConfiguration) => RTCPeerConnection
  /** Injected for tests; defaults to getUserMedia. */
  getUserMedia?: (constraints: MediaStreamConstraints) => Promise<MediaStream>
}

/** ws(s):// URL for the signalling socket of a share token. */
export function liveSignalUrl(token: string, origin?: { protocol: string; host: string }): string {
  const proto =
    (origin?.protocol ?? window.location.protocol) === 'https:' ? 'wss' : 'ws'
  const host = origin?.host ?? window.location.host
  return `${proto}://${host}/api/share/${encodeURIComponent(token)}/signal`
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

  private _state: LiveViewerState = 'idle'
  private _talkEnabled = false
  private _talking = false
  private _notesEnabled = false
  private _screenEnabled = false
  private _screenSharer = ''
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
  private _closed = false

  constructor(opts: LiveViewerOptions) {
    this.opts = opts
    this.cb = opts.callbacks ?? {}
  }

  get state(): LiveViewerState {
    return this._state
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
      ws = factory(liveSignalUrl(this.opts.token))
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
      case 'publisher-gone':
        this.setState('ended', { reason: msg.reason })
        this.teardown()
        break
      case 'talk-enabled':
        // The room-level switch changed after we joined. `joined` carries the
        // state at attach time, so this is the only way an already-connected
        // viewer learns the publisher enabled (or re-enabled) speaking.
        this._talkEnabled = msg.talkEnabled !== false
        this.cb.onTalkEnabled?.(this._talkEnabled)
        break
      case 'talk-revoked':
        // Two different meanings share this frame: the publisher muted THIS
        // viewer ('revoked'/'muted'), or the room switch went off ('disabled').
        // Only the latter means the microphone toggle itself should disappear —
        // clearing it on a personal mute would take away the button the viewer
        // is still entitled to use.
        this._talking = false
        void this.detachMic()
        if (msg.reason === 'disabled') {
          this._talkEnabled = false
          this.cb.onTalkEnabled?.(false)
        }
        this.cb.onTalkRevoked?.(msg.reason ?? '')
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

  private onJoined(msg: ServerMessage): void {
    this._talkEnabled = Boolean(msg.talkEnabled)
    this.cb.onTalkEnabled?.(this._talkEnabled)
    this._notesEnabled = Boolean(msg.notesEnabled)
    this.cb.onNotesEnabled?.(this._notesEnabled)
    this._screenEnabled = Boolean(msg.screenEnabled)
    this._screenSharer = msg.screenSharer ?? ''
    this.cb.onScreenEnabled?.(this._screenEnabled)
    this.cb.onScreenSharer?.(this._screenSharer)
    if (msg.title) this.cb.onTitle?.(msg.title)

    const pcFactory =
      this.opts.pcFactory ??
      ((config: RTCConfiguration) => new RTCPeerConnection(config))
    const pc = pcFactory({ iceServers: msg.iceServers ?? [] })
    this.pc = pc

    pc.ontrack = (ev) => {
      // Two video m-lines arrive here: the host's picture (with the mixed
      // audio, one stream) and — only while somebody is presenting — another
      // viewer's screen forwarded by the host. They are told apart by the
      // transceiver, not by kind.
      const stream = ev.streams[0] ?? new MediaStream([ev.track])
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
      if (ev.track.kind === 'video') this.setState('playing')
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

    // m-line order is a contract with the publisher (it adds the same three in
    // the same order): 1 audio, 2 video (host picture down + our screen up),
    // 3 video (a forwarded member screen, down only).
    this.screenTx = pc.addTransceiver('video', { direction: 'sendrecv' })
    this.relayRxTx = pc.addTransceiver('video', { direction: 'recvonly' })

    void this.sendOffer()
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
    if (!this.talkTx) return false
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
    await this.talkTx.sender.replaceTrack(this.micTrack)
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
    if (!this.screenTx || this.screenTrack) return false
    const gdm = navigator.mediaDevices?.getDisplayMedia
    if (!gdm) return false
    try {
      const stream = await gdm.call(navigator.mediaDevices, {
        video: { frameRate: 15 },
        // No audio: the audio m-line already carries our microphone, and a
        // second audio track would need another transceiver (another
        // renegotiation) for a marginal gain.
        audio: false,
      })
      const track = stream.getVideoTracks()[0]
      if (!track) return false
      this.screenTrack = track
      await this.screenTx.sender.replaceTrack(track)
      // The browser's own "stop sharing" bar ends the track without telling us
      // through a socket, so it has to be handled here.
      track.addEventListener('ended', () => {
        void this.stopScreenShare()
      })
      this.send({ type: 'screen-start' })
      return true
    } catch (e) {
      appLog.w(TAG, 'screen capture unavailable', e)
      this.screenTrack = null
      return false
    }
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
    try {
      await this.screenTx?.sender.replaceTrack(null)
    } catch (e) {
      appLog.d(TAG, 'screen replaceTrack(null) failed', e)
    }
  }

  /** Detach the microphone track without reporting it (used on mute). */
  private async detachMic(): Promise<void> {
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
