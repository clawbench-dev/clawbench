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
}))

vi.mock('@/utils/clawbenchNative', () => ({
  getNative: () => bridge,
}))

class FakeTrack {
  kind: string
  id: string
  stopped = false
  private endedHandlers: Array<() => void> = []
  constructor(kind: string, id: string) {
    this.kind = kind
    this.id = id
  }
  stop() {
    this.stopped = true
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
}

class FakeSender {
  track: FakeTrack | null = null
  replaceTrack = vi.fn(async (t: FakeTrack | null) => {
    this.track = t
  })
}

class FakeTransceiver {
  sender = new FakeSender()
  receiver = { track: null as FakeTrack | null }
  constructor(public direction: string) {}
}

class FakePC {
  static instances: FakePC[] = []
  ontrack: ((ev: { track: FakeTrack; streams: FakeStream[] }) => void) | null = null
  onicecandidate: ((ev: { candidate: { toJSON: () => unknown } | null }) => void) | null = null
  onconnectionstatechange: (() => void) | null = null
  connectionState = 'new'
  transceivers: FakeTransceiver[] = []
  tracks: FakeTrack[] = []
  remoteDescriptions: unknown[] = []
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
  createAnswer = vi.fn(async () => ({ type: 'answer', sdp: 'v=0-answer' }))
  setLocalDescription = vi.fn(async (d: unknown) => {
    this.localDescription = d
  })
  setRemoteDescription = vi.fn(async (d: unknown) => {
    this.remoteDescriptions.push(d)
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
}

const displayStream = new FakeStream([new FakeTrack('video', 'v1'), new FakeTrack('audio', 'sys1')])
const micStream = new FakeStream([new FakeTrack('audio', 'mic1')])

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
  vi.stubGlobal('WebSocket', FakeWS as unknown as typeof WebSocket)
  vi.stubGlobal('RTCPeerConnection', FakePC as unknown as typeof RTCPeerConnection)
  vi.stubGlobal('AudioContext', FakeAudioCtx as unknown as typeof AudioContext)
  // `new MediaStream([track])` runs on every viewer join; without this stub it
  // throws and silently aborts the rest of that handler (no ICE, no state hook).
  vi.stubGlobal('MediaStream', FakeMediaStream as unknown as typeof MediaStream)
  vi.stubGlobal('navigator', {
    mediaDevices: {
      getDisplayMedia: vi.fn(async () => displayStream),
      getUserMedia: vi.fn(async () => micStream),
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

  const mod = await import('../useLiveShare')
  mod._resetLiveShareForTest()
})

afterEach(() => {
  vi.unstubAllGlobals()
  vi.restoreAllMocks()
})

async function startSharing() {
  const { useLiveShare } = await import('../useLiveShare')
  const share = useLiveShare()
  const ok = await share.start({ sourceId: 'screen:1:0', systemAudio: true, microphone: true })
  return { share, ok }
}

function lastWS(): FakeWS {
  return FakeWS.instances[FakeWS.instances.length - 1]
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

  it('fails cleanly when the room cannot be created', async () => {
    fetchMock.mockResolvedValueOnce({ ok: false, status: 503, json: async () => ({}) })
    const { share, ok } = await startSharing()

    expect(ok).toBe(false)
    expect(share.state.value).toBe('idle')
    // No real source was ever declared (the teardown path clears with '').
    expect(bridge.setPendingCaptureSource).not.toHaveBeenCalledWith('screen:1:0', expect.anything())
  })

  it('fails when the host cannot declare a capture source', async () => {
    const native = await import('@/utils/clawbenchNative')
    const spy = vi.spyOn(native, 'getNative').mockReturnValue({} as never)

    const { share, ok } = await startSharing()

    expect(ok).toBe(false)
    expect(share.state.value).toBe('idle')
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
    const again = await share.start({ sourceId: 'x', systemAudio: false, microphone: false })

    expect(again).toBe(false)
    expect(FakeWS.instances).toHaveLength(1)
  })
})

describe('viewers', () => {
  it('builds one PeerConnection per viewer with video and mixed audio', async () => {
    const { share } = await startSharing()

    lastWS().emit({ type: 'viewer-join', viewerId: 'v1' })
    await vi.waitFor(() => expect(FakePC.instances).toHaveLength(1))

    const pc = FakePC.instances[0]
    // m-line order is a contract with the viewer: audio, then video, then the
    // relay m-line. The audio track comes first because it is the viewer's
    // microphone slot.
    expect(pc.tracks.map((t) => t.kind)).toEqual(['audio', 'video'])
    // CRITICAL: both tracks must belong to the SAME MediaStream. The viewer
    // assigns `srcObject` per received stream, so two streams would mean the
    // second assignment replaces the first — the viewer would get either video
    // or audio, never both.
    expect(pc.addedStreams).toHaveLength(2)
    expect(pc.addedStreams[0]).toBe(pc.addedStreams[1])
    // Three m-lines: audio (mic up), video (our picture down / their screen
    // up), video (relayed member screen down). All reserved up front so that
    // sharing never renegotiates.
    expect(pc.transceivers.map((t) => t.direction)).toEqual(['sendrecv', 'sendrecv', 'sendonly'])
    expect(share.viewers.value).toHaveLength(1)
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

  it('stops when the user ends capture from the browser banner', async () => {
    const { share } = await startSharing()

    // The video track ending is what Chromium fires when the user clicks the
    // "Stop sharing" bar; ignoring it would leave the app publishing a dead feed.
    displayStream.getVideoTracks()[0].emitEnded()

    await vi.waitFor(() => expect(share.state.value).toBe('idle'))
  })

  it('stops when the publisher socket closes', async () => {
    const { share } = await startSharing()
    await vi.waitFor(() => expect(share.state.value).toBe('publishing'))

    lastWS().onclose?.()

    await vi.waitFor(() => expect(share.state.value).toBe('idle'))
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

describe('getAudioCaptureSupport', () => {
  it('reports what the host says', async () => {
    bridge.getAudioCaptureSupport.mockResolvedValueOnce(JSON.stringify({ systemAudio: true }))
    const { getAudioCaptureSupport } = await import('../useLiveShare')

    expect(await getAudioCaptureSupport()).toBe(true)
  })

  it('defaults to false when unsupported', async () => {
    const native = await import('@/utils/clawbenchNative')
    const spy = vi.spyOn(native, 'getNative').mockReturnValue({} as never)
    const { getAudioCaptureSupport } = await import('../useLiveShare')

    expect(await getAudioCaptureSupport()).toBe(false)
    spy.mockRestore()
  })
})

describe('audience screen relay', () => {
  it('forwards a viewer screen to everyone else and never back to the sharer', async () => {
    const { share } = await startSharing()
    lastWS().emit({ type: 'viewer-join', viewerId: 'v1' })
    lastWS().emit({ type: 'viewer-join', viewerId: 'v2' })
    await vi.waitFor(() => expect(FakePC.instances).toHaveLength(2))

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
    lastWS().emit({ type: 'viewer-join', viewerId: 'v1' })
    lastWS().emit({ type: 'viewer-join', viewerId: 'v2' })
    await vi.waitFor(() => expect(FakePC.instances).toHaveLength(2))

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
    lastWS().emit({ type: 'viewer-join', viewerId: 'v1' })
    await vi.waitFor(() => expect(FakePC.instances).toHaveLength(1))
    const screen = new FakeTrack('video', 'screen-1')
    lastWS().emit({ type: 'screen-state', screenEnabled: true, viewerId: 'v1', sharing: true })
    FakePC.instances[0].ontrack?.({ track: screen, streams: [] })

    lastWS().emit({ type: 'viewer-join', viewerId: 'v2' })
    await vi.waitFor(() => expect(FakePC.instances).toHaveLength(2))

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
