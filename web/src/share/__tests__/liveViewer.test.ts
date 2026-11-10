import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { LiveViewer, liveSignalUrl, type LiveViewerState } from '../liveViewer'

/**
 * The relay client is replaced wholesale: `livekit-client` needs a real
 * browser (WebRTC, workers) and cannot run under jsdom. Everything the viewer
 * does with the room is a function call, so asserting on those calls covers
 * the contract — the LiveKit side is `useLiveRelay`'s own responsibility.
 */
const relayMock = vi.hoisted(() => ({
  openRelay: vi.fn(),
  closeRelay: vi.fn(),
  publishVideo: vi.fn(),
  publishMic: vi.fn(),
  sendRelayData: vi.fn(),
  // The page always goes through this server's /livekit proxy; the real
  // rewrite has its own test in useLiveRelay. A fixed value here proves the
  // viewer uses the helper rather than the relay's own address.
  relayConnectURL: () => 'wss://clawbench.test/livekit',
}))

vi.mock('@/composables/useLiveRelay', () => ({
  openRelay: relayMock.openRelay,
  closeRelay: relayMock.closeRelay,
  publishVideo: relayMock.publishVideo,
  publishMic: relayMock.publishMic,
  sendRelayData: relayMock.sendRelayData,
  relayConnectURL: relayMock.relayConnectURL,
  // Real implementation: the viewer must run frames through the same parser
  // the peer-to-peer path uses, so a fake that always returns null would hide
  // a broken wiring.
  decodeRelayData: (payload: Uint8Array) => {
    try {
      return JSON.parse(new TextDecoder().decode(payload)) as unknown
    } catch {
      return null
    }
  },
}))

/**
 * Fakes for the browser APIs the viewer touches.
 *
 * RTCPeerConnection is not implemented by jsdom, so the tests drive a minimal
 * double that records the calls the viewer makes and lets each test inject
 * server frames by hand. That keeps the assertions about OUR protocol handling
 * rather than about Chromium.
 */

class FakeSender {
  track: MediaStreamTrack | null = null
  params: { encodings: Array<Record<string, unknown>>; degradationPreference?: string } = {
    encodings: [{}],
  }
  replaceTrack = vi.fn(async (t: MediaStreamTrack | null) => {
    this.track = t
  })
  getParameters() {
    return this.params
  }
  setParameters = vi.fn(async (p: typeof this.params) => {
    this.params = p
  })
}

class FakeTransceiver {
  sender = new FakeSender()
  /** The m-line identity, assigned by `addTransceiver` (null before SDP). */
  mid: string | null = null
  constructor(public direction: string) {}
}

class FakePC {
  static instances: FakePC[] = []
  ontrack:
    | ((ev: {
        streams: MediaStream[]
        track: MediaStreamTrack
        transceiver?: FakeTransceiver
      }) => void)
    | null = null
  onicecandidate: ((ev: { candidate: { toJSON: () => RTCIceCandidateInit } | null }) => void) | null = null
  onconnectionstatechange: (() => void) | null = null
  connectionState = 'new'
  transceivers: FakeTransceiver[] = []
  localDescription: { type: string; sdp: string } | null = null
  remoteDescriptions: RTCSessionDescriptionInit[] = []
  addedCandidates: RTCIceCandidateInit[] = []
  closed = false

  constructor(public config: RTCConfiguration) {
    FakePC.instances.push(this)
  }

  addTransceiver(kind: string, init?: { direction: string }) {
    const tx = new FakeTransceiver(init?.direction ?? 'sendrecv')
    // Chromium numbers the m-lines in creation order; the fake does the same so
    // a test can tell the picture line from the relay/camera ones.
    tx.mid = String(this.transceivers.length)
    this.transceivers.push(tx)
    return tx as unknown as RTCRtpTransceiver
  }

  /** What the next `getStats()` returns (tests fill it per case). */
  statsReport = new Map<string, Record<string, unknown>>()
  getStats = vi.fn(async () => this.statsReport)

  /** Channels the VIEWER created (the offerer owns the control bus). */
  channels: FakeDC[] = []
  createDataChannel = vi.fn((label: string) => {
    const dc = new FakeDC(label)
    this.channels.push(dc)
    return dc as unknown as RTCDataChannel
  })

  createOffer = vi.fn(async () => ({ type: 'offer' as const, sdp: 'v=0-offer' }))

  setLocalDescription = vi.fn(async (desc: RTCSessionDescriptionInit) => {
    this.localDescription = { type: desc.type ?? '', sdp: desc.sdp ?? '' }
  })

  setRemoteDescription = vi.fn(async (desc: RTCSessionDescriptionInit) => {
    this.remoteDescriptions.push(desc)
  })

  addIceCandidate = vi.fn(async (c: RTCIceCandidateInit) => {
    this.addedCandidates.push(c)
  })

  close = vi.fn(() => {
    this.closed = true
  })
}

class FakeDC {
  readyState = 'open'
  sent: string[] = []
  onmessage: ((ev: { data: string }) => void) | null = null
  onopen: (() => void) | null = null
  constructor(public label: string) {}
  send(data: string) {
    this.sent.push(data)
  }
  /** Push a frame as if the publisher sent it. */
  emit(msg: unknown) {
    this.onmessage?.({ data: JSON.stringify(msg) })
  }
  sentJSON(): Record<string, unknown>[] {
    return this.sent.map((x) => JSON.parse(x) as Record<string, unknown>)
  }
}

class FakeWS {
  static instances: FakeWS[] = []
  readyState = 1 // OPEN
  sent: string[] = []
  onmessage: ((ev: { data: unknown }) => void) | null = null
  onerror: (() => void) | null = null
  onclose: (() => void) | null = null

  constructor(public url: string) {
    FakeWS.instances.push(this)
  }

  send(data: string) {
    this.sent.push(data)
  }

  close() {
    this.readyState = 3
  }

  /** Push one server frame into the viewer. */
  emit(msg: Record<string, unknown>) {
    this.onmessage?.({ data: JSON.stringify(msg) })
  }

  sentJSON(): Record<string, unknown>[] {
    return this.sent.map((s) => JSON.parse(s) as Record<string, unknown>)
  }
}

let trackSeq = 0

function makeTrack(kind = 'audio'): MediaStreamTrack & { fireEnded: () => void } {
  const listeners = new Map<string, Array<() => void>>()
  const track = {
    id: `track-${++trackSeq}`,
    kind,
    stop: vi.fn(),
    addEventListener: (type: string, cb: () => void) => {
      const list = listeners.get(type) ?? []
      list.push(cb)
      listeners.set(type, list)
    },
    fireEnded: () => {
      for (const cb of listeners.get('ended') ?? []) cb()
    },
  }
  return track as unknown as MediaStreamTrack & { fireEnded: () => void }
}

/**
 * jsdom has no `mediaDevices`. The viewer only needs the `devicechange` event,
 * so this installs exactly that and returns the trigger.
 */
function installMediaDevices(): () => void {
  const listeners: Array<() => void> = []
  Object.defineProperty(navigator, 'mediaDevices', {
    configurable: true,
    value: {
      addEventListener: (_type: string, cb: () => void) => listeners.push(cb),
      removeEventListener: () => undefined,
    },
  })
  return () => {
    for (const cb of [...listeners]) cb()
  }
}

function setup(overrides: Record<string, unknown> = {}) {  const states: Array<{ state: LiveViewerState; reason?: string }> = []
  const streams: MediaStream[] = []
  const viewer = new LiveViewer({
    token: 'tok123',
    callbacks: {
      onStateChange: (state, detail) => states.push({ state, reason: detail?.reason }),
      onStream: (s) => streams.push(s),
    },
    wsFactory: (url) => new FakeWS(url) as unknown as WebSocket,
    pcFactory: (cfg) => new FakePC(cfg) as unknown as RTCPeerConnection,
    ...overrides,
  })
  return { viewer, states, streams }
}

