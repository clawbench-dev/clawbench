import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'

/**
 * Publisher-side live share: room creation, capture wiring, per-viewer peer
 * management and the talk-authorisation relay.
 *
 * The interesting failures here are all "silently wrong" rather than "throws":
 * a viewer that never gets a destination, a removed viewer whose audio keeps
 * flowing, a stop() that leaves the camera light on. The fakes below let each be
 * asserted directly.
 */

const bridge = vi.hoisted(() => ({
  listCaptureSources: vi.fn(async () => '[]'),
  setPendingCaptureSource: vi.fn(async () => undefined),
  getAudioCaptureSupport: vi.fn(async () => JSON.stringify({ systemAudio: false, reason: 'test' })),
  setPublishingActive: vi.fn(async () => undefined),
  remoteControlGrant: vi.fn(async () => undefined),
  remoteControlRevoke: vi.fn(async () => undefined),
  // Absent on the desktop shell; the Android host is the only one that has it,
  // so tests opt in by assigning a function.
  startScreenCapture: undefined as undefined | (() => string),
}))

vi.mock('@/utils/clawbenchNative', () => ({
  getNative: () => bridge,
}))

const androidCapture = vi.hoisted(() => ({
  startAndroidScreenCapture: vi.fn(),
}))

vi.mock('@/composables/androidScreenCapture', () => ({
  startAndroidScreenCapture: androidCapture.startAndroidScreenCapture,
}))

class FakeTrack {
  kind: string
  id: string
  stopped = false
  /** What the browser reports about the surface it captured (displaySurface). */
  settings: Record<string, unknown> = {}
  private endedHandlers: Array<() => void> = []
  constructor(kind: string, id: string) {
    this.kind = kind
    this.id = id
  }
  stop() {
    this.stopped = true
  }
  getSettings() {
    return this.settings
  }
  addEventListener(ev: string, fn: () => void) {
    if (ev === 'ended') this.endedHandlers.push(fn)
  }
  /** Simulate the user ending capture from the browser's own banner. */
  emitEnded() {
    this.endedHandlers.forEach((f) => f())
  }
}

class FakeStream {
  tracks: FakeTrack[]
  constructor(tracks: FakeTrack[]) {
    this.tracks = tracks
  }
  getTracks() {
    return this.tracks
  }
  getVideoTracks() {
    return this.tracks.filter((t) => t.kind === 'video')
  }
  getAudioTracks() {
    return this.tracks.filter((t) => t.kind === 'audio')
  }
  addTrack(track: FakeTrack) {
    if (!this.tracks.includes(track)) this.tracks.push(track)
  }
}

class FakeSender {
  track: FakeTrack | null = null
  streams: unknown[] = []
  replaceTrack = vi.fn(async (t: FakeTrack | null) => {
    this.track = t
  })
  setStreams = vi.fn((...s: unknown[]) => {
    this.streams = s
  })
}

class FakeTransceiver {
  sender = new FakeSender()
  receiver = { track: null as FakeTrack | null }
  constructor(public direction: string) {}
}

class FakeDataChannel {
  static instances: FakeDataChannel[] = []
  readyState = 'open'
  sent: string[] = []
  onmessage: ((ev: { data: string }) => void) | null = null
  constructor(public label: string) {
    FakeDataChannel.instances.push(this)
  }
  send(data: string) {
    this.sent.push(data)
  }
  /** Push a frame as if the remote peer sent it. */
  emit(msg: unknown) {
    this.onmessage?.({ data: JSON.stringify(msg) })
  }
  sentJSON(): Record<string, unknown>[] {
    return this.sent.map((x) => JSON.parse(x) as Record<string, unknown>)
  }
}

class FakePC {
  static instances: FakePC[] = []
  ontrack: ((ev: { track: FakeTrack; streams: FakeStream[] }) => void) | null = null
  ondatachannel: ((ev: { channel: FakeDataChannel }) => void) | null = null
  onicecandidate: ((ev: { candidate: { toJSON: () => unknown } | null }) => void) | null = null
  onconnectionstatechange: (() => void) | null = null
  connectionState = 'new'
  transceivers: FakeTransceiver[] = []
  tracks: FakeTrack[] = []
  remoteDescriptions: unknown[] = []
  remoteDescription: unknown = null
  localDescription: unknown = null
  addedCandidates: unknown[] = []
  closed = false

  constructor(public config: unknown) {
    FakePC.instances.push(this)
  }
  addTrack(t: FakeTrack, stream?: unknown) {
    this.tracks.push(t)
    this.addedStreams.push(stream)
  }
  addedStreams: unknown[] = []
  addTransceiver(kindOrTrack: string | FakeTrack, init?: { direction?: string; streams?: unknown[] }) {
    const tx = new FakeTransceiver(init?.direction ?? 'sendrecv')
    if (typeof kindOrTrack !== 'string') {
      // addTransceiver(track, {streams}) is what the publisher uses for the
      // audio and video m-lines; record it the same way addTrack did.
      this.tracks.push(kindOrTrack)
      tx.sender.track = kindOrTrack
      for (const st of init?.streams ?? []) this.addedStreams.push(st)
    }
    this.transceivers.push(tx)
    return tx
  }
  channelsDelivered = false
  /** The publisher must NEVER create a channel: an answer cannot add an m-line. */
  createDataChannel = vi.fn((label: string) => new FakeDataChannel(label) as unknown as RTCDataChannel)
  getTransceivers() {
    return this.transceivers
  }
  createAnswer = vi.fn(async () => ({ type: 'answer', sdp: 'v=0-answer' }))
  setLocalDescription = vi.fn(async (d: unknown) => {
    this.localDescription = d
  })
  setRemoteDescription = vi.fn(async (d: unknown) => {
    this.remoteDescriptions.push(d)
    this.remoteDescription = d
    // Chrome creates one transceiver per offered m-line when the offer is
    // applied, and only THOSE end up in the answer with a send direction. A
    // transceiver created by addTransceiver() beforehand keeps `mid === null`
    // and the browser answers with a second, recvonly set instead — the bug
    // this fake has to be able to reproduce.
    if ((d as { type?: string } | null)?.type === 'offer') {
      if (this.transceivers.length === 0) {
        const kinds = offerCameraLine ? ['audio', 'video', 'video', 'video'] : ['audio', 'video', 'video']
        for (const kind of kinds) {
          const tx = new FakeTransceiver('recvonly')
          tx.receiver.track = new FakeTrack(kind, `rx-${kind}`)
          this.transceivers.push(tx)
        }
      }
      // The viewer's offer carries the control bus, so applying it is what
      // delivers the channel. The publisher only adopts it.
      if (!this.channelsDelivered) {
        this.channelsDelivered = true
        this.ondatachannel?.({ channel: new FakeDataChannel('cb-live') })
      }
    }
  })
  addIceCandidate = vi.fn(async (c: unknown) => {
    this.addedCandidates.push(c)
  })
  close = vi.fn(() => {
    this.closed = true
  })
}

class FakeWS {
  static instances: FakeWS[] = []
  readyState = 0
  sent: string[] = []
  onopen: (() => void) | null = null
  onmessage: ((ev: { data: unknown }) => void) | null = null
  onerror: (() => void) | null = null
  onclose: (() => void) | null = null

  constructor(public url: string) {
    FakeWS.instances.push(this)
    // Open on the next microtask, mirroring a real socket.
    void Promise.resolve().then(() => {
      this.readyState = 1
      this.onopen?.()
    })
  }
  send(d: string) {
    this.sent.push(d)
  }
  close() {
    this.readyState = 3
  }
  emit(msg: Record<string, unknown>) {
    this.onmessage?.({ data: JSON.stringify(msg) })
  }
  sentJSON() {
    return this.sent.map((s) => JSON.parse(s) as Record<string, unknown>)
  }
}

class FakeDest {
  stream: FakeStream
  constructor(id: string) {
    this.stream = new FakeStream([new FakeTrack('audio', `mix-${id}`)])
  }
  connect() {}
  disconnect() {}
}

