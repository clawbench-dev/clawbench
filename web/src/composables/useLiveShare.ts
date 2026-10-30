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
import { appLog } from '@/utils/appLog'
import { getNative } from '@/utils/clawbenchNative'
import { buildShareLinks, type ShareLink } from '@/composables/useShareLinks'
import { LiveMixer } from './useLiveMixer'
import { useLiveNotes } from './useLiveNotes'

const TAG = 'LiveShare'

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
/** The mixed track sent to viewers (video + mixed audio). */
let outVideoTrack: MediaStreamTrack | null = null
/** The track a viewer is sharing, relayed to every other viewer. */
let remoteScreenTrack: MediaStreamTrack | null = null
const peers = new Map<string, PublisherPeer>()

function resetSession(): void {
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
  displayStream = null
  micStream = null
  outVideoTrack = null
  remoteScreenTrack = null
  remoteScreenStream.value = null
  screenSharer.value = ''
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
    return Array.isArray(parsed) ? (parsed as CaptureSource[]) : []
  } catch (e) {
    appLog.e(TAG, 'listCaptureSources failed', e)
    return []
  }
}

/** Whether this host can capture system (loopback) audio. */
export async function getAudioCaptureSupport(): Promise<boolean> {
  const bridge = getNative()
  if (!bridge?.getAudioCaptureSupport) return false
  try {
    const raw = await bridge.getAudioCaptureSupport()
    const parsed = JSON.parse(raw) as { systemAudio?: boolean }
    return Boolean(parsed?.systemAudio)
  } catch {
    return false
  }
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

      // 2. Declare the capture source before asking for the stream.
      const bridge = getNative()
      if (!bridge?.setPendingCaptureSource) {
        fail('screen capture is unavailable on this host')
        // The room already exists at this point. Leaving it behind would burn a
        // slot in maxLiveRooms forever (nothing else deletes a room that never
        // got a publisher), so end it explicitly.
        await stop()
        return false
      }
      await bridge.setPendingCaptureSource(opts.sourceId, opts.systemAudio)

      // 3. Capture. The host answers getDisplayMedia with the declared source.
      displayStream = await navigator.mediaDevices.getDisplayMedia({
        video: true,
        audio: opts.systemAudio,
      })
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
          micStream = await navigator.mediaDevices.getUserMedia({
            audio: { echoCancellation: true, noiseSuppression: true, autoGainControl: true },
          })
        } catch (e) {
          // A denied microphone must not abort the share: the audience still
          // gets the screen and (possibly) system audio.
          appLog.w(TAG, 'microphone unavailable, continuing without it', e)
          micStream = null
        }
      }

      // 4. Build the mixing graph. One destination per viewer is created as
      //    viewers arrive (see onViewerJoin).
      audioCtx = new AudioContext({ sampleRate: 48000 })
      await audioCtx.resume()
      mixer = new LiveMixer(audioCtx, displayStream, micStream)

      // AI 听记 records the same sources, but one channel at a time: the mix
      // sent to viewers is per-viewer and excludes that viewer, so it cannot be
      // used to transcribe the meeting. Started before the socket so the first
      // viewer's audio is never missed.
      if (opts.notes) {
        useLiveNotes().start({
          token: token.value,
          ctx: audioCtx,
          systemStream: displayStream,
          micStream,
          title: title.value,
          label: opts.notesLabel,
        })
      }

      // 5. Open the publisher socket.
      await openSocket()

      // 6. Keep the renderer unthrottled: a minimized window otherwise throttles
      //    the encoder and the stream stalls for every viewer.
      void bridge.setPublishingActive?.(true)

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
    peers.set(viewerId, { pc, viewerId, relayTx: null, screenTrack: null })
    viewers.value = [...viewers.value, { id: viewerId, name: '', talk: 'muted' }]

    // One destination per viewer, carrying system audio + mic + the OTHER
    // viewers. Excluding this viewer is what keeps it from hearing itself.
    const mixed = mixer?.addViewer(viewerId) ?? null
    // Both tracks MUST share one MediaStream: the viewer assigns `srcObject`
    // per received stream, so two streams means the second assignment replaces
    // the first and the viewer gets either video or audio, never both.
    const outgoing = new MediaStream()
    if (outVideoTrack) outgoing.addTrack(outVideoTrack)
    if (mixed) outgoing.addTrack(mixed)

    // THREE m-lines, in this exact order (a contract with liveViewer.ts):
    //   1 audio  — our mix down, the viewer's microphone up
    //   2 video  — our picture down, the viewer's shared screen up
    //   3 video  — another viewer's screen, relayed down
    // They are reserved up front so starting/stopping a share only needs
    // replaceTrack. Adding an m-line mid-call would force a renegotiation on a
    // connection that is already carrying the meeting.
    if (mixed) pc.addTransceiver(mixed, { direction: 'sendrecv', streams: [outgoing] })
    else pc.addTransceiver('audio', { direction: 'sendrecv' })
    if (outVideoTrack) pc.addTransceiver(outVideoTrack, { direction: 'sendrecv', streams: [outgoing] })
    else pc.addTransceiver('video', { direction: 'sendrecv' })
    const relayTx = pc.addTransceiver('video', { direction: 'sendonly' })
    if (remoteScreenTrack) void relayTx.sender.replaceTrack(remoteScreenTrack).catch(() => undefined)
    const peer = peers.get(viewerId)
    if (peer) peer.relayTx = relayTx

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

  async function onOffer(viewerId: string, sdp: string): Promise<void> {
    const peer = peers.get(viewerId)
    if (!peer || !sdp) return
    try {
      await peer.pc.setRemoteDescription({ type: 'offer', sdp })
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
    viewers.value = viewers.value.filter((v) => v.id !== viewerId)
  }

  function updateViewer(viewerId: string, patch: Partial<LiveViewerInfo>): void {
    viewers.value = viewers.value.map((v) => (v.id === viewerId ? { ...v, ...patch } : v))
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
  }
}

/** Test seam: drop all module state without touching the network. */
export function _resetLiveShareForTest(): void {
  resetSession()
  useLiveNotes().reset()
  state.value = 'idle'
  token.value = ''
  shareUrl.value = ''
  viewers.value = []
  talkEnabled.value = false
  error.value = ''
}
