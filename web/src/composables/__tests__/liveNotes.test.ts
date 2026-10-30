import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import {
  LiveNotesCapture,
  groupNoteSegments,
  mergeNoteSegments,
  type LiveNoteSegment,
  type NotesUploadArgs,
} from '../liveNotes'

function seg(id: number, channel: string, speaker: string, startedAt: number, text: string): LiveNoteSegment {
  return { id, channel, speaker, startedAt, durationMs: 8000, text }
}

describe('mergeNoteSegments', () => {
  it('sorts by speaking time, not by arrival order', () => {
    const merged = mergeNoteSegments(
      [seg(1, 'system', '系统声音', 20_000, '后说的')],
      [seg(2, 'mic', '我', 10_000, '先说的')],
    )
    expect(merged.map((s) => s.text)).toEqual(['先说的', '后说的'])
  })

  it('replaces a row when the same id arrives again', () => {
    const merged = mergeNoteSegments([seg(1, 'mic', '我', 1_000, '半句')], [seg(1, 'mic', '我', 1_000, '完整一句')])
    expect(merged).toHaveLength(1)
    expect(merged[0].text).toBe('完整一句')
  })

  it('ignores the id:0 placeholder returned for silence', () => {
    const merged = mergeNoteSegments([], [seg(0, 'mic', '我', 1_000, '')])
    expect(merged).toHaveLength(0)
  })
})

describe('groupNoteSegments', () => {
  it('merges consecutive lines from the same speaker', () => {
    const blocks = groupNoteSegments([
      seg(1, 'mic', '我', 1_000, '第一句'),
      seg(2, 'mic', '我', 9_000, '第二句'),
      seg(3, 'system', '系统声音', 18_000, '对方说话'),
    ])
    expect(blocks).toHaveLength(2)
    expect(blocks[0].text).toBe('第一句 第二句')
    expect(blocks[1].speaker).toBe('系统声音')
  })

  it('does not merge the same speaker across a long gap', () => {
    const blocks = groupNoteSegments([
      seg(1, 'mic', '我', 1_000, '开场'),
      seg(2, 'mic', '我', 60_000, '很久以后'),
    ])
    expect(blocks).toHaveLength(2)
  })

  it('skips empty text', () => {
    expect(groupNoteSegments([seg(1, 'mic', '我', 1_000, '   ')])).toHaveLength(0)
  })
})

// ── capture ─────────────────────────────────────────────────────────────────

class FakeRecorder {
  static instances: FakeRecorder[] = []
  state: 'inactive' | 'recording' = 'inactive'
  mimeType = 'audio/webm;codecs=opus'
  ondataavailable: ((ev: { data: Blob }) => void) | null = null
  onstop: (() => void) | null = null

  constructor(
    public stream: unknown,
    public opts?: { mimeType?: string },
  ) {
    FakeRecorder.instances.push(this)
  }

  start(): void {
    this.state = 'recording'
  }

  stop(): void {
    this.state = 'inactive'
    this.ondataavailable?.({ data: new Blob(['chunk'], { type: this.mimeType }) })
    this.onstop?.()
  }
}

function fakeStream(): MediaStream {
  const track = { kind: 'audio' } as MediaStreamTrack
  return { getAudioTracks: () => [track] } as unknown as MediaStream
}

/** Current loudness fed to the fake analyser (0 = silence, 1 = speech). */
let fakeLevel = 0

function fakeCtx(): AudioContext {
  return {
    destination: {},
    createMediaStreamSource: () => ({ connect: vi.fn(), disconnect: vi.fn() }),
    createMediaStreamDestination: () => ({
      stream: fakeStream(),
      connect: vi.fn(),
      disconnect: vi.fn(),
    }),
    createAnalyser: () => ({
      fftSize: 512,
      connect: vi.fn(),
      disconnect: vi.fn(),
      getByteTimeDomainData: (buf: Uint8Array) => {
        buf.fill(fakeLevel > 0 ? 200 : 128)
      },
    }),
    createGain: () => ({
      gain: { value: 1 },
      connect: vi.fn(),
      disconnect: vi.fn(),
    }),
  } as unknown as AudioContext
}

