/**
 * Publisher side of a live screen/audio share.
 *
 * Owns one RTCPeerConnection per viewer (P2P: the server only relays SDP/ICE),
 * the capture streams, and the per-viewer audio mix. State is module-level so
 * the dialog and the header button observe the same session, mirroring
 * useAutoSpeech / useVoiceInput.
 *
 * Protocol: see internal/liveshare/signal.go. The publisher answers one offer
 * per viewer and forwards ICE both ways; it never parses SDP.
 */
import { ref, shallowRef } from 'vue'
import i18n from '@/i18n'
import { appLog } from '@/utils/appLog'
import { getNative } from '@/utils/clawbenchNative'
import { buildShareLinks, type ShareLink } from '@/composables/useShareLinks'
import { LiveMixer } from './useLiveMixer'
import {
  VIEWER_MESSAGE_TYPES,
  parseControlMessage,
  type ControlMessage,
} from '@/share/liveControl'
import { useLiveNotes } from './useLiveNotes'
import { acquireMic, describeMicError, logMicEnv } from '@/utils/micCapture'
import { browserScreenCaptureAvailable } from '@/utils/screenCaptureSupport'
import { startAndroidScreenCapture, type AndroidCaptureHandle } from './androidScreenCapture'

const TAG = 'LiveShare'

/**
 * Capture shape for the Android path.
 *
 * 10 fps at 1280 px keeps the bridge traffic (JPEG over the WebView JS bridge,
 * then re-encoded by WebRTC) inside what a mid-range phone can sustain; a phone
 * screen is tall and mostly static, so the frame rate matters less than the
 * resolution. See androidScreenCapture.ts for why the transport looks like this.
 */
const ANDROID_CAPTURE_FPS = 10
const ANDROID_CAPTURE_MAX_WIDTH = 1280
const ANDROID_CAPTURE_QUALITY = 55

export type LiveShareState = 'idle' | 'capturing' | 'publishing'

/** One audience member, as far as the publisher's UI is concerned. */
export interface LiveViewerInfo {
  id: string
  name: string
  /** Speech state reported by the server. */
  /** 'talking' = that viewer's microphone is on; 'muted' = it is off. */
  talk: 'muted' | 'talking'
}

export interface CaptureSource {
  id: string
  name: string
  kind: 'screen' | 'window'
  thumbnail: string
}

// ── Module-level session state (one share per app instance) ──────────────────
const state = ref<LiveShareState>('idle')
const token = ref('')
const shareUrl = ref('')
/** Every address this share can be opened on; shareUrl is the primary. */
const shareLinks = ref<ShareLink[]>([])
const title = ref('')
const viewers = ref<LiveViewerInfo[]>([])
const talkEnabled = ref(false)
/** Room-level switch: may viewers share their screen? */
const screenEnabled = ref(false)
/** Viewer id currently sharing a screen ('' = nobody). */
const screenSharer = ref('')
/** The screen a viewer is sharing, as received from that viewer's PC. */
const remoteScreenStream = shallowRef<MediaStream | null>(null)
const error = ref('')

interface PublisherPeer {
  pc: RTCPeerConnection
  viewerId: string
  /** m-line that carries another viewer's screen to this peer (null before setup). */
  relayTx: RTCRtpTransceiver | null
  /**
   * The video track this viewer's m-line carries.
   *
   * It exists from the moment the m-line is negotiated — muted, because nobody
   * is sending yet. `ontrack` alone therefore does NOT mean "this viewer is
   * sharing"; only the server's screen-state does. The track is kept here so
   * the state change has something to relay.
   */
  screenTrack: MediaStreamTrack | null
  /** Control bus: laser/ink/chat/presence/remote-control frames. */
  channel: RTCDataChannel | null
  /** The m-line carrying our picture; kept so a source switch can swap it. */
  videoTx: RTCRtpTransceiver | null
  /** This viewer's audio mix, wired into the m-lines when its offer arrives. */
  mixedTrack: MediaStreamTrack | null
  /**
   * Whether the offered m-lines have been wired up already.
   *
   * The publisher ANSWERS, so it cannot reserve m-lines ahead of the offer: a
   * transceiver created by `addTransceiver()` before `setRemoteDescription()`
   * keeps `mid === null` and the browser answers the offered m-lines with
   * brand-new recvonly ones instead — the answer then says "I send nothing" and
   * the viewer never leaves "negotiating". Wiring therefore happens once, right
   * after the first offer is applied.
   */
  attached: boolean
  /** Viewer candidates that arrived before its offer was applied. */
  pendingIce: RTCIceCandidateInit[]
}