/** Level the fake analyser reports; 0 = silence, 1 = full scale. */
let fakeMicLevel = 0

class FakeAnalyser {
  fftSize = 256
  connect() {}
  disconnect() {}
  getByteTimeDomainData(buf: Uint8Array) {
    // A constant waveform whose RMS equals the requested level.
    buf.fill(128 + Math.round(Math.min(fakeMicLevel, 0.9) * 128))
  }
}

class FakeAudioCtx {
  state = 'running'
  dests: FakeDest[] = []
  resume = vi.fn(async () => undefined)
  close = vi.fn(async () => undefined)
  createMediaStreamDestination() {
    const d = new FakeDest(String(this.dests.length))
    this.dests.push(d)
    return d
  }
  createMediaStreamSource() {
    return { connect: () => undefined, disconnect: () => undefined }
  }
  createAnalyser() {
    return new FakeAnalyser()
  }
}

/**
 * Fresh streams for every test.
 *
 * These used to be module-level singletons, which quietly made "stop() stops
 * the tracks" unfalsifiable: once any earlier test had stopped them the flag
 * stayed true for the rest of the file, so the assertion passed even if stop()
 * did nothing.
 */
let displayStream: FakeStream
let micStream: FakeStream
let cameraStream: FakeStream
/**
 * Whether the simulated viewer offers the fourth (camera) m-line.
 *
 * A page served from an older bundle offers three; the publisher has to keep
 * answering it. Default false so every pre-existing test keeps asserting that
 * path.
 */
let offerCameraLine = false

/** jsdom's MediaStream rejects plain objects; the code only wraps tracks. */
class FakeMediaStream {
  tracks: FakeTrack[]
  constructor(tracks: FakeTrack[] = []) {
    this.tracks = tracks
  }
  addTrack(t: FakeTrack) {
    this.tracks.push(t)
  }
  getTracks() {
    return this.tracks
  }
  getAudioTracks() {
    return this.tracks.filter((t) => t.kind === 'audio')
  }
  getVideoTracks() {
    return this.tracks.filter((t) => t.kind === 'video')
  }
}

function installGlobals() {
  FakePC.instances = []
  FakeWS.instances = []
  FakeDataChannel.instances = []
  vi.stubGlobal('WebSocket', FakeWS as unknown as typeof WebSocket)
  vi.stubGlobal('RTCPeerConnection', FakePC as unknown as typeof RTCPeerConnection)
  vi.stubGlobal('AudioContext', FakeAudioCtx as unknown as typeof AudioContext)
  fakeMicLevel = 0
  // `new MediaStream([track])` runs on every viewer join; without this stub it
  // throws and silently aborts the rest of that handler (no ICE, no state hook).
  vi.stubGlobal('MediaStream', FakeMediaStream as unknown as typeof MediaStream)
  displayStream = new FakeStream([new FakeTrack('video', 'v1'), new FakeTrack('audio', 'sys1')])
  micStream = new FakeStream([new FakeTrack('audio', 'mic1')])
  cameraStream = new FakeStream([new FakeTrack('video', 'cam1')])
  offerCameraLine = false
  vi.stubGlobal('navigator', {
    mediaDevices: {
      getDisplayMedia: vi.fn(async () => displayStream),
      getUserMedia: vi.fn(async (constraints?: { video?: unknown }) =>
        constraints?.video ? cameraStream : micStream,
      ),
      enumerateDevices: vi.fn(async () => [{ kind: 'videoinput', deviceId: 'cam1' }]),
    },
  })
}

let fetchMock: ReturnType<typeof vi.fn>

beforeEach(async () => {
  installGlobals()
  fetchMock = vi.fn(async (url: string, init?: { method?: string }) => {
    if (url === '/api/live-share' && init?.method === 'POST') {
      return { ok: true, json: async () => ({ token: 'tok-1', path: '/share/tok-1' }) }
    }
    return { ok: true, json: async () => ({}) }
  })
  vi.stubGlobal('fetch', fetchMock)
  bridge.setPendingCaptureSource.mockClear()
  bridge.setPublishingActive.mockClear()
  bridge.remoteControlRevoke.mockClear()
  bridge.remoteControlGrant.mockClear()
  bridge.startScreenCapture = undefined
  androidCapture.startAndroidScreenCapture.mockReset()

  const mod = await import('../useLiveShare')
  mod._resetLiveShareForTest()
})

afterEach(async () => {
  // Tear the module down too, not just the globals: a test that starts a share
  // without stopping it leaves the level-metering interval running, which
  // vitest reports as an async leak at the end of the file.
  const mod = await import('../useLiveShare')
  mod._resetLiveShareForTest()
  vi.unstubAllGlobals()
  vi.restoreAllMocks()
})

async function startSharing(opts: { micDeviceId?: string } = {}) {
  const { useLiveShare } = await import('../useLiveShare')
  const share = useLiveShare()
  const ok = await share.start({ sourceId: 'screen:1:0', microphone: true, ...opts })
  return { share, ok }
}

function lastWS(): FakeWS {
  return FakeWS.instances[FakeWS.instances.length - 1]
}

/**
 * Attach a viewer and drive its offer.
 *
 * The publisher ANSWERS, so the m-lines only exist once the offer is applied —
 * a viewer that never offers has no transceivers and no control channel.
 */
async function joinAndOffer(viewerId: string, name?: string): Promise<FakePC> {
  lastWS().emit({ type: 'viewer-join', viewerId, ...(name ? { name } : {}) })
  lastWS().emit({ type: 'offer', viewerId, sdp: `v=0-offer-${viewerId}` })
  await vi.waitFor(() =>
    expect(lastWS().sentJSON().filter((m) => m.type === 'answer' && m.viewerId === viewerId)).toHaveLength(1),
  )
  return FakePC.instances[FakePC.instances.length - 1]
}