function lastWS(): FakeWS {
  return FakeWS.instances[FakeWS.instances.length - 1]
}

function lastPC(): FakePC {
  return FakePC.instances[FakePC.instances.length - 1]
}

/** Drive the viewer to the `negotiating` state. */
function joinRoom(
  viewer: LiveViewer,
  opts: { talkEnabled?: boolean; roster?: Array<Record<string, unknown>> } = {},
) {
  viewer.connect()
  lastWS().emit({
    type: 'joined',
    viewerId: 'v1',
    title: '测试共享',
    iceServers: [],
    talkEnabled: opts.talkEnabled ?? false,
    ...(opts.roster ? { roster: opts.roster } : {}),
  })
}

beforeEach(() => {
  FakePC.instances = []
  FakeWS.instances = []
  relayMock.openRelay.mockReset()
  relayMock.closeRelay.mockReset()
  relayMock.publishVideo.mockReset()
  relayMock.publishMic.mockReset()
  relayMock.sendRelayData.mockReset()
})

afterEach(() => {
  vi.restoreAllMocks()
  // @ts-expect-error -- jsdom has no real implementation to restore to.
  delete navigator.mediaDevices
})

describe('control bus', () => {
  it('creates the channel itself, before the offer, and exchanges frames over it', async () => {
    const seen: unknown[] = []
    const viewer = new LiveViewer({
      token: 'tok123',
      callbacks: { onControl: (m) => seen.push(m) },
      wsFactory: (url) => new FakeWS(url) as unknown as WebSocket,
      pcFactory: (cfg) => new FakePC(cfg) as unknown as RTCPeerConnection,
    })
    joinRoom(viewer)
    const pc = lastPC()

    // The OFFERER owns the bus: an answer cannot introduce an m-line, so a
    // channel created by the publisher would stay `connecting` forever and
    // every chat/control frame would go nowhere.
    expect(pc.channels).toHaveLength(1)
    expect(pc.createDataChannel).toHaveBeenCalledWith('cb-live', { ordered: true })
    expect(pc.createDataChannel.mock.invocationCallOrder[0]).toBeLessThan(
      pc.createOffer.mock.invocationCallOrder[0],
    )

    expect(viewer.sendControl({ t: 'ctrl-req' })).toBe(true)
    expect(pc.channels[0].sentJSON()).toEqual([{ t: 'ctrl-req' }])

    pc.channels[0].emit({ t: 'chat', id: 'c1', from: 'host', name: '主持人', text: 'hi', at: 1 })
    expect(seen).toHaveLength(1)
    expect(seen[0]).toMatchObject({ t: 'chat', text: 'hi' })
  })

  it('also tells the SERVER when control is requested', () => {
    const { viewer } = setup()
    joinRoom(viewer)

    viewer.requestControl()

    // Two copies, two audiences: `ctrl-req` (sent by the component, over the
    // data channel) is what the publisher's UI answers; this WS frame is what
    // lets the server notify a host who is not looking at the meeting page.
    // Without it the server never learns the request happened.
    expect(lastWS().sentJSON()).toContainEqual({ type: 'control-request' })
    expect(lastPC().channels[0].sentJSON()).toEqual([])
  })

  it('asks for the missed transcript once its channel opens', () => {
    const viewer = new LiveViewer({
      token: 'tok123',
      callbacks: {},
      wsFactory: (url) => new FakeWS(url) as unknown as WebSocket,
      pcFactory: (cfg) => new FakePC(cfg) as unknown as RTCPeerConnection,
    })
    joinRoom(viewer)
    const dc = lastPC().channels[0]

    // Not before `open`: the reply travels back on this same channel, so asking
    // any earlier would have the publisher's answer race a half-open channel.
    expect(dc.sentJSON()).toEqual([])
    dc.onopen?.()
    expect(dc.sentJSON()).toEqual([{ t: 'chat-history-req' }])
  })
})

describe('liveSignalUrl', () => {
  it('uses ws for http pages', () => {
    expect(liveSignalUrl('abc', { protocol: 'http:', host: 'h:20000' })).toBe(
      'ws://h:20000/api/share/abc/signal',
    )
  })

  it('uses wss for https pages', () => {
    expect(liveSignalUrl('abc', { protocol: 'https:', host: 'h:20000' })).toBe(
      'wss://h:20000/api/share/abc/signal',
    )
  })

  it('encodes the token', () => {
    expect(liveSignalUrl('a/b', { protocol: 'http:', host: 'h' })).toContain('/api/share/a%2Fb/signal')
  })
})

describe('LiveViewer connection', () => {
  it('reaches negotiating and sends an offer after joined', async () => {
    const { viewer, states } = setup()
    joinRoom(viewer)

    await vi.waitFor(() => expect(lastPC().createOffer).toHaveBeenCalled())
    await vi.waitFor(() => expect(viewer.state).toBe('negotiating'))

    expect(states.map((s) => s.state)).toEqual(['connecting', 'negotiating'])
    const sent = lastWS().sentJSON()
    expect(sent).toContainEqual({ type: 'offer', sdp: 'v=0-offer' })
  })

  it('passes iceServers from joined straight into the PeerConnection', () => {
    const { viewer } = setup()
    viewer.connect()
    lastWS().emit({
      type: 'joined',
      viewerId: 'v1',
      iceServers: [{ urls: 'stun:example.org' }],
    })

    // This is the seam that makes a future TURN rollout client-change-free.
    expect(lastPC().config.iceServers).toEqual([{ urls: 'stun:example.org' }])
  })

  it('reserves four m-lines in the agreed order', () => {
    const { viewer } = setup()
    joinRoom(viewer)

    const kinds = lastPC().transceivers.map((t) => t.direction)
    // 1 audio (mic up / mix down), 2 video (host picture down / our screen up),
    // 3 video (another viewer's screen down), 4 video (the host's camera tile
    // down). The publisher wires the same lines in the same order — this
    // ordering IS the contract.
    expect(kinds).toEqual(['sendrecv', 'sendrecv', 'recvonly', 'recvonly'])
    // Every sender starts empty: tracks are attached with replaceTrack, which
    // does not renegotiate.
    expect(lastPC().transceivers[0].sender.track).toBeNull()
    expect(lastPC().transceivers[1].sender.track).toBeNull()
  })

  it('routes the camera m-line to its own callback, not the main picture', () => {
    const cameraStreams: (MediaStream | null)[] = []
    const mainStreams: MediaStream[] = []
    const viewer = new LiveViewer({
      token: 'tok123',
      callbacks: {
        onStream: (s) => mainStreams.push(s),
        onCameraStream: (s) => cameraStreams.push(s),
      },
      wsFactory: (url) => new FakeWS(url) as unknown as WebSocket,
      pcFactory: (cfg) => new FakePC(cfg) as unknown as RTCPeerConnection,
    })
    joinRoom(viewer)

    const cameraStream = { id: 'cam' } as unknown as MediaStream
    lastPC().ontrack?.({
      streams: [cameraStream],
      track: makeTrack('video'),
      transceiver: lastPC().transceivers[3],
    })

    // The tile gets the stream; the main <video> must NOT — assigning the
    // camera there would replace the screen, and its lack of audio would also
    // clear the "tap to unmute" prompt.
    expect(cameraStreams).toEqual([cameraStream])
    expect(mainStreams).toEqual([])
  })

  it('reports playing when the remote track arrives', () => {
    const { viewer, streams } = setup()
    joinRoom(viewer)

    const track = makeTrack('video')
    const stream = { id: 's1' } as unknown as MediaStream
    lastPC().ontrack?.({ streams: [stream], track })

    expect(streams).toEqual([stream])
    expect(viewer.state).toBe('playing')
  })

  it('applies the answer', async () => {
    const { viewer } = setup()
    joinRoom(viewer)
    await vi.waitFor(() => expect(viewer.state).toBe('negotiating'))

    lastWS().emit({ type: 'answer', sdp: 'v=0-answer' })

    await vi.waitFor(() =>
      expect(lastPC().remoteDescriptions).toContainEqual({ type: 'answer', sdp: 'v=0-answer' }),
    )
  })

  it('forwards local ICE candidates to the server', async () => {
    const { viewer } = setup()
    joinRoom(viewer)

    lastPC().onicecandidate?.({
      candidate: { toJSON: () => ({ candidate: 'cand-1', sdpMid: '0' }) },
    })

    expect(lastWS().sentJSON()).toContainEqual({
      type: 'ice',
      candidate: { candidate: 'cand-1', sdpMid: '0' },
    })
  })

  it('adds remote ICE candidates', async () => {
    const { viewer } = setup()
    joinRoom(viewer)

    lastWS().emit({ type: 'ice', candidate: { candidate: 'remote-1' } })

    await vi.waitFor(() =>
      expect(lastPC().addedCandidates).toContainEqual({ candidate: 'remote-1' }),
    )
  })

  it('treats a null candidate as end-of-candidates, not an error', () => {
    const { viewer } = setup()
    joinRoom(viewer)

    // Must not throw and must not send anything.
    lastPC().onicecandidate?.({ candidate: null })
    expect(lastWS().sentJSON().filter((m) => m.type === 'ice')).toHaveLength(0)
  })
})

