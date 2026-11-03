import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import {
  LiveNotesCapture,
  groupNoteSegments,
  mergeNoteSegments,
  withMeetingFileEvents,
  withMeetingShots,
  type LiveNoteSegment,
  type NotesUploadArgs,
} from '../liveNotes'

function seg(id: number, channel: string, speaker: string, startedAt: number, text: string): LiveNoteSegment {
  return { id, channel, speaker, startedAt, durationMs: 8000, text }
}

/** A segment of a continuous monologue: it runs until the next one starts. */
function spoken(id: number, startedAt: number, text: string): LiveNoteSegment {
  return { id, channel: 'mic', speaker: '我', startedAt, durationMs: 20_000, text }
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

  it('splits a monologue into paragraphs instead of one endless block', () => {
    // Back-to-back 20s segments, the shape the recorder produces while someone
    // talks without pausing: every gap is ~0, so the gap rule alone merges the
    // whole meeting and the panel becomes one wall of text.
    const blocks = groupNoteSegments([
      spoken(1, 0, '第一段'),
      spoken(2, 20_000, '第二段'),
      spoken(3, 40_000, '第三段'),
      spoken(4, 60_000, '第四段'),
      spoken(5, 80_000, '第五段'),
    ])
    expect(blocks.map((b) => b.text)).toEqual(['第一段 第二段 第三段', '第四段 第五段'])
    expect(blocks[0].continuation).toBeUndefined()
    expect(blocks[1].continuation).toBe(true)
  })

  it('does not call a real break a continuation', () => {
    // A pause and a speaker change each start a new turn, which shows the name
    // again — only the span cap splits a turn that is still going.
    const paused = groupNoteSegments([spoken(1, 0, '开场'), spoken(2, 120_000, '很久以后')])
    expect(paused).toHaveLength(2)
    expect(paused[1].continuation).toBeUndefined()

    const afterFile = groupNoteSegments([
      spoken(1, 0, '先看文档'),
      { id: -1, channel: 'file', speaker: '文件', startedAt: 10_000, durationMs: 0, text: 'a.md', kind: 'file' },
      spoken(2, 20_000, '接着说'),
    ])
    expect(afterFile.map((b) => b.continuation)).toEqual([undefined, undefined, undefined])
  })

  it('skips empty text', () => {
    expect(groupNoteSegments([seg(1, 'mic', '我', 1_000, '   ')])).toHaveLength(0)
  })

  it('never merges two file events into one line', () => {
    const blocks = groupNoteSegments([
      { id: -1, channel: 'file', speaker: '文件', startedAt: 1_000, durationMs: 0, text: '打开 a.md', kind: 'file' },
      { id: -2, channel: 'file', speaker: '文件', startedAt: 2_000, durationMs: 0, text: '关闭 a.md', kind: 'file' },
    ])
    expect(blocks).toHaveLength(2)
    expect(blocks.map((b) => b.text)).toEqual(['打开 a.md', '关闭 a.md'])
  })
})

describe('withMeetingFileEvents', () => {
  it('interleaves open/close rows with the speech by time', () => {
    const out = withMeetingFileEvents([seg(1, 'mic', '我', 10_000, '先看文档')], [
      { path: 'docs/a.md', openedAt: 12_000, closedAt: 20_000 },
    ])
    expect(out.map((s) => s.text)).toEqual(['先看文档', 'docs/a.md', 'docs/a.md'])
    expect(out.map((s) => s.fileAction)).toEqual([undefined, 'open', 'close'])
    expect(out[1].kind).toBe('file')
  })

  it('uses the redacted name when the viewer has no path', () => {
    const out = withMeetingFileEvents([], [{ path: '', name: 'a.md', openedAt: 12_000, closedAt: 0 }])
    expect(out[0].text).toBe('a.md')
    expect(out[0].fileAction).toBe('open')
    expect(out).toHaveLength(1) // an open interval has no close row
  })

  it('returns the original list untouched when nothing was opened', () => {
    const segments = [seg(1, 'mic', '我', 1_000, '内容')]
    expect(withMeetingFileEvents(segments, [])).toBe(segments)
  })
})