describe('start', () => {
  it('creates a room, declares the source and opens the publisher socket', async () => {
    const { share, ok } = await startSharing()

    expect(ok).toBe(true)
    expect(share.state.value).toBe('publishing')
    expect(share.token.value).toBe('tok-1')
    expect(share.shareUrl.value).toContain('/share/tok-1')

    // The declaration must happen BEFORE getDisplayMedia, or the host has no
    // source to answer with and the capture is refused.
    expect(bridge.setPendingCaptureSource).toHaveBeenCalledWith('screen:1:0', true)
    expect(lastWS().url).toContain('/api/live-share/publish/ws?token=tok-1')
  })

  it('sends the publish frame on open', async () => {
    await startSharing()
    await vi.waitFor(() => expect(lastWS().sentJSON().some((m) => m.type === 'publish')).toBe(true))
  })

  it('keeps the renderer unthrottled while publishing', async () => {
    await startSharing()
    // A minimized window throttles timers, which stalls the encoder.
    expect(bridge.setPublishingActive).toHaveBeenCalledWith(true)
  })

  it('captures through MediaProjection when the host offers it (Android)', async () => {
    const stopAndroid = vi.fn()
    const androidStream = new FakeStream([new FakeTrack('video', 'android-1')])
    androidCapture.startAndroidScreenCapture.mockResolvedValue({
      stream: androidStream,
      width: 1080,
      height: 2400,
      systemAudio: false,
      stop: stopAndroid,
    })
    bridge.startScreenCapture = vi.fn(() => '{"state":"requesting"}')

    const { share, ok } = await startSharing()

    expect(ok).toBe(true)
    expect(androidCapture.startAndroidScreenCapture).toHaveBeenCalledTimes(1)
    // Android has no getDisplayMedia, and there is no source to declare either:
    // touching either path would fail the capture on a real device. (The reset
    // helper clears the declaration with '', so only a real id would count.)
    expect(navigator.mediaDevices.getDisplayMedia).not.toHaveBeenCalled()
    expect(bridge.setPendingCaptureSource).not.toHaveBeenCalledWith('screen:1:0', expect.anything())
    expect(share.captureKind.value).toBe('screen')
    expect(share.previewStream.value).toBe(androidStream)

    await share.stop()
    // Stopping the tracks alone would leave the native foreground service
    // capturing with nobody reading it.
    expect(stopAndroid).toHaveBeenCalled()
  })

  it('captures before it asks the server for a room', async () => {
    // getDisplayMedia needs a transient user activation, which a slow round
    // trip can outlive — the browser then refuses a request the user did make.
    // Reversing these two steps is a real regression on Firefox and Safari, not
    // a style choice, so the order itself is what gets asserted.
    const order: string[] = []
    const gdm = (navigator as unknown as { mediaDevices: { getDisplayMedia: ReturnType<typeof vi.fn> } })
      .mediaDevices.getDisplayMedia
    gdm.mockImplementationOnce(async () => {
      order.push('capture')
      return displayStream
    })
    fetchMock.mockImplementation(async (url: string, init?: { method?: string }) => {
      if (url === '/api/live-share' && init?.method === 'POST') order.push('room')
      return { ok: true, json: async () => ({ token: 'tok-1', path: '/share/tok-1' }) }
    })

    const { ok } = await startSharing()

    expect(ok).toBe(true)
    expect(order).toEqual(['capture', 'room'])
  })

  it('releases the capture when the room cannot be created', async () => {
    fetchMock.mockResolvedValueOnce({ ok: false, status: 503, json: async () => ({}) })
    const { share, ok } = await startSharing()

    expect(ok).toBe(false)
    expect(share.state.value).toBe('idle')
    // The picture is already live by the time the room request fails. Leaving
    // it on would keep the browser's "sharing" indicator lit with nowhere to
    // publish, and the mic would stay open.
    expect(displayStream.tracks.every((t) => t.stopped)).toBe(true)
    expect(micStream.tracks.every((t) => t.stopped)).toBe(true)
    expect(FakeWS.instances).toHaveLength(0)
  })

  it('captures through the browser picker when the host has no capture bridge', async () => {
    // A plain web page: no IPC at all. The browser's own picker replaces both
    // the source list and the declaration step.
    const native = await import('@/utils/clawbenchNative')
    const spy = vi.spyOn(native, 'getNative').mockReturnValue({} as never)
    const track = new FakeTrack('video', 'browser-1')
    track.settings = { displaySurface: 'monitor' }
    const stream = new FakeStream([track, new FakeTrack('audio', 'sys-1')])
    const gdm = (navigator as unknown as { mediaDevices: { getDisplayMedia: ReturnType<typeof vi.fn> } })
      .mediaDevices.getDisplayMedia
    gdm.mockResolvedValueOnce(stream)

    const { share, ok } = await startSharing()

    expect(ok).toBe(true)
    expect(share.state.value).toBe('publishing')
    expect(gdm).toHaveBeenCalledTimes(1)
    expect(bridge.setPendingCaptureSource).not.toHaveBeenCalledWith('screen:1:0', expect.anything())
    // The source id is meaningless here; the captured surface is what decides
    // whether remote control could ever map the pointer onto the desktop.
    expect(share.captureKind.value).toBe('screen')
    spy.mockRestore()
  })

  it('fails when the page has no getDisplayMedia (insecure origin or mobile browser)', async () => {
    const native = await import('@/utils/clawbenchNative')
    const spy = vi.spyOn(native, 'getNative').mockReturnValue({} as never)
    // `mediaDevices` exists for the microphone on Android Chrome, but screen
    // capture was never implemented there — the method is the real gate.
    vi.stubGlobal('navigator', { mediaDevices: { getUserMedia: vi.fn() } })

    const { share, ok } = await startSharing()

    expect(ok).toBe(false)
    expect(share.state.value).toBe('idle')
    expect(share.error.value).toContain('screen capture is unavailable')
    spy.mockRestore()
  })

  it('continues without the microphone when it is denied', async () => {
    vi.stubGlobal('navigator', {
      mediaDevices: {
        getDisplayMedia: vi.fn(async () => displayStream),
        getUserMedia: vi.fn(async () => {
          throw new Error('NotAllowedError')
        }),
      },
    })

    const { share, ok } = await startSharing()

    // A denied mic must not abort the share: the screen is the point.
    expect(ok).toBe(true)
    expect(share.state.value).toBe('publishing')
  })

  it('is a no-op when already publishing', async () => {
    const { share } = await startSharing()
    const again = await share.start({ sourceId: 'x', microphone: false })

    expect(again).toBe(false)
    expect(FakeWS.instances).toHaveLength(1)
  })

  // A machine with a headset and a built-in mic: the host picks one, and that
  // choice has to reach the capture constraint or the picker is decorative.
  it('captures the microphone the host chose', async () => {
    const { share, ok } = await startSharing({ micDeviceId: 'mic-b' })

    expect(ok).toBe(true)
    const gum = navigator.mediaDevices.getUserMedia as unknown as ReturnType<typeof vi.fn>
    const audio = gum.mock.calls.find((c) => !c[0].video)?.[0].audio
    expect(audio).toMatchObject({ deviceId: { exact: 'mic-b' } })
    expect(share.state.value).toBe('publishing')
  })

  it('leaves the input to the system default when none was chosen', async () => {
    await startSharing()

    const gum = navigator.mediaDevices.getUserMedia as unknown as ReturnType<typeof vi.fn>
    const audio = gum.mock.calls.find((c) => !c[0].video)?.[0].audio
    // No `deviceId` at all — `{exact: ''}` would fail on every machine.
    expect(audio).not.toHaveProperty('deviceId')
  })

  // Putting a headset on mid-call: the audience must not hear a gap, and the
  // device the host stopped using must be released (the OS indicator stays lit
  // otherwise).
  it('moves the capture to another input device mid-meeting', async () => {
    const { share, ok } = await startSharing()
    expect(ok).toBe(true)
    const oldTrack = micStream.getAudioTracks()[0] as unknown as FakeTrack

    const nextStream = new FakeStream([new FakeTrack('audio', 'mic2')])
    const gum = navigator.mediaDevices.getUserMedia as unknown as ReturnType<typeof vi.fn>
    gum.mockImplementationOnce(async () => nextStream)

    const switched = await share.switchMicDevice('mic-b')

    expect(switched).toBe(true)
    expect(oldTrack.stopped).toBe(true)
    const audio = gum.mock.calls[gum.mock.calls.length - 1][0].audio
    expect(audio).toMatchObject({ deviceId: { exact: 'mic-b' } })
    expect(share.state.value).toBe('publishing')
  })

  it('keeps the running microphone when the new device cannot be opened', async () => {
    const { share } = await startSharing()
    const oldTrack = micStream.getAudioTracks()[0] as unknown as FakeTrack
    const gum = navigator.mediaDevices.getUserMedia as unknown as ReturnType<typeof vi.fn>
    gum.mockRejectedValueOnce(new Error('NotFoundError'))

    const switched = await share.switchMicDevice('unplugged')

    // Acquire before release: a device that refuses to open must never leave
    // the meeting without the microphone it already had.
    expect(switched).toBe(false)
    expect(oldTrack.stopped).toBe(false)
    expect(share.error.value).toBeTruthy()
    expect(share.state.value).toBe('publishing')
  })
})

