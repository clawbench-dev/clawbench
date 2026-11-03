import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { LiveViewer, liveSignalUrl, type LiveViewerState } from '../liveViewer'

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

function setup(overrides: Record<string, unknown> = {}) {
  const states: Array<{ state: LiveViewerState; reason?: string }> = []
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
})

afterEach(() => {
  vi.restoreAllMocks()
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
        video: { width: { max: 1920 }, height: { max: 1200 }, frameRate: { max: 15 } },
      }),
    )
    expect((track as unknown as { contentHint?: string }).contentHint).toBe('text')
    await vi.waitFor(() =>
      expect(lastPC().transceivers[1].sender.params.encodings[0].maxBitrate).toBe(2_500_000),
    )
    expect(lastPC().transceivers[1].sender.params.encodings[0].maxFramerate).toBe(15)
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