describe('LiveViewer terminal states', () => {
  it('reports full and tears down', () => {
    const { viewer, states } = setup()
    viewer.connect()
    lastWS().emit({ type: 'full' })

    expect(viewer.state).toBe('full')
    expect(states.some((s) => s.state === 'full')).toBe(true)
    expect(lastWS().readyState).toBe(3)
  })

  it('reports a refused duplicate name and tears down', () => {
    const { viewer, states } = setup()
    viewer.connect()
    lastWS().emit({ type: 'name-taken' })

    // Its own state, not 'failed': the page has to send the reader back to the
    // name field, and a failure state would offer a retry of the same name.
    expect(viewer.state).toBe('name-taken')
    expect(states.some((s) => s.state === 'name-taken')).toBe(true)
    expect(lastWS().readyState).toBe(3)
  })

  it('reports ended with the server reason', () => {
    const { viewer, states } = setup()
    joinRoom(viewer)
    lastWS().emit({ type: 'publisher-gone', reason: 'ended' })

    expect(viewer.state).toBe('ended')
    expect(states.at(-1)).toEqual({ state: 'ended', reason: 'ended' })
  })

  it('reports failed when the socket closes mid-session', () => {
    const { viewer } = setup()
    joinRoom(viewer)

    lastWS().onclose?.()

    expect(viewer.state).toBe('failed')
  })

  it('does not report failed when the socket closes after ended', () => {
    const { viewer } = setup()
    joinRoom(viewer)
    lastWS().emit({ type: 'publisher-gone', reason: 'ended' })

    lastWS().onclose?.()

    // A close following `ended` is expected and must not overwrite the reason.
    expect(viewer.state).toBe('ended')
  })

  it('reports failed when the peer connection fails', () => {
    const { viewer } = setup()
    joinRoom(viewer)

    const pc = lastPC()
    pc.connectionState = 'failed'
    pc.onconnectionstatechange?.()

    expect(viewer.state).toBe('failed')
  })
})