describe('viewers', () => {
  // The server accepts the nickname and forwards it; the publisher's audience
  // list and the persisted chat both use it. It must be written AFTER the row
  // exists — `updateViewer` only patches existing rows.
  it('keeps the nickname the server accepted for the viewer', async () => {
    const { share } = await startSharing()

    lastWS().emit({ type: 'viewer-join', viewerId: 'v1', name: 'Alex' })
    await vi.waitFor(() => expect(share.viewers.value).toHaveLength(1))

    expect(share.viewers.value[0].name).toBe('Alex')
  })

  it('builds one PeerConnection per viewer and wires its m-lines on the offer', async () => {
    const { share } = await startSharing()

    lastWS().emit({ type: 'viewer-join', viewerId: 'v1' })
    await vi.waitFor(() => expect(FakePC.instances).toHaveLength(1))

    const pc = FakePC.instances[0]
    // Nothing is reserved before the offer. A transceiver created by
    // addTransceiver() this early keeps `mid === null` and the browser answers
    // the offered m-lines with a second, recvonly set instead — which is what
    // left every viewer stuck on "协商中".
    expect(pc.transceivers).toHaveLength(0)
    expect(share.viewers.value).toHaveLength(1)

    lastWS().emit({ type: 'offer', viewerId: 'v1', sdp: 'v=0-offer' })
    await vi.waitFor(() => expect(lastWS().sentJSON().some((m) => m.type === 'answer')).toBe(true))

    // m-line order is a contract with the viewer: audio, then video, then the
    // relay m-line. The audio track comes first because it is the viewer's
    // microphone slot.
    expect(pc.transceivers.map((t) => t.direction)).toEqual(['sendrecv', 'sendrecv', 'sendonly'])
    expect(pc.transceivers[0].sender.track?.kind).toBe('audio')
    expect(pc.transceivers[1].sender.track?.kind).toBe('video')
    // CRITICAL: both tracks must belong to the SAME MediaStream. The viewer
    // assigns `srcObject` per received stream, so two streams would mean the
    // second assignment replaces the first — the viewer would get either video
    // or audio, never both.
    const audioStreams = pc.transceivers[0].sender.streams
    const videoStreams = pc.transceivers[1].sender.streams
    expect(audioStreams).toHaveLength(1)
    expect(audioStreams[0]).toBe(videoStreams[0])
    expect((audioStreams[0] as unknown as FakeMediaStream).getTracks().map((t) => t.kind)).toEqual([
      'video',
      'audio',
    ])
  })

  it('answers every offered m-line with a direction that actually sends', async () => {
    const { share } = await startSharing()
    lastWS().emit({ type: 'viewer-join', viewerId: 'v1' })
    await vi.waitFor(() => expect(FakePC.instances).toHaveLength(1))
    lastWS().emit({ type: 'offer', viewerId: 'v1', sdp: 'v=0-offer' })
    await vi.waitFor(() => expect(lastWS().sentJSON().some((m) => m.type === 'answer')).toBe(true))

    const pc = FakePC.instances[0]
    // The answer is built from the transceivers the offer created: all three
    // must be reused (not duplicated) and carry our media outwards.
    expect(pc.transceivers).toHaveLength(3)
    expect(pc.transceivers.map((t) => t.direction)).toEqual(['sendrecv', 'sendrecv', 'sendonly'])
    // The control bus rides the same answer, so it has to exist before it.
    expect(FakeDataChannel.instances).toHaveLength(1)
    expect(pc.remoteDescription).toEqual({ type: 'offer', sdp: 'v=0-offer' })
  })

  it('holds a viewer candidate until its offer has been applied', async () => {
    const { share } = await startSharing()
    lastWS().emit({ type: 'viewer-join', viewerId: 'v1' })
    await vi.waitFor(() => expect(FakePC.instances).toHaveLength(1))

    // Trickled right after the offer, so it routinely overtakes it. Applying it
    // before a remote description exists throws, and a dropped candidate leaves
    // ICE with nowhere to send.
    lastWS().emit({ type: 'ice', viewerId: 'v1', candidate: { candidate: 'early' } })
    expect(FakePC.instances[0].addedCandidates).toHaveLength(0)

    lastWS().emit({ type: 'offer', viewerId: 'v1', sdp: 'v=0-offer' })
    await vi.waitFor(() =>
      expect(FakePC.instances[0].addedCandidates).toContainEqual({ candidate: 'early' }),
    )
    expect(share.viewers.value).toHaveLength(1)
  })

  it("adopts the viewer's control channel instead of creating one", async () => {
    const { share } = await startSharing()
    const pc = await joinAndOffer('v1')

    // An answer may not introduce an m-line, so a channel created here would
    // stay `connecting` forever — chat and "request control" would look dead.
    expect(pc.createDataChannel).not.toHaveBeenCalled()
    expect(FakeDataChannel.instances).toHaveLength(1)

    const seen: Array<Record<string, unknown>> = []
    share.onControl((m) => seen.push(m as unknown as Record<string, unknown>))
    FakeDataChannel.instances[0].emit({ t: 'ctrl-req' })
    expect(seen).toEqual([{ t: 'ctrl-req', from: 'v1' }])
  })

  it('answers an offer and forwards ICE both ways', async () => {
    const { share } = await startSharing()
    lastWS().emit({ type: 'viewer-join', viewerId: 'v1' })
    await vi.waitFor(() => expect(FakePC.instances).toHaveLength(1))

    lastWS().emit({ type: 'offer', viewerId: 'v1', sdp: 'v=0-offer' })
    await vi.waitFor(() => expect(lastWS().sentJSON().some((m) => m.type === 'answer')).toBe(true))

    const answer = lastWS().sentJSON().find((m) => m.type === 'answer')!
    expect(answer).toMatchObject({ viewerId: 'v1', sdp: 'v=0-answer' })

    FakePC.instances[0].onicecandidate?.({ candidate: { toJSON: () => ({ candidate: 'c1' }) } })
    expect(lastWS().sentJSON()).toContainEqual({ type: 'ice', viewerId: 'v1', candidate: { candidate: 'c1' } })

    lastWS().emit({ type: 'ice', viewerId: 'v1', candidate: { candidate: 'remote' } })
    await vi.waitFor(() =>
      expect(FakePC.instances[0].addedCandidates).toContainEqual({ candidate: 'remote' }),
    )
    expect(share.viewers.value).toHaveLength(1)
  })

  it('drops a viewer on viewer-leave', async () => {
    const { share } = await startSharing()
    lastWS().emit({ type: 'viewer-join', viewerId: 'v1' })
    await vi.waitFor(() => expect(FakePC.instances).toHaveLength(1))

    lastWS().emit({ type: 'viewer-leave', viewerId: 'v1' })

    expect(FakePC.instances[0].closed).toBe(true)
    expect(share.viewers.value).toHaveLength(0)
  })

  it('drops a viewer whose connection failed', async () => {
    const { share } = await startSharing()
    lastWS().emit({ type: 'viewer-join', viewerId: 'v1' })
    await vi.waitFor(() => expect(FakePC.instances).toHaveLength(1))

    const pc = FakePC.instances[0]
    pc.connectionState = 'failed'
    pc.onconnectionstatechange?.()

    expect(share.viewers.value).toHaveLength(0)
  })

  it('ignores a duplicate viewer-join', async () => {
    const { share } = await startSharing()

    lastWS().emit({ type: 'viewer-join', viewerId: 'v1' })
    lastWS().emit({ type: 'viewer-join', viewerId: 'v1' })
    await vi.waitFor(() => expect(FakePC.instances).toHaveLength(1))

    // A second PeerConnection would double the viewer's video and audio.
    expect(FakePC.instances).toHaveLength(1)
    expect(share.viewers.value).toHaveLength(1)
  })
})

