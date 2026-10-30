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
  replaceTrack = vi.fn(async (t: MediaStreamTrack | null) => {
    this.track = t
  })
}

class FakeTransceiver {
  sender = new FakeSender()
  constructor(public direction: string) {}
}

class FakePC {
  static instances: FakePC[] = []
  ontrack: ((ev: { streams: MediaStream[]; track: MediaStreamTrack }) => void) | null = null
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
    this.transceivers.push(tx)
    return tx as unknown as RTCRtpTransceiver
  }

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

function makeTrack(kind = 'audio'): MediaStreamTrack & { fireEnded: () => void } {
  const listeners = new Map<string, Array<() => void>>()
  const track = {
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
function joinRoom(viewer: LiveViewer, opts: { talkEnabled?: boolean } = {}) {
  viewer.connect()
  lastWS().emit({
    type: 'joined',
    viewerId: 'v1',
    title: '测试共享',
    iceServers: [],
    talkEnabled: opts.talkEnabled ?? false,
  })
}

beforeEach(() => {
  FakePC.instances = []
  FakeWS.instances = []
})

afterEach(() => {
  vi.restoreAllMocks()
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

  it('reserves three m-lines in the agreed order', () => {
    const { viewer } = setup()
    joinRoom(viewer)

    const kinds = lastPC().transceivers.map((t) => t.direction)
    // 1 audio (mic up / mix down), 2 video (host picture down / our screen up),
    // 3 video (another viewer's screen down). The publisher adds the same three
    // in the same order — this ordering IS the contract.
    expect(kinds).toEqual(['sendrecv', 'sendrecv', 'recvonly'])
    // Every sender starts empty: tracks are attached with replaceTrack, which
    // does not renegotiate.
    expect(lastPC().transceivers[0].sender.track).toBeNull()
    expect(lastPC().transceivers[1].sender.track).toBeNull()
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