describe('LiveViewer talk', () => {
  it('turns the mic on and reports it, with no approval step', async () => {
    const micTrack = makeTrack('audio')
    const getUserMedia = vi.fn(async () => ({ getAudioTracks: () => [micTrack] }) as unknown as MediaStream)
    const { viewer } = setup({ getUserMedia })
    joinRoom(viewer, { talkEnabled: true })

    const ok = await viewer.startTalking()

    expect(ok).toBe(true)
    expect(getUserMedia).toHaveBeenCalledOnce()
    expect(lastPC().transceivers[0].sender.track).toBe(micTrack)
    // The only control frame is talk-start: no talk-request, no grant round trip.
    const sent = lastWS().sentJSON()
    expect(sent).toContainEqual({ type: 'talk-start' })
    expect(sent.some((m) => m.type === 'talk-request')).toBe(false)
    expect(viewer.talking).toBe(true)
  })

  it('does not renegotiate when the mic is turned on', async () => {
    const micTrack = makeTrack('audio')
    const getUserMedia = vi.fn(async () => ({ getAudioTracks: () => [micTrack] }) as unknown as MediaStream)
    const { viewer } = setup({ getUserMedia })
    joinRoom(viewer, { talkEnabled: true })
    await vi.waitFor(() => expect(viewer.state).toBe('negotiating'))
    const offersBefore = lastPC().createOffer.mock.calls.length

    await viewer.startTalking()

    // The whole point of the reserved sendrecv transceiver: no new offer.
    expect(lastPC().createOffer.mock.calls.length).toBe(offersBefore)
    expect(lastWS().sentJSON().filter((m) => m.type === 'offer')).toHaveLength(1)
  })

  it('turns the mic off by detaching the track', async () => {
    const micTrack = makeTrack('audio')
    const getUserMedia = vi.fn(async () => ({ getAudioTracks: () => [micTrack] }) as unknown as MediaStream)
    const { viewer } = setup({ getUserMedia })
    joinRoom(viewer, { talkEnabled: true })
    await viewer.startTalking()

    await viewer.stopTalking()

    expect(lastPC().transceivers[0].sender.track).toBeNull()
    expect(lastWS().sentJSON()).toContainEqual({ type: 'talk-end' })
    expect(viewer.talking).toBe(false)
  })

  it('reuses one microphone stream across on/off cycles', async () => {
    const micTrack = makeTrack('audio')
    const getUserMedia = vi.fn(async () => ({ getAudioTracks: () => [micTrack] }) as unknown as MediaStream)
    const { viewer } = setup({ getUserMedia })
    joinRoom(viewer, { talkEnabled: true })

    await viewer.startTalking()
    await viewer.stopTalking()
    await viewer.startTalking()

    // Asking for the mic twice would re-prompt on some platforms.
    expect(getUserMedia).toHaveBeenCalledOnce()
  })

  it('fails to start when the microphone is unavailable', async () => {
    const getUserMedia = vi.fn(async () => {
      throw new Error('NotAllowedError')
    })
    const { viewer } = setup({ getUserMedia })
    joinRoom(viewer, { talkEnabled: true })

    const ok = await viewer.startTalking()

    // The caller flips its toggle back on a false result.
    expect(ok).toBe(false)
    expect(viewer.talking).toBe(false)
    expect(lastWS().sentJSON().some((m) => m.type === 'talk-start')).toBe(false)
  })

  it('reports WHY the microphone could not be opened', async () => {
    const kinds: string[] = []
    const getUserMedia = vi.fn(async () => {
      throw { name: 'NotAllowedError' }
    })
    const viewer = new LiveViewer({
      token: 'tok123',
      name: 'n',
      callbacks: { onMicError: (k) => kinds.push(k) },
      wsFactory: (url) => new FakeWS(url) as unknown as WebSocket,
      pcFactory: (cfg) => new FakePC(cfg) as unknown as RTCPeerConnection,
      getUserMedia,
    })
    joinRoom(viewer, { talkEnabled: true })

    await viewer.startTalking()

    // "Denied" and "no device" need different fixes, so the page must be told
    // which one happened instead of showing one generic line for both.
    expect(kinds).toEqual(['denied'])
  })

  it('retries once when the device is still starting (just plugged in)', async () => {
    vi.useFakeTimers()
    try {
      const micTrack = makeTrack('audio')
      const getUserMedia = vi.fn()
        .mockRejectedValueOnce({ name: 'NotReadableError' })
        .mockResolvedValueOnce({ getAudioTracks: () => [micTrack] } as unknown as MediaStream)
      const { viewer } = setup({ getUserMedia })
      joinRoom(viewer, { talkEnabled: true })

      const pending = viewer.startTalking()
      await vi.advanceTimersByTimeAsync(400)

      await expect(pending).resolves.toBe(true)
      expect(getUserMedia).toHaveBeenCalledTimes(2)
    } finally {
      vi.useRealTimers()
    }
  })

  it('re-opens the CURRENT default after the device set changes', async () => {
    const fire = installMediaDevices()
    const first = makeTrack('audio')
    const second = makeTrack('audio')
    const getUserMedia = vi.fn()
      .mockResolvedValueOnce({ getAudioTracks: () => [first] } as unknown as MediaStream)
      .mockResolvedValueOnce({ getAudioTracks: () => [second] } as unknown as MediaStream)
    const { viewer } = setup({ getUserMedia })
    joinRoom(viewer, { talkEnabled: true })

    await viewer.startTalking()
    await viewer.stopTalking()

    // A headset was plugged in while the microphone was muted. The cached track
    // is still bound to the old device, so it must not be reused.
    fire()

    await viewer.startTalking()

    expect(getUserMedia).toHaveBeenCalledTimes(2)
    expect(first.stop).toHaveBeenCalled()
    expect(lastPC().transceivers[0].sender.track).toBe(second)
  })

  it('stops speaking when the device disappears mid-sentence', async () => {
    const lost: boolean[] = []
    const micTrack = makeTrack('audio')
    const getUserMedia = vi.fn(async () => ({ getAudioTracks: () => [micTrack] }) as unknown as MediaStream)
    const viewer = new LiveViewer({
      token: 'tok123',
      name: 'n',
      callbacks: { onMicLost: () => lost.push(true) },
      wsFactory: (url) => new FakeWS(url) as unknown as WebSocket,
      pcFactory: (cfg) => new FakePC(cfg) as unknown as RTCPeerConnection,
      getUserMedia,
    })
    joinRoom(viewer, { talkEnabled: true })
    await viewer.startTalking()

    // Headset unplugged: the OS ends the track. Leaving it attached would keep
    // the toggle "on" while the room hears silence.
    micTrack.fireEnded()

    await vi.waitFor(() => expect(viewer.talking).toBe(false))
    expect(lost).toEqual([true])
    // `stopTalking` reports AFTER detaching, so this is the frame that proves
    // the room was told, not just the local flag.
    await vi.waitFor(() =>
      expect(lastWS().sentJSON()).toContainEqual({ type: 'talk-end' }),
    )
  })

  it('stops sending when the publisher mutes us', async () => {
    const micTrack = makeTrack('audio')
    const getUserMedia = vi.fn(async () => ({ getAudioTracks: () => [micTrack] }) as unknown as MediaStream)
    const { viewer } = setup({ getUserMedia })
    joinRoom(viewer, { talkEnabled: true })
    await viewer.startTalking()

    lastWS().emit({ type: 'talk-revoked', reason: 'muted' })

    expect(viewer.talking).toBe(false)
    // The track must be detached locally too, not just flagged: otherwise the
    // microphone keeps feeding a stream nobody is listening to.
    await vi.waitFor(() => expect(lastPC().transceivers[0].sender.track).toBeNull())
  })

  it('learns about the switch being enabled after joining', () => {
    const enabled: boolean[] = []
    const { viewer } = setup({
      callbacks: { onTalkEnabled: (v: boolean) => enabled.push(v) },
    })
    // Joined while the switch was OFF: the toggle is not rendered at all.
    joinRoom(viewer, { talkEnabled: false })
    expect(viewer.talkEnabled).toBe(false)

    // The publisher enables it later. Without this frame an already-connected
    // viewer could never discover it (its `joined` is long gone).
    lastWS().emit({ type: 'talk-enabled', talkEnabled: true })

    expect(viewer.talkEnabled).toBe(true)
    // `joined` reports the initial (off) state; the later frame flips it on.
    expect(enabled).toEqual([false, true])
  })

  it('learns about AI 听记 being switched on after joining', () => {
    const states: boolean[] = []
    const { viewer } = setup({
      callbacks: { onNotesEnabled: (v: boolean) => states.push(v) },
    })
    // Joined while 听记 was off, so the transcript pane is not offered.
    joinRoom(viewer, { notesEnabled: false })
    expect(viewer.notesEnabled).toBe(false)

    // The publisher turns it on mid-meeting. `joined` only carries the state at
    // attach time, so this frame is the only way an earlier viewer finds out.
    lastWS().emit({ type: 'notes-enabled', notesEnabled: true })
    expect(viewer.notesEnabled).toBe(true)

    // Switching it off again must reach the pane too — that direction is why
    // the frame carries an explicit boolean instead of omitting the field.
    lastWS().emit({ type: 'notes-enabled', notesEnabled: false })
    expect(viewer.notesEnabled).toBe(false)
    expect(states).toEqual([false, true, false])
  })

  it('drops the microphone when the connection fails', async () => {
    const micTrack = makeTrack('audio')
    const getUserMedia = vi.fn(async () => ({ getAudioTracks: () => [micTrack] }) as unknown as MediaStream)
    const { viewer } = setup({ getUserMedia })
    joinRoom(viewer, { talkEnabled: true })
    await viewer.startTalking()

    const pc = lastPC()
    pc.connectionState = 'failed'
    pc.onconnectionstatechange?.()

    // A dead connection must not leave the OS microphone live.
    expect(viewer.talking).toBe(false)
    expect(pc.transceivers[0].sender.track).toBeNull()
    expect(micTrack.stop).toHaveBeenCalled()
  })

  it('flips back when the room switch is off (talk-disabled)', async () => {
    const revoked: string[] = []
    const { viewer } = setup({
      callbacks: { onTalkRevoked: (r: string) => revoked.push(r) },
    })
    joinRoom(viewer, { talkEnabled: true })

    // A stale UI whose toggle was enabled by an older `joined` tries to speak
    // after the publisher disabled the room switch.
    lastWS().emit({ type: 'talk-disabled', reason: 'disabled' })

    expect(revoked).toEqual(['disabled'])
    expect(viewer.talking).toBe(false)
  })

  it('reports a per-viewer mute separately from the room switch', () => {
    const muted: boolean[] = []
    const { viewer } = setup({
      callbacks: { onTalkMuted: (v: boolean) => muted.push(v) },
    })
    joinRoom(viewer, { talkEnabled: true })

    lastWS().emit({ type: 'talk-revoked', reason: 'host' })

    // The room still allows speech — only this viewer is blocked. Losing the
    // switch here would hide the button the host is about to re-enable.
    expect(viewer.talkEnabled).toBe(true)
    expect(muted).toEqual([true])

    lastWS().emit({ type: 'talk-unmuted' })
    expect(muted).toEqual([true, false])
  })

  it('ends with a distinct reason when the host removes us', () => {
    const reasons: Array<string | undefined> = []
    const { viewer } = setup({
      callbacks: {
        onStateChange: (_s: string, detail?: { reason?: string }) => reasons.push(detail?.reason),
      },
    })
    joinRoom(viewer, { talkEnabled: true })

    lastWS().emit({ type: 'kicked' })

    // "Removed" is not "the share ended": the page says so, and the socket is
    // closed rather than left half-open.
    expect(viewer.state).toBe('ended')
    expect(reasons).toContain('kicked')
    expect(lastWS().readyState).toBe(3)
  })
})