describe('talk authorisation', () => {
  it('relays the room-level switch', async () => {
    const { share } = await startSharing()

    share.setTalkEnabled(true)

    expect(share.talkEnabled.value).toBe(true)
    expect(lastWS().sentJSON()).toContainEqual({ type: 'set-talk-enabled', enabled: true })
  })

  it('only needs revoke — there is no approval queue', async () => {
    const { share } = await startSharing()

    share.revokeTalk('v1')

    const sent = lastWS().sentJSON()
    expect(sent).toContainEqual({ type: 'talk-revoke', viewerId: 'v1' })
    // The grant verb no longer exists: the room switch is the authorisation.
    expect(sent.some((m) => m.type === 'talk-grant')).toBe(false)
  })

  it('starts every viewer muted', async () => {
    const { share } = await startSharing()

    lastWS().emit({ type: 'viewer-join', viewerId: 'v1' })
    await vi.waitFor(() => expect(share.viewers.value).toHaveLength(1))

    expect(share.viewers.value[0].talk).toBe('muted')
  })

  it('tracks speaking state from the server', async () => {
    const { share } = await startSharing()
    lastWS().emit({ type: 'viewer-join', viewerId: 'v1' })
    await vi.waitFor(() => expect(share.viewers.value).toHaveLength(1))

    lastWS().emit({ type: 'talk-state', viewerId: 'v1', talking: true })
    expect(share.viewers.value[0].talk).toBe('talking')

    lastWS().emit({ type: 'talk-state', viewerId: 'v1', talking: false })
    expect(share.viewers.value[0].talk).toBe('muted')
  })

  it('mute-all clears every viewer locally and notifies the server', async () => {
    const { share } = await startSharing()
    lastWS().emit({ type: 'viewer-join', viewerId: 'v1' })
    lastWS().emit({ type: 'viewer-join', viewerId: 'v2' })
    await vi.waitFor(() => expect(share.viewers.value).toHaveLength(2))
    lastWS().emit({ type: 'talk-state', viewerId: 'v1', talking: true })

    share.muteAll()

    expect(lastWS().sentJSON()).toContainEqual({ type: 'talk-mute-all' })
    expect(share.viewers.value.every((v) => v.talk === 'muted')).toBe(true)
  })
})

describe('stop', () => {
  it('ends the room, closes peers and restores throttling', async () => {
    const { share } = await startSharing()
    lastWS().emit({ type: 'viewer-join', viewerId: 'v1' })
    await vi.waitFor(() => expect(FakePC.instances).toHaveLength(1))

    await share.stop()

    expect(lastWS().sentJSON().some((m) => m.type === 'end')).toBe(true)
    expect(FakePC.instances[0].closed).toBe(true)
    expect(share.state.value).toBe('idle')
    expect(share.viewers.value).toHaveLength(0)
    // Throttling must be restored, or the app stays unthrottled forever.
    expect(bridge.setPublishingActive).toHaveBeenLastCalledWith(false)
    expect(fetchMock).toHaveBeenCalledWith(expect.stringContaining('/api/live-share?token=tok-1'), expect.objectContaining({ method: 'DELETE' }))
  })

  it('stops the capture tracks so the screen-sharing indicator goes away', async () => {
    await startSharing()

    const { useLiveShare } = await import('../useLiveShare')
    await useLiveShare().stop()

    expect(displayStream.tracks.every((t) => t.stopped)).toBe(true)
    expect(micStream.tracks.every((t) => t.stopped)).toBe(true)
  })

  it('is idempotent', async () => {
    const { share } = await startSharing()

    await share.stop()
    await share.stop() // must not throw or re-issue a DELETE

    const deletes = fetchMock.mock.calls.filter((c) => (c[1] as { method?: string } | undefined)?.method === 'DELETE')
    expect(deletes).toHaveLength(1)
  })

  it('stops the SCREEN, not the meeting, when the user ends capture from the banner', async () => {
    const { share } = await startSharing()

    // The video track ending is what Chromium fires when the user clicks the
    // "Stop sharing" bar. Meetings have no type, so losing the picture is not a
    // reason to hang up — the call continues without one.
    displayStream.getVideoTracks()[0].emitEnded()

    await vi.waitFor(() => expect(share.captureKind.value).toBeNull())
    expect(share.state.value).toBe('publishing')
    expect(share.videoEnabled.value).toBe(false)
    expect(lastWS().sentJSON()).toContainEqual({ type: 'set-picture', video: false, camera: false, screen: false })
  })

  it('stops when the publisher socket closes', async () => {
    const { share } = await startSharing()
    await vi.waitFor(() => expect(share.state.value).toBe('publishing'))

    lastWS().onclose?.()

    await vi.waitFor(() => expect(share.state.value).toBe('idle'))
  })
})

describe('listAudioInputs', () => {
  it('returns only the input devices, with their labels', async () => {
    vi.stubGlobal('navigator', {
      mediaDevices: {
        enumerateDevices: vi.fn(async () => [
          { kind: 'audioinput', deviceId: 'mic-a', label: 'Built-in Microphone' },
          { kind: 'videoinput', deviceId: 'cam-a', label: 'FaceTime HD' },
          { kind: 'audioinput', deviceId: 'mic-b', label: 'Headset Microphone' },
        ]),
      },
    })
    const { listAudioInputs } = await import('../useLiveShare')

    const list = await listAudioInputs()

    expect(list.map((d) => d.id)).toEqual(['mic-a', 'mic-b'])
    expect(list[1].name).toBe('Headset Microphone')
  })

  it('numbers the devices the browser will not name yet', async () => {
    // Labels are empty until the page has microphone permission, which is the
    // normal state on a first visit — hiding them would leave the picker empty.
    vi.stubGlobal('navigator', {
      mediaDevices: {
        enumerateDevices: vi.fn(async () => [
          { kind: 'audioinput', deviceId: 'mic-a', label: '' },
          { kind: 'audioinput', deviceId: 'mic-b', label: '' },
        ]),
      },
    })
    const { listAudioInputs } = await import('../useLiveShare')

    const list = await listAudioInputs()

    expect(list.map((d) => d.name)).toEqual(['Microphone 1', 'Microphone 2'])
    expect(list[0].id).toBe('mic-a')
  })

  it('returns an empty list when enumeration is unavailable or fails', async () => {
    vi.stubGlobal('navigator', { mediaDevices: {} })
    const { listAudioInputs } = await import('../useLiveShare')
    expect(await listAudioInputs()).toEqual([])

    vi.stubGlobal('navigator', {
      mediaDevices: {
        enumerateDevices: vi.fn(async () => {
          throw new Error('boom')
        }),
      },
    })
    expect(await listAudioInputs()).toEqual([])
  })
})

describe('listVideoInputs', () => {
  it('returns only the cameras, numbered when the browser hides their names', async () => {
    vi.stubGlobal('navigator', {
      mediaDevices: {
        enumerateDevices: vi.fn(async () => [
          { kind: 'audioinput', deviceId: 'mic-a', label: 'Built-in Microphone' },
          { kind: 'videoinput', deviceId: 'cam-a', label: '' },
          { kind: 'videoinput', deviceId: 'cam-b', label: 'USB Camera' },
        ]),
      },
    })
    const { listVideoInputs } = await import('../useLiveShare')

    const list = await listVideoInputs()

    expect(list.map((d) => d.id)).toEqual(['cam-a', 'cam-b'])
    expect(list[0].name).toBe('Camera 1')
    expect(list[1].name).toBe('USB Camera')
  })
})

describe('listCaptureSources', () => {
  it('parses the host JSON', async () => {
    bridge.listCaptureSources.mockResolvedValueOnce(
      JSON.stringify([{ id: 'screen:1:0', name: 'Display 1', kind: 'screen', thumbnail: '' }]),
    )
    const { listCaptureSources } = await import('../useLiveShare')

    const list = await listCaptureSources()

    expect(list).toHaveLength(1)
    expect(list[0].kind).toBe('screen')
  })

  it('labels a nameless source instead of rendering a blank tile', async () => {
    // Wayland lists one placeholder with no name; the system dialog does the
    // real picking. An empty label would render an unclickable-looking tile.
    bridge.listCaptureSources.mockResolvedValueOnce(
      JSON.stringify([{ id: 'window:1:0:s', name: '', kind: 'screen', thumbnail: '' }]),
    )
    const { listCaptureSources } = await import('../useLiveShare')

    const list = await listCaptureSources()

    expect(list[0].name).not.toBe('')
    expect(list[0].id).toBe('window:1:0:s')
  })

  it('returns an empty list when the host fails', async () => {
    bridge.listCaptureSources.mockRejectedValueOnce(new Error('boom'))
    const { listCaptureSources } = await import('../useLiveShare')

    expect(await listCaptureSources()).toEqual([])
  })

  it('returns an empty list when the host is absent', async () => {
    const native = await import('@/utils/clawbenchNative')
    const spy = vi.spyOn(native, 'getNative').mockReturnValue({} as never)
    const { listCaptureSources } = await import('../useLiveShare')

    expect(await listCaptureSources()).toEqual([])
    spy.mockRestore()
  })
})