let ws: WebSocket | null = null
/**
 * Whether this client ever attached the publisher socket for `token`.
 *
 * Only then may it DELETE the room. POST is idempotent per project, so two
 * devices in the same project share one token; a client that failed during
 * capture must not delete the room another device is actively publishing to.
 */
let ownsRoom = false
let audioCtx: AudioContext | null = null
let mixer: LiveMixer | null = null
let displayStream: MediaStream | null = null
let micStream: MediaStream | null = null
/** The Android native capture, when the share runs through MediaProjection. */
let androidCapture: AndroidCaptureHandle | null = null
/** The mixed track sent to viewers (video + mixed audio). */
let outVideoTrack: MediaStreamTrack | null = null
/** The track a viewer is sharing, relayed to every other viewer. */
let remoteScreenTrack: MediaStreamTrack | null = null
/** UI subscribers for control frames coming from viewers. */
const controlHandlers = new Set<(msg: ControlMessage) => void>()
/**
 * Viewer id the panel last granted remote control to ('' when nobody).
 *
 * The authoritative `controlledBy` lives in the share panel; this mirror is
 * only what the publisher needs in order to notice that grantee disappearing.
 */
let controlGrantee = ''
/** The captured screen, exposed so the publisher can annotate on it. */
const previewStream = shallowRef<MediaStream | null>(null)
/**
 * Whether the current capture is a whole screen or a single window.
 *
 * Remote control needs this: the pointer is mapped over the desktop, which is
 * only correct when the picture IS the desktop. A window capture would put
 * every click at the same fraction of the screen instead of inside the window.
 */
const captureKind = ref<'screen' | 'window'>('screen')
/** Whether system audio was requested, remembered for a mid-share source swap. */
let systemAudioWanted = false
const peers = new Map<string, PublisherPeer>()

/**
 * Live microphone levels, 0..1, keyed by viewer id with `host` for ourselves.
 *
 * Measured on the publisher because that is where every audio track already
 * lands: viewers only receive the mix, so they cannot tell who is speaking.
 * The publisher broadcasts a `talk` frame when a level changes materially —
 * the control channel is ordered and cheap, but it is not a place for a
 * 60 Hz firehose.
 */
const levels = ref<Record<string, number>>({})
let talkTimer: ReturnType<typeof setInterval> | null = null
let lastSentLevel = new Map<string, number>()

/** Level at which a channel counts as "speaking" (≈ -30 dBFS). */
const SPEAKING_LEVEL = 0.05

function stopTalkMetering(): void {
  if (talkTimer !== null) {
    clearInterval(talkTimer)
    talkTimer = null
  }
  lastSentLevel = new Map()
  levels.value = {}
}

function resetSession(): void {
  stopTalkMetering()
  for (const p of peers.values()) {
    try {
      p.pc.close()
    } catch {
      /* already closed */
    }
  }
  peers.clear()
  mixer?.close()
  mixer = null
  if (audioCtx) {
    void audioCtx.close().catch(() => undefined)
    audioCtx = null
  }
  for (const s of [displayStream, micStream]) {
    s?.getTracks().forEach((t) => t.stop())
  }
  // Ends the native projection as well; stopping the tracks alone would leave
  // the foreground service capturing (and the system "screen is shared" badge
  // showing) with nobody reading the frames.
  androidCapture?.stop()
  androidCapture = null
  displayStream = null
  previewStream.value = null
  micStream = null
  outVideoTrack = null
  remoteScreenTrack = null
  remoteScreenStream.value = null
  screenSharer.value = ''
  controlGrantee = ''
  if (ws) {
    try {
      ws.close()
    } catch {
      /* already closed */
    }
    ws = null
  }
  ownsRoom = false
  void getNative()?.setPublishingActive?.(false)
  // Clear a declared-but-never-consumed source. The declaration is one-shot by
  // design; leaving it set would let a later getDisplayMedia capture silently.
  void getNative()?.setPendingCaptureSource?.('', false)
}

function send(payload: Record<string, unknown>): void {
  if (!ws || ws.readyState !== 1) return
  try {
    ws.send(JSON.stringify(payload))
  } catch (e) {
    appLog.w(TAG, 'send failed', e)
  }
}

function fail(msg: string): void {
  error.value = msg
  appLog.e(TAG, msg)
}

/** Ask the desktop host which screens/windows can be shared. */
export async function listCaptureSources(): Promise<CaptureSource[]> {
  const bridge = getNative()
  if (!bridge?.listCaptureSources) return []
  try {
    const raw = await bridge.listCaptureSources()
    const parsed = JSON.parse(raw) as unknown
    if (!Array.isArray(parsed)) return []
    // A Wayland host cannot name what it lists: the compositor exposes one
    // nameless placeholder and the actual pick happens in the system dialog.
    // Without a label the picker would render a blank tile.
    return (parsed as CaptureSource[]).map((s) => ({
      ...s,
      name: s.name || i18n.global.t('liveShare.entireScreen'),
    }))
  } catch (e) {
    appLog.e(TAG, 'listCaptureSources failed', e)
    return []
  }
}

