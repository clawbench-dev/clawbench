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

  private _state: LiveViewerState = 'idle'
  private _talkEnabled = false
  private _talking = false
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
      case 'talk-revoked':
        // The publisher muted us, or turned the room switch off.
        this._talking = false
        void this.detachMic()
        this.cb.onTalkRevoked?.(msg.reason ?? '')
        break
      case 'talk-disabled':
        // We asked to speak while the room switch was off. Flip the toggle back
        // instead of leaving it on and silently sending nothing.
        this._talking = false
        void this.detachMic()
        this.cb.onTalkRevoked?.(msg.reason ?? 'disabled')
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
    if (msg.title) this.cb.onTitle?.(msg.title)

    const pcFactory =
      this.opts.pcFactory ??
      ((config: RTCConfiguration) => new RTCPeerConnection(config))
    const pc = pcFactory({ iceServers: msg.iceServers ?? [] })
    this.pc = pc

    pc.ontrack = (ev) => {
      // The publisher sends video plus one mixed audio track. Wrap whatever
      // arrives into a single stream so the <video> element has one srcObject.
      const stream = ev.streams[0] ?? new MediaStream([ev.track])
      this.cb.onStream?.(stream)
      this.setState('playing')
    }
    pc.onicecandidate = (ev) => {
      if (ev.candidate) this.send({ type: 'ice', candidate: ev.candidate.toJSON() })
    }
    pc.onconnectionstatechange = () => {
      if (pc.connectionState === 'failed') {
        appLog.w(TAG, 'peer connection failed')
        this.setState('failed', { reason: 'ice' })
      }
    }

    // Reserve our own audio m-line up front as sendrecv with no track. Speaking
    // then only needs replaceTrack, which does NOT renegotiate — verified
    // against Electron 44 / Chromium (see the design doc §8.3.4). Adding the
    // transceiver later would require a second offer.
    this.talkTx = pc.addTransceiver('audio', { direction: 'sendrecv' })

    // Ask for video + the publisher's mixed audio.
    pc.addTransceiver('video', { direction: 'recvonly' })

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