describe('audience screen relay', () => {
  it('forwards a viewer screen to everyone else and never back to the sharer', async () => {
    const { share } = await startSharing()
    await joinAndOffer('v1')
    await joinAndOffer('v2')

    const [pc1, pc2] = FakePC.instances
    const screen = new FakeTrack('video', 'screen-1')
    // The server says WHO is sharing; `ontrack` alone is not enough (the video
    // m-line exists, muted, from the moment the viewer joins).
    lastWS().emit({ type: 'screen-state', screenEnabled: true, viewerId: 'v1', sharing: true })
    pc1.ontrack?.({ track: screen, streams: [] })

    // The sharer must never receive its own screen: that would be an echo of
    // its own desktop one network hop late.
    expect(pc1.transceivers[2].sender.replaceTrack).not.toHaveBeenCalled()
    expect(pc2.transceivers[2].sender.replaceTrack).toHaveBeenCalledWith(screen)
    expect(share.remoteScreenStream.value?.getTracks()).toEqual([screen])
    expect(share.screenSharer.value).toBe('v1')
  })

  it('detaches the relayed screen from every viewer when the share ends', async () => {
    const { share } = await startSharing()
    await joinAndOffer('v1')
    await joinAndOffer('v2')

    const [pc1, pc2] = FakePC.instances
    lastWS().emit({ type: 'screen-state', screenEnabled: true, viewerId: 'v1', sharing: true })
    pc1.ontrack?.({ track: new FakeTrack('video', 'screen-2'), streams: [] })

    lastWS().emit({ type: 'screen-state', screenEnabled: true, viewerId: '', sharing: false })

    expect(pc2.transceivers[2].sender.replaceTrack).toHaveBeenLastCalledWith(null)
    expect(share.remoteScreenStream.value).toBeNull()
    expect(share.screenSharer.value).toBe('')
  })

  it('relays to a viewer that joins after the share started', async () => {
    const { share } = await startSharing()
    await joinAndOffer('v1')
    const screen = new FakeTrack('video', 'screen-1')
    lastWS().emit({ type: 'screen-state', screenEnabled: true, viewerId: 'v1', sharing: true })
    FakePC.instances[0].ontrack?.({ track: screen, streams: [] })

    await joinAndOffer('v2')

    // The late joiner gets the already-running screen attached at join time.
    expect(FakePC.instances[1].transceivers[2].sender.replaceTrack).toHaveBeenCalledWith(screen)
    expect(share.remoteScreenStream.value?.getTracks()).toEqual([screen])
  })

  it('relays the room switch to the server', async () => {
    const { share } = await startSharing()
    share.setScreenEnabled(true)
    expect(lastWS().sentJSON()).toContainEqual({ type: 'set-screen-enabled', enabled: true })

    share.revokeScreen('v1')
    expect(lastWS().sentJSON()).toContainEqual({ type: 'screen-revoke', viewerId: 'v1' })
  })
})

describe('control channel', () => {
  it('relays a viewer frame to the others, tagged with the real sender', async () => {
    const { share } = await startSharing()
    await joinAndOffer('v1')
    await joinAndOffer('v2')

    const [dc1, dc2] = FakeDataChannel.instances
    const seen: unknown[] = []
    share.onControl((m) => seen.push(m))

    // A viewer cannot name itself: whatever `from` it sends is replaced.
    dc1.emit({ t: 'chat', id: 'c1', from: 'v2', name: '主持人', text: 'hi', at: 1 })

    expect(seen).toHaveLength(1)
    expect((seen[0] as { from: string }).from).toBe('v1')
    // The sharer does not get its own frame back.
    expect(dc1.sentJSON()).toHaveLength(0)
    expect(dc2.sentJSON()).toEqual([expect.objectContaining({ t: 'chat', from: 'v1' })])
  })

  it('replaces a viewer-supplied chat name with the one the server accepted', async () => {
    const { share } = await startSharing()
    await joinAndOffer('v1', '小明')
    await joinAndOffer('v2', '小红')

    const [dc1, dc2] = FakeDataChannel.instances
    const seen: unknown[] = []
    share.onControl((m) => seen.push(m))

    // A viewer can put anything in `name`; other viewers render it verbatim, so
    // without the roster rewrite it could label itself as the host.
    dc1.emit({ t: 'chat', id: 'c1', from: 'v1', name: '主持人', text: 'hi', at: 1 })

    expect((seen[0] as { name: string }).name).toBe('小明')
    expect(dc2.sentJSON()).toEqual([expect.objectContaining({ t: 'chat', from: 'v1', name: '小明' })])
  })

  it('drops frames a viewer is not allowed to send', async () => {
    const { share } = await startSharing()
    await joinAndOffer('v1')
    const dc = FakeDataChannel.instances[0]
    const seen: unknown[] = []
    share.onControl((m) => seen.push(m))

    // `talk` is publisher→viewer only; a viewer must not be able to fake it.
    dc.emit({ t: 'talk', viewerId: 'v1', level: 1 })
    expect(seen).toHaveLength(0)
  })

  it('hands a history request to the UI without relaying it to other viewers', async () => {
    const { share } = await startSharing()
    await joinAndOffer('v1')
    await joinAndOffer('v2')

    const [dc1, dc2] = FakeDataChannel.instances
    const seen: unknown[] = []
    share.onControl((m) => seen.push(m))

    dc1.emit({ t: 'chat-history-req' })

    // The publisher answers; the other viewers have no transcript to hand out.
    expect(seen).toEqual([expect.objectContaining({ t: 'chat-history-req', from: 'v1' })])
    expect(dc2.sentJSON()).toHaveLength(0)
  })

  it('addresses a grant to one viewer instead of broadcasting it', async () => {
    const { share } = await startSharing()
    await joinAndOffer('v1')
    await joinAndOffer('v2')
    const [dc1, dc2] = FakeDataChannel.instances

    share.sendControlTo('v1', { t: 'ctrl-grant', viewerId: 'v1', granted: true })

    expect(dc1.sentJSON()).toEqual([{ t: 'ctrl-grant', viewerId: 'v1', granted: true }])
    // Broadcasting would put every viewer into "controlling" and swallow their
    // typing, so v2 must see nothing.
    expect(dc2.sentJSON()).toHaveLength(0)
  })
})

describe('remote control grantee', () => {
  it('revokes control when the granted viewer disappears', async () => {
    const { share } = await startSharing()
    await joinAndOffer('v1')
    share.sendControlTo('v1', { t: 'ctrl-grant', viewerId: 'v1', granted: true })
    const seen: Array<Record<string, unknown>> = []
    share.onControl((m) => seen.push(m as unknown as Record<string, unknown>))

    lastWS().emit({ type: 'viewer-leave', viewerId: 'v1' })

    // The main process must not stay armed for a viewer that is gone: it holds
    // the real capability (`grantedViewer`), the panel only shows it.
    expect(bridge.remoteControlRevoke).toHaveBeenCalled()
    // The panel owns `controlledBy`, so it clears it from the frame it already
    // handles — a viewer's own release — rather than a second event type.
    expect(seen).toContainEqual({ t: 'ctrl-release', from: 'v1' })
  })

  it('leaves an unrelated viewer departure alone', async () => {
    const { share } = await startSharing()
    await joinAndOffer('v1')
    await joinAndOffer('v2')
    share.sendControlTo('v1', { t: 'ctrl-grant', viewerId: 'v1', granted: true })
    const seen: unknown[] = []
    share.onControl((m) => seen.push(m))

    lastWS().emit({ type: 'viewer-leave', viewerId: 'v2' })

    // v1 still holds control: revoking here would stop a working session.
    expect(bridge.remoteControlRevoke).not.toHaveBeenCalled()
    expect(seen).toHaveLength(0)
  })

  it('forgets the grantee once the panel revokes it', async () => {
    const { share } = await startSharing()
    await joinAndOffer('v1')
    share.sendControlTo('v1', { t: 'ctrl-grant', viewerId: 'v1', granted: true })
    share.sendControlTo('v1', { t: 'ctrl-grant', viewerId: 'v1', granted: false })

    lastWS().emit({ type: 'viewer-leave', viewerId: 'v1' })

    // A revoked grant must not fire a second revoke when that viewer leaves.
    expect(bridge.remoteControlRevoke).not.toHaveBeenCalled()
  })
})