/** Whether this host can capture system (loopback) audio. */
export async function getAudioCaptureSupport(): Promise<boolean> {
  const bridge = getNative()
  // A browser has no capability probe: what comes with the picture is decided
  // by the user in the picker (Chrome offers audio for a tab or the entire
  // screen, never for a single window). Asking anyway is harmless — an
  // unavailable track simply never arrives — so the toggle stays usable
  // instead of being permanently disabled on every web install.
  if (!bridge?.getAudioCaptureSupport) return browserScreenCaptureAvailable()
  try {
    const raw = await bridge.getAudioCaptureSupport()
    const parsed = JSON.parse(raw) as { systemAudio?: boolean }
    return Boolean(parsed?.systemAudio)
  } catch {
    return false
  }
}

/**
 * What the captured picture actually is, as far as remote control cares.
 *
 * A browser does not name its sources; it reports the surface it ended up
 * capturing. Only `monitor` means "this is the desktop" — a window or a tab
 * puts the pointer somewhere other than where the fraction of the screen says.
 */
function captureKindOf(stream: MediaStream): 'screen' | 'window' {
  const track = stream.getVideoTracks()[0]
  const settings = track?.getSettings?.() as { displaySurface?: string } | undefined
  return settings?.displaySurface === 'monitor' ? 'screen' : 'window'
}