describe('LiveViewer cleanup', () => {
  it('closes the socket and peer connection', () => {
    const { viewer } = setup()
    joinRoom(viewer)
    const pc = lastPC()

    viewer.close()

    expect(pc.closed).toBe(true)
    expect(lastWS().readyState).toBe(3)
  })

  it('is idempotent', () => {
    const { viewer } = setup()
    joinRoom(viewer)

    viewer.close()
    viewer.close() // must not throw

    expect(viewer.state).toBe('idle')
  })

  it('stops the microphone on close', async () => {
    const micTrack = makeTrack('audio')
    const getUserMedia = vi.fn(async () => ({ getAudioTracks: () => [micTrack] }) as unknown as MediaStream)
    const { viewer } = setup({ getUserMedia })
    joinRoom(viewer, { talkEnabled: true })
    lastWS().emit({ type: 'talk-granted' })
    await viewer.startTalking()

    viewer.close()

    // A live microphone after the panel closes is a privacy bug.
    expect(micTrack.stop).toHaveBeenCalled()
  })

  it('ignores malformed frames instead of throwing', () => {
    const { viewer } = setup()
    viewer.connect()

    expect(() => lastWS().onmessage?.({ data: 'not json' })).not.toThrow()
    expect(viewer.state).toBe('connecting')
  })

  it('connect() is a no-op the second time', () => {
    const { viewer } = setup()
    viewer.connect()
    viewer.connect()

    expect(FakeWS.instances).toHaveLength(1)
  })
})

// ── Audience screen sharing ─────────────────────────────────────────────────

function setupWithScreen() {
  const screenStates: Array<{ enabled: boolean }> = []
  const sharers: string[] = []
  const memberStreams: Array<MediaStream | null> = []
  const denied: string[] = []
  const viewer = new LiveViewer({
    token: 'tok123',
    callbacks: {
      onScreenEnabled: (v) => screenStates.push({ enabled: v }),
      onScreenSharer: (id) => sharers.push(id),
      onMemberScreenStream: (s) => memberStreams.push(s),
      onScreenDenied: (r) => denied.push(r),
    },
    wsFactory: (url) => new FakeWS(url) as unknown as WebSocket,
    pcFactory: (cfg) => new FakePC(cfg) as unknown as RTCPeerConnection,
  })
  return { viewer, screenStates, sharers, memberStreams, denied }
}

function installDisplayMedia(track: MediaStreamTrack) {
  const getDisplayMedia = vi.fn(async () => ({ getVideoTracks: () => [track] }))
  Object.defineProperty(navigator, 'mediaDevices', {
    configurable: true,
    value: { getDisplayMedia },
  })
  return getDisplayMedia
}