describe('switchSource', () => {
  it('swaps the track on every peer and stops the old capture', async () => {
    const { share } = await startSharing()
    await joinAndOffer('v1')

    const oldTrack = displayStream.getVideoTracks()[0]
    const nextTrack = new FakeTrack('video', 'screen-2')
    const nextStream = new FakeMediaStream([nextTrack])
    const gdm = (navigator as unknown as { mediaDevices: { getDisplayMedia: ReturnType<typeof vi.fn> } })
      .mediaDevices.getDisplayMedia
    gdm.mockResolvedValueOnce(nextStream)

    const ok = await share.switchSource('window:2:0')

    expect(ok).toBe(true)
    expect(FakePC.instances[0].transceivers[1].sender.replaceTrack).toHaveBeenCalledWith(nextTrack)
    expect(oldTrack.stopped).toBe(true)
    expect(share.previewStream.value).toBe(nextStream)
    // The new stream must become the session's stream, otherwise resetSession()
    // never stops it and the capture indicator stays on after "end share".
    expect(share.captureKind.value).toBe('window')
  })

  it('re-opens the browser picker when there is no source list to choose from', async () => {
    const native = await import('@/utils/clawbenchNative')
    const spy = vi.spyOn(native, 'getNative').mockReturnValue({} as never)
    const { share } = await startSharing()
    await joinAndOffer('v1')

    const nextTrack = new FakeTrack('video', 'browser-2')
    nextTrack.settings = { displaySurface: 'window' }
    const nextStream = new FakeMediaStream([nextTrack])
    const gdm = (navigator as unknown as { mediaDevices: { getDisplayMedia: ReturnType<typeof vi.fn> } })
      .mediaDevices.getDisplayMedia
    gdm.mockResolvedValueOnce(nextStream)
    // The reset helper clears the declaration with '', so start counting after.
    bridge.setPendingCaptureSource.mockClear()

    const ok = await share.switchSource('')

    expect(ok).toBe(true)
    // Nothing to declare: the second picker call IS the switch.
    expect(bridge.setPendingCaptureSource).not.toHaveBeenCalled()
    expect(FakePC.instances[0].transceivers[1].sender.replaceTrack).toHaveBeenCalledWith(nextTrack)
    expect(share.captureKind.value).toBe('window')
    spy.mockRestore()
  })

  it('clears the one-shot capture declaration when the switch fails', async () => {
    const { share } = await startSharing()
    const gdm = (navigator as unknown as { mediaDevices: { getDisplayMedia: ReturnType<typeof vi.fn> } })
      .mediaDevices.getDisplayMedia
    gdm.mockRejectedValueOnce(new Error('cancelled'))

    const ok = await share.switchSource('screen:0:0')

    expect(ok).toBe(false)
    // Leaving the declaration armed would let the NEXT getDisplayMedia capture
    // that source without a picker.
    expect(bridge.setPendingCaptureSource).toHaveBeenLastCalledWith('', false)
  })
})


describe('speaking indicator', () => {
  it('broadcasts the publisher level when it crosses the speaking threshold', async () => {
    const { share } = await startSharing()
    await joinAndOffer('v1')
    const dc = FakeDataChannel.instances[0]

    fakeMicLevel = 0.6
    await vi.waitFor(
      () =>
        expect(dc.sentJSON()).toEqual(
          expect.arrayContaining([
            expect.objectContaining({ t: 'talk', viewerId: 'host', level: expect.any(Number) }),
          ]),
        ),
      { timeout: 2000 },
    )
    expect(share.levels.value.host).toBeGreaterThan(0.05)

    // Falling silent is also a state change: the chip has to disappear.
    fakeMicLevel = 0
    await vi.waitFor(
      () =>
        expect(dc.sentJSON()).toEqual(
          expect.arrayContaining([expect.objectContaining({ t: 'talk', viewerId: 'host', level: 0 })]),
        ),
      { timeout: 2000 },
    )
  })
})

describe('camera', () => {
  /** Start a meeting with an explicit source / camera combination. */
  async function startMeeting(opts: { sourceId?: string; camera?: boolean; cameraDeviceId?: string }) {
    const { useLiveShare } = await import('../useLiveShare')
    const share = useLiveShare()
    const ok = await share.start({
      sourceId: opts.sourceId ?? '',
      microphone: true,
      cameraDeviceId: opts.cameraDeviceId ?? (opts.camera ? 'default' : ''),
    })
    return { share, ok }
  }

  it('captures the camera and reports it to the room', async () => {
    const { share, ok } = await startMeeting({ sourceId: 'screen:1:0', camera: true })

    expect(ok).toBe(true)
    expect(share.cameraEnabled.value).toBe(true)
    expect(share.previewCameraStream.value).toBe(cameraStream)

    const body = JSON.parse(String((fetchMock.mock.calls[0][1] as { body?: string }).body))
    expect(body).toMatchObject({ video: true, camera: true, screen: true })
  })

  it('puts the camera on the main picture when there is no screen', async () => {
    const { share, ok } = await startMeeting({ sourceId: '', camera: true })

    expect(ok).toBe(true)
    // No screen: the camera IS the picture. captureKind stays null so annotate,
    // switch-source and remote control stay hidden.
    expect(share.captureKind.value).toBeNull()
    expect(share.videoEnabled.value).toBe(true)

    offerCameraLine = true
    const pc = await joinAndOffer('v1')
    // Main picture carries the camera; the tile m-line carries nothing, or the
    // viewer would render the same face twice.
    expect(pc.transceivers[1].sender.track?.id).toBe('cam1')
    expect(pc.transceivers[3].sender.track).toBeNull()
  })

  it('sends the camera on its own m-line and stream when a screen is shared', async () => {
    await startMeeting({ sourceId: 'screen:1:0', camera: true })

    offerCameraLine = true
    const pc = await joinAndOffer('v1')

    expect(pc.transceivers.map((t) => t.direction)).toEqual([
      'sendrecv',
      'sendrecv',
      'sendonly',
      'sendonly',
    ])
    expect(pc.transceivers[1].sender.track?.id).toBe('v1')
    expect(pc.transceivers[3].sender.track?.id).toBe('cam1')

    // CRITICAL: the camera must NOT share the main stream. The viewer assigns
    // `srcObject` per received stream, so the camera would replace the screen.
    const mainStreams = pc.transceivers[1].sender.streams
    const cameraStreams = pc.transceivers[3].sender.streams
    expect(cameraStreams).toHaveLength(1)
    expect(cameraStreams[0]).not.toBe(mainStreams[0])
    expect((cameraStreams[0] as unknown as FakeMediaStream).getTracks().map((t) => t.id)).toEqual(['cam1'])
  })

  it('still answers a viewer that offers only three m-lines', async () => {
    await startMeeting({ sourceId: 'screen:1:0', camera: true })

    // A stale bundle: no camera m-line offered. Refusing would break the whole
    // share for that viewer; it simply gets no tile.
    const pc = await joinAndOffer('v1')

    expect(pc.transceivers).toHaveLength(3)
    expect(lastWS().sentJSON().some((m) => m.type === 'answer')).toBe(true)
  })

  it('toggles the camera with track.enabled and tells the viewers', async () => {
    const { share } = await startMeeting({ sourceId: 'screen:1:0', camera: true })

    await share.setCameraDevice('')
    expect(share.cameraEnabled.value).toBe(false)
    expect(cameraStream.getVideoTracks()[0].stopped).toBe(false)
    // Muting, not removing: removing would need a renegotiation, and the
    // publisher is the answerer. The screen is still up, so there IS still a
    // picture — only the camera tile goes away.
    expect(lastWS().sentJSON()).toContainEqual({ type: 'set-picture', video: true, camera: false, screen: true })
  })

  it('leaves the camera alone when the screen source is switched', async () => {
    const { share } = await startMeeting({ sourceId: 'screen:1:0', camera: true })
    offerCameraLine = true
    const pc = await joinAndOffer('v1')
    expect(pc.transceivers[3].sender.track?.id).toBe('cam1')

    const next = new FakeStream([new FakeTrack('video', 'v2')])
    ;(navigator.mediaDevices.getDisplayMedia as unknown as ReturnType<typeof vi.fn>).mockResolvedValueOnce(next)
    await share.switchSource('screen:1:1')

    expect(pc.transceivers[1].sender.track?.id).toBe('v2')
    // The camera must survive a source swap: it is a different m-line, and
    // stopping it here would silently kill the tile.
    expect(pc.transceivers[3].sender.track?.id).toBe('cam1')
    expect(cameraStream.getVideoTracks()[0].stopped).toBe(false)
  })

  it('releases the camera on stop()', async () => {
    const { share } = await startMeeting({ sourceId: 'screen:1:0', camera: true })
    expect(share.cameraEnabled.value).toBe(true)

    await share.stop()

    // A camera left running is a privacy problem, not just a leak: the
    // indicator light stays on with nobody receiving the frames.
    expect(cameraStream.getVideoTracks()[0].stopped).toBe(true)
    expect(share.cameraEnabled.value).toBe(false)
    expect(share.previewCameraStream.value).toBeNull()
  })
})