describe('withMeetingShots', () => {
  it('places frames on the timeline by time, not at the end', () => {
    const rows = withMeetingShots([seg(1, 'mic', '我', 10_000, '先看文档'), seg(2, 'mic', '我', 30_000, '继续')], [
      { id: 8, at: 20_000, reason: 'manual', thumbUrl: '/t/8', fullUrl: '/f/8' },
    ])
    const blocks = groupNoteSegments(rows)
    expect(blocks.map((b) => b.kind ?? 'speech')).toEqual(['speech', 'shot', 'speech'])
    expect(blocks[1].shot?.thumbUrl).toBe('/t/8')
    // The inline thumbnail and the viewer's image are separate URLs: the row
    // must not send the full-size frame to the transcript's <img>.
    expect(blocks[1].shot?.fullUrl).toBe('/f/8')
  })

  it('labels a frame with its reason, and a file trigger with the base name', () => {
    const rows = withMeetingShots([], [
      { id: 3, at: 1_000, reason: 'file', ref: 'C:\\docs\\arch.md', thumbUrl: '/t/3', fullUrl: '/f/3' },
    ])
    // The label is the row's text: the grouper drops blank rows, and the
    // record's "copy all" reads this field.
    expect(rows[0].text).toContain('arch.md')
    expect(rows[0].text).not.toContain('docs')
    expect(rows[0].id).toBe(-3) // negative: never collides with cue ids
  })

  it('gives frames and file rows distinct keys', () => {
    // Both kinds carry negative ids, so the key must include the kind.
    const rows = withMeetingShots(
      withMeetingFileEvents([], [{ path: 'a.md', openedAt: 1_000, closedAt: 0 }]),
      [{ id: 1, at: 1_000, reason: 'manual', thumbUrl: '/t/1', fullUrl: '/f/1' }],
    )
    const keys = groupNoteSegments(rows).map((b) => b.key)
    expect(new Set(keys).size).toBe(keys.length)
  })

  it('returns the original list untouched when no frames were kept', () => {
    const rows = [seg(1, 'mic', '我', 1_000, '内容')]
    expect(withMeetingShots(rows, [])).toBe(rows)
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

/** Every source node handed out, so a test can inspect the routing. */
let fakeSources: Array<{ connect: ReturnType<typeof vi.fn>; disconnect: ReturnType<typeof vi.fn> }> = []
/** Every destination node handed out, in creation order. */
let fakeDests: Array<{ stream: MediaStream; connect: ReturnType<typeof vi.fn>; disconnect: ReturnType<typeof vi.fn> }> = []

function fakeCtx(): AudioContext {
  return {
    destination: {},
    createMediaStreamSource: () => {
      const node = { connect: vi.fn(), disconnect: vi.fn() }
      fakeSources.push(node)
      return node
    },
    createMediaStreamDestination: () => {
      const node = { stream: fakeStream(), connect: vi.fn(), disconnect: vi.fn() }
      fakeDests.push(node)
      return node
    },
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
    fakeSources = []
    fakeDests = []
    fakeLevel = 1
    vi.stubGlobal('MediaRecorder', FakeRecorder)
  })

  afterEach(() => {
    vi.useRealTimers()
    vi.unstubAllGlobals()
  })

  it('records the whole meeting on a shared mix bus', async () => {
    const uploads: NotesUploadArgs[] = []
    const archives: NotesUploadArgs[] = []
    const capture = new LiveNotesCapture(
      fakeCtx(),
      async (args) => {
        uploads.push(args)
        return null
      },
      {
        segmentMs: 1000,
        archiveUpload: async (args) => {
          archives.push(args)
          return null
        },
      },
    )

    capture.addChannel('mic', () => '我', fakeStream())
    await vi.advanceTimersByTimeAsync(1000)

    // Every channel keeps its own track (that is where the speaker label comes
    // from) AND feeds the mix, which is the file the user listens to.
    expect(uploads.map((u) => u.channel)).toEqual(['mic'])
    expect(archives.map((u) => u.channel)).toEqual(['mix'])
    expect(archives[0].speaker).toBe('')

    // The mix recorder's own source must not be wired back into the bus, or the
    // mix would feed itself. The bus is the first destination created: the
    // capture makes it before it builds any recorder.
    const mixBus = fakeDests[0]
    expect(fakeSources[0].connect).not.toHaveBeenCalledWith(mixBus)
    expect(fakeSources[1].connect).toHaveBeenCalledWith(mixBus)

    capture.stop()
    await capture.drainUploads(1000)
  })

  it('records no mix track when no archive uploader is given', async () => {
    const uploads: NotesUploadArgs[] = []
    const capture = new LiveNotesCapture(fakeCtx(), async (args) => {
      uploads.push(args)
      return null
    }, { segmentMs: 1000 })

    capture.addChannel('mic', () => '我', fakeStream())
    expect(FakeRecorder.instances).toHaveLength(1)

    await vi.advanceTimersByTimeAsync(1000)
    expect(uploads.map((u) => u.channel)).toEqual(['mic'])
    expect(fakeDests).toHaveLength(1)
    capture.stop()
    await capture.drainUploads(1000)
  })

  it('uploads a segment per interval and flushes the last one on stop', async () => {
    const uploads: NotesUploadArgs[] = []
    const capture = new LiveNotesCapture(fakeCtx(), async (args) => {
      uploads.push(args)
      return null
    }, { segmentMs: 1000 })

    capture.addChannel('mic', () => '我', fakeStream())
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

  // A viewer's nickname arrives with the join frame, which can land after its
  // first audio track. Resolving the label per segment is what lets the stored
  // transcript (and the minutes) carry the real name.
  it('resolves the speaker label per segment, not once per channel', async () => {
    const uploads: NotesUploadArgs[] = []
    let name = ''
    const capture = new LiveNotesCapture(fakeCtx(), async (args) => {
      uploads.push(args)
      return null
    }, { segmentMs: 1000 })

    capture.addChannel('viewer:v1', () => name || '观众 1', fakeStream())
    await vi.advanceTimersByTimeAsync(1000)
    expect(uploads[0].speaker).toBe('观众 1')

    name = '张三'
    await vi.advanceTimersByTimeAsync(1000)
    expect(uploads[1].speaker).toBe('张三')

    capture.stop()
    await capture.drainUploads(1000)
  })

  it('cuts a segment at a pause instead of on a fixed clock', async () => {
    const uploads: NotesUploadArgs[] = []
    const capture = new LiveNotesCapture(fakeCtx(), async (args) => {
      uploads.push(args)
      return null
    }, { segmentMs: 20_000, minSegmentMs: 1_000 })

    capture.addChannel('mic', () => '我', fakeStream())
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

    capture.addChannel('mic', () => '我', fakeStream())
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
    capture.addChannel('system', () => '系统声音', { getAudioTracks: () => [] } as unknown as MediaStream)
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

    capture.addChannel('mic', () => '我', fakeStream())
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

    capture.addChannel('mic', () => '我', fakeStream())
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

    capture.addChannel('mic', () => '我', fakeStream())
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

    capture.addChannel('viewer:v1', () => '观众 1', fakeStream())
    await vi.advanceTimersByTimeAsync(1000)
    capture.removeChannel('viewer:v1')
    await capture.drainUploads(1000)

    // One from the interval, one flushed when the channel was removed.
    expect(uploads).toHaveLength(2)
    expect(uploads.every((u) => u.channel === 'viewer:v1')).toBe(true)
  })
})