describe('LiveNotesCapture', () => {
  beforeEach(() => {
    vi.useFakeTimers()
    FakeRecorder.instances = []
    fakeLevel = 1
    vi.stubGlobal('MediaRecorder', FakeRecorder)
  })

  afterEach(() => {
    vi.useRealTimers()
    vi.unstubAllGlobals()
  })

  it('uploads a segment per interval and flushes the last one on stop', async () => {
    const uploads: NotesUploadArgs[] = []
    const capture = new LiveNotesCapture(fakeCtx(), async (args) => {
      uploads.push(args)
      return null
    }, { segmentMs: 1000 })

    capture.addChannel('mic', '我', fakeStream())
    expect(FakeRecorder.instances).toHaveLength(1)

    await vi.advanceTimersByTimeAsync(1000)
    expect(uploads).toHaveLength(1)
    expect(uploads[0].channel).toBe('mic')
    expect(uploads[0].speaker).toBe('我')
    expect(uploads[0].blob.size).toBeGreaterThan(0)

    await capture.stopAndFlush(1000)
    await capture.drainUploads(1000)
    expect(uploads).toHaveLength(2)
  })

  it('cuts a segment at a pause instead of on a fixed clock', async () => {
    const uploads: NotesUploadArgs[] = []
    const capture = new LiveNotesCapture(fakeCtx(), async (args) => {
      uploads.push(args)
      return null
    }, { segmentMs: 20_000, minSegmentMs: 1_000 })

    capture.addChannel('mic', '我', fakeStream())
    // Still speaking well past the minimum: the segment must stay open.
    await vi.advanceTimersByTimeAsync(2_000)
    expect(uploads).toHaveLength(0)

    // The speaker stops; ~450ms of silence closes the segment long before the
    // 20s ceiling, so no word is cut in half.
    fakeLevel = 0
    await vi.advanceTimersByTimeAsync(600)
    expect(uploads).toHaveLength(1)
    expect(uploads[0].durationMs).toBeGreaterThanOrEqual(1_000)
    expect(uploads[0].durationMs).toBeLessThan(5_000)

    capture.stop()
    await capture.drainUploads(1000)
  })

  it('does not upload a segment that contains no speech', async () => {
    fakeLevel = 0
    const uploads: NotesUploadArgs[] = []
    const capture = new LiveNotesCapture(fakeCtx(), async (args) => {
      uploads.push(args)
      return null
    }, { segmentMs: 5_000, minSegmentMs: 1_000 })

    capture.addChannel('mic', '我', fakeStream())
    await vi.advanceTimersByTimeAsync(3_000)
    // Silence is pure overhead: nothing to transcribe, so nothing is uploaded
    // while the channel stays quiet (it still cycles its recorders).
    expect(uploads).toHaveLength(0)

    await capture.stopAndFlush(1000)
    await capture.drainUploads(1000)
    // The final tail never accumulated a metering sample, and "unknown" must
    // never mean "silent", so it is uploaded rather than judged.
    expect(uploads).toHaveLength(1)
    expect(uploads[0].durationMs).toBeLessThan(1_000)
  })

  it('ignores channels with no audio track', () => {
    const capture = new LiveNotesCapture(fakeCtx(), async () => null, { segmentMs: 1000 })
    capture.addChannel('system', '系统声音', { getAudioTracks: () => [] } as unknown as MediaStream)
    expect(capture.size).toBe(0)
    expect(FakeRecorder.instances).toHaveLength(0)
  })

  it('drops a segment when the upload backlog is full', async () => {
    let releaseFirst: (() => void) | null = null
    let blocked = false
    const uploads: NotesUploadArgs[] = []
    const capture = new LiveNotesCapture(fakeCtx(), async (args) => {
      uploads.push(args)
      // Only the first upload is slow; the rest complete immediately so the
      // drain below terminates.
      if (!blocked) {
        blocked = true
        await new Promise<void>((resolve) => {
          releaseFirst = resolve
        })
      }
      return null
    }, { segmentMs: 1000, maxPending: 1 })

    capture.addChannel('mic', '我', fakeStream())
    // Three segments: the first is in flight, the second queues, the third is
    // dropped (backlog = 1).
    await vi.advanceTimersByTimeAsync(1000)
    await vi.advanceTimersByTimeAsync(1000)
    await vi.advanceTimersByTimeAsync(1000)
    await capture.stopAndFlush(1000)

    expect(uploads).toHaveLength(1)
    releaseFirst!()
    await capture.drainUploads(1000)
    expect(uploads).toHaveLength(2)
  })

  it('retries a failed upload instead of losing the segment', async () => {
    let attempts = 0
    const capture = new LiveNotesCapture(fakeCtx(), async () => {
      attempts += 1
      if (attempts < 3) throw new Error('network')
      return null
    }, { segmentMs: 60_000 })

    capture.addChannel('mic', '我', fakeStream())
    // The only segment is produced when the recorder stops.
    await capture.stopAndFlush(1000)
    expect(attempts).toBe(1)

    await vi.advanceTimersByTimeAsync(1000) // first backoff
    expect(attempts).toBe(2)
    await vi.advanceTimersByTimeAsync(3000) // second backoff
    expect(attempts).toBe(3)

    await capture.drainUploads(1000)
  })

  it('reports a segment as lost only after every retry failed', async () => {
    const lost: string[] = []
    const capture = new LiveNotesCapture(fakeCtx(), async () => {
      throw new Error('backend down')
    }, { segmentMs: 60_000, onSegmentLost: (channel) => lost.push(channel) })

    capture.addChannel('mic', '我', fakeStream())
    await capture.stopAndFlush(1000)
    await vi.advanceTimersByTimeAsync(14_000)
    await capture.drainUploads(1000)

    expect(lost).toEqual(['mic'])
  })

  it('keeps uploading a channel whose viewer left', async () => {
    const uploads: NotesUploadArgs[] = []
    const capture = new LiveNotesCapture(fakeCtx(), async (args) => {
      uploads.push(args)
      return null
    }, { segmentMs: 1000 })

    capture.addChannel('viewer:v1', '观众 1', fakeStream())
    await vi.advanceTimersByTimeAsync(1000)
    capture.removeChannel('viewer:v1')
    await capture.drainUploads(1000)

    // One from the interval, one flushed when the channel was removed.
    expect(uploads).toHaveLength(2)
    expect(uploads.every((u) => u.channel === 'viewer:v1')).toBe(true)
  })
})
