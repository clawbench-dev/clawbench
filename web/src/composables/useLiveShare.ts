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
import { ref, shallowRef, watch } from 'vue'
import i18n from '@/i18n'
import { appLog } from '@/utils/appLog'
import { getNative } from '@/utils/clawbenchNative'
import { buildShareLinks, type ShareLink } from '@/composables/useShareLinks'
import { LiveMixer } from './useLiveMixer'
import {
  VIEWER_MESSAGE_TYPES,
  parseControlMessage,
  type ControlMessage,
  type InkStroke,
} from '@/share/liveControl'
import { useLiveNotes } from './useLiveNotes'
import { useDialog } from './useDialog'
import { useMeetingShots, _resetMeetingShotsForTest } from './useMeetingShots'
import { useInkBoard } from '@/share/useInkBoard'
import { notesRecordingSupported } from './liveNotes'
import { startMeetingFileTracking, stopMeetingFileTracking, _resetMeetingFileTrackerForTest } from '@/utils/meetingFileTracker'
import { acquireMic, describeMicError, logMicEnv } from '@/utils/micCapture'
import { startAndroidScreenCapture, type AndroidCaptureHandle } from './androidScreenCapture'
import {
  CAMERA_BUDGET,
  RELAY_BUDGET,
  SCREEN_BUDGET,
  VideoLoadGovernor,
  applyCodecPreference,
  applyContentHint,
  resolvePreferredVideoMime,
  screenCaptureConstraints,
} from './liveMediaPolicy'
import {
  closeRelay,
  openRelay,
  publishMic,
  publishVideo,
  relayConnectURL,
  sendRelayData,
} from './useLiveRelay'

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
  /**
   * The publisher has blocked this viewer's microphone.
   *
   * Distinct from `talk`: `talk` is the viewer's own switch, `muted` is a
   * decision only the host can undo. The viewer's client refuses to turn its
   * microphone back on while this is set, so the button in the list is a real
   * mute rather than a request.
   */
  muted: boolean
}

export interface CaptureSource {
  id: string
  name: string
  kind: 'screen' | 'window'
  thumbnail: string
}

/** One microphone the host can publish from. */
export interface AudioInputDevice {
  /** `deviceId` from enumerateDevices; '' is the system default. */
  id: string
  name: string
}

// ── Module-level session state (one share per app instance) ──────────────────
const state = ref<LiveShareState>('idle')
const token = ref('')
const shareUrl = ref('')
/** Every address this share can be opened on; shareUrl is the primary. */
const shareLinks = ref<ShareLink[]>([])
const title = ref('')
/**
 * The host's nickname, sent with the publish frame.
 *
 * It is what the audience's member list shows on the host row. The room has
 * always known the meeting's TITLE; the host's own name only lived in the chat
 * messages, so the member list would have shown a bare "主持人".
 */
const hostName = ref('')
const viewers = ref<LiveViewerInfo[]>([])
const talkEnabled = ref(false)
/** Room-level switch: may viewers share their screen? */
const screenEnabled = ref(false)
/** Viewer id currently sharing a screen ('' = nobody). */
const screenSharer = ref('')
/** The screen a viewer is sharing, as received from that viewer's PC. */
const remoteScreenStream = shallowRef<MediaStream | null>(null)
const error = ref('')

/**
 * Warn before the page goes away while a meeting is live.
 *
 * The publisher socket IS the meeting's lifetime: when it closes the server
 * tells every viewer `publisher-gone`, deletes the room and closes the record
 * (internal/handler/live_share.go's endPublisherSocket), with no reconnect
 * grace. A stray F5 therefore ends the meeting for everybody watching, and the
 * in-flight recording tail never reaches the server — so the browser's own
 * "leave this page?" prompt is the only thing standing between a reflex and a
 * destroyed meeting.
 *
 * Registered once at module scope rather than from a component: the meeting
 * outlives the panel (switching dock tabs unmounts it) and the state this reads
 * is module-level too. Browsers ignore the message and show their own generic
 * text, so there is nothing to translate. Electron's default is to drop the
 * unload SILENTLY (no prompt at all), which is why the desktop shell answers
 * `will-prevent-unload` with a native dialog; Android's WebView has no
 * onJsBeforeUnload handler and silently skips it.
 */
function warnBeforeLeavingMeeting(e: BeforeUnloadEvent): void {
  if (state.value !== 'publishing') return
  // Cancelling is what makes the prompt appear in current browsers; the
  // returnValue assignment is for engines (and Electron's default dialog) that
  // only look at it.
  e.preventDefault()
  e.returnValue = ''
}

if (typeof window !== 'undefined') window.addEventListener('beforeunload', warnBeforeLeavingMeeting)

/** Set once the F5 guard is on the window; the state it reads is shared too. */
let f5GuardInstalled = false

/**
 * Intercept F5 while a meeting is publishing.
 *
 * A reload is exactly as destructive as closing the page (see the
 * `beforeunload` guard above), but the browser's own prompt cannot be worded
 * for the occasion — and Electron shows nothing at all without a
 * `will-prevent-unload` handler, which made the key look simply broken. This
 * path asks in the app's own dialog and then ends the meeting cleanly.
 *
 * BUBBLE phase, deliberately: the terminal (pages its TUI) and the file manager
 * (refreshes its listing) claim F5 by calling preventDefault on the way up, and
 * `useF5Reload` defers its reload decision so it can read the same flag. Taking
 * the key in the capture phase would break both.
 */
function installF5Guard(endMeeting: () => Promise<void>): void {
  if (f5GuardInstalled || typeof window === 'undefined') return
  f5GuardInstalled = true
  window.addEventListener('keydown', (e: KeyboardEvent) => {
    if (e.key !== 'F5') return
    if (e.ctrlKey || e.metaKey || e.altKey || e.shiftKey) return
    if (e.defaultPrevented) return
    if (state.value !== 'publishing') return
    e.preventDefault()
    void askThenLeave(endMeeting)
  })
}

async function askThenLeave(endMeeting: () => Promise<void>): Promise<void> {
  const ok = await useDialog().confirm(i18n.global.t('meeting.stage.leaveConfirm'), {
    title: i18n.global.t('meeting.stage.leaveTitle'),
    confirmText: i18n.global.t('meeting.stage.leaveRefresh'),
    cancelText: i18n.global.t('meeting.stage.leaveStay'),
    dangerous: true,
  })
  if (!ok) return
  // End it properly instead of letting the socket drop mid-recording: the
  // recorder folds its last segment, minutes generation starts and viewers get
  // a real "ended". `stop()` also clears `state`, so the `beforeunload` guard
  // above no longer blocks the reload.
  try {
    await endMeeting()
  } finally {
    window.location.reload()
  }
}

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
  /**
   * The m-line carrying our CAMERA, present only on viewers that offered a
   * fourth m-line and only while the meeting shares a screen as well.
   *
   * The camera travels on its own m-line so it can be a picture-in-picture
   * tile next to the screen. When the meeting has no screen the camera IS the
   * main picture and rides on `videoTx` instead — one picture, no tile.
   */
  cameraTx: RTCRtpTransceiver | null
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
/** The camera capture, kept apart from `displayStream` so neither owns the other. */
let cameraStream: MediaStream | null = null
/**
 * The camera track sent on the dedicated m-line, or null when there is none.
 *
 * Null in two very different cases, both correct: no camera at all, and a
 * camera-only meeting (where the camera is the MAIN picture and must not also
 * arrive on the tile m-line, or the viewer would render it twice).
 */
let outCameraTrack: MediaStreamTrack | null = null
/** The track a viewer is sharing, relayed to every other viewer. */
let remoteScreenTrack: MediaStreamTrack | null = null
/**
 * Machine-wide encoder governor.
 *
 * One per session, shared by every sender: CPU pressure is global, and separate
 * controllers would react to the same saturation at different times and leave
 * viewers on mismatched quality.
 */
let governor: VideoLoadGovernor | null = null
/** Video codecs this browser advertised, cached for `setCodecPreferences`. */
let videoCodecs: RTCRtpCodec[] = []
/** Hardware-accelerated codec to prefer, or null to keep the browser default. */
let preferredVideoMime: string | null = null
/**
 * LiveKit room, when this meeting's media goes through the relay.
 *
 * Null in peer-to-peer. The two topologies never coexist: the mode is fixed at
 * creation, and every branch below keys off `relayMode` rather than testing the
 * room handle, so a failed connect cannot silently fall back to a p2p room that
 * is capped at eight seats.
 */