describe('LiveViewer screen sharing', () => {
  beforeEach(() => {
    // jsdom's MediaStream rejects plain track objects; the viewer wraps the
    // relayed track into one, so a minimal double is enough here.
    vi.stubGlobal(
      'MediaStream',
      class {
        constructor(public tracks: unknown[] = []) {}
        getVideoTracks() {
          return this.tracks
        }
        getAudioTracks() {
          return []
        }
      },
    )
  })

  afterEach(() => {
    vi.unstubAllGlobals()
    // @ts-expect-error -- jsdom has no mediaDevices to restore.
    delete navigator.mediaDevices
  })

  it('attaches the screen with replaceTrack, without renegotiating', async () => {
    const track = makeTrack('video')
    installDisplayMedia(track)
    const { viewer } = setupWithScreen()
    joinRoom(viewer)
    await vi.waitFor(() => expect(viewer.state).toBe('negotiating'))
    const offersBefore = lastPC().createOffer.mock.calls.length

    const ok = await viewer.startScreenShare()

    expect(ok).toBe(true)
    expect(viewer.isSharingScreen).toBe(true)
    // m-line 2 is the screen line; replacing the track must not renegotiate.
    expect(lastPC().transceivers[1].sender.replaceTrack).toHaveBeenCalledWith(track)
    expect(lastPC().createOffer.mock.calls.length).toBe(offersBefore)
    expect(lastWS().sentJSON()).toContainEqual({ type: 'screen-start' })
  })

  it('caps the member capture and bounds its uplink', async () => {
    const track = makeTrack('video')
    const gdm = installDisplayMedia(track)
    const { viewer } = setupWithScreen()
    joinRoom(viewer)
    await viewer.startScreenShare()

    // The host decodes this and re-encodes it for everyone else, so the encoder
    // that has to be bounded is on the member's machine — the host cannot do it
    // from its side.
    expect(gdm).toHaveBeenCalledWith(
      expect.objectContaining({
        video: { width: { max: 1920 }, height: { max: 1200 }, frameRate: { max: 20 } },
      }),
    )
    expect((track as unknown as { contentHint?: string }).contentHint).toBe('text')
    await vi.waitFor(() =>
      expect(lastPC().transceivers[1].sender.params.encodings[0].maxBitrate).toBe(2_000_000),
    )
    expect(lastPC().transceivers[1].sender.params.encodings[0].maxFramerate).toBe(20)
  })

  it('stops when the browser ends the track (its own "stop sharing" bar)', async () => {
    const track = makeTrack('video')
    installDisplayMedia(track)
    const { viewer } = setupWithScreen()
    joinRoom(viewer)
    await viewer.startScreenShare()

    track.fireEnded()
    // The stop is reported after replaceTrack resolves, so wait for the frame
    // rather than for the synchronous flag.
    await vi.waitFor(() =>
      expect(lastWS().sentJSON()).toContainEqual({ type: 'screen-stop' }),
    )

    expect(viewer.isSharingScreen).toBe(false)
    expect(lastPC().transceivers[1].sender.replaceTrack).toHaveBeenLastCalledWith(null)
  })

  it('releases the local capture when the room denies the share', async () => {
    const track = makeTrack('video')
    installDisplayMedia(track)
    const { viewer, denied } = setupWithScreen()
    joinRoom(viewer)
    await viewer.startScreenShare()

    lastWS().emit({ type: 'screen-denied', reason: 'busy' })

    expect(viewer.isSharingScreen).toBe(false)
    expect(track.stop).toHaveBeenCalled()
    expect(denied).toEqual(['busy'])
  })

  it('reads the camera and host-screen flags from joined', () => {
    const cameraModes: boolean[] = []
    const screens: boolean[] = []
    const viewer = new LiveViewer({
      token: 'tok123',
      callbacks: {
        onCameraMode: (v) => cameraModes.push(v),
        onHostScreen: (v) => screens.push(v),
      },
      wsFactory: (url) => new FakeWS(url) as unknown as WebSocket,
      pcFactory: (cfg) => new FakePC(cfg) as unknown as RTCPeerConnection,
    })
    viewer.connect()
    lastWS().emit({ type: 'joined', viewerId: 'v1', video: true, camera: true, hostScreen: true })

    expect(cameraModes).toEqual([true])
    expect(screens).toEqual([true])
    expect(viewer.cameraEnabled).toBe(true)
    expect(viewer.hostScreen).toBe(true)
  })

  it('treats an older publisher as a screen share with no camera', () => {
    const cameraModes: boolean[] = []
    const screens: boolean[] = []
    const viewer = new LiveViewer({
      token: 'tok123',
      callbacks: {
        onCameraMode: (v) => cameraModes.push(v),
        onHostScreen: (v) => screens.push(v),
      },
      wsFactory: (url) => new FakeWS(url) as unknown as WebSocket,
      pcFactory: (cfg) => new FakePC(cfg) as unknown as RTCPeerConnection,
    })
    viewer.connect()
    // A frame from before video meetings existed: neither flag is present.
    lastWS().emit({ type: 'joined', viewerId: 'v1', video: true })

    expect(cameraModes).toEqual([false])
    expect(screens).toEqual([true], 'its picture has always been a screen')
  })

  it('follows the picture appearing and disappearing mid-meeting', () => {
    const videoModes: boolean[] = []
    const cameraModes: boolean[] = []
    const screens: boolean[] = []
    const viewer = new LiveViewer({
      token: 'tok123',
      callbacks: {
        onVideoMode: (v) => videoModes.push(v),
        onCameraMode: (v) => cameraModes.push(v),
        onHostScreen: (v) => screens.push(v),
      },
      wsFactory: (url) => new FakeWS(url) as unknown as WebSocket,
      pcFactory: (cfg) => new FakePC(cfg) as unknown as RTCPeerConnection,
    })
    joinRoom(viewer)
    expect(viewer.videoEnabled).toBe(true)

    // The host stops the screen and has no camera: no picture at all. All three
    // flags arrive in one frame because they describe one picture.
    lastWS().emit({ type: 'picture-state', video: false, camera: false, hostScreen: false })

    expect(videoModes.at(-1)).toBe(false)
    expect(cameraModes.at(-1)).toBe(false)
    expect(screens.at(-1)).toBe(false)
    expect(viewer.videoEnabled).toBe(false)
    expect(viewer.hostScreen).toBe(false)

    // ...and a camera turned on later brings the picture back, with no screen
    // behind it (so remote control and annotation stay off).
    lastWS().emit({ type: 'picture-state', video: true, camera: true, hostScreen: false })

    expect(videoModes.at(-1)).toBe(true)
    expect(cameraModes.at(-1)).toBe(true)
    expect(screens.at(-1)).toBe(false)
    expect(viewer.videoEnabled).toBe(true)
    expect(viewer.cameraEnabled).toBe(true)
  })

  // Remote control is the PUBLISHER's capability: injection happens on the host
  // machine and needs a whole-screen picture. A viewer cannot work it out, so it
  // must arrive from the server — and a viewer that joins after the host
  // advertised it still has to see it, since its `joined` frame is its only
  // source of truth.
  it('learns the remote-control capability from joined and from later changes', () => {
    const seen: boolean[] = []
    const viewer = new LiveViewer({
      token: 'tok123',
      callbacks: { onControlEnabled: (v) => seen.push(v) },
      wsFactory: (url) => new FakeWS(url) as unknown as WebSocket,
      pcFactory: (cfg) => new FakePC(cfg) as unknown as RTCPeerConnection,
    })
    viewer.connect()
    lastWS().emit({ type: 'joined', viewerId: 'v1', video: true, hostScreen: true, controlEnabled: true })

    expect(viewer.controlEnabled).toBe(true)
    expect(seen).toEqual([true])

    // The host swapped to a window capture (or stopped sharing): the pointer
    // could no longer be mapped onto a desktop, so the button must go away.
    lastWS().emit({ type: 'control-enabled', controlEnabled: false })
    expect(viewer.controlEnabled).toBe(false)
    expect(seen).toEqual([true, false])
  })

  it('defaults the capability to unavailable when the publisher never advertises it', () => {
    const seen: boolean[] = []
    const viewer = new LiveViewer({
      token: 'tok123',
      callbacks: { onControlEnabled: (v) => seen.push(v) },
      wsFactory: (url) => new FakeWS(url) as unknown as WebSocket,
      pcFactory: (cfg) => new FakePC(cfg) as unknown as RTCPeerConnection,
    })
    viewer.connect()
    // A frame with no `controlEnabled`: the safe reading is "cannot", because
    // the alternative is offering a button whose request can only be refused.
    lastWS().emit({ type: 'joined', viewerId: 'v1', video: true, hostScreen: true })

    expect(viewer.controlEnabled).toBe(false)
    expect(seen).toEqual([false])
  })

  it('reports who is sharing and the room switch', () => {
    const { viewer, screenStates, sharers } = setupWithScreen()
    joinRoom(viewer)

    lastWS().emit({ type: 'screen-state', screenEnabled: true, viewerId: 'v2', sharing: true })
    expect(screenStates.at(-1)).toEqual({ enabled: true })
    expect(sharers.at(-1)).toBe('v2')
    expect(viewer.screenSharer).toBe('v2')

    lastWS().emit({ type: 'screen-state', screenEnabled: true, viewerId: '', sharing: false })
    expect(sharers.at(-1)).toBe('')
  })

  it('does not surface the relay line until the room says somebody is sharing', () => {
    const { viewer, memberStreams } = setupWithScreen()
    joinRoom(viewer)

    // The relay m-line is negotiated at join and its receiver fires `ontrack`
    // with a MUTED track. Treating that as "someone is sharing" would black out
    // the host's picture for every viewer the moment they join.
    lastPC().ontrack?.({
      streams: [],
      track: makeTrack('video'),
      transceiver: lastPC().transceivers[2],
    } as never)
    expect(memberStreams).toHaveLength(0)

    lastWS().emit({ type: 'screen-state', screenEnabled: true, viewerId: 'v2', sharing: true })
    expect(memberStreams.at(-1)).not.toBeNull()

    lastWS().emit({ type: 'screen-state', screenEnabled: true, viewerId: '', sharing: false })
    expect(memberStreams.at(-1)).toBeNull()
  })

  it('reports false when the browser has no display-capture API', async () => {
    const { viewer } = setupWithScreen()
    joinRoom(viewer)

    // jsdom has no mediaDevices at all — the same situation as an http:// page.
    await expect(viewer.startScreenShare()).resolves.toBe(false)
    expect(viewer.isSharingScreen).toBe(false)
  })
})

// ── AI 听记 toggle ──────────────────────────────────────────────────────────

/**
 * The one gap the frame's happy-path test (above, "learns about AI 听记 being
 * switched on after joining") cannot cover: what happens when the key is
 * absent. The frame's entire meaning is that boolean, so a missing key must read
 * as "off" — never as "no news, keep what you had".
 */
describe('LiveViewer 听记 toggle', () => {
  it('reads a frame with the key missing as OFF, not as "unchanged"', () => {
    const seen: boolean[] = []
    const { viewer } = setup({ callbacks: { onNotesEnabled: (v: boolean) => seen.push(v) } })
    joinRoom(viewer)

    lastWS().emit({ type: 'notes-enabled', notesEnabled: true })
    expect(viewer.notesEnabled).toBe(true)

    // The server is supposed to send the key explicitly. If it ever stops doing
    // so (an `omitempty` creeping onto the Go struct), the honest reading is
    // "off" — a `msg.notesEnabled ?? this._notesEnabled` shape would instead
    // keep the pane open forever on a value nobody sent.
    lastWS().emit({ type: 'notes-enabled' })

    expect(viewer.notesEnabled).toBe(false)
    // First entry is `joined`'s initial state, as in the happy-path test.
    expect(seen).toEqual([false, true, false])
  })
})