export function useLiveShare() {
  /**
   * Create (or reuse) the room, capture the screen and start publishing.
   *
   * `sourceId` comes from listCaptureSources(); the host consumes it on the next
   * getDisplayMedia call and clears it immediately.
   */
  async function start(opts: {
    sourceId: string
    systemAudio: boolean
    microphone: boolean
    shareTitle?: string
    /** Enable AI 听记 (transcription + minutes) for this share. */
    notes?: boolean
    /** Localised speaker label for a channel ("system" / "mic" / "viewer:<id>"). */
    notesLabel?: (channel: string) => string
  }): Promise<boolean> {
    if (state.value !== 'idle') return false
    error.value = ''
    state.value = 'capturing'

    try {
      // 1. Room first: without a token there is nothing to publish to, and a
      //    failed capture would otherwise leave an orphaned room behind.
      const resp = await fetch('/api/live-share', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ title: opts.shareTitle ?? '', notes: Boolean(opts.notes) }),
      })
      if (!resp.ok) {
        fail(`create room failed: ${resp.status}`)
        state.value = 'idle'
        return false
      }
      const data = (await resp.json()) as { token?: string; path?: string }
      if (!data.token) {
        fail('create room returned no token')
        state.value = 'idle'
        return false
      }
      token.value = data.token
      // Build from the server's address list, not window.location.origin: the
      // browser only knows the one address it used, which may not be reachable
      // from a colleague's subnet.
      shareLinks.value = await buildShareLinks(data.path ?? `/share/${data.token}`)
      shareUrl.value = shareLinks.value[0]?.url ?? window.location.origin + `/share/${data.token}`
      title.value = opts.shareTitle ?? ''

      // 2. Capture. Three hosts, two mechanisms:
      //    - Electron declares a source and answers getDisplayMedia with it.
      //    - Android has no getDisplayMedia at all, so it runs a native
      //      MediaProjection session and hands back a stream built from its
      //      frames (see androidScreenCapture.ts).
      //    - A plain browser has no declaration step: its own picker IS the
      //      source choice, so it calls getDisplayMedia directly.
      //    Everything downstream — mixing, peers, notes, the room — is
      //    identical for all three.
      const bridge = getNative()
      systemAudioWanted = opts.systemAudio
      if (bridge?.startScreenCapture) {
        androidCapture = await startAndroidScreenCapture({
          fps: ANDROID_CAPTURE_FPS,
          maxWidth: ANDROID_CAPTURE_MAX_WIDTH,
          quality: ANDROID_CAPTURE_QUALITY,
          systemAudio: opts.systemAudio,
          // The user can revoke the projection from the system UI; when that
          // happens the picture is gone and the room must not keep advertising
          // a live share.
          onEnded: () => {
            void stop()
          },
        })
        displayStream = androidCapture.stream
        captureKind.value = 'screen'
      } else {
        // Both remaining hosts end in getDisplayMedia. Missing here means an
        // insecure origin (no `mediaDevices` at all) or a mobile browser that
        // never implemented it — neither can be worked around in the page.
        const capture = navigator.mediaDevices?.getDisplayMedia
        if (!capture) {
          fail('screen capture is unavailable on this host')
          // The room already exists at this point. Leaving it behind would burn a
          // slot in maxLiveRooms forever (nothing else deletes a room that never
          // got a publisher), so end it explicitly.
          await stop()
          return false
        }
        const declare = bridge?.setPendingCaptureSource
        if (declare && bridge) {
          await declare.call(bridge, opts.sourceId, opts.systemAudio)
          captureKind.value = opts.sourceId.startsWith('screen:') ? 'screen' : 'window'
        }
        displayStream = await capture.call(navigator.mediaDevices, {
          video: true,
          audio: opts.systemAudio,
        })
        if (!declare) captureKind.value = captureKindOf(displayStream)
      }
      previewStream.value = displayStream
      outVideoTrack = displayStream.getVideoTracks()[0] ?? null
      if (!outVideoTrack) {
        fail('no video track captured')
        await stop()
        return false
      }
      // The user can also end the share from the browser's own capture banner.
      outVideoTrack.addEventListener('ended', () => {
        void stop()
      })

      if (opts.microphone) {
        try {
          micStream = await acquireMic({
            audio: { echoCancellation: true, noiseSuppression: true, autoGainControl: true },
          })
        } catch (e) {
          // A denied microphone must not abort the share: the audience still
          // gets the screen and (possibly) system audio. The notes channel is
          // lost, so leave a trail that can be diagnosed later.
          appLog.w(TAG, `microphone unavailable, continuing without it: ${describeMicError(e)}`)
          void logMicEnv('LiveShare')
          micStream = null
        }
      }

      // 4. Build the mixing graph. One destination per viewer is created as
      //    viewers arrive (see onViewerJoin).
      audioCtx = new AudioContext({ sampleRate: 48000 })
      await audioCtx.resume()
      mixer = new LiveMixer(audioCtx, displayStream, micStream)
      startTalkMetering()

      // AI 听记 records the same sources, but one channel at a time: the mix
      // sent to viewers is per-viewer and excludes that viewer, so it cannot be
      // used to transcribe the meeting. Started before the socket so the first
      // viewer's audio is never missed.
      if (opts.notes) {
        useLiveNotes().start({
          token: token.value,
          ctx: audioCtx,
          micStream,
          title: title.value,
          label: opts.notesLabel,
        })
      }

      // 5. Open the publisher socket.
      await openSocket()

      // 6. Keep the renderer unthrottled: a minimized window otherwise throttles
      //    the encoder and the stream stalls for every viewer. A browser has no
      //    such switch — it also has no window to minimize into the tray.
      void bridge?.setPublishingActive?.(true)

      state.value = 'publishing'
      return true
    } catch (e) {
      appLog.e(TAG, 'start failed', e)
      fail(e instanceof Error ? e.message : String(e))
      // stop() rather than resetSession(): the room may already exist and must
      // be deleted server-side, not just forgotten locally.
      await stop()
      return false
    }
  }

  async function openSocket(): Promise<void> {
    const proto = window.location.protocol === 'https:' ? 'wss' : 'ws'
    const url = `${proto}://${window.location.host}/api/live-share/publish/ws?token=${encodeURIComponent(token.value)}`
    const socket = new WebSocket(url)
    ws = socket

    await new Promise<void>((resolve, reject) => {
      socket.onopen = () => {
        socket.send(JSON.stringify({ type: 'publish', title: title.value }))
        // The server has claimed the room for us; only now may we delete it.
        ownsRoom = true
        resolve()
      }
      socket.onerror = () => reject(new Error('publisher socket failed'))
    })

    socket.onmessage = (ev) => void onMessage(ev.data)
    socket.onclose = () => {
      if (state.value === 'publishing') {
        // The server went away: the share is over.
        appLog.w(TAG, 'publisher socket closed')
        void stop()
      }
    }
  }

  async function onMessage(raw: unknown): Promise<void> {
    let msg: Record<string, unknown>
    try {
      msg = JSON.parse(String(raw)) as Record<string, unknown>
    } catch {
      return
    }
    const type = msg.type as string
    const viewerId = (msg.viewerId as string) ?? ''

    switch (type) {
      case 'viewer-join':
        await onViewerJoin(viewerId)
        break
      case 'viewer-leave':
        onViewerLeave(viewerId)
        break
      case 'offer':
        await onOffer(viewerId, (msg.sdp as string) ?? '')
        break
      case 'ice':
        await onRemoteICE(viewerId, msg.candidate as RTCIceCandidateInit)
        break
      case 'talk-state':
        // The only per-viewer state is "microphone on / off"; there is no
        // approval queue to track.
        updateViewer(viewerId, { talk: msg.talking ? 'talking' : 'muted' })
        break
      case 'screen-state':
        screenEnabled.value = Boolean(msg.screenEnabled)
        screenSharer.value = viewerId
        if (!msg.sharing) {
          clearRemoteScreen()
          break
        }
        // The track usually exists already (negotiated at join); if not, the
        // ontrack handler picks it up once it arrives.
        {
          const peer = peers.get(viewerId)
          if (peer?.screenTrack) onRemoteScreen(viewerId, peer.screenTrack)
        }
        break
      default:
        appLog.d(TAG, 'unhandled publisher frame', type)
    }
  }

  /** A viewer arrived: build its PeerConnection and wait for its offer. */
  async function onViewerJoin(viewerId: string): Promise<void> {
    if (!viewerId || peers.has(viewerId)) return

    const pc = new RTCPeerConnection({ iceServers: [] })
    // One destination per viewer, carrying system audio + mic + the OTHER
    // viewers. Excluding this viewer is what keeps it from hearing itself.
    const mixedTrack = mixer?.addViewer(viewerId) ?? null
    peers.set(viewerId, {
      pc,
      viewerId,
      relayTx: null,
      screenTrack: null,
      channel: null,
      videoTx: null,
      mixedTrack,
      attached: false,
      pendingIce: [],
    })
    viewers.value = [...viewers.value, { id: viewerId, name: '', talk: 'muted' }]

    // Control bus. The viewer's offer carries it (an answer cannot introduce an
    // m-line, so the answerer must never try to create one), and it arrives
    // here as soon as we apply that offer. Hooked up at join time so a channel
    // that fires during setRemoteDescription is not missed.
    pc.ondatachannel = (ev) => {
      const p = peers.get(viewerId)
      if (!p) return
      p.channel = ev.channel
      ev.channel.onmessage = (e) => onViewerControl(viewerId, e.data)
    }

    pc.ontrack = (ev) => {
      if (ev.track.kind === 'video') {
        // The only video this side receives is this viewer's shared screen.
        // It arrives (muted) at negotiation time, so remember it but wait for
        // the server to say that this viewer is actually sharing.
        const p = peers.get(viewerId)
        if (p) p.screenTrack = ev.track
        ev.track.addEventListener('ended', () => {
          if (screenSharer.value === viewerId) clearRemoteScreen()
        })
        if (screenSharer.value === viewerId) onRemoteScreen(viewerId, ev.track)
        return
      }
      const stream = ev.streams[0] ?? new MediaStream([ev.track])
      // Route it to everyone EXCEPT this viewer.
      mixer?.attachRemote(viewerId, stream)
      // …and transcribe it on its own channel, so the line is attributed to
      // this viewer rather than merged into the system audio.
      useLiveNotes().addViewer(viewerId, stream)
    }
    pc.onicecandidate = (ev) => {
      if (ev.candidate) {
        send({ type: 'ice', viewerId, candidate: ev.candidate.toJSON() })
      }
    }
    pc.onconnectionstatechange = () => {
      if (pc.connectionState === 'failed' || pc.connectionState === 'closed') {
        onViewerLeave(viewerId)
      }
    }
  }

  /**
   * Wire our media into the m-lines the viewer's offer just created.
   *
   * THREE m-lines, in this exact order (a contract with liveViewer.ts):
   *   1 audio  — our mix down, the viewer's microphone up
   *   2 video  — our picture down, the viewer's shared screen up
   *   3 video  — another viewer's screen, relayed down
   *
   * They are reused rather than reserved: `setRemoteDescription(offer)` is what
   * associates a transceiver with an m-line, and only associated transceivers
   * end up in the answer with a `send` direction. Wiring them here (with
   * replaceTrack, so no renegotiation is ever needed later) is what keeps the
   * answer from being all-recvonly.
   *
   * Returns false when the layout is not the one we expect: answering anyway
   * would produce a connection that silently carries nothing.
   */
  async function attachPeerMedia(peer: PublisherPeer): Promise<boolean> {
    const txs = peer.pc.getTransceivers()
    const kinds = txs.map((t) => t.receiver.track?.kind ?? '?')
    if (txs.length !== 3 || kinds[0] !== 'audio' || kinds[1] !== 'video' || kinds[2] !== 'video') {
      appLog.e(TAG, 'viewer offered an unexpected m-line layout', peer.viewerId, kinds)
      return false
    }
    const [audioTx, videoTx, relayTx] = txs

    // Both tracks MUST share one MediaStream: the viewer assigns `srcObject`
    // per received stream, so two streams means the second assignment replaces
    // the first and the viewer gets either video or audio, never both.
    const outgoing = new MediaStream()
    if (outVideoTrack) outgoing.addTrack(outVideoTrack)
    if (peer.mixedTrack) outgoing.addTrack(peer.mixedTrack)

    if (peer.mixedTrack) {
      await audioTx.sender.replaceTrack(peer.mixedTrack)
      audioTx.sender.setStreams?.(outgoing)
    }
    audioTx.direction = 'sendrecv'
    if (outVideoTrack) {
      await videoTx.sender.replaceTrack(outVideoTrack)
      videoTx.sender.setStreams?.(outgoing)
    }
    videoTx.direction = 'sendrecv'
    relayTx.direction = 'sendonly'
    if (remoteScreenTrack) await relayTx.sender.replaceTrack(remoteScreenTrack)

    peer.videoTx = videoTx
    peer.relayTx = relayTx
    return true
  }

  /** Apply the candidates that arrived while the offer was still being set. */
  async function flushPendingIce(peer: PublisherPeer): Promise<void> {
    const queued = peer.pendingIce
    peer.pendingIce = []
    for (const candidate of queued) {
      try {
        await peer.pc.addIceCandidate(candidate)
      } catch (e) {
        appLog.d(TAG, 'addIceCandidate ignored', e)
      }
    }
  }

  async function onOffer(viewerId: string, sdp: string): Promise<void> {
    const peer = peers.get(viewerId)
    if (!peer || !sdp) return
    try {
      await peer.pc.setRemoteDescription({ type: 'offer', sdp })
      if (!peer.attached) {
        if (!(await attachPeerMedia(peer))) return
        peer.attached = true
      }
      await flushPendingIce(peer)
      const answer = await peer.pc.createAnswer()
      await peer.pc.setLocalDescription(answer)
      send({ type: 'answer', viewerId, sdp: answer.sdp ?? '' })
    } catch (e) {
      appLog.e(TAG, 'negotiation failed for viewer', viewerId, e)
    }
  }

  async function onRemoteICE(viewerId: string, candidate: RTCIceCandidateInit | undefined): Promise<void> {
    const peer = peers.get(viewerId)
    if (!peer || !candidate) return
    // A viewer trickles right after its offer, so candidates routinely arrive
    // before that offer has been applied — addIceCandidate throws until there
    // is a remote description, and dropping them leaves ICE with nowhere to
    // send. Hold them until the offer lands.
    if (!peer.pc.remoteDescription) {
      peer.pendingIce.push(candidate)
      return
    }
    try {
      await peer.pc.addIceCandidate(candidate)
    } catch (e) {
      appLog.d(TAG, 'addIceCandidate ignored', e)
    }
  }

  function onViewerLeave(viewerId: string): void {
    const peer = peers.get(viewerId)
    if (peer) {
      try {
        peer.pc.close()
      } catch {
        /* already closed */
      }
      peers.delete(viewerId)
    }
    mixer?.removeViewer(viewerId)
    useLiveNotes().removeViewer(viewerId)
    if (screenSharer.value === viewerId) clearRemoteScreen()
    // A viewer that closes its tab or crashes never sends `ctrl-release`, so the
    // publisher has to revoke on its behalf: otherwise the panel keeps showing
    // "stop controlling" and the main process stays armed for a viewer that is
    // gone. `controlledBy` lives in the panel, so its cleanup is reached by
    // replaying the frame it already handles — a `ctrl-release` from that viewer
    // runs exactly its `revokeControl`. Revoking here as well is deliberate and
    // idempotent: it disarms the main process even when no panel is listening
    // (the share window can be closed while publishing continues).
    if (controlGrantee === viewerId) {
      controlGrantee = ''
      void getNative()?.remoteControlRevoke?.()
      controlHandlers.forEach((cb) => cb({ t: 'ctrl-release', from: viewerId }))
    }
    viewers.value = viewers.value.filter((v) => v.id !== viewerId)
  }

  function updateViewer(viewerId: string, patch: Partial<LiveViewerInfo>): void {
    viewers.value = viewers.value.map((v) => (v.id === viewerId ? { ...v, ...patch } : v))
  }

  /**
   * Swap the shared window/screen without ending the session.
   *
   * Only the video track changes: the audio graph, the viewers' connections and
   * the room all stay up. `replaceTrack` is what makes this invisible — adding
   * a new track would force a renegotiation on every peer.
   */
  async function switchSource(sourceId: string): Promise<boolean> {
    if (state.value !== 'publishing') return false
    const bridge = getNative()
    const declare = bridge?.setPendingCaptureSource
    // A browser re-opens its own picker on every call, which is exactly the
    // interaction "switch source" means there — `sourceId` is unused.
    const capture = navigator.mediaDevices?.getDisplayMedia
    if (!capture) {
      fail('screen capture is unavailable on this host')
      return false
    }
    try {
      if (declare && bridge) await declare.call(bridge, sourceId, systemAudioWanted)
      const next = await capture.call(navigator.mediaDevices, {
        video: true,
        // Audio stays with the original capture: switching the picture must not
        // silently change what the meeting hears.
        audio: false,
      })
      const track = next.getVideoTracks()[0]
      if (!track) {
        fail('no video track captured')
        return false
      }
      captureKind.value = declare
        ? sourceId.startsWith('screen:') ? 'screen' : 'window'
        : captureKindOf(next)
      const previousStream = displayStream
      const previous = outVideoTrack
      outVideoTrack = track
      // The stream must be the one resetSession() stops later; keeping the
      // original here would leave the new capture running after "end share"
      // (the OS capture indicator stays on).
      displayStream = next
      previewStream.value = next
      for (const peer of peers.values()) {
        await peer.videoTx?.sender.replaceTrack(track)
      }
      previous?.stop()
      // Stop any leftover audio track from the old capture too.
      for (const t of previousStream?.getTracks() ?? []) {
        if (t !== previous) t.stop()
      }
      track.addEventListener('ended', () => {
        void stop()
      })
      return true
    } catch (e) {
      appLog.w(TAG, 'switch source failed', e)
      // The declaration is one-shot; leaving it armed would let the NEXT
      // getDisplayMedia (e.g. a viewer starting a share) capture this source
      // without a picker.
      void bridge?.setPendingCaptureSource?.('', false)
      fail('switch source failed')
      return false
    }
  }

  // ── Control bus ──────────────────────────────────────────────────────────

  /**
   * Broadcast one control frame to every viewer.
   *
   * The publisher is also a participant, so the caller applies it locally; this
   * only fans it out. Frames are JSON and small (a laser update is ~40 bytes),
   * so they go straight onto the channel without batching.
   */
  function sendControl(msg: ControlMessage): void {
    const data = JSON.stringify(msg)
    for (const peer of peers.values()) {
      if (peer.channel?.readyState === 'open') {
        try {
          peer.channel.send(data)
        } catch (e) {
          appLog.d(TAG, 'control send failed', peer.viewerId, e)
        }
      }
    }
  }

  /**
   * Sample every microphone 5×/s and broadcast the ones that changed.
   *
   * Only meaningful levels are sent: a channel that is silent in both the
   * previous and the current window is skipped, so an idle room produces no
   * traffic at all.
   */
  function startTalkMetering(): void {
    if (talkTimer !== null) return
    talkTimer = setInterval(() => {
      if (!mixer) return
      const next: Record<string, number> = {}
      const publish = (id: string, level: number): void => {
        next[id] = level
        const prev = lastSentLevel.get(id) ?? 0
        const wasSpeaking = prev >= SPEAKING_LEVEL
        const isSpeaking = level >= SPEAKING_LEVEL
        // Crossing the threshold matters even for a small numeric change: it
        // is what makes the "X is speaking" chip appear and disappear.
        if (wasSpeaking === isSpeaking && Math.abs(level - prev) < 0.1) return
        lastSentLevel.set(id, level)
        sendControl({ t: 'talk', viewerId: id, level })
      }
      publish('host', mixer.selfLevel())
      for (const v of viewers.value) publish(v.id, mixer.level(v.id))
      levels.value = next
    }, 200)
  }

  /** Send one control frame to a single viewer (grants must not be broadcast). */
  function sendControlTo(viewerId: string, msg: ControlMessage): void {
    // Remembered even when the channel is closed: the panel sets its own
    // `controlledBy` regardless, and a viewer that vanishes right after being
    // granted still has to be cleaned up.
    if (msg.t === 'ctrl-grant') controlGrantee = msg.granted ? viewerId : ''
    const peer = peers.get(viewerId)
    if (peer?.channel?.readyState !== 'open') return
    try {
      peer.channel.send(JSON.stringify(msg))
    } catch (e) {
      appLog.d(TAG, 'control send to viewer failed', viewerId, e)
    }
  }

  /**
   * A viewer sent something.
   *
   * Tagged with the sender (viewers cannot be trusted to name themselves) and
   * relayed to everyone else, so a viewer's laser is visible to the other
   * viewers without a server round trip. The publisher applies it through the
   * same `onControl` hook the UI already listens on.
   */
  function onViewerControl(viewerId: string, raw: unknown): void {
    const msg = parseControlMessage(String(raw))
    if (!msg) return
    if (!VIEWER_MESSAGE_TYPES.has(msg.t)) return
    const tagged = { ...msg, from: viewerId } as ControlMessage
    controlHandlers.forEach((cb) => cb(tagged))
    const data = JSON.stringify(tagged)
    for (const [id, peer] of peers) {
      if (id === viewerId) continue
      if (peer.channel?.readyState === 'open') {
        try {
          peer.channel.send(data)
        } catch {
          /* peer went away mid-send */
        }
      }
    }
  }

  /** Subscribe to control frames from viewers (UI hook). */
  function onControl(cb: (msg: ControlMessage) => void): () => void {
    controlHandlers.add(cb)
    return () => controlHandlers.delete(cb)
  }

  /**
   * A viewer's screen arrived.
   *
   * Relayed to every OTHER viewer with replaceTrack on the third m-line: no
   * renegotiation, and the sharer never gets its own picture back (it would
   * show up as an echo of their own screen, one network hop behind).
   */
  function onRemoteScreen(viewerId: string, track: MediaStreamTrack): void {
    remoteScreenTrack = track
    remoteScreenStream.value = new MediaStream([track])
    screenSharer.value = viewerId
    track.addEventListener('ended', () => {
      if (remoteScreenTrack === track) clearRemoteScreen()
    })
    for (const [id, peer] of peers) {
      if (id === viewerId) continue
      void peer.relayTx?.sender.replaceTrack(track).catch((e) => {
        appLog.w(TAG, 'relay replaceTrack failed', id, e)
      })
    }
  }

  /** Drop the relayed screen and detach it from every viewer. */
  function clearRemoteScreen(): void {
    remoteScreenTrack = null
    remoteScreenStream.value = null
    for (const peer of peers.values()) {
      void peer.relayTx?.sender.replaceTrack(null).catch(() => undefined)
    }
  }

  /** Enable/disable audience screen sharing for the whole room. */
  function setScreenEnabled(on: boolean): void {
    screenEnabled.value = on
    send({ type: 'set-screen-enabled', enabled: on })
  }

  /** Stop one viewer's screen share without disabling the switch. */
  function revokeScreen(viewerId: string): void {
    send({ type: 'screen-revoke', viewerId })
  }

  /** Enable/disable audience speech for the whole room. */
  function setTalkEnabled(on: boolean): void {
    talkEnabled.value = on
    send({ type: 'set-talk-enabled', enabled: on })
  }

  function revokeTalk(viewerId: string): void {
    send({ type: 'talk-revoke', viewerId })
  }

  function muteAll(): void {
    send({ type: 'talk-mute-all' })
    viewers.value = viewers.value.map((v) => ({ ...v, talk: 'muted' }))
  }

  /** End the share and release every device. Idempotent. */
  async function stop(): Promise<void> {
    const tok = token.value
    if (state.value === 'idle' && !tok) return

    // Capture ownership before resetSession() clears it.
    const mayDelete = ownsRoom

    // Stop the notes recorders and wait for their last blobs BEFORE the audio
    // context is closed: MediaRecorder produces the final chunk in `onstop`,
    // so closing first would silently drop the last few seconds of the meeting.
    const notes = useLiveNotes()
    const hadNotes = notes.phase.value !== 'idle'
    if (hadNotes) await notes.stopCapture()

    send({ type: 'end' })
    resetSession()
    state.value = 'idle'
    token.value = ''
    shareUrl.value = ''
    shareLinks.value = []
    viewers.value = []
    talkEnabled.value = false
    screenEnabled.value = false
    screenSharer.value = ''
    remoteScreenStream.value = null

    // Uploads and minutes generation continue in the background; the dialog
    // watches the notes state, not this promise.
    if (hadNotes) void notes.finishUploads()

    if (tok && mayDelete) {
      try {
        await fetch(`/api/live-share?token=${encodeURIComponent(tok)}`, { method: 'DELETE' })
      } catch (e) {
        // The server drops the room when the publisher socket closes anyway;
        // a failed DELETE must not leave the UI stuck in "publishing".
        appLog.w(TAG, 'end share request failed', e)
      }
    }
  }

  return {
    state,
    token,
    shareUrl,
    shareLinks,
    title,
    viewers,
    talkEnabled,
    screenEnabled,
    screenSharer,
    remoteScreenStream,
    error,
    start,
    stop,
    setTalkEnabled,
    revokeTalk,
    muteAll,
    setScreenEnabled,
    revokeScreen,
    sendControl,
    sendControlTo,
    onControl,
    levels,
    switchSource,
    previewStream,
    captureKind,
  }
}

/** Test seam: drop all module state without touching the network. */
export function _resetLiveShareForTest(): void {
  resetSession()
  controlHandlers.clear()
  useLiveNotes().reset()
  state.value = 'idle'
  token.value = ''
  shareUrl.value = ''
  viewers.value = []
  talkEnabled.value = false
  error.value = ''
}