let relayRoom: import('livekit-client').Room | null = null
let relayMode = false
/** Identities that were speaking at the previous sample, to send their stop. */
let lastRelaySpeakers: string[] = []
/** UI subscribers for control frames coming from viewers. */
const controlHandlers = new Set<(msg: ControlMessage) => void>()
/**
 * A local copy of the ink that is on the wire.
 *
 * The publisher draws in a separate window (or in Document PiP), so the main
 * window's picture never contains the strokes: on Windows and macOS the
 * overlay is even excluded from the capture. Key frames composite this board
 * on top of the grabbed frame, which is the only way a screenshot shows what
 * the presenter actually pointed at.
 *
 * Fed from both directions: `sendControl` (the publisher's own strokes, and the
 * native overlay's, which arrive as control frames) and `onViewerControl` (the
 * protocol allows a viewer to draw, even though no UI does today).
 */
const inkMirror = useInkBoard()

/** Apply an ink frame to the mirror; anything else is ignored. */
function mirrorInk(msg: ControlMessage): void {
  if (msg.t === 'ink' || msg.t === 'ink-undo' || msg.t === 'ink-clear') inkMirror.apply(msg)
}

/** Drop the mirrored strokes (the annotation window closed). */
function clearInkMirror(): void {
  inkMirror.clear()
}
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
 *
 * `null` means "no screen is on the wire", which is the state a meeting that
 * started without one is in. It must NOT default to 'screen': the running
 * meeting reads this as "a screen is being shared" to route a click on the
 * picture grid to `switchSource` (there is nothing to switch) and to show the
 * annotation tools, so a fresh page load would claim a screen that was never
 * captured.
 */
const captureKind = ref<'screen' | 'window' | null>(null)
/**
 * Which tile of the source list is on the wire, as an id from
 * `listCaptureSources()`.
 *
 * The running meeting draws the same picker the create page draws, so it has
 * to know which tile to mark as selected. `captureKind` cannot answer that —
 * it says screen vs window, not which one — and the browser/Android hosts have
 * no id at all, so their panels map an empty id onto their synthetic tile.
 */
const captureSourceId = ref('')
/**
 * Whether this host can inject input at all (the shell's `input-helper`, Windows
 * only). Probed by the share panel, which owns the native bridge and also needs
 * the answer for its own "allow control" row.
 */
const controlSupported = ref(false)
/**
 * What the room was last told about remote control: injector present AND a
 * whole screen on the wire. Viewers grey their "request control" button on it,
 * so it must follow the picture — a switch to a window capture turns it off.
 */
const controlEnabled = ref(false)
/**
 * Whether a picture is being published RIGHT NOW.
 *
 * Meetings have no type: the host can start or stop a screen share and turn the
 * camera on or off at any time. This is derived state, not a mode — every
 * change is broadcast so viewers and late joiners agree with it.
 */
const videoEnabled = ref(false)
/**
 * Whether the publisher's camera is on right now.
 *
 * Distinct from "the camera exists": the track stays in the peer connection
 * while off (`enabled = false`), so flipping it back needs no renegotiation.
 * Viewers learn about the change from the `picture-state` frame.
 */
const cameraEnabled = ref(false)
/** The camera capture, exposed so the publisher can see itself. */
const previewCameraStream = shallowRef<MediaStream | null>(null)
/** Whether the publisher's microphone is currently being sent. */
const micEnabled = ref(true)
/**
 * The device the live microphone came from ('' = the system default).
 *
 * Tracked because the UI's cards and the captured stream must agree: selecting
 * a card re-opens the device only when it is actually a different one, and
 * "unmute" has to know whether the stream still matches the chosen card.
 */
const activeMicDevice = ref('')
/** Speaker label fn from `start()`, reused when 听记 is switched on mid-meeting. */
let notesLabelFn: ((channel: string) => string) | undefined
/**
 * Whether 听记 has been started for the CURRENT share.
 *
 * A pause has to RESUME the recorder (keeping what was already transcribed),
 * but a meeting that never had notes must not inherit the previous meeting's
 * transcript — the notes module is a singleton and keeps its state across
 * shares. `phase` alone cannot tell the two apart: both are 'idle'.
 */
let notesStarted = false
/**
 * Bumped by every `resetSession()`.
 *
 * Capturing is asynchronous and can take minutes (the OS picker stays open),
 * and the meeting can end while it is in flight. Every entry point that awaits
 * a capture compares this afterwards: a late continuation that writes the
 * stream into the module would re-light the camera/microphone/screen indicator
 * for a meeting that no longer exists, with no UI left to turn it off.
 */
let sessionGen = 0
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
  sessionGen += 1
  notesStarted = false
  stopTalkMetering()
  governor?.stop()
  governor = null
  videoCodecs = []
  preferredVideoMime = null
  // Disconnect before dropping the handle: leaving the LiveKit room open keeps
  // the server's participant alive and the OS camera/mic indicators lit.
  closeRelay(relayRoom)
  relayRoom = null
  relayMode = false
  lastRelaySpeakers = []
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
  for (const s of [displayStream, micStream, cameraStream]) {
    s?.getTracks().forEach((t) => t.stop())
  }
  // Ends the native projection as well; stopping the tracks alone would leave
  // the foreground service capturing (and the system "screen is shared" badge
  // showing) with nobody reading the frames.
  androidCapture?.stop()
  androidCapture = null
  displayStream = null
  previewStream.value = null
  captureKind.value = null
  captureSourceId.value = ''
  controlSupported.value = false
  controlEnabled.value = false
  videoEnabled.value = false
  micStream = null
  cameraStream = null
  previewCameraStream.value = null
  cameraEnabled.value = false
  outVideoTrack = null
  outCameraTrack = null
  remoteScreenTrack = null
  remoteScreenStream.value = null
  screenSharer.value = ''
  // The mirror belongs to the meeting, not to the module: a leftover stroke
  // would be composited into the next meeting's first frame.
  inkMirror.clear()
  hostName.value = ''
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

/**
 * Which message a failed camera capture deserves.
 *
 * `NotAllowedError` means the user (or an OS privacy switch) refused, which
 * needs a different fix from "the device is busy or missing". Telling somebody
 * to grant permission when the camera is held by another app sends them to the
 * wrong place, so the two never share a message.
 */
function cameraErrorKey(e: unknown): string {
  const name = (e as { name?: string } | null)?.name
  return name === 'NotAllowedError' ? 'meeting.create.cameraDenied' : 'meeting.create.cameraUnavailable'
}

/**
 * Whether this host has a camera to offer.
 *
 * Kinds are visible without permission (only labels need it), so this answers
 * "should the camera option be selectable at all" without prompting. Any
 * failure answers true: guessing "no camera" would hide the feature from a
 * machine that has one.
 */
export async function hasCameraDevice(): Promise<boolean> {
  const md = navigator.mediaDevices
  if (!md?.getUserMedia || !md.enumerateDevices) return false
  try {
    const devices = await md.enumerateDevices()
    return devices.some((d) => d.kind === 'videoinput')
  } catch {
    return true
  }
}

/** The last camera failure, so the caller can pick the right message. */
let lastCameraError: unknown = null

/**
 * The constraints every microphone capture uses.
 *
 * One definition because the same microphone is opened twice — at start and
 * when the host switches input mid-meeting — and two copies would drift into
 * "the device I switched to sounds different" bugs.
 */
function micConstraints(deviceId: string): MediaTrackConstraints {
  return {
    echoCancellation: true,
    noiseSuppression: true,
    autoGainControl: true,
    // A chosen input wins; otherwise the OS default, which is what `audio:
    // true` means. `{exact: ''}` would fail on every machine.
    ...(deviceId ? { deviceId: { exact: deviceId } } : {}),
  }
}

/**
 * The microphones this machine can publish from.
 *
 * Labels are empty until the page has been granted microphone access — the
 * browser hides them from a page that could otherwise fingerprint the user's
 * hardware, which is the normal state on a first visit. Unnamed devices are
 * therefore NUMBERED rather than hidden: the list still works as a picker, and
 * the real names appear once permission has been granted (i.e. from the second
 * meeting onwards).
 */
export async function listAudioInputs(): Promise<AudioInputDevice[]> {
  return listInputs('audioinput', 'meeting.create.micDeviceN')
}