describe('LiveViewer roster', () => {
  it('takes the participant list from the joined frame', () => {
    const seen: unknown[] = []
    const { viewer } = setup({ callbacks: { onRoster: (r: unknown) => seen.push(r) } })

    joinRoom(viewer, {
      roster: [
        { id: 'host', name: '泷泽', host: true },
        { id: 'v1', name: '小明' },
      ],
    })

    // The list rides `joined` so the panel is never empty for a viewer that
    // joins after everybody else.
    expect(viewer.roster).toHaveLength(2)
    expect(viewer.roster[0]).toMatchObject({ id: 'host', host: true, name: '泷泽' })
    expect(seen).toHaveLength(1)
  })

  it('replaces the list on every roster frame', () => {
    const seen: Array<Array<{ id: string }>> = []
    const { viewer } = setup({
      callbacks: { onRoster: (r: Array<{ id: string }>) => seen.push(r) },
    })
    joinRoom(viewer, { roster: [{ id: 'host', name: '泷泽', host: true }] })

    lastWS().emit({ type: 'roster', roster: [{ id: 'host', host: true }, { id: 'v2', name: '小红' }] })

    // Full snapshots, not deltas: the new list replaces the old one.
    expect(viewer.roster.map((m) => m.id)).toEqual(['host', 'v2'])
    expect(seen[seen.length - 1]).toHaveLength(2)
  })
})

// ── Quality readings ────────────────────────────────────────────────────────

/**
 * A meeting can carry three video m-lines at once (host picture, a forwarded
 * member screen, the camera tile). `getStats()` reports one `inbound-rtp` entry
 * per line, and the old code let whichever came LAST win — so with a camera on,
 * the badge described the 4:3 thumbnail instead of the picture being watched.
 */
describe('LiveViewer quality stats', () => {
  function videoStat(over: Record<string, unknown>): Record<string, unknown> {
    return { type: 'inbound-rtp', kind: 'video', ...over }
  }

  it('reads the host picture, not the camera tile or the relayed screen', async () => {
    const { viewer } = setup()
    joinRoom(viewer)
    const pc = lastPC()
    const [, screenTx, relayTx, cameraTx] = pc.transceivers

    pc.statsReport = new Map<string, Record<string, unknown>>([
      ['PAIR', { type: 'candidate-pair', state: 'succeeded', currentRoundTripTime: 0.05 }],
      ['MAIN', videoStat({ mid: screenTx.mid, bytesReceived: 1_000, packetsReceived: 100, packetsLost: 5, jitter: 0.01 })],
      ['RELAY', videoStat({ mid: relayTx.mid, bytesReceived: 5_000_000, packetsReceived: 1_000, packetsLost: 300, jitter: 0.2 })],
      ['CAMERA', videoStat({ mid: cameraTx.mid, bytesReceived: 9_000_000, packetsReceived: 2_000, packetsLost: 1_800, jitter: 0.5 })],
    ])

    const st = await viewer.getStats()

    // 5 lost of 105 — the picture's own numbers. The camera tile (last in the
    // report) would read 47.4% loss and 500ms of jitter.
    expect(st.lossPct).toBe(4.8)
    expect(st.jitterMs).toBe(10)
    expect(st.rttMs).toBe(50)
  })

  it('falls back to the track identity when the report carries no mid', async () => {
    const { viewer } = setup()
    joinRoom(viewer)
    const mainTrack = makeTrack('video')
    lastPC().ontrack?.({ streams: [{ id: 'main' } as unknown as MediaStream], track: mainTrack })

    lastPC().statsReport = new Map<string, Record<string, unknown>>([
      ['OTHER', videoStat({ trackIdentifier: 'someone-else', packetsReceived: 10, packetsLost: 90 })],
      ['MAIN', videoStat({ trackIdentifier: mainTrack.id, packetsReceived: 100, packetsLost: 0 })],
    ])

    const st = await viewer.getStats()
    expect(st.lossPct).toBe(0)
  })

  it('still reports when the browser identifies neither mid nor track', async () => {
    const { viewer } = setup()
    joinRoom(viewer)
    // Older implementations omit both fields. A blank badge is worse than a
    // reading off the wrong-but-plausible line, so the first entry is used.
    lastPC().statsReport = new Map<string, Record<string, unknown>>([
      ['A', videoStat({ packetsReceived: 100, packetsLost: 10, jitter: 0.02 })],
      ['B', videoStat({ packetsReceived: 100, packetsLost: 90, jitter: 0.9 })],
    ])

    const st = await viewer.getStats()
    expect(st.lossPct).toBe(9.1)
    expect(st.jitterMs).toBe(20)
  })
})

// ── Relay (SFU) mode ────────────────────────────────────────────────────────

/**
 * In a relayed room the media and the control bus live in LiveKit, and the
 * signalling socket carries room state only. These tests pin that split: no
 * PeerConnection, control frames on the relay's data channel, and the same
 * callbacks the peer-to-peer path fires.
 */
const RELAY = { url: 'wss://livekit.example.com:7880', token: 'jwt-1', room: 'tok123' }

interface RelayHandlers {
  onTrack?: (track: MediaStreamTrack, source: string, identity: string) => void
  onTrackGone?: (track: MediaStreamTrack) => void
  onData?: (payload: Uint8Array, from: string) => void
  onActiveSpeakers?: (identities: string[]) => void
  onDisconnected?: (reason: string) => void
}

function setupRelay(overrides: Record<string, unknown> = {}) {
  const states: Array<{ state: LiveViewerState; reason?: string }> = []
  const streams: MediaStream[] = []
  const cameraStreams: Array<MediaStream | null> = []
  const memberStreams: Array<MediaStream | null> = []
  const controls: unknown[] = []
  const rosters: Array<Array<{ id: string; talking?: boolean }>> = []
  const viewer = new LiveViewer({
    token: 'tok123',
    callbacks: {
      onStateChange: (state, detail) => states.push({ state, reason: detail?.reason }),
      onStream: (s) => streams.push(s),
      onCameraStream: (s) => cameraStreams.push(s),
      onMemberScreenStream: (s) => memberStreams.push(s),
      onControl: (m) => controls.push(m),
      onRoster: (r) => rosters.push(r as Array<{ id: string; talking?: boolean }>),
    },
    wsFactory: (url) => new FakeWS(url) as unknown as WebSocket,
    pcFactory: (cfg) => new FakePC(cfg) as unknown as RTCPeerConnection,
    ...overrides,
  })
  return { viewer, states, streams, cameraStreams, memberStreams, controls, rosters }
}

/** Emit the SFU `joined` and wait until the viewer has adopted the room. */
async function joinRelay(
  viewer: LiveViewer,
  extra: Record<string, unknown> = {},
): Promise<RelayHandlers> {
  viewer.connect()
  lastWS().emit({
    type: 'joined',
    viewerId: 'v1',
    mode: 'sfu',
    relay: RELAY,
    iceServers: [],
    ...extra,
  })
  await vi.waitFor(() => expect(relayMock.sendRelayData).toHaveBeenCalled())
  return relayMock.openRelay.mock.calls[0][2] as RelayHandlers
}

