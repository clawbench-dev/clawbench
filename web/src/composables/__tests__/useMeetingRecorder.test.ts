import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import {
  _resetMeetingRecorderForTest,
  _setRecordingRecorderForTest,
  _setRecordingSurfaceForTest,
  useMeetingRecorder,
  type RecordingSurface,
} from '../useMeetingRecorder'

class FakeStream {
  constructor(private readonly tracks: Array<{ kind: string }> = []) {}
  getTracks(): Array<{ kind: string }> {
    return this.tracks
  }
  getVideoTracks(): Array<{ kind: string }> {
    return this.tracks.filter((t) => t.kind === 'video')
  }
  getAudioTracks(): Array<{ kind: string }> {
    return this.tracks.filter((t) => t.kind === 'audio')
  }
}

class FakeRecorder {
  static instances: FakeRecorder[] = []
  state: 'inactive' | 'recording' = 'inactive'
  ondataavailable: ((ev: { data: Blob }) => void) | null = null
  onstop: (() => void) | null = null
  onerror: (() => void) | null = null
  readonly mime: string

  constructor(public readonly stream: unknown, mime = '') {
    this.mime = mime
    FakeRecorder.instances.push(this)
  }

  start(_timeslice?: number): void {
    this.state = 'recording'
  }

  stop(): void {
    this.state = 'inactive'
    // MediaRecorder delivers the final chunk before `stop`, which is exactly
    // the ordering the composable has to preserve.
    this.ondataavailable?.({ data: new Blob(['tail']) })
    this.onstop?.()
  }

  emit(text: string): void {
    this.ondataavailable?.({ data: new Blob([text]) })
  }
}

function surface(): RecordingSurface {
  return {
    video: new FakeStream([{ kind: 'video' }]) as unknown as MediaStream,
    draw: vi.fn(),
    follow: vi.fn(),
    dispose: vi.fn(),
  }
}

/** Let the queued uploads' microtasks run. */
async function settle(): Promise<void> {
  for (let i = 0; i < 6; i++) await Promise.resolve()
  await new Promise((resolve) => setTimeout(resolve, 0))
}

interface Upload {
  seq: number
  body: Blob
}

function recordingUploads(): Upload[] {
  const calls = (fetch as unknown as { mock: { calls: unknown[][] } }).mock.calls
  return calls
    .filter(([url]) => String(url).includes('/api/live-share/notes/recording?'))
    .map(([url, init]) => {
      const params = new URL(String(url), 'http://x').searchParams
      return { seq: Number(params.get('seq')), body: (init as RequestInit).body as Blob }
    })
}

function finished(): boolean {
  const calls = (fetch as unknown as { mock: { calls: unknown[][] } }).mock.calls
  return calls.some(([url]) => String(url).endsWith('/api/live-share/notes/recording/finish'))
}

describe('useMeetingRecorder', () => {
  beforeEach(() => {
    vi.stubGlobal('MediaStream', FakeStream)
    vi.stubGlobal('MediaRecorder', class {})
    FakeRecorder.instances = []
    _resetMeetingRecorderForTest()
    _setRecordingSurfaceForTest(async () => surface())
    _setRecordingRecorderForTest(
      (stream, mime) => new FakeRecorder(stream, mime) as unknown as MediaRecorder,
    )
    vi.stubGlobal(
      'fetch',
      vi.fn(async (url: string, init?: RequestInit) => {
        if (String(url).endsWith('/recording/finish')) {
          return {
            ok: true,
            status: 200,
            json: async () => ({ fileName: 'recording/1700000000000-abc.webm', bytes: 42 }),
          }
        }
        const size = (init?.body as Blob | undefined)?.size ?? 0
        return { ok: true, status: 200, json: async () => ({ bytes: size, nextSeq: 1 }) }
      }),
    )
  })

  afterEach(() => {
    _resetMeetingRecorderForTest()
    _setRecordingSurfaceForTest(null)
    _setRecordingRecorderForTest(null)
    vi.unstubAllGlobals()
  })

  function source(): MediaStream {
    return new FakeStream([{ kind: 'video' }]) as unknown as MediaStream
  }

  it('records the picture, uploads chunks in order and reports the saved file', async () => {
    const recorder = useMeetingRecorder()
    const ok = await recorder.start({
      token: 'tok',
      getSource: source,
      getStrokes: () => [],
      getAudio: () => new FakeStream([{ kind: 'audio' }]) as unknown as MediaStream,
    })

    expect(ok).toBe(true)
    expect(recorder.active.value).toBe(true)
    expect(FakeRecorder.instances).toHaveLength(1)
    const rec = FakeRecorder.instances[0]
    // The recording carries the meeting's audio alongside the canvas picture.
    expect((rec.stream as FakeStream).getAudioTracks()).toHaveLength(1)

    rec.emit('one')
    await settle()
    rec.emit('two')
    await settle()

    const uploads = recordingUploads()
    expect(uploads.map((u) => u.seq)).toEqual([0, 1])
    expect(await uploads[0].body.text()).toBe('one')
    expect(await uploads[1].body.text()).toBe('two')

    await recorder.stop()

    expect(rec.state).toBe('inactive')
    expect(recorder.active.value).toBe(false)
    expect(finished()).toBe(true)
    expect(recorder.savedName.value).toBe('recording/1700000000000-abc.webm')
    expect(recorder.bytes.value).toBe(42)
  })

  it('refuses to start without a picture', async () => {
    const recorder = useMeetingRecorder()
    const ok = await recorder.start({
      token: 'tok',
      getSource: () => null,
      getStrokes: () => [],
      getAudio: () => null,
    })
    expect(ok).toBe(false)
    expect(recorder.error.value).toBe('noSource')
    expect(recorder.active.value).toBe(false)
  })

  it('reports an engine without MediaRecorder instead of throwing', async () => {
    vi.stubGlobal('MediaRecorder', undefined)
    const recorder = useMeetingRecorder()
    const ok = await recorder.start({
      token: 'tok',
      getSource: source,
      getStrokes: () => [],
      getAudio: () => null,
    })
    expect(ok).toBe(false)
    expect(recorder.error.value).toBe('unsupported')
  })

  it('stops the recording when the server reports a gap', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(async () => ({ ok: false, status: 409, json: async () => ({}) })),
    )
    const recorder = useMeetingRecorder()
    await recorder.start({
      token: 'tok',
      getSource: source,
      getStrokes: () => [],
      getAudio: () => null,
    })
    FakeRecorder.instances[0].emit('lost')
    await settle()

    expect(recorder.error.value).toBe('gap')
    expect(recorder.active.value).toBe(false)
  })
})