/** The cameras this machine can publish from. Same rules as the microphones. */
export async function listVideoInputs(): Promise<AudioInputDevice[]> {
  return listInputs('videoinput', 'meeting.create.cameraDeviceN')
}

async function listInputs(
  kind: 'audioinput' | 'videoinput',
  fallbackKey: string,
): Promise<AudioInputDevice[]> {
  const md = navigator.mediaDevices
  if (!md?.enumerateDevices) return []
  try {
    const devices = await md.enumerateDevices()
    return devices
      .filter((d) => d.kind === kind)
      .map((d, i) => ({
        id: d.deviceId,
        name: d.label || i18n.global.t(fallbackKey, { n: i + 1 }),
      }))
  } catch (e) {
    appLog.w(TAG, `list ${kind} failed`, e)
    return []
  }
}

/**
 * Acquire the camera at the size the tile uses.
 *
 * Small and slow on purpose: a face in a corner (or the whole picture on a
 * camera-only call), and each viewer costs the publisher another upload of it.
 * 640×480 at 15 fps is plenty.
 */
async function acquireCamera(deviceId = ''): Promise<MediaStream | null> {
  const gum = navigator.mediaDevices?.getUserMedia
  if (!gum) {
    lastCameraError = new Error('getUserMedia is unavailable on this host')
    return null
  }
  try {
    lastCameraError = null
    const stream = await gum.call(navigator.mediaDevices, {
      video: {
        width: { ideal: 640 },
        height: { ideal: 480 },
        frameRate: { ideal: 15, max: 20 },
        // A chosen camera wins; empty means the system default. `{exact: ''}`
        // would fail everywhere, which is why this is conditional.
        ...(deviceId ? { deviceId: { exact: deviceId } } : {}),
      },
      audio: false,
    })
    applyContentHint(stream.getVideoTracks()[0], 'motion')
    return stream
  } catch (e) {
    appLog.w(TAG, `camera unavailable: ${describeMicError(e)}`)
    lastCameraError = e
    return null
  }
}

/** The device a live camera stream came from, as the browser reports it. */
function cameraDeviceOf(stream: MediaStream | null): string {
  const track = stream?.getVideoTracks()[0] as { getSettings?: () => { deviceId?: string } } | undefined
  return track?.getSettings?.().deviceId ?? ''
}

/**
 * Decide once per session which video codec to prefer, and cache it.
 *
 * Per-peer decisions are not an option: every answer would name whatever that
 * peer happened to get, so the publisher would encode H.264 for one viewer and
 * VP8 for the next — multiplying the encoder work this policy exists to cut.
 *
 * A null answer means "no hardware encoder, or cannot tell", and leaves the
 * browser's own choice alone.
 */
async function resolveVideoCodecPreference(): Promise<void> {
  try {
    const caps = typeof RTCRtpSender === 'undefined' ? null : RTCRtpSender.getCapabilities?.('video')
    videoCodecs = caps?.codecs ? [...caps.codecs] : []
  } catch (e) {
    appLog.d(TAG, 'video codec capabilities unavailable', e)
    videoCodecs = []
  }
  preferredVideoMime = await resolvePreferredVideoMime()
  if (preferredVideoMime) appLog.i(TAG, 'hardware video encoder preferred', preferredVideoMime)
}

// ── Relay (LiveKit) ────────────────────────────────────────────────────────

/** The viewer id inside a LiveKit identity (`viewer:3` → `3`). */
function relayViewerId(identity: string): string {
  return identity.startsWith('viewer:') ? identity.slice('viewer:'.length) : ''
}

/**
 * Publish the current picture.
 *
 * One video track at a time: screen and camera both travel as video, and the
 * viewer distinguishes them by source. A camera TILE next to a screen is a
 * peer-to-peer layout (two m-lines); in relay mode the main picture is the only
 * one that matters, and publishing two video tracks would double every viewer's
 * downstream for a thumbnail.
 */
async function publishRelayPicture(): Promise<void> {
  if (!relayRoom) return
  const screen = displayStream?.getVideoTracks()[0] ?? null
  const camera = cameraStream?.getVideoTracks()[0] ?? null
  const main = screen ?? camera
  await publishVideo(relayRoom, screen, 'screen_share')
  await publishVideo(relayRoom, screen ? null : camera, 'camera')
  applyContentHint(main, screen ? 'text' : 'motion')
}

/** A control frame from a viewer, over LiveKit's data channel. */
function handleRelayData(payload: Uint8Array): void {
  const msg = parseControlMessage(new TextDecoder().decode(payload))
  if (msg) controlHandlers.forEach((cb) => cb(msg))
}

/**
 * LiveKit computes audio levels server-side, so the relay path does not poll.
 *
 * Returns the new level map plus the frames to broadcast: a speaker that has
 * stopped needs an explicit zero, or its chip stays lit forever.
 */
export function relaySpeakerFrames(
  identities: readonly string[],
  previous: readonly string[],
): { levels: Record<string, number>; frames: Array<{ id: string; level: number }> } {
  const speaking = identities.filter((id) => id === 'host' || relayViewerId(id) !== '')
  const levels: Record<string, number> = {}
  const frames: Array<{ id: string; level: number }> = []
  for (const identity of speaking) {
    const id = identity === 'host' ? 'host' : relayViewerId(identity)
    levels[id] = 1
    frames.push({ id, level: 1 })
  }
  for (const identity of previous) {
    if (speaking.includes(identity)) continue
    frames.push({ id: identity === 'host' ? 'host' : relayViewerId(identity), level: 0 })
  }
  return { levels, frames }
}

/**
 * The host subscribes to every guest's microphone, because that audio is what
 * the transcript is made of. A guest it does not subscribe to is silently
 * missing from the minutes.
 */
function handleRelayTrack(track: MediaStreamTrack, _source: unknown, identity: string): void {
  const viewerId = relayViewerId(identity)
  if (viewerId && track.kind === 'audio') {
    useLiveNotes().addViewer(viewerId, new MediaStream([track]))
  }
}

function handleRelayDisconnected(reason: string): void {
  appLog.w(TAG, 'relay disconnected', reason)
  relayRoom = null
  if (state.value === 'publishing') fail(i18n.global.t('liveShare.relayLost'))
}

/** Connect and publish everything this meeting has. */
async function connectRelay(
  relay: { url: string; token: string },
  onActiveSpeakers: (identities: string[]) => void,
): Promise<void> {
  relayRoom = await openRelay(relayConnectURL(), relay.token, {
    onData: handleRelayData,
    onActiveSpeakers,
    onTrack: handleRelayTrack,
    onDisconnected: handleRelayDisconnected,
  })
  await publishRelayPicture()
  const mic = micEnabled.value ? (micStream?.getAudioTracks()[0] ?? null) : null
  await publishMic(relayRoom, mic)
}

/**
 * Capture a screen or window on whichever host we are running on.
 *
 * Three hosts, two mechanisms:
 * - Electron declares a source and answers getDisplayMedia with it.
 * - Android has no getDisplayMedia at all, so it runs a native MediaProjection
 *   session and hands back a stream built from its frames (see
 *   androidScreenCapture.ts).
 * - A plain browser has no declaration step: its own picker IS the source
 *   choice, so it calls getDisplayMedia directly.
 *
 * System audio is always requested. It is not a user choice: a loopback track
 * arrives with the picture or not at all, and a source without one simply
 * yields a video-only stream.
 */
/**
 * What one capture attempt produced.
 *
 * Returned instead of written into the module by `captureScreen()` itself: a
 * capture can resolve AFTER the meeting it belonged to is over (the picker
 * stays open for minutes, a second meeting may already be running), and a
 * write made inside the capture would have already clobbered the new meeting's
 * picture before the caller could compare generations. The caller applies these
 * only once it knows the capture is still wanted.
 */
interface CapturedScreen {
  stream: MediaStream
  /** What the picture is; the panel routes the next click by this. */
  kind: 'screen' | 'window'
  /** Which tile this is, so the live picker can mark it. */
  sourceId: string
  /** The native projection, on the Android path only. */
  android: AndroidCaptureHandle | null
}