describe('picture changes mid-meeting', () => {
  /** Start with nothing but the microphone. */
  async function startEmpty() {
    const { useLiveShare } = await import('../useLiveShare')
    const share = useLiveShare()
    const ok = await share.start({ sourceId: '', microphone: true })
    return { share, ok }
  }

  it('adds a screen without renegotiating and tells the room', async () => {
    const { share, ok } = await startEmpty()
    expect(ok).toBe(true)
    expect(share.videoEnabled.value).toBe(false)
    expect(share.captureKind.value).toBeNull()

    const pc = await joinAndOffer('v1')
    // No picture yet: the m-line exists (it was negotiated) but carries nothing.
    expect(pc.transceivers[1].sender.track).toBeNull()
    const negotiations = pc.setRemoteDescription.mock.calls.length

    expect(await share.startScreen('screen:2:0')).toBe(true)

    // replaceTrack on the already-negotiated m-line: no second offer/answer.
    expect(pc.setRemoteDescription).toHaveBeenCalledTimes(negotiations)
    expect(pc.transceivers[1].sender.track?.id).toBe('v1')
    expect(share.videoEnabled.value).toBe(true)
    expect(share.captureKind.value).toBe('screen')
    expect(lastWS().sentJSON()).toContainEqual({
      type: 'set-picture',
      video: true,
      camera: false,
      screen: true,
    })
  })

  it('hands the main picture back to the camera when the screen stops', async () => {
    const { useLiveShare } = await import('../useLiveShare')
    const share = useLiveShare()
    await share.start({ sourceId: 'screen:1:0', microphone: true, cameraDeviceId: 'default' })

    offerCameraLine = true
    const pc = await joinAndOffer('v1')
    expect(pc.transceivers[1].sender.track?.id).toBe('v1')
    expect(pc.transceivers[3].sender.track?.id).toBe('cam1')

    await share.stopScreen()

    // The screen's m-line falls back to the camera, and the tile m-line is
    // emptied: one picture, no duplicate face.
    expect(pc.transceivers[1].sender.track?.id).toBe('cam1')
    expect(pc.transceivers[3].sender.track).toBeNull()
    // Still a meeting, still a picture.
    expect(share.state.value).toBe('publishing')
    expect(share.videoEnabled.value).toBe(true)
    expect(share.captureKind.value).toBeNull()
    expect(displayStream.getVideoTracks()[0].stopped).toBe(true)
    expect(lastWS().sentJSON()).toContainEqual({
      type: 'set-picture',
      video: true,
      camera: true,
      screen: false,
    })
  })

  it('goes to the no-picture state when the screen stops and no camera is on', async () => {
    const { useLiveShare } = await import('../useLiveShare')
    const share = useLiveShare()
    await share.start({ sourceId: 'screen:1:0', microphone: true })

    const pc = await joinAndOffer('v1')
    expect(pc.transceivers[1].sender.track?.id).toBe('v1')

    await share.stopScreen()

    expect(pc.transceivers[1].sender.track).toBeNull()
    expect(share.state.value).toBe('publishing')
    expect(share.videoEnabled.value).toBe(false)
    expect(lastWS().sentJSON()).toContainEqual({
      type: 'set-picture',
      video: false,
      camera: false,
      screen: false,
    })
  })

  it('turns the camera on after the meeting started', async () => {
    const { share } = await startEmpty()

    offerCameraLine = true
    const pc = await joinAndOffer('v1')
    expect(pc.transceivers[1].sender.track).toBeNull()

    expect(await share.setCameraDevice('default')).toBe(true)

    // No screen: the camera IS the picture, on the main m-line.
    expect(pc.transceivers[1].sender.track?.id).toBe('cam1')
    expect(pc.transceivers[3].sender.track).toBeNull()
    expect(share.videoEnabled.value).toBe(true)
    expect(share.previewCameraStream.value).toBe(cameraStream)
    expect(lastWS().sentJSON()).toContainEqual({
      type: 'set-picture',
      video: true,
      camera: true,
      screen: false,
    })
  })

  // One entry point for "mute" and "pick a device", so the card the host sees
  // and the stream being published cannot disagree.
  it('mutes and unmutes through the same choice, without reopening the device', async () => {
    const { share } = await startSharing()
    const gum = navigator.mediaDevices.getUserMedia as unknown as ReturnType<typeof vi.fn>
    const callsAfterStart = gum.mock.calls.length

    expect(await share.setMicChoice('')).toBe(true)
    expect(share.micEnabled.value).toBe(false)
    expect(await share.setMicChoice('default')).toBe(true)
    expect(share.micEnabled.value).toBe(true)
    // Same device as the live stream: unmuting must not churn the capture.
    expect(gum.mock.calls.length).toBe(callsAfterStart)
  })

  it('moves the camera to another device mid-meeting', async () => {
    const { share } = await startEmpty()
    expect(await share.setCameraDevice('default')).toBe(true)
    const oldTrack = cameraStream.getVideoTracks()[0]
    const next = new FakeStream([new FakeTrack('video', 'cam2')])
    const gum = navigator.mediaDevices.getUserMedia as unknown as ReturnType<typeof vi.fn>
    gum.mockImplementationOnce(async () => next)

    expect(await share.setCameraDevice('cam-b')).toBe(true)

    // The old device is released, and the self-view follows the new stream.
    expect(oldTrack.stopped).toBe(true)
    expect(share.previewCameraStream.value).toBe(next)
    const video = gum.mock.calls[gum.mock.calls.length - 1][0].video
    expect(video).toMatchObject({ deviceId: { exact: 'cam-b' } })
  })

  it('reports a camera that cannot start instead of pretending it is on', async () => {
    const { share } = await startEmpty()
    ;(navigator.mediaDevices.getUserMedia as unknown as ReturnType<typeof vi.fn>).mockRejectedValueOnce(
      Object.assign(new Error('denied'), { name: 'NotAllowedError' }),
    )

    expect(await share.setCameraDevice('default')).toBe(false)
    expect(share.cameraEnabled.value).toBe(false)
    expect(share.videoEnabled.value).toBe(false)
    expect(share.error.value).toBeTruthy()
  })
})