describe('LiveViewer relay mode', () => {
  let room: { name: string }

  beforeEach(() => {
    room = { name: 'livekit-room' }
    relayMock.openRelay.mockResolvedValue(room)
    // jsdom has no MediaStream; the viewer merges the host's tracks into one,
    // so the double has to answer the kind queries the merge relies on.
    vi.stubGlobal(
      'MediaStream',
      class {
        constructor(public tracks: MediaStreamTrack[] = []) {}
        getVideoTracks() {
          return this.tracks.filter((t) => t.kind === 'video')
        }
        getAudioTracks() {
          return this.tracks.filter((t) => t.kind === 'audio')
        }
      },
    )
  })

  afterEach(() => {
    vi.unstubAllGlobals()
    // @ts-expect-error -- jsdom has no mediaDevices to restore.
    delete navigator.mediaDevices
  })

  it('joins the relay instead of creating a PeerConnection', async () => {
    const { viewer } = setupRelay()

    await joinRelay(viewer)

    expect(relayMock.openRelay).toHaveBeenCalledWith(
      'wss://clawbench.test/livekit',
      RELAY.token,
      expect.any(Object),
    )
    // The whole point of the mode: no second transport, and no offer that
    // nobody would answer.
    expect(FakePC.instances).toHaveLength(0)
    expect(lastWS().sentJSON().some((m) => m.type === 'offer')).toBe(false)
  })

  it('asks for the transcript as soon as the room is up', async () => {
    const { viewer } = setupRelay()

    await joinRelay(viewer)

    // Peer-to-peer sends this from the data channel's `open`; a relayed room
    // has no such event, so it must be sent here or late joiners see an empty
    // chat panel forever.
    expect(relayMock.sendRelayData).toHaveBeenCalledWith(room, { t: 'chat-history-req' })
  })

  it('reaches playing when the host picture arrives, and merges its audio', async () => {
    const { viewer, states, streams } = setupRelay()
    const handlers = await joinRelay(viewer)
    expect(viewer.state).toBe('connecting')

    const video = makeTrack('video')
    handlers.onTrack?.(video, 'screen_share', 'host')

    expect(states.at(-1)?.state).toBe('playing')
    expect(streams).toHaveLength(1)
    expect(streams[0].getVideoTracks()).toEqual([video])

    // The relay delivers the mixed audio as its own track; the page renders a
    // single <video>, so both must end up in the same stream.
    const audio = makeTrack('audio')
    handlers.onTrack?.(audio, 'microphone', 'host')

    expect(streams.at(-1)?.getVideoTracks()).toEqual([video])
    expect(streams.at(-1)?.getAudioTracks()).toEqual([audio])
  })

  it('routes the host camera to its own callback, not the main picture', async () => {
    const { viewer, streams, cameraStreams } = setupRelay()
    const handlers = await joinRelay(viewer)

    handlers.onTrack?.(makeTrack('video'), 'camera', 'host')

    expect(cameraStreams).toHaveLength(1)
    expect(streams).toEqual([])
  })

  it('routes another viewer screen to the member callback', async () => {
    const { viewer, memberStreams } = setupRelay()
    const handlers = await joinRelay(viewer)

    handlers.onTrack?.(makeTrack('video'), 'screen_share', 'v2')

    expect(memberStreams).toHaveLength(1)
    expect(memberStreams[0]?.getVideoTracks()).toHaveLength(1)
  })

  it('clears the tile callbacks when a track goes away', async () => {
    const { viewer, cameraStreams, memberStreams } = setupRelay()
    const handlers = await joinRelay(viewer)
    const camera = makeTrack('video')
    const member = makeTrack('video')
    handlers.onTrack?.(camera, 'camera', 'host')
    handlers.onTrack?.(member, 'screen_share', 'v2')

    handlers.onTrackGone?.(camera)
    handlers.onTrackGone?.(member)

    expect(cameraStreams.at(-1)).toBeNull()
    expect(memberStreams.at(-1)).toBeNull()
  })

  it('sends control frames over the relay data channel', async () => {
    const { viewer } = setupRelay()
    await joinRelay(viewer)
    relayMock.sendRelayData.mockClear()

    const ok = viewer.sendControl({ t: 'ctrl-req' })

    expect(ok).toBe(true)
    expect(relayMock.sendRelayData).toHaveBeenCalledWith(room, { t: 'ctrl-req' })
  })

  it('parses control frames arriving on the relay data channel', async () => {
    const { viewer, controls } = setupRelay()
    const handlers = await joinRelay(viewer)

    handlers.onData?.(
      new TextEncoder().encode(
        JSON.stringify({ t: 'chat', id: 'c1', from: 'host', name: '主持人', text: 'hi', at: 1 }),
      ),
      'host',
    )
    expect(controls).toEqual([expect.objectContaining({ t: 'chat', text: 'hi' })])

    // A malformed frame must be dropped, not thrown into the render path.
    handlers.onData?.(new TextEncoder().encode('not json'), 'host')
    expect(controls).toHaveLength(1)
  })

  it('folds who is speaking into the roster', async () => {
    const { viewer, rosters } = setupRelay()
    const handlers = await joinRelay(viewer, {
      roster: [
        { id: 'host', name: '主持人', host: true },
        { id: 'v1', name: '我' },
        { id: 'v2', name: '小明' },
      ],
    })

    handlers.onActiveSpeakers?.(['v2'])

    expect(viewer.activeSpeakers).toEqual(['v2'])
    // The relay ids are the roster ids, so the existing callback carries it —
    // no new API for the panel to keep in step.
    expect(viewer.roster.find((m) => m.id === 'v2')?.talking).toBe(true)
    expect(viewer.roster.find((m) => m.id === 'host')?.talking).toBe(false)
    expect(rosters.at(-1)).toHaveLength(3)
  })

  it('publishes the microphone instead of replacing a transceiver track', async () => {
    const micTrack = makeTrack('audio')
    const getUserMedia = vi.fn(
      async () => ({ getAudioTracks: () => [micTrack] }) as unknown as MediaStream,
    )
    const { viewer } = setupRelay({ getUserMedia })
    await joinRelay(viewer)

    const ok = await viewer.startTalking()

    expect(ok).toBe(true)
    expect(relayMock.publishMic).toHaveBeenCalledWith(room, micTrack)
    // The server still owns the room's talk switch, so it is still told.
    expect(lastWS().sentJSON()).toContainEqual({ type: 'talk-start' })
  })

  it('publishes our own screen instead of attaching it to a sender', async () => {
    const track = makeTrack('video')
    installDisplayMedia(track)
    const { viewer } = setupRelay()
    await joinRelay(viewer)

    const ok = await viewer.startScreenShare()

    expect(ok).toBe(true)
    expect(relayMock.publishVideo).toHaveBeenCalledWith(room, track, 'screen_share')
    expect(viewer.isSharingScreen).toBe(true)
    expect(lastWS().sentJSON()).toContainEqual({ type: 'screen-start' })
  })

  it('reports a failure and releases the room when the relay drops', async () => {
    const { viewer, states } = setupRelay()
    const handlers = await joinRelay(viewer)
    handlers.onTrack?.(makeTrack('video'), 'screen_share', 'host')
    expect(viewer.state).toBe('playing')

    handlers.onDisconnected?.('server')

    expect(viewer.state).toBe('failed')
    expect(states.at(-1)).toEqual({ state: 'failed', reason: 'relay' })
    expect(relayMock.closeRelay).toHaveBeenCalledWith(room)
  })

  it('disconnects the room on teardown, and does not mistake that for a failure', async () => {
    const { viewer } = setupRelay()
    const handlers = await joinRelay(viewer)

    viewer.close()
    expect(relayMock.closeRelay).toHaveBeenCalledWith(room)

    // LiveKit reports the close we just asked for through the same callback.
    // Acting on it would turn every clean exit into `failed`.
    handlers.onDisconnected?.('client')
    expect(viewer.state).toBe('idle')
    expect(relayMock.closeRelay).toHaveBeenCalledTimes(1)
  })

  it('releases a room that arrives after the page was closed', async () => {
    const { viewer } = setupRelay()
    viewer.connect()
    lastWS().emit({ type: 'joined', viewerId: 'v1', mode: 'sfu', relay: RELAY })

    // Close while the relay handshake is still in flight.
    viewer.close()

    await vi.waitFor(() => expect(relayMock.closeRelay).toHaveBeenCalledWith(room))
    expect(viewer.state).toBe('idle')
  })
})