async function captureScreen(sourceId: string): Promise<CapturedScreen | null> {
  const bridge = getNative()
  if (bridge?.startScreenCapture) {
    const android = await startAndroidScreenCapture({
      fps: ANDROID_CAPTURE_FPS,
      maxWidth: ANDROID_CAPTURE_MAX_WIDTH,
      quality: ANDROID_CAPTURE_QUALITY,
      systemAudio: true,
      // The user can revoke the projection from the system UI. That ends the
      // SCREEN, not the meeting: without a meeting type there is no reason to
      // hang up because the picture went away.
      onEnded: () => {
        void stopScreen()
      },
    })
    applyContentHint(android.stream.getVideoTracks()[0], 'text')
    return { stream: android.stream, kind: 'screen', sourceId, android }
  }

  const capture = navigator.mediaDevices?.getDisplayMedia
  if (!capture) {
    fail('screen capture is unavailable on this host')
    return null
  }
  try {
    const declare = bridge?.setPendingCaptureSource
    const declaresSource = Boolean(declare && bridge)
    if (declare && bridge) {
      await declare.call(bridge, sourceId, true)
    }
    const stream = await capture.call(navigator.mediaDevices, {
      // A capture cap, not a preference: the publisher encodes this picture once
      // per viewer, so native 4K multiplies across every connection. See
      // liveMediaPolicy.ts for the numbers.
      video: screenCaptureConstraints(),
      audio: true,
    })
    applyContentHint(stream.getVideoTracks()[0], 'text')
    // Recorded only once the picture is actually here. Setting it before the
    // await (as the source declaration used to) left the meeting believing a
    // screen was on the wire after a cancelled picker, and every later click on
    // the grid then went to `switchSource` — which has nothing to swap and can
    // only report "switch failed".
    const kind: 'screen' | 'window' = declaresSource
      ? sourceId.startsWith('screen:')
        ? 'screen'
        : 'window'
      : captureKindOf(stream)
    // Remember which tile this is so the live picker can mark it. A browser
    // pick has no id (its dialog chose), which the panel maps to its own tile.
    return { stream, kind, sourceId, android: null }
  } catch (e) {
    appLog.w(TAG, 'screen capture unavailable', e)
    // The declaration is one-shot; leaving it armed would let the NEXT
    // getDisplayMedia capture that source without a picker.
    void bridge?.setPendingCaptureSource?.('', false)
    return null
  }
}

/**
 * Release a capture that turned out to belong to nobody.
 *
 * Nothing else holds a reference to it — `resetSession()` only stops what was
 * assigned at the time it ran — so this is the only place that can put the
 * device (and, on Android, the native projection) back down.
 */
function discardLateCapture(captured: CapturedScreen | null): void {
  captured?.stream.getTracks().forEach((track) => track.stop())
  captured?.android?.stop()
}

/** Adopt a capture into the module's picture state. */
function adoptScreen(captured: CapturedScreen): MediaStream {
  androidCapture = captured.android
  captureKind.value = captured.kind
  captureSourceId.value = captured.sourceId
  return captured.stream
}

/**
 * Point every peer's m-lines at the current picture.
 *
 * The m-lines are negotiated once, at join, as sendrecv/sendonly with no track.
 * That is what makes a picture appear or disappear without renegotiating: the
 * receiver already exists, it simply starts (or stops) receiving frames.
 *
 * Two m-lines are involved: the main one carries the screen when there is one
 * and the camera otherwise, and the tile line carries the camera only while a
 * screen is also up.
 */
async function applyPicture(): Promise<void> {
  const screenTrack = displayStream?.getVideoTracks()[0] ?? null
  const cameraTrack = cameraStream?.getVideoTracks()[0] ?? null
  const main = screenTrack ?? cameraTrack
  const tile = screenTrack && cameraEnabled.value ? cameraTrack : null
  outVideoTrack = main
  outCameraTrack = tile
  // Relay mode publishes instead of fanning out to per-viewer senders.
  if (relayMode) {
    await publishRelayPicture()
    publishControl()
    return
  }
  // The m-line is the same whether it carries a screen or a camera, but the two
  // want different encode budgets and different encoder hints.
  applyContentHint(main, screenTrack ? 'text' : 'motion')
  for (const peer of peers.values()) {
    governor?.setBudget(`${peer.viewerId}:video`, screenTrack ? SCREEN_BUDGET : CAMERA_BUDGET)
    try {
      await peer.videoTx?.sender.replaceTrack(main)
    } catch (e) {
      appLog.w(TAG, 'main picture replaceTrack failed', peer.viewerId, e)
    }
    if (peer.cameraTx) {
      try {
        await peer.cameraTx.sender.replaceTrack(tile)
      } catch (e) {
        appLog.w(TAG, 'camera tile replaceTrack failed', peer.viewerId, e)
      }
    }
  }
  // The picture just changed, and remote control depends on what it is: a
  // window capture or a camera-only meeting must take the viewers' button away.
  publishControl()
}

/**
 * Tell the room what the picture is now.
 *
 * One frame for all three flags: they always change together, and a viewer that
 * applied them separately could render a combination that never existed. The
 * room keeps them too, so a viewer joining later gets the current state in its
 * `joined` frame.
 */
function publishPicture(): void {
  const screen = Boolean(displayStream?.getVideoTracks().length)
  const camera = cameraEnabled.value && Boolean(cameraStream?.getVideoTracks().length)
  videoEnabled.value = screen || camera
  send({ type: 'set-picture', video: videoEnabled.value, camera, screen })
}

/**
 * Advertise whether viewers may ask to drive this publisher.
 *
 * The capability is the publisher's alone: it needs the input injector AND a
 * whole-screen picture (a window capture has no desktop to map the pointer
 * onto). Viewers cannot work that out, so the room carries it and every change
 * is pushed — a viewer that joined earlier has no other way to learn.
 */
function publishControl(): void {
  const on =
    controlSupported.value &&
    captureKind.value === 'screen' &&
    Boolean(displayStream?.getVideoTracks().length)
  if (on === controlEnabled.value) return
  controlEnabled.value = on
  send({ type: 'set-control-enabled', enabled: on })
}

/** The share panel's native probe result. Called once it resolves. */
function setControlSupported(on: boolean): void {
  if (controlSupported.value === on) return
  controlSupported.value = on
  publishControl()
}

/** Start sharing a screen mid-meeting (or after joining with none). */
async function startScreen(sourceId: string): Promise<boolean> {
  if (state.value !== 'publishing' || displayStream) return false
  // The message is read by the caller right after this resolves, so it must
  // describe THIS attempt: a cancelled picker returns null without setting one,
  // and a leftover from an earlier failure would be reported as its cause.
  error.value = ''
  const gen = sessionGen
  let captured: CapturedScreen | null
  try {
    captured = await captureScreen(sourceId)
  } catch (e) {
    // The Android host throws (consent denied, projection failed) where the
    // desktop path returns null. Either way this click has to end with a
    // message: an unhandled rejection would leave the button silently dead.
    appLog.w(TAG, 'screen capture failed', e)
    fail(e instanceof Error ? e.message : String(e))
    return false
  }
  if (gen !== sessionGen) {
    // The meeting ended (or another one began) while the picker was open.
    discardLateCapture(captured)
    return false
  }
  if (!captured) return false
  const stream = adoptScreen(captured)
  displayStream = stream
  previewStream.value = stream
  mixer?.setSystemStream(stream)
  stream.getVideoTracks()[0]?.addEventListener('ended', () => {
    void stopScreen()
  })
  await applyPicture()
  publishPicture()
  return true
}

/**
 * Stop sharing the screen without ending the meeting.
 *
 * A camera that is on takes over the main picture — the meeting simply keeps
 * going with a different picture. When there is no camera either, viewers get
 * the "no picture" placeholder.
 */
async function stopScreen(): Promise<void> {
  if (!displayStream) return
  const stream = displayStream
  displayStream = null
  previewStream.value = null
  captureKind.value = null
  captureSourceId.value = ''
  androidCapture?.stop()
  androidCapture = null
  for (const track of stream.getTracks()) track.stop()
  mixer?.setSystemStream(null)
  await applyPicture()
  publishPicture()
}

/**
 * Turn the camera on, off, or onto a different device.
 *
 * `''` turns it off; `'default'` turns it on with whatever the system picks;
 * anything else is a `deviceId`. Turning it off keeps the track (muted) so
 * turning it back on is instant; switching devices opens the new one and
 * releases the old, and the m-lines are rewired without renegotiating.
 */
