import { describe, it, expect, vi, afterEach } from 'vitest'
import { LiveMixer } from '../useLiveMixer'

/**
 * Mixer tests.
 *
 * jsdom has no Web Audio, so the fakes below model the graph as adjacency lists.
 * That is enough to assert the ONE invariant that matters — a viewer's
 * destination never receives that viewer's own audio — plus the wiring rules
 * around add/remove/re-attach. An OfflineAudioContext FFT check would be
 * stronger but is not available here; the numbers it produced are recorded in
 * the design doc §8.3.2.
 */

interface FakeNode {
  id: string
  outputs: FakeNode[]
}

class FakeSource implements FakeNode {
  id: string
  outputs: FakeNode[] = []
  constructor(public stream: { id: string }) {
    this.id = `src:${stream.id}`
  }
  connect(dest: FakeNode) {
    this.outputs.push(dest)
  }
  disconnect(dest?: FakeNode) {
    if (dest) this.outputs = this.outputs.filter((d) => d !== dest)
    else this.outputs = []
  }
}

class FakeDest implements FakeNode {
  id: string
  outputs: FakeNode[] = []
  track: { id: string; kind: string } = { id: '', kind: 'audio' }
  /** Real destinations expose `stream`; the mixer reads the track from it. */
  stream: { getAudioTracks: () => unknown[] }
  constructor(n: number) {
    this.id = `dest:${n}`
    this.track = { id: `track-${n}`, kind: 'audio' }
    this.stream = { getAudioTracks: () => [this.track] }
  }
  connect() {}
  disconnect() {}
}

class FakeCtx {
  dests: FakeDest[] = []
  sources: FakeSource[] = []
  createMediaStreamDestination(): AudioDestinationNode {
    const d = new FakeDest(this.dests.length)
    this.dests.push(d)
    return d as unknown as AudioDestinationNode
  }
  createMediaStreamSource(stream: MediaStream): MediaStreamAudioSourceNode {
    const s = new FakeSource({ id: (stream as unknown as { id: string }).id })
    this.sources.push(s)
    return s as unknown as MediaStreamAudioSourceNode
  }
}

function stream(id: string, audioTracks = 1): MediaStream {
  return {
    id,
    getAudioTracks: () => Array.from({ length: audioTracks }, () => ({ id: `${id}-t`, kind: 'audio' })),
  } as unknown as MediaStream
}

function destFor(ctx: FakeCtx, trackId: string): FakeDest {
  const d = ctx.dests.find((x) => x.track.id === trackId)
  if (!d) throw new Error(`no destination for ${trackId}`)
  return d
}

/** True when `dest` is fed by the source that carries `streamId`. */
function fedBy(dest: FakeDest, ctx: FakeCtx, streamId: string): boolean {
  return ctx.sources.some((s) => s.id === `src:${streamId}` && s.outputs.includes(dest))
}

afterEach(() => vi.restoreAllMocks())