async function setCameraDevice(choice: string): Promise<boolean> {
  error.value = ''
  if (!choice) {
    cameraEnabled.value = false
    cameraStream?.getVideoTracks().forEach((track) => {
      track.enabled = false
    })
    await applyPicture()
    publishPicture()
    return true
  }
  const wanted = choice === 'default' ? '' : choice
  // "System default" with a stream already open means "on": re-opening it would
  // blink the self-view for no reason. A named device only counts as the same
  // one when the live stream reports that id.
  const sameDevice = Boolean(cameraStream) && (wanted === '' || cameraDeviceOf(cameraStream) === wanted)
  if (!sameDevice) {
    const gen = sessionGen
    const next = await acquireCamera(wanted)
    if (gen !== sessionGen) {
      // The meeting ended while the camera was opening: releasing it here is
      // the only chance, the camera light would otherwise stay on forever.
      next?.getTracks().forEach((track) => track.stop())
      return false
    }
    if (!next) {
      error.value = i18n.global.t(cameraErrorKey(lastCameraError))
      return false
    }
    cameraStream?.getTracks().forEach((track) => track.stop())
    cameraStream = next
    previewCameraStream.value = next
  }
  cameraEnabled.value = true
  cameraStream?.getVideoTracks().forEach((track) => {
    track.enabled = true
  })
  await applyPicture()
  publishPicture()
  return true
}

/**
 * Who is sharing the screen right now: "" | "host" | "viewer:<id>".
 *
 * An audience member wins over the host's own picture: once a participant takes
 * the screen, the host's camera/screen is no longer what everyone is looking at.
 * Outside a meeting this is always "", so the watcher below cannot report the
 * module's initial values.
 */
function currentScreenSharer(): string {
  if (state.value !== 'publishing') return ''
  if (screenSharer.value) return `viewer:${screenSharer.value}`
  return captureKind.value ? 'host' : ''
}

/** The nickname shown in the transcript; empty for the host (localized there). */
function screenSharerLabel(sharer: string): string {
  if (!sharer.startsWith('viewer:')) return ''
  const id = sharer.slice('viewer:'.length)
  return viewers.value.find((v) => v.id === id)?.name ?? ''
}

/**
 * Records a CHANGE of screen sharer into the meeting timeline.
 *
 * Best effort: the meeting itself never depends on this landing, and the server
 * treats a repeated sharer as a no-op rather than a new interval.
 */
async function reportScreenSharer(sharer: string): Promise<void> {
  const tok = token.value
  if (!tok || state.value !== 'publishing') return
  try {
    const resp = await fetch('/api/live-share/notes/screen', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        token: tok,
        sharer,
        sharerName: screenSharerLabel(sharer),
        at: Date.now(),
      }),
    })
    if (!resp.ok) throw new Error(`status ${resp.status}`)
  } catch (e) {
    appLog.w(TAG, 'report screen sharer failed', e)
  }
}

// Every transition — the host switching picture, a viewer claiming the screen,
// a revocation, the sharer disconnecting — ends up here, because all of them
// are expressed by these two refs changing.
watch(currentScreenSharer, (next, prev) => {
  if (next === prev) return
  void reportScreenSharer(next)
  // A member taking the screen over (or giving it back) changes the picture the
  // meeting is about. Host → host is a source switch, which this watcher does
  // NOT see: `switchSource` reports that one itself.
  if (next) useMeetingShots().noteSourceChanged('sharer')
})

export function useLiveShare() {
  /**
   * Create (or reuse) the room, capture whatever the host ticked, and start
   * publishing.
   *
   * There is no meeting type. The checkboxes are independent — screen, camera,
   * microphone — and every one of them can be flipped again while the meeting
   * runs (`startScreen` / `stopScreen` / `setCameraDevice`).
   *
   * `sourceId` comes from listCaptureSources(); the host consumes it on the next
   * getDisplayMedia call and clears it immediately.
   */
  async function start(opts: {
    /**
     * Which screen/window to share, or '' for none.
     *
     * Empty is a normal value: a camera-only or voice-only meeting shares no
     * screen, and a plain browser has no list to pick from — its own picker
     * decides (see `screen`).
     */
    sourceId: string
    /**
     * Whether a screen should be captured at all.
     *
     * Separate from `sourceId` because a browser's source is chosen in ITS
     * picker, not by us: it has an empty id and still shares a screen. When
     * omitted, a non-empty `sourceId` implies a screen (the desktop path).
     */
    screen?: boolean
    microphone: boolean
    /**
     * Which input device to capture, by `deviceId`; empty means the system
     * default.
     *
     * Only the id is passed: `exact` on a device that has since been
     * unplugged fails the whole capture, and the existing failure path already
     * degrades to "no local channel" instead of aborting the meeting.
     */
    micDeviceId?: string
    /**
     * Which camera to publish: `''` for none, `'default'` for the system
     * default, otherwise a `deviceId`.
     */
    cameraDeviceId?: string
    shareTitle?: string
    /**
     * The host's nickname. Sent with the publish frame so the audience's member
     * list can name the host row; omitted means the page shows 主持人.
     */
    hostName?: string
    /** Enable AI 听记 (transcription + minutes) for this share. */
    notes?: boolean
    /**
     * Keep key frames (a screenshot when a file is opened, the source changes,
     * or the presenter says "look here").
     *
     * Independent of `notes`: a meeting may want the pictures without a
     * transcript, or the transcript without pictures. Omitted means on.
     */
    shots?: boolean
    /**
     * Ask for minutes when the meeting ends. Omitted means on (the historical
     * behaviour); the audio is merged either way, so turning this off only
     * skips the automatic summary request.
     */
    autoMinutes?: boolean
    /**
     * The two audience permissions the room STARTS with. They are room-level
     * switches (not local capture choices), so they travel with the create
     * request instead of a follow-up control frame: viewers may join before the
     * publisher socket is up, and they must see the right state from their
     * `joined` frame.
     *
     * Omitted means off, which is what every client before these options did.
     */
    allowTalk?: boolean
    allowMemberScreen?: boolean
    /**
     * Route media through the external LiveKit relay instead of peer-to-peer.
     *
     * Chosen once, at creation: switching topologies mid-meeting rebuilds every
     * connection and blacks the room out for seconds. Omitted means p2p, which
     * is what every client before the relay asked for.
     */
    useRelay?: boolean
    /** Localised speaker label for a channel ("system" / "mic" / "viewer:<id>"). */
    notesLabel?: (channel: string) => string
  }): Promise<boolean> {
    if (state.value !== 'idle') return false
    error.value = ''
    state.value = 'capturing'

    try {
      // 1. Capture before the room.
      //
      //    `getDisplayMedia` requires a transient user activation, which lapses
      //    a few seconds after the click. The room POST plus the address lookup
      //    are two network round trips, and on a slow link they can outlive it —
      //    the browser then rejects a request the user really did make. Firefox
      //    and Safari are the strictest about the window. Nothing the capture
      //    needs is remote (the native source declaration is IPC), so going
      //    first costs nothing, and a cancelled picker no longer leaves a room
      //    behind that has to be deleted again.
      const bridge = getNative()
      const wantsScreen = opts.screen ?? Boolean(opts.sourceId)
      const wantsCamera = Boolean(opts.cameraDeviceId)
      micEnabled.value = Boolean(opts.microphone)
      notesLabelFn = opts.notesLabel
      if (wantsScreen) {
        const captured = await captureScreen(opts.sourceId)
        if (!captured) {
          // The picker was cancelled or the host cannot capture: there is
          // nothing to publish, and the room does not exist yet.
          await stop()
          return false
        }
        displayStream = adoptScreen(captured)
      }
      if (wantsCamera) {
        cameraStream = await acquireCamera(
          opts.cameraDeviceId === 'default' ? '' : (opts.cameraDeviceId ?? ''),
        )
        if (!cameraStream) {
          // A missing camera no longer aborts: without a meeting type there is
          // no "camera-only meeting" that would be left with a black rectangle.
          // The host can start one anyway and turn the camera on later.
          error.value = i18n.global.t(cameraErrorKey(lastCameraError))
        }
      }

      const screenTrack = displayStream?.getVideoTracks()[0] ?? null
      const cameraTrack = cameraStream?.getVideoTracks()[0] ?? null
      // The main picture: the screen when there is one, the camera otherwise.
      outVideoTrack = screenTrack ?? cameraTrack
      // Only a screen+camera picture sends the camera on the tile m-line. A
      // camera-only picture already carries it as the main one; sending it
      // twice would render the same face full-screen and in a corner.
      outCameraTrack = screenTrack ? cameraTrack : null
      cameraEnabled.value = Boolean(cameraTrack)
      previewStream.value = displayStream
      previewCameraStream.value = cameraStream
      videoEnabled.value = Boolean(screenTrack) || cameraEnabled.value
      // The user can also end the share from the browser's own capture banner.
      // It stops the SCREEN, not the meeting: the meeting is no longer a screen
      // share, so losing the picture is not a reason to end the call.
      screenTrack?.addEventListener('ended', () => {
        void stopScreen()
      })

      if (opts.microphone) {
        try {
          micStream = await acquireMic({ audio: micConstraints(opts.micDeviceId ?? '') })
          activeMicDevice.value = opts.micDeviceId ?? ''
        } catch (e) {
          // A denied microphone must not abort the share: the audience still
          // gets the screen and (possibly) system audio. The notes channel is
          // lost, so leave a trail that can be diagnosed later.
          appLog.w(TAG, `microphone unavailable, continuing without it: ${describeMicError(e)}`)
          void logMicEnv('LiveShare')
          micStream = null
        }
      }

      // 3. Room. Ask for a token. A failure here must release the capture:
      //    otherwise the browser's "sharing" indicator stays lit with no room
      //    to publish to.
      const resp = await fetch('/api/live-share', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          title: opts.shareTitle ?? '',
          notes: Boolean(opts.notes),
          // The room's initial picture state. It is not a meeting type: any of
          // these can change a second later through `set-picture`.
          video: videoEnabled.value,
          camera: cameraEnabled.value,
          screen: Boolean(displayStream),
          // Audience permissions, likewise a starting state: the meeting's
          // 会议管理 tab can flip both at any time afterwards.
          allowTalk: Boolean(opts.allowTalk),
          allowMemberScreen: Boolean(opts.allowMemberScreen),
          // Topology, fixed at creation. The server refuses the request when
          // the relay is not configured rather than silently giving a p2p room.
          useRelay: Boolean(opts.useRelay),
        }),
      })
      if (!resp.ok) {
        fail(`create room failed: ${resp.status}`)
        await stop()
        return false
      }
      const data = (await resp.json()) as {
        token?: string
        path?: string
        talkEnabled?: boolean
        screenEnabled?: boolean
        mode?: string
        relay?: { url: string; token: string; room: string }
      }
      if (!data.token) {
        fail('create room returned no token')
        await stop()
        return false
      }
      token.value = data.token
      // The publisher never receives a `joined` frame, so the create response is
      // the only source for these two room-level switches. Without this the
      // 会议管理 tab would draw both as off while the room has them on.
      talkEnabled.value = Boolean(data.talkEnabled)
      screenEnabled.value = Boolean(data.screenEnabled)
      // Build from the server's address list, not window.location.origin: the
      // browser only knows the one address it used, which may not be reachable
      // from a colleague's subnet.
      shareLinks.value = await buildShareLinks(data.path ?? `/share/${data.token}`)
      shareUrl.value = shareLinks.value[0]?.url ?? window.location.origin + `/share/${data.token}`
      title.value = opts.shareTitle ?? ''
      hostName.value = opts.hostName?.trim() ?? ''

      // 4. Build the mixing graph. One destination per viewer is created as
      //    viewers arrive (see onViewerJoin).
      //
      //    `resume()` is started but deliberately NOT awaited: on a host with no
      //    audio output device (headless Firefox, a kiosk, a machine whose audio
      //    server is down) the context stays `suspended` and the promise stays
      //    PENDING FOREVER — awaiting it hung meeting creation after the room had
      //    already been created, so the stage never appeared and the meeting
      //    could not be ended or chatted in. Audio is optional here: a meeting
      //    with only a picture and chat is a normal meeting, and a context that
      //    is allowed to run later starts producing sound at that point.
      audioCtx = new AudioContext({ sampleRate: 48000 })
      void audioCtx.resume().catch((e: unknown) => {
        appLog.w(TAG, `audio context resume failed, continuing without audio: ${String(e)}`)
      })
      mixer = new LiveMixer(audioCtx, displayStream, micStream)
      startTalkMetering()

      // AI 听记 records the same sources, but one channel at a time: the mix
      // sent to viewers is per-viewer and excludes that viewer, so it cannot be
      // used to transcribe the meeting. Started before the socket so the first
      // viewer's audio is never missed.
      //
      // An engine without `MediaRecorder` simply has no 听记: the room still
      // comes up with the picture, the microphone and the chat. `notesStarted`
      // must NOT be set in that case — it is what tells the end-of-meeting
      // merge to look for recordings that were never made.
      if (opts.notes && notesRecordingSupported()) {
        notesStarted = true
        useLiveNotes().start({
          token: token.value,
          ctx: audioCtx,
          micStream,
          title: title.value,
          label: opts.notesLabel,
          autoMinutes: opts.autoMinutes,
        })
      } else if (opts.notes) {
        appLog.w(TAG, 'MediaRecorder unavailable: starting the meeting without notes')
      }

      // 5. Encode policy. One governor for the whole session (CPU is shared by
      //    every sender) and one codec decision, resolved BEFORE the socket
      //    opens so the first viewer's offer is answered with it: a codec
      //    preference only affects negotiation that has not happened yet.
      governor?.stop()
      governor = new VideoLoadGovernor()
      governor.start()
      await resolveVideoCodecPreference()

      // 6. Open the publisher socket.
      await openSocket()

      // 6b. Relay. The create reply carries the room's mode and, in sfu mode,
      //     this participant's LiveKit credential. Connecting here (before the
      //     state flips to publishing) means the first viewer already finds a
      //     publisher in the room.
      if (data.relay) {
        relayMode = true
        await connectRelay(data.relay, (identities) => {
          const { levels: next, frames } = relaySpeakerFrames(identities, lastRelaySpeakers)
          levels.value = next
          for (const frame of frames) {
            sendControl({ t: 'talk', viewerId: frame.id, level: frame.level })
          }
          lastRelaySpeakers = [...identities]
        })
      }

      // 7. Keep the renderer unthrottled: a minimized window otherwise throttles
      //    the encoder and the stream stalls for every viewer. A browser has no
      //    such switch — it also has no window to minimize into the tray.
      void bridge?.setPublishingActive?.(true)

      state.value = 'publishing'
      // File opens are recorded for the whole meeting, not just while AI 听记
      // is on: the record answers "what did we look at", which is exactly what
      // a later summary needs even when nobody recorded audio.
      startMeetingFileTracking({ token: token.value })
      // Key frames share the file timeline but not the audio one: a meeting can
      // keep pictures without a transcript. The source is read at capture time
      // (a member may take the screen at any moment), so only the getters are
      // handed over here.
      if (opts.shots !== false) startShots()
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

  /**
   * Mute / unmute the publisher's microphone for the REST of the meeting.
   *
   * Disabling the track (rather than removing it) keeps every peer connection
   * and the mixer graph intact: no renegotiation, no gap for the other side.
   */
  function setMicEnabled(on: boolean): void {
    micEnabled.value = on
    micStream?.getAudioTracks().forEach((track) => {
      track.enabled = on
    })
  }

  /**
   * Switch the publisher's input device while the meeting runs.
   *
   * Three things hold the microphone: the mixer that feeds every viewer, the
   * 听记 recorder, and the level meter (an analyser tapped off the mixer). All
   * three are rewired here; the peer connections are untouched, so there is no
   * renegotiation and no gap for the audience.
   *
   * Only used while the microphone is ON — the device list is hidden when it is
   * off — so a switch never opens a device the host believes is muted.
   */
  async function switchMicDevice(deviceId: string): Promise<boolean> {
    if (state.value !== 'publishing') return false
    error.value = ''
    const gen = sessionGen
    let next: MediaStream
    try {
      next = await acquireMic({ audio: micConstraints(deviceId) })
    } catch (e) {
      // The old device is still live: acquire before releasing, so a device
      // that refuses to open never leaves the meeting without a microphone.
      appLog.w(TAG, `microphone switch failed: ${describeMicError(e)}`)
      fail(i18n.global.t('liveShare.micSwitchFailed'))
      return false
    }
    if (gen !== sessionGen) {
      // The meeting ended while the device was opening. The old stream is
      // already stopped by resetSession(); this new one is ours to release.
      next.getTracks().forEach((track) => track.stop())
      return false
    }
    micStream?.getAudioTracks().forEach((track) => track.stop())
    micStream = next
    activeMicDevice.value = deviceId
    next.getAudioTracks().forEach((track) => {
      track.enabled = micEnabled.value
    })
    mixer?.setMicStream(next)
    useLiveNotes().setMicStream(next)
    return true
  }

  /**
   * The UI's microphone choice: `''` mutes, `'default'` is the system default,
   * anything else is a device id.
   *
   * One entry point for both halves of the decision, so "pick a device" and
   * "unmute" can never disagree — picking a card means "publish from this
   * device", and the device is only re-opened when it actually changed.
   */
  async function setMicChoice(choice: string): Promise<boolean> {
    if (!choice) {
      setMicEnabled(false)
      return true
    }
    const device = choice === 'default' ? '' : choice
    if (device !== activeMicDevice.value) {
      if (!(await switchMicDevice(device))) return false
    }
    setMicEnabled(true)
    return true
  }

  /**
   * Turn AI 听记 on mid-meeting.
   *
   * The recorder needs the AudioContext and the microphone the share already
   * owns, which is why this lives here rather than in the panel. Starting late
   * loses the earlier audio — the transcript begins at the toggle.
   *
   * Switching it back on after a pause RESUMES the recorder: the notes module
   * keeps the meeting's segments (and re-attaches every member already in the
   * room), so the transcript is not wiped and the members who joined before the
   * pause are transcribed again.
   */
  function startNotes(): boolean {
    if (!audioCtx || !token.value) return false
    if (useLiveNotes().phase.value === 'recording') return true
    // Nothing to record with: report failure so the switch reverts and the
    // stage can say why, instead of showing 听记 as on with an empty transcript.
    if (!notesRecordingSupported()) {
      appLog.w(TAG, 'MediaRecorder unavailable: notes cannot be turned on')
      return false
    }
    useLiveNotes().start({
      token: token.value,
      ctx: audioCtx,
      micStream,
      title: title.value,
      label: notesLabelFn,
      resume: notesStarted,
    })
    notesStarted = true
    publishNotes(true)
    return true
  }

  /** Turn AI 听记 off mid-meeting: stop recording and drain the upload queue.
   *  The audio is NOT merged yet — the meeting may still be running, and a
   *  merge now would leave whatever comes after the toggle as loose segments. */
  async function stopNotes(): Promise<void> {
    const notes = useLiveNotes()
    if (notes.phase.value !== 'recording') return
    await notes.stopCapture()
    publishNotes(false)
    await notes.finishUploads(false)
  }

  /**
   * Tell the audience that AI 听记 was switched on or off.
   *
   * The room's flag used to be write-once at create time, so a viewer that
   * joined mid-meeting read a stale value: a transcript turned on later never
   * appeared, and one turned off kept its pane polling a transcript that would
   * never grow. `enabled` is sent both ways — "off" is the whole point of the
   * frame, so it cannot be an omitted field.
   */
  function publishNotes(on: boolean): void {
    send({ type: 'set-notes', enabled: on })
  }

  /**
   * Start keeping key frames mid-meeting.
   *
   * The capture needs the streams and the ink mirror, which this module owns —
   * the shots composable only receives getters, so it cannot import this file
   * (that would be a cycle). Nothing earlier is recovered: the frames begin at
   * the toggle.
   */
  function startShots(): void {
    if (!token.value || state.value !== 'publishing') return
    useMeetingShots().start({
      token: token.value,
      getSource: () => (screenSharer.value ? remoteScreenStream.value : previewStream.value),
      getStrokes: () => inkMirror.strokes.value,
    })
  }

  /** Stop keeping frames. The queue drains on its own (it carries the token). */
  function stopShots(): void {
    useMeetingShots().stop()
  }

  async function openSocket(): Promise<void> {
    const proto = window.location.protocol === 'https:' ? 'wss' : 'ws'
    const url = `${proto}://${window.location.host}/api/live-share/publish/ws?token=${encodeURIComponent(token.value)}`
    const socket = new WebSocket(url)
    ws = socket

    await new Promise<void>((resolve, reject) => {
      socket.onopen = () => {
        socket.send(JSON.stringify({ type: 'publish', title: title.value, name: hostName.value }))
        // The capability may already have been decided before this socket
        // existed (the panel probes the injector at mount, and a capture can
        // start before the room is open), in which case its `send` went
        // nowhere. The room needs the current value, not just future changes.
        send({ type: 'set-control-enabled', enabled: controlEnabled.value })
        // Same reason as the capability above: 听记 can be switched on before
        // this socket exists (the panel starts recording while the room is
        // still being created), and that `send` went nowhere.
        //
        // The CURRENT state, not "was it ever on": a meeting whose 听记 was
        // switched off again before the socket opened must not tell the room it
        // is recording, or every viewer would poll a transcript that will never
        // grow.
        if (useLiveNotes().phase.value === 'recording') {
          send({ type: 'set-notes', enabled: true })
        }
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
        // The server forwards the nickname it accepted, so the publisher's list
        // and the chat attribution use one authoritative value. The update must
        // come AFTER the join: `updateViewer` only patches existing rows, and
        // the row is appended inside `onViewerJoin`.
        await onViewerJoin(viewerId)
        updateViewer(viewerId, { name: (msg.name as string) ?? '' })
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
        // The only per-viewer state is "microphone on / off" plus the host's
        // mute flag; there is no approval queue to track.
        updateViewer(viewerId, {
          talk: msg.talking ? 'talking' : 'muted',
          // `false` is meaningful here (the block was lifted), so the server
          // sends the field on every talk-state frame; `=== true` only guards
          // against an older server that omits it.
          muted: msg.muted === true,
        })
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

    // Relay mode: the viewer subscribes to LiveKit, so there is nothing to
    // answer. The row still goes in the list — it is what the host's roster and
    // the "who is speaking" chips are built from.
    if (relayMode) {
      viewers.value = [...viewers.value, { id: viewerId, name: '', talk: 'muted', muted: false }]
      return
    }

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
      cameraTx: null,
      mixedTrack,
      attached: false,
      pendingIce: [],
    })
    viewers.value = [...viewers.value, { id: viewerId, name: '', talk: 'muted', muted: false }]

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
   * THREE OR FOUR m-lines, in this exact order (a contract with liveViewer.ts):
   *   1 audio  — our mix down, the viewer's microphone up
   *   2 video  — our main picture down, the viewer's shared screen up
   *   3 video  — another viewer's screen, relayed down
   *   4 video  — our camera, down only (only sent when m2 is a screen)
   *
   * Four is what current viewers offer; three is a page served from an older
   * bundle, which simply gets no camera tile. Refusing three would break a
   * share for anybody with a stale tab, and the main picture works either way.
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
    const known =
      (txs.length === 3 || txs.length === 4) &&
      kinds[0] === 'audio' &&
      kinds[1] === 'video' &&
      kinds[2] === 'video' &&
      (txs.length === 3 || kinds[3] === 'video')
    if (!known) {
      appLog.e(TAG, 'viewer offered an unexpected m-line layout', peer.viewerId, kinds)
      return false
    }
    const [audioTx, videoTx, relayTx, cameraTx] = txs

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

    if (cameraTx) {
      cameraTx.direction = 'sendonly'
      if (outCameraTrack) {
        await cameraTx.sender.replaceTrack(outCameraTrack)
        // Its OWN stream: sharing `outgoing` would make the viewer attach the
        // camera to the main <video> and lose the screen.
        cameraTx.sender.setStreams?.(new MediaStream([outCameraTrack]))
      }
    }

    // Codec preference has to be in place BEFORE createAnswer: the answer is
    // what fixes the codec, and it fixes it for both directions of the m-line —
    // including a guest's screen coming up to us on `videoTx`.
    for (const tx of [videoTx, relayTx, cameraTx]) {
      applyCodecPreference(tx, videoCodecs, preferredVideoMime)
    }

    peer.videoTx = videoTx
    peer.relayTx = relayTx
    peer.cameraTx = cameraTx ?? null

    // Every video m-line this viewer will receive is a separate encoder on this
    // machine. Registering them with the one governor is what lets CPU pressure
    // step all of them down together.
    //
    // The main m-line carries the screen when there is one and the camera
    // otherwise, so its budget has to be chosen HERE: `applyPicture()` corrects
    // it later, but it only runs when the picture CHANGES, and a camera-only
    // meeting never changes. Registering at SCREEN_BUDGET meant such a meeting
    // encoded 4 Mbps/30 fps — about 2.7× the uplink and CPU of the camera
    // budget — for its whole length.
    governor?.register(
      `${peer.viewerId}:video`,
      videoTx.sender,
      displayStream ? SCREEN_BUDGET : CAMERA_BUDGET,
    )
    governor?.register(`${peer.viewerId}:relay`, relayTx.sender, RELAY_BUDGET)
    if (peer.cameraTx) {
      governor?.register(`${peer.viewerId}:camera`, peer.cameraTx.sender, CAMERA_BUDGET)
    }
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
    // A closed PeerConnection's senders still answer getStats() with their last
    // values; leaving them registered would let a departed viewer's encoder
    // keep the governor stepped down for everyone still in the room.
    governor?.unregister(`${viewerId}:video`)
    governor?.unregister(`${viewerId}:relay`)
    governor?.unregister(`${viewerId}:camera`)
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
   * Only the video track changes: the viewers' connections and the room stay
   * up. `replaceTrack` is what makes this invisible — adding a new track would
   * force a renegotiation on every peer.
   *
   * The new capture also carries system audio, so the mixer is rewired to it.
   * A source without an audio track (a single window on some platforms) keeps
   * the previous one: switching the picture must not silently mute the meeting.
   */
  async function switchSource(sourceId: string): Promise<boolean> {
    if (state.value !== 'publishing' || !displayStream) return false
    error.value = ''
    const gen = sessionGen
    const previous = displayStream
    const captured = await captureScreen(sourceId)
    if (gen !== sessionGen) {
      // The meeting ended while the picker was open: the new capture belongs to
      // nobody, and `previous` was already released by resetSession().
      discardLateCapture(captured)
      return false
    }
    if (!captured) {
      fail(i18n.global.t('liveShare.switchSourceFailed'))
      return false
    }
    // The projection being replaced must be released: nothing else holds its
    // handle, and its foreground service would keep capturing forever.
    androidCapture?.stop()
    const next = adoptScreen(captured)
    if (next.getAudioTracks().length === 0) {
      for (const track of previous.getAudioTracks()) next.addTrack(track)
    }
    displayStream = next
    previewStream.value = next
    mixer?.setSystemStream(next)
    await applyPicture()
    next.getVideoTracks()[0]?.addEventListener('ended', () => {
      void stopScreen()
    })
    // Everything the new capture does not carry is ours to release. The
    // stream must be the one resetSession() stops later; keeping the original
    // here would leave the new capture running after the meeting ends (the OS
    // capture indicator stays on).
    for (const track of previous.getTracks()) {
      if (!next.getTracks().includes(track)) track.stop()
    }
    // Same sharer ("host") but a different picture: the timeline watcher cannot
    // see this one, so the key-frame trigger is reported here.
    useMeetingShots().noteSourceChanged('switch')
    return true
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
    // Keep the screenshot pipeline's copy in step with what the audience sees.
    mirrorInk(msg)
    // Relay mode has no per-viewer data channels: the same frames ride
    // LiveKit's data channel, where every participant already is.
    if (relayMode) {
      if (relayRoom) void sendRelayData(relayRoom, msg)
      return
    }
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
      // Relay mode gets its levels from LiveKit (ActiveSpeakersChanged): polling
      // here would fight it, and there are no per-viewer analysers to read.
      if (relayMode) return
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
    // `from` alone is not enough: a chat line carries a display name, and the
    // other viewers render it verbatim — a viewer could label itself "主持人".
    // The roster name the server accepted at join is the only trustworthy one,
    // and it is what the publisher's own chat list already uses.
    const frame: ControlMessage =
      msg.t === 'chat'
        ? {
            t: 'chat',
            id: msg.id,
            from: viewerId,
            name: viewers.value.find((v) => v.id === viewerId)?.name ?? '',
            text: msg.text,
            at: msg.at,
          }
        : tagged
    controlHandlers.forEach((cb) => cb(frame))
    // A viewer's strokes are part of the picture too; mirror them so a frame
    // captured while somebody else is drawing still carries the annotation.
    mirrorInk(frame)
    // A history request is addressed to the publisher alone; relaying it would
    // ask every other viewer to answer a question only this side can answer.
    if (msg.t === 'chat-history-req') return
    const data = JSON.stringify(frame)
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
    // This track is decoded and re-encoded once per remaining viewer, so the
    // hint matters more here than on our own capture, not less.
    applyContentHint(track, 'text')
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

  /**
   * Block or unblock one viewer's microphone.
   *
   * Different from revokeTalk: that one silences the microphone for the moment,
   * this one refuses to let it come back on until the host lifts it. The list
   * writes the new state optimistically so the button flips immediately; the
   * server's talk-state frame then confirms it.
   */
  function setViewerMuted(viewerId: string, muted: boolean): void {
    send({ type: 'talk-mute', viewerId, enabled: muted })
    updateViewer(viewerId, muted ? { muted: true, talk: 'muted' } : { muted: false })
  }

  /** Remove one viewer from the meeting. The server hangs up its socket. */
  function kickViewer(viewerId: string): void {
    send({ type: 'kick', viewerId })
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
    // ...and the same for 听记: whether THIS share ever used it. The notes module
    // is a singleton, so a meeting without notes must not fold and summarize the
    // previous meeting's recording when it ends.
    const usedNotes = notesStarted

    // Stop the notes recorders and wait for their last blobs BEFORE the audio
    // context is closed: MediaRecorder produces the final chunk in `onstop`,
    // so closing first would silently drop the last few seconds of the meeting.
    const notes = useLiveNotes()
    const hadNotes = notes.phase.value !== 'idle'
    if (hadNotes) await notes.stopCapture()

    // Close the file timeline BEFORE the socket goes away: the server stamps
    // every still-open interval when the publisher detaches, and an interval we
    // are still holding would be stored with that moment as its end.
    await stopMeetingFileTracking()
    // Same for the screen: "still sharing at the end" must be closed by us,
    // while the token and the publishing state are still set.
    await reportScreenSharer('')
    // Stop grabbing before resetSession() nulls the streams the getter reads.
    useMeetingShots().stop()

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
    //
    // Always the same call, whether the recorder is still running or was
    // switched off earlier: `finishUploads()` with no live capture folds the
    // recording and generates the minutes from EVERY segment, which is exactly
    // what a paused recorder needs. Generating them at the toggle instead (the
    // old behaviour) summarized the first half and never ran again.
    if (usedNotes) void notes.finishUploads()

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

  // A meeting is only ever started from this composable, so this is the first
  // moment F5 can need guarding; the installer is idempotent across instances.
  installF5Guard(stop)

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
    setViewerMuted,
    kickViewer,
    muteAll,
    setScreenEnabled,
    revokeScreen,
    sendControl,
    sendControlTo,
    onControl,
    levels,
    switchSource,
    previewStream,
    previewCameraStream,
    captureKind,
    captureSourceId,
    controlSupported,
    controlEnabled,
    setControlSupported,
    videoEnabled,
    cameraEnabled,
    micEnabled,
    setMicEnabled,
    switchMicDevice,
    setMicChoice,
    activeMicDevice,
    setCameraDevice,
    startScreen,
    stopScreen,
    startNotes,
    stopNotes,
    startShots,
    stopShots,
    clearInkMirror,
  }
}

/** Test seam: the strokes the key-frame capture would composite. */
export function _inkMirrorForTest(): InkStroke[] {
  return inkMirror.strokes.value
}

/** Test seam: drop all module state without touching the network. */
export function _resetLiveShareForTest(): void {
  resetSession()
  _resetMeetingShotsForTest()
  controlHandlers.clear()
  useLiveNotes().reset()
  _resetMeetingFileTrackerForTest()
  state.value = 'idle'
  token.value = ''
  shareUrl.value = ''
  viewers.value = []
  talkEnabled.value = false
  screenEnabled.value = false
  controlSupported.value = false
  controlEnabled.value = false
  error.value = ''
}

/**
 * The session's encode governor, for tests.
 *
 * Its poll timer runs every 3 s, which a unit test must not wait for; exposing
 * the instance lets a test call `sample()` directly and assert what the
 * publisher actually pushed onto its senders.
 */
export function _liveGovernorForTest(): VideoLoadGovernor | null {
  return governor
}