describe('LiveMixer', () => {
  it('feeds system audio and mic into every viewer destination', () => {
    const ctx = new FakeCtx()
    const mixer = new LiveMixer(ctx as unknown as AudioContext, stream('sys'), stream('mic'))

    const trackA = mixer.addViewer('A')
    const destA = destFor(ctx, trackA.id)

    expect(fedBy(destA, ctx, 'sys')).toBe(true)
    expect(fedBy(destA, ctx, 'mic')).toBe(true)
  })

  it('does not feed a viewer its own audio', () => {
    const ctx = new FakeCtx()
    const mixer = new LiveMixer(ctx as unknown as AudioContext, stream('sys'), null)

    const trackA = mixer.addViewer('A')
    mixer.addViewer('B')
    mixer.attachRemote('A', stream('a-voice'))
    mixer.attachRemote('B', stream('b-voice'))

    // THE invariant: A must never hear A, B must never hear B.
    expect(fedBy(destFor(ctx, trackA.id), ctx, 'a-voice')).toBe(false)
    expect(fedBy(destFor(ctx, mixer.outTrackFor('B')!.id), ctx, 'b-voice')).toBe(false)
  })

  it('feeds each viewer the other viewers audio', () => {
    const ctx = new FakeCtx()
    const mixer = new LiveMixer(ctx as unknown as AudioContext, null, null)

    const trackA = mixer.addViewer('A')
    const trackB = mixer.addViewer('B')
    mixer.attachRemote('A', stream('a-voice'))
    mixer.attachRemote('B', stream('b-voice'))

    expect(fedBy(destFor(ctx, trackA.id), ctx, 'b-voice')).toBe(true)
    expect(fedBy(destFor(ctx, trackB.id), ctx, 'a-voice')).toBe(true)
  })

  it('wires a viewer that arrives after the others already speak', () => {
    const ctx = new FakeCtx()
    const mixer = new LiveMixer(ctx as unknown as AudioContext, null, null)

    mixer.addViewer('A')
    mixer.attachRemote('A', stream('a-voice'))

    // B joins later: A's audio must reach B's destination, and B's own must not.
    const trackB = mixer.addViewer('B')
    mixer.attachRemote('B', stream('b-voice'))

    expect(fedBy(destFor(ctx, trackB.id), ctx, 'a-voice')).toBe(true)
    expect(fedBy(destFor(ctx, trackB.id), ctx, 'b-voice')).toBe(false)
  })

  it('keeps the destination track stable across viewer churn', () => {
    const ctx = new FakeCtx()
    const mixer = new LiveMixer(ctx as unknown as AudioContext, null, null)

    mixer.addViewer('A')
    const before = mixer.outTrackFor('A')!.id

    mixer.addViewer('B')
    mixer.attachRemote('B', stream('b-voice'))
    mixer.removeViewer('B')

    // A stable track id means the publisher never has to call replaceTrack
    // again — the sender keeps pointing at the same destination.
    expect(mixer.outTrackFor('A')!.id).toBe(before)
  })

  it('stops routing a removed viewer to everyone else', () => {
    const ctx = new FakeCtx()
    const mixer = new LiveMixer(ctx as unknown as AudioContext, null, null)

    const trackA = mixer.addViewer('A')
    mixer.addViewer('B')
    mixer.attachRemote('B', stream('b-voice'))
    expect(fedBy(destFor(ctx, trackA.id), ctx, 'b-voice')).toBe(true)

    mixer.removeViewer('B')

    expect(fedBy(destFor(ctx, trackA.id), ctx, 'b-voice')).toBe(false)
  })

  it('addViewer is idempotent', () => {
    const ctx = new FakeCtx()
    const mixer = new LiveMixer(ctx as unknown as AudioContext, null, null)

    const first = mixer.addViewer('A')
    const second = mixer.addViewer('A')

    expect(second.id).toBe(first.id)
    // A second destination would double the viewer's audio and leak a node.
    expect(ctx.dests).toHaveLength(1)
    expect(mixer.size).toBe(1)
  })

  it('re-attaching a remote stream replaces the old source', () => {
    const ctx = new FakeCtx()
    const mixer = new LiveMixer(ctx as unknown as AudioContext, null, null)

    const trackA = mixer.addViewer('A')
    mixer.addViewer('B')
    mixer.attachRemote('B', stream('b-old'))
    mixer.attachRemote('B', stream('b-new'))

    const destA = destFor(ctx, trackA.id)
    expect(fedBy(destA, ctx, 'b-old')).toBe(false)
    expect(fedBy(destA, ctx, 'b-new')).toBe(true)
  })

  it('ignores attachRemote for an unknown viewer', () => {
    const ctx = new FakeCtx()
    const mixer = new LiveMixer(ctx as unknown as AudioContext, null, null)

    // A late ontrack after the viewer left must not resurrect a slot.
    expect(() => mixer.attachRemote('ghost', stream('x'))).not.toThrow()
    expect(mixer.size).toBe(0)
  })

  it('ignores a remote stream with no audio tracks', () => {
    const ctx = new FakeCtx()
    const mixer = new LiveMixer(ctx as unknown as AudioContext, null, null)

    const trackA = mixer.addViewer('A')
    mixer.addViewer('B')
    mixer.attachRemote('B', stream('b-voice', 0))

    expect(fedBy(destFor(ctx, trackA.id), ctx, 'b-voice')).toBe(false)
  })

  it('works with no system audio and no microphone', () => {
    const ctx = new FakeCtx()
    const mixer = new LiveMixer(ctx as unknown as AudioContext, null, null)

    // A viewer-only graph is valid: the audience still hears each other.
    const trackA = mixer.addViewer('A')
    mixer.addViewer('B')
    mixer.attachRemote('B', stream('b-voice'))

    expect(fedBy(destFor(ctx, trackA.id), ctx, 'b-voice')).toBe(true)
  })

  it('treats a stream with zero tracks as absent', () => {
    const ctx = new FakeCtx()
    // getUserMedia can resolve with a stream that has no audio track (device
    // busy); createMediaStreamSource would throw on it.
    const mixer = new LiveMixer(ctx as unknown as AudioContext, stream('sys', 0), stream('mic', 0))

    expect(() => mixer.addViewer('A')).not.toThrow()
    expect(ctx.sources).toHaveLength(0)
  })

  it('close() removes every viewer', () => {
    const ctx = new FakeCtx()
    const mixer = new LiveMixer(ctx as unknown as AudioContext, stream('sys'), null)
    mixer.addViewer('A')
    mixer.addViewer('B')

    mixer.close()

    expect(mixer.size).toBe(0)
    expect(mixer.outTrackFor('A')).toBeNull()
  })

  it('outTrackFor returns null for an unknown viewer', () => {
    const ctx = new FakeCtx()
    const mixer = new LiveMixer(ctx as unknown as AudioContext, null, null)

    expect(mixer.outTrackFor('nope')).toBeNull()
  })
})
